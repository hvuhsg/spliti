// Command radio3d is an interactive 3D radio-wave propagation visualizer built on
// plugin/render3d. A transmitter on a procedurally generated street emits waves
// that propagate, reflect off buildings, and decay; a draggable receiver shows
// what it receives (signal strength and the multipath profile). It demonstrates
// the renderer features added for this app — per-instance color (the coverage
// heatmap), the transparent pass (expanding wavefront shells), line gizmos (the
// contributing ray paths), picking (dragging the receiver), and the 2D overlay
// (the HUD) — driven by the pure, GPU-free propagation model in ./prop.
//
// Controls:
//
//	W/A/S/D     move (horizontal), Q/E down/up
//	right-drag  look around
//	left-drag   move the receiver along the ground
//	arrow keys  move the transmitter (recomputes the coverage heatmap)
//	H           toggle the heatmap, R toggle rays, space toggle wavefronts
//	Esc         quit
//
// Requires a GPU window, cgo, and a C toolchain:
//
//	CGO_ENABLED=1 go run ./examples/radio3d
package main

import (
	"runtime"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radio3d/prop"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"
)

// The GLFW window must live on the main OS thread.
func init() { runtime.LockOSThread() }

// --- components ---

// heatCell tags one coverage-heatmap quad with its grid coordinates so the viz
// system can recolor it after a coverage recompute.
type heatCell struct{ Ix, Iz int }

// shell tags an expanding translucent wavefront sphere with a phase offset, so a
// pool of shells emit at staggered times.
type shell struct{ Offset float32 }

// receiverTag marks the single draggable receiver entity.
type receiverTag struct{}

// txTag marks the transmitter entity (its transform follows Scene.Tx.Pos).
type txTag struct{}

// --- resources ---

// Scene holds the propagation world: the static faces + BVH, the building boxes,
// the transmitter, the coverage grid, and the config. It is the single source of
// truth shared by the sim and the viz.
type Scene struct {
	Faces     []prop.Face
	BVH       *prop.BVH
	Buildings []building
	Tx        prop.Tx
	Cfg       prop.Config
	Grid      prop.Grid
	RxHeight  float32

	// recompute requests a coverage re-evaluation (set when the Tx moves).
	recompute bool
}

// Coverage is the latest evaluated coverage field plus its dynamic range, used to
// normalize the heatmap colors.
type Coverage struct {
	Field          []float64
	MinDBm, MaxDBm float64
}

// Sim holds the per-frame result for the active (draggable) receiver: its
// position, the contributing paths, received power, and the multipath taps.
type Sim struct {
	RxPos  m.Vec3
	Paths  []prop.Path
	PowerW float64
	Taps   []prop.Tap
}

// View holds UI toggles and the wavefront-animation clock.
type View struct {
	T             float32
	ShowHeatmap   bool
	ShowRays      bool
	ShowWavefront bool
}

func main() {
	a := app.New()
	a.AddPlugins(
		splititime.Plugin{TargetFrameRate: 0},
		render3d.Plugin{
			Width: 1280, Height: 720,
			Title:      "spliti — radio3d propagation",
			ClearColor: render3d.Color{R: 0.02, G: 0.03, B: 0.05, A: 1},
			Ambient:    m.Vec3{X: 0.05, Y: 0.06, Z: 0.08},
			Samples:    4,
			VSync:      true,
		},
	)

	app.InsertResource(a, newScene())
	app.InsertResource(a, &Coverage{})
	app.InsertResource(a, &Sim{})
	app.InsertResource(a, &CamCtl{})
	app.InsertResource(a, &View{ShowHeatmap: true, ShowRays: true, ShowWavefront: true})
	app.InsertResource(a, &HUD{})

	a.AddSystems(schedule.Startup, setup)
	a.AddSystems(schedule.Update, controlsSystem)
	a.AddSystems(schedule.Update, coverageSystem)
	render3d.AddPreRender(a, vizSystem)
	a.AddSystems(schedule.Update, quitOnEscape)

	a.Run()
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
