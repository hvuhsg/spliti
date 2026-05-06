# Network

`network.Plugin` adds 2..N-player multiplayer over TCP using **lockstep input synchronisation**. Game code stays nearly identical to single-player; the API change is a single event type.

## The model in one paragraph

Every peer captures its own keyboard and broadcasts an *acknowledgment* per fixed simulation tick — including idle ticks where the player did nothing. The simulation only advances after every peer's ack for the current tick has arrived. Inputs are then re-emitted as `network.PlayerKey` events tagged with the sender's `PlayerID` and the tick number. Because every peer consumes the exact same input sequence at the exact same tick rate, both peers compute the same world state without ever sending the world state itself.

Latency: each peer sees its own input delayed by ~RTT. For 10-Hz games (Snake at 100 ms tick) this is invisible. For action games (Counter-Strike pace) it isn't — that's where rollback netcode comes in, which `spliti` doesn't have yet.

## Drop-in usage

```go
import (
    "github.com/hvuhsg/spliti/app"
    "github.com/hvuhsg/spliti/plugin/defaultplugins"
    "github.com/hvuhsg/spliti/plugin/network"
)

func main() {
    a := app.New()
    a.AddPlugins(defaultplugins.Plugins{...})
    a.AddPlugins(network.Plugin{
        Mode:    network.Host,    // or network.Client
        Listen:  ":7777",         // host only
        Connect: "host:7777",     // client only
        Players: 2,               // total expected; default 2
    })
    // … your game systems …
    a.Run()
}
```

Plugin order: add it **after** `defaultplugins.Plugins{}`. The network plugin captures `input.KeyEvent` events, so the input plugin must already be installed.

## What the plugin installs

### Resources

```go
type LocalPlayer struct{ ID PlayerID; Total uint8 }   // your assigned id and player count
type NetClock    struct{ Tick uint64 }                // synchronised simulation tick
type Random      struct{ *rand.Rand }                 // RNG seeded identically on all peers
type Status      struct {                             // for HUD overlays
    Phase      Phase                                  // Connecting | Running | Stalled | PeerLost | Closed
    StalledFor time.Duration
    LostPeer   PlayerID
}
```

Read them in any system:

```go
me := app.GetResource[network.LocalPlayer](c)         // me.ID, me.Total
r  := app.GetResource[network.Random](c)              // r.Intn(50)
st := app.GetResource[network.Status](c)              // st.Phase
```

### Events

```go
type PlayerKey struct {
    Player PlayerID
    Tick   uint64
    Key    tcell.Key
    Rune   rune
    Mod    tcell.ModMask
}

type PeerJoined struct{ ID PlayerID }
type PeerLeft   struct{ ID PlayerID; Reason string }
```

Game code reads `PlayerKey` instead of `input.KeyEvent` for game logic:

```go
for _, ev := range app.ReadEvents[network.PlayerKey](c) {
    snake := snakes.ByOwner(ev.Player)
    // … route based on ev.Player and ev.Key/Rune
}
```

`PeerJoined` fires once per remote during the connecting → running transition. `PeerLeft` fires on disconnect (clean Bye, socket error, or stall-timeout drop).

### Systems

- `__spliti_net_handshake` (PreStartup): runs the host accept-and-greet or client dial-and-await, blocks until everyone is connected. Panics on timeout or version mismatch.
- `__spliti_net_capture` (PreUpdate): drains this frame's `Events[input.KeyEvent]` into a per-tick buffer for the next outbound ack.
- `__spliti_net_gate` (FixedFirst, before any user FixedFirst system): the heart of lockstep — sends the local ack, blocks on remote acks, emits `PlayerKey` events, advances `NetClock.Tick`.

You don't reference these labels directly in user code.

## Idle ticks are first-class

The protocol's tick of work for a peer is "send a tick acknowledgment frame." Whether you pressed any keys or not is irrelevant — every peer sends one ack per fixed tick. So:

| Tick | P0 sent | P1 sent | What happens |
|---|---|---|---|
| 0 | `kInput{Inputs: [Up]}` | `kInput{Inputs: []}` | Both gates unblock; one PlayerKey emitted; tick advances |
| 1 | `kInput{Inputs: []}`   | `kInput{Inputs: []}` | Both gates unblock; no PlayerKey; tick advances |
| 2 | `kInput{Inputs: [Down]}` | `kInput{Inputs: [Right]}` | Both gates unblock; two PlayerKey emitted; tick advances |

A silent player progresses the simulation as fast as a key-mashing one. There's no "waiting for the other player to do something." The only thing that stalls is network latency or a real disconnect.

## Stall policies

When a peer's ack doesn't arrive within `StallGrace` (default 5 s):

```go
type StallPolicy int
const (
    PauseThenDrop StallPolicy = iota   // default
    PauseForever
    StopOnStall
)
```

| Policy | Behavior |
|---|---|
| `PauseThenDrop` | Block the simulation. After `StallGrace`, mark the peer as lost, emit `PeerLeft`, continue with empty inputs from that peer for the rest of the session. Game decides whether to surface a "disconnected" overlay or end the round. |
| `PauseForever` | Block indefinitely. Useful for puzzle games where a stalled match shouldn't advance. There's no reconnect in v1, so this effectively hangs on disconnect. |
| `StopOnStall` | After `StallGrace`, call `App.Stop()` and exit cleanly. |

Configure on the plugin:

```go
network.Plugin{
    Mode:        network.Host,
    Listen:      ":7777",
    StallPolicy: network.PauseThenDrop,
    StallGrace:  3 * time.Second,
}
```

While stalled but before grace expires, `Status.Phase` flips to `Stalled` so HUDs can react. After grace expires under `PauseThenDrop`, it becomes `PeerLost`.

## Determinism contract

This is the only real cost of lockstep. Game systems running in `FixedFirst`, `FixedUpdate`, and `FixedLast` must produce identical results across peers. To stay deterministic:

1. **Use `network.Random` for any RNG** in fixed-step systems. The host generates the seed during the handshake and broadcasts it; both peers' RNGs are seeded identically.

   ```go
   r := app.GetResource[network.Random](c)
   x := r.Intn(boardW)              // ✓ deterministic
   // x := rand.Intn(boardW)        // ✗ math/rand global is NOT seeded across peers
   ```

2. **Don't read `time.Now()` in fixed-step systems.** Use `time.Time.Elapsed()` from the time plugin if you need a clock, since both peers reach the same `Elapsed()` value at the same `NetClock.Tick`.

3. **Don't iterate Go maps in a way that affects visible behavior.** Map iteration order is randomized per process. If you spawn entities or update state based on map iteration, the two peers will diverge. Sort keys first, or use slices.

4. **Floating-point math is fine in practice** for amd64/arm64 with default Go compiler flags. If you're targeting weird platforms, validate with a determinism spot-check (run two clients with the same scripted inputs, compare board hashes after N ticks).

`Update`, `PostUpdate`, and `Last` stages are *not* on the determinism contract — they can read `time.Now()` and use `math/rand` freely. So local-only concerns (rendering, frame-paced animations, audio) don't pay the determinism cost.

## What about "quit" and "restart"?

Quitting is a **local** concern — read `input.KeyEvent` (not `PlayerKey`) and call `App.Stop()`. The other peer will see `PeerLeft` on the next gate.

```go
func handleLocalQuit(c *app.Ctx) {
    for _, ev := range app.ReadEvents[input.KeyEvent](c) {
        if ev.Rune == 'q' || ev.Key == tcell.KeyCtrlC {
            c.App().Stop()
            return
        }
    }
}
```

Restart is a **shared** concern — read `network.PlayerKey` so both peers transition together:

```go
func handleRestart(c *app.Ctx) {
    state := app.GetState[GameMode](c)
    if state.Get() != GameOver { return }
    for _, ev := range app.ReadEvents[network.PlayerKey](c) {
        if ev.Rune == 'r' {
            state.Set(Playing)        // both peers set this on the same tick
            return
        }
    }
}
```

Both peers see the same `PlayerKey('r', tick=N)` on the same tick, both queue the transition, both fire the same `OnExit(GameOver)` + `OnEnter(Playing)` on the next StateTransition stage.

## Wire format (informational)

One TCP connection per peer-pair. Long-lived `gob.Encoder`/`gob.Decoder` per connection. The wire format is one struct envelope (`frame`) with a `frameKind` discriminator:

```
kHello   (Client → Host)   protocol version
kWelcome (Host → Client)   assigned PlayerID, total peers, RNG seed
kReady   (Host → all)      "tick 0 starts now"
kInput   (bidirectional)   per-tick ack, possibly with keys
kBye     (any → any)       graceful close, with reason
```

Encoded gob, no length-prefix needed (gob frames itself). One reader goroutine and one writer per connection — the App goroutine writes after handshake, no mutex.

## Limitations (v1)

- **No reconnect.** A dropped peer can't rejoin. State recovery would need either input-log replay or a state snapshot transfer.
- **No NAT traversal.** Peers must be reachable by direct IP:port. Use a relay server outside the engine if needed.
- **No rollback / prediction.** Lockstep latency is RTT-bounded. Future work could swap the gate for a rollback-style buffer using the same `PlayerKey` event surface.
- **No encryption.** Trusted peers only.
- **One pre-update hook slot.** The time plugin owns it. If a future plugin needed to take it, we'd convert the field to a slice — straightforward but not done yet.

## Verifying it works (manually)

Two terminals on one machine:

```bash
go run github.com/hvuhsg/spliti/examples/snake-net -mode=host   -addr=:7777
go run github.com/hvuhsg/spliti/examples/snake-net -mode=client -addr=localhost:7777
```

Both windows should show the same board every frame. Eat food on either side and both score columns update. Quit one with `q` — the other should display "player N left" in an overlay.

For a quick determinism spot-check: kill one peer with `kill -STOP`. The other should freeze the simulation (its gate is blocking on the missing ack), display the `Stalled` overlay within ~100 ms, then transition to `PeerLost` after 5 s and resume with no input from the dead peer.

## Next

- The `examples/snake-net/main.go` source is the canonical end-to-end example. Read it alongside the single-player `examples/snake/main.go` to see exactly what changes for multiplayer.
- [docs/events-and-states.md](events-and-states.md) — `PlayerKey` follows the standard event lifetime; `ClearEvents` is what keeps fixed-tick events isolated across catch-up iterations.
