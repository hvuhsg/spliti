package main

import (
	"math"
	"math/cmplx"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radio3d/prop"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/mlange-42/arche/ecs"
)

const (
	shellMaxR   = 60  // meters a wavefront shell expands before recycling
	shellPeriod = 2.2 // seconds per shell cycle
)

// vizSystem maps the current simulation state onto renderer components and the
// immediate-mode overlays each frame, before the render pass. It runs the active
// receiver's propagation, animates the wavefront shells, recolors the coverage
// heatmap, draws the contributing rays, and refreshes the HUD.
func vizSystem(c *app.Ctx) {
	scene := app.GetResource[Scene](c)
	sim := app.GetResource[Sim](c)
	cov := app.GetResource[Coverage](c)
	view := app.GetResource[View](c)
	tm := app.GetResource[splititime.Time](c)
	if scene == nil || sim == nil || view == nil {
		return
	}
	if tm != nil {
		view.T += float32(tm.Delta().Seconds())
	}

	// Active receiver position from its entity.
	app.Query2[render3d.Transform3D, receiverTag](c, func(_ ecs.Entity, t *render3d.Transform3D, _ *receiverTag) {
		sim.RxPos = t.Translation
	})

	// Run the propagation for the active receiver.
	sim.Paths = prop.Propagate(scene.Tx, sim.RxPos, scene.Faces, scene.BVH, scene.Cfg)
	sim.PowerW, sim.Taps = prop.Received(sim.Paths)

	updateShells(c, scene, view)
	updateHeatmap(c, scene, cov, view)
	if view.ShowRays {
		drawRays(c, sim)
	}
	updateHUD(c, scene, sim)
}

// updateShells animates the translucent wavefront spheres expanding from the Tx.
func updateShells(c *app.Ctx, scene *Scene, view *View) {
	app.Query3[render3d.Transform3D, render3d.InstanceColor, shell](c,
		func(_ ecs.Entity, t *render3d.Transform3D, col *render3d.InstanceColor, sh *shell) {
			phase := float32(math.Mod(float64(view.T/shellPeriod+sh.Offset), 1))
			r := phase * shellMaxR
			t.Translation = scene.Tx.Pos
			t.Scale = m.Vec3{X: r, Y: r, Z: r}
			alpha := float32(0)
			if view.ShowWavefront {
				// Fade out with radius (energy spreads) and a soft fade-in at birth.
				alpha = (1 - phase) * 0.28 * smoothstep(phase, 0, 0.08)
			}
			*col = render3d.InstanceColor{R: 0.35, G: 0.65, B: 1.0, A: alpha}
		})
}

// updateHeatmap recolors the coverage quads from the latest field and hides them
// (drops them below the ground) when the heatmap is toggled off.
func updateHeatmap(c *app.Ctx, scene *Scene, cov *Coverage, view *View) {
	hasField := cov != nil && len(cov.Field) == scene.Grid.Len()
	app.Query3[render3d.Transform3D, render3d.InstanceColor, heatCell](c,
		func(_ ecs.Entity, t *render3d.Transform3D, col *render3d.InstanceColor, hc *heatCell) {
			if !view.ShowHeatmap || !hasField {
				t.Translation.Y = -100 // tuck under the ground, out of view
				return
			}
			p := scene.Grid.Cell(hc.Ix, hc.Iz)
			t.Translation = m.Vec3{X: p.X, Y: 0.05, Z: p.Z}
			pw := cov.Field[scene.Grid.Index(hc.Ix, hc.Iz)]
			r, g, b := heatColor(pw, cov.MinDBm, cov.MaxDBm)
			*col = render3d.InstanceColor{R: r, G: g, B: b, A: 1}
		})
}

// drawRays draws each contributing path as a polyline, colored by its amplitude
// (strong = warm, weak = cool), via the line gizmo.
func drawRays(c *app.Ctx, sim *Sim) {
	if len(sim.Paths) == 0 {
		return
	}
	maxAmp := 0.0
	for _, p := range sim.Paths {
		if a := cmplx.Abs(p.Amp); a > maxAmp {
			maxAmp = a
		}
	}
	if maxAmp <= 0 {
		return
	}
	for _, p := range sim.Paths {
		t := float32(cmplx.Abs(p.Amp) / maxAmp)
		r, g, b := rayColor(t)
		render3d.Polyline(c, p.Points, m.Vec4{X: r, Y: g, Z: b, W: 1})
	}
}

// --- color ramps ---

// heatColor maps a received power (W) to a blue→cyan→green→yellow→red ramp over
// the [minDBm, maxDBm] range.
func heatColor(powerW, minDBm, maxDBm float64) (r, g, b float32) {
	if powerW <= 0 {
		return 0, 0, 0.12
	}
	d := prop.DBm(powerW)
	t := (d - minDBm) / (maxDBm - minDBm)
	return ramp(float32(clampf64(t, 0, 1)))
}

// rayColor maps a normalized amplitude to cool(weak)→warm(strong).
func rayColor(t float32) (r, g, b float32) {
	// Blend blue → yellow → red as strength rises.
	if t < 0.5 {
		u := t / 0.5
		return u, u * 0.7, 1 - u // blue to yellowish
	}
	u := (t - 0.5) / 0.5
	return 1, 0.7 * (1 - u), 0 // yellow to red
}

// ramp is a 5-stop blue→cyan→green→yellow→red gradient.
func ramp(t float32) (r, g, b float32) {
	stops := [][3]float32{
		{0.05, 0.0, 0.4},  // deep blue
		{0.0, 0.6, 0.9},   // cyan
		{0.1, 0.8, 0.2},   // green
		{0.95, 0.9, 0.1},  // yellow
		{0.95, 0.15, 0.1}, // red
	}
	t = clampf32(t, 0, 1) * float32(len(stops)-1)
	i := int(t)
	if i >= len(stops)-1 {
		s := stops[len(stops)-1]
		return s[0], s[1], s[2]
	}
	f := t - float32(i)
	a, bb := stops[i], stops[i+1]
	return a[0] + (bb[0]-a[0])*f, a[1] + (bb[1]-a[1])*f, a[2] + (bb[2]-a[2])*f
}

func smoothstep(x, edge0, edge1 float32) float32 {
	t := clampf32((x-edge0)/(edge1-edge0), 0, 1)
	return t * t * (3 - 2*t)
}

func clampf32(v, lo, hi float32) float32 { return clamp(v, lo, hi) }

func clampf64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
