# Editor backlog

User-reported issues and requests (2026-06-13), plus previously deferred follow-ups.

## Reported issues

1. **Camera entity support** — no camera icon in the scene viewport to select/move, and no way
   to add another camera to the scene or choose which one is the default/active camera.
   (Today the game camera is a global `Camera3D` resource, not an entity — this likely needs a
   camera *component* or editor affordance for posing the resource, plus an icon billboard like
   the light icons and picking for it.)
2. **Light direction should follow transform** — moving/rotating a light entity in the viewport
   should update its direction, not just its position. (Directional/spot direction is currently
   a separate field; it should derive from the entity's rotation, or the gizmo should write it.)
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
8. **Collider visualization + editing** — show the collider wireframe in the viewport and allow
   editing its transform/extents there (gizmo on the collider, not just the entity transform).
9. **Removing collision layers** — ~~the Layers panel can name/add layers but offers no way to
   remove one.~~ **Done.** Added `LayersFile.Remove` plus an "x" button on the last row. Only the
   highest bit can be popped (renumbers nothing); removing a middle bit is refused since it would
   silently renumber every later layer in compiled game code.

## Previously deferred (from M4)

- `spliti build --wasm` polish
- Resource snapshot in play mode (beyond the game camera, which is restored)
- System order edits via srcmodel
- Inspector multi-edit of common components
- Persist-play-edits toggle
