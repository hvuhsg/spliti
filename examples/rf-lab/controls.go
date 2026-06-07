package main

import (
	"math"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// CamCtl is a fly camera plus the input state for looking and dragging markers.
type CamCtl struct {
	Yaw, Pitch float32 // radians; yaw 0 looks down -Z
	Pos        m.Vec3

	looking      bool    // right mouse held: mouse-look active
	dragging     bool    // left mouse held on a marker: drag active
	lastX, lastY float64 // last cursor pos for look deltas
	haveLast     bool
}

const (
	moveSpeed = 30.0  // m/s
	lookSpeed = 0.005 // rad per pixel
)

// init places the camera at pos looking toward the scene origin.
func (ctl *CamCtl) init(cam *render3d.Camera3D, pos m.Vec3) {
	ctl.Pos = pos
	dir := m.Vec3{}.Sub(pos).Normalize()
	ctl.Pitch = float32(math.Asin(float64(clamp(dir.Y, -1, 1))))
	ctl.Yaw = float32(math.Atan2(float64(dir.X), float64(-dir.Z)))
	ctl.apply(cam)
}

// forward returns the unit view direction for the current yaw/pitch.
func (ctl *CamCtl) forward() m.Vec3 {
	cp := float32(math.Cos(float64(ctl.Pitch)))
	sp := float32(math.Sin(float64(ctl.Pitch)))
	sy := float32(math.Sin(float64(ctl.Yaw)))
	cy := float32(math.Cos(float64(ctl.Yaw)))
	return m.Vec3{X: cp * sy, Y: sp, Z: -cp * cy}
}

func (ctl *CamCtl) apply(cam *render3d.Camera3D) {
	cam.Position = ctl.Pos
	cam.Target = ctl.Pos.Add(ctl.forward())
	cam.Up = m.Vec3{Y: 1}
}

// controlsSystem drives the camera, marker selection/dragging, parameter editing,
// and the selection highlight each frame.
func controlsSystem(c *app.Ctx) {
	// In graph mode the editor owns input; the 3D controls go inert.
	if ui := app.GetResource[UI](c); ui != nil && ui.Mode != ModeExplore {
		return
	}
	cam := app.GetResource[render3d.Camera3D](c)
	ctl := app.GetResource[CamCtl](c)
	lab := app.GetResource[Lab](c)
	tm := app.GetResource[splititime.Time](c)
	win := render3d.Window(c)
	if cam == nil || ctl == nil || lab == nil || win == nil || tm == nil {
		return
	}
	dt := float32(tm.Delta().Seconds())

	handleButtons(c, ctl, lab)
	handleLook(c, ctl)
	handleConfigKeys(c, lab)
	handleDeviceKeys(c, lab)

	// Camera movement (held keys), relative to view direction but kept horizontal.
	fwd := ctl.forward()
	flat := m.Vec3{X: fwd.X, Z: fwd.Z}.Normalize()
	right := flat.Cross(m.Vec3{Y: 1}).Normalize()
	var move m.Vec3
	if down(win, glfw.KeyW) {
		move = move.Add(flat)
	}
	if down(win, glfw.KeyS) {
		move = move.Sub(flat)
	}
	if down(win, glfw.KeyD) {
		move = move.Add(right)
	}
	if down(win, glfw.KeyA) {
		move = move.Sub(right)
	}
	if down(win, glfw.KeyE) {
		move = move.Add(m.Vec3{Y: 1})
	}
	if down(win, glfw.KeyQ) {
		move = move.Sub(m.Vec3{Y: 1})
	}
	if move != (m.Vec3{}) {
		ctl.Pos = ctl.Pos.Add(move.Normalize().Scale(moveSpeed * dt))
	}
	ctl.apply(cam)

	if ctl.dragging && lab.Sel != SelNone {
		dragMarker(c, lab)
	}
	applyHighlight(c, lab)
}

// handleButtons resolves clicks: right button toggles mouse-look; left-press
// raycasts to select whichever marker was hit (or deselect on empty ground) and
// begins a drag.
func handleButtons(c *app.Ctx, ctl *CamCtl, lab *Lab) {
	txMap := generic.NewMap[txTag](c.World())
	rxMap := generic.NewMap[rxTag](c.World())
	for _, ev := range app.ReadEvents[render3d.MouseButtonEvent](c) {
		switch ev.Button {
		case glfw.MouseButtonRight:
			ctl.looking = ev.Action == glfw.Press
			ctl.haveLast = false
		case glfw.MouseButtonLeft:
			if ev.Action == glfw.Release {
				ctl.dragging = false
				continue
			}
			origin, dir := render3d.ScreenToRay(c, ev.X, ev.Y)
			hit, ok := render3d.Raycast(c, origin, dir)
			switch {
			case ok && txMap.Has(hit.Entity):
				lab.Sel, lab.Ent, ctl.dragging = SelTx, hit.Entity, true
			case ok && rxMap.Has(hit.Entity):
				lab.Sel, lab.Ent, ctl.dragging = SelRx, hit.Entity, true
			default:
				lab.Sel, lab.Ent, ctl.dragging = SelNone, ecs.Entity{}, false
			}
		}
	}
}

// handleDeviceKeys adds and removes devices: T spawns a transmitter, R spawns a
// receiver (the new one is selected), and Delete/Backspace removes the current
// selection along with its mast.
func handleDeviceKeys(c *app.Ctx, lab *Lab) {
	for _, ev := range app.ReadEvents[render3d.KeyEvent](c) {
		if ev.Action != glfw.Press {
			continue
		}
		switch ev.Key {
		case glfw.KeyT:
			spawnTx(c.Commands(), spawnSpot[txTag](c, -30), lab, true)
		case glfw.KeyR:
			spawnRx(c.Commands(), spawnSpot[rxTag](c, 30), lab, true)
		case glfw.KeyDelete, glfw.KeyBackspace:
			removeSelected(c, lab)
		}
	}
}

// spawnSpot picks a ground position for a newly added marker of tag T: fixed in
// X, staggered in Z by how many of that tag already exist so adds don't stack.
func spawnSpot[T any](c *app.Ctx, x float32) m.Vec3 {
	n := 0
	app.Query1[T](c, func(ecs.Entity, *T) { n++ })
	z := clamp(float32(n)*12-24, -planeHalf, planeHalf)
	return m.Vec3{X: x, Y: markerHeight, Z: z}
}

// removeSelected despawns the selected marker and its mast, then clears the
// selection.
func removeSelected(c *app.Ctx, lab *Lab) {
	if lab.Sel == SelNone || lab.Ent.IsZero() {
		return
	}
	ent := lab.Ent
	cmd := c.Commands()
	// Despawn the mast(s) parented to this marker so they don't dangle.
	app.Query1[render3d.Parent](c, func(e ecs.Entity, p *render3d.Parent) {
		if p.Entity == ent {
			cmd.Despawn(e)
		}
	})
	cmd.Despawn(ent)
	lab.Sel, lab.Ent = SelNone, ecs.Entity{}
}

// handleLook accumulates yaw/pitch from cursor motion while the right button is
// held.
func handleLook(c *app.Ctx, ctl *CamCtl) {
	for _, ev := range app.ReadEvents[render3d.MouseMoveEvent](c) {
		if !ctl.looking || !ctl.haveLast {
			ctl.lastX, ctl.lastY = ev.X, ev.Y
			ctl.haveLast = true
			continue
		}
		dx := float32(ev.X - ctl.lastX)
		dy := float32(ev.Y - ctl.lastY)
		ctl.lastX, ctl.lastY = ev.X, ev.Y
		ctl.Yaw += dx * lookSpeed
		ctl.Pitch -= dy * lookSpeed
		const lim = math.Pi/2 - 0.02
		ctl.Pitch = clamp(ctl.Pitch, -lim, lim)
	}
}

// handleConfigKeys edits the selected marker's parameters on arrow-key presses
// (and repeats so holding a key ramps the value). The transmitter's power and
// frequency, or the receiver's antenna gain and noise figure.
func handleConfigKeys(c *app.Ctx, lab *Lab) {
	if lab.Sel == SelNone {
		return
	}
	txd := selectedTx(c, lab)
	rxd := selectedRx(c, lab)
	for _, ev := range app.ReadEvents[render3d.KeyEvent](c) {
		if ev.Action != glfw.Press && ev.Action != glfw.Repeat {
			continue
		}
		switch {
		case lab.Sel == SelTx && txd != nil:
			switch ev.Key {
			case glfw.KeyUp: // +1 dB
				txd.PowerW *= math.Pow(10, 0.1)
			case glfw.KeyDown: // -1 dB
				txd.PowerW *= math.Pow(10, -0.1)
			case glfw.KeyRight: // +2 MHz (shorter wavelength)
				txd.FreqHz += 2e6
			case glfw.KeyLeft: // -2 MHz (longer wavelength)
				txd.FreqHz -= 2e6
			}
			txd.PowerW = clampf(txd.PowerW, 1e-12, 1e-3) // -90 … 0 dBm
			txd.FreqHz = clampf(txd.FreqHz, 10e6, 45e6)  // 10–45 MHz (λ 6.7–30 m, visible)
		case lab.Sel == SelRx && rxd != nil:
			switch ev.Key {
			case glfw.KeyUp:
				rxd.GainDBi += 1
			case glfw.KeyDown:
				rxd.GainDBi -= 1
			case glfw.KeyRight: // tune up (shorter wavelength)
				rxd.TuneHz += 2e6
			case glfw.KeyLeft: // tune down (longer wavelength)
				rxd.TuneHz -= 2e6
			case glfw.KeyRightBracket: // ] louder noise floor
				rxd.NoiseFigDB += 0.5
			case glfw.KeyLeftBracket: // [ quieter noise floor
				rxd.NoiseFigDB -= 0.5
			}
			rxd.GainDBi = clampf(rxd.GainDBi, -10, 30)
			rxd.TuneHz = clampf(rxd.TuneHz, 10e6, 45e6) // same span as the transmitters
			rxd.NoiseFigDB = clampf(rxd.NoiseFigDB, 0, 20)
		}
	}
}

// dragMarker moves the selected marker to where the cursor ray meets the marker-
// height plane, clamped to the ground, by writing its transform.
func dragMarker(c *app.Ctx, lab *Lab) {
	win := render3d.Window(c)
	if win == nil || lab.Ent.IsZero() {
		return
	}
	x, y := win.GetCursorPos()
	origin, dir := render3d.ScreenToRay(c, x, y)
	hit, ok := rayPlaneY(origin, dir, markerHeight)
	if !ok {
		return
	}
	hit.X = clamp(hit.X, -planeHalf, planeHalf)
	hit.Z = clamp(hit.Z, -planeHalf, planeHalf)
	hit.Y = markerHeight

	tmap := generic.NewMap[render3d.Transform3D](c.World())
	if tmap.Has(lab.Ent) {
		tmap.Get(lab.Ent).Translation = hit
	}
}

// applyHighlight swaps each marker's material to its "selected" variant for the
// one selected entity, and back to the base material for every other marker.
func applyHighlight(c *app.Ctx, lab *Lab) {
	app.Query2[render3d.MaterialRef, txTag](c, func(e ecs.Entity, mr *render3d.MaterialRef, _ *txTag) {
		mr.Material = pick(lab.Sel == SelTx && e == lab.Ent, "tx_sel", "tx")
	})
	app.Query2[render3d.MaterialRef, rxTag](c, func(e ecs.Entity, mr *render3d.MaterialRef, _ *rxTag) {
		mr.Material = pick(lab.Sel == SelRx && e == lab.Ent, "rx_sel", "rx")
	})
}

// --- small helpers ---

func down(win *glfw.Window, key glfw.Key) bool { return win.GetKey(key) == glfw.Press }

func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func rayPlaneY(origin, dir m.Vec3, y float32) (m.Vec3, bool) {
	if dir.Y > -1e-6 && dir.Y < 1e-6 {
		return m.Vec3{}, false
	}
	t := (y - origin.Y) / dir.Y
	if t < 0 {
		return m.Vec3{}, false
	}
	return origin.Add(dir.Scale(t)), true
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampf(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
