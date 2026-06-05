# Plan: 3D Radio-Wave Propagation Visualizer on `plugin/render3d`

## 1. Context & Goal

Build an interactive 3D application: a **street scene** with a **transmitter** emitting
radio waves that **propagate, reflect off buildings, and decay**, plus **movable
receivers** that show **what they receive at each location** (signal strength, multipath,
and a decoded view).

This builds on the new `plugin/render3d` backend (perspective camera, depth buffer, PBR
meshes, instancing). That foundation renders solid lit geometry well, but this app needs
several capabilities it does not yet have, **plus** an entirely new **propagation
simulation** layer (which a renderer never provides — it is application/physics logic).

This document is the implementation plan: what to add to the renderer, the simulation
design, how the two connect, and a phased delivery order with files, APIs, tests, and
verification.

### Reusable assets already in the repo
- `examples/radio/dsp.go` (+ `dsp_test.go`) — modulation (QPSK/QAM), FFT, symbol/decode
  math. Pure functions; reusable for the "what does the receiver decode" view.
- `examples/radio/plot.go`, `examples/radio/text.go`, `examples/radio/art.go` — CPU
  rasterization of plots, a bitmap font, and procedural textures. The radio app renders
  these by **baking them into images and drawing them as 2D sprites** on the `webgpu`
  (2D) backend. The *rasterization* is reusable; the *drawing path* is 2D-only and must
  be re-hosted (see Phase 5).
- `examples/radio/scene_channel.go`, `scene_isi.go` — multipath / inter-symbol
  interference visualizations; directly relevant once the propagation model produces a
  multi-tap channel.
- `plugin/render3d/*` — `Cube`, `Plane`, `UVSphere`, `Quad`, `Camera3D`, instancing,
  `Transform3D`, lights, `AddOverlay`/`AddPreRender`/`Pass` seam, `KeyEvent`/
  `MouseButtonEvent`/`MouseMoveEvent`.

---

## 2. Scope & guiding decisions

**Recommended v1 = "schematic but physically motivated":** a CPU propagation model using
free-space path loss + first/second-order specular reflections (the *image method*),
drawn with illustrative expanding wavefront shells, contributing ray paths, a ground
coverage heatmap, and draggable receivers with a HUD. This is tractable, runs in real
time, and demonstrates "leaves, bounces, decays, and is received."

**Deferred to "full" version:** GPU compute field solve (FDTD or large ray batches),
diffraction, frequency-dependent materials, higher-order reflections, glTF-imported
cities, textured PBR buildings.

Design every renderer addition as a **general feature of `render3d`** (transparency,
per-instance color, lines, picking, 2D overlay), not a one-off for this app — they are
broadly useful and keep the plugin coherent.

---

## 3. High-level architecture

Three layers, kept separate:

```
examples/radio3d/                 # the application
  main.go                         # app wiring, plugins, systems
  city.go                         # procedural street generator -> meshes + face list
  controls.go                     # fly camera + receiver placement/drag (uses picking)
  hud.go                          # CPU-rasterized panels (reuse radio plot.go/text.go)
  viz.go                          # maps sim state -> wavefront shells, rays, heatmap
  prop/                           # propagation simulation (pure, GPU-free, testable)
    geometry.go                   # Face, Segment, AABB, ray/segment intersection, BVH
    propagate.go                  # image-method reflections, path loss, complex sum
    channel.go                    # multipath taps -> received power + channel response
    coverage.go                   # grid of virtual receivers -> heatmap field
    *_test.go
  signal/  (or reuse examples/radio/dsp.go via a shared package)

plugin/render3d/                  # renderer feature additions (general-purpose)
  + per-instance color            # heatmaps, tinted wavefronts
  + transparent pass + sorting    # translucent wavefront shells
  + gizmo (line/point) rendering  # rays, wavefront contours, debug
  + picking (unproject + raycast) # click/drag receivers
  + overlay2d (screen quads+text) # HUD host for CPU-rasterized panels
  + (optional) compute pipeline   # advanced field solve
```

The simulation never imports `render3d`; the app's `viz.go` reads sim output and drives
renderer components. This keeps `prop/` unit-testable without a GPU.

---

## 4. Renderer additions (`plugin/render3d`)

Each item lists the gap, the design, the files touched, and the public API.

### 4.1 Per-instance color (heatmaps & tinted wavefronts)
**Gap:** color is a per-*material* uniform; you cannot cheaply tint thousands of
instances (heatmap cells, wavefront samples) differently.

**Design:** extend the per-instance buffer from a `mat4` (64 B) to `mat4 + vec4 color`
(80 B). Add vertex attribute `@location(8)` and multiply it into `baseColor` in the
fragment shader (tint). Add an optional `InstanceColor{m.Vec4}` component; default white
(no tint) when absent.

**Files:** `pipeline.go` (instance stride 64→80, new attribute), `pbr.wgsl` (instance
color in + tint), `render.go` (`renderItem` gains a color; `packMeshInstances` packs an
`instanceData{model mat4; color vec4}` struct instead of bare `m.Mat4`), `components.go`
(`InstanceColor`). Update `render_test.go` stride/packing tests.

**API:** `render3d.InstanceColor{R,G,B,A}` component; helper `SetInstanceColor`.

### 4.2 Transparent pass + depth sorting (wavefront shells)
**Gap:** pipeline is opaque only (`Blend: nil`, depth write on).

**Design:** add a **second pipeline** with premultiplied-alpha blending, **depth test ON,
depth write OFF** (so translucent shells don't occlude each other or the scene wrongly).
Mark entities transparent via a `Transparent{}` tag (or `Material.Transparent bool`).
Render order: opaque pass (existing) → transparent pass, the latter **sorted
back-to-front by camera distance** (cannot instance-batch a sorted set, so draw per
instance or per small group; counts are modest for shells). Reuse the open render pass
(same depth attachment) — just bind the transparent pipeline after opaque draws.

**Files:** `pipeline.go` (build a `transparentPipeline`), `render.go` (collect+sort
transparent items, second draw loop), `components.go` (`Transparent`).

**Notes/footgun:** premultiplied alpha must match the wavefront color packing; document
that transparent objects don't write depth, so heavily overlapping shells will show order
artifacts (acceptable for schematic viz; weighted-blended OIT is a future upgrade that
avoids sorting).

### 4.3 Gizmo (line/point) rendering (rays, contours, debug)
**Gap:** only `TriangleList`; no way to draw rays/beams/contours.

**Design:** an immediate-mode gizmo system: a per-frame dynamic vertex buffer of colored
line segments and points, a `LineList`/`PointList` pipeline (depth test on, no cull, no
depth write), drawn in an overlay-stage system. API collects segments during `Update`/
`PreRender`, uploads once, draws once, clears.

**Files:** new `gizmo.go` + `lines.wgsl`; a `GizmoBuffer` resource; an overlay system
registered in `installSystems`.

**API:**
```go
func Line(c *app.Ctx, a, b m.Vec3, color m.Vec4)
func Ray(c *app.Ctx, origin, dir m.Vec3, len float32, color m.Vec4)
func Point(c *app.Ctx, p m.Vec3, color m.Vec4)
func Polyline(c *app.Ctx, pts []m.Vec3, color m.Vec4)
```

### 4.4 Picking: screen→ray + scene raycast (place/drag receivers)
**Gap:** mouse events are screen coords only; no unproject, no scene raycast; CPU mesh
geometry is discarded after GPU upload.

**Design (two parts):**
1. **Unproject** — `Camera3D.ScreenToRay(px, py, viewportW, viewportH) (origin, dir
   m.Vec3)` using `inverse(proj*view)` on NDC near/far points. Pure math (in `m`/camera).
2. **CPU raycast** — retain CPU geometry: have `MeshRegistry.Load` keep the `Mesh`
   (vertices+indices) alongside the GPU buffers (optionally a `[]Face` collision form).
   Add `Raycast(world, origin, dir) (Hit, bool)` that, per entity with `MeshRenderer +
   GlobalTransform`, transforms the ray into local space via `inverse(model)` and tests
   triangles (Möller–Trumbore). Brute force is fine for a street of boxes; a BVH over
   entities/AABBs is an easy optimization later.

**Files:** `camera.go` (`ScreenToRay`), `mesh.go` (retain CPU mesh / faces), new
`raycast.go` (`Hit{Entity, Point, Dist, Normal}`, `Raycast`), `m/mat.go` already has
`Inverse`.

**API:** `render3d.ScreenToRay(c, px, py)`, `render3d.Raycast(c, origin, dir)`.

### 4.5 2D overlay + text (HUD host for plots & readouts)
**Gap:** `render3d` has the overlay seam but no 2D drawing or text.

**Design (pragmatic):** a lightweight **screen-space textured-quad overlay**: an
orthographic pipeline that draws RGBA textures at pixel rects on top of the 3D frame
(reusing the texture-upload pattern from `plugin/webgpu/texture.go`). The HUD itself is
**CPU-rasterized into images** each time it changes (reuse `examples/radio/plot.go` and
`text.go`), uploaded as a texture, and blitted as one quad per panel. This avoids porting
a full glyph/2D engine while giving arbitrary plots and text.

**Files:** new `overlay2d.go` + `quad2d.wgsl` (screen-space quads + a small
`HUDTextureRegistry`); the app's `hud.go` builds the panel images.

**API:** `render3d.DrawPanel(c, ref string, x, y, w, h int)` (from an `AddOverlay`
system), `render3d.LoadPanel(ref, img)`.

**Alternative considered:** world-space billboard labels (3D text facing camera) — useful
for per-receiver tags; can reuse the same textured-quad machinery in world space later.

### 4.6 (Optional, advanced) GPU compute pipeline
**Gap:** only render pipelines exist. Needed only for a grid **field solve** (FDTD) or
very large ray batches. wgpu supports compute; add a minimal `ComputePipeline` +
storage-buffer dispatch wrapper. **Deferred** — v1 simulation runs on CPU.

### 4.7 (Optional) Material textures & glTF
For realistic buildings/imported cities. The shader already reserves texture slots; add
sampler/texture bind-group entries and a glTF loader. **Deferred.**

---

## 5. Simulation layer (`examples/radio3d/prop`, pure Go)

The physics. No GPU, fully unit-tested.

### 5.1 Geometry (`geometry.go`)
- `Face{ P0, U, V m.Vec3; Normal m.Vec3 }` — a planar rectangle (building wall/roof). A
  box → 6 faces; the ground → 1 large face.
- `AABB`, `Segment`, ray/triangle and ray/AABB intersection (Möller–Trumbore), point-in-
  rectangle test (project onto U,V).
- `BVH` over faces for fast occlusion/line-of-sight queries (brute force acceptable for a
  small street; BVH for the coverage grid where queries are many).
- `mirror(point, face) m.Vec3` — reflect a point across a face's plane (image method).
- `lineOfSight(a, b, faces) bool` — segment unobstructed (ignoring the endpoints' own
  faces, with an epsilon).

### 5.2 Propagation model (`propagate.go`)
Frequency `f` → wavelength `λ = c/f`, wavenumber `k = 2π/λ`.

For transmitter `Tx` (position, power, optionally pattern) and a field point `P`:
- **Direct path:** if `lineOfSight(Tx, P)`, contribute complex amplitude
  `a₀ = sqrt(Pt)·(λ/(4π d))·e^{-jk d}` (Friis amplitude form), `d=|Tx−P|`.
- **First-order reflections:** for each face, image `Tx' = mirror(Tx, face)`; the reflected
  path exists if segment `Tx'→P` crosses the face rectangle at point `Q` and both legs
  `Tx→Q` and `Q→P` are unobstructed. Contribute with total length `L=|Tx'−P|`, a
  reflection coefficient `Γ` (constant, e.g. −0.7, or a simple Fresnel from incidence
  angle + material permittivity), amplitude `a = Γ·sqrt(Pt)·(λ/(4π L))·e^{-jk L}`.
- **Second-order (optional):** recurse the image method over ordered face pairs; cap order
  at 1–2 for real time. Each path yields `(delay = L/c, complexAmplitude, polyline)`.

Output per field point: `[]Path{ Length, Delay, Amp complex, Points []m.Vec3 }`.

### 5.3 Channel & received metrics (`channel.go`)
- Sum paths: `H = Σ aᵢ` → **received power** `Pr = |H|²` (→ dBm for display).
- **Channel impulse response:** bin paths by delay into taps → a multipath channel; feed
  the existing DSP (`examples/radio/dsp.go`) to produce a **decoded view** (constellation
  with ISI, EVM, BER estimate) — reuse `scene_channel.go`/`scene_isi.go` visuals.
- Per-path data also drives the **ray** visualization (color by `|aᵢ|`).

### 5.4 Coverage (`coverage.go`)
- Sample a horizontal grid (e.g. receiver-height plane over the street), compute `Pr` per
  cell (direct + reflections) → a 2D field. Drives the **ground heatmap**. This is the
  many-query case → use the BVH; consider downsampling/throttling (recompute on TX move
  or on demand, not every frame).

---

## 6. Visualization mapping (`examples/radio3d/viz.go`)

How sim output becomes renderer state:

- **Transmitter:** an emissive `UVSphere` + a billboard label.
- **Expanding wavefront shells (the "wave leaving"):** translucent `UVSphere`s centered at
  Tx with radius `r = c·(t − t₀)`, alpha faded by `1/r` and by age (uses §4.2 transparency
  + §4.1 per-instance color). **Reflections:** when the primary shell radius reaches a
  face, spawn a secondary shell centered at the reflection/image point (event-driven), so
  viewers see waves "bounce." Pure illustration, decoupled from the exact `prop` sum.
- **Contributing ray paths (the "bouncing & decaying," exact):** for the selected/active
  receiver, draw each `Path.Points` polyline via gizmo lines (§4.3), colored by `|aᵢ|`
  (strong=warm, weak=cool) — this is the physically computed multipath.
- **Coverage heatmap:** a grid of small instanced `Quad`s (or a baked texture on the
  ground `Plane`) colored by `Pr` via per-instance color (§4.1).
- **Receivers:** movable entities (a distinct gizmo/mesh) placed and dragged with picking
  (§4.4); each shows a HUD panel (§4.5): `Pr` meter, multipath delay profile (reuse
  `plot.go`), and decoded constellation (reuse `dsp.go`).

---

## 7. Phased delivery

Each phase is independently runnable and verifiable. Renderer phases (1–3, 5) are general
features; sim phases (4, 6) are pure and testable without a GPU.

| Phase | Deliverable | Key files | Verify |
|------|-------------|-----------|--------|
| **0. Street + camera** | Procedural street (instanced boxes + ground), fly camera (WASD+mouse), reuse existing render3d | `examples/radio3d/main.go`, `city.go`, `controls.go` | Run; walk the street; depth/lighting correct (screenshot) |
| **1. Per-instance color + transparency** | render3d: instance color attr; transparent blended pass + sort | `render3d/pipeline.go`, `render.go`, `pbr.wgsl`, `components.go` | Unit: stride/packing tests; visual: translucent tinted spheres over scene |
| **2. Gizmo lines** | render3d: immediate-mode colored lines/points | `render3d/gizmo.go`, `lines.wgsl` | Draw axis cross + a few rays; screenshot |
| **3. Picking** | render3d: `ScreenToRay` + CPU `Raycast`; retain CPU meshes | `render3d/camera.go`, `mesh.go`, `raycast.go` | Unit: ray/triangle, unproject round-trip; click a box → highlight |
| **4. Propagation core (CPU)** | `prop/`: faces, LOS, free-space + 1st/2nd-order image reflections, complex sum, channel | `prop/geometry.go`, `propagate.go`, `channel.go` + tests | Unit: known 2-ray ground-reflection result; energy/decay monotonic vs distance |
| **5. Receivers + HUD** | Draggable receivers; 2D overlay + CPU-rasterized panels; decoded view via DSP | `render3d/overlay2d.go`, `quad2d.wgsl`; `radio3d/hud.go`, `controls.go` | Move a receiver; HUD power + constellation update |
| **6. Coverage heatmap** | grid sim → ground heatmap (per-instance colored quads / baked texture) | `prop/coverage.go`, `radio3d/viz.go` | Heatmap brightens near Tx, shadows behind buildings, recompute on Tx move |
| **7. Wave animation** | expanding translucent shells + event-driven reflection shells + contributing ray paths | `radio3d/viz.go` | Visual: wave leaves Tx, bounces off a wall, fades; rays to selected Rx |
| **8. Advanced (optional)** | GPU compute field solve; diffraction; higher-order; glTF + textured buildings | `render3d/compute*`, `prop/` extensions | Perf + fidelity; compare to CPU model |

A credible first milestone is **Phases 0–7**; Phase 8 is open-ended research.

---

## 8. Key types & signatures (sketch)

```go
// render3d additions
type InstanceColor struct{ R, G, B, A float32 }
type Transparent struct{} // tag

func Line(c *app.Ctx, a, b m.Vec3, color m.Vec4)
func Polyline(c *app.Ctx, pts []m.Vec3, color m.Vec4)

func (cam *Camera3D) ScreenToRay(px, py float64, vw, vh int) (origin, dir m.Vec3)
type Hit struct{ Entity ecs.Entity; Point, Normal m.Vec3; Dist float32 }
func Raycast(c *app.Ctx, origin, dir m.Vec3) (Hit, bool)

func LoadPanel(c *app.Ctx, ref string, img image.Image) error
func DrawPanel(c *app.Ctx, ref string, x, y, w, h int) // from an AddOverlay system

// prop (pure)
type Face struct{ P0, U, V, Normal m.Vec3 }
type Path struct{ Length, Delay float32; Amp complex128; Points []m.Vec3 }
func Propagate(tx Tx, p m.Vec3, faces []Face, bvh *BVH, cfg Config) []Path
func Received(paths []Path) (powerW float64, taps []Tap)
func Coverage(tx Tx, grid Grid, faces []Face, bvh *BVH, cfg Config) []float64
```

---

## 9. Testing & verification strategy

- **Pure unit tests (CI, no GPU):**
  - `prop`: ray/triangle & point-in-rect correctness; `mirror` involution; a closed-form
    **two-ray ground-reflection** check; received power decays ~1/d² in free space; LOS
    occlusion behind a box; channel tap delays match path lengths/c.
  - `render3d`: instance stride/packing (extend existing `render_test.go`); `ScreenToRay`
    round-trips a projected world point; `Raycast` hits a unit box from known directions.
- **GPU-dependent code** (pipelines, overlay, gizmo upload) stays out of pure functions,
  matching the existing discipline (`ortho`, `packInstances`, `packMeshInstances`,
  `packLights` are pure & tested).
- **Visual verification:** run `CGO_ENABLED=1 go run ./examples/radio3d` and screenshot at
  each phase (street, transparency, rays, heatmap, wave bounce, HUD).
- **Whole-repo:** `CGO_ENABLED=1 go build ./... && go vet ./... && go test ./...` green.

---

## 10. Risks & footguns

- **Transparency ordering** — translucent shells without depth-write show overlap
  artifacts; mitigate with back-to-front sort, accept residual error, or adopt
  weighted-blended OIT later.
- **Instance stride change (64→80)** breaks the existing packing/tests; update together
  and keep the `unsafe.Sizeof` guard test.
- **Picking needs CPU geometry** the renderer currently discards — retaining it adds
  memory; store a compact face list, not full vertex copies, where possible.
- **Image-method explosion** — reflection order grows combinatorially; hard-cap order and
  cull by face visibility/AABB early. Coverage recompute is the hot path — throttle it.
- **Phase coherence / units** — keep a single source of truth for `λ`, `k`, power (W vs
  dBm); the interference pattern is wrong if path phase isn't consistent.
- **Double gamma / color space** (already handled in render3d: linear shader output + sRGB
  swapchain) — keep heatmap/ray colors in the same convention.
- **HUD cost** — rasterizing panels every frame is wasteful; bake only when inputs change
  (the radio example already uses this "rebake on change" pattern).

---

## 11. Summary of what's missing (at a glance)

**Renderer (general features to add to `plugin/render3d`):**
1. Per-instance color (heatmaps, tinted waves)
2. Transparent/alpha-blended pass + sorting (wavefront shells)
3. Line/point gizmo rendering (rays, contours)
4. Picking: screen→ray unproject + CPU mesh raycast (place/drag receivers)
5. 2D screen-space overlay + text/plot host (HUD)
6. *(optional)* GPU compute; material textures + glTF

**Simulation (new, pure Go — the actual physics):**
7. Scene as CPU faces + BVH; line-of-sight
8. Image-method reflections + free-space path loss + complex multipath sum
9. Receiver channel → power + decoded view (reuse `examples/radio/dsp.go`)
10. Coverage grid → heatmap field

**Application glue:** procedural street, fly camera, receiver controls, sim→viz mapping.

The renderer additions are ~5 well-scoped features; the propagation simulation is the
larger, independent effort and is where the "leaves, bounces, decays, is received"
behavior actually comes from.
