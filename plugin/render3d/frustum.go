package render3d

import (
	"math"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// plane is a frustum plane in the form n·p + d = 0, with the normal pointing
// toward the inside of the frustum (so a point is inside the plane when
// n·p + d >= 0).
type plane struct {
	nx, ny, nz, d float32
}

// frustum is the six clipping planes of a view-projection, extracted from the
// combined matrix. Fixed size, so it is cheap to build per frame with no
// allocation.
type frustum [6]plane

// extractFrustum derives the six frustum planes from a column-major
// view-projection matrix using the Gribb-Hartmann method, adapted for WebGPU's
// [0,1] clip-space depth (so the near plane is row2 and the far plane is
// row3 - row2, rather than the OpenGL row3 ± row2 pair). Each plane is
// normalized so sphereInside can compare signed distances directly.
//
// vp is indexed column-major: vp[c*4+r]. Row r is (vp[0*4+r], vp[1*4+r],
// vp[2*4+r], vp[3*4+r]).
func extractFrustum(vp m.Mat4) frustum {
	row := func(r int) (float32, float32, float32, float32) {
		return vp[0*4+r], vp[1*4+r], vp[2*4+r], vp[3*4+r]
	}
	x0, x1, x2, x3 := row(0)
	y0, y1, y2, y3 := row(1)
	z0, z1, z2, z3 := row(2)
	w0, w1, w2, w3 := row(3)

	var f frustum
	f[0] = normalizePlane(w0+x0, w1+x1, w2+x2, w3+x3) // left:   w + x
	f[1] = normalizePlane(w0-x0, w1-x1, w2-x2, w3-x3) // right:  w - x
	f[2] = normalizePlane(w0+y0, w1+y1, w2+y2, w3+y3) // bottom: w + y
	f[3] = normalizePlane(w0-y0, w1-y1, w2-y2, w3-y3) // top:    w - y
	f[4] = normalizePlane(z0, z1, z2, z3)             // near:   z       (z >= 0)
	f[5] = normalizePlane(w0-z0, w1-z1, w2-z2, w3-z3) // far:    w - z   (z <= w)
	return f
}

func normalizePlane(a, b, c, d float32) plane {
	inv := float32(math.Sqrt(float64(a*a + b*b + c*c)))
	if inv > 0 {
		inv = 1 / inv
	}
	return plane{nx: a * inv, ny: b * inv, nz: c * inv, d: d * inv}
}

// sphereInside reports whether a world-space sphere intersects or lies inside
// the frustum. It is conservative: a sphere straddling a plane counts as inside,
// so nothing visible is wrongly culled.
func (f frustum) sphereInside(center m.Vec3, radius float32) bool {
	for _, p := range f {
		dist := p.nx*center.X + p.ny*center.Y + p.nz*center.Z + p.d
		if dist < -radius {
			return false
		}
	}
	return true
}

// transformPoint applies a column-major affine matrix to a point.
func transformPoint(mat m.Mat4, p m.Vec3) m.Vec3 {
	v := mat.MulVec4(m.Vec4{X: p.X, Y: p.Y, Z: p.Z, W: 1})
	return m.Vec3{X: v.X, Y: v.Y, Z: v.Z}
}

// maxAxisScale returns the largest per-axis scale factor of a model matrix (the
// max length of its three basis columns), used to scale a model-space bounding
// radius into world space conservatively under non-uniform scale.
func maxAxisScale(mat m.Mat4) float32 {
	c0 := m.Vec3{X: mat[0], Y: mat[1], Z: mat[2]}.Length()
	c1 := m.Vec3{X: mat[4], Y: mat[5], Z: mat[6]}.Length()
	c2 := m.Vec3{X: mat[8], Y: mat[9], Z: mat[10]}.Length()
	return max(c0, max(c1, c2))
}
