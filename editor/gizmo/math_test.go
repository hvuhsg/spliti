package gizmo

import (
	"math"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

const eps = 1e-3

func approx(a, b float32) bool { return float32(math.Abs(float64(a-b))) < eps }

func approxV3(a, b m.Vec3) bool { return approx(a.X, b.X) && approx(a.Y, b.Y) && approx(a.Z, b.Z) }

// testCamera looks at the origin from +Z, 800x600 viewport at offset (100,50)
// — offsets catch rectMin bugs.
func testCamera(t *testing.T) camera {
	t.Helper()
	view := m.LookAt(m.Vec3{Z: 8}, m.Vec3{}, m.Vec3{Y: 1})
	proj := m.Perspective(m.DegToRad(60), 800.0/600.0, 0.1, 100)
	cam, ok := newCamera(view, proj, m.Vec2{X: 100, Y: 50}, m.Vec2{X: 800, Y: 600})
	if !ok {
		t.Fatal("newCamera failed")
	}
	return cam
}

func TestCameraBasis(t *testing.T) {
	cam := testCamera(t)
	if !approxV3(cam.eye, m.Vec3{Z: 8}) {
		t.Fatalf("eye = %v, want (0,0,8)", cam.eye)
	}
	if !approxV3(cam.fwd, m.Vec3{Z: -1}) {
		t.Fatalf("fwd = %v, want (0,0,-1)", cam.fwd)
	}
}

func TestWorldToScreenCenter(t *testing.T) {
	cam := testCamera(t)
	px, ok := cam.worldToScreen(m.Vec3{})
	if !ok {
		t.Fatal("origin not visible")
	}
	if !approx(px.X, 500) || !approx(px.Y, 350) {
		t.Fatalf("origin projects to %v, want viewport center (500,350)", px)
	}

	// +Y is up on screen → smaller Y pixel value.
	up, _ := cam.worldToScreen(m.Vec3{Y: 1})
	if up.Y >= px.Y {
		t.Fatalf("world +Y projected downward: %v vs center %v", up, px)
	}
	// +X is right on screen.
	right, _ := cam.worldToScreen(m.Vec3{X: 1})
	if right.X <= px.X {
		t.Fatalf("world +X projected leftward: %v vs center %v", right, px)
	}
}

func TestWorldToScreenBehindCamera(t *testing.T) {
	cam := testCamera(t)
	if _, ok := cam.worldToScreen(m.Vec3{Z: 20}); ok {
		t.Fatal("point behind the camera reported visible")
	}
}

func TestMouseRayRoundTrip(t *testing.T) {
	cam := testCamera(t)
	// Project a world point, shoot a ray through the resulting pixel, and
	// check the ray passes back through the point.
	p := m.Vec3{X: 1.3, Y: -0.7, Z: 2}
	px, ok := cam.worldToScreen(p)
	if !ok {
		t.Fatal("test point not visible")
	}
	ro, rd := cam.mouseRay(px)
	toP := p.Sub(ro)
	dist := toP.Sub(rd.Scale(toP.Dot(rd))).Length() // point-to-line distance
	if dist > 0.01 {
		t.Fatalf("ray misses the projected point by %v world units", dist)
	}
}

func TestScreenFactorGivesConstantPixelSize(t *testing.T) {
	cam := testCamera(t)
	for _, origin := range []m.Vec3{{}, {Z: -10}, {X: 2, Y: 1, Z: 3}} {
		sf := cam.screenFactor(origin, 96)
		a, _ := cam.worldToScreen(origin)
		b, _ := cam.worldToScreen(origin.Add(m.Vec3{X: sf}))
		got := v2dist(a, b)
		if got < 80 || got > 112 {
			t.Errorf("origin %v: %v px for screenFactor length, want ~96", origin, got)
		}
	}
}

func TestIntersectRayPlane(t *testing.T) {
	hit, ok := intersectRayPlane(m.Vec3{Z: 5}, m.Vec3{Z: -1}, m.Vec3{}, m.Vec3{Z: 1})
	if !ok || !approxV3(hit, m.Vec3{}) {
		t.Fatalf("straight-on hit = %v ok=%v", hit, ok)
	}
	if _, ok := intersectRayPlane(m.Vec3{Z: 5}, m.Vec3{X: 1}, m.Vec3{}, m.Vec3{Z: 1}); ok {
		t.Fatal("parallel ray reported a hit")
	}
	if _, ok := intersectRayPlane(m.Vec3{Z: 5}, m.Vec3{Z: 1}, m.Vec3{}, m.Vec3{Z: 1}); ok {
		t.Fatal("hit behind the ray origin reported")
	}
}

func TestAxisDragPlaneContainsAxisAndFacesCamera(t *testing.T) {
	origin := m.Vec3{X: 1, Y: 2, Z: 3}
	eye := m.Vec3{X: 4, Y: 8, Z: 12}
	for _, axis := range []m.Vec3{{X: 1}, {Y: 1}, {Z: 1}, {X: 0.577, Y: 0.577, Z: 0.577}} {
		n := axisDragPlane(origin, axis, eye)
		if !approx(n.Length(), 1) {
			t.Fatalf("axis %v: normal not unit: %v", axis, n)
		}
		if d := float32(math.Abs(float64(n.Dot(axis)))); d > eps {
			t.Errorf("axis %v: plane does not contain axis (n·a=%v)", axis, d)
		}
		view := eye.Sub(origin).Normalize()
		if float32(math.Abs(float64(n.Dot(view)))) < 0.1 {
			t.Errorf("axis %v: plane nearly edge-on to camera", axis)
		}
	}
}

func TestSignedAngleOnPlane(t *testing.T) {
	// X rotated to Y around Z is +90°.
	got := signedAngleOnPlane(m.Vec3{X: 1}, m.Vec3{Y: 1}, m.Vec3{Z: 1})
	if !approx(got, math.Pi/2) {
		t.Fatalf("X→Y around Z = %v rad, want +π/2", got)
	}
	got = signedAngleOnPlane(m.Vec3{Y: 1}, m.Vec3{X: 1}, m.Vec3{Z: 1})
	if !approx(got, -math.Pi/2) {
		t.Fatalf("Y→X around Z = %v rad, want -π/2", got)
	}
}

func TestSnapF(t *testing.T) {
	if got := snapF(1.34, 0.5); !approx(got, 1.5) {
		t.Errorf("snapF(1.34, .5) = %v", got)
	}
	if got := snapF(-1.1, 0.5); !approx(got, -1) {
		t.Errorf("snapF(-1.1, .5) = %v", got)
	}
	if got := snapF(1.34, 0); !approx(got, 1.34) {
		t.Errorf("snap disabled changed the value: %v", got)
	}
}

func TestRotateLinearPartKeepsTranslation(t *testing.T) {
	mat := m.TRS(m.Vec3{X: 5, Y: 6, Z: 7}, m.IdentityQuat(), m.Vec3{X: 2, Y: 2, Z: 2})
	out := rotateLinearPart(mat, m.Vec3{Y: 1}, m.DegToRad(90))
	if !approxV3(matTranslation(out), m.Vec3{X: 5, Y: 6, Z: 7}) {
		t.Fatalf("translation moved: %v", matTranslation(out))
	}
	// Local +X (scaled by 2) rotated 90° around Y lands on -Z.
	if got := matCol3(out, 0); !approxV3(got, m.Vec3{Z: -2}) {
		t.Fatalf("rotated X column = %v, want (0,0,-2)", got)
	}
	// Column lengths (scale) survive.
	if l := matCol3(out, 2).Length(); !approx(l, 2) {
		t.Fatalf("scale not preserved: |col2| = %v", l)
	}
}

func TestPointInQuad(t *testing.T) {
	q := [4]m.Vec2{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}
	if !pointInQuad(m.Vec2{X: 5, Y: 5}, q[0], q[1], q[2], q[3]) {
		t.Fatal("center not inside")
	}
	if pointInQuad(m.Vec2{X: 15, Y: 5}, q[0], q[1], q[2], q[3]) {
		t.Fatal("outside point inside")
	}
	// Reversed winding must work too (projection can flip it).
	if !pointInQuad(m.Vec2{X: 5, Y: 5}, q[3], q[2], q[1], q[0]) {
		t.Fatal("center not inside reversed-winding quad")
	}
}

func TestDistPointSegment(t *testing.T) {
	a, b := m.Vec2{X: 0, Y: 0}, m.Vec2{X: 10, Y: 0}
	if got := distPointSegment(m.Vec2{X: 5, Y: 3}, a, b); !approx(got, 3) {
		t.Errorf("mid distance = %v", got)
	}
	if got := distPointSegment(m.Vec2{X: -4, Y: 0}, a, b); !approx(got, 4) {
		t.Errorf("beyond-endpoint distance = %v", got)
	}
}

// TestTranslateDragMovesAlongAxis drives the full state machine through a
// simulated X-axis drag: press on the axis handle, move the mouse, release.
func TestTranslateDragMovesAlongAxis(t *testing.T) {
	view := m.LookAt(m.Vec3{Z: 8}, m.Vec3{}, m.Vec3{Y: 1})
	proj := m.Perspective(m.DegToRad(60), 800.0/600.0, 0.1, 100)
	frame := func(mouse m.Vec2, down bool) Frame {
		return Frame{
			View: view, Proj: proj,
			RectMin: m.Vec2{}, RectSize: m.Vec2{X: 800, Y: 600},
			MousePos: mouse, MouseDown: down,
		}
	}
	cam, _ := newCamera(view, proj, m.Vec2{}, m.Vec2{X: 800, Y: 600})

	model := m.Translation(m.Vec3{})
	g := &Gizmo{}

	// A point partway out the +X handle.
	sf := cam.screenFactor(m.Vec3{}, gizmoPx)
	grabPx, _ := cam.worldToScreen(m.Vec3{X: 0.8 * sf})
	hot := hitTest(cam, frame(grabPx, false), Translate, m.Vec3{}, mustScreen(cam, m.Vec3{}), [3]m.Vec3{{X: 1}, {Y: 1}, {Z: 1}}, sf)
	if hot != partAxisX {
		t.Fatalf("hit test on +X handle returned part %d", hot)
	}

	// begin/applyDrag are the math half of the state machine (draw is the
	// only part that needs an ImGui context, and it isn't exercised here).
	g.begin(cam, frame(grabPx, true), Translate, partAxisX, m.Vec3{}, [3]m.Vec3{{X: 1}, {Y: 1}, {Z: 1}}, model)
	if !g.Using() {
		t.Fatal("drag did not start")
	}

	// Drag the cursor to where world (2,0,0) projects. The grab point sat at
	// world x = 0.8*sf on the drag plane, so the constrained delta — and the
	// final translation — is 2 - 0.8*sf along X.
	targetPx, _ := cam.worldToScreen(m.Vec3{X: 2})
	changed := g.applyDrag(cam, frame(targetPx, true), &model)
	if !changed {
		t.Fatal("drag did not change the model")
	}
	got := matTranslation(model)
	if !approx(got.Y, 0) || !approx(got.Z, 0) {
		t.Fatalf("axis drag leaked off-axis: %v", got)
	}
	want := 2 - 0.8*sf
	if float32(math.Abs(float64(got.X-want))) > 0.05 {
		t.Fatalf("dragged X = %v, want ≈ %v", got.X, want)
	}
}
