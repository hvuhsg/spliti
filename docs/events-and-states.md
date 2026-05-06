# Events & States

Two adjacent ways for systems to coordinate without sharing pointers: events for transient messages, states for "where in the game am I right now?"

## Events

`Events[T]` is a typed message buffer. Any Go type can be an event:

```go
type ScoreChanged struct{ Delta int }
type Quit struct{}
```

There's no registration step — types appear on first use.

### Sending and reading

```go
// Producer
app.SendEvent(c, ScoreChanged{Delta: 1})

// Consumer (in any later system, this frame)
for _, ev := range app.ReadEvents[ScoreChanged](c) {
    score.Value += ev.Delta
}
```

`SendEvent` appends to the per-type buffer. `ReadEvents` returns a snapshot slice. Order is insertion order.

### Lifetime

The engine drains every `Events[T]` buffer **once per frame**, after the `Last` stage. So:

- An event sent at any point during frame N is readable by any system later in frame N.
- An event sent at the very end of frame N is **not** readable in frame N+1 — the drain wipes it.
- An event sent in `FixedUpdate` is readable in subsequent FixedUpdate iterations of the same frame, **and** in `Update`, `PostUpdate`, `Last`. It's drained at the end of the frame.

This is intentionally simpler than Bevy's double-buffered model. If you need cross-frame events, copy them into a resource yourself.

### Manual clearing — `ClearEvents[T]`

Sometimes you need to drop the buffer mid-frame. The canonical case: a plugin emits events at the start of each fixed iteration (e.g. `network.Plugin` emitting `PlayerKey`), and the next iteration shouldn't see the previous one's events. The plugin calls `app.ClearEvents[T](ctx)` at the top of its fixed-tick system.

```go
app.ClearEvents[PlayerKey](c)        // discard the current buffer
```

Game code rarely needs this — the per-frame drain is enough.

### Events vs resources — when to pick which

| Use an event when | Use a resource when |
|---|---|
| The data is a discrete occurrence ("a key was pressed", "the player died") | The data is continuous state ("current score", "which level") |
| Multiple producers may emit independently | One value at a time |
| Consumers should be allowed to miss it (events disappear after the frame) | Consumers should always see the latest |

## States

States are a typed state machine. One state machine per type.

```go
type GameMode int
const (
    Menu GameMode = iota
    Playing
    GameOver
)

// Init at App build time
app.InitState(a, Menu)

// Hook OnEnter / OnExit systems per value
app.OnEnter(a, Playing,  setupGame)
app.OnExit(a, Playing,   teardownGame)
app.OnEnter(a, GameOver, showGameOverScreen)
```

In a system, read or queue a transition:

```go
state := app.GetState[GameMode](c)
state.Get()                  // → current value
state.Set(Playing)           // queue transition; not applied immediately
```

### When transitions actually run

Queued transitions are applied at the start of `StateTransition` stage (between `PreUpdate` and `Update`). For each machine with a queued `next`:

1. `OnExit` systems for the current value run.
2. `current ← next`, `next ← nil`.
3. `OnEnter` systems for the new value run.

The initial state's `OnEnter` runs once at the end of startup, before the loop begins. So if `InitState(a, Playing)` is your initial value, the `OnEnter(Playing)` callback fires before the first frame.

This means: the first frame your `Update` systems run in, the world is already in the "post-OnEnter" state. There's no chicken-and-egg.

### Multiple state machines

Each `InitState[T]` creates a separate machine, keyed by the type. Two machines of different types coexist without collision:

```go
type GameMode int
type SoundMode int

app.InitState(a, Menu)
app.InitState(a, MusicOn)

// Both transition independently
app.GetState[GameMode](c).Set(Playing)
app.GetState[SoundMode](c).Set(MusicOff)
```

Re-`InitState`-ing the same type panics — types are unique.

## Patterns

### Restart on a key press

```go
// In Update or FixedUpdate
state := app.GetState[GameMode](c)
if state.Get() == GameOver {
    for _, ev := range app.ReadEvents[input.KeyEvent](c) {
        if ev.Rune == 'r' {
            state.Set(Playing)
            return
        }
    }
}
```

`OnExit(GameOver)` runs (cleanup), then `OnEnter(Playing)` runs (fresh setup). Perfect symmetry.

### Pausing

Add a `Paused` state and gate game systems with `RunIf`:

```go
type GameMode int
const (Playing GameMode = iota; Paused; GameOver)

a.AddSystems(schedule.FixedUpdate,
    app.System(physicsStep).RunIf(func(c *app.Ctx) bool {
        return app.GetState[GameMode](c).Get() == Playing
    }))
```

### Debounced events

Events live for one frame. If you want "fire only once per N frames" semantics, accumulate in a resource and emit conditionally — the event system itself doesn't do throttling.

## Next

- [docs/plugins.md](plugins.md) — how the standard plugins use events for input, time, and network.
- [docs/network.md](network.md) — `PlayerKey` is a network-emitted event; `PeerLeft`/`PeerJoined` are state-transition events.
