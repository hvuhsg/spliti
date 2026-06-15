package systems

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
	splititime "github.com/hvuhsg/spliti/plugin/time"
)

// CameraControl drives an orbit camera around a movable focus point:
//
//   - right-drag  : orbit (look around)
//   - middle-drag : pan the focus across the board
//   - scroll wheel: zoom in/out
//
// The keyboard mirrors all three (A/D and W/S orbit, arrows pan, Q/E zoom) so
// the camera is usable without a multi-button mouse.
func CameraControl(c *app.Ctx) {
	g := app.GetResource[Game](c)
	cam := app.GetResource[render3d.Camera3D](c)
	if g == nil || cam == nil {
		return
	}
	dt := float32(app.GetResource[splititime.Time](c).Delta().Seconds())

	cx, cy := render3d.CursorPos(c)
	dx, dy := cx-g.lastCurX, cy-g.lastCurY

	// Right-drag orbits. Only apply the delta on frames where the button was
	// already held, so the first frame of a drag doesn't jump.
	if rDown := render3d.MouseButtonDown(c, inputs.MouseButtonRight); rDown {
		if g.orbiting {
			g.Yaw += float32(dx) * 0.01
			g.Pitch += float32(dy) * 0.01
		}
		g.orbiting = true
	} else {
		g.orbiting = false
	}

	// Middle-drag pans the focus point across the ground, grabbing the world so
	// it follows the cursor. Pan speed scales with zoom so it feels constant.
	if mDown := render3d.MouseButtonDown(c, inputs.MouseButtonMiddle); mDown {
		if g.panning {
			g.panBy(-dx*0.0016*float64(g.Dist), -dy*0.0016*float64(g.Dist))
		}
		g.panning = true
	} else {
		g.panning = false
	}

	g.lastCurX, g.lastCurY = cx, cy

	// Scroll wheel zooms (positive y = scroll up = zoom in).
	if _, sy := render3d.ScrollDelta(c); sy != 0 {
		g.Dist -= float32(sy) * 1.4
	}

	// Keyboard fallbacks.
	const rotSpeed, zoomSpeed, panSpeed = 1.8, 14.0, 9.0
	if render3d.KeyDown(c, inputs.KeyA) {
		g.Yaw -= rotSpeed * dt
	}
	if render3d.KeyDown(c, inputs.KeyD) {
		g.Yaw += rotSpeed * dt
	}
	if render3d.KeyDown(c, inputs.KeyW) {
		g.Pitch += rotSpeed * dt
	}
	if render3d.KeyDown(c, inputs.KeyS) {
		g.Pitch -= rotSpeed * dt
	}
	if render3d.KeyDown(c, inputs.KeyQ) {
		g.Dist -= zoomSpeed * dt
	}
	if render3d.KeyDown(c, inputs.KeyE) {
		g.Dist += zoomSpeed * dt
	}
	if render3d.KeyDown(c, inputs.KeyLeft) {
		g.panBy(float64(-panSpeed*dt), 0)
	}
	if render3d.KeyDown(c, inputs.KeyRight) {
		g.panBy(float64(panSpeed*dt), 0)
	}
	if render3d.KeyDown(c, inputs.KeyUp) {
		g.panBy(0, float64(-panSpeed*dt))
	}
	if render3d.KeyDown(c, inputs.KeyDown) {
		g.panBy(0, float64(panSpeed*dt))
	}

	g.Pitch = clamp(g.Pitch, 0.25, 1.45)
	g.Dist = clamp(g.Dist, 6, 36)

	cam.Orbit(g.Center, g.Dist, g.Yaw, g.Pitch)
}

// panBy slides the focus point across the XZ ground plane in screen-relative
// directions (right amount r, forward amount f), then clamps it to roughly the
// board extent so the camera can't wander off into empty space.
func (g *Game) panBy(r, f float64) {
	yaw := float64(g.Yaw)
	sin, cos := math.Sin(yaw), math.Cos(yaw)
	// Camera-relative ground axes derived from yaw (see Camera3D.Orbit).
	g.Center.X += float32(r*cos + f*sin)
	g.Center.Z += float32(-r*sin + f*cos)
	const lim = 9
	g.Center.X = clamp(g.Center.X, -lim, lim)
	g.Center.Z = clamp(g.Center.Z, -lim, lim)
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
