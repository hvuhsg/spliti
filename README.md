# spliti

A Bevy-shaped game engine in Go — renders to the terminal **or** the GPU.

`spliti` is an opinionated wrapper over [arche](https://github.com/mlange-42/arche) (ECS). The shapes — `App`, `Plugin`, `Schedule`, typed `Query`/`Resource`/`Event`/`State`, `Commands` — mirror Bevy. The substrate is single-threaded, archetype-stored, and small enough to read in one sitting.

Rendering is just a plugin against a common seam, so the same game loop and ECS drive any of three backends:

- **Terminal** — [tcell](https://github.com/gdamore/tcell)-backed glyphs, ASCII sprites, and a truecolor pixel canvas.
- **2D GPU** — a windowed, textured-sprite renderer (WebGPU + GLFW).
- **3D GPU** — a real-time renderer with a perspective camera, depth buffer, indexed meshes, and PBR metallic-roughness lighting.

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

Most CLI-game tutorials stop at "draw a character and read a key." Real games need: a frame loop with a stable tick rate, deterministic ordering between systems, an event bus, state machines, scene management, plugins. Bevy figured out a clean shape for all of this; `spliti` ports the shape into Go. It started terminal-only, but because rendering is a plugin behind a render/present seam, the same engine now also drives 2D and 3D GPU windows — pick the backend, keep the game.

## Status

Working today:

- App / Plugin / Schedule with topo-sorted ordering, `.Before`/`.After`/`.Chain`, `.RunIf`.
- Components and queries via arche, with generic `Spawn1..4` / `Query1..4` helpers.
- Resources, events with frame-buffered lifetime, typed state machines.
- Time plugin with fixed-timestep accumulator and frame pacing.
- **Terminal rendering**: tcell-backed glyph render + input, with a single flush per frame so HUD overlays don't flicker. Plus ASCII-art `sprite` rendering and a truecolor half-block pixel `canvas` for image-like output in the terminal.
- **2D GPU rendering**: drop in `webgpu.Plugin` to open a window and draw textured-sprite entities through the GPU (WebGPU via [cogentcore/webgpu](https://github.com/cogentcore/webgpu) + GLFW). It's a render/present/input plugin against the same seam as `tui`, with its own tcell-free `Transform`/`Color`/`Sprite` components. Requires `CGO_ENABLED=1`. See `examples/gpu-demo`.
- **3D GPU rendering**: drop in `render3d.Plugin` for a real-time 3D renderer — perspective camera, depth buffer, indexed triangle meshes with vertex normals, PBR metallic-roughness shading with directional and point lights, GPU instancing, a transparent pass, line gizmos, picking, and a 2D overlay. Entities render via `Transform3D` + `MeshRenderer`. Requires `CGO_ENABLED=1`. See `examples/render3d-demo` and `examples/radio3d`.
- The engine keeps owning the loop in every case — no render backend takes over `app.Run()`.
- **Visual editor**: a tcell-based editor (`editor/`) for authoring scenes from data — entities, components, and behaviors from the `runtime` plugin's built-in vocabulary, saved/loaded as project files. See `examples/editor-demo`.
- TCP **lockstep multiplayer** for 2..N players. Drop in `network.Plugin`, read `PlayerKey` events, stay deterministic. See [docs/network.md](docs/network.md).
- Examples: single-player Snake (`examples/snake`), networked two-player Snake (`examples/snake-net`), a networked stick-figure fighter (`examples/stick-fight`), a single-player fighter vs AI (`examples/stick-fight-ai`), an auto-shooter (`examples/survivors`), a first-person raycaster (`examples/doom`), and five arcade classics — Pong, Tetris, Breakout, Space Invaders, and Pac-Man. GPU showcases: the 2D `examples/gpu-demo`, the 3D `examples/render3d-demo`, an interactive radio-wave teaching game (`examples/radio`), and a 3D radio-propagation visualizer (`examples/radio3d`).

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

# Auto-shooter and a first-person raycaster (truecolor terminal recommended)
go run github.com/hvuhsg/spliti/examples/survivors
go run github.com/hvuhsg/spliti/examples/doom

# Arcade classics
go run github.com/hvuhsg/spliti/examples/pong
go run github.com/hvuhsg/spliti/examples/tetris
go run github.com/hvuhsg/spliti/examples/breakout
go run github.com/hvuhsg/spliti/examples/invaders
go run github.com/hvuhsg/spliti/examples/pacman

# GPU windows (need cgo + a C toolchain)
CGO_ENABLED=1 go run github.com/hvuhsg/spliti/examples/gpu-demo        # 2D textured sprites
CGO_ENABLED=1 go run github.com/hvuhsg/spliti/examples/render3d-demo   # 3D PBR scene
CGO_ENABLED=1 go run github.com/hvuhsg/spliti/examples/radio          # 2D radio teaching game
CGO_ENABLED=1 go run github.com/hvuhsg/spliti/examples/radio3d        # 3D radio-wave propagation
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
| [2D GPU rendering](docs/gpu.md)              | The `webgpu` backend: textured sprites, textures, camera, GLFW input, cgo.  |
| [Network](docs/network.md)                   | Lockstep multiplayer, determinism contract, stall policies, `PlayerKey`.    |

The 3D backend's design and component surface are documented in the `plugin/render3d` package comments.

## Project layout

```
spliti/
├── app/                       # core engine: App, Plugin, Schedule, Ctx, ECS helpers
├── schedule/                  # named stages (Startup, Update, FixedUpdate, …)
├── editor/                    # tcell-based visual scene/entity editor
├── plugin/
│   ├── time/                  # Time resource, FixedUpdate accumulator, frame pacing
│   ├── terminal/              # tcell screen as a shared resource
│   ├── input/                 # raw key/resize/mouse events
│   ├── tui/                   # Position+Glyph render + overlay system
│   ├── sprite/                # multi-cell ASCII-art sprite rendering
│   ├── canvas/                # truecolor half-block RGB pixel framebuffer
│   ├── webgpu/                # 2D GPU window: textured-sprite render via WebGPU + GLFW (cgo)
│   ├── render3d/              # 3D GPU window: PBR meshes, camera, lights, instancing (cgo)
│   ├── runtime/               # data-driven component/system vocabulary for the editor
│   ├── network/               # lockstep multiplayer over TCP
│   └── defaultplugins/        # bundle of time + terminal + input + tui
├── examples/                  # snake, stick-fight, survivors, doom, arcade classics,
│                              #   gpu-demo, render3d-demo, radio, radio3d, editor-demo
└── docs/
```

## License

Not yet specified — treat as "all rights reserved" until I add a license file. Open an issue if you want to use it; I'm happy to dual-license MIT/Apache-2.0.
