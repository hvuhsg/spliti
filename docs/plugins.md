# Plugins

A plugin is a value with a single method:

```go
type Plugin interface {
    Build(app *App)
}
```

`Build` runs once during `app.AddPlugins(...)`. It's where the plugin registers everything it owns: resources, systems, hooks, cleanup.

There's no plugin lifecycle to manage — `Build` is the only callback. Anything dynamic (loops, goroutines, sockets) is started by the systems and OnExit hooks the plugin installs, not by `Build` itself.

## Anatomy of a plugin

A minimal sketch:

```go
package score

import (
    "github.com/hvuhsg/spliti/app"
    "github.com/hvuhsg/spliti/schedule"
)

type Score struct{ Value int }

type Plugin struct {
    Initial int
}

func (p Plugin) Build(a *app.App) {
    app.InsertResource(a, &Score{Value: p.Initial})

    a.AddSystems(schedule.PostUpdate, app.System(func(c *app.Ctx) {
        // example: clamp score to non-negative
        s := app.GetResource[Score](c)
        if s.Value < 0 {
            s.Value = 0
        }
    }).Label("__score_clamp"))
}
```

Use it:

```go
a.AddPlugins(score.Plugin{Initial: 0})
```

That's the whole pattern. The plugin's exported types (`Score`) become public API; everything internal stays in the package.

## Hooks the App exposes to plugins

| Hook | When it fires | Typical use |
|---|---|---|
| `app.AddSystems(stage, ...)` | Per `Run()` invocation | The bread and butter — register your systems. |
| `app.InsertResource[T](&v)` | Build time | Create a resource the plugin owns. |
| `app.AddOnExit(fn)` | After `Run()` exits, including on panic. LIFO order. | Release sockets, close files, restore the terminal. |
| `app.SetPreUpdateHook(fn)` | After `StateTransition`, before `Update`, every frame. **Single slot.** | Currently used by the time plugin to drive `FixedUpdate`. Avoid in user plugins until we make this a slice. |
| `app.SetPostUpdateHook(fn)` | After `Last`, every frame. | Frame pacing (time plugin uses this). |
| `app.RunStage(stage)` | Anywhere | Manually run a stage's systems. The time plugin does this inside its pre-update hook. |

## Internal labels: keep them private, expose extension points

When a plugin's system needs to coordinate with user code (e.g. "draw on top of the rendered scene"), don't leak the engine-internal label name. Expose a function instead.

The `tui` plugin demonstrates the pattern:

```go
const renderLabel = "__spliti_render"     // unexported

func (Plugin) Build(a *app.App) {
    a.AddSystems(schedule.PostUpdate, app.System(renderFn).Label(renderLabel))
    a.AddSystems(schedule.PostUpdate, app.System(presentFn).Label(presentLabel).After(renderLabel))
}

// Public extension point — users call this; never reference renderLabel.
func AddOverlay(a *app.App, sys app.SystemFunc) {
    a.AddSystems(schedule.PostUpdate,
        app.System(sys).After(renderLabel).Before(presentLabel))
}
```

Result: user code reads `tui.AddOverlay(a, drawHUD)` instead of `app.AddSystems(schedule.PostUpdate, app.System(drawHUD).After("__spliti_render"))`. The label is implementation detail.

## Plugin order matters

Plugins run their `Build` in the order you call `AddPlugins`. So:

- A plugin that **depends on** another plugin's resource must be added *after* it.
- A plugin that **wants to wrap** another plugin's behavior should be added *after* it (so its `.After(otherLabel)` edges resolve).

The default plugin set composes in this order:

```go
defaultplugins.Plugins{}.Build(a)
  → time.Plugin{}.Build(a)        // installs Time, takes the pre-update hook
  → terminal.Plugin{}.Build(a)    // initializes tcell.Screen, registers OnExit cleanup
  → input.Plugin{}.Build(a)       // depends on terminal.Terminal — must come after
  → tui.Plugin{}.Build(a)         // depends on terminal.Terminal — must come after
```

`network.Plugin` depends on the input plugin (it captures `input.KeyEvent`). Add it after `defaultplugins.Plugins{}`.

## Cleaning up

Always pair side-effecting setup with `AddOnExit`. Examples in the standard plugins:

- `terminal.Plugin` calls `screen.Fini()` to restore the user's terminal.
- `input.Plugin` closes its event channel and posts a synthetic interrupt to wake its goroutine so it exits cleanly.
- `network.Plugin` sends `kBye` to each peer and closes the sockets.

OnExit hooks run **in reverse registration order** (LIFO), the same as `defer`. They run on a normal loop exit *and* if a panic propagates out of `Run()`. This is why a panic in user code never leaves your terminal in raw mode.

## The standard plugins, briefly

### `time` — `plugin/time`

```go
splititime.Plugin{
    FixedTimestep:   100 * time.Millisecond,
    TargetFrameRate: 60,
}
```

Installs `*Time` resource (`Delta()`, `Elapsed()`, `FixedDelta()`), a tick system in `First`, the `FixedUpdate` accumulator hook, and optional frame pacing.

### `terminal` — `plugin/terminal`

```go
terminal.Plugin{EnableMouse: false}
```

Initializes the tcell screen, installs `*Terminal` resource (`.Screen`), and registers cleanup. It's a low-level plugin; most game code touches it through `tui.Screen(c)` rather than directly.

### `input` — `plugin/input`

```go
input.Plugin{}
```

Spawns a goroutine that polls tcell events. In `First` stage, drains them into `Events[KeyEvent]`, `Events[ResizeEvent]`, `Events[MouseEvent]`. Game code reads these via `app.ReadEvents[input.KeyEvent](c)`.

### `tui` — `plugin/tui`

```go
tui.Plugin{ClearStyle: tcell.StyleDefault}
```

Defines `Position` and `Glyph` components. In `PostUpdate`: clears the back buffer, draws every (Position, Glyph) entity, and presents (a single `Show()` call). See [docs/tui-and-input.md](tui-and-input.md) for the render/present split.

### `defaultplugins` — `plugin/defaultplugins`

```go
defaultplugins.Plugins{
    Time:        splititime.Plugin{...},
    EnableMouse: false,
}
```

Bundle of `time + terminal + input + tui` in the right order. Use this for anything terminal-rendered.

### `network` — `plugin/network`

Opt-in. See [docs/network.md](docs/network.md).

## Anti-patterns

- **Don't do work in `Build`.** The terminal isn't yet open in `Build` (well — it is, since `terminal.Plugin.Build` runs first, but that's fragile). Push everything to a `PreStartup` system.
- **Don't share `Build`-time variables across plugins.** If two plugins need to coordinate, use a resource (typed, looked up by `GetResource`) — not closure capture.
- **Don't forget `AddOnExit` for goroutines.** The Go runtime cleans up the process, but a leaked goroutine that holds a tcell handle leaves the terminal in an unusable state.
- **Don't take `SetPreUpdateHook`** unless you know the time plugin isn't loaded. The hook is a single slot today.

## Next

- [docs/tui-and-input.md](tui-and-input.md) — the rendering pipeline in detail.
- [docs/network.md](network.md) — building a multiplayer game with `network.Plugin`.
