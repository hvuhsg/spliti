// Command render3d-model loads a textured glTF model with the render3d backend's
// glTF loader and renders it with full PBR (base-color / metallic-roughness /
// normal maps) under a directional "sun" and a point light, viewed by an
// orbiting camera. Frustum culling runs automatically — entities whose bounds
// leave the view are skipped before batching.
//
// The model (a Khronos BoxTextured sample) is embedded in the binary, so the
// example is self-contained.
//
// Requires a GPU window, cgo, and a C toolchain:
//
//	CGO_ENABLED=1 go run ./examples/render3d-model
package main

import (
	"embed"
	"log"
	"runtime"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"
)

//go:embed assets/BoxTextured.glb
var assets embed.FS

// The GLFW window must live on the main OS thread.
func init() { runtime.LockOSThread() }

// Clock accumulates wall-clock time to drive the orbiting camera.
type Clock struct{ T float32 }

func main() {
	a := app.New()
	a.AddPlugins(
		splititime.Plugin{TargetFrameRate: 0}, // VSync paces the loop
		render3d.Plugin{
			Width: 1280, Height: 720,
			Title:      "spliti — render3d model loader",
			ClearColor: render3d.Color{R: 0.02, G: 0.03, B: 0.05, A: 1},
			Ambient:    m.Vec3{X: 0.05, Y: 0.06, Z: 0.07},
			Samples:    4,
			VSync:      true,
		},
	)
	app.InsertResource(a, &Clock{})

	a.AddSystems(schedule.Startup, setup)
	render3d.AddPreRender(a, orbitCamera)
	a.AddSystems(schedule.Update, quitOnEscape)

	a.Run()
}

func setup(c *app.Ctx) {
	meshes := app.GetResource[render3d.MeshRegistry](c)
	materials := app.GetResource[render3d.MaterialRegistry](c)

	// Load the embedded glTF model. Its meshes and materials (with the embedded
	// base-color texture) are registered automatically; SpawnModel turns the node
	// tree into entities.
	model, err := render3d.LoadGLTFFS(meshes, materials, "box", assets, "assets/BoxTextured.glb")
	if err != nil {
		panic(err)
	}
	log.Printf("render3d-model: loaded glTF with %d nodes, %d roots", len(model.Nodes), len(model.Roots))
	render3d.SpawnModel(c.Commands(), model, render3d.NewTransform3D(m.Vec3{}))

	// A ground plane beneath the model to give it scale and ground the lighting.
	must(meshes.Load("plane", render3d.Plane(20, 20, 1, 1)))
	must(materials.Load("ground", render3d.Material{
		BaseColor: render3d.Color{R: 0.35, G: 0.37, B: 0.4, A: 1},
		Roughness: 0.9,
	}))
	render3d.SpawnMesh(c.Commands(),
		render3d.NewTransform3D(m.Vec3{X: 0, Y: -1.5, Z: 0}), "plane", "ground")

	// Lighting: a directional "sun" plus a warm point light.
	render3d.SpawnDirectionalLight(c.Commands(), render3d.DirectionalLight{
		Direction: m.Vec3{X: -0.4, Y: -1, Z: -0.3},
		Color:     m.Vec3{X: 1, Y: 0.97, Z: 0.9},
		Intensity: 3,
	})
	render3d.SpawnPointLight(c.Commands(),
		render3d.NewTransform3D(m.Vec3{X: 2.5, Y: 2.5, Z: 2.5}),
		render3d.PointLight{Color: m.Vec3{X: 1, Y: 0.8, Z: 0.6}, Intensity: 8, Range: 12})
}

// orbitCamera circles the camera around the model so all faces (and the texture)
// are visible, and so frustum culling has something to do as geometry leaves and
// re-enters the view.
func orbitCamera(c *app.Ctx) {
	clk := app.GetResource[Clock](c)
	cam := app.GetResource[render3d.Camera3D](c)
	tm := app.GetResource[splititime.Time](c)
	if clk == nil || cam == nil || tm == nil {
		return
	}
	clk.T += float32(tm.Delta().Seconds())
	yaw := clk.T * 0.4
	cam.Orbit(m.Vec3{X: 0, Y: 0, Z: 0}, 4, yaw, 0.4)
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
