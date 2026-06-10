package render3d

import (
	"math"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// --- tangents ---

// TestPrimitiveTangentsOrthonormal checks each primitive emits a tangent that is
// unit-length, perpendicular to its normal, and carries a ±1 handedness sign.
func TestPrimitiveTangentsOrthonormal(t *testing.T) {
	meshes := map[string]*Mesh{
		"cube":   Cube(2),
		"sphere": UVSphere(1.5, 16, 12),
		"plane":  Plane(4, 4, 2, 2),
		"quad":   Quad(2, 2),
	}
	for name, mesh := range meshes {
		for i, v := range mesh.Vertices {
			tan := m.Vec3{X: v.TX, Y: v.TY, Z: v.TZ}
			if l := tan.Length(); l < 0.99 || l > 1.01 {
				t.Errorf("%s vertex %d tangent length = %v, want ~1", name, i, l)
			}
			n := m.Vec3{X: v.NX, Y: v.NY, Z: v.NZ}
			if d := math.Abs(float64(n.Dot(tan))); d > 1e-3 {
				t.Errorf("%s vertex %d tangent not ⟂ normal: dot = %v", name, i, d)
			}
			if v.TW != 1 && v.TW != -1 {
				t.Errorf("%s vertex %d handedness = %v, want ±1", name, i, v.TW)
			}
		}
	}
}

// TestComputeTangentsDerivesBasis checks the loader's tangent fallback produces a
// valid orthonormal tangent for a UV-mapped quad with no supplied tangents.
func TestComputeTangentsDerivesBasis(t *testing.T) {
	// A quad in the XY plane (normal +Z) with U along +X, V along +Y.
	mesh := &Mesh{
		Vertices: []Vertex{
			{PX: 0, PY: 0, PZ: 0, NX: 0, NY: 0, NZ: 1, U: 0, V: 0},
			{PX: 1, PY: 0, PZ: 0, NX: 0, NY: 0, NZ: 1, U: 1, V: 0},
			{PX: 1, PY: 1, PZ: 0, NX: 0, NY: 0, NZ: 1, U: 1, V: 1},
			{PX: 0, PY: 1, PZ: 0, NX: 0, NY: 0, NZ: 1, U: 0, V: 1},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
	computeTangents(mesh)
	for i, v := range mesh.Vertices {
		tan := m.Vec3{X: v.TX, Y: v.TY, Z: v.TZ}
		// U increases along +X, so the tangent should point ~+X.
		if tan.X < 0.99 {
			t.Errorf("vertex %d tangent = %+v, want ~+X", i, tan)
		}
		if v.TW != 1 {
			t.Errorf("vertex %d handedness = %v, want +1", i, v.TW)
		}
	}
}

// --- bounding sphere ---

func TestBoundingSphereEnclosesVertices(t *testing.T) {
	mesh := UVSphere(2, 16, 12)
	center, radius := boundingSphere(mesh.Vertices)
	for i, v := range mesh.Vertices {
		d := m.Vec3{X: v.PX, Y: v.PY, Z: v.PZ}.Sub(center).Length()
		if d > radius+1e-3 {
			t.Errorf("vertex %d at distance %v exceeds radius %v", i, d, radius)
		}
	}
	// A radius-2 sphere centered at origin: center ~0, radius ~2.
	if center.Length() > 1e-3 {
		t.Errorf("center = %+v, want ~origin", center)
	}
	if radius < 1.9 || radius > 2.1 {
		t.Errorf("radius = %v, want ~2", radius)
	}
}

// --- frustum culling ---

// testFrustum builds a frustum for a camera at +Z looking toward the origin.
func testFrustum() frustum {
	view := m.LookAt(m.Vec3{X: 0, Y: 0, Z: 5}, m.Vec3{}, m.Vec3{Y: 1})
	proj := m.Perspective(m.DegToRad(60), 1, 0.1, 100)
	return extractFrustum(proj.Mul(view))
}

func TestFrustumKeepsVisibleSphere(t *testing.T) {
	f := testFrustum()
	if !f.sphereInside(m.Vec3{}, 1) {
		t.Errorf("sphere at origin should be inside the frustum")
	}
}

func TestFrustumCullsOffscreenSpheres(t *testing.T) {
	f := testFrustum()
	cases := []struct {
		name   string
		center m.Vec3
	}{
		{"behind camera", m.Vec3{X: 0, Y: 0, Z: 20}},
		{"far right", m.Vec3{X: 100, Y: 0, Z: 0}},
		{"far above", m.Vec3{X: 0, Y: 100, Z: 0}},
		{"beyond far plane", m.Vec3{X: 0, Y: 0, Z: -200}},
	}
	for _, c := range cases {
		if f.sphereInside(c.center, 1) {
			t.Errorf("%s: sphere at %+v should be culled", c.name, c.center)
		}
	}
}

func TestFrustumStraddlingSphereKept(t *testing.T) {
	f := testFrustum()
	// A huge sphere off to the right still overlaps the frustum and must be kept.
	if !f.sphereInside(m.Vec3{X: 100, Y: 0, Z: 0}, 110) {
		t.Errorf("large straddling sphere should be kept (conservative cull)")
	}
}

func TestMaxAxisScale(t *testing.T) {
	mat := m.Scaling(m.Vec3{X: 2, Y: 5, Z: 3})
	if got := maxAxisScale(mat); math.Abs(float64(got-5)) > 1e-4 {
		t.Errorf("maxAxisScale = %v, want 5", got)
	}
}

// --- glTF node transform decomposition ---

func TestDecomposeMatrixRoundTrip(t *testing.T) {
	tr := m.Vec3{X: 1, Y: -2, Z: 3}
	rot := m.FromAxisAngle(m.Vec3{X: 0, Y: 1, Z: 0}, m.DegToRad(90))
	sc := m.Vec3{X: 2, Y: 2, Z: 2}
	mat := m.TRS(tr, rot, sc)

	// Column-major Mat4 -> [16]float64 in the same order.
	var arr [16]float64
	for i := range arr {
		arr[i] = float64(mat[i])
	}
	got := decomposeMatrix(arr)

	if got.Translation.Sub(tr).Length() > 1e-4 {
		t.Errorf("translation = %+v, want %+v", got.Translation, tr)
	}
	if math.Abs(float64(got.Scale.X-2)) > 1e-4 || math.Abs(float64(got.Scale.Y-2)) > 1e-4 || math.Abs(float64(got.Scale.Z-2)) > 1e-4 {
		t.Errorf("scale = %+v, want ~(2,2,2)", got.Scale)
	}
	// Rotations q and -q are equivalent; compare via the rotated +X axis.
	wantX := rot.RotateVec3(m.Vec3{X: 1})
	gotX := got.Rotation.RotateVec3(m.Vec3{X: 1})
	if gotX.Sub(wantX).Length() > 1e-3 {
		t.Errorf("rotation maps +X to %+v, want %+v", gotX, wantX)
	}
}
