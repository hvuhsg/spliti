package network

import (
	"errors"
	"fmt"
	"time"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/input"
)

// captureLocalInput is registered in PreUpdate. It drains this frame's
// raw input.KeyEvent stream into a buffer that the gate ships to peers on
// the next FixedFirst tick.
func captureLocalInput(c *app.Ctx, state *netState) {
	if state.closed {
		return
	}
	for _, ev := range app.ReadEvents[input.KeyEvent](c) {
		state.pendingInputs = append(state.pendingInputs, rawKey{
			Key:  ev.Key,
			Rune: ev.Rune,
			Mod:  ev.Mod,
		})
	}
}

// runGate is registered as the very first system in FixedFirst. For each
// fixed tick it:
//  1. Clears stale PlayerKey events from any previous fixed iteration.
//  2. Sends the local input frame (possibly empty — idle ticks are first-class).
//  3. Blocks until every live peer sends its input frame for this tick or
//     the configured StallPolicy fires.
//  4. Re-emits all collected keys (local + remote) as PlayerKey events.
//  5. Advances NetClock.Tick.
func runGate(c *app.Ctx, state *netState) {
	if state.closed {
		return
	}

	app.ClearEvents[PlayerKey](c)

	clk := app.GetResource[NetClock](c)
	status := app.GetResource[Status](c)
	tick := clk.Tick

	// Send local ack for this tick.
	localKeys := state.pendingInputs
	state.pendingInputs = nil
	for _, pc := range state.peers {
		if pc.lost {
			continue
		}
		if err := pc.send(frame{
			Kind:      kInput,
			Player:    state.self,
			InputTick: tick,
			Inputs:    localKeys,
		}); err != nil {
			markPeerLost(c, state, pc, fmt.Sprintf("send: %v", err))
		}
	}

	// Emit local PlayerKey events.
	for _, k := range localKeys {
		app.SendEvent(c, PlayerKey{
			Player: state.self,
			Tick:   tick,
			Key:    k.Key,
			Rune:   k.Rune,
			Mod:    k.Mod,
		})
	}

	// Wait for each remote peer's ack for this tick.
	for _, pc := range state.peers {
		if pc.lost {
			continue
		}
		if err := waitTickAck(c, state, pc, tick); err != nil {
			applyStallPolicy(c, state, pc, err)
		}
	}

	// If at least one live peer remains and we got through, restore Running.
	if status.Phase == Stalled {
		status.Phase = Running
		status.StalledFor = 0
	}

	clk.Tick++
}

// waitTickAck blocks until pc delivers a kInput frame for tick, or the
// stall threshold fires. On success the matching keys are emitted as
// PlayerKey events and nil is returned.
//
// Status.Phase flips to Stalled after a brief grace (100ms) so HUDs can
// react before StallGrace expires.
func waitTickAck(c *app.Ctx, state *netState, pc *peerConn, tick uint64) error {
	p := state.plugin
	status := app.GetResource[Status](c)

	var graceCh <-chan time.Time
	if p.StallPolicy != PauseForever {
		graceCh = time.After(p.StallGrace)
	}

	const stalledThreshold = 100 * time.Millisecond
	var stallSignal <-chan time.Time = time.After(stalledThreshold)

	for {
		select {
		case f, ok := <-pc.in:
			if !ok {
				return errors.New("peer disconnected")
			}
			switch f.Kind {
			case kBye:
				return fmt.Errorf("peer sent Bye: %s", f.Reason)
			case kInput:
				if f.InputTick != tick {
					// TCP guarantees order, so a mismatched tick means the peer
					// got out of sync — drop and keep waiting; if they recover
					// we'll see the right tick eventually.
					continue
				}
				for _, k := range f.Inputs {
					app.SendEvent(c, PlayerKey{
						Player: pc.id,
						Tick:   tick,
						Key:    k.Key,
						Rune:   k.Rune,
						Mod:    k.Mod,
					})
				}
				if status.Phase == Stalled {
					status.Phase = Running
					status.StalledFor = 0
				}
				return nil
			default:
				// Unexpected frame kind — ignore and keep waiting.
				continue
			}
		case <-stallSignal:
			if status.Phase != Stalled {
				status.Phase = Stalled
				status.StalledFor = stalledThreshold
			}
			stallSignal = nil // don't re-fire
		case <-graceCh:
			return errors.New("stall grace expired")
		}
	}
}

func applyStallPolicy(c *app.Ctx, state *netState, pc *peerConn, err error) {
	switch state.plugin.StallPolicy {
	case StopOnStall:
		markPeerLost(c, state, pc, err.Error())
		c.App().Stop()
	case PauseThenDrop:
		markPeerLost(c, state, pc, err.Error())
	case PauseForever:
		// Should not be reachable: waitTickAck has no graceCh under
		// PauseForever. Treat as disconnect for safety.
		markPeerLost(c, state, pc, err.Error())
	}
}

func markPeerLost(c *app.Ctx, state *netState, pc *peerConn, reason string) {
	if pc.lost {
		return
	}
	pc.lost = true
	pc.close()
	status := app.GetResource[Status](c)
	status.Phase = PeerLost
	status.LostPeer = pc.id
	app.SendEvent(c, PeerLeft{ID: pc.id, Reason: reason})
}
