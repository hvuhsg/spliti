//go:build !js

package webgpu

import (
	"runtime"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/cogentcore/webgpu/wgpuglfw"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
)

// platform holds the native (GLFW) window. The GLFW window and its event calls
// must run on the OS thread that called glfw.Init, so Build calls
// runtime.LockOSThread and both AddPlugins (which runs Build) and Run must
// execute on the same (main) goroutine — the normal spliti usage.
type platform struct {
	window *glfw.Window
}

// Build implements app.Plugin. It opens a GLFW window, creates the wgpu surface
// from it, runs the shared GPU setup, then wires the GLFW input callbacks into
// the shared input buffers (translating to plugin/inputs events).
func (p Plugin) Build(a *app.App) {
	runtime.LockOSThread()

	width, height, title := p.sizeDefaults()

	if err := glfw.Init(); err != nil {
		panic("webgpu: glfw init: " + err.Error())
	}
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI) // wgpu owns the surface, not GL
	glfw.WindowHint(glfw.Resizable, glfw.True)
	win, err := glfw.CreateWindow(width, height, title, nil, nil)
	if err != nil {
		glfw.Terminate()
		panic("webgpu: create window: " + err.Error())
	}

	instance := wgpu.CreateInstance(nil)
	surface := instance.CreateSurface(wgpuglfw.GetSurfaceDescriptor(win))

	// Configure at the real framebuffer size in pixels — on HiDPI this is larger
	// than the window's screen-coordinate size, giving crisp native-resolution
	// rendering.
	fbw, fbh := win.GetFramebufferSize()
	g := finishBuild(p, a, instance, surface, fbw, fbh)
	g.plat.window = win

	// GLFW callbacks run on the main thread during PollEvents (in the input
	// system), so appending here races nothing.
	win.SetFramebufferSizeCallback(func(_ *glfw.Window, w, h int) {
		g.pendingW, g.pendingH = w, h
		g.resized = true
	})
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, mods glfw.ModifierKey) {
		g.keyEvents = append(g.keyEvents, inputs.KeyEvent{
			Key: inputs.Key(key), Action: inputs.Action(action), Mods: inputs.Mod(mods),
		})
	})
	win.SetCharCallback(func(_ *glfw.Window, r rune) {
		g.keyEvents = append(g.keyEvents, inputs.KeyEvent{Rune: r, Action: inputs.Press})
	})
	win.SetCursorPosCallback(func(_ *glfw.Window, xpos, ypos float64) {
		g.cursorX, g.cursorY = xpos, ypos
		g.mouseMove = append(g.mouseMove, inputs.MouseMoveEvent{X: xpos, Y: ypos})
	})
	win.SetMouseButtonCallback(func(_ *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
		g.mouseButton = append(g.mouseButton, inputs.MouseButtonEvent{
			Button: inputs.MouseButton(button), Action: inputs.Action(action),
			Mods: inputs.Mod(mods), X: g.cursorX, Y: g.cursorY,
		})
	})
}

// pollInput runs in schedule.First on the main goroutine (required by GLFW). It
// pumps the event queue — which fires the callbacks installed in Build — applies
// any pending framebuffer resize, forwards buffered events, and translates a
// window-close request into a CloseEvent + App.Stop().
func pollInput(c *app.Ctx) {
	g := app.GetResource[GPU](c)
	if g == nil {
		return
	}

	glfw.PollEvents()

	if g.resized {
		applyResize(c, g)
		g.resized = false
	}

	drainEvents(c, g)

	if g.plat.window.ShouldClose() {
		app.SendEvent(c, inputs.CloseEvent{})
		c.App().Stop()
	}
}

// platformShutdown destroys the window and terminates GLFW. Called from
// releaseGPU after the GPU objects are freed.
func (g *GPU) platformShutdown() {
	if g.plat.window != nil {
		g.plat.window.Destroy()
	}
	glfw.Terminate()
}

// Window returns the GLFW window, or nil before Build. Native-only convenience
// for systems that need direct window access (e.g. cursor position). Not
// available on js builds.
func Window(c *app.Ctx) *glfw.Window {
	g := app.GetResource[GPU](c)
	if g == nil {
		return nil
	}
	return g.plat.window
}
