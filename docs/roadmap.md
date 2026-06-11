# Roadmap — gaps before building real games

An engineering TODO derived from an audit of the engine (2026-06, statuses
refreshed 2026-06-12). spliti has a solid core — App/Plugin/Schedule,
topo-sorted ordering, frame-buffered events, typed states, a correct
fixed-timestep loop with render interpolation, and a clean render/present seam
across terminal / 2D GPU / 3D GPU backends. The items below are what's missing
or weak before real games — especially GPU and real-time/action games — become
viable.

Severity: 🔴 blocking · 🟠 high · 🟡 moderate / genre-dependent

## 🔴 Blocking — most games need these on day one

- [x] **Audio plugin** — done: `plugin/audio` is a software mixer (per-voice
      volume/pan/pitch, looping, generation-safe handles, master/sfx/music
      buses, click-free ramps) over one oto/v3 device, native + browser. WAV/
      OGG/MP3 decoding, streamed music with fade/crossfade, 2D/3D spatial
      audio (`audio/spatial3d` ties voices to render3d), silent null-sink
      fallback for headless runs. See [docs/audio.md](audio.md); Snake plays
      music + SFX. Still missing (future work): per-voice DSP effects
      (reverb/filters), and higher-quality resampling than linear.
- [ ] **Sprite animation** — `plugin/sprite` stores static sprite-sheet assets
      (grid cells, anchors) but has no frame cycling, tweening, or state-driven
      animation. Add an `Animation` component + system; can lean on the
      existing `time.Time.Alpha()` for smooth interpolation.
- [x] **GPU text rendering** — done: `plugin/text` rasterizes strings with the
      embedded Go fonts (or any TTF/OTF via `NewFace`) on every build target,
      and `render3d.LoadTextPanel` uploads the result straight into the
      existing Overlay2D panel path — static/occasional HUD text is one call.
      Fully dynamic per-frame UI text is covered by `plugin/ui` (Dear ImGui on
      the GPU, with its own font atlas). The 2D `webgpu` backend has no panel
      path yet; `text` images can still be uploaded as sprites there.

## 🔴 Blocking for action games

- [x] **Held-key / input-action layer** — done in two parts. Held state: both
      GPU backends track key and mouse-button state from their event streams
      (`render3d.KeyDown` / `MouseButtonDown`, same on `webgpu`), portable
      across native and browser. Action mapping: `plugin/inputs/actions` binds
      named actions to keys, mouse buttons, and gamepad buttons/axes —
      `Held` / `JustPressed` / `JustReleased` / `Axis(-1..1)`, rebindable at
      runtime, updated once per frame in PreUpdate. The terminal backend still
      can't deliver key-release (platform limitation), so held-state semantics
      need a GPU backend.
- [x] **Gamepad support** — done: `plugin/gamepad` polls controllers on native
      (GLFW gamepad mappings) and in the browser (Gamepad API, "standard"
      mapping), exposing the neutral `inputs.Gamepads` resource plus
      connection/button-edge events, with triggers canonicalized to [0,1] on
      both platforms. Integrates with `plugin/inputs/actions` via
      `actions.Pad` / `actions.PadAxis`.

## 🟠 High

- [x] **Collision + spatial partitioning** — done: collision is now the reusable,
      cgo-free `plugin/collision` package with a uniform spatial-hash-grid
      broadphase (2D over `tui.Position`, 3D over a caller-supplied position) and
      collision layers/masks for filtering. `runtime`'s `Bounds`/`CollisionEvent`
      are aliases for it. Still missing (future work): physics integration
      (velocity/gravity/restitution), circles/rotated bodies, and continuous
      collision (fast objects still tunnel).
- [x] **UI toolkit** — done for development needs: `plugin/ui` is Dear ImGui
      (cimgui-go) over a custom wgpu backend on top of render3d — windows,
      widgets, layout, keyboard/mouse capture, HiDPI scale. Immediate-mode
      only; a styled retained-mode menu system for shipped games remains
      future work.
- [ ] **Asset pipeline** — partial: `render3d.LoadGLTF`/`LoadGLTFFS` load
      glTF/GLB (meshes, PBR materials with base-color/metallic-roughness/normal
      textures, node hierarchy via `SpawnModel`, synthesized normals/tangents)
      with real error propagation. Still missing: async/streaming loads, OBJ,
      texture atlases, hot-reload, and glTF animation/skinning (see below).
- [ ] **Runtime save/load** — scene *load* exists but *save* is editor-only
      (`editor/scene_load.go`); shipped games can't persist player data. Expose
      a save path usable at runtime.

## 🟡 Moderate / genre-dependent

- [ ] **glTF animation / skinning** — the loader ignores glTF animation
      channels and skins; characters can only be moved per-node from game code.
      A big 3D game wants keyframe playback and skeletal skinning.
- [ ] **Entity-reference serialization** — the component registry can't
      serialize `ecs.Entity` fields generically (`Parent` is hand-coded).
- [x] **>4-component query/spawn helpers** — done: `Query5`/`Query6` and
      `Spawn5`/`Spawn6` (`app/query.go`, `app/spawn.go`). Raw arche maps remain
      the escape hatch beyond six.
- [ ] **Entity hierarchy** — improved: `render3d.Children` (inverse lookup, O(n)
      scan) and `render3d.DespawnRecursive` (subtree teardown, cycle-safe) now
      exist alongside the `Parent` link. Still missing: an indexed children
      list / scene-graph resource for cheap per-frame traversal of deep
      hierarchies.
- [x] **Frustum culling** — done: per-entity bounding-sphere culling against
      the camera frustum is active in render3d's draw loop. **LOD** remains
      future work (no distance-based mesh swap).
- [ ] **Particles** — none.
- [ ] **Shadows / post-processing** — render3d has no shadow mapping and no
      HDR/post pipeline; the architecture leaves seams for both.
- [ ] **Netcode beyond lockstep** — lockstep TCP only; no rollback, reconnect,
      or UDP. Needed for real-time multiplayer.
- [ ] **Parallel scheduling** — engine is single-threaded; out of scope today
      but a ceiling at high entity counts.

## Suggested next three (for a 3D game)

1. Runtime save/load — shipped games need to persist player data
2. glTF animation/skinning — animated characters are table stakes for a big 3D game
3. Asset pipeline: async loads + load-failure recovery, so big scenes stream
   without hitching

(For 2D games, sprite animation remains the top gap.)
