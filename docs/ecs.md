# ECS

`spliti` exposes a Bevy-shaped ECS API on top of [arche](https://github.com/mlange-42/arche). The terms map cleanly:

| Concept | Type | What it is |
|---|---|---|
| Entity | `ecs.Entity` (from arche) | Opaque ID. No data of its own. |
| Component | any Go struct | Data attached to an entity. No interface to implement. |
| System | `func(*app.Ctx)` | A function that reads/writes the world. |
| Resource | any Go struct | Process-wide singleton, keyed by type. |
| Query | `app.Query1..4[A,B,…]` | Iteration over entities with a given set of components. |
| Commands | `c.Commands()` | Deferred mutation queue (drained at end of each stage). |

## Components

Just structs. No tags, no interfaces, no registration step.

```go
type Position struct{ X, Y int }
type Velocity struct{ X, Y int }
type Player struct{}        // a marker; zero size is fine
```

arche registers component types lazily on first use, so you don't manage a registry yourself.

## Spawning entities

Two paths, picked based on whether you need the entity ID *now*.

### Deferred (recommended in systems)

```go
app.Spawn2[Position, Velocity](c.Commands(), func(p *Position, v *Velocity) {
    *p = Position{X: 0, Y: 0}
    *v = Velocity{X: 1, Y: 0}
})
```

The closure runs at the end of the current stage, when the deferred commands buffer is applied. This avoids the "spawning while iterating" foot-gun and pairs naturally with the `Despawn`/`Insert`/`Remove` operations that also live on `Commands`.

`Spawn1`, `Spawn2`, `Spawn3`, `Spawn4` cover up to four components. Beyond that, drop down to `c.Commands().Add(func(w *ecs.World) { ... })` and use arche directly inside.

### Immediate (fine in Startup or one-shot setup)

```go
m := generic.NewMap2[Position, Velocity](c.World())
e := m.NewWith(&Position{X: 0, Y: 0}, &Velocity{X: 1, Y: 0})
// e is a valid ecs.Entity right now
```

This runs synchronously against the world. Use it when you need the entity ID immediately (to store in a resource, for example) and you're not iterating a query at the same time.

## Querying

Generic helpers iterate matching entities:

```go
app.Query2[Position, Velocity](c, func(e ecs.Entity, p *Position, v *Velocity) {
    p.X += v.X
    p.Y += v.Y
})
```

The pointers are live — mutations land directly in the world. The entity ID is yielded for use with `Commands` (e.g. `c.Commands().Despawn(e)`).

### What if I need filters?

The wrapper covers the common case (all entities with components A, B, C). For "with X but without Y" or optional components, drop to arche's filter builder:

```go
filter := generic.NewFilter2[Position, Velocity]().
    With(generic.T[Player]()).
    Without(generic.T[Frozen]())
q := filter.Query(c.World())
for q.Next() {
    p, v := q.Get()
    // …
}
```

## Despawning + component changes

```go
c.Commands().Despawn(e)                   // queued; applied end of stage
```

For removing a single component or adding one, use arche's mapper directly inside a `Commands().Add(...)` block:

```go
c.Commands().Add(func(w *ecs.World) {
    m := generic.NewMap1[Frozen](w)
    m.Add(e)             // attach Frozen
    // or m.Remove(e)
})
```

A typed `Insert` / `Remove` helper on `Commands` would be a nice future addition; the lower-level path is here today.

## Resources

Resources are typed singletons. Use them for state that doesn't belong to any one entity: scores, configuration, the local player ID.

```go
type Score struct{ Value int }

app.InsertResource(a, &Score{})            // at Build / setup
score := app.GetResource[Score](c)         // in a system
score.Value++
```

`GetResource[T]` returns `nil` if nothing was inserted. `HasResource[T](app)` is the no-side-effect probe. Re-`InsertResource` overwrites — there's no separate "remove" step.

The keying is `reflect.Type`. Two distinct types (e.g. `time.Time` from the time plugin and a user-defined `Time`) coexist without collision.

## Commands buffer details

Every system call gets a `*Ctx`. `c.Commands()` is the same queue across the whole stage; mutations recorded by system A are visible to system B's queries only after the stage completes. That's the simplest mental model: "everything I do via `Commands` is visible after this stage's last system finishes."

Direct world mutations through `c.World()` and arche's mapper are immediate and observable to subsequent queries within the same stage. Use direct mutation when ordering matters mid-stage; use commands when it doesn't.

## Change detection

For systems that should react only to entities whose component changed —
spawned, gained the component, or written through this frame — `spliti` ships
a per-type change buffer.

Enable tracking once at `Build` time, then query the buffer from any system:

```go
type Health struct{ Value int }

func (Plugin) Build(a *app.App) {
    app.TrackChanges[Health](a)         // register the listener
    a.AddSystems(schedule.Update, onHealthChanged)
}

func onHealthChanged(c *app.Ctx) {
    app.QueryChanged1[Health](c, func(e ecs.Entity, h *Health) {
        // runs only for entities whose Health was added or marked changed
        // since the last frame end.
    })
}
```

Two kinds of change are detected:

| Source | How |
|---|---|
| Component **added** to an entity (spawn, late `Add`) | Auto-detected via arche's `Listener` on the world. No code needed. |
| Component **mutated** through a `*T` pointer | Go can't intercept pointer writes. The writer must call `app.MarkChanged[T](c, e)` after writing. |

Buffers are drained at the end of each frame, after `Last`, alongside the
event drain. So an entity that was added in frame N is in the changed set
for systems running later in frame N, and gone by frame N+1.

Re-`TrackChanges`-ing the same type is a no-op. Calling `QueryChanged1[T]`
without first calling `TrackChanges[T]` panics with a directive to register.

### Pattern: invalidate a derived index when an entity moves

```go
app.TrackChanges[Position](a)

a.AddSystems(schedule.Update, app.System(func(c *app.Ctx) {
    // Move things.
    app.Query2[Position, Velocity](c, func(e ecs.Entity, p *Position, v *Velocity) {
        p.X += v.X; p.Y += v.Y
        app.MarkChanged[Position](c, e)   // tell observers we wrote
    })
}).Label("move"))

a.AddSystems(schedule.Update, app.System(func(c *app.Ctx) {
    grid := app.GetResource[SpatialGrid](c)
    app.QueryChanged1[Position](c, func(e ecs.Entity, p *Position) {
        grid.Reindex(e, p.X, p.Y)         // O(changed) instead of O(all)
    })
}).After("move"))
```

## What's deliberately not in this layer

- **Relations.** arche supports parent/child relations; we don't expose a typed wrapper yet.
- **Reflection-based system parameters.** Bevy systems take typed params (`Query<…>`, `Res<T>`, `Commands`). Go's reflection couldn't do this cleanly without an unsafe perf cost. We use the explicit `GetResource[T](ctx)` / `Query1[T](ctx, fn)` form instead. Less magic, no reflection in the hot path.

## Next

- [docs/scheduling.md](scheduling.md) — when systems run, in what order.
- [docs/events-and-states.md](events-and-states.md) — passing data between systems via events.
