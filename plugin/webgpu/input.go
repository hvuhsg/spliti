package webgpu

import (
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
)

// KeyEvent is emitted for keyboard activity. Two sources feed it, mirroring
// GLFW's two callbacks:
//
//   - Key presses/releases/repeats: Key, Action, and Mods are set; Rune is 0.
//   - Typed text: Rune is set and Action is glfw.Press; Key is 0.
//
// These are GLFW types, deliberately distinct from input.KeyEvent's tcell types,
// so the GPU backend stays free of any terminal dependency.
type KeyEvent struct {
	Key    glfw.Key
	Rune   rune
	Action glfw.Action
	Mods   glfw.ModifierKey
}

// CloseEvent is emitted once when the window's close button is pressed. The
// input system also calls App.Stop() on close, so games can rely on a clean
// shutdown without handling this; read it if you want to react first.
type CloseEvent struct{}

// pollInput runs in schedule.First on the main goroutine (required by GLFW). It
// pumps the event queue — which fires the callbacks installed in Build — applies
// any pending framebuffer resize, forwards buffered key events, and translates a
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

	for _, ev := range g.keyEvents {
		app.SendEvent(c, ev)
	}
	g.keyEvents = g.keyEvents[:0]

	if g.window.ShouldClose() {
		app.SendEvent(c, CloseEvent{})
		c.App().Stop()
	}
}

// applyResize reconfigures the surface to the new framebuffer size. A pixel-
// default camera (Plugin.WorldW/H unset) is re-fitted to the new size so one
// world unit stays one pixel; a game-set world rect is left as-is (its content
// simply rescales to the window).
func applyResize(c *app.Ctx, g *GPU) {
	w, h := g.pendingW, g.pendingH
	if w <= 0 || h <= 0 {
		return
	}
	g.config.Width = uint32(w)
	g.config.Height = uint32(h)
	g.surface.Configure(g.adapter, g.device, g.config)

	// The MSAA target is sized to the surface; rebuild it for the new dimensions.
	ensureMSAATarget(g)

	if !g.pixelCamera {
		return
	}
	cam := app.GetResource[Camera](c)
	if cam == nil {
		return
	}
	cam.WorldW = float32(w)
	cam.WorldH = float32(h)
	writeCamera(g, cam)
}
