# Visual editor

`spliti` ships a Dear ImGui scene editor whose save format is **your game's own Go source**. There is no `.scene` file, no JSON, no binary asset DB: dragging a prefab into the viewport adds a `scene.Spawn(...)` line to `game/scenes/main.go`, moving it with the gizmo rewrites the float literals in that line, and hand-editing the same file in your IDE flows back into the running editor through a file watcher. The code is the truth; the editor is a fancy way of editing it.

## Quick start

```bash
go install github.com/hvuhsg/spliti/cmd/spliti@latest

spliti new mygame
cd mygame
spliti edit
```

`spliti edit` generates the editor target, builds it (needs cgo — the editor runs on the 3D GPU backend), and launches it. You get a viewport with a ground plane, a couple of demo entities, and the panel set described below.

### The `spliti` CLI

| Command | What it does |
| --- | --- |
| `spliti new <name> [--engine <path>]` | Scaffold a new game project. `--engine` (or `SPLITI_ENGINE`) points the module at a local engine checkout. |
| `spliti gen` | Regenerate the `.spliti/editor` target from the project's `//spliti:` markers. |
| `spliti edit` | `gen`, then build and launch the editor binary. |
| `spliti run` | `go run .` — run the game itself, no editor. |
| `spliti build [--wasm]` | Build the shippable game binary (or `game.wasm` for the browser). |

## Project layout

`spliti new` scaffolds this shape, and the editor's code generator expects it:

```
mygame/
├── spliti.toml            # project name, window size, default scene
├── main.go                # the game entry point — plain spliti app, no editor code
├── game/
│   ├── game.go            # //spliti:systems and //spliti:assets hooks
│   ├── components/        # exported component structs
│   ├── entities/          # //spliti:entity prefab functions
│   ├── systems/           # game logic systems
│   └── scenes/            # //spliti:scene functions — the editor's save target
└── .spliti/
    └── editor/            # GENERATED editor binary target (gitignored)
```

Three marker comments wire your code into the editor:

- `//spliti:scene` on a `func(c *app.Ctx)` in `game/scenes/` — a scene the editor can open and write back to.
- `//spliti:entity` on a `func(c *app.Ctx, t render3d.Transform3D) ecs.Entity` in `game/entities/` — a prefab; it appears in the Assets panel and is what spawn lines call.
- `//spliti:systems` / `//spliti:assets` on functions in `game/game.go` — register your gameplay systems (run in Play mode) and load assets (meshes, materials, textures).

`spliti gen` scans these markers and writes `.spliti/editor/`: a `main.go` that wires your game into `editor.Plugin`, and a `registry_gen.go` that registers every exported component in `game/components/` for the inspector and every prefab for the Assets panel.

`.spliti/editor` is a **nested Go module**, not a package of your game. The `go` tool ignores dot-directories, so `go mod tidy` at the project root never sees it — editor-only dependencies (dst, cimgui-go) stay out of your shipped game's `go.mod`.

## The scene grammar

A scene file is ordinary Go, restricted to a small vocabulary the editor can parse and rewrite (`scene/scene.go`):

```go
//spliti:scene Main
func Main(c *app.Ctx) {
    _ = scene.Spawn(c, "ground", entities.SpawnGround(c, render3d.XForm()))

    crate1 := scene.Spawn(c, "crate1",
        entities.SpawnCrate(c, render3d.XForm().At(1.74, 2.42, 0.2).EulerDeg(0, 45, 0)))
    scene.Set(c, crate1, render3d.MaterialRef{Material: "metal"})
    scene.Set(c, crate1, components.Spinner{Speed: 11.3})
}
```

- `scene.Spawn(c, "name", prefab(...))` — instantiate a prefab and tag it with a stable string identity (`scene.Name`). The name is what selection, undo, and the hierarchy key on.
- `scene.Set(c, e, v)` — per-instance component override (add or overwrite).
- `scene.Remove[T](c, e)` — strip a component the prefab added.
- `scene.Parent(c, child, parent)` — parent one instance under another.

At runtime (in your shipped game) these are plain function calls — `scene` is a tiny runtime package, not an editor dependency. In the editor they are *also* the writeback grammar: gizmo moves rewrite the `XForm()` chain literals, inspector edits rewrite `scene.Set` lines, reparenting rewrites `scene.Parent` lines.

The parser is conservative: anything it doesn't recognize (loops, conditionals, computed arguments) is preserved verbatim and simply isn't editable from the GUI. A transform chain with a non-literal argument is shown read-only rather than clobbered. Every save is re-parsed before it hits disk, so the editor can never write a scene file it can't read back.

## Panels and controls

- **Scene (viewport)** — the offscreen-rendered 3D view. Click to pick (lights draw clickable star icons), ctrl/cmd-click for multi-select, drag the gizmo to transform — with several entities selected the whole group follows one drag as one undo step. Camera: right-drag orbits, middle-drag pans, scroll dollies, holding RMB gives WASD fly (Q/E vertical, Shift for 3× speed), `F` frames the selection.
- **Hierarchy** — the scene's named instances as a tree (built from `render3d.Parent` links). Click selects (ctrl/cmd-click toggles multi-selection), drag reparents; context menu renames, duplicates, deletes, unparents — delete and duplicate act on the whole selection.
- **Inspector** — the selected entity's components, editable field-by-field: numbers, vectors, quaternions (as Euler degrees), nested structs, slices (with add/remove), and entity-reference fields (a combo of named instances, written to source as the referenced spawn's variable). Edits apply live while you drag and commit one undo step (and one source rewrite) when you release. "Add component" lists every registered type. `Collider3D` gets a layer combo and a mask checklist when the project declares named layers.
- **Assets** — the project's prefabs. Drag one into the viewport to spawn it where the drop ray lands (a new `scene.Spawn` line); double-click spawns at the origin.
- **Systems** — your game systems grouped by stage, each with an enable toggle. Toggles gate execution during Play and are session-only — they're never written to source.
- **Layers** — the game's named collision layers, backed by the `//spliti:layers` const block in `game/layers.go`. Rename or append in place (removal would silently renumber compiled bits, so it's not offered).
- **Input** — the game's action bindings, backed by the `//spliti:input` function in `game/input.go`. Actions and axes list their sources as removable chips; "+ bind" opens a press-any-input capture (keys and gamepads live, mouse via buttons, two presses for a negative/positive button axis). Every edit rewrites the source *and* rebinds the live `actions.Map`, so Play picks it up without a rebuild.
- **Console** — editor status, game log output, and rebuild output. Build errors with `file:line` locations are clickable and open in your editor.

Shortcuts: `W`/`E`/`R` translate/rotate/scale gizmo, `F` frame selection, `Ctrl+Z` / `Ctrl+Shift+Z` undo/redo, `Ctrl+S` save now, `Ctrl+D` duplicate, `Delete` delete, `Ctrl+P` toggle Play.

Every structural edit — spawn, delete, duplicate, rename, reparent, component add/remove, transform — goes through the undo stack, and each undo entry maps to a source edit, so undo history and file history never diverge.

## Two-way sync

Saves are debounced (~500 ms after the last edit) and written atomically. A file watcher (fsnotify) covers the scene file and the `game/` tree:

- **Scene file changed on disk** (you edited it in your IDE): the editor re-parses it and diffs the result into the live world — entities appear, move, and disappear without a restart. The editor recognizes its own saves by content hash and ignores them, so the loop doesn't echo.
- **Any other `.go` file changed** (components, entities, systems): that's a code change the running binary can't absorb — a **Rebuild & Restart** banner appears.

## Play mode

`Ctrl+P` (or the toolbar) snapshots the world and starts running your game systems in place — same process, same window. Pause freezes them; Step advances exactly one frame while paused. Stop despawns everything and restores the snapshot, so whatever your systems did is rolled back; selection survives by instance name.

Notes on the snapshot:

- It captures every live entity's *registered* components (anything in the registry — engine built-ins plus your `game/components/` types). Unregistered component types and resources are not captured.
- On restore, entities get fresh IDs; every entity reference inside every registered component — `render3d.Parent` links and your own component fields alike — is remapped to the new handles.
- Edits made during Play are **live-only**: they bypass both the undo stack and source writeback, since Stop reverts them anyway. Tune values during Play to experiment. The **keep transforms** toggle in the play controls is the exception: with it on, Stop re-applies where play moved the named instances as one undoable edit, written to the scene file.

## Rebuild & Restart

When the watcher flags a code change (or you add a new component/prefab the registry doesn't know), clicking **Rebuild & Restart** runs `spliti gen`, `go mod tidy`, and `go build` for the editor target in the background, streaming output to the Console. On success the editor saves its session — camera pose, selection, panel layout — to `.spliti/session.json` (+ `imgui.ini`) and re-execs the fresh binary in place (`syscall.Exec`), which restores the session on startup. From your side it's a few seconds of pause and the editor comes back where you left it, now knowing your new code. On failure, the Console shows the compiler errors (clickable) and the old binary keeps running.

## What it doesn't do (yet)

- The editor edits **scenes**, not arbitrary game code — systems, prefab internals, and asset loading are written in your IDE (the watcher + rebuild flow is designed around that).
- Scenes are 3D (`render3d`) only; there's no 2D/terminal scene editing.
- Resources aren't snapshotted in Play mode, and entity references inside game components don't survive a Stop restore.
- No multi-select, no prefab *editing* in the GUI (prefabs are Go functions), no animation timeline.
