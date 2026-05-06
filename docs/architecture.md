# Architecture

`spliti` is a small wrapper around three Bevy-shaped abstractions and one ECS storage layer (arche). Everything else is built from these.

```
┌──────────────────────────────────────────────────────────────────┐
│                            App                                  │
│  ┌────────────┐  ┌─────────────────────────────────────────┐     │
│  │  Plugins   │  │  Schedule (named stages of systems)     │     │
│  │  (Build)   │  │                                         │     │
│  └────────────┘  └─────────────────────────────────────────┘     │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐                  │
│  │ Resources  │  │  Events    │  │   States   │                  │
│  │ (typed)    │  │  Events[T] │  │  State[T]  │                  │
│  └────────────┘  └────────────┘  └────────────┘                  │
│  ┌─────────────────────────────────────────────┐                 │
│  │             World (arche.World)             │                 │
│  │   entities + archetype-stored components    │                 │
│  └─────────────────────────────────────────────┘                 │
└──────────────────────────────────────────────────────────────────┘
```

## App lifecycle

```
app.New()                  ← world, ctx, empty schedule
  └─ AddPlugins(...)       ← each plugin's Build runs synchronously
       └─ Build(app):
            InsertResource[T](...)
            AddSystems(stage, ...)
            AddOnExit(fn)
            SetPreUpdateHook(fn)        (rare; time plugin uses it)
  └─ AddSystems(stage, ...)
  └─ InitState[T](...) (optional)
  └─ Run()
       resolveAll()                     ← topo-sort every stage
       PreStartup → Startup → PostStartup
       fireInitialOnEnter()             ← one-shot OnEnter for current state
       loop {
         First → PreUpdate
           applyStateTransitions()      ← OnExit(prev) + OnEnter(next)
         StateTransition (user systems here)
           preUpdateHook()               ← time plugin runs FixedUpdate 0..N times
         Update → PostUpdate → Last
           drainAllEvents()              ← clear every Events[T] buffer
           postUpdateHook()              ← frame pacing sleep
       }
       OnExit hooks (LIFO; runs even on panic)
```

The loop terminates when `App.Stop()` is called or `SetMaxFrames(N)` reaches its bound.

## Plugins

A plugin is a value with one method:

```go
type Plugin interface {
    Build(app *App)
}
```

Plugins are pure construction — they run once during `AddPlugins` and don't persist as runtime objects. Whatever they install (resources, systems, hooks) becomes part of the App.

The standard plugin set is in `plugin/defaultplugins`:

```go
defaultplugins.Plugins{}
  ├─ time.Plugin{}        ← Time resource, FixedUpdate accumulator, frame pacing
  ├─ terminal.Plugin{}    ← tcell.Screen as a shared *Terminal resource
  ├─ input.Plugin{}       ← background event poller → Events[KeyEvent]
  └─ tui.Plugin{}         ← Position+Glyph render system + present
```

`network.Plugin` is opt-in — it's not in the default bundle because not every game wants the network. See [docs/network.md](network.md).

## Schedule

Stages are named string-typed identifiers in the `schedule` package. The hard-coded order is:

```
Startup once:    PreStartup, Startup, PostStartup
Per frame:       First, PreUpdate, StateTransition, Update, PostUpdate, Last
Per fixed tick:  FixedFirst, FixedUpdate, FixedLast
                 (run 0..N times per frame by the time plugin)
```

You don't add stages — they're a fixed taxonomy borrowed from Bevy. You register systems into them.

Within a single stage, ordering between systems is controlled by `.Before(label)`, `.After(label)`, and `.Chain(...)`. The scheduler topo-sorts at `Run()` and panics on cycles or unknown labels.

## ECS

Storage is provided by arche. `spliti` adds idiomatic-Go generic wrappers so call sites stay short:

```go
// Iterate two-component entities
app.Query2[Position, Velocity](ctx, func(e ecs.Entity, p *Position, v *Velocity) {
    p.X += v.X
})

// Spawn (deferred via the commands buffer)
app.Spawn2[Position, Velocity](ctx.Commands(), func(p *Position, v *Velocity) {
    *p = Position{X: 0, Y: 0}
    *v = Velocity{X: 1, Y: 0}
})

// Spawn (immediate via arche directly)
m := generic.NewMap2[Position, Velocity](ctx.World())
m.NewWith(&Position{}, &Velocity{X: 1, Y: 0})
```

Both spawn paths exist because they suit different moments. The deferred path runs at the end of the current stage and is safer to use mid-iteration. The direct path is fine in `Startup` or any context where you want the entity ID immediately.

Resources are `reflect.Type`-keyed singletons:

```go
app.InsertResource(a, &Score{})
score := app.GetResource[Score](ctx)
```

## Why this shape

The decisions worth knowing about:

- **Single-threaded.** Parallel systems require static query-conflict analysis, which is a lot of machinery for marginal gains in a CLI game. Single-threaded scheduling means we can drain the deferred commands buffer at the end of every stage with no synchronisation primitives. If parallelism becomes worth it later, the public API doesn't have to change.
- **Reflection-keyed resources, not type-erased keys.** The pattern `GetResource[T](ctx)` reads cleanly and there's no parallel API in arche to integrate with.
- **arche, not a hand-rolled ECS.** Storage is the most expensive part of an ECS to get right. arche has archetype storage with cache-friendly iteration, mature query APIs, and a small surface. Wrapping it lets us focus on the App/Plugin/Schedule layer.
- **tcell, not bubbletea/ratatui-port.** tcell exposes the cell grid we want. Bubbletea is Elm-shaped and fights ECS. Ratatui is Rust.
- **Bevy stage names without Bevy's parallelism.** The names (`First`, `PreUpdate`, `FixedUpdate`, etc.) carry meaning to anyone arriving from Bevy. Their semantics are preserved; only the parallelism is dropped.

## Next

- [docs/scheduling.md](scheduling.md) — full stage taxonomy and ordering rules.
- [docs/plugins.md](plugins.md) — writing your own plugin, lifecycle hooks.
