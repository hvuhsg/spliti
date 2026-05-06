// Package network adds TCP-based lockstep multiplayer to spliti.
//
// Drop-in usage:
//
//	a.AddPlugins(defaultplugins.Plugins{...})
//	a.AddPlugins(network.Plugin{
//	    Mode: network.Host, Listen: ":7777", Players: 2,
//	})
//
// Game code reads network.PlayerKey events instead of input.KeyEvent. Each
// PlayerKey carries the originating PlayerID and the simulation tick on which
// it should apply. Both peers see identical event sequences because the
// simulation is gated to advance only after every peer's per-tick input
// acknowledgment has arrived.
//
// # Determinism contract
//
// Game systems running in FixedFirst/FixedUpdate/FixedLast must produce
// identical results across peers. To stay deterministic:
//
//   - Use app.GetResource[network.Random](ctx).Intn(...) instead of math/rand
//     package globals.
//   - Do not read time.Now() in fixed-step systems.
//   - Do not rely on Go map iteration order for visible behaviour.
//
// # Idle inputs are first-class
//
// Every peer sends one ack per fixed tick whether the player pressed
// anything or not. The gate blocks on ack arrival, not on key presses; idle
// players progress the simulation just as fast as active ones.
package network

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
)

// PlayerID is a peer's assigned numeric id. Host is always 0; clients are
// numbered 1..Players-1 in connection order.
type PlayerID uint8

// Mode selects the role of the local peer.
type Mode int

const (
	Host Mode = iota
	Client
)

// StallPolicy selects what to do when a peer's tick acknowledgment is late.
type StallPolicy int

const (
	// PauseThenDrop blocks the simulation until either the input arrives
	// or StallGrace expires. On expiry, the peer is marked PeerLost and
	// the simulation continues with empty inputs from that peer for the
	// rest of the session. This is the default.
	PauseThenDrop StallPolicy = iota
	// PauseForever blocks indefinitely. There is no reconnect in v1, so
	// this hangs the game on disconnect. Suitable for puzzle games where
	// a stalled match should not advance.
	PauseForever
	// StopOnStall calls App.Stop after StallGrace expires, exiting cleanly.
	StopOnStall
)

// Phase reports overall network status. Read via *Status resource for HUDs.
type Phase int

const (
	Connecting Phase = iota
	Running
	Stalled
	PeerLost
	Closed
)

func (p Phase) String() string {
	switch p {
	case Connecting:
		return "Connecting"
	case Running:
		return "Running"
	case Stalled:
		return "Stalled"
	case PeerLost:
		return "PeerLost"
	case Closed:
		return "Closed"
	}
	return fmt.Sprintf("Phase(%d)", int(p))
}

// Plugin is the user-facing configuration. Add it to App after the time and
// input plugins (i.e. after defaultplugins.Plugins{}).
type Plugin struct {
	Mode             Mode
	Listen           string        // Host
	Connect          string        // Client
	Players          int           // total expected; defaults to 2 when zero
	StallPolicy      StallPolicy
	StallGrace       time.Duration // default 5s
	HandshakeTimeout time.Duration // default 30s
}

// LocalPlayer is the resource exposing this peer's assigned id and total
// peer count, set during the PreStartup handshake.
type LocalPlayer struct {
	ID    PlayerID
	Total uint8
}

// NetClock is the synchronised simulation tick. Increments once per
// successful gate pass (one increment per fixed tick).
type NetClock struct {
	Tick uint64
}

// Random is an RNG seeded identically on all peers. Use this in fixed-step
// systems instead of the math/rand package globals.
type Random struct {
	*rand.Rand
}

// Status reports current network state for HUD overlays.
type Status struct {
	Phase      Phase
	StalledFor time.Duration
	LostPeer   PlayerID
}

// PlayerKey is the networked input event. One is emitted per logical key
// press by any peer, in deterministic order, scoped to a single tick.
type PlayerKey struct {
	Player PlayerID
	Tick   uint64
	Key    tcell.Key
	Rune   rune
	Mod    tcell.ModMask
}

// PeerJoined fires once per remote peer during the Connecting → Running
// transition.
type PeerJoined struct{ ID PlayerID }

// PeerLeft fires when a peer disconnects (graceful Bye, socket error, or
// timeout under PauseThenDrop).
type PeerLeft struct {
	ID     PlayerID
	Reason string
}

// netState is the package-private mutable state shared between handshake,
// capture system, gate system, and shutdown hook. It lives as a resource
// so any system can find it via the App.
type netState struct {
	plugin Plugin
	peers  []*peerConn
	self   PlayerID
	total  uint8
	seed   int64

	pendingInputs []rawKey
	closed        bool
}

// Build implements app.Plugin.
func (p Plugin) Build(a *app.App) {
	p = p.applyDefaults()

	app.InsertResource(a, &LocalPlayer{})
	app.InsertResource(a, &NetClock{})
	app.InsertResource(a, &Random{Rand: rand.New(rand.NewSource(0))})
	app.InsertResource(a, &Status{Phase: Connecting})

	state := &netState{plugin: p}

	a.AddSystems(schedule.PreStartup, app.System(func(c *app.Ctx) {
		runHandshake(c, p, state)
	}).Label("__spliti_net_handshake"))

	a.AddSystems(schedule.PreUpdate, app.System(func(c *app.Ctx) {
		captureLocalInput(c, state)
	}).Label("__spliti_net_capture"))

	a.AddSystems(schedule.FixedFirst, app.System(func(c *app.Ctx) {
		runGate(c, state)
	}).Label("__spliti_net_gate"))

	a.AddOnExit(func() { state.shutdown() })
}

func (p Plugin) applyDefaults() Plugin {
	if p.Players == 0 {
		p.Players = 2
	}
	if p.StallGrace == 0 {
		p.StallGrace = 5 * time.Second
	}
	if p.HandshakeTimeout == 0 {
		p.HandshakeTimeout = 30 * time.Second
	}
	return p
}

func runHandshake(c *app.Ctx, p Plugin, state *netState) {
	var res *handshakeResult
	var err error
	switch p.Mode {
	case Host:
		res, err = runHostHandshake(p)
	case Client:
		res, err = runClientHandshake(p)
	default:
		panic(fmt.Sprintf("network.Plugin: unknown mode %v", p.Mode))
	}
	if err != nil {
		panic(fmt.Errorf("network handshake failed: %w", err))
	}
	state.peers = res.peers
	state.self = res.self
	state.total = res.total
	state.seed = res.seed

	*app.GetResource[LocalPlayer](c) = LocalPlayer{ID: res.self, Total: res.total}
	*app.GetResource[Random](c) = Random{Rand: rand.New(rand.NewSource(res.seed))}
	app.GetResource[Status](c).Phase = Running

	for _, pc := range res.peers {
		app.SendEvent(c, PeerJoined{ID: pc.id})
	}
}

func (s *netState) shutdown() {
	if s.closed {
		return
	}
	s.closed = true
	for _, pc := range s.peers {
		if pc.lost {
			continue
		}
		_ = pc.send(frame{Kind: kBye, Reason: "shutdown"})
		pc.close()
	}
}
