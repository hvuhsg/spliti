package editor

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/scene"
	"github.com/mlange-42/arche/generic"
)

// nameMap returns the scene.Name accessor for the world.
func nameMap(c *app.Ctx) generic.Map[scene.Name] {
	return generic.NewMap[scene.Name](c.World())
}

// decomposeTRS splits a column-major TRS matrix into translation, rotation,
// and scale. Assumes no shear/negative scale (true of gizmo-produced
// matrices).
func decomposeTRS(mat m.Mat4) (m.Vec3, m.Quat, m.Vec3) {
	t := m.Vec3{X: mat[12], Y: mat[13], Z: mat[14]}
	cx := m.Vec3{X: mat[0], Y: mat[1], Z: mat[2]}
	cy := m.Vec3{X: mat[4], Y: mat[5], Z: mat[6]}
	cz := m.Vec3{X: mat[8], Y: mat[9], Z: mat[10]}
	s := m.Vec3{X: cx.Length(), Y: cy.Length(), Z: cz.Length()}
	if s.X == 0 || s.Y == 0 || s.Z == 0 {
		return t, m.IdentityQuat(), m.Vec3{X: 1, Y: 1, Z: 1}
	}
	r := quatFromAxes(cx.Scale(1/s.X), cy.Scale(1/s.Y), cz.Scale(1/s.Z))
	return t, r, s
}

// quatFromAxes builds a quaternion from an orthonormal rotation basis (the
// classic Shepperd / trace method).
func quatFromAxes(x, y, z m.Vec3) m.Quat {
	m00, m10, m20 := x.X, x.Y, x.Z
	m01, m11, m21 := y.X, y.Y, y.Z
	m02, m12, m22 := z.X, z.Y, z.Z

	trace := m00 + m11 + m22
	var q m.Quat
	switch {
	case trace > 0:
		s := float32(math.Sqrt(float64(trace+1))) * 2
		q = m.Quat{W: s / 4, X: (m21 - m12) / s, Y: (m02 - m20) / s, Z: (m10 - m01) / s}
	case m00 > m11 && m00 > m22:
		s := float32(math.Sqrt(float64(1+m00-m11-m22))) * 2
		q = m.Quat{W: (m21 - m12) / s, X: s / 4, Y: (m01 + m10) / s, Z: (m02 + m20) / s}
	case m11 > m22:
		s := float32(math.Sqrt(float64(1+m11-m00-m22))) * 2
		q = m.Quat{W: (m02 - m20) / s, X: (m01 + m10) / s, Y: s / 4, Z: (m12 + m21) / s}
	default:
		s := float32(math.Sqrt(float64(1+m22-m00-m11))) * 2
		q = m.Quat{W: (m10 - m01) / s, X: (m02 + m20) / s, Y: (m12 + m21) / s, Z: s / 4}
	}
	return q.Normalize()
}
