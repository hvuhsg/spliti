// Command radiosim is an interactive, physically-accurate 3D radio-wave
// propagation simulator built on plugin/render3d. It descends from the radio3d
// example but replaces the propagation model with a band-aware engine behind a
// swappable seam (examples/radiosim/sim): the same per-interaction physics —
// Fresnel reflection/transmission, UTD diffraction, a thermal noise floor — feeds
// either an exact image-method engine (the accuracy oracle) or a real-time
// shooting-bouncing-rays engine, so the numbers a receiver reports are meant to
// match reality across sub-6 GHz and mmWave bands.
//
// A transmitter on a procedurally generated street emits waves that propagate,
// reflect, refract, and (in later phases) diffract; a draggable receiver shows
// what it receives. This Phase-0 build wires the new sim package through the
// Engine seam and reproduces the radio3d visualization — coverage heatmap,
// wavefront shells, contributing rays, and a receiver HUD.
//
// Controls:
//
//	W/A/S/D     move (horizontal), Q/E down/up
//	right-drag  look around
//	left-drag   move the receiver along the ground
//	arrow keys  move the transmitter (recomputes the coverage heatmap)
//	1/2/3       band preset: 2.4 GHz / 28 GHz / 60 GHz
//	T           toggle heavy rain (atmospheric loss)
//	M           cycle wall material (concrete/brick/glass/wood)
//	G           toggle engine: exact image method ↔ real-time SBR
//	H           toggle the heatmap, R toggle rays, space toggle wavefronts
//	Esc         quit
//
// Requires a GPU window, cgo, and a C toolchain:
//
//	CGO_ENABLED=1 go run ./examples/radiosim
package main

import (
	"runtime"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
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

// Scene holds the propagation world: the physics scene (faces + BVH), the
// building boxes, the transmitter, the coverage grid, and the engine config. It
// is the single source of truth shared by the sim and the viz.
type Scene struct {
	Sim       *sim.Scene
	Buildings []building
	Tx        sim.Tx
	Cfg       sim.Config
	Grid      sim.Grid
	RxHeight  float32
	WallMat   sim.MaterialID // material currently painted on every building wall

	// recompute requests a coverage re-evaluation (set when the Tx moves).
	recompute bool
}

// Engine holds the active propagation engine behind the sim.Engine interface, so
// swapping the image-method engine for the real-time SBR engine (or a GPU engine)
// is a one-line change here and invisible to the rest of the app.
type Engine struct{ sim.Engine }

// Coverage is the latest evaluated coverage field plus its dynamic range, used to
// normalize the heatmap colors.
type Coverage struct {
	Field          []float64
	MinDBm, MaxDBm float64
}

// Live holds the active (draggable) receiver's configuration and its per-frame
// result: the contributing paths, received power, and the multipath taps. Rx
// carries the antenna and front-end parameters that set the noise floor, so the
// HUD can report SNR and BER alongside raw power.
type Live struct {
	Rx     sim.Rx
	Paths  []sim.Path
	PowerW float64
	Taps   []sim.Tap
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
			Title:      "spliti — radiosim",
			ClearColor: render3d.Color{R: 0.02, G: 0.03, B: 0.05, A: 1},
			Ambient:    m.Vec3{X: 0.05, Y: 0.06, Z: 0.08},
			Samples:    4,
			VSync:      true,
		},
	)

	app.InsertResource(a, newScene())
	app.InsertResource(a, &Engine{Engine: sim.ImageEngine{}})
	app.InsertResource(a, &Coverage{})
	app.InsertResource(a, &Live{Rx: sim.Rx{
		Ant:         sim.Antenna{Kind: sim.Isotropic},
		BandwidthHz: 20e6, // 20 MHz channel
		NoiseFigDB:  7,
		TempK:       290,
		Mod:         sim.QPSK,
	}})
	app.InsertResource(a, &CamCtl{})
	app.InsertResource(a, &View{ShowHeatmap: true, ShowRays: true, ShowWavefront: true})
	app.InsertResource(a, &HUD{})
	app.InsertResource(a, &UI{})

	a.AddSystems(schedule.Startup, setup)
	a.AddSystems(schedule.Update, controlsSystem)
	a.AddSystems(schedule.Update, coverageSystem)
	render3d.AddPreRender(a, vizSystem)
	a.AddSystems(schedule.Update, quitOnEscape)

	a.Run()
}

func quitOnEscape(c *app.Ctx) {
	for _, ev := range app.ReadEvents[render3d.KeyEvent](c) {
		if ev.Key == glfw.KeyEscape && ev.Action == glfw.Press {
			c.App().Stop()
		}
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
