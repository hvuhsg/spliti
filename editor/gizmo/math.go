// Package gizmo is a hand-rolled 3D transform gizmo (translate / rotate /
// scale) drawn through an ImGui draw list. It replaces the bundled ImGuizmo
// binding, whose Manipulate corrupts the frame on this platform; the interface
// is deliberately Manipulate-shaped so swapping back stays cheap.
//
// The package splits into a pure-math core (this file — projection, rays,
// planes, matrix column surgery; unit-testable headless) and the interactive
// drawing layer in gizmo.go, which is the only part that touches ImGui.
package gizmo

import (
	"math"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// camera bundles the per-frame projection state: world<->screen mapping over
// the viewport rect, plus the camera basis in world space. Screen coordinates
// follow ImGui display space (pixels, origin top-left, y down); clip space is
// WebGPU's (NDC y up, depth 0..1) — the same conventions as render3d.Camera3D.
type camera struct {
	viewProj    m.Mat4
	invViewProj m.Mat4
	rectMin     m.Vec2
	rectSize    m.Vec2

	eye m.Vec3 // camera position, world
	fwd m.Vec3 // view direction (into the scene), world
}

func newCamera(view, proj m.Mat4, rectMin, rectSize m.Vec2) (camera, bool) {
	vp := proj.Mul(view)
	inv, ok := vp.Inverse()
	if !ok || rectSize.X <= 0 || rectSize.Y <= 0 {
		return camera{}, false
	}
	invView, ok := view.Inverse()
	if !ok {
		return camera{}, false
	}
	// Column-major: invView columns are the camera basis in world space.
	// Right-handed view space looks down -Z, so forward = -column 2.
	return camera{
		viewProj:    vp,
		invViewProj: inv,
		rectMin:     rectMin,
		rectSize:    rectSize,
		eye:         m.Vec3{X: invView[12], Y: invView[13], Z: invView[14]},
		fwd:         m.Vec3{X: -invView[8], Y: -invView[9], Z: -invView[10]}.Normalize(),
	}, true
}

// worldToScreen projects a world point into viewport pixels. ok is false when
// the point is at or behind the eye plane (w too small to divide by).
func (c camera) worldToScreen(p m.Vec3) (m.Vec2, bool) {
	v := c.viewProj.MulVec4(m.Vec4{X: p.X, Y: p.Y, Z: p.Z, W: 1})
	if v.W < 1e-6 {
		return m.Vec2{}, false
	}
	ndcX, ndcY := v.X/v.W, v.Y/v.W
	return m.Vec2{
		X: c.rectMin.X + (ndcX*0.5+0.5)*c.rectSize.X,
		Y: c.rectMin.Y + (0.5-ndcY*0.5)*c.rectSize.Y,
	}, true
}

// mouseRay unprojects a viewport pixel into a world ray (origin on the near
// plane, normalized direction into the scene).
func (c camera) mouseRay(px m.Vec2) (origin, dir m.Vec3) {
	ndcX := (px.X-c.rectMin.X)/c.rectSize.X*2 - 1
	ndcY := 1 - (px.Y-c.rectMin.Y)/c.rectSize.Y*2
	near := c.unproject(ndcX, ndcY, 0)
	far := c.unproject(ndcX, ndcY, 1)
	return near, far.Sub(near).Normalize()
}

func (c camera) unproject(x, y, z float32) m.Vec3 {
	p := c.invViewProj.MulVec4(m.Vec4{X: x, Y: y, Z: z, W: 1})
	if p.W == 0 {
		return m.Vec3{X: p.X, Y: p.Y, Z: p.Z}
	}
	inv := 1 / p.W
	return m.Vec3{X: p.X * inv, Y: p.Y * inv, Z: p.Z * inv}
}

// screenFactor returns the world-space length that spans gizmoPx pixels at the
// given world point, so the gizmo keeps a constant on-screen size.
func (c camera) screenFactor(origin m.Vec3, gizmoPx float32) float32 {
	// Measure how many pixels one world unit perpendicular to the view
	// direction covers at the origin's depth.
	right := m.Vec3{X: c.fwd.Y, Y: -c.fwd.X, Z: 0}
	if right.LengthSq() < 1e-6 {
		right = m.Vec3{X: 1}
	}
	right = right.Normalize()
	a, okA := c.worldToScreen(origin)
	b, okB := c.worldToScreen(origin.Add(right))
	if !okA || !okB {
		return 1
	}
	px := v2len(b.Sub(a))
	if px < 1e-3 {
		return 1
	}
	return gizmoPx / px
}

// intersectRayPlane returns the intersection of ray (ro,rd) with the plane
// through p0 with normal n. ok is false when the ray is parallel to the plane
// or the hit is behind the origin.
func intersectRayPlane(ro, rd, p0, n m.Vec3) (m.Vec3, bool) {
	denom := rd.Dot(n)
	if float32(math.Abs(float64(denom))) < 1e-6 {
		return m.Vec3{}, false
	}
	t := p0.Sub(ro).Dot(n) / denom
	if t < 0 {
		return m.Vec3{}, false
	}
	return ro.Add(rd.Scale(t)), true
}

// axisDragPlane returns the plane used while dragging along axis: it contains
// the axis and faces the camera as much as possible.
func axisDragPlane(origin, axis, eye m.Vec3) m.Vec3 {
	toCam := eye.Sub(origin)
	tmp := axis.Cross(toCam)
	if tmp.LengthSq() < 1e-9 {
		// Axis points straight at the camera; any plane containing it works.
		tmp = axis.Cross(m.Vec3{Y: 1})
		if tmp.LengthSq() < 1e-9 {
			tmp = axis.Cross(m.Vec3{X: 1})
		}
	}
	return tmp.Cross(axis).Normalize()
}

// planeBasis returns two unit vectors spanning the plane perpendicular to n.
func planeBasis(n m.Vec3) (u, v m.Vec3) {
	u = n.Cross(m.Vec3{Y: 1})
	if u.LengthSq() < 1e-6 {
		u = n.Cross(m.Vec3{X: 1})
	}
	u = u.Normalize()
	v = n.Cross(u).Normalize()
	return u, v
}

// signedAngleOnPlane measures the rotation from a to b around axis n (all unit
// vectors, a and b on the plane ⊥ n), in radians, right-hand rule.
func signedAngleOnPlane(a, b, n m.Vec3) float32 {
	return float32(math.Atan2(float64(a.Cross(b).Dot(n)), float64(a.Dot(b))))
}

// snapF rounds v to the nearest multiple of snap; snap <= 0 disables.
func snapF(v, snap float32) float32 {
	if snap <= 0 {
		return v
	}
	return float32(math.Round(float64(v/snap))) * snap
}

// --- matrix column surgery (column-major: element (r,c) at index c*4+r) ---

func matCol3(a m.Mat4, c int) m.Vec3 {
	return m.Vec3{X: a[c*4], Y: a[c*4+1], Z: a[c*4+2]}
}

func setMatCol3(a *m.Mat4, c int, v m.Vec3) {
	a[c*4], a[c*4+1], a[c*4+2] = v.X, v.Y, v.Z
}

func matTranslation(a m.Mat4) m.Vec3 { return matCol3(a, 3) }

func setMatTranslation(a *m.Mat4, t m.Vec3) { setMatCol3(a, 3, t) }

// rotateLinearPart rotates the 3x3 linear part of mat by the rotation (axis,
// angle), leaving translation untouched — i.e. spins the object in place
// around the world-space axis through its own origin.
func rotateLinearPart(mat m.Mat4, axis m.Vec3, angle float32) m.Mat4 {
	r := m.FromAxisAngle(axis, angle)
	out := mat
	for c := 0; c < 3; c++ {
		setMatCol3(&out, c, r.RotateVec3(matCol3(mat, c)))
	}
	return out
}

// --- small Vec2 helpers (m.Vec2 has no length/distance) ---

func v2len(v m.Vec2) float32 {
	return float32(math.Hypot(float64(v.X), float64(v.Y)))
}

func v2dist(a, b m.Vec2) float32 { return v2len(a.Sub(b)) }

// distPointSegment returns the distance from p to segment [a,b] in pixels.
func distPointSegment(p, a, b m.Vec2) float32 {
	ab := b.Sub(a)
	denom := ab.Dot(ab)
	if denom < 1e-9 {
		return v2dist(p, a)
	}
	t := p.Sub(a).Dot(ab) / denom
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return v2dist(p, a.Add(ab.Scale(t)))
}

// quadArea returns the absolute area of quad q0..q3 (shoelace formula).
func quadArea(q0, q1, q2, q3 m.Vec2) float32 {
	quad := [4]m.Vec2{q0, q1, q2, q3}
	var sum float32
	for i := range quad {
		a, b := quad[i], quad[(i+1)%4]
		sum += a.X*b.Y - b.X*a.Y
	}
	return float32(math.Abs(float64(sum))) / 2
}

// pointInQuad tests p against the convex quad q0..q3 (either winding). A
// degenerate (collinear) quad contains nothing — it fails closed so an
// edge-on plane handle can never swallow clicks.
func pointInQuad(p, q0, q1, q2, q3 m.Vec2) bool {
	if quadArea(q0, q1, q2, q3) < 1 {
		return false
	}
	quad := [4]m.Vec2{q0, q1, q2, q3}
	var sign float32
	for i := range quad {
		a, b := quad[i], quad[(i+1)%4]
		e := b.Sub(a)
		d := p.Sub(a)
		cross := e.X*d.Y - e.Y*d.X
		if math.Abs(float64(cross)) < 1e-3 {
			continue
		}
		s := float32(1)
		if cross < 0 {
			s = -1
		}
		if sign == 0 {
			sign = s
		} else if s != sign {
			return false
		}
	}
	return true
}
