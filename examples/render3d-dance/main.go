// Command render3d-dance loads a rigged, animated humanoid (the Khronos CesiumMan
// sample, CC0) and plays its skeletal animation on a lit stage with an orbiting
// camera — a "dancing model in a scene" end to end.
//
// Everything that makes this work already lives in the render3d backend:
// LoadGLTFFS parses the skin (JOINTS_0/WEIGHTS_0 + inverse-bind matrices) and the
// animation clips, SpawnModel wires up an Animator that drives the joint nodes each
// frame, and the skinned pipeline blends the joint matrices per vertex on the GPU.
//
// The .glb assets are downloaded from the Khronos glTF-Sample-Assets repo and
// embedded in the binary, so the example is self-contained.
//
// Controls: C cycles animation clips, Space pauses/resumes, Esc quits.
//
// Requires a GPU window, cgo, and a C toolchain:
//
//	CGO_ENABLED=1 go run ./examples/render3d-dance
package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/plugin/screenshot"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
)

//go:embed assets/*
var assets embed.FS

// modelFile picks which rigged model to dance. CesiumMan is an upright humanoid
// (~1.6 units tall, one clip). Swap to "assets/Fox.glb" for a quadruped with three
// clips (Survey/Walk/Run) — set foxScale below and cycle clips with C.
const modelFile = "assets/CesiumMan.glb"

// clock is a tiny resource holding accumulated time for the camera orbit.
type clock struct{ t float32 }

func init() { runtime.LockOSThread() }

func main() {
	a := app.New()
	a.AddPlugins(
		splititime.Plugin{TargetFrameRate: 0},
		render3d.Plugin{
			Width: 1280, Height: 720,
			Title:      "spliti — dancing model",
			ClearColor: render3d.Color{R: 0.04, G: 0.04, B: 0.07, A: 1},
			Position:   m.Vec3{X: 0, Y: 1.1, Z: 3.5},
			Target:     m.Vec3{X: 0, Y: 0.9, Z: 0},
			Samples:    4,
			VSync:      true,
		},
	)
	app.InsertResource(a, &clock{})
	a.AddSystems(schedule.Startup, setup)
	a.AddSystems(schedule.Update, orbitCamera, controls)
	// SPLITI_DANCE_SHOTS=<dir> turns this into a self-driving capture session:
	// it stops the camera orbit, grabs frames at several animation times, and quits.
	if dir := os.Getenv("SPLITI_DANCE_SHOTS"); dir != "" {
		app.InsertResource(a, &capture{dir: dir})
		a.AddSystems(schedule.Update, captureShots)
	}
	a.Run()
}

func setup(c *app.Ctx) {
	meshes := app.GetResource[render3d.MeshRegistry](c)
	materials := app.GetResource[render3d.MaterialRegistry](c)

	// Stage: a matte ground plane to ground the figure.
	must(meshes.Load("plane", render3d.Plane(20, 20, 1, 1)))
	must(materials.Load("ground", render3d.Material{
		BaseColor: render3d.Color{R: 0.16, G: 0.17, B: 0.2, A: 1},
		Metallic:  0.0, Roughness: 0.95,
	}))
	render3d.SpawnMesh(c.Commands(), render3d.NewTransform3D(m.Vec3{}), "plane", "ground")

	// Load the rigged model. The skin + animation clips come along automatically.
	model, err := render3d.LoadGLTFFS(meshes, materials, "dancer", assets, modelFile)
	if err != nil {
		panic(err)
	}
	names := make([]string, len(model.Animations))
	for i, an := range model.Animations {
		names[i] = an.Name
	}
	log.Printf("render3d-dance: %s — %d nodes, %d skins, %d clips %v",
		modelFile, len(model.Nodes), len(model.Skins), len(model.Animations), names)

	// SpawnModel attaches an Animator playing clip 0, looped. CesiumMan stands at
	// the origin already; the Fox is ~100x too big, so scale it down if used.
	t := render3d.NewTransform3D(m.Vec3{})
	if modelFile == "assets/Fox.glb" {
		t = t.Scaled(0.02, 0.02, 0.02)
	}
	render3d.SpawnModel(c.Commands(), model, t)

	// Lighting: a key sun plus a warm fill so the silhouette reads while it moves.
	render3d.SpawnDirectionalLight(c.Commands(),
		render3d.XForm().Facing(m.Vec3{X: -0.4, Y: -1, Z: -0.35}),
		render3d.DirectionalLight{Color: m.Vec3{X: 1, Y: 0.98, Z: 0.92}, Intensity: 3.2})
	render3d.SpawnPointLight(c.Commands(),
		render3d.NewTransform3D(m.Vec3{X: 2, Y: 2, Z: 2}),
		render3d.PointLight{Color: m.Vec3{X: 1, Y: 0.7, Z: 0.4}, Intensity: 6, Range: 12})
}

// orbitCamera slowly circles the stage so the animation is visible from all sides.
func orbitCamera(c *app.Ctx) {
	t := app.GetResource[splititime.Time](c)
	ck := app.GetResource[clock](c)
	ck.t += float32(t.Delta().Seconds())
	if cam := app.GetResource[render3d.Camera3D](c); cam != nil {
		cam.Orbit(m.Vec3{Y: 0.9}, 3.6, ck.t*0.3, 0.25)
	}
}

// controls cycles clips (C), pauses/resumes (Space), and quits (Esc).
func controls(c *app.Ctx) {
	for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
		if ev.Action != inputs.Press {
			continue
		}
		switch ev.Key {
		case inputs.KeyEscape:
			c.App().Stop()
		case inputs.KeySpace:
			app.Query1[render3d.Animator](c, func(_ ecs.Entity, a *render3d.Animator) {
				a.Playing = !a.Playing
			})
		case inputs.KeyC:
			app.Query1[render3d.Animator](c, func(_ ecs.Entity, a *render3d.Animator) {
				if n := len(a.Model.Animations); n > 0 {
					a.Clip = (a.Clip + 1) % n
					a.Time = 0
					log.Printf("clip -> %d (%s)", a.Clip, a.Model.Animations[a.Clip].Name)
				}
			})
		}
	}
}

// capture drives the screenshot session.
type capture struct {
	dir   string
	frame int
	idx   int
	shot  bool // screenshot already requested for the current target
}

// shotFrames are the frame numbers at which to grab a PNG. They're spaced across
// the ~2s clip so the captured poses differ — if the skeleton animates, the four
// images show four different body positions from a fixed camera.
var shotFrames = []int{45, 70, 95, 120}

// captureShots pins the camera, lets the animation play, and writes one frame at
// each entry of shotFrames, then quits. The camera is held still so any change
// between frames is purely the skeletal pose moving.
func captureShots(c *app.Ctx) {
	cap := app.GetResource[capture](c)
	if cap.idx >= len(shotFrames) {
		c.App().Stop()
		return
	}
	// Hold the camera at a fixed three-quarter view (overrides orbitCamera).
	if cam := app.GetResource[render3d.Camera3D](c); cam != nil {
		cam.Position = m.Vec3{X: 2.0, Y: 1.2, Z: 3.0}
		cam.Target = m.Vec3{X: 0, Y: 0.9, Z: 0}
	}
	cap.frame++
	if cap.frame >= shotFrames[cap.idx] && !cap.shot {
		var t float32
		app.Query1[render3d.Animator](c, func(_ ecs.Entity, a *render3d.Animator) { t = a.Time })
		path := fmt.Sprintf("%s/dance-%d-f%d-t%.2f.png", cap.dir, cap.idx, cap.frame, t)
		if screenshot.Save(c, path) {
			log.Printf("captured %s (clip time %.2fs)", path, t)
			cap.shot = true
		}
	}
	if cap.shot {
		// Screenshot is written on the next present; advance after one frame.
		cap.idx++
		cap.shot = false
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
