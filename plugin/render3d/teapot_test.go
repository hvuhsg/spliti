package render3d

import (
	"math"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// TestTeapotSane checks the teapot builds a non-empty, finite, indexed mesh
// whose bounds match the profile (base on y=0, knob near the top).
func TestTeapotSane(t *testing.T) {
	mesh := Teapot(1)
	if len(mesh.Vertices) == 0 || len(mesh.Indices) == 0 {
		t.Fatalf("empty teapot: %d verts, %d indices", len(mesh.Vertices), len(mesh.Indices))
	}
	if len(mesh.Indices)%3 != 0 {
		t.Fatalf("indices not a multiple of 3: %d", len(mesh.Indices))
	}
	var lo, hi m.Vec3 = mesh.vert3(0), mesh.vert3(0)
	for i := range mesh.Vertices {
		p := mesh.vert3(uint32(i))
		if math.IsNaN(float64(p.X+p.Y+p.Z)) || math.IsInf(float64(p.X+p.Y+p.Z), 0) {
			t.Fatalf("non-finite vertex %d: %+v", i, p)
		}
		lo.X, lo.Y, lo.Z = min(lo.X, p.X), min(lo.Y, p.Y), min(lo.Z, p.Z)
		hi.X, hi.Y, hi.Z = max(hi.X, p.X), max(hi.Y, p.Y), max(hi.Z, p.Z)
		n := m.Vec3{X: mesh.Vertices[i].NX, Y: mesh.Vertices[i].NY, Z: mesh.Vertices[i].NZ}
		if d := abs32(n.Length() - 1); d > 1e-3 {
			t.Fatalf("vertex %d normal not unit length: |n|=%v", i, n.Length())
		}
	}
	if lo.Y < -1e-4 || hi.Y < 1.0 {
		t.Fatalf("unexpected height bounds: lo.Y=%v hi.Y=%v", lo.Y, hi.Y)
	}
	// Spout reaches out past the body on +X; handle past it on -X.
	if hi.X < 1.0 || lo.X > -0.8 {
		t.Fatalf("spout/handle not present: lo.X=%v hi.X=%v", lo.X, hi.X)
	}
}

// TestRevolutionOutwardNormals verifies appendGrid's winding yields outward
// normals for a revolved surface: a straight cylinder's normals are radial.
func TestRevolutionOutwardNormals(t *testing.T) {
	mesh := &Mesh{}
	appendRevolution(mesh, []m.Vec2{{X: 1, Y: 0}, {X: 1, Y: 2}}, 24)
	computeNormals(mesh)
	for i := range mesh.Vertices {
		p := mesh.vert3(uint32(i))
		radial := m.Vec3{X: p.X, Z: p.Z} // outward direction from the Y axis
		if radial.LengthSq() < 1e-6 {
			continue
		}
		n := m.Vec3{X: mesh.Vertices[i].NX, Y: mesh.Vertices[i].NY, Z: mesh.Vertices[i].NZ}
		if n.Dot(radial.Normalize()) <= 0 {
			t.Fatalf("vertex %d normal faces inward: n=%+v radial=%+v", i, n, radial)
		}
	}
}

// TestTubeOutwardNormals verifies appendTube's normals point away from the
// centerline for a straight tube along +Z.
func TestTubeOutwardNormals(t *testing.T) {
	mesh := &Mesh{}
	appendTube(mesh, []m.Vec3{{Z: 0}, {Z: 1}, {Z: 2}}, []float32{0.3, 0.3, 0.3}, 16)
	computeNormals(mesh)
	for i := range mesh.Vertices {
		p := mesh.vert3(uint32(i))
		radial := m.Vec3{X: p.X, Y: p.Y} // centerline is the Z axis here
		if radial.LengthSq() < 1e-6 {
			continue
		}
		n := m.Vec3{X: mesh.Vertices[i].NX, Y: mesh.Vertices[i].NY, Z: mesh.Vertices[i].NZ}
		if n.Dot(radial.Normalize()) <= 0 {
			t.Fatalf("vertex %d tube normal faces inward: n=%+v radial=%+v", i, n, radial)
		}
	}
}

func (mesh *Mesh) vert3(i uint32) m.Vec3 {
	v := mesh.Vertices[i]
	return m.Vec3{X: v.PX, Y: v.PY, Z: v.PZ}
}
