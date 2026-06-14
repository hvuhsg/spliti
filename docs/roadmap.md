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
- [x] **Sprite animation** — done: `plugin/sprite` now has a `SpriteAnimation`
      component (named `Clip`s of frame Refs + per-frame durations, loop/one-shot)
      and an Update-stage system that cycles `Sprite.Ref` on wall-clock
      `Time.Delta()`. Because it rewrites the existing `Sprite.Ref`, every backend
      animates with no renderer change; `Play(name)` is idempotent for
      state-driven switching. `Time.Alpha()` is deliberately not used — frames are
      discrete, so there is nothing to interpolate between them.
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
      collision layers/masks for filtering. Still missing (future work): physics integration
      (velocity/gravity/restitution), circles/rotated bodies, and continuous
      collision (fast objects still tunnel).
- [x] **UI toolkit** — done for development needs: `plugin/ui` is Dear ImGui
      (cimgui-go) over a custom wgpu backend on top of render3d — windows,
      widgets, layout, keyboard/mouse capture, HiDPI scale. Immediate-mode
      only; a styled retained-mode menu system for shipped games remains
      future work.
- [ ] **Asset pipeline** — mostly done: `render3d.LoadGLTF`/`LoadGLTFFS` load
      glTF/GLB (meshes, PBR materials with base-color/metallic-roughness/normal
      textures, node hierarchy via `SpawnModel`, synthesized normals/tangents) and
      `LoadOBJ` loads Wavefront meshes, all with real error propagation. The
      `AssetLoader` resource now adds **async loads** (`LoadMeshAsync` /
      `LoadModelAsync` decode off-thread, upload on the main thread via a
      `schedule.First` drain) with **load-failure recovery** (failures land on a
      queryable `AssetHandle`, never a panic; unresolved refs render nothing /
      default material so spawn-now-resolve-later is safe), plus opt-in **hot-reload**
      (`Plugin.HotReload`, native-only fsnotify, no-op on wasm). Still missing:
      texture atlases (glTF skinning, the other half of the item below, is now
      done).
- [ ] **Runtime save/load** — scenes live in Go source (the editor's
      code-as-truth format), so static content needs no loader; shipped games
      still can't persist *player data*. Expose a save path usable at runtime.

## 🟡 Moderate / genre-dependent

- [x] **glTF animation / skinning** — done: the loader parses animation channels
      (translation/rotation/scale; STEP/LINEAR, CUBICSPLINE reduced to its keyframe
      values) and skins (joints + inverse-bind matrices), and `SpawnModel` attaches
      an `Animator` that samples the active clip each frame and writes the node
      `Transform3D`s, so keyframe playback flows through the existing transform-
      propagation path. **Skeletal skinning is implemented end-to-end**: a skinned
      vertex layout (JOINTS_0/WEIGHTS_0), a dedicated skinned pipeline + `pbr_skin.wgsl`
      (linear blend skinning) reusing `fs_main`, and a per-frame joint-matrix storage
      buffer (`computeJointMatrices`, dynamic-offset bound per skinned mesh). Verified
      on the GPU with the Khronos SimpleSkin model — see `examples/render3d-skin`.
      Future work: morph targets and CUBICSPLINE Hermite tangents.
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

## Scalability — large-scene / large-game ceilings (2026-06 review)

From a "what would not scale or is incorrect for a big game engine" pass over
the engine + editor. The cheap, behavior-preserving wins were landed in the
`perf/scalability-fixes` branch (marked **DONE** below, with the full test
suite green after each); the rest are architectural rewrites left as scoped
follow-ups — each is large enough to need its own design, and several need a
product decision (most of all the netcode model).

### DONE — allocation / O(n²) fixes (perf/scalability-fixes)

- [x] **ECS query-filter caching** — `Query1..6` rebuilt and recompiled their
      arche filter every call (per-call alloc + reflect-keyed component-ID
      lookups + mask compile). Filters are now cached per-`Ctx` keyed by their
      component-type set. The cache is **per-`Ctx` on purpose**: arche assigns
      component IDs per-world, so a global type-keyed cache would mis-resolve
      across the multiple worlds tests / editor play-mode create. The
      `QueryChanged` generic `Map` is cached under the same invariant.
- [x] **Collision broadphase reuse** — `Grid2`/`Grid3` rebuilt the whole
      `map`-backed grid + scratch slices every `FixedUpdate`. They now `reset`
      and recycle cell slices from a pool, and the systems keep a persistent
      broadphase + body list across ticks. Tested `pairs2/3` / `NewGrid*` API
      preserved; brute-force-match fuzz tests confirm identical output.
- [x] **Render per-frame allocations** — `drawMeshes` allocated three component
      maps per pass per frame (now cached on `drawState`); transform
      propagation allocated a memo map + slice per frame (now reusable scratch).
- [x] **Editor O(n²) name resolution** — `entityByInstance` is an O(n) world
      scan called per-spawn inside reload / undo-restore (→ O(n²) per scene).
      Added a `nameIndex` built once per batch (kept live as `spawnLive` adds
      entities so parent links still resolve); `freeInstanceName` / `depthUnder`
      no longer rescan / reallocate per candidate. The widely-used
      `entityByInstance` signature was kept; a *persistent* always-on index is
      deferred (it needs invalidation across every structural path, a
      stale-entity correctness risk).
- [x] **Texture mipmaps** — see also webgpu-production-gaps #2; the render3d
      path now generates a CPU mip chain (linear-space averaging for sRGB maps)
      and raises the sampler LOD clamp.

### Deferred — architectural rewrites

- [ ] 🟠 **Parallel ECS scheduler** — see "Parallel scheduling" above. Needs
      per-system read/write-set analysis and parallel dispatch in the run loop;
      the current `before`/`after` only express a total order. Hard ceiling on
      many-core machines at high system/entity counts.
- [ ] 🟡 **Per-entity transform dirty-flagging** — `propagateTransforms`
      recomputes every world matrix every frame. Skipping unchanged transforms
      safely needs a real dirty/hierarchy subsystem: engine change-tracking is
      opt-in via manual `MarkChanged`, which gameplay movers don't call, so a
      naive skip would freeze moved entities. Pairs with the "indexed children
      list / scene-graph resource" item above.
- [ ] 🔴 **Netcode rewrite** — see "Netcode beyond lockstep" above. The current
      stack is lockstep (slowest peer gates everyone) + full-mesh + TCP
      (head-of-line blocking) + unauthenticated `encoding/gob` over a plain
      socket (a `gob.Decode` allocation-DoS, no length cap, no auth/encryption).
      Past LAN / 2 players this is unplayable and unsafe. Needs a designed
      target model (rollback vs. snapshot-interpolation, P2P vs. authoritative
      server) before code — and the gob frame-size cap + auth belong here, not
      as an isolated patch (they change the wire framing the handshake shares).
- [ ] 🟡 **Time / NetClock coupling** — the fixed-step count is wall-clock
      driven (`plugin/time`) while the network gate assumes one tick per
      `FixedFirst` pass. Under a frame hitch peers can run a different number of
      fixed steps → silent desync. Determinism needs the step count driven by
      the synchronized tick. Belongs with the netcode rewrite.
- [ ] 🟠 **Audio real-time locking** — the mix runs under the same coarse mutex
      as every gameplay control call (`Play`/`SetVolume`/…), so a control call
      can block the audio callback (priority inversion / glitch risk). Wants a
      lock-free command queue from the game thread to the mixer.
- [ ] 🟡 **Renderer scale** — covered in detail in
      [performance.md](performance.md) (transparent-object batching, material
      bind-group caching, draw-call reduction) and
      [webgpu-production-gaps.md](webgpu-production-gaps.md) (#5/#7/#8). Open
      large items: no spatial acceleration (linear frustum cull + brute-force
      raycast picking), and forward shading with an unbounded per-fragment light
      loop and a hard 256-light cap (no clustered / forward+ binning).
- [ ] 🟡 **Editor at scale** — the code-as-truth model reparses + reprints the
      whole scene file on every save and rebuilds the source model on every
      structural mutation; Play snapshot/restore deep-copies the whole world via
      reflection (and drops resources); the hierarchy/inspector panels run O(n)
      world scans per UI frame; there is no multi-file / concurrent-edit (merge)
      story — an external write discards unsaved edits + undo history. All are
      inherent to the current single-file design and are milestone-sized.

## Suggested next three (for a 3D game)

1. Runtime save/load — shipped games need to persist player data
2. ~~glTF animation/skinning~~ — done (keyframe playback + GPU skeletal skinning)
3. ~~Asset pipeline: async loads + load-failure recovery~~ — done (AssetLoader)

(For 2D games, sprite animation remains the top gap.)
