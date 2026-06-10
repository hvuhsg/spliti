package webgpu

import "github.com/hvuhsg/spliti/app"

// drainEvents forwards the platform-filled input buffers as backend-agnostic
// plugin/inputs events and clears them. Both the native and js pollInput call
// it after pumping/receiving platform events; it runs on the main goroutine, so
// no locking is needed.
func drainEvents(c *app.Ctx, g *GPU) {
	for _, ev := range g.keyEvents {
		app.SendEvent(c, ev)
	}
	g.keyEvents = g.keyEvents[:0]

	for _, ev := range g.mouseMove {
		app.SendEvent(c, ev)
	}
	g.mouseMove = g.mouseMove[:0]

	for _, ev := range g.mouseButton {
		app.SendEvent(c, ev)
	}
	g.mouseButton = g.mouseButton[:0]
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
