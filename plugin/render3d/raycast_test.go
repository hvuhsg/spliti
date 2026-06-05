package render3d

import (
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// --- ray/triangle ---

func TestRayTriangleHit(t *testing.T) {
	// Triangle in the z=0 plane; ray from +z straight down hits the centroid.
	v0 := m.Vec3{X: 0, Y: 0}
	v1 := m.Vec3{X: 1, Y: 0}
	v2 := m.Vec3{X: 0, Y: 1}
	orig := m.Vec3{X: 0.2, Y: 0.2, Z: 5}
	dir := m.Vec3{Z: -1}
	got, ok := rayTriangle(orig, dir, v0, v1, v2)
	if !ok {
		t.Fatal("expected hit")
	}
	if got < 4.99 || got > 5.01 {
		t.Errorf("t = %v, want ~5", got)
	}
}

func TestRayTriangleMiss(t *testing.T) {
	v0 := m.Vec3{X: 0, Y: 0}
	v1 := m.Vec3{X: 1, Y: 0}
	v2 := m.Vec3{X: 0, Y: 1}
	// Outside the triangle (u+v > 1).
	orig := m.Vec3{X: 0.9, Y: 0.9, Z: 5}
	if _, ok := rayTriangle(orig, m.Vec3{Z: -1}, v0, v1, v2); ok {
		t.Error("expected miss for point outside triangle")
	}
	// Pointing away from the triangle.
	if _, ok := rayTriangle(m.Vec3{X: 0.2, Y: 0.2, Z: 5}, m.Vec3{Z: 1}, v0, v1, v2); ok {
		t.Error("expected miss for ray pointing away")
	}
}

// --- raycastMesh against a unit cube ---

func TestRaycastMeshCube(t *testing.T) {
	cube := Cube(2) // spans [-1,1]
	// Ray from +x toward -x hits the +X face at x=1, so t = 4 from x=5.
	orig := m.Vec3{X: 5}
	dir := m.Vec3{X: -1}
	tHit, n, ok := raycastMesh(cube, orig, dir)
	if !ok {
		t.Fatal("expected cube hit")
	}
	if tHit < 3.99 || tHit > 4.01 {
		t.Errorf("t = %v, want ~4", tHit)
	}
	// The +X face normal should point along +x (outward).
	if n.X < 0.99 {
		t.Errorf("normal = %+v, want ~+X", n)
	}
}

// --- ScreenToRay round-trip ---

func TestScreenToRayRoundTrip(t *testing.T) {
	cam := &Camera3D{
		Position: m.Vec3{X: 0, Y: 0, Z: 5},
		Target:   m.Vec3{},
		Up:       m.Vec3{Y: 1},
		FovYDeg:  60, Near: 0.1, Far: 100,
		aspect: 1,
	}
	vw, vh := 800, 600
	// Project a known world point to screen, then unproject and check the point
	// lies on the returned ray.
	world := m.Vec3{X: 0.5, Y: -0.3, Z: 0}
	vp := cam.Projection().Mul(cam.View())
	clip := vp.MulVec4(m.Vec4{X: world.X, Y: world.Y, Z: world.Z, W: 1})
	ndcX, ndcY := clip.X/clip.W, clip.Y/clip.W
	px := (float64(ndcX) + 1) / 2 * float64(vw)
	py := (1 - float64(ndcY)) / 2 * float64(vh)

	origin, dir := cam.ScreenToRay(px, py, vw, vh)
	// The world point must lie along origin + t*dir for some t>0.
	toPoint := world.Sub(origin)
	tEst := toPoint.Dot(dir)
	closest := origin.Add(dir.Scale(tEst))
	if d := closest.Sub(world).Length(); d > 1e-3 {
		t.Errorf("world point off the ray by %v (origin %+v dir %+v)", d, origin, dir)
	}
}
