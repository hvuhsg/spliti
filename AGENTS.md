# Building spliti games — a guide for AI agents

spliti is a Bevy-shaped ECS game engine in Go that renders to the terminal or
the GPU. It is also built to be driven by an AI agent: a game can be run
**headlessly and deterministically**, and its state read back as **JSON**, so
you can close the loop — *write → run → observe → correct* — with no window and
no human watching.

Read this before building or modifying a spliti game. For deeper detail follow
the links into `docs/`.

## The loop: how you verify your work

This is the most important thing to internalize. You do **not** need a human to
look at the screen. Use `spliti check`:

```bash
spliti new mygame && cd mygame      # scaffold (never hand-roll the layout)
# ... edit game code ...
spliti check -ticks 120 -out world.json   # run 120 deterministic frames, dump state
# read world.json, assert on it, fix, repeat — works for 3D games too,
# headless: no GPU or display needed (-png is unsupported here; it needs a GPU build)
```

`spliti check` runs the game's default scene + systems under a **virtual clock**
(`plugin/time` Manual mode — one fixed step per frame) and a **seeded RNG**
(`plugin/rng`), so the same code + seed produces **byte-identical** `world.json`
every run. Diff the dump across edits to confirm a change took effect. Flags:
`-ticks N`, `-seed N`, `-out world.json`, `-png frame.png`.

`world.json` is `{entities: [{id, gen, components: {"<pkg>.<Type>": value}}], resources: {...}}`.
Entity-reference fields appear as `[id, gen]` pairs. Assert on it like:
"the entity with `components.Player` has `Transform3D.Translation.X > 0`",
"`resources["game.Score"].Points == 3`".

For pure-logic verification in Go tests, use the `check` package directly
(`check.Run(app, check.Options{Ticks, Script, WorldPath})` + `check.NewScript()`
for input) — it is cgo-free and runs anywhere, including CI.

## Project layout (scaffolded by `spliti new`)

```
mygame/
  spliti.toml              # name, window, default scene
  main.go                  # wires plugins + Run() (native + wasm)
  game/
    game.go                # //spliti:systems RegisterSystems, //spliti:assets LoadAssets
    input.go               # //spliti:input BuildActions (named actions → keys/pad)
    layers.go              # //spliti:layers collision layer constants
    components/            # plain component structs (one concern each)
    systems/               # systems (func(*app.Ctx))
    entities/              # //spliti:entity prefab spawn funcs
    scenes/                # //spliti:scene setup funcs (the editor edits these)
```

Always scaffold with `spliti new <name>` — the editor only understands this
layout. `spliti gen` regenerates the editor/check targets after structural
changes; `spliti edit` opens the visual editor (needs cgo + a GPU).

Run `spliti manifest` inside a project to print its live surface (components,
prefabs, scenes, input actions) plus this engine quick-reference — load it into
context before working on an unfamiliar game.

## ECS in one screen

- **Components** are plain structs. **Systems** are `func(c *app.Ctx)`.
- Spawn/query with the generic helpers: `app.Query1..6[A,B,...](c, fn)`,
  `generic.NewMap1..6[...](w).NewWith(&a, &b)` for spawning.
- **Resources** are singletons: `app.InsertResource(a, &v)` /
  `app.GetResource[T](c)`. Use a resource for game-wide state (score, phase).
- **Events** are frame-buffered: `app.SendEvent(c, v)` / `app.ReadEvents[T](c)`.
- **States**: typed state machines with `OnEnter`/`OnExit`. See `docs/ecs.md`,
  `docs/events-and-states.md`.

## Schedule (when systems run)

Startup (once): `PreStartup → Startup → PostStartup`.
Every frame: `First → PreUpdate → StateTransition → Update → PostUpdate → Last`.
Fixed-timestep loop (driven between StateTransition and Update):
`FixedFirst → FixedUpdate → FixedLast`, run zero-or-more times per frame to catch
the accumulator up. Put deterministic gameplay/physics in `FixedUpdate`; put
input-reading and rendering-facing work in `Update`. Order within a stage with
`.Before`/`.After`/`.Chain`, gate with `.RunIf`. See `docs/scheduling.md`.

## Determinism rules (so `spliti check` stays reproducible)

- Draw randomness from `app.GetResource[rng.Rand](c)`, **never** package-level
  `math/rand`. The scaffold wires `rng.Plugin{Seed: 1}`.
- Read time from the `time.Time` resource (`Delta()`/`Alpha()`); don't call
  `time.Now()` in gameplay.
- Keep gameplay single-threaded (the engine is) and free of map-iteration-order
  dependence.

## Plugins you'll reach for

| Plugin | Provides |
| --- | --- |
| `time` | `Time` resource, FixedUpdate loop; `Manual` virtual clock for checks |
| `rng` | seeded `Rand` resource (deterministic randomness) |
| `inputs` + `inputs/actions` | named actions bound to keys/mouse/pad; `Held/JustPressed/Axis` |
| `render3d` | 3D GPU renderer: `Transform3D`, `MeshRenderer`, camera, lights, PBR (cgo) |
| `webgpu` | 2D GPU textured-sprite renderer (cgo) |
| `tui` / `sprite` / `canvas` | terminal glyph / ASCII-sprite / truecolor-pixel rendering |
| `collision` | spatial-hash broadphase, 2D + 3D, layers/masks |
| `audio` | software mixer: SFX, music, buses, spatial; silent null-sink when headless |
| `ui` | Dear ImGui GPU UI (dev tools) |
| `network` | lockstep multiplayer over TCP |

For persisting **player data** (progress, settings, high scores) at runtime, use
the `save` package (not a plugin): `save.Open("mygame")` → a `Store` of JSON
slots, `Write(slot, &v)` / `Read(slot, &v)` (`ErrNotFound` on first run), `Has`,
`Delete`, `List`. Atomic files under the OS config dir on native, `localStorage`
in the browser. Static scene content stays in Go source — `save` is only for
mutable per-player state.

`defaultplugins.Plugins{}` bundles time + terminal + input + tui for terminal
games. GPU games wire `render3d`/`webgpu` explicitly (cgo, `CGO_ENABLED=1`).

## Gotchas

- GPU backends (`render3d`, `webgpu`, `ui`, the editor) need `CGO_ENABLED=1` and
  a GPU/display. **`spliti check` on a 3D game does not need a GPU or display**:
  the generated check target runs render3d in headless mode (no device, no
  window), so the world dump works on a headless CI runner (it still compiles
  with `CGO_ENABLED=1` for the wgpu binding). `-png` is the exception — capturing
  a frame needs a real GPU build, so it is a no-op (with a notice) under
  `spliti check`. Logic-only checks via the `check` package need neither cgo nor
  a GPU.
- `.spliti/` (editor + check targets) is generated and git-ignored; never hand-edit it.
- Scene files (`game/scenes/*.go`) are the editor's save format — keep spawn
  arguments literal so the editor can round-trip them.

## Where to look

`docs/architecture.md` (big picture), `docs/ecs.md`, `docs/scheduling.md`,
`docs/events-and-states.md`, `docs/plugins.md`, `docs/editor.md`,
`docs/roadmap.md` (incl. the **AI track**: AI-0 determinism, AI-1 `inspect`,
AI-2 `check`, this AI-3 surface).
