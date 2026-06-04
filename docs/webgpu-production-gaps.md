# WebGPU backend: production-engine gaps

This document is an honest critique of the `plugin/webgpu` backend (and the
`examples/gpu-demo`) measured against what a large production game engine
(Unreal, Unity, Godot, Bevy) would require. Most of it is **fine for spliti's
scope** — the backend is a "render-as-a-plugin" proof, not a renderer — but if
this code were lifted into a big engine, the items below are what a graphics
reviewer would flag.

Ordered worst-first.

> **Update (2026-06-04):** the cheap correctness items and the fixed-timestep
> completeness gap have been fixed — #2 (mipmaps), #3 (premultiplied alpha), #9
> (render interpolation), and the graceful-fallback half of #1 (MSAA degrades
> instead of panicking). Each section below is annotated **RESOLVED** where so.
> The architectural items (#5, #7, #8) and tooling (#10) are left as-is — they're
> deliberate scope choices for a render *plugin*, not a standalone engine.

---

## Actually wrong (correctness / robustness, not polish)

### 1. We `panic` on GPU resource failures — *partially RESOLVED*
`buildPipeline`, `ensureMSAATarget`, and every `CreateTexture` / `CreateSampler`
panic on error (`plugin/webgpu/pipeline.go`).

A production engine **never** takes the process down on a transient device
error. GPUs reset on driver updates, alt-tab, and TDR (timeout detection &
recovery). The engine needs:
- **Device-lost handling** — detect the lost device and recreate the device,
  swapchain, and all GPU resources. *(Still TODO — needs a device-lost callback
  and full resource re-creation; the biggest remaining piece.)*
- **Graceful fallback** — drop to no-MSAA or a lower sample count rather than
  crash when a target can't be allocated. **DONE:** `ensureMSAATarget` now calls
  `disableMSAA`, which drops `g.samples` to 1 and *rebuilds the pipeline* at one
  sample (so its `MultisampleState.Count` matches the now-1-sample swapchain
  attachment) instead of panicking. `Load` already propagates errors.
- **Error propagation** — return errors up to a render-graph layer that can
  decide, instead of `panic`. *(Init-time `buildPipeline` failures stay fatal —
  a startup that can't create its pipeline has nothing to fall back to.)*

### 2. No mipmaps — only half the aliasing is fixed — *RESOLVED*
~~`plugin/webgpu/texture.go` uploads `MipLevelCount: 1`, and the sampler uses
`LodMaxClamp: 1`, `MaxAnisotropy: 1`.~~

**Fixed.** `Load` now generates a full mip chain on the CPU (box-filtered in
premultiplied space — see #3 — by `downsample`/`mipLevelCount`) and uploads every
level. The sampler ranges over the whole chain (`LodMaxClamp = maxLOD`); `Smooth`
selects **trilinear + 16x anisotropic** filtering (anisotropy stays 1 in Nearest
mode, where it would be illegal). Sprites scaled down now sample a matching mip
instead of shimmering.

### 3. Straight (non-premultiplied) alpha + linear filtering → edge fringing — *RESOLVED*
~~Blend state is `SrcAlpha / OneMinusSrcAlpha` and textures are straight-alpha.~~

**Fixed.** Textures are premultiplied on upload (`premultiplyAlpha`, on a copy so
the caller's image isn't mutated), the blend state is now `One /
OneMinusSrcAlpha`, and `fs_main` outputs premultiplied color (tint multiplies rgb
then scales the whole texel by tint alpha). Colored sprites no longer fringe at
alpha edges. *(Caveat: premultiply and mip downsample happen in the sRGB-encoded
byte domain, not gamma-correct linear space — that's the separate item #7.)*

### 4. Immediate resource destruction, ignoring frames in flight
`ensureMSAATarget` does `Release()` then recreate on resize. In a pipelined
renderer where the GPU may still be reading last frame's target, that is a
use-after-free. Engines use a **deferred deletion queue** (destroy after N
frames, or behind a fence). We are safe only because the loop is strictly
synchronous and single-frame.

---

## Architecture that won't scale

### 5. The whole loop blocks on `Present()`
Our "let vsync (FIFO) pace the loop, drop the sleep cap" conclusion is correct
for a single-threaded toy, but it means **simulation stalls while waiting for
the GPU / display refresh**. Production engines run a separate render / RHI
thread, keep **2–3 frames in flight**, and use a frame pacer so simulation never
blocks on present. The single-goroutine `input → sim → render → present` loop is
the ceiling here.

### 6. Filtering and AA are global, build-time settings
- `Smooth` bakes **one** sampler at startup for the entire engine. Real engines
  make filtering a **per-texture / per-material** property (point-sampled sprites
  and smooth sprites coexist in one scene) and keep a **sampler cache**.
- `Samples` is fixed at pipeline build, supports only 1 or 4 (hardcoded in
  `normalizeSamples` rather than queried from device/format capabilities), and
  is not a **runtime quality setting**. Engines expose AA quality through a
  scalability/settings system and rebuild pipeline state via a cache.

### 7. We render straight to the swapchain
MSAA resolves directly to the backbuffer. There is no intermediate HDR target,
no post-process chain, no tonemap, no color management. The moment you want
bloom, post-AA, or correct linear-space blending, this topology must be rebuilt.

Related: **sRGB / gamma is unconsidered.** The tint multiply happens in whatever
space `caps.Formats[0]` happens to be; blending and the MSAA resolve are not
guaranteed gamma-correct.

---

## Right for a 2D demo, wrong as an engine default

### 8. MSAA itself
For modern AAA (deferred / clustered 3D), MSAA is largely abandoned — it is
expensive and does not address shader / specular aliasing. The contemporary
default is **TAA** or post-process AA plus temporal upscaling (DLSS / FSR /
TAAU). MSAA is fine for forward 2D and simple 3D, but "best AA in a big engine"
today is temporal, not what we built.

### 9. FixedUpdate without interpolation — *RESOLVED*
~~We decoupled physics to a fixed 64 Hz step but never added the render-time
**interpolation** (the deferred "Layer 2").~~

**Fixed**, exactly along the lines below:
1. Each movable entity stores **previous** and **current** fixed positions
   (`Body.Prev*/Cur*` in `examples/gpu-demo`); `move` steps `Cur` and rolls the
   old `Cur` into `Prev`, never touching the draw transform.
2. The time plugin exposes the interpolation alpha via `Time.Alpha()`
   (`fixedAccum / fixedTimestep`).
3. A render-time system (`interpolate`, registered with `webgpu.AddPreRender`)
   writes `lerp(prev, cur, alpha)` into each `Transform` before the sprite pass.

---

## Demo hygiene

### 10. Profiling is `fmt.Printf` + global flags
The `frameStats` system prints to stdout once a second, with package-level flag
vars (`-trace`, `-cpuprofile`). Fine for our debugging, not shippable. Real
engines use Tracy / PIX / Unreal Insights, an in-engine stats HUD, and crucially
**GPU timestamp queries**. Our `frameStats` measures only **CPU** frame delta —
it literally cannot see GPU cost or per-pass timing. We diagnosed the pacing
issue only because the scene is GPU-trivial.

---

## Summary

| # | Gap | Class | Cheap to fix? | Status |
|---|-----|-------|---------------|--------|
| 1 | Panics on GPU errors / no device-loss handling | Correctness | No (biggest) | Partial — graceful MSAA fallback done; device-loss TODO |
| 2 | No mipmaps (minification aliasing) | Correctness | **Yes** | **Fixed** |
| 3 | Non-premultiplied alpha fringing | Correctness | **Yes** | **Fixed** |
| 4 | Immediate destruction w/ frames in flight | Correctness | Medium | Open (not a live bug: loop is synchronous single-frame) |
| 5 | Loop blocks on Present (single-threaded) | Architecture | No | Open (scope) |
| 6 | Global build-time filtering / AA settings | Architecture | Medium | Open (scope) |
| 7 | Renders straight to swapchain (no post / HDR / sRGB) | Architecture | No | Open (scope) |
| 8 | MSAA vs TAA/temporal | Design default | N/A | N/A |
| 9 | FixedUpdate without render interpolation | Design completeness | **Yes** | **Fixed** |
| 10 | Printf profiling, CPU-only timing | Tooling | Yes | Open (scope) |

**Showstoppers if lifted into a big engine:** #1, #2, #3, #5. Of these, #2 and
#3 are now fixed and #1's graceful-fallback half is done; the remaining
showstopper work is device-loss recovery (#1) and the threaded render loop (#5).

Nothing here is conceptually wrong **for spliti's scope** (a render plugin
proving the engine seam). This list is the gap between that scope and a
production renderer.
