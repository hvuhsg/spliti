package render3d

import (
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// TestParseOBJ exercises the loader's attribute formats, polygon (quad)
// triangulation, negative indices, comments, and V-flip.
func TestParseOBJ(t *testing.T) {
	src := []byte(`# a unit quad split into two triangles
v 0 0 0
v 1 0 0
v 1 1 0
v 0 1 0
vt 0 0
vt 1 0
vt 1 1
vt 0 1
vn 0 0 1
# one quad face: fan-triangulated into 2 tris (6 indices)
f 1/1/1 2/2/1 3/3/1 4/4/1
# a triangle using negative (relative) indices into the 4 verts above
f -4//-1 -3//-1 -2//-1
`)
	mesh, err := ParseOBJ(src)
	if err != nil {
		t.Fatalf("ParseOBJ: %v", err)
	}
	// 4 unique corners with UVs from the quad; the negative-index triangle reuses
	// positions but omits UVs, so its corners are distinct keys: +3 => 7.
	if len(mesh.Vertices) != 7 {
		t.Fatalf("vertices = %d, want 7", len(mesh.Vertices))
	}
	if len(mesh.Indices) != 9 { // 2 tris (quad) + 1 tri
		t.Fatalf("indices = %d, want 9", len(mesh.Indices))
	}
	// V was flipped: vt (0,0) -> (0,1).
	if mesh.Vertices[0].U != 0 || mesh.Vertices[0].V != 1 {
		t.Fatalf("uv not V-flipped: got (%v,%v), want (0,1)", mesh.Vertices[0].U, mesh.Vertices[0].V)
	}
	// Explicit normals survive; tangents are unit and perpendicular to them.
	for i := range mesh.Vertices {
		v := mesh.Vertices[i]
		n := m.Vec3{X: v.NX, Y: v.NY, Z: v.NZ}
		tan := m.Vec3{X: v.TX, Y: v.TY, Z: v.TZ}
		if abs32(tan.Length()-1) > 1e-3 {
			t.Fatalf("vertex %d tangent not unit: |t|=%v", i, tan.Length())
		}
		if abs32(n.Dot(tan)) > 1e-3 {
			t.Fatalf("vertex %d tangent not perpendicular to normal: dot=%v", i, n.Dot(tan))
		}
	}
}

// TestParseOBJComputesNormals checks that faces without normals get unit normals
// derived from the triangle geometry.
func TestParseOBJComputesNormals(t *testing.T) {
	// A single triangle in the z=0 plane, wound CCW => normal +Z.
	mesh, err := ParseOBJ([]byte("v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 3\n"))
	if err != nil {
		t.Fatalf("ParseOBJ: %v", err)
	}
	for i := range mesh.Vertices {
		n := m.Vec3{X: mesh.Vertices[i].NX, Y: mesh.Vertices[i].NY, Z: mesh.Vertices[i].NZ}
		if abs32(n.Z-1) > 1e-4 {
			t.Fatalf("vertex %d normal = %+v, want +Z", i, n)
		}
	}
}

// TestParseOBJErrors checks the loader rejects malformed input with an error
// rather than panicking or returning empty geometry.
func TestParseOBJErrors(t *testing.T) {
	cases := map[string]string{
		"empty":           ``,
		"no faces":        "v 0 0 0\nv 1 0 0\nv 0 1 0\n",
		"index too big":   "v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 9\n",
		"index zero":      "v 0 0 0\nv 1 0 0\nv 0 1 0\nf 0 1 2\n",
		"degenerate face": "v 0 0 0\nv 1 0 0\nf 1 2\n",
		"bad vertex":      "v 0 zero 0\nv 1 0 0\nv 0 1 0\nf 1 2 3\n",
	}
	for name, src := range cases {
		if _, err := ParseOBJ([]byte(src)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}
