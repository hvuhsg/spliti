# Roadmap — gaps before building real games

An engineering TODO derived from an audit of the engine (2026-06). spliti has a
solid core — App/Plugin/Schedule, topo-sorted ordering, frame-buffered events,
typed states, a correct fixed-timestep loop with render interpolation, and a
clean render/present seam across terminal / 2D GPU / 3D GPU backends. The items
below are what's missing or weak before real games — especially GPU and
real-time/action games — become viable.

Severity: 🔴 blocking · 🟠 high · 🟡 moderate / genre-dependent

## 🔴 Blocking — most games need these on day one

- [ ] **Audio plugin** — no sound/music anywhere today. Add a plugin (e.g.
      `hajimehoshi/oto`) with play/loop/stop, volume, and a small mixer. Fits
      the existing plugin shape; self-contained.
- [ ] **Sprite animation** — no frame cycling, tweening, or state-driven
      animation. Add an `Animation` component + system; can lean on the existing
      `time.Time.Alpha()` for smooth interpolation.
- [ ] **GPU text rendering** — `webgpu` and `render3d` cannot draw text; only
      the terminal has glyphs. Bundle a font rasterizer
      (`golang.org/x/image/font` / freetype) feeding the existing
      overlay-texture path in `render3d`. Blocks every HUD/menu on GPU.

## 🔴 Blocking for action games

- [ ] **Held-key / input-action layer** — `plugin/input` is press-only
      (`KeyEvent` per press, no key-up, no held-state, no just-pressed).
      Add per-frame key-state tracking and an input→action mapping abstraction.
      Note: terminal can't deliver key-release, so held-state needs the GPU
      backends or a timeout shim.
- [ ] **Gamepad support** — none today. Wrap GLFW joystick polling in the GPU
      backends.

## 🟠 High

- [x] **Collision + spatial partitioning** — done: collision is now the reusable,
      cgo-free `plugin/collision` package with a uniform spatial-hash-grid
      broadphase (2D over `tui.Position`, 3D over a caller-supplied position) and
      collision layers/masks for filtering. `runtime`'s `Bounds`/`CollisionEvent`
      are aliases for it. Still missing (future work): physics integration
      (velocity/gravity/restitution), circles/rotated bodies, and continuous
      collision (fast objects still tunnel).
- [ ] **Asset pipeline** — string-keyed maps, synchronous loads, programmatic
      meshes only. Add glTF/OBJ loaders, async/streaming loads, caching/dedupe,
      texture atlases, and (optional) hot-reload. No recoverable error path on
      load failure today.
- [ ] **Runtime save/load** — scene *load* exists but *save* is editor-only
      (`editor/scene_load.go`); shipped games can't persist player data. Expose
      a save path usable at runtime.
- [ ] **UI toolkit** — no widgets, panels, layout, or menus. Build a minimal
      retained or immediate-mode layer on top of the render seam.

## 🟡 Moderate / genre-dependent

- [ ] **Entity-reference serialization** — the component registry can't
      serialize `ecs.Entity` fields generically (`Parent` is hand-coded).
- [ ] **>4-component query/spawn helpers** — `Query1..4` / `Spawn1..4` are
      hard-capped (`app/query.go`, `app/spawn.go`); `render3d` already had to
      drop to raw arche `NewMap5`. Add 5/6-arity helpers or a variadic path.
- [ ] **Entity hierarchy** — only a single `Parent` link in `render3d`; no
      children list, no scene-graph traversal, despawning a parent ignores
      children.
- [ ] **Frustum culling / LOD** — both GPU backends draw every renderable each
      frame; no visibility culling.
- [ ] **Particles** — none.
- [ ] **Netcode beyond lockstep** — lockstep TCP only; no rollback, reconnect,
      or UDP. Needed for real-time multiplayer.
- [ ] **Parallel scheduling** — engine is single-threaded (`app/app.go:143`);
      out of scope today but a ceiling at high entity counts.

## Suggested first three

1. Audio plugin
2. Sprite-animation component + system
3. GPU text rendering

These unblock the most games and are all small, self-contained additions that
fit the current plugin shape. After that: held-key input state, then a
grid/quadtree for collision.
