# Editor backlog

User-reported issues and requests (2026-06-13), plus previously deferred follow-ups.

## Reported issues

1. **Camera entity support** — no camera icon in the scene viewport to select/move, and no way
   to add another camera to the scene or choose which one is the default/active camera.
   (Today the game camera is a global `Camera3D` resource, not an entity — this likely needs a
   camera *component* or editor affordance for posing the resource, plus an icon billboard like
   the light icons and picking for it.)
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
6. **Asset thumbnail previews** in the Assets panel.
7. **Drag-in asset import** — dragging files (models, textures, …) from the OS into the editor
   should copy them into `assets/` and register them.
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
11. **Default start scene = spinning teapot** — a newly-created game's starting scene should be a
    teapot model slowly spinning, instead of whatever the current empty/default scaffold is.
12. **Prefab creation UX** — there's no discoverable way to create a prefab. Add an explicit
    affordance (e.g. "Create prefab" from an entity's context menu or a button in the Assets panel)
    that registers the selected entity/subtree as a prefab.
13. **Multi-scene support** — if scenes can't yet be created/switched/managed beyond a single one,
    add multi-scene support (create new scenes, switch the active scene, list them in the editor).
14. **Modern ImGui theme** — ~~if it's possible to restyle the ImGui UI, give it a nicer, more modern
    theme (colors, rounding, spacing, fonts).~~ **Done.** Added `editor/theme.go`: a cool slate dark
    palette with a single teal accent for interactive state, plus rounded frames/tabs/scrollbars and
    roomier padding/spacing. cimgui-go v1.5.0 exposes no setters on the persistent `Style`, so
    `pushEditorTheme` applies it per frame via `PushStyleColor`/`PushStyleVar` and `editorUI` pops the
    exact counts it returns. (Fonts left as the ImGui default — the binding ships no alternate atlas.)
15. **Skybox support** — add a skybox to the scene and the ability to change it.
16. **Export option in the toolbar** — there's no export option. Add one to the top menu bar
    alongside "Window".

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
