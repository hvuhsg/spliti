package main

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/webgpu"
	"github.com/mlange-42/arche/ecs"
)

// Clickable marks an entity as a navigation target. Its hit rectangle is the
// entity's Transform; clicking inside it switches to Target.
type Clickable struct {
	Target Scene
}

// mouseToWorld converts a click in window coordinates to world coordinates.
// Dividing by the window size (not the framebuffer size) makes the HiDPI scale
// cancel, so clicks land correctly on Retina displays.
func mouseToWorld(c *app.Ctx, x, y float64) (float32, float32) {
	cam := app.GetResource[webgpu.Camera](c)
	if cam == nil {
		return -1, -1
	}
	w, h := webgpu.WindowSize(c)
	if w == 0 || h == 0 {
		return -1, -1
	}
	return float32(x/float64(w)) * cam.WorldW, float32(y/float64(h)) * cam.WorldH
}

// clickSystem hit-tests left-clicks against Clickable entities (topmost Layer
// wins) and switches state to the clicked target. Registered globally; runs in
// every scene so MainFlow's stage boxes and the detail scenes' Back button all
// work through the same path.
func clickSystem(c *app.Ctx) {
	for _, ev := range app.ReadEvents[inputs.MouseButtonEvent](c) {
		if ev.Button != inputs.MouseButtonLeft || ev.Action != inputs.Press {
			continue
		}
		wx, wy := mouseToWorld(c, ev.X, ev.Y)
		hit, hitZ, found := Scene(0), 0, false
		app.Query3[webgpu.Transform, webgpu.Layer, Clickable](c,
			func(_ ecs.Entity, t *webgpu.Transform, l *webgpu.Layer, cl *Clickable) {
				if wx < t.X || wx > t.X+t.W || wy < t.Y || wy > t.Y+t.H {
					return
				}
				if !found || l.Z > hitZ {
					hit, hitZ, found = cl.Target, l.Z, true
				}
			})
		if found {
			app.GetState[Scene](c).Set(hit)
		}
	}
}

// escapeSystem makes Escape go back: to MainFlow from a detail scene, or quit
// from MainFlow.
func escapeSystem(c *app.Ctx) {
	for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
		if ev.Key == inputs.KeyEscape && ev.Action == inputs.Press {
			if app.GetState[Scene](c).Get() == MainFlow {
				c.App().Stop()
			} else {
				app.GetState[Scene](c).Set(MainFlow)
			}
		}
	}
}

// spawnButton creates a labelled, clickable rounded-ish panel. The background is
// a stretched square; the label sits on top; the Clickable entity carries the
// hit rectangle and target.
func spawnButton(c *app.Ctx, ref string, x, y, w, h float32, label string, target Scene, fill webgpu.Color) {
	// Background panel that is also the Clickable hit area.
	spawn5[webgpu.Transform, webgpu.Sprite, webgpu.Color, webgpu.Layer, Clickable](c.Commands(),
		func(t *webgpu.Transform, s *webgpu.Sprite, col *webgpu.Color, l *webgpu.Layer, cl *Clickable) {
			t.X, t.Y, t.W, t.H = x, y, w, h
			s.Ref = "sq"
			*col = fill
			l.Z = zButton
			cl.Target = target
		})
	// Fit the label inside the box so longer captions don't overflow.
	spawnLabelFit(c, ref, label, x+w/2, y+h/2, w*0.82, h*0.32, colText, zBtnTxt)
}

// spawnBackButton places a standard Back button in the top-left of a detail scene.
func spawnBackButton(c *app.Ctx) {
	spawnButton(c, "lbl:back", 3, 3, 18, 7, "< Back", MainFlow, colPanel2)
}
