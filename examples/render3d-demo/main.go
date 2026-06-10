// Command render3d-demo showcases the 3D GPU render backend (plugin/render3d):
// a rotating metallic cube and a plastic sphere sitting on a ground plane, lit by
// a directional "sun" and an orbiting point light, viewed by an orbiting
// perspective camera — all driven by the ordinary spliti app loop and ECS.
//
// It demonstrates the fixed-timestep + interpolation pattern: the spinners step
// on FixedUpdate and are interpolated each rendered frame (time.Alpha()) so
// rotation stays smooth regardless of frame rate. The camera and point light
// orbit on a frame-time clock.
//
// Requires a GPU window, cgo, and a C toolchain:
//
//	CGO_ENABLED=1 go run ./examples/render3d-demo
package main

import (
	"math"
	"runtime"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// The GLFW window must live on the main OS thread. render3d.Plugin.Build also
// locks it, but doing it here documents the requirement.
func init() { runtime.LockOSThread() }

// Spinner makes an entity rotate about Axis at Speed rad/s. Prev/Cur hold the
// accumulated angle at the start/end of the last fixed step; interpolate writes
// the in-between rotation into the entity's Transform3D each rendered frame.
type Spinner struct {
	Axis      m.Vec3
	Speed     float32
	Prev, Cur float32
}

// orbitMarker tags the point-light entity so the camera/light clock can move it.
type orbitMarker struct{ Radius, Height, Speed float32 }

// Clock accumulates wall-clock time to drive the camera and point-light orbits.
type Clock struct{ T float32 }

func main() {
	a := app.New()
	a.AddPlugins(
		splititime.Plugin{TargetFrameRate: 0}, // VSync paces the loop
		render3d.Plugin{
			Width: 1280, Height: 720,
			Title:      "spliti — render3d demo",
			ClearColor: render3d.Color{R: 0.02, G: 0.03, B: 0.05, A: 1},
			Ambient:    m.Vec3{X: 0.04, Y: 0.05, Z: 0.06},
			Samples:    4,
			VSync:      true,
		},
	)
	app.InsertResource(a, &Clock{})

	a.AddSystems(schedule.Startup, setup)
	a.AddSystems(schedule.FixedUpdate, stepSpinners)
	render3d.AddPreRender(a, interpolate)
	a.AddSystems(schedule.Update, quitOnEscape)

	a.Run()
}

func setup(c *app.Ctx) {
	meshes := app.GetResource[render3d.MeshRegistry](c)
	materials := app.GetResource[render3d.MaterialRegistry](c)

	must(meshes.Load("cube", render3d.Cube(1.2)))
	must(meshes.Load("sphere", render3d.UVSphere(0.75, 48, 32)))
	must(meshes.Load("plane", render3d.Plane(20, 20, 1, 1)))

	must(materials.Load("metal", render3d.Material{
		BaseColor: render3d.Color{R: 1.0, G: 0.78, B: 0.34, A: 1}, // gold
		Metallic:  1.0, Roughness: 0.25,
	}))
	must(materials.Load("plastic", render3d.Material{
		BaseColor: render3d.Color{R: 0.85, G: 0.15, B: 0.18, A: 1}, // red
		Metallic:  0.0, Roughness: 0.4,
	}))
	must(materials.Load("ground", render3d.Material{
		BaseColor: render3d.Color{R: 0.5, G: 0.5, B: 0.55, A: 1},
		Metallic:  0.0, Roughness: 0.9,
	}))

	cmd := c.Commands()

	// Ground plane at y=0.
	render3d.SpawnMesh(cmd, render3d.NewTransform3D(m.Vec3{}), "plane", "ground")

	// Spinning cube (sits on the plane: half-height 0.6) and sphere (radius 0.75).
	spawnSpinner(cmd, render3d.NewTransform3D(m.Vec3{X: -1.4, Y: 0.6, Z: 0}), "cube", "metal",
		Spinner{Axis: m.Vec3{X: 0.3, Y: 1, Z: 0}, Speed: 1.0})
	spawnSpinner(cmd, render3d.NewTransform3D(m.Vec3{X: 1.4, Y: 0.75, Z: 0}), "sphere", "plastic",
		Spinner{Axis: m.Vec3{Y: 1}, Speed: 0.6})

	// Sun.
	render3d.SpawnDirectionalLight(cmd, render3d.DirectionalLight{
		Direction: m.Vec3{X: -0.4, Y: -1, Z: -0.3},
		Color:     m.Vec3{X: 1, Y: 0.98, Z: 0.92},
		Intensity: 3.0,
	})

	// Orbiting point light (warm).
	cmd.Add(func(w *ecs.World) {
		mp := generic.NewMap4[render3d.Transform3D, render3d.GlobalTransform, render3d.PointLight, orbitMarker](w)
		t := render3d.NewTransform3D(m.Vec3{X: 0, Y: 2.5, Z: 0})
		mp.NewWith(&t, &render3d.GlobalTransform{Matrix: m.Identity4()},
			&render3d.PointLight{Color: m.Vec3{X: 1, Y: 0.6, Z: 0.3}, Intensity: 60, Range: 14},
			&orbitMarker{Radius: 3.5, Height: 2.5, Speed: 1.2})
	})
}

// spawnSpinner spawns a renderable that also carries a Spinner (5 components, so
// it goes through arche's generic map directly rather than render3d.SpawnMesh).
func spawnSpinner(cmd *app.Commands, t render3d.Transform3D, mesh, material string, sp Spinner) {
	cmd.Add(func(w *ecs.World) {
		mp := generic.NewMap5[render3d.Transform3D, render3d.GlobalTransform, render3d.MeshRenderer, render3d.MaterialRef, Spinner](w)
		mp.NewWith(&t, &render3d.GlobalTransform{Matrix: m.Identity4()},
			&render3d.MeshRenderer{Mesh: mesh}, &render3d.MaterialRef{Material: material}, &sp)
	})
}

// stepSpinners advances each spinner's angle on the fixed timestep.
func stepSpinners(c *app.Ctx) {
	t := app.GetResource[splititime.Time](c)
	dt := float32(t.FixedDelta().Seconds())
	app.Query1[Spinner](c, func(_ ecs.Entity, s *Spinner) {
		s.Prev = s.Cur
		s.Cur += s.Speed * dt
	})
}

// interpolate runs each rendered frame before the render pass: it writes the
// interpolated spinner rotations and advances the camera + point-light orbit.
func interpolate(c *app.Ctx) {
	t := app.GetResource[splititime.Time](c)
	if t == nil {
		return
	}
	alpha := float32(t.Alpha())
	app.Query2[render3d.Transform3D, Spinner](c, func(_ ecs.Entity, tr *render3d.Transform3D, s *Spinner) {
		angle := s.Prev + (s.Cur-s.Prev)*alpha
		tr.Rotation = m.FromAxisAngle(s.Axis, angle)
	})

	// Camera + point light orbit on a frame-time clock.
	clock := app.GetResource[Clock](c)
	clock.T += float32(t.Delta().Seconds())

	cam := app.GetResource[render3d.Camera3D](c)
	if cam != nil {
		cam.Orbit(m.Vec3{Y: 0.6}, 7, clock.T*0.25, 0.45)
	}

	app.Query2[render3d.Transform3D, orbitMarker](c, func(_ ecs.Entity, tr *render3d.Transform3D, o *orbitMarker) {
		a := float64(clock.T * o.Speed)
		tr.Translation = m.Vec3{
			X: o.Radius * float32(math.Cos(a)),
			Y: o.Height,
			Z: o.Radius * float32(math.Sin(a)),
		}
	})
}

func quitOnEscape(c *app.Ctx) {
	for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
		if ev.Key == inputs.KeyEscape && ev.Action == inputs.Press {
			c.App().Stop()
		}
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
