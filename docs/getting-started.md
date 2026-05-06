# Getting started

This walks through enough of `spliti` to make a moving character on the terminal in five minutes. By the end you'll have used every core abstraction: components, systems, queries, resources, events, and the standard plugin set.

## Setup

```bash
mkdir hello-spliti && cd hello-spliti
go mod init example.com/hello-spliti
go get github.com/hvuhsg/spliti@latest
```

## Step 1 — A blinking dot

```go
package main

import (
    "github.com/gdamore/tcell/v2"
    "github.com/hvuhsg/spliti/app"
    "github.com/hvuhsg/spliti/plugin/defaultplugins"
    "github.com/hvuhsg/spliti/plugin/tui"
    "github.com/hvuhsg/spliti/schedule"
    "github.com/mlange-42/arche/generic"
)

func main() {
    a := app.New()
    a.AddPlugins(defaultplugins.Plugins{})

    a.AddSystems(schedule.Startup, func(c *app.Ctx) {
        m := generic.NewMap2[tui.Position, tui.Glyph](c.World())
        m.NewWith(
            &tui.Position{X: 10, Y: 5},
            &tui.Glyph{Char: '@', Style: tcell.StyleDefault.Foreground(tcell.ColorLightGreen)},
        )
    })

    a.Run()
}
```

`go run .` — you should see a green `@` at column 10, row 5. `Ctrl-C` to exit.

What just happened:

- `app.New()` builds an empty `App`.
- `defaultplugins.Plugins{}` registers the standard four: `time`, `terminal`, `input`, `tui`. After this call, you have a frame loop, a clock, a screen, raw input events, and an entity-based renderer.
- The `Startup` system runs once before the loop. It uses arche's typed mapper to create one entity with two components — `Position` and `Glyph` are defined by the `tui` plugin and are what its render system looks for.
- `a.Run()` enters the loop until `App.Stop()` is called or `Ctrl-C` reaches the input plugin (which surfaces it as an event — game code decides what to do with it).

## Step 2 — Move it with the arrow keys

Now we'll handle input. Add a system that reads the input plugin's events and updates the entity's position.

```go
import (
    // …
    "github.com/hvuhsg/spliti/plugin/input"
    "github.com/mlange-42/arche/ecs"
)

func handleInput(c *app.Ctx) {
    for _, ev := range app.ReadEvents[input.KeyEvent](c) {
        if ev.Key == tcell.KeyCtrlC || ev.Rune == 'q' {
            c.App().Stop()
            return
        }
        var dx, dy int
        switch ev.Key {
        case tcell.KeyUp:
            dy = -1
        case tcell.KeyDown:
            dy = 1
        case tcell.KeyLeft:
            dx = -1
        case tcell.KeyRight:
            dx = 1
        }
        if dx == 0 && dy == 0 {
            continue
        }
        app.Query1[tui.Position](c, func(_ ecs.Entity, p *tui.Position) {
            p.X += dx
            p.Y += dy
        })
    }
}

func main() {
    // …
    a.AddSystems(schedule.Update, handleInput)
    a.Run()
}
```

`Update` runs every frame. `app.ReadEvents[T](ctx)` returns a snapshot of events of type `T` produced this frame. `app.Query1[Position]` iterates every entity that has a `Position` component and gives us a pointer to mutate.

## Step 3 — Pick a target frame rate and a fixed step

`defaultplugins.Plugins` accepts a configurable `time` plugin. If you want a slower or faster game you change one number:

```go
import (
    gotime "time"
    splititime "github.com/hvuhsg/spliti/plugin/time"
)

a.AddPlugins(defaultplugins.Plugins{
    Time: splititime.Plugin{
        FixedTimestep:   100 * gotime.Millisecond, // 10 Hz simulation tick
        TargetFrameRate: 60,                        // 60 fps render
    },
})
```

`FixedUpdate` systems run zero or more times per frame to keep up with the configured fixed step, regardless of frame rate. That's where simulation logic that needs a stable cadence (movement, physics, AI) belongs. See [docs/scheduling.md](scheduling.md).

## Where to go next

- [docs/architecture.md](architecture.md) explains how `App`, plugins, and the schedule fit together.
- [docs/ecs.md](ecs.md) covers components, resources, queries, and the deferred commands buffer.
- [docs/events-and-states.md](events-and-states.md) covers events and state machines (`Playing` → `GameOver` → `Playing` patterns).
- The `examples/snake` source is the next-step exercise — every concept above is in there.
- For multiplayer, [docs/network.md](network.md) ties it all together.
