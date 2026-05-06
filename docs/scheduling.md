# Scheduling

A `spliti` App runs systems through a fixed taxonomy of stages, with explicit ordering edges between systems within a stage. This doc covers the stages, the registration forms, and the rules.

## Stage taxonomy

```
once at startup:           per frame:                        per fixed tick:
  PreStartup                 First                             FixedFirst
  Startup                    PreUpdate                         FixedUpdate
  PostStartup                StateTransition                   FixedLast
                             [time plugin runs FixedUpdate    ↑ run 0..N times by time plugin
                              0..N times here]
                             Update
                             PostUpdate
                             Last
                           [event drain + frame pacing]
```

Strings (`schedule.Stage` is `type Stage string`) name the stages. They're constants in the `schedule` package — `schedule.Update`, `schedule.PreStartup`, etc.

### When to use which

| Stage | Typical use |
|---|---|
| `Startup` | One-shot world setup: spawn the player, load static data. |
| `First` | Side-effect-free pre-frame work (engine: input drain, time tick). |
| `PreUpdate` | Read input, accumulate per-frame inputs (engine: network capture). |
| `StateTransition` | Systems that should run after auto-transitions, before normal logic. Rarely user-facing. |
| `FixedUpdate` | Simulation logic that needs a stable tick rate: movement, physics, AI. |
| `Update` | Per-frame game logic that doesn't need a fixed cadence: UI input handling, animations driven by `Time.Delta`. |
| `PostUpdate` | Reaction systems: cleanup of dead entities, transform propagation, **rendering** (engine). |
| `Last` | Genuinely last things — debug logging, frame counters. |

Most user code lives in `Update`, `FixedUpdate`, and `Startup`. The others are mostly engine-occupied.

## Registering systems

`AddSystems` accepts a stage and one or more system items. Three forms are supported:

```go
// Bare function — same as a SystemConfig with no metadata
a.AddSystems(schedule.Update, MoveSystem)

// SystemConfig — adds label + ordering + run conditions
a.AddSystems(schedule.Update,
    app.System(MoveSystem).Label("move"),
    app.System(RenderSystem).Label("render").After("move"),
)

// Chain — sugar that wires sequential .After edges
a.AddSystems(schedule.Update, app.Chain(
    app.System(StepA),
    app.System(StepB),
    app.System(StepC),
))
```

Type-wise, `AddSystems(stage, items ...any)` accepts `SystemFunc`, `*SystemConfig`, or `[]*SystemConfig` (what `Chain` returns). Anything else panics.

## Ordering edges

```go
app.System(fn).Label("name")                 // give it a name to be referenced
app.System(fn).Before("other_label")         // must run before that system
app.System(fn).After("other_label")          // must run after that system
app.System(fn).Before("a", "b", "c")         // multiple edges, all enforced
app.System(fn).After("a").Before("b")        // chain configuration
```

Labels are string-keyed. They must be unique within a single stage (duplicate label panics at `Run()`). References to labels that don't exist also panic — there's no silent fallback.

`Chain(s1, s2, s3)` is sugar: it auto-labels each entry and adds `s2.After(s1.label)`, `s3.After(s2.label)`. Equivalent to spelling those out.

## Run conditions

Gate execution per-tick:

```go
app.System(fn).RunIf(func(c *app.Ctx) bool {
    return app.GetState[GameMode](c).Get() == Playing
})
```

`RunIf` ANDs together if you call it more than once. Conditions are evaluated each tick of the stage (including each fixed iteration if the system is in `FixedUpdate`).

The system is still ordered into the DAG normally; `RunIf` only decides whether to call its body. Other systems' `.After(thisOne)` edges are unaffected by skipped ticks.

## Resolution rules

When you call `Run()`:

1. For each stage, build a directed graph from `Before`/`After` edges.
2. Stable topo-sort (Kahn's algorithm with insertion-order tiebreak).
3. Cycles → panic with the stage name.
4. Unknown labels → panic naming the offending edge.

The order is fixed before the loop starts. There's no dynamic re-ordering at runtime.

## FixedUpdate

The time plugin owns the fixed-tick loop. Each main-loop frame, after `StateTransition` and before `Update`, the time plugin's hook fires:

```
accumulator += Time.Delta()
while accumulator >= FixedTimestep:
    run FixedFirst stage
    run FixedUpdate stage
    run FixedLast stage
    accumulator -= FixedTimestep
```

The accumulator is capped at `4 * FixedTimestep` to prevent the spiral-of-death after a long stall.

If `FixedTimestep` is unconfigured, it defaults to `time.Second / 64` (64 Hz). For something like Snake at 10 Hz:

```go
splititime.Plugin{FixedTimestep: 100 * gotime.Millisecond}
```

A frame can run zero, one, or many fixed iterations. Game logic in `FixedUpdate` should not assume "exactly one per frame."

## A debugging tip

If a system isn't running when you expect:

1. Check the stage. `Update` runs every frame; `FixedUpdate` may not run on a given frame at all if the accumulator hasn't reached the timestep.
2. Check `RunIf`. State-gated systems are easy to forget.
3. Check ordering. If you used `.After("foo")` and there's no system labeled `"foo"`, you'd have gotten a panic at `Run()`. If the panic didn't happen, the label exists — but maybe the labeled system has its own `RunIf` that's gating it out.

## Next

- [docs/events-and-states.md](events-and-states.md) — the event buffer's per-frame lifetime, state-machine transitions.
- [docs/plugins.md](plugins.md) — how plugins compose and what hooks they install.
