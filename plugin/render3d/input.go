package render3d

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
)

// drainEvents forwards the platform-filled input buffers as backend-agnostic
// plugin/inputs events and clears them. Both the native and js pollInput call it
// after pumping/receiving platform events; it runs on the main goroutine, so no
// locking is needed.
func drainEvents(c *app.Ctx, g *GPU) {
	for _, ev := range g.keyEvents {
		// Track held keys from key events (Rune == 0); skip typed-text events.
		if ev.Rune == 0 {
			g.keysDown[ev.Key] = ev.Action != inputs.Release
		}
		app.SendEvent(c, ev)
	}
	g.keyEvents = g.keyEvents[:0]

	for _, ev := range g.mouseMove {
		app.SendEvent(c, ev)
	}
	g.mouseMove = g.mouseMove[:0]

	for _, ev := range g.mouseButton {
		g.buttonsDown[ev.Button] = ev.Action != inputs.Release
		app.SendEvent(c, ev)
	}
	g.mouseButton = g.mouseButton[:0]
}

// applyResize reconfigures the surface to the new framebuffer size and rebuilds
// the size-dependent attachments (MSAA color target and depth target). The
// camera's aspect ratio is updated so the projection (rebuilt each frame in
// writeFrameUniforms) stays correct.
func applyResize(c *app.Ctx, g *GPU) {
	w, h := g.pendingW, g.pendingH
	if w <= 0 || h <= 0 {
		return
	}
	g.config.Width = uint32(w)
	g.config.Height = uint32(h)
	g.surface.Configure(g.adapter, g.device, g.config)

	ensureMSAATarget(g)
	ensureDepthTarget(g)

	// With an offscreen scene target the camera frames the target, not the
	// window, and its aspect is owned by the caller (Camera3D.SetAspect).
	if g.target == nil {
		if cam := app.GetResource[Camera3D](c); cam != nil {
			cam.aspect = float32(w) / float32(h)
		}
	}
}
