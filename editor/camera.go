package editor

import (
	"math"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/generic"
)

// cameraRig is the editor's orbit camera: a pivot point, a distance, and a
// yaw/pitch pair. Right-drag orbits, middle-drag pans the pivot in the view
// plane, the wheel dollies, WASD (while right button is held) flies the pivot,
// and F frames the selection.
type cameraRig struct {
	pivot      m.Vec3
	dist       float32
	yaw, pitch float32
}

func defaultRig() cameraRig {
	return cameraRig{dist: 10, yaw: 0.6, pitch: 0.5}
}

// apply writes the rig into the render3d camera each frame (pre-render).
func (r *cameraRig) apply(c *app.Ctx) {
	cam := app.GetResource[render3d.Camera3D](c)
	if cam == nil {
		return
	}
	cam.Orbit(r.pivot, r.dist, r.yaw, r.pitch)
}

// handleInput processes viewport navigation. Called from the Scene panel only
// while the cursor is over the viewport image, except for ongoing drags.
func (r *cameraRig) handleInput(hovered bool) {
	io := imgui.CurrentIO()
	rmb := imgui.IsMouseDown(imgui.MouseButtonRight)

	if hovered || rmb {
		if imgui.IsMouseDraggingV(imgui.MouseButtonRight, 0) {
			d := io.MouseDelta()
			r.yaw -= d.X * 0.008
			r.pitch += d.Y * 0.008
			r.pitch = clampF(r.pitch, -1.55, 1.55)
		}
		if imgui.IsMouseDraggingV(imgui.MouseButtonMiddle, 0) {
			d := io.MouseDelta()
			right, up := r.viewBasis()
			s := r.dist * 0.0016
			r.pivot = r.pivot.Add(right.Scale(-d.X * s)).Add(up.Scale(d.Y * s))
		}
	}
	if hovered {
		if wheel := io.MouseWheel(); wheel != 0 {
			r.dist *= 1 - wheel*0.1
			r.dist = clampF(r.dist, 0.5, 200)
		}
	}
	// Fly: WASD(+QE for down/up) moves the pivot while the right button is
	// held, scaled by distance so it feels constant at any zoom.
	if rmb && !io.WantCaptureKeyboard() {
		fwd, right := r.flyBasis()
		s := r.dist * 0.02
		if imgui.IsKeyDown(imgui.KeyLeftShift) {
			s *= 3
		}
		move := m.Vec3{}
		if imgui.IsKeyDown(imgui.KeyW) {
			move = move.Add(fwd.Scale(s))
		}
		if imgui.IsKeyDown(imgui.KeyS) {
			move = move.Add(fwd.Scale(-s))
		}
		if imgui.IsKeyDown(imgui.KeyD) {
			move = move.Add(right.Scale(s))
		}
		if imgui.IsKeyDown(imgui.KeyA) {
			move = move.Add(right.Scale(-s))
		}
		if imgui.IsKeyDown(imgui.KeyE) {
			move.Y += s
		}
		if imgui.IsKeyDown(imgui.KeyQ) {
			move.Y -= s
		}
		r.pivot = r.pivot.Add(move)
	}
}

// viewBasis returns the camera's right and up vectors in world space.
func (r *cameraRig) viewBasis() (right, up m.Vec3) {
	fwd := r.forward()
	right = fwd.Cross(m.Vec3{Y: 1}).Normalize()
	up = right.Cross(fwd).Normalize()
	return right, up
}

// flyBasis returns the ground-projected forward and right vectors for WASD.
func (r *cameraRig) flyBasis() (fwd, right m.Vec3) {
	f := r.forward()
	f.Y = 0
	if f.LengthSq() < 1e-6 {
		f = m.Vec3{Z: -1}
	}
	fwd = f.Normalize()
	right = fwd.Cross(m.Vec3{Y: 1}).Normalize().Scale(-1)
	return fwd, right
}

// forward is the unit vector from the eye toward the pivot, mirroring
// Camera3D.Orbit's spherical convention.
func (r *cameraRig) forward() m.Vec3 {
	cp := float32(math.Cos(float64(r.pitch)))
	eyeOffset := m.Vec3{
		X: cp * float32(math.Sin(float64(r.yaw))),
		Y: float32(math.Sin(float64(r.pitch))),
		Z: cp * float32(math.Cos(float64(r.yaw))),
	}
	return eyeOffset.Scale(-1).Normalize()
}

// focusSelection re-pivots onto the selected entity and pulls in close.
func (r *cameraRig) focusSelection(c *app.Ctx, st *state) {
	if !st.hasSelected || !c.World().Alive(st.selected) {
		return
	}
	gt := generic.NewMap[render3d.GlobalTransform](c.World())
	if !gt.Has(st.selected) {
		return
	}
	mat := gt.Get(st.selected).Matrix
	r.pivot = m.Vec3{X: mat[12], Y: mat[13], Z: mat[14]}
	if r.dist > 12 {
		r.dist = 12
	}
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
