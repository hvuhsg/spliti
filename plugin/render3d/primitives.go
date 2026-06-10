package render3d

import (
	"math"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// Primitive generators produce CPU meshes with outward-facing (CCW from outside)
// triangles, per-vertex normals, and UVs. Load one into a MeshRegistry to draw
// it. They are pure functions, unit-tested without a GPU.

func vert(p, n m.Vec3, u, v float32, tangent m.Vec4) Vertex {
	return Vertex{
		PX: p.X, PY: p.Y, PZ: p.Z,
		NX: n.X, NY: n.Y, NZ: n.Z,
		U: u, V: v,
		TX: tangent.X, TY: tangent.Y, TZ: tangent.Z, TW: tangent.W,
	}
}

// addQuad appends a flat quad (two triangles) with corners a,b,c,d in CCW order
// as seen from the normal side, with UVs (0,0),(1,0),(1,1),(0,1). The tangent
// (direction of increasing U) is the a->b edge; handedness is +1 since the
// a->d edge (increasing V) and cross(normal, tangent) agree for this winding.
func addQuad(mesh *Mesh, a, b, c, d, normal m.Vec3) {
	tangent := m.Vec4{X: 0, Y: 0, Z: 0, W: 1}
	if t := b.Sub(a); t.LengthSq() > 0 {
		tn := t.Normalize()
		tangent = m.Vec4{X: tn.X, Y: tn.Y, Z: tn.Z, W: 1}
	}
	base := uint32(len(mesh.Vertices))
	mesh.Vertices = append(mesh.Vertices,
		vert(a, normal, 0, 0, tangent),
		vert(b, normal, 1, 0, tangent),
		vert(c, normal, 1, 1, tangent),
		vert(d, normal, 0, 1, tangent),
	)
	mesh.Indices = append(mesh.Indices, base, base+1, base+2, base, base+2, base+3)
}

// Cube returns an axis-aligned cube centered at the origin with the given edge
// length. It uses 24 vertices (4 per face) so each face has its own flat normal
// and clean UVs, and 36 indices.
func Cube(size float32) *Mesh {
	h := size / 2
	mesh := &Mesh{}
	// +X
	addQuad(mesh, m.Vec3{X: h, Y: -h, Z: -h}, m.Vec3{X: h, Y: h, Z: -h},
		m.Vec3{X: h, Y: h, Z: h}, m.Vec3{X: h, Y: -h, Z: h}, m.Vec3{X: 1})
	// -X
	addQuad(mesh, m.Vec3{X: -h, Y: -h, Z: h}, m.Vec3{X: -h, Y: h, Z: h},
		m.Vec3{X: -h, Y: h, Z: -h}, m.Vec3{X: -h, Y: -h, Z: -h}, m.Vec3{X: -1})
	// +Y
	addQuad(mesh, m.Vec3{X: -h, Y: h, Z: h}, m.Vec3{X: h, Y: h, Z: h},
		m.Vec3{X: h, Y: h, Z: -h}, m.Vec3{X: -h, Y: h, Z: -h}, m.Vec3{Y: 1})
	// -Y
	addQuad(mesh, m.Vec3{X: -h, Y: -h, Z: -h}, m.Vec3{X: h, Y: -h, Z: -h},
		m.Vec3{X: h, Y: -h, Z: h}, m.Vec3{X: -h, Y: -h, Z: h}, m.Vec3{Y: -1})
	// +Z
	addQuad(mesh, m.Vec3{X: -h, Y: -h, Z: h}, m.Vec3{X: h, Y: -h, Z: h},
		m.Vec3{X: h, Y: h, Z: h}, m.Vec3{X: -h, Y: h, Z: h}, m.Vec3{Z: 1})
	// -Z
	addQuad(mesh, m.Vec3{X: h, Y: -h, Z: -h}, m.Vec3{X: -h, Y: -h, Z: -h},
		m.Vec3{X: -h, Y: h, Z: -h}, m.Vec3{X: h, Y: h, Z: -h}, m.Vec3{Z: -1})
	return mesh
}

// Quad returns a single w×h quad in the XY plane facing +Z, centered at origin.
func Quad(w, h float32) *Mesh {
	hw, hh := w/2, h/2
	mesh := &Mesh{}
	addQuad(mesh,
		m.Vec3{X: -hw, Y: -hh}, m.Vec3{X: hw, Y: -hh},
		m.Vec3{X: hw, Y: hh}, m.Vec3{X: -hw, Y: hh}, m.Vec3{Z: 1})
	return mesh
}

// Plane returns a flat grid in the XZ plane (normal +Y) centered at origin,
// spanning width along X and depth along Z, with subX×subZ cells. Subdivisions
// below 1 are clamped to 1.
func Plane(width, depth float32, subX, subZ int) *Mesh {
	if subX < 1 {
		subX = 1
	}
	if subZ < 1 {
		subZ = 1
	}
	mesh := &Mesh{}
	normal := m.Vec3{Y: 1}
	// U increases along +X, so the tangent is +X; +V is +Z and cross(+Y,+X) = -Z,
	// matching a handedness of -1.
	tangent := m.Vec4{X: 1, W: -1}
	for iz := 0; iz <= subZ; iz++ {
		fz := float32(iz) / float32(subZ)
		z := -depth/2 + depth*fz
		for ix := 0; ix <= subX; ix++ {
			fx := float32(ix) / float32(subX)
			x := -width/2 + width*fx
			mesh.Vertices = append(mesh.Vertices, vert(m.Vec3{X: x, Z: z}, normal, fx, fz, tangent))
		}
	}
	stride := subX + 1
	for iz := 0; iz < subZ; iz++ {
		for ix := 0; ix < subX; ix++ {
			i0 := uint32(iz*stride + ix)
			i1 := i0 + 1
			i2 := uint32((iz+1)*stride+ix) + 1
			i3 := uint32((iz+1)*stride + ix)
			// CCW from +Y: (i0,i3,i2) and (i0,i2,i1).
			mesh.Indices = append(mesh.Indices, i0, i3, i2, i0, i2, i1)
		}
	}
	return mesh
}

// UVSphere returns a sphere of the given radius approximated by a latitude/
// longitude grid: sectors around (longitude) and stacks top-to-bottom
// (latitude). Normals are the unit position; UVs map sector/stack to [0,1].
// sectors/stacks below 3/2 are clamped.
func UVSphere(radius float32, sectors, stacks int) *Mesh {
	if sectors < 3 {
		sectors = 3
	}
	if stacks < 2 {
		stacks = 2
	}
	mesh := &Mesh{}
	for i := 0; i <= stacks; i++ {
		phi := math.Pi/2 - math.Pi*float64(i)/float64(stacks) // +pi/2 (top) .. -pi/2 (bottom)
		cp := float32(math.Cos(phi))
		sp := float32(math.Sin(phi))
		for j := 0; j <= sectors; j++ {
			theta := 2 * math.Pi * float64(j) / float64(sectors)
			ct := float32(math.Cos(theta))
			st := float32(math.Sin(theta))
			n := m.Vec3{X: cp * ct, Y: sp, Z: cp * st}
			pos := n.Scale(radius)
			u := float32(j) / float32(sectors)
			v := float32(i) / float32(stacks)
			// Tangent is dPos/dθ direction: d/dθ(cosθ, _, sinθ) = (-sinθ, 0, cosθ),
			// the direction of increasing U (longitude). Handedness +1.
			tangent := m.Vec4{X: -st, Y: 0, Z: ct, W: 1}
			mesh.Vertices = append(mesh.Vertices, vert(pos, n, u, v, tangent))
		}
	}
	stride := sectors + 1
	for i := 0; i < stacks; i++ {
		for j := 0; j < sectors; j++ {
			k1 := uint32(i*stride + j)
			k2 := k1 + uint32(stride)
			// Skip the degenerate triangle at each pole.
			if i != 0 {
				mesh.Indices = append(mesh.Indices, k1, k1+1, k2+1)
			}
			if i != stacks-1 {
				mesh.Indices = append(mesh.Indices, k1, k2+1, k2)
			}
		}
	}
	return mesh
}
