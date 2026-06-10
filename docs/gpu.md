# GPU Rendering

The `webgpu` plugin is a second render backend: it opens a real window and draws **textured sprites** through the GPU, as an alternative to the terminal `tui` plugin. It's the proof that rendering in spliti is a swappable plugin — the engine core (`app`, `schedule`) never knew about tcell, so a GPU backend slots into the same seam.

Crucially, `webgpu` is a *library*, not a framework: spliti keeps owning `app.Run()` and the schedule. The plugin only installs a `GPU` resource at `Build` and three systems — input poll in `First`, render and present in `PostUpdate`. The shape mirrors `tui` exactly, just targeting a window instead of a terminal.

> **Build targets.** This backend runs two ways. On **native desktop** it links a bundled `wgpu-native` static library and uses GLFW (a cgo binding), so it needs `CGO_ENABLED=1` and a C toolchain (on macOS, the Xcode command-line tools). In the **browser** it targets `GOOS=js GOARCH=wasm` (no cgo), driving the page's native WebGPU through the same `wgpu` API and reading input from the DOM. The platform-specific window/surface/input lives in `webgpu_native.go` / `webgpu_js.go`; everything else is shared. The terminal backend stays pure Go.

```bash
# native window
CGO_ENABLED=1 go run github.com/hvuhsg/spliti/examples/gpu-demo

# browser (WebGPU-capable browser required)
scripts/build-wasm.sh && go run ./cmd/webserve   # open http://localhost:8080/?demo=gpu-demo
```

Input is reported as backend-agnostic [`plugin/inputs`](../plugin/inputs) events (`inputs.KeyEvent`, `inputs.MouseButtonEvent`, …) with neutral key/button constants, so game code is identical on native and in the browser.

> **Browser harness requirements.** The page that loads the `.wasm` must do two
> things (the bundled `web/index.html` already does both):
>
> 1. Expose the instance as `globalThis.wasm = result` after instantiation —
>    cogentcore/webgpu's js backend reads the wasm memory through it.
> 2. Install the detached-`ArrayBuffer` shim before `go.run`. The library builds a
>    `Uint8ClampedArray` over Go's linear memory for queue writes; when Go grows
>    (and detaches) that memory mid-write the construct throws. The shim wraps the
>    `Uint8ClampedArray` constructor and retargets a detached buffer to the live
>    `globalThis.wasm.instance.exports.mem.buffer` at the same offset (growth
>    preserves contents). Copy the snippet from `web/index.html` into any custom
>    harness, or this will crash intermittently under memory growth.

## Wiring it up

`webgpu` replaces the terminal/input/tui trio. Don't use `defaultplugins` — that bundle pulls in tcell. Pair it with `time.Plugin` for frame pacing:

```go
a := app.New()
a.AddPlugins(
    splititime.Plugin{TargetFrameRate: 60},
    webgpu.Plugin{
        Width: 800, Height: 600,
        Title:      "my game",
        WorldW:     80, WorldH: 60,                       // visible world rect
        ClearColor: webgpu.Color{R: 0.05, G: 0.06, B: 0.10, A: 1},
    },
)
```

The plugin's fields:

| Field            | Meaning                                                                 |
| ---------------- | ---------------------------------------------------------------------- |
| `Width, Height`  | Initial window size in pixels (defaults 800×600).                       |
| `Title`          | Window title (defaults `"spliti"`).                                     |
| `WorldW, WorldH` | The visible world rectangle mapped to the window. **Zero** → use the framebuffer pixel size (one world unit == one pixel). |
| `ClearColor`     | Background fill each frame. Zero value is opaque black.                 |
| `VSync`          | `true` caps to the display refresh (FIFO). `false` presents as fast as the surface allows (Immediate), falling back to FIFO if unsupported. |
| `Smooth`         | `false` (default) samples textures with **Nearest** (crisp pixel art); `true` uses **Linear**, which anti-aliases ordinary sprite edges (whose shape lives in the texture's alpha). This is the cheap, effective AA for 2D sprites — no MSAA target needed. |
| `Samples`        | Multisample anti-aliasing (MSAA) on sprite-quad **geometry** edges. `0`/`1` off; any value `>1` is treated as `4` (the portably-supported count). Does **not** touch texture-alpha edges, so axis-aligned alpha-cutout sprites change little — it pays off for rotated, non-integer-scaled, or camera-zoomed content. For smooth sprite edges use `Smooth`, not this. |

### The main-thread rule

GLFW and WebGPU require the window and event pump to live on the OS thread that created them. `app.AddPlugins` (which runs `Build`) and `app.Run()` both execute on the goroutine `main()` calls them from, so the plugin calls `runtime.LockOSThread()` in `Build`. To make the requirement explicit — and guarantee `main` stays put — also lock in the main package:

```go
func init() { runtime.LockOSThread() }
```

This is the one real departure from `input.Plugin`, which polls tcell in a *background* goroutine. GLFW cannot do that; its poll must run inline in the main loop.

## Components: Transform + Sprite

The render system looks for entities with both:

```go
type Transform struct{ X, Y, W, H float32 } // world-space top-left + size
type Sprite    struct{ Ref string }          // key into the TextureRegistry
```

`Color` and `Layer` are optional add-ons:

```go
type Color struct{ R, G, B, A float32 } // tint; zero value == opaque white
type Layer struct{ Z int }              // higher Z draws on top
```

These are deliberately **tcell-free** — they don't share `tui.Position`/`tui.Glyph`. A GPU game targets these components; a terminal game targets tui's. The final fragment is `texture * tint`, so a `Color` of `{1,1,1,1}` (or no `Color` at all) leaves the texture untouched. A zero or zero-sized `Transform.W`/`H` defaults to the source texture's pixel size, so a sprite drawn 1:1 only needs `X,Y`.

```go
app.Spawn3[webgpu.Transform, webgpu.Sprite, webgpu.Color](c.Commands(),
    func(t *webgpu.Transform, s *webgpu.Sprite, col *webgpu.Color) {
        t.X, t.Y, t.W, t.H = 10, 5, 6, 6
        s.Ref = "ball"
        *col = webgpu.Color{R: 0.13, G: 0.59, B: 0.95, A: 1}
    },
)
```

### `Layer` — explicit draw order

Without a `Layer`, an entity draws at `Z = 0`. Add one to lift a sprite above the crowd. The renderer stable-sorts by `Z` ascending, so equal-`Z` entities keep spawn order. This matters for transparency: lower layers paint first, higher ones blend on top.

## Textures: the registry

A `Sprite.Ref` names a texture you've uploaded into the `TextureRegistry` resource. Upload from any `image.Image` — typically in a `Startup` system, since by then the plugin's `Build` has created the device:

```go
a.AddSystems(schedule.Startup, func(c *app.Ctx) {
    reg := app.GetResource[webgpu.TextureRegistry](c)
    if err := reg.Load("ball", myImage); err != nil {
        panic(err)
    }
})
```

`Load(ref, img)` converts the image to RGBA8, creates a GPU texture, uploads the pixels, and builds the texture's bind group — caching all of it under `ref`. Re-loading the same `ref` replaces it. The image can be a file you decoded (`image/png` etc.) or one you generated procedurally; the demo builds a white disc in code so it ships no binary asset.

The renderer **skips** sprites whose `Ref` isn't registered — the same tolerance `tui` has for unknown sprite refs. No panic, just nothing drawn.

## The render → present pipeline (and why)

The plugin registers two systems in `PostUpdate`, split the same way tui splits draw from flush:

1. **Render** (label `__webgpu_render`):
   - Acquires the surface texture and opens a render pass that clears to `ClearColor`.
   - Collects `(Transform, Sprite)` entities (plus optional `Color`/`Layer`), z-sorts them, packs all instances into one buffer, and issues one **instanced draw per texture**.
   - Leaves the render pass *open*.
2. **Present** (label `__webgpu_present`, `.After(__webgpu_render)`):
   - Ends the pass, submits the command buffer, and calls `surface.Present()`.

```
PostUpdate sequence:
  __webgpu_render    → acquire, clear, draw all sprites (pass left open)
  any AddOverlay-registered systems (HUDs, debug quads, …)
  __webgpu_present   → end pass, submit, present
```

### Why the split exists

Same reasoning as tui: the present is the single point that finalizes the frame, so overlay systems can append draws to the open pass between render and present without each one finalizing a half-built frame. Use `webgpu.AddOverlay(a, fn)` to register a system there, and `webgpu.Pass(c)` to get the open `*wgpu.RenderPassEncoder` (nil if no frame is active this tick). There's also `webgpu.AddPreRender(a, fn)` for systems that should run before the render — mutate world state there, don't draw.

### Batching

Within a frame, instances are grouped into contiguous same-texture runs after the z-sort, and each run is one `Draw` call. All instances are uploaded once per frame into a single growable buffer, and each batch draws from its own offset — reusing one region across draws would make every draw read the last write at submit time. The per-frame scratch slices are reused across frames, so steady-state rendering doesn't allocate.

> **A sharp edge worth knowing.** WebGPU *owns* the texture returned by `GetCurrentTexture()`. You release the `TextureView` you create from it, but **never** the surface texture itself — doing so corrupts the swapchain's drawable pool and faults after a few frames. The present system follows this rule; if you write your own present logic, follow it too.

## The camera

```go
type Camera struct{ WorldW, WorldH float32 } // visible world rect, (0,0) top-left
```

The camera maps world coordinates `[0,WorldW] × [0,WorldH]` to the whole window via an orthographic projection. World space matches the terminal convention: **(0,0) is top-left and Y grows downward** (the projection flips Y for the GPU). Set it through the plugin's `WorldW/WorldH`; leaving them zero defaults the rect to the framebuffer pixel size, so one world unit equals one pixel.

A pixel-default camera auto-refits on window resize (one unit stays one pixel). A camera you sized explicitly is left alone on resize — its content simply rescales to the new window.

## Input

Input is emitted from the `First`-stage poll system as backend-agnostic [`plugin/inputs`](../plugin/inputs) events — the same types whether the platform source is GLFW (native) or the DOM (browser). On native the poll runs `glfw.PollEvents()` (firing the buffered callbacks), applies any pending resize, forwards events, and on a window-close request sends a `CloseEvent` and calls `App.Stop()`. In the browser the events arrive asynchronously from DOM listeners and the poll just drains them.

```go
// plugin/inputs
type KeyEvent struct {
    Key    inputs.Key     // set on press/release/repeat; 0 for typed text
    Rune   rune           // set for typed text; 0 for key events
    Action inputs.Action  // inputs.Press / Release / Repeat
    Mods   inputs.Mod
}

type CloseEvent struct{} // window/tab close requested
```

Two sources feed `KeyEvent`, mirroring GLFW's split: key press/release/repeat fills `Key`/`Action`/`Mods` (with `Rune == 0`), and typed text fills `Rune` (with `Action == inputs.Press`). Check `Key` for control/arrow keys, `Rune` for typed characters. The neutral key/button constants (`inputs.KeyEscape`, `inputs.MouseButtonLeft`, …) mirror GLFW's values.

```go
import "github.com/hvuhsg/spliti/plugin/inputs"

func handleInput(c *app.Ctx) {
    for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
        if ev.Key == inputs.KeyEscape && ev.Action == inputs.Press {
            c.App().Stop()
        }
    }
}
```

The close request already calls `App.Stop()` for you; read `CloseEvent` only if you want to react first (save state, confirm, etc.).

> **Native-only window polling.** `webgpu.Window(c)` returns the `*glfw.Window` for direct GLFW polling (e.g. `win.GetKey(...)`); it exists only in native builds. Code that uses it won't compile for `js/wasm` — prefer the `inputs` events for portable input.

## A complete GPU frame, end to end

```
First:                  time tick; __webgpu_input → poll/drain platform input,
                        resize handling, KeyEvent/CloseEvent, Stop-on-close
PreUpdate:              (network capture, if network.Plugin loaded)
StateTransition:        applyStateTransitions → OnExit/OnEnter systems
[time pre-update hook]: 0..N iterations of FixedFirst → FixedUpdate → FixedLast
Update:                 game logic, input handling, animation
PostUpdate:
  __webgpu_render       acquire surface, clear, draw (Transform, Sprite) entities
  user overlays         extra draws via webgpu.AddOverlay
  __webgpu_present      end pass, submit, present
Last:                   anything genuinely last
[engine drains all Events[T] buffers]
[time post-update hook] frame pacing sleep (native; in the browser the
                        requestAnimationFrame loop paces frames instead)
```

That's the canonical GPU-game frame — structurally identical to the terminal frame in [tui-and-input.md](tui-and-input.md), with the terminal cells swapped for a GPU surface.

## Convenience accessors

```go
win := webgpu.Window(c) // *glfw.Window, native builds only (see Input note)
w, h := webgpu.Size(c)  // current framebuffer size in pixels (0,0 if not ready)
```

## Next

- The `examples/gpu-demo/main.go` source — every concept above in one runnable file.
- [docs/tui-and-input.md](tui-and-input.md) — the terminal backend this mirrors.
- [docs/plugins.md](plugins.md) — the plugin/lifecycle model both backends are built on.
