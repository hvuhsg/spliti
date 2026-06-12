package render3d

import (
	"math"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// Camera3D is the active view into the scene, a resource (one camera for this
// foundation). Position is the eye, Target the look-at point, Up the world up
// hint. FovYDeg is the vertical field of view in degrees; Near/Far are the clip
// distances. aspect (width/height) is maintained by the resize handler.
//
// Games drive the camera by mutating these fields (often each frame); the
// renderer rebuilds the view/projection from them in writeFrameUniforms.
type Camera3D struct {
	Position m.Vec3
	Target   m.Vec3
	Up       m.Vec3

	FovYDeg float32
	Near    float32
	Far     float32

	aspect float32
}

// View returns the world-to-camera matrix.
func (c *Camera3D) View() m.Mat4 {
	up := c.Up
	if up == (m.Vec3{}) {
		up = m.Vec3{X: 0, Y: 1, Z: 0}
	}
	return m.LookAt(c.Position, c.Target, up)
}

// Projection returns the camera-to-clip perspective matrix using the current
// aspect ratio.
func (c *Camera3D) Projection() m.Mat4 {
	aspect := c.aspect
	if aspect <= 0 {
		aspect = 1
	}
	fov := c.FovYDeg
	if fov <= 0 {
		fov = 60
	}
	near, far := c.Near, c.Far
	if near <= 0 {
		near = 0.1
	}
	if far <= near {
		far = near + 1000
	}
	return m.Perspective(m.DegToRad(fov), aspect, near, far)
}

// SetAspect overrides the projection aspect ratio (width/height). Normally the
// resize handler keeps it matched to the window; when the scene renders into an
// offscreen target (SetSceneTarget) the caller drives it from the target's size
// instead.
func (c *Camera3D) SetAspect(a float32) {
	if a > 0 {
		c.aspect = a
	}
}

// ScreenToRay unprojects a window pixel (px,py, origin at the top-left, y down)
// into a world-space ray. vw,vh are the viewport size in the same pixel units. It
// returns the ray origin on the near plane and a normalized direction pointing
// into the scene. Pure math (no GPU), so it is unit-testable.
//
// The aspect ratio used is the camera's current aspect (maintained by the resize
// handler), not vw/vh — they may differ if the caller passes window vs.
// framebuffer units, but the NDC mapping only needs vw/vh as the pixel extents.
func (c *Camera3D) ScreenToRay(px, py float64, vw, vh int) (origin, dir m.Vec3) {
	if vw <= 0 || vh <= 0 {
		return c.Position, m.Vec3{Z: -1}
	}
	ndcX := float32(px/float64(vw)*2 - 1)
	ndcY := float32(1 - py/float64(vh)*2) // flip: screen y is down, NDC y is up

	vp := c.Projection().Mul(c.View())
	inv, ok := vp.Inverse()
	if !ok {
		return c.Position, m.Vec3{Z: -1}
	}
	// WebGPU clip depth: near = 0, far = 1.
	near := unproject(inv, ndcX, ndcY, 0)
	far := unproject(inv, ndcX, ndcY, 1)
	return near, far.Sub(near).Normalize()
}

// unproject maps an NDC point (x,y,z, w=1) back to world space through the
// inverse view-projection, applying the perspective divide.
func unproject(invVP m.Mat4, x, y, z float32) m.Vec3 {
	p := invVP.MulVec4(m.Vec4{X: x, Y: y, Z: z, W: 1})
	if p.W == 0 {
		return p.XYZ()
	}
	inv := 1 / p.W
	return m.Vec3{X: p.X * inv, Y: p.Y * inv, Z: p.Z * inv}
}

// Orbit positions the camera on a sphere around center at the given distance,
// looking at center. yaw rotates around the world Y axis (radians, 0 looks
// down -Z toward center from +Z), pitch tilts up/down (radians, clamped to just
// under +/-90 degrees to avoid gimbal flip at the poles).
func (c *Camera3D) Orbit(center m.Vec3, distance, yaw, pitch float32) {
	const limit = math.Pi/2 - 0.01
	if pitch > limit {
		pitch = limit
	}
	if pitch < -limit {
		pitch = -limit
	}
	cp := float32(math.Cos(float64(pitch)))
	sp := float32(math.Sin(float64(pitch)))
	cy := float32(math.Cos(float64(yaw)))
	sy := float32(math.Sin(float64(yaw)))
	offset := m.Vec3{
		X: distance * cp * sy,
		Y: distance * sp,
		Z: distance * cp * cy,
	}
	c.Position = center.Add(offset)
	c.Target = center
	c.Up = m.Vec3{X: 0, Y: 1, Z: 0}
}

// defaultCamera builds the Camera3D resource from the plugin config, filling in
// sensible defaults and the initial aspect ratio.
func defaultCamera(p Plugin, aspect float32) *Camera3D {
	c := &Camera3D{
		Position: p.Position,
		Target:   p.Target,
		Up:       p.Up,
		FovYDeg:  p.FovYDeg,
		Near:     p.Near,
		Far:      p.Far,
		aspect:   aspect,
	}
	if c.Up == (m.Vec3{}) {
		c.Up = m.Vec3{X: 0, Y: 1, Z: 0}
	}
	if c.Position == (m.Vec3{}) && c.Target == (m.Vec3{}) {
		c.Position = m.Vec3{X: 0, Y: 2, Z: 6}
	}
	if c.FovYDeg <= 0 {
		c.FovYDeg = 60
	}
	if c.Near <= 0 {
		c.Near = 0.1
	}
	if c.Far <= c.Near {
		c.Far = 1000
	}
	return c
}
