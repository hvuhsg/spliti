# Editor backlog

User-reported issues and requests (2026-06-13), plus previously deferred follow-ups.

## Reported issues

1. **Camera entity support** — ~~no camera icon in the scene viewport to select/move, and no way
   to add another camera to the scene or choose which one is the default/active camera.~~
   **Done.** Added a `render3d.Camera` component (`FovYDeg`/`Near`/`Far`/`Active`): a camera entity
   poses the view from its transform — eye at the world translation, looking down forward (`-Z`),
   `+Y` as up — so the gizmo moves/aims it like a light. A new `applyCameraEntity` system
   (render.go, scheduled after transform propagation and before `writeFrameUniforms`) copies the
   *active* camera entity into the global `Camera3D` resource each frame, so the Game panel and a
   shipped game both render what the entity frames; with no active camera the resource is left
   untouched (scene/game-driven cameras keep working). Among several active cameras the last
   queried wins, and the editor keeps exactly one active. `NewCamera`/`SpawnCamera` spawn helpers;
   `SpawnCamera` is injected as a builtin prefab (`render3d.SpawnCamera`) so it's drag-spawnable
   from Assets and writes a compiling `scene.Spawn` line. Editor affordances: a cyan view-frustum
   icon per camera (brighter when active) with ray-sphere picking (editor/cameraentity.go, wired
   into `pickEntity`); an "Add Camera" toolbar button that spawns a camera posed at the current
   editor view and makes it active; and an inspector "Make active" button (single-active is
   editor-managed, batched as one undo step) in place of a raw `Active` checkbox. The viewport
   glyph is a small camera-body box at the eye plus a view volume — a converging cone for a
   perspective camera, a parallel box for an orthographic one. **Projections:** `Camera3D` and the
   `Camera` component gained orthographic support alongside perspective (`m.Ortho`, matching the
   column-major `[0,1]`-depth `-Z` convention); the inspector has a Perspective/Orthographic picker
   that swaps the `FovYDeg` / `OrthoSize` field, and `applyCameraEntity` copies the mode through to
   the resource. Resource-driving and the ortho matrix mapping are unit-tested (pose + projection
   copy, inactive-leaves-resource, last-active-wins, ortho drive + near/far/edge clip mapping).
2. **Light direction should follow transform** — ~~moving/rotating a light entity in the viewport
   should update its direction, not just its position.~~ **Done.** Removed `DirectionalLight.Direction`
   entirely: a directional light now casts along its transform's forward (`-Z`) world axis, computed
   each frame from `GlobalTransform` (`render3d.Forward`). Direction is no longer a separate field, so
   rotating the light with the gizmo just works and persists through the normal `cmdTransform` (the
   icon arrow follows live). `SpawnDirectionalLight`/`NewDirectionalLight` now take a `Transform3D`;
   added `Transform3D.Facing(dir)` (shortest-arc aim from `-Z`) for code-side authoring, and migrated
   the scaffold sun (`EulerDeg(-60, 30, 0)`, gizmo-editable) and all examples. Renderer reads the
   light's `Forward` instead of a stored vector.
3. **Fly navigation** — ~~no way to move *through* the scene, only orbit/pan.~~ **Done.** The
   fly code (RMB + WASD, Shift to boost, Q/E vertical) was present but dead: it gated on
   `WantCaptureKeyboard`, which `NavEnableKeyboard` forces true whenever the viewport is focused.
   Now gated on `WantTextInput` (true only while editing a text field), so flight works.
4. **W/E/R hotkeys don't switch gizmo modes** — ~~pressing W/E/R should select move/rotate/scale.~~
   **Done.** Same root cause as #3: `handleShortcuts` bailed on `WantCaptureKeyboard` with keyboard
   nav enabled. Re-gated on `WantTextInput`.
5. **Dropdowns for asset-keyed fields** — ~~materials should be picked from a dropdown of known
   materials, not typed into a text field; same for meshes and other registered assets.~~ **Done.**
   Added `MeshRegistry.Keys()` / `MaterialRegistry.Keys()` (sorted) and an inspector combo for
   `MeshRenderer.Mesh` and `MaterialRef.Material`: pick from registered keys, "(none)" clears the
   ref (registry falls back to its default), and an unregistered current value stays visible.
6. **Asset thumbnail previews** in the Assets panel. **Done (partial).** The Assets panel now has
   a browser with previews: a live render-to-texture pane for the selected model/material (turntable,
   offscreen `SceneView`), image thumbnails (`ui.NewImageTexture`), audio waveforms + Play
   (`audio.DecodeWaveform`), and material color swatches. See the asset-panel follow-ups below for
   per-tile thumbnails and preview lighting.
7. **Drag-in asset import** — ~~dragging files (models, textures, …) from the OS into the editor
   should copy them into `assets/` and register them.~~ **Done (partial).** A `glfw.SetDropCallback`
   (editor/drop.go) queues OS-dropped paths, drained on the main thread into `importAssetFile`
   (editor/assetimport.go), which copies the file into `game/assets/`. **Models (.obj)** and
   **materials** are fully wired (code-as-truth): import loads the asset live *and* writes a
   registration line into the `//spliti:assets` `LoadAssets` function via a new `srcmodel.AssetsFile`
   round-trip (mirrors `InputFile`/`LayersFile`), raising the rebuild banner. Drag a model into the
   Scene viewport to spawn it (built-in `render3d.NewMesh` prefab) or onto a `MeshRenderer.Mesh` /
   `MaterialRef.Material` combo to set the ref. **Images (.png/.jpg) and audio (.wav/.mp3/.ogg)** are
   copied + previewed only (not code-registered); **glTF/.glb** is copied only. See follow-ups below.
8. **Collider visualization + editing** — ~~show the collider wireframe in the viewport~~ and allow
   editing its transform/extents there (gizmo on the collider, not just the entity transform).
   **Visualization done.** `drawColliderBoxes` (editor/aids.go) outlines every `collision.Collider3D`
   as a green world-space AABB in the scene line pass each frame — a box centered on the entity's
   world position spanning ±`Half`, axis-aligned and unrotated, matching exactly what the broad phase
   tests (so it shows the real collision bounds, not the mesh AABB). Shares the new `lineBox`/`boxEdges`
   helpers with `drawSelectionBox`. **Still open:** in-viewport gizmo editing of the collider's extents
   (today `Half` is editable only via the inspector).
9. **Removing collision layers** — ~~the Layers panel can name/add layers but offers no way to
   remove one.~~ **Done.** Added `LayersFile.Remove` plus an "x" button on the last row. Only the
   highest bit can be popped (renumbers nothing); removing a middle bit is refused since it would
   silently renumber every later layer in compiled game code.
10. **Inverted side-to-side movement** — ~~moving side to side in the editor is inverted: pressing
    "go left" pans/moves right and "go right" moves left.~~ **Done.** The fly strafe was inverted:
    `cameraRig.flyBasis` (editor/camera.go) computed its right vector as `fwd × up` then applied a
    stray `.Scale(-1)`, so `D` (right) moved `-X` and `A` (left) moved `+X`. Pan already used the
    correct `viewBasis` right (`fwd × up` = `+X` when looking down `-Z`); dropping the extra negation
    makes strafe agree with it.
11. **Default start scene = spinning teapot** — ~~a newly-created game's starting scene should be a
    teapot model slowly spinning, instead of whatever the current empty/default scaffold is.~~
    **Done.** Added `render3d.Teapot(scale)` (plugin/render3d/teapot.go): a compact *procedural*
    teapot (revolved body+lid+knob plus swept tube spout and handle) — not the Bezier Utah teapot, so
    no embedded patch data — with normals from triangle winding (unit-tested for outward-facing
    revolution/tube normals and a sane bounding box). The scaffold's default scene
    (cmd/spliti/scaffold.go) is now ground + sun + a centered `SpawnTeapot` carrying a slow
    `Spinner{Speed: 0.5}`; added a `ceramic` material and the `teapot` mesh to `LoadAssets`.
12. **Prefab creation UX** — ~~there's no discoverable way to create a prefab. Add an explicit
    affordance (e.g. "Create prefab" from an entity's context menu or a button in the Assets panel)
    that registers the selected entity/subtree as a prefab.~~ **Done.** Added "Create prefab..." to
    the hierarchy context menu (editor/prefab.go). It writes the selected mesh entity to
    `game/entities/<name>.go` as a `//spliti:entity` function: `render3d.NewMesh(c, t, mesh, material)`
    plus a `scene.Set` for each of its other editable components (Transform3D is the spawn argument,
    so it is not baked in). Codegen lives in `srcmodel.RenderPrefab`, which reuses `LitForValue`/
    `ensureImport` (the same path scene.Set writeback uses) and skips components that can't be
    expressed as scene literals (e.g. entity refs). Since the new `SpawnX` is a fresh Go symbol, it
    only becomes spawnable after Rebuild & Restart (flagged so the toolbar banner appears).
    **Scoped:** single mesh entity only — non-mesh entities are refused, and children/subtrees are
    not yet included.
13. **Multi-scene support** — if scenes can't yet be created/switched/managed beyond a single one,
    add multi-scene support (create new scenes, switch the active scene, list them in the editor).
14. **Modern ImGui theme** — ~~if it's possible to restyle the ImGui UI, give it a nicer, more modern
    theme (colors, rounding, spacing, fonts).~~ **Done.** Added `editor/theme.go`: a cool slate dark
    palette with a single teal accent for interactive state, plus rounded frames/tabs/scrollbars and
    roomier padding/spacing. cimgui-go v1.5.0 exposes no setters on the persistent `Style`, so
    `pushEditorTheme` applies it per frame via `PushStyleColor`/`PushStyleVar` and `editorUI` pops the
    exact counts it returns. (Fonts left as the ImGui default — the binding ships no alternate atlas.)
15. **Skybox support** — add a skybox to the scene and the ability to change it.
16. **Export option in the toolbar** — ~~there's no export option. Add one to the top menu bar
    alongside "Window".~~ **Done.** Added an "Export" menu-bar button (editor/export.go) opening a
    popup with two targets — native binary and wasm bundle — that build the same artifacts as
    `spliti build [--wasm]`. The build runs detached with output streaming to the Console (reusing
    `streamCommandEnv`, factored out of `streamCommand` so wasm can set `GOOS=js GOARCH=wasm`);
    `checkExport` (schedule.First) reports the outcome. The native binary is named after the project
    dir to avoid colliding with the scaffold's `game/` package directory.

## Asset panel follow-ups

Deferred from the Assets-panel work (items 6 & 7 above). The infrastructure
(`srcmodel.AssetsFile`, the import pipeline, the preview pane, the
`assetDragType` drag/drop) is in place; these extend its coverage and polish.

**Asset-type coverage**

1. **glTF/GLB import.** Today `.gltf`/`.glb` files are copied but not registered (a glTF is a
   multi-mesh Model with its own materials/skins, not a single mesh key). Wire it end to end:
   register via the async `AssetLoader.LoadModelAsync`, generate a `LoadAssets` line, and spawn via
   `render3d.SpawnModel` (a new built-in "model prefab", analogous to the `render3d.NewMesh` mesh
   prefab). Then it previews and drag-spawns like an OBJ.
2. **Image assets become first-class.** render3d has no standalone texture registry — images are only
   used as material textures (`Material.BaseColorTex` etc.), which are `image.Image` fields, not
   string refs. Options: (a) a "Create material from image" action that writes a `materials.Load` line
   with the texture loaded from disk (needs a `Material` texture-path constructor / loader helper), or
   (b) a real `render3d.TextureRegistry` with string refs that materials reference by key. Until then,
   images are preview-only.
3. **Audio registration code-gen.** Audio import is preview + play only because the audio plugin (and
   its `audio.Registry` resource) isn't always present (the testgame editor has none). When it is,
   generate a registration line — needs a disk-path loader like `audio.Registry.LoadFile(ref, path)`
   (today only `LoadFS`/`Load([]byte)` exist) and a `//spliti:assets`-style hook the game opts into.
   Gate the code-gen on the registry resource being present so it never emits uncompilable code.

**Preview quality**

4. ~~**Per-tile 3D thumbnails.**~~ **Done.** The Assets panel is now a thumbnail **grid**
   (`assetGrid`/`assetTile` in editor/assets.go): models show a rendered 3D thumbnail, materials a
   color swatch, images their picture, audio a placeholder. Mesh thumbnails build lazily one at a
   time (editor/assetthumb.go): each mesh renders once into its own small offscreen
   `RenderTarget`+`SceneView` off a shared far-parked entity, then the view is disabled so the target
   freezes as a static image. Invalidated on re-import (`invalidateThumb`). The Assets panel also now
   defaults to the bottom-center dock node, **tabbed with Console/Terminal** (layoutVersion bumped).
   Tiles `PushID` per (kind,key) so identically-named assets (e.g. a mesh and a material both named
   `marker`) no longer collide in ImGui's ID stack. **Still open:** thumbnails don't yet rebuild on
   external hot-reload of an unchanged key; preview lighting (#5) still applies.
5. **Preview lighting / environment.** The preview pane reuses the scene's lights via a hidden entity
   at `previewOrigin={0,1e5,0}`; in a scene with only nearby point lights the preview is dark. Give
   the preview its own light/environment without polluting the world — cleanest via a **separate
   editor-only preview world or render path** (see #8), avoiding the far-offset-entity hack and the
   reserved `__preview_sphere` mesh that currently has to be filtered from asset lists.

**Lifecycle & UX**

6. **Mesh live-unload for clean undo.** `MeshRegistry` has no `Unload`, so undoing a mesh import
   removes the source line but leaves the GPU mesh resident until restart. Add `MeshRegistry.Unload`
   (mirroring `audio.Registry.Unload`) so `cmdImportMesh.Undo` fully reverses.
7. **Delete / rename assets from the panel.** `AssetsFile.Remove` only handles single-statement
   entries (a `LoadOBJFile` mesh or a material); a mesh loaded via a shared local var (the scaffold's
   traced `teapot, err := LoadOBJ(...)` form) is refused. Support removing those (drop the loader
   assignment + its `must(err)` too), add rename, and optionally delete the underlying file (with
   confirmation).
8. **Dedicated preview render path / world.** Replace the far-offset preview entity + reserved sphere
   with a small `render3d` primitive that renders a single mesh+material to a target with a given
   camera and light (e.g. `render3d.RenderPreview`). Removes the world pollution, the `"__"` key
   filtering, and the lighting caveat in #5 — at the cost of a bit of render3d plumbing (a one-item
   `drawState`).
9. **Panel polish.** Filter/search box; a grid layout with larger thumbnails; per-asset metadata
   (file size, image dimensions, audio duration/channels); a waveform playhead/scrub; and a focus on
   the material editor when "+ New Material" creates one.
10. **Generic asset-path field drops.** Only the `MeshRenderer.Mesh` / `MaterialRef.Material` combos
    are drop targets today. Let an image/audio asset be dropped onto any plain `string` field that
    holds an asset path (heuristic by field name/kind), for game components with their own ref fields.

## Previously deferred (from M4)

- `spliti build --wasm` polish
- Resource snapshot in play mode (beyond the game camera, which is restored)
- System order edits via srcmodel
- ~~Inspector multi-edit of common components~~ **Done.** With a multi-selection, an inspector
  edit to the primary now copies the new component value onto every other selected entity that
  has that component, batched into one undo step (`commitComponentEdit` in editor/inspector.go).
  `Transform3D` is excluded — an absolute copy would collapse the selection onto one spot, and
  multi-transform is already the viewport gizmo's job (it applies a world-space delta, not an
  absolute value). The header hint now reads "N selected - edits apply to all (Transform: primary only)".
- Persist-play-edits toggle
