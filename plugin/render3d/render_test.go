package render3d

import (
	"testing"
	"unsafe"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// --- struct layout guards (the std430/vertex-layout footgun) ---

func TestStructStrides(t *testing.T) {
	if got := unsafe.Sizeof(Vertex{}); got != vertexStride {
		t.Errorf("Vertex size = %d, want %d", got, vertexStride)
	}
	if got := unsafe.Sizeof(pointLightGPU{}); got != pointLightStride {
		t.Errorf("pointLightGPU size = %d, want %d", got, pointLightStride)
	}
	if got := unsafe.Sizeof(m.Mat4{}); got != instanceStride {
		t.Errorf("Mat4 size = %d, want %d", got, instanceStride)
	}
	if frameUBOBytes%16 != 0 {
		t.Errorf("frame UBO size %d not 16-byte aligned", frameUBOBytes)
	}
}

// --- packMeshInstances ---

func TestPackMeshInstancesGroups(t *testing.T) {
	mk := func(mesh, mat string) renderItem {
		return renderItem{mesh: mesh, material: mat, model: m.Identity4()}
	}
	items := []renderItem{
		mk("cube", "red"),
		mk("sphere", "blue"),
		mk("cube", "red"),
		mk("cube", "blue"),
		mk("sphere", "blue"),
	}
	scratch, batches := packMeshInstances(items, nil, nil)

	if len(scratch) != 5 {
		t.Fatalf("packed %d matrices, want 5", len(scratch))
	}
	// After a stable sort by (mesh, material): cube/blue(1), cube/red(2),
	// sphere/blue(2) -> 3 batches.
	if len(batches) != 3 {
		t.Fatalf("got %d batches, want 3: %+v", len(batches), batches)
	}
	want := []meshBatch{
		{mesh: "cube", material: "blue", start: 0, count: 1},
		{mesh: "cube", material: "red", start: 1, count: 2},
		{mesh: "sphere", material: "blue", start: 3, count: 2},
	}
	for i, w := range want {
		if batches[i] != w {
			t.Errorf("batch %d = %+v, want %+v", i, batches[i], w)
		}
	}
	// Every instance is accounted for exactly once by the batches.
	total := 0
	for _, b := range batches {
		total += b.count
	}
	if total != len(scratch) {
		t.Errorf("batch counts sum to %d, want %d", total, len(scratch))
	}
}

func TestPackMeshInstancesReusesSlices(t *testing.T) {
	items := []renderItem{{mesh: "a", material: "x", model: m.Identity4()}}
	scratch := make([]m.Mat4, 0, 8)
	batches := make([]meshBatch, 0, 8)
	gotS, gotB := packMeshInstances(items, scratch[:0], batches[:0])
	if &gotS[0] != &scratch[:1][0] {
		t.Errorf("scratch slice was reallocated, expected reuse")
	}
	if len(gotB) != 1 {
		t.Errorf("got %d batches, want 1", len(gotB))
	}
}

// --- packLights ---

func TestPackLightsCapAndLayout(t *testing.T) {
	in := []collectedPoint{
		{Pos: m.Vec3{X: 1, Y: 2, Z: 3}, Color: m.Vec3{X: 0.5, Y: 0.6, Z: 0.7}, Intensity: 4, Range: 9},
		{Pos: m.Vec3{X: -1}, Color: m.Vec3{Y: 1}, Intensity: 2, Range: 0}, // zero range -> default
	}
	out := packLights(in, 10, nil)
	if len(out) != 2 {
		t.Fatalf("packed %d, want 2", len(out))
	}
	first := out[0]
	if first.PX != 1 || first.PY != 2 || first.PZ != 3 || first.Range != 9 {
		t.Errorf("first pos/range = %+v", first)
	}
	if first.R != 0.5 || first.G != 0.6 || first.B != 0.7 || first.Intensity != 4 {
		t.Errorf("first color/intensity = %+v", first)
	}
	if out[1].Range != 10 {
		t.Errorf("zero range = %v, want default 10", out[1].Range)
	}
}

func TestPackLightsTruncates(t *testing.T) {
	in := make([]collectedPoint, 5)
	out := packLights(in, 3, nil)
	if len(out) != 3 {
		t.Errorf("truncated to %d, want 3", len(out))
	}
}

// --- primitives ---

func TestCubeCounts(t *testing.T) {
	c := Cube(2)
	if len(c.Vertices) != 24 {
		t.Errorf("cube has %d vertices, want 24", len(c.Vertices))
	}
	if len(c.Indices) != 36 {
		t.Errorf("cube has %d indices, want 36", len(c.Indices))
	}
}

func TestPlaneCounts(t *testing.T) {
	p := Plane(10, 10, 4, 3)
	wantV := (4 + 1) * (3 + 1)
	wantI := 4 * 3 * 6
	if len(p.Vertices) != wantV {
		t.Errorf("plane has %d vertices, want %d", len(p.Vertices), wantV)
	}
	if len(p.Indices) != wantI {
		t.Errorf("plane has %d indices, want %d", len(p.Indices), wantI)
	}
}

func TestSphereCounts(t *testing.T) {
	sectors, stacks := 8, 6
	s := UVSphere(1, sectors, stacks)
	wantV := (sectors + 1) * (stacks + 1)
	if len(s.Vertices) != wantV {
		t.Errorf("sphere has %d vertices, want %d", len(s.Vertices), wantV)
	}
	// Caps contribute one triangle per sector; middle stacks two.
	wantI := (2*(stacks-1) - 0) // placeholder, computed below
	wantI = sectors*3*2 + sectors*(stacks-2)*6
	if len(s.Indices) != wantI {
		t.Errorf("sphere has %d indices, want %d", len(s.Indices), wantI)
	}
}

// TestNormalsUnitLength checks every primitive emits unit-length normals.
func TestNormalsUnitLength(t *testing.T) {
	meshes := map[string]*Mesh{
		"cube":   Cube(2),
		"sphere": UVSphere(1.5, 12, 8),
		"plane":  Plane(4, 4, 2, 2),
		"quad":   Quad(2, 2),
	}
	for name, mesh := range meshes {
		for i, v := range mesh.Vertices {
			n := m.Vec3{X: v.NX, Y: v.NY, Z: v.NZ}
			if l := n.Length(); l < 0.99 || l > 1.01 {
				t.Errorf("%s vertex %d normal length = %v, want ~1", name, i, l)
			}
		}
	}
}

// TestOutwardWinding checks every triangle winds CCW as seen from outside: the
// geometric face normal (edge cross product) agrees with the vertex normals.
func TestOutwardWinding(t *testing.T) {
	meshes := map[string]*Mesh{
		"cube":   Cube(2),
		"sphere": UVSphere(1.5, 16, 12),
		"plane":  Plane(4, 4, 3, 3),
		"quad":   Quad(2, 2),
	}
	for name, mesh := range meshes {
		for tri := 0; tri+2 < len(mesh.Indices); tri += 3 {
			ia, ib, ic := mesh.Indices[tri], mesh.Indices[tri+1], mesh.Indices[tri+2]
			a := pos(mesh.Vertices[ia])
			b := pos(mesh.Vertices[ib])
			c := pos(mesh.Vertices[ic])
			face := b.Sub(a).Cross(c.Sub(a))
			if face.LengthSq() < 1e-12 {
				continue // degenerate (pole) triangle
			}
			// Average vertex normal of the triangle.
			avg := nrm(mesh.Vertices[ia]).Add(nrm(mesh.Vertices[ib])).Add(nrm(mesh.Vertices[ic]))
			if face.Normalize().Dot(avg.Normalize()) <= 0 {
				t.Errorf("%s triangle %d winds inward (face . normal <= 0)", name, tri/3)
			}
		}
	}
}

func pos(v Vertex) m.Vec3 { return m.Vec3{X: v.PX, Y: v.PY, Z: v.PZ} }
func nrm(v Vertex) m.Vec3 { return m.Vec3{X: v.NX, Y: v.NY, Z: v.NZ} }

// --- normalizeSamples ---

func TestNormalizeSamples(t *testing.T) {
	cases := map[int]uint32{0: 1, 1: 1, 2: 4, 4: 4, 8: 4}
	for in, want := range cases {
		if got := normalizeSamples(in); got != want {
			t.Errorf("normalizeSamples(%d) = %d, want %d", in, got, want)
		}
	}
}
