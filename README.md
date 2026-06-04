# spliti

A Bevy-shaped game engine for the terminal, written in Go.

`spliti` is an opinionated wrapper over [arche](https://github.com/mlange-42/arche) (ECS) and [tcell](https://github.com/gdamore/tcell) (terminal). The shapes — `App`, `Plugin`, `Schedule`, typed `Query`/`Resource`/`Event`/`State`, `Commands` — mirror Bevy. The substrate is single-threaded, archetype-stored, terminal-rendered, and small enough to read in one sitting.

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

That puts a green `@` on the screen and keeps the loop running until you `Ctrl-C`. Press the same keys you'd press in any terminal app — they flow through the input plugin as events.

## Why it exists

Most CLI-game tutorials stop at "draw a character and read a key." Real games need: a frame loop with a stable tick rate, deterministic ordering between systems, an event bus, state machines, scene management, plugins. Bevy figured out a clean shape for all of this; `spliti` ports the shape into Go for terminal-targeted games.

## Status

Working today:

- App / Plugin / Schedule with topo-sorted ordering, `.Before`/`.After`/`.Chain`, `.RunIf`.
- Components and queries via arche, with generic `Spawn1..4` / `Query1..4` helpers.
- Resources, events with frame-buffered lifetime, typed state machines.
- Time plugin with fixed-timestep accumulator and frame pacing.
- tcell-backed terminal, input, and rendering plugins. Single-flush-per-frame so HUD overlays don't flicker.
- **GPU rendering** off the terminal: drop in `webgpu.Plugin` to open a window and draw textured-sprite entities through the GPU (WebGPU via [cogentcore/webgpu](https://github.com/cogentcore/webgpu) + GLFW). The engine keeps owning the loop — it's a render/present/input plugin against the same seam as `tui`, with its own tcell-free `Transform`/`Color`/`Sprite` components. Requires `CGO_ENABLED=1`. See `examples/gpu-demo`.
- TCP **lockstep multiplayer** for 2..N players. Drop in `network.Plugin`, read `PlayerKey` events, stay deterministic. See [docs/network.md](docs/network.md).
- Examples: single-player Snake (`examples/snake`), networked two-player Snake (`examples/snake-net`), a networked stick-figure fighter (`examples/stick-fight`), a single-player stick-figure fighter against an AI (`examples/stick-fight-ai`), and five arcade classics — Pong (`examples/pong`), Tetris (`examples/tetris`), Breakout (`examples/breakout`), Space Invaders (`examples/invaders`), and Pac-Man (`examples/pacman`).

Out of scope today: parallel scheduling, NAT traversal, rollback netcode, hot reload. None of these are precluded — the engine is small enough to grow into them.

## Install

```bash
go get github.com/hvuhsg/spliti@latest
```

Requires Go 1.21+ for generics.

## Run the examples

```bash
# Single-player Snake
go run github.com/hvuhsg/spliti/examples/snake

# Two-player networked Snake (run each command in its own terminal)
go run github.com/hvuhsg/spliti/examples/snake-net -mode=host   -addr=:7777
go run github.com/hvuhsg/spliti/examples/snake-net -mode=client -addr=localhost:7777

# Two-player networked Stick-Fight (run each command in its own terminal)
go run github.com/hvuhsg/spliti/examples/stick-fight -mode=host   -addr=:7777
go run github.com/hvuhsg/spliti/examples/stick-fight -mode=client -addr=localhost:7777

# Single-player Stick-Fight against an AI opponent
go run github.com/hvuhsg/spliti/examples/stick-fight-ai

# Arcade classics
go run github.com/hvuhsg/spliti/examples/pong
go run github.com/hvuhsg/spliti/examples/tetris
go run github.com/hvuhsg/spliti/examples/breakout
go run github.com/hvuhsg/spliti/examples/invaders
go run github.com/hvuhsg/spliti/examples/pacman

# GPU window demo (textured sprites via WebGPU; needs cgo + a C toolchain)
CGO_ENABLED=1 go run github.com/hvuhsg/spliti/examples/gpu-demo
```

Snake controls: arrow keys or WASD, `q` to quit, `r` to restart on game over.

Stick-Fight controls: `a`/`d` move, `w` jump, `s` crouch, `j` attack, `k` defend, `r` restart, `q` quit. Strikes are height-aware — jump over a punch, duck a high kick, sweep low to hit a crouching opponent.

Pong controls: `w`/`s` left paddle, `↑`/`↓` (or `i`/`k`) right paddle. First to 7 wins.

Tetris controls: `←`/`→` shift, `↑` rotate, `↓` soft drop, `space` hard drop. Level (and gravity) climbs every 10 lines.

Breakout controls: `←`/`→` move paddle, `space` to launch the ball after each life. Three lives, brick value rises with row color.

Space Invaders controls: `←`/`→` move cannon, `space` to fire (one bullet on screen). Aliens speed up as they thin out and waves get harder.

Pac-Man controls: arrows or WASD to steer through the maze. Power pellets turn ghosts blue and edible for a few seconds; tunnels on the middle row wrap to the other side.

## Documentation

| Doc                                          | What it covers                                                              |
| -------------------------------------------- | --------------------------------------------------------------------------- |
| [Getting started](docs/getting-started.md)   | Five-minute walkthrough — your first entity, system, and query.             |
| [Architecture](docs/architecture.md)         | The big picture: App lifecycle, Plugin model, Schedule shape.               |
| [ECS](docs/ecs.md)                           | Components, resources, queries, spawning, deferred commands.                |
| [Scheduling](docs/scheduling.md)             | Stages, ordering edges, run conditions, fixed timestep.                     |
| [Events & States](docs/events-and-states.md) | Event lifetime, `ClearEvents`, state machines, `OnEnter`/`OnExit`.          |
| [Plugins](docs/plugins.md)                   | Writing your own plugin, the built-in plugin set, lifecycle hooks.          |
| [TUI & Input](docs/tui-and-input.md)         | Render/present split, overlays, the no-flicker invariant, raw input events. |
| [GPU rendering](docs/gpu.md)                  | The `webgpu` backend: textured sprites, textures, camera, GLFW input, cgo.  |
| [Network](docs/network.md)                   | Lockstep multiplayer, determinism contract, stall policies, `PlayerKey`.    |

## Project layout

```
spliti/
├── app/                       # core engine: App, Plugin, Schedule, Ctx, ECS helpers
├── schedule/                  # named stages (Startup, Update, FixedUpdate, …)
├── plugin/
│   ├── time/                  # Time resource, FixedUpdate accumulator, frame pacing
│   ├── terminal/              # tcell screen as a shared resource
│   ├── input/                 # raw key/resize/mouse events
│   ├── tui/                   # Position+Glyph render + overlay system
│   ├── webgpu/                # GPU window: textured-sprite render via WebGPU + GLFW (cgo)
│   ├── network/               # lockstep multiplayer over TCP
│   └── defaultplugins/        # bundle of time + terminal + input + tui
├── examples/
│   ├── snake/                 # single-player
│   ├── snake-net/             # two-player networked
│   ├── stick-fight/           # two-player networked fighter
│   ├── stick-fight-ai/        # single-player vs AI
│   ├── pong/                  # two-player Pong
│   ├── tetris/                # falling tetrominoes
│   ├── breakout/              # paddle, ball, brick wall
│   ├── invaders/              # marching aliens, four shields, one cannon
│   ├── pacman/                # maze, dots, four ghosts, power pellets
│   └── gpu-demo/              # bouncing textured quads in a GPU window (webgpu.Plugin)
└── docs/
```

## License

Not yet specified — treat as "all rights reserved" until I add a license file. Open an issue if you want to use it; I'm happy to dual-license MIT/Apache-2.0.
