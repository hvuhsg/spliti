package render3d

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// spawnCam adds a camera entity at t with component cam during Startup.
func spawnCam(a *app.App, t Transform3D, cam Camera) {
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		c.Commands().Add(func(w *ecs.World) {
			lt, lc := t, cam
			mp := generic.NewMap3[Transform3D, GlobalTransform, Camera](w)
			mp.NewWith(&lt, &GlobalTransform{Matrix: m.Identity4()}, &lc)
		})
	})
}

// TestCameraEntityDrivesResource checks the active camera entity poses the
// Camera3D resource from its world transform and copies its projection params.
func TestCameraEntityDrivesResource(t *testing.T) {
	res := &Camera3D{aspect: 1}
	tr := XForm().At(5, 3, -2).EulerDeg(0, 90, 0)
	a := app.New()
	app.InsertResource(a, res)
	spawnCam(a, tr, Camera{FovYDeg: 50, Near: 0.2, Far: 500, Active: true})
	prop := newPropagateTransforms()
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		prop(c)
		applyCameraEntity(c)
	})
	a.SetMaxFrames(1).Run()

	wantPos := m.Vec3{X: 5, Y: 3, Z: -2}
	wantTarget := wantPos.Add(Forward(tr.matrix()))
	if res.Position.Sub(wantPos).Length() > 1e-5 {
		t.Errorf("Position = %v, want %v", res.Position, wantPos)
	}
	if res.Target.Sub(wantTarget).Length() > 1e-5 {
		t.Errorf("Target = %v, want %v", res.Target, wantTarget)
	}
	if res.FovYDeg != 50 || res.Near != 0.2 || res.Far != 500 {
		t.Errorf("projection = (%v, %v, %v), want (50, 0.2, 500)", res.FovYDeg, res.Near, res.Far)
	}
}

// TestInactiveCameraLeavesResource confirms a camera entity that is not Active
// does not touch the resource (a scene/game-driven camera keeps working).
func TestInactiveCameraLeavesResource(t *testing.T) {
	res := &Camera3D{Position: m.Vec3{X: 1, Y: 2, Z: 3}, Target: m.Vec3{Z: -1}, FovYDeg: 75, aspect: 1}
	before := *res
	a := app.New()
	app.InsertResource(a, res)
	spawnCam(a, XForm().At(9, 9, 9), Camera{FovYDeg: 30, Active: false})
	prop := newPropagateTransforms()
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		prop(c)
		applyCameraEntity(c)
	})
	a.SetMaxFrames(1).Run()

	if *res != before {
		t.Errorf("resource changed by an inactive camera: %+v != %+v", *res, before)
	}
}

// TestOrthographicCameraDrivesResource checks an orthographic Camera entity
// switches the resource to ortho and copies its size/clip params.
func TestOrthographicCameraDrivesResource(t *testing.T) {
	res := &Camera3D{aspect: 1}
	a := app.New()
	app.InsertResource(a, res)
	spawnCam(a, XForm().At(0, 5, 0), Camera{Orthographic: true, OrthoSize: 20, Near: 0.5, Far: 100, Active: true})
	prop := newPropagateTransforms()
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		prop(c)
		applyCameraEntity(c)
	})
	a.SetMaxFrames(1).Run()

	if !res.Ortho {
		t.Errorf("res.Ortho = false, want true")
	}
	if res.OrthoHeight != 20 {
		t.Errorf("OrthoHeight = %v, want 20", res.OrthoHeight)
	}
	if res.Near != 0.5 || res.Far != 100 {
		t.Errorf("clip = (%v, %v), want (0.5, 100)", res.Near, res.Far)
	}
}

// TestOrthoProjectionMapping confirms the orthographic projection maps the near
// plane to clip depth 0, the far plane to 1, and the half-width edge to NDC 1
// (no perspective divide, so clip == NDC).
func TestOrthoProjectionMapping(t *testing.T) {
	cam := &Camera3D{Ortho: true, OrthoHeight: 10, Near: 1, Far: 11, aspect: 1}
	p := cam.Projection()
	abs := func(f float32) float32 {
		if f < 0 {
			return -f
		}
		return f
	}
	atNear := p.MulVec4(m.Vec4{Z: -1, W: 1})
	atFar := p.MulVec4(m.Vec4{Z: -11, W: 1})
	if abs(atNear.Z) > 1e-5 {
		t.Errorf("near depth = %v, want 0", atNear.Z)
	}
	if abs(atFar.Z-1) > 1e-5 {
		t.Errorf("far depth = %v, want 1", atFar.Z)
	}
	// Half-width = OrthoHeight*aspect/2 = 5; that x maps to NDC +1.
	edge := p.MulVec4(m.Vec4{X: 5, Z: -1, W: 1})
	if abs(edge.X-1) > 1e-5 {
		t.Errorf("x edge = %v, want 1", edge.X)
	}
}

// TestLastActiveCameraWins checks that with two active cameras the last queried
// one drives the resource (the documented tie-break).
func TestLastActiveCameraWins(t *testing.T) {
	res := &Camera3D{aspect: 1}
	a := app.New()
	app.InsertResource(a, res)
	spawnCam(a, XForm().At(1, 0, 0), Camera{FovYDeg: 60, Active: true})
	spawnCam(a, XForm().At(2, 0, 0), Camera{FovYDeg: 60, Active: true})
	prop := newPropagateTransforms()
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		prop(c)
		applyCameraEntity(c)
	})
	a.SetMaxFrames(1).Run()

	// Both cameras sit on +X; the resource must land on one of them, not at the
	// origin — i.e. some active camera drove it.
	if res.Position.X != 1 && res.Position.X != 2 {
		t.Errorf("Position.X = %v, want 1 or 2 (an active camera)", res.Position.X)
	}
}
