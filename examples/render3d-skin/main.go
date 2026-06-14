// Command render3d-skin loads the Khronos SimpleSkin model — a mesh bound to a
// two-joint skeleton with a bend animation — and plays it with the render3d
// backend's glTF animation + GPU skeletal-skinning path. SpawnModel attaches an
// Animator that drives the joint nodes each frame; the skinned pipeline blends the
// joint matrices per vertex so the mesh deforms with the skeleton.
//
// The model (a Khronos sample, CC0) is embedded in the binary, so the example is
// self-contained.
//
// Requires a GPU window, cgo, and a C toolchain:
//
//	CGO_ENABLED=1 go run ./examples/render3d-skin
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

//go:embed assets/*
var assets embed.FS

func init() { runtime.LockOSThread() }

func main() {
	a := app.New()
	a.AddPlugins(
		splititime.Plugin{TargetFrameRate: 0},
		render3d.Plugin{
			Width: 1280, Height: 720,
			Title:      "spliti — render3d skinning",
			ClearColor: render3d.Color{R: 0.06, G: 0.06, B: 0.09, A: 1},
			Position:   m.Vec3{X: 0, Y: 1, Z: 3.5},
			Target:     m.Vec3{X: 0, Y: 1, Z: 0},
			Samples:    4,
			VSync:      true,
		},
	)
	a.AddSystems(schedule.Startup, setup)
	a.AddSystems(schedule.Update, quitOnEscape)
	a.Run()
}

func setup(c *app.Ctx) {
	meshes := app.GetResource[render3d.MeshRegistry](c)
	materials := app.GetResource[render3d.MaterialRegistry](c)

	// LoadGLTFFS reads the embedded glTF (and its external .bin buffers) from the
	// embed.FS. The skinned primitive's JOINTS_0/WEIGHTS_0 + the skin's inverse-
	// bind matrices are loaded automatically; SpawnModel wires up the Animator.
	model, err := render3d.LoadGLTFFS(meshes, materials, "skin", assets, "assets/SimpleSkin.gltf")
	if err != nil {
		panic(err)
	}
	log.Printf("render3d-skin: %d nodes, %d skins, %d animations", len(model.Nodes), len(model.Skins), len(model.Animations))
	render3d.SpawnModel(c.Commands(), model, render3d.NewTransform3D(m.Vec3{}))

	render3d.SpawnDirectionalLight(c.Commands(),
		render3d.XForm().EulerDeg(-25, 15, 0),
		render3d.DirectionalLight{Color: m.Vec3{X: 1, Y: 1, Z: 1}, Intensity: 3})
}

func quitOnEscape(c *app.Ctx) {
	for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
		if ev.Key == inputs.KeyEscape && ev.Action == inputs.Press {
			c.App().Stop()
		}
	}
}
