package render3d

import "github.com/hvuhsg/spliti/plugin/render3d/m"

// computeTangents fills every vertex's tangent (TX..TW) from the per-triangle
// gradient of position with respect to UV (Lengyel's method), accumulated per
// vertex and then Gram-Schmidt-orthogonalized against the normal. The handedness
// sign stored in .w reconstructs the bitangent as cross(normal, tangent) * w.
//
// It is used by the glTF loader when a primitive provides no TANGENT attribute
// (the primitive generators emit analytic tangents and don't need it). Triangles
// with degenerate UVs contribute nothing; a vertex that ends up with no usable
// tangent falls back to an arbitrary basis perpendicular to its normal so the
// TBN is always well-formed.
func computeTangents(mesh *Mesh) {
	if mesh == nil || len(mesh.Vertices) == 0 {
		return
	}
	tan := make([]m.Vec3, len(mesh.Vertices))   // accumulated tangent (dP/du)
	bitan := make([]m.Vec3, len(mesh.Vertices)) // accumulated bitangent (dP/dv)

	tri := func(i0, i1, i2 uint32) {
		v0, v1, v2 := mesh.Vertices[i0], mesh.Vertices[i1], mesh.Vertices[i2]
		p0 := m.Vec3{X: v0.PX, Y: v0.PY, Z: v0.PZ}
		p1 := m.Vec3{X: v1.PX, Y: v1.PY, Z: v1.PZ}
		p2 := m.Vec3{X: v2.PX, Y: v2.PY, Z: v2.PZ}
		e1 := p1.Sub(p0)
		e2 := p2.Sub(p0)
		du1, dv1 := v1.U-v0.U, v1.V-v0.V
		du2, dv2 := v2.U-v0.U, v2.V-v0.V
		det := du1*dv2 - du2*dv1
		if det == 0 {
			return // degenerate UVs: no gradient
		}
		r := 1.0 / det
		t := e1.Scale(dv2 * r).Sub(e2.Scale(dv1 * r)) // dP/du
		b := e2.Scale(du1 * r).Sub(e1.Scale(du2 * r)) // dP/dv
		for _, idx := range [3]uint32{i0, i1, i2} {
			tan[idx] = tan[idx].Add(t)
			bitan[idx] = bitan[idx].Add(b)
		}
	}
	for i := 0; i+2 < len(mesh.Indices); i += 3 {
		tri(mesh.Indices[i], mesh.Indices[i+1], mesh.Indices[i+2])
	}

	for i := range mesh.Vertices {
		n := m.Vec3{X: mesh.Vertices[i].NX, Y: mesh.Vertices[i].NY, Z: mesh.Vertices[i].NZ}
		t := tan[i]
		// Gram-Schmidt: remove the normal component from the accumulated tangent.
		t = t.Sub(n.Scale(n.Dot(t)))
		if t.LengthSq() < 1e-12 {
			t = perpendicular(n) // degenerate: pick any tangent in the surface plane
		} else {
			t = t.Normalize()
		}
		// Handedness: +1 if the accumulated bitangent agrees with cross(n, t).
		w := float32(1)
		if n.Cross(t).Dot(bitan[i]) < 0 {
			w = -1
		}
		mesh.Vertices[i].TX, mesh.Vertices[i].TY, mesh.Vertices[i].TZ, mesh.Vertices[i].TW = t.X, t.Y, t.Z, w
	}
}

// perpendicular returns an arbitrary unit vector orthogonal to n (assumed
// roughly unit-length). It picks the axis least aligned with n to stay stable.
func perpendicular(n m.Vec3) m.Vec3 {
	axis := m.Vec3{X: 1}
	if abs(n.X) > 0.9 {
		axis = m.Vec3{Y: 1}
	}
	t := axis.Sub(n.Scale(n.Dot(axis)))
	if t.LengthSq() < 1e-12 {
		return m.Vec3{X: 1}
	}
	return t.Normalize()
}

func abs(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
