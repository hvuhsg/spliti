package render3d

import (
	"math"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// Teapot returns a stylized teapot mesh: a revolved body+lid+knob with swept
// tube spout and handle. It is not the Bezier Utah teapot — it is a compact,
// fully procedural stand-in (no embedded patch data) sized so its base sits on
// y=0 and its footprint scales with the given factor. It serves as the default
// scaffold scene's spinning centerpiece. Normals are computed from the triangle
// winding (see computeNormals); the rings/profile are wound so they face out.
func Teapot(scale float32) *Mesh {
	mesh := &Mesh{}

	// Body, lid and knob as one revolution profile, (radius, height) bottom→top.
	// The small radius bumps at ~0.95 (lid lip) and ~1.30 (knob) read as the lid
	// overhang and the lid knob without modeling them separately.
	profile := []m.Vec2{
		{X: 0.00, Y: 0.00}, {X: 0.30, Y: 0.00}, {X: 0.46, Y: 0.10},
		{X: 0.56, Y: 0.28}, {X: 0.60, Y: 0.50}, {X: 0.55, Y: 0.70},
		{X: 0.42, Y: 0.86}, {X: 0.34, Y: 0.94}, // body rim (opening)
		{X: 0.40, Y: 0.95}, {X: 0.36, Y: 1.00}, {X: 0.26, Y: 1.10}, // lid
		{X: 0.14, Y: 1.18}, {X: 0.07, Y: 1.22}, // dome → knob base
		{X: 0.05, Y: 1.26}, {X: 0.10, Y: 1.32}, {X: 0.06, Y: 1.37}, {X: 0.00, Y: 1.39}, // knob
	}
	appendRevolution(mesh, profile, 36)

	// Spout: a tapering tube curving up and out on +X.
	appendTube(mesh,
		[]m.Vec3{{X: 0.45, Y: 0.52}, {X: 0.70, Y: 0.60}, {X: 0.92, Y: 0.74}, {X: 1.06, Y: 0.93}},
		[]float32{0.16, 0.12, 0.08, 0.05}, 14)

	// Handle: a near-uniform tube arcing out and back on -X.
	appendTube(mesh,
		[]m.Vec3{{X: -0.42, Y: 0.55}, {X: -0.74, Y: 0.62}, {X: -0.84, Y: 0.82}, {X: -0.66, Y: 0.96}, {X: -0.45, Y: 0.95}},
		[]float32{0.055, 0.06, 0.06, 0.06, 0.055}, 12)

	if scale != 1 {
		for i := range mesh.Vertices {
			mesh.Vertices[i].PX *= scale
			mesh.Vertices[i].PY *= scale
			mesh.Vertices[i].PZ *= scale
		}
	}
	computeNormals(mesh)
	fillTangents(mesh)
	return mesh
}

// appendRevolution sweeps a (radius, height) profile around the +Y axis in
// `segments` steps, appending a watertight surface to mesh.
func appendRevolution(mesh *Mesh, profile []m.Vec2, segments int) {
	if segments < 3 || len(profile) < 2 {
		return
	}
	cols := segments + 1 // duplicate the seam column for clean UVs
	grid := make([][]m.Vec3, len(profile))
	for i, p := range profile {
		grid[i] = make([]m.Vec3, cols)
		for j := 0; j < cols; j++ {
			a := 2 * math.Pi * float64(j) / float64(segments)
			grid[i][j] = m.Vec3{X: p.X * float32(math.Cos(a)), Y: p.Y, Z: p.X * float32(math.Sin(a))}
		}
	}
	appendGrid(mesh, grid)
}

// appendTube sweeps a circular cross-section of the given per-point radius along
// the path, appending a tube surface to mesh. Cross-section frames use a fixed
// world-up reference (the teapot's spout and handle paths lie in the XY plane,
// so this never degenerates), falling back to +Z if a segment runs vertical.
func appendTube(mesh *Mesh, path []m.Vec3, radii []float32, ringSegs int) {
	if len(path) < 2 || len(radii) != len(path) || ringSegs < 3 {
		return
	}
	cols := ringSegs + 1
	grid := make([][]m.Vec3, len(path))
	for i, c := range path {
		// Tangent from neighbours (one-sided at the ends).
		var t m.Vec3
		switch {
		case i == 0:
			t = path[1].Sub(path[0])
		case i == len(path)-1:
			t = path[i].Sub(path[i-1])
		default:
			t = path[i+1].Sub(path[i-1])
		}
		t = t.Normalize()
		up := m.Vec3{Y: 1}
		if abs32(t.Dot(up)) > 0.99 {
			up = m.Vec3{Z: 1}
		}
		right := t.Cross(up).Normalize()
		up = right.Cross(t).Normalize()
		grid[i] = make([]m.Vec3, cols)
		for j := 0; j < cols; j++ {
			a := 2 * math.Pi * float64(j) / float64(ringSegs)
			off := right.Scale(float32(math.Cos(a)) * radii[i]).Add(up.Scale(float32(math.Sin(a)) * radii[i]))
			grid[i][j] = c.Add(off)
		}
	}
	appendGrid(mesh, grid)
}

// appendGrid stitches a rows×cols vertex grid into the mesh as quads, winding
// each triangle so the surface faces outward (verified against the radial
// normal in TestTeapotOutwardNormals). UVs span [0,1] across the grid.
func appendGrid(mesh *Mesh, grid [][]m.Vec3) {
	rows := len(grid)
	if rows < 2 {
		return
	}
	cols := len(grid[0])
	base := uint32(len(mesh.Vertices))
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			u := float32(j) / float32(cols-1)
			v := float32(i) / float32(rows-1)
			// Normal is filled later by computeNormals; tangent is a nonzero
			// placeholder refined by fillTangents.
			mesh.Vertices = append(mesh.Vertices,
				vert(grid[i][j], m.Vec3{Y: 1}, u, v, m.Vec4{X: 1, W: 1}))
		}
	}
	idx := func(i, j int) uint32 { return base + uint32(i*cols+j) }
	for i := 0; i < rows-1; i++ {
		for j := 0; j < cols-1; j++ {
			a, b, c, d := idx(i, j), idx(i, j+1), idx(i+1, j+1), idx(i+1, j)
			mesh.Indices = append(mesh.Indices, a, c, b, a, d, c)
		}
	}
}

// fillTangents sets each vertex tangent to a unit vector perpendicular to its
// (already computed) normal, so untextured PBR materials never see a degenerate
// tangent. The choice is arbitrary — these meshes carry no normal maps. It also
// repairs degenerate normals: the seam-duplicate vertices on the radius-0 apex
// rows only touch zero-area triangles, so computeNormals leaves them zero; those
// points lie on the Y axis, so +Y is a fine substitute and keeps the shader from
// normalizing a zero vector.
func fillTangents(mesh *Mesh) {
	for i := range mesh.Vertices {
		n := m.Vec3{X: mesh.Vertices[i].NX, Y: mesh.Vertices[i].NY, Z: mesh.Vertices[i].NZ}
		if n.LengthSq() < 1e-6 {
			n = m.Vec3{Y: 1}
			mesh.Vertices[i].NX, mesh.Vertices[i].NY, mesh.Vertices[i].NZ = 0, 1, 0
		}
		ref := m.Vec3{Y: 1}
		if abs32(n.Dot(ref)) > 0.99 {
			ref = m.Vec3{X: 1}
		}
		t := ref.Cross(n).Normalize()
		mesh.Vertices[i].TX, mesh.Vertices[i].TY, mesh.Vertices[i].TZ, mesh.Vertices[i].TW = t.X, t.Y, t.Z, 1
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
