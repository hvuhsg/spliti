//go:build js

package webgpu

import (
	"strconv"
	"syscall/js"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
)

// platform holds the browser canvas and the DOM event callbacks (kept alive and
// released on shutdown). There is no OS-thread requirement on js — wasm is
// single-threaded.
type platform struct {
	canvas js.Value
	funcs  []js.Func
}

// Build implements app.Plugin. It finds (or creates) the page's canvas, creates
// the wgpu surface from it via the browser's WebGPU, runs the shared GPU setup,
// then registers DOM listeners that translate keyboard/mouse/resize into the
// shared input buffers (as plugin/inputs events).
func (p Plugin) Build(a *app.App) {
	width, height, title := p.sizeDefaults()

	doc := js.Global().Get("document")
	if doc.Truthy() {
		doc.Set("title", title)
	}

	canvas := doc.Call("getElementById", canvasID)
	if !canvas.Truthy() {
		canvas = doc.Call("createElement", "canvas")
		canvas.Set("id", canvasID)
		doc.Get("body").Call("appendChild", canvas)
	}

	// CSS (logical) size: prefer an existing layout size from the shell's CSS,
	// else fall back to the plugin's configured pixel size.
	cssW, cssH := width, height
	if cw := canvas.Get("clientWidth").Int(); cw > 0 {
		cssW = cw
	}
	if ch := canvas.Get("clientHeight").Int(); ch > 0 {
		cssH = ch
	}
	style := canvas.Get("style")
	style.Set("width", strconv.Itoa(cssW)+"px")
	style.Set("height", strconv.Itoa(cssH)+"px")

	// Backing-store size in device pixels for crisp HiDPI rendering, mirroring the
	// native GetFramebufferSize behavior.
	fbw, fbh := devicePixels(cssW, cssH)
	canvas.Set("width", fbw)
	canvas.Set("height", fbh)

	instance := wgpu.CreateInstance(nil)
	if instance == nil {
		panic("webgpu: WebGPU is not available in this browser")
	}
	surface := instance.CreateSurface(&wgpu.SurfaceDescriptor{Canvas: canvas})

	g := finishBuild(p, a, instance, surface, fbw, fbh)
	g.plat.canvas = canvas

	installDOMInput(g, canvas)
}

// canvasID is the element id the js backend looks for (and creates if absent).
// The web/ shell provides a <canvas id="spliti-canvas">.
//
// Note on the detached-ArrayBuffer hazard: cogentcore/webgpu's js queue writes
// (WriteBuffer/WriteTexture) build a typed-array view over Go's wasm linear
// memory; if Go grows (and detaches) that memory mid-write, the view's
// construction throws. The web/ harness installs a Uint8ClampedArray shim that
// retargets a detached buffer to the live wasm memory — see web/index.html. A
// custom HTML harness must include the same shim (and set globalThis.wasm).
const canvasID = "spliti-canvas"

// devicePixels scales a logical (CSS px) size by the device pixel ratio.
func devicePixels(w, h int) (int, int) {
	dpr := js.Global().Get("devicePixelRatio").Float()
	if dpr <= 0 {
		dpr = 1
	}
	return int(float64(w) * dpr), int(float64(h) * dpr)
}

// installDOMInput registers the keyboard, mouse, and resize listeners. Callbacks
// append to the shared input buffers; pollInput drains them on the main loop.
func installDOMInput(g *GPU, canvas js.Value) {
	add := func(target js.Value, event string, fn func(js.Value)) {
		cb := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				fn(args[0])
			}
			return nil
		})
		g.plat.funcs = append(g.plat.funcs, cb)
		target.Call("addEventListener", event, cb)
	}

	win := js.Global()

	add(win, "keydown", func(e js.Value) {
		code := e.Get("code").String()
		if scrolls(code) {
			e.Call("preventDefault")
		}
		act := inputs.Press
		if e.Get("repeat").Bool() {
			act = inputs.Repeat
		}
		g.keyEvents = append(g.keyEvents, inputs.KeyEvent{
			Key: inputs.KeyFromDOMCode(code), Action: act, Mods: domMods(e),
		})
		// Typed text mirrors the native char callback: a separate Rune event for
		// single-character keys (excludes "Enter", "ArrowUp", etc.).
		if k := e.Get("key").String(); len([]rune(k)) == 1 {
			g.keyEvents = append(g.keyEvents, inputs.KeyEvent{
				Rune: []rune(k)[0], Action: inputs.Press,
			})
		}
	})
	add(win, "keyup", func(e js.Value) {
		g.keyEvents = append(g.keyEvents, inputs.KeyEvent{
			Key:    inputs.KeyFromDOMCode(e.Get("code").String()),
			Action: inputs.Release, Mods: domMods(e),
		})
	})

	add(canvas, "mousemove", func(e js.Value) {
		x, y := e.Get("offsetX").Float(), e.Get("offsetY").Float()
		g.cursorX, g.cursorY = x, y
		g.mouseMove = append(g.mouseMove, inputs.MouseMoveEvent{X: x, Y: y})
	})
	add(canvas, "mousedown", func(e js.Value) {
		g.mouseButton = append(g.mouseButton, inputs.MouseButtonEvent{
			Button: domButton(e.Get("button").Int()), Action: inputs.Press,
			Mods: domMods(e), X: e.Get("offsetX").Float(), Y: e.Get("offsetY").Float(),
		})
	})
	add(canvas, "mouseup", func(e js.Value) {
		g.mouseButton = append(g.mouseButton, inputs.MouseButtonEvent{
			Button: domButton(e.Get("button").Int()), Action: inputs.Release,
			Mods: domMods(e), X: e.Get("offsetX").Float(), Y: e.Get("offsetY").Float(),
		})
	})
	add(canvas, "contextmenu", func(e js.Value) { e.Call("preventDefault") })

	// Resize: track the canvas's CSS size and rescale the backing store.
	resize := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		cw, ch := canvas.Get("clientWidth").Int(), canvas.Get("clientHeight").Int()
		if cw <= 0 || ch <= 0 {
			return nil
		}
		nw, nh := devicePixels(cw, ch)
		canvas.Set("width", nw)
		canvas.Set("height", nh)
		g.pendingW, g.pendingH = nw, nh
		g.resized = true
		return nil
	})
	g.plat.funcs = append(g.plat.funcs, resize)
	if ro := js.Global().Get("ResizeObserver"); ro.Truthy() {
		ro.New(resize).Call("observe", canvas)
	} else {
		win.Call("addEventListener", "resize", resize)
	}
}

// pollInput runs in schedule.First. DOM events arrive asynchronously into the
// shared buffers; here we just apply any pending resize and forward them.
func pollInput(c *app.Ctx) {
	g := app.GetResource[GPU](c)
	if g == nil {
		return
	}
	if g.resized {
		applyResize(c, g)
		g.resized = false
	}
	drainEvents(c, g)
}

// platformShutdown releases the DOM callbacks. The canvas and WebGPU context are
// owned by the page and need no explicit teardown.
func (g *GPU) platformShutdown() {
	for _, f := range g.plat.funcs {
		f.Release()
	}
	g.plat.funcs = nil
}

func domMods(e js.Value) inputs.Mod {
	var m inputs.Mod
	if e.Get("shiftKey").Bool() {
		m |= inputs.ModShift
	}
	if e.Get("ctrlKey").Bool() {
		m |= inputs.ModControl
	}
	if e.Get("altKey").Bool() {
		m |= inputs.ModAlt
	}
	if e.Get("metaKey").Bool() {
		m |= inputs.ModSuper
	}
	return m
}

func domButton(b int) inputs.MouseButton {
	switch b {
	case 0:
		return inputs.MouseButtonLeft
	case 1:
		return inputs.MouseButtonMiddle
	case 2:
		return inputs.MouseButtonRight
	default:
		return inputs.MouseButton(b)
	}
}

// scrolls reports whether a key code would scroll the page by default, so the
// game can claim it.
func scrolls(code string) bool {
	switch code {
	case "Space", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight",
		"PageUp", "PageDown", "Home", "End", "Tab":
		return true
	}
	return false
}
