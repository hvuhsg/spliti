package main

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/mlange-42/arche/ecs"
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
	if cam == nil || ctl == nil || lab == nil || tm == nil {
		return
	}
	dt := float32(tm.Delta().Seconds())

	handleButtons(c, ctl, lab)
	handleLook(c, ctl)
	handleConfigKeys(c, lab)

	// Camera movement (held keys), relative to view direction but kept horizontal.
	fwd := ctl.forward()
	flat := m.Vec3{X: fwd.X, Z: fwd.Z}.Normalize()
	right := flat.Cross(m.Vec3{Y: 1}).Normalize()
	var move m.Vec3
	if render3d.KeyDown(c, inputs.KeyW) {
		move = move.Add(flat)
	}
	if render3d.KeyDown(c, inputs.KeyS) {
		move = move.Sub(flat)
	}
	if render3d.KeyDown(c, inputs.KeyD) {
		move = move.Add(right)
	}
	if render3d.KeyDown(c, inputs.KeyA) {
		move = move.Sub(right)
	}
	if render3d.KeyDown(c, inputs.KeyE) {
		move = move.Add(m.Vec3{Y: 1})
	}
	if render3d.KeyDown(c, inputs.KeyQ) {
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
// raycasts to select a marker (or deselect on empty ground) and begins a drag.
func handleButtons(c *app.Ctx, ctl *CamCtl, lab *Lab) {
	txE, rxE := markerEntities(c)
	for _, ev := range app.ReadEvents[inputs.MouseButtonEvent](c) {
		switch ev.Button {
		case inputs.MouseButtonRight:
			ctl.looking = ev.Action == inputs.Press
			ctl.haveLast = false
		case inputs.MouseButtonLeft:
			if ev.Action == inputs.Release {
				ctl.dragging = false
				continue
			}
			origin, dir := render3d.ScreenToRay(c, ev.X, ev.Y)
			hit, ok := render3d.Raycast(c, origin, dir)
			switch {
			case ok && hit.Entity == txE:
				lab.Sel = SelTx
				ctl.dragging = true
			case ok && hit.Entity == rxE:
				lab.Sel = SelRx
				ctl.dragging = true
			default:
				lab.Sel = SelNone
				ctl.dragging = false
			}
		}
	}
}

// markerEntities returns the transmitter and receiver head entities.
func markerEntities(c *app.Ctx) (tx, rx ecs.Entity) {
	app.Query1[txTag](c, func(e ecs.Entity, _ *txTag) { tx = e })
	app.Query1[rxTag](c, func(e ecs.Entity, _ *rxTag) { rx = e })
	return tx, rx
}

// handleLook accumulates yaw/pitch from cursor motion while the right button is
// held.
func handleLook(c *app.Ctx, ctl *CamCtl) {
	for _, ev := range app.ReadEvents[inputs.MouseMoveEvent](c) {
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
	txd := txDevice(c)
	rxd := rxDevice(c)
	for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
		if ev.Action != inputs.Press && ev.Action != inputs.Repeat {
			continue
		}
		switch {
		case lab.Sel == SelTx && txd != nil:
			switch ev.Key {
			case inputs.KeyUp: // +1 dB
				txd.PowerW *= math.Pow(10, 0.1)
			case inputs.KeyDown: // -1 dB
				txd.PowerW *= math.Pow(10, -0.1)
			case inputs.KeyRight: // +2 MHz (shorter wavelength)
				txd.FreqHz += 2e6
			case inputs.KeyLeft: // -2 MHz (longer wavelength)
				txd.FreqHz -= 2e6
			}
			txd.PowerW = clampf(txd.PowerW, 1e-12, 1e-3) // -90 … 0 dBm
			txd.FreqHz = clampf(txd.FreqHz, 10e6, 45e6)  // 10–45 MHz (λ 6.7–30 m, visible)
		case lab.Sel == SelRx && rxd != nil:
			switch ev.Key {
			case inputs.KeyUp:
				rxd.GainDBi += 1
			case inputs.KeyDown:
				rxd.GainDBi -= 1
			case inputs.KeyRight:
				rxd.NoiseFigDB += 0.5
			case inputs.KeyLeft:
				rxd.NoiseFigDB -= 0.5
			}
			rxd.GainDBi = clampf(rxd.GainDBi, -10, 30)
			rxd.NoiseFigDB = clampf(rxd.NoiseFigDB, 0, 20)
		}
	}
}

// dragMarker moves the selected marker to where the cursor ray meets the marker-
// height plane, clamped to the ground, updating both its transform and Lab.
func dragMarker(c *app.Ctx, lab *Lab) {
	x, y := render3d.CursorPos(c)
	origin, dir := render3d.ScreenToRay(c, x, y)
	hit, ok := rayPlaneY(origin, dir, markerHeight)
	if !ok {
		return
	}
	hit.X = clamp(hit.X, -planeHalf, planeHalf)
	hit.Z = clamp(hit.Z, -planeHalf, planeHalf)
	hit.Y = markerHeight

	if lab.Sel == SelTx {
		lab.TxPos = hit
		setMarkerPos[txTag](c, hit)
	} else {
		lab.RxPos = hit
		setMarkerPos[rxTag](c, hit)
	}
}

// setMarkerPos writes the position into the transform of the entity carrying tag T.
func setMarkerPos[T any](c *app.Ctx, pos m.Vec3) {
	app.Query2[render3d.Transform3D, T](c, func(_ ecs.Entity, t *render3d.Transform3D, _ *T) {
		t.Translation = pos
	})
}

// applyHighlight swaps each marker's material to its "selected" variant when it is
// the current selection, and back to the base material otherwise.
func applyHighlight(c *app.Ctx, lab *Lab) {
	app.Query2[render3d.MaterialRef, txTag](c, func(_ ecs.Entity, mr *render3d.MaterialRef, _ *txTag) {
		mr.Material = pick(lab.Sel == SelTx, "tx_sel", "tx")
	})
	app.Query2[render3d.MaterialRef, rxTag](c, func(_ ecs.Entity, mr *render3d.MaterialRef, _ *rxTag) {
		mr.Material = pick(lab.Sel == SelRx, "rx_sel", "rx")
	})
}

// --- small helpers ---

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
