package render3d

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/schedule"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// TestHeadlessInstallsResources confirms the headless plugin installs the same
// resources a real build does — registries, camera, GPU — without a device, so a
// game's scene setup can run on a machine with no GPU or display.
func TestHeadlessInstallsResources(t *testing.T) {
	a := app.New()
	a.AddPlugins(Plugin{Headless: true, Width: 800, Height: 600})
	a.SetMaxFrames(1).Run()
	c := a.Ctx()

	g := app.GetResource[GPU](c)
	if g == nil || !g.headless {
		t.Fatal("GPU resource missing or not headless")
	}
	if g.device != nil || g.instance != nil || g.surface != nil {
		t.Fatal("headless GPU must not create a device/instance/surface")
	}
	if w, h := Size(c); w != 800 || h != 600 {
		t.Errorf("Size = (%d, %d), want (800, 600)", w, h)
	}
	if app.GetResource[MeshRegistry](c) == nil {
		t.Error("MeshRegistry not installed")
	}
	if app.GetResource[MaterialRegistry](c) == nil {
		t.Error("MaterialRegistry not installed")
	}
	if app.GetResource[Camera3D](c) == nil {
		t.Error("Camera3D not installed")
	}
}

// TestHeadlessRegistriesCPUOnly confirms a mesh/material Load succeeds with no
// device, retaining CPU geometry + bounds (for picking/Keys) but uploading
// nothing.
func TestHeadlessRegistriesCPUOnly(t *testing.T) {
	a := app.New()
	a.AddPlugins(Plugin{Headless: true})

	// Load + inspect inside a Startup system: the registries are torn down by the
	// app's AddOnExit hook once Run returns, so we capture what we need live.
	var meshGM, matGM bool
	var keys []string
	var meshBuffers, matBuffers bool
	var meshBounds bool
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		meshes := app.GetResource[MeshRegistry](c)
		if err := meshes.Load("cube", Cube(1)); err != nil {
			t.Errorf("headless mesh Load: %v", err)
			return
		}
		gm := meshes.get("cube")
		meshGM = gm != nil
		if gm != nil {
			meshBuffers = gm.vbuf != nil || gm.ibuf != nil
			meshBounds = gm.boundsRadius > 0 && gm.cpu != nil
		}
		keys = meshes.Keys()

		materials := app.GetResource[MaterialRegistry](c)
		if err := materials.Load("red", Material{BaseColor: Color{R: 1, A: 1}, DoubleSided: true}); err != nil {
			t.Errorf("headless material Load: %v", err)
			return
		}
		mg := materials.get("red")
		matGM = mg != nil
		if mg != nil {
			matBuffers = mg.buf != nil || mg.bindGroup != nil
		}
	})
	a.SetMaxFrames(1).Run()

	if !meshGM {
		t.Error("mesh not registered")
	}
	if meshBuffers {
		t.Error("headless mesh must not upload GPU buffers")
	}
	if !meshBounds {
		t.Error("headless mesh missing CPU bounds/geometry")
	}
	if len(keys) != 1 || keys[0] != "cube" {
		t.Errorf("Keys = %v, want [cube]", keys)
	}
	if !matGM {
		t.Error("material not registered")
	}
	if matBuffers {
		t.Error("headless material must be CPU-only (no GPU buffers/bind group)")
	}
}

// TestHeadlessRunsWorldSystems confirms the world-mutating systems run headless:
// a camera entity drives the Camera3D resource and transform propagation is
// applied, and two runs are deterministic.
func TestHeadlessRunsWorldSystems(t *testing.T) {
	run := func() m.Vec3 {
		a := app.New()
		a.AddPlugins(splititime.Plugin{Manual: true}, Plugin{Headless: true})
		tr := XForm().At(5, 3, -2)
		a.AddSystems(schedule.Startup, func(c *app.Ctx) {
			c.Commands().Add(func(w *ecs.World) {
				lt := tr
				cam := Camera{FovYDeg: 50, Active: true}
				mp := generic.NewMap3[Transform3D, GlobalTransform, Camera](w)
				mp.NewWith(&lt, &GlobalTransform{Matrix: m.Identity4()}, &cam)
			})
		})
		a.SetMaxFrames(10).Run()
		return app.GetResource[Camera3D](a.Ctx()).Position
	}
	got := run()
	want := m.Vec3{X: 5, Y: 3, Z: -2}
	if got.Sub(want).Length() > 1e-5 {
		t.Errorf("camera-driven Position = %v, want %v", got, want)
	}
	if again := run(); again != got {
		t.Errorf("non-deterministic headless run: %v vs %v", got, again)
	}
}
