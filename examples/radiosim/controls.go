package main

import (
	"math"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/mlange-42/arche/ecs"
)

// CamCtl is a fly camera plus the input state for looking and dragging.
type CamCtl struct {
	Yaw, Pitch float32 // radians; yaw 0 looks down -Z
	Pos        m.Vec3

	looking      bool    // right mouse held: mouse-look active
	dragging     bool    // left mouse held: receiver drag active
	lastX, lastY float64 // last cursor pos for look deltas
	haveLast     bool
}

const (
	moveSpeed = 22.0  // m/s
	lookSpeed = 0.005 // rad per pixel
	txSpeed   = 14.0  // m/s for arrow-key Tx motion
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

// controlsSystem drives the camera, receiver dragging, transmitter motion, and
// the view toggles each frame.
func controlsSystem(c *app.Ctx) {
	cam := app.GetResource[render3d.Camera3D](c)
	ctl := app.GetResource[CamCtl](c)
	scene := app.GetResource[Scene](c)
	view := app.GetResource[View](c)
	tm := app.GetResource[splititime.Time](c)
	win := render3d.Window(c)
	if cam == nil || ctl == nil || scene == nil || win == nil || tm == nil {
		return
	}
	dt := float32(tm.Delta().Seconds())

	handleButtons(c, ctl)
	handleLook(c, ctl)
	handleToggles(c, view)
	handleBandsWeather(c, scene)
	handleEngineToggle(c, scene)

	// Movement (held keys), relative to view direction but kept horizontal.
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

	// Transmitter motion with the arrow keys, recomputing coverage on change.
	moveTransmitter(c, scene, win, dt)

	// Update the receiver position while dragging.
	if ctl.dragging {
		dragReceiver(c, scene)
	}
}

// handleButtons updates the looking/dragging flags from mouse button events and
// kicks off a receiver drag on left-press.
func handleButtons(c *app.Ctx, ctl *CamCtl) {
	for _, ev := range app.ReadEvents[render3d.MouseButtonEvent](c) {
		switch ev.Button {
		case glfw.MouseButtonRight:
			ctl.looking = ev.Action == glfw.Press
			ctl.haveLast = false
		case glfw.MouseButtonLeft:
			ctl.dragging = ev.Action == glfw.Press
		}
	}
}

// handleLook accumulates yaw/pitch from cursor motion while the right button is
// held.
func handleLook(c *app.Ctx, ctl *CamCtl) {
	for _, ev := range app.ReadEvents[render3d.MouseMoveEvent](c) {
		if !ctl.looking {
			ctl.lastX, ctl.lastY = ev.X, ev.Y
			ctl.haveLast = true
			continue
		}
		if !ctl.haveLast {
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

// handleToggles flips the heatmap/ray/wavefront switches on key presses.
func handleToggles(c *app.Ctx, view *View) {
	for _, ev := range app.ReadEvents[render3d.KeyEvent](c) {
		if ev.Action != glfw.Press {
			continue
		}
		switch ev.Key {
		case glfw.KeyH:
			view.ShowHeatmap = !view.ShowHeatmap
		case glfw.KeyR:
			view.ShowRays = !view.ShowRays
		case glfw.KeySpace:
			view.ShowWavefront = !view.ShowWavefront
		}
	}
}

// handleBandsWeather switches the carrier between band presets (1: 2.4 GHz,
// 2: 28 GHz, 3: 60 GHz) and toggles rain (T), recomputing coverage on change so
// the band-dependent materials and atmospheric loss take effect immediately.
func handleBandsWeather(c *app.Ctx, scene *Scene) {
	for _, ev := range app.ReadEvents[render3d.KeyEvent](c) {
		if ev.Action != glfw.Press {
			continue
		}
		switch ev.Key {
		case glfw.Key1:
			scene.Tx.FreqHz = 2.4e9
			scene.recompute = true
		case glfw.Key2:
			scene.Tx.FreqHz = 28e9
			scene.recompute = true
		case glfw.Key3:
			scene.Tx.FreqHz = 60e9
			scene.recompute = true
		case glfw.KeyT:
			if scene.Sim.Weather.RainRateMMH > 0 {
				scene.Sim.Weather.RainRateMMH = 0
			} else {
				scene.Sim.Weather.RainRateMMH = 25 // mm/h, heavy rain
			}
			scene.recompute = true
		case glfw.KeyM:
			cycleWallMaterial(scene)
		}
	}
}

// wallMaterials is the palette the M key cycles every building wall through.
var wallMaterials = []sim.MaterialID{sim.MatConcrete, sim.MatBrick, sim.MatGlass, sim.MatWood}

// cycleWallMaterial repaints every building face (all faces except the ground at
// index 0) with the next material in the palette and recomputes coverage, so the
// effect of the wall material on reflection and penetration is visible live.
func cycleWallMaterial(scene *Scene) {
	next := sim.MatConcrete
	for i, mat := range wallMaterials {
		if mat == scene.WallMat {
			next = wallMaterials[(i+1)%len(wallMaterials)]
			break
		}
	}
	scene.WallMat = next
	for fi := 1; fi < len(scene.Sim.FaceMat); fi++ {
		scene.Sim.FaceMat[fi] = next
	}
	scene.recompute = true
}

// handleEngineToggle swaps the active propagation engine (G) between the exact
// image method and the real-time SBR engine, recomputing coverage so the change
// is visible. Both implement sim.Engine, so the rest of the app is unaffected.
func handleEngineToggle(c *app.Ctx, scene *Scene) {
	eng := app.GetResource[Engine](c)
	if eng == nil {
		return
	}
	for _, ev := range app.ReadEvents[render3d.KeyEvent](c) {
		if ev.Action != glfw.Press || ev.Key != glfw.KeyG {
			continue
		}
		if _, isSBR := eng.Engine.(sim.SBREngine); isSBR {
			eng.Engine = sim.ImageEngine{}
		} else {
			eng.Engine = sim.SBREngine{}
		}
		scene.recompute = true
	}
}

// moveTransmitter nudges the Tx in the XZ plane with the arrow keys and flags a
// coverage recompute when it actually moves.
func moveTransmitter(c *app.Ctx, scene *Scene, win *glfw.Window, dt float32) {
	var d m.Vec3
	if down(win, glfw.KeyUp) {
		d.X += 1
	}
	if down(win, glfw.KeyDown) {
		d.X -= 1
	}
	if down(win, glfw.KeyLeft) {
		d.Z -= 1
	}
	if down(win, glfw.KeyRight) {
		d.Z += 1
	}
	if d == (m.Vec3{}) {
		return
	}
	scene.Tx.Pos = scene.Tx.Pos.Add(d.Normalize().Scale(txSpeed * dt))
	scene.Tx.Pos.X = clamp(scene.Tx.Pos.X, -streetHalfX, streetHalfX)
	scene.Tx.Pos.Z = clamp(scene.Tx.Pos.Z, -streetHalfZ, streetHalfZ)
	scene.recompute = true

	// Move the Tx marker entity.
	app.Query2[render3d.Transform3D, txTag](c, func(_ ecs.Entity, t *render3d.Transform3D, _ *txTag) {
		t.Translation = scene.Tx.Pos
	})
}

// dragReceiver moves the receiver to where the cursor ray meets the receiver-
// height plane.
func dragReceiver(c *app.Ctx, scene *Scene) {
	win := render3d.Window(c)
	if win == nil {
		return
	}
	x, y := win.GetCursorPos()
	origin, dir := render3d.ScreenToRay(c, x, y)
	hit, ok := rayPlaneY(origin, dir, scene.RxHeight)
	if !ok {
		return
	}
	hit.X = clamp(hit.X, -streetHalfX, streetHalfX)
	hit.Z = clamp(hit.Z, -streetHalfZ, streetHalfZ)
	app.Query2[render3d.Transform3D, receiverTag](c, func(_ ecs.Entity, t *render3d.Transform3D, _ *receiverTag) {
		t.Translation = hit
	})
}

// coverageSystem re-evaluates the coverage field when requested (Tx moved), and
// fills the Coverage resource with the field and its dBm range for normalization.
// It drives the active engine through the sim.Engine seam.
func coverageSystem(c *app.Ctx) {
	scene := app.GetResource[Scene](c)
	cov := app.GetResource[Coverage](c)
	eng := app.GetResource[Engine](c)
	if scene == nil || cov == nil || eng == nil || !scene.recompute {
		return
	}
	scene.recompute = false

	field := eng.Coverage(scene.Tx, scene.Grid, scene.Sim, scene.Cfg)
	cov.Field = field

	minDBm, maxDBm := math.Inf(1), math.Inf(-1)
	for _, p := range field {
		if p <= 0 {
			continue
		}
		d := sim.DBm(p)
		if d < minDBm {
			minDBm = d
		}
		if d > maxDBm {
			maxDBm = d
		}
	}
	if math.IsInf(minDBm, 1) {
		minDBm, maxDBm = -120, -40
	}
	// Clamp the dynamic range so a few hot cells near the Tx don't wash out the
	// rest of the map.
	if maxDBm-minDBm < 1 {
		maxDBm = minDBm + 1
	}
	if maxDBm-minDBm > 60 {
		minDBm = maxDBm - 60
	}
	cov.MinDBm, cov.MaxDBm = minDBm, maxDBm
}

// --- small helpers ---

func down(win *glfw.Window, key glfw.Key) bool { return win.GetKey(key) == glfw.Press }

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
