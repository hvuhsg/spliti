// Command rf-lab is an interactive RF learning playground built on
// plugin/render3d. A ground plane carries transmitters, receivers, and
// line-of-sight blocks, each of which you can click to select and drag to move.
// A bottom toolbar lets you drag new objects into the scene and drag existing
// ones back onto it to remove them; you can also add objects by key (T / R / B),
// so you can watch several sources interfere and fall into shadow behind a block.
// When one is selected a config panel pops up on the right: for a transmitter the
// sliders change its power and frequency, for a receiver they tune its listening
// frequency and antenna gain and noise figure, and for a block they resize it. A
// receiver only hears transmitters within its bandwidth of the frequency it is
// tuned to, and a block standing on the path heavily attenuates the link. A
// readout in the top-left shows the live link budget — received power (dBm),
// distance, and SNR — for the focused pair, computed from the free-space (Friis)
// path-loss model, so you can watch how power, frequency, distance, and
// obstacles trade off. A "Channel" panel toggles multipath: with it on, the wave
// also reaches the receiver by bouncing off the wall blocks, and the reflected
// copies interfere with the direct path — you can watch standing waves band the
// ground, the constellation spread and rotate, and the link fade in and out as
// you drag the receiver a few wavelengths or move a wall.
//
// Controls:
//
//	W/A/S/D     move the camera (horizontal), Q/E down/up
//	right-drag  look around
//	left-click  select a transmitter, receiver, or block (click empty ground to deselect)
//	left-drag   move the selected object along the ground
//	toolbar     drag a palette button onto the ground to add; drag an object onto the bar to remove
//	↑/↓ ←/→     adjust the selected marker's parameters (see the config panel)
//	[ / ]       receiver noise figure
//	T / R / B   add a transmitter / receiver / block (the new one is selected)
//	Del / ⌫     remove the selected object
//	Esc         quit
//
// Requires a GPU window, cgo, and a C toolchain:
//
//	CGO_ENABLED=1 go run ./examples/rf-lab
package main

import (
	"os"
	"runtime"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	splitiui "github.com/hvuhsg/spliti/plugin/ui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
)

// The GLFW window must live on the main OS thread.
func init() { runtime.LockOSThread() }

// --- components ---

// txTag marks a transmitter marker entity (the pickable head sphere). There may
// be any number of them.
type txTag struct{}

// rxTag marks a receiver marker entity (the pickable head sphere). There may be
// any number of them.
type rxTag struct{}

// --- selection ---

// Selection identifies the kind of marker, if any, that is currently selected.
type Selection int

const (
	SelNone Selection = iota
	SelTx
	SelRx
	SelBlock
)

// --- resources ---

// Lab holds the scene-level selection: which kind of marker is selected and which
// specific entity it is. Per-device radio settings and signal chains live on the
// TxDevice / RxDevice components (see device.go) and per-device positions live in
// each marker's Transform3D, so every device is independent.
type Lab struct {
	Sel Selection  // kind of the current selection (SelNone when nothing is selected)
	Ent ecs.Entity // the selected marker entity (valid when Sel != SelNone)
}

// Link holds the per-frame link readout. The signal quantities are measured from
// the sampled wave by fieldSystem (so they match what the decoder sees); only the
// geometric distance is set by rfSystem.
type Link struct {
	PowerW float64 // measured in-band received power, watts (direct + multipath)
	SNRdB  float64 // SNR of that power against the thermal floor, dB (modeled)
	EVMdB  float64 // SNR measured from the constellation scatter (EVM), dB; +Inf = none
	DistM  float64 // Tx–Rx distance, meters (geometry)
}

// Channel holds the global propagation settings. With Multipath off the lab uses
// the single direct line-of-sight path it always has; turning it on adds specular
// reflections off the wall blocks (see multipath.go), which interfere with the
// direct path to produce ground standing waves, constellation fading, and the
// reflected rays drawn in the scene.
type Channel struct {
	Multipath    bool    // wall-reflection master toggle; off ⇒ no wall bounces
	MaxOrder     int     // reflection bounces to model, 1..maxRefl
	Reflectivity float64 // per-bounce amplitude coefficient, 0..1

	// Ground reflection is the classic two-ray model: a single bounce off the ground
	// plane that interferes with the direct path. It needs no walls, so it is an
	// independent toggle; off by default so the bare lab is a clean single path.
	Ground     bool    // enable the ground bounce (two-ray fading)
	GroundRefl float64 // ground reflection magnitude, 0..1 (phase flips at grazing)
}

func newChannel() *Channel {
	return &Channel{Multipath: false, MaxOrder: 1, Reflectivity: 0.6, Ground: false, GroundRefl: 0.9}
}

const markerHeight = 2.5 // antenna head height above the ground, meters
const planeHalf = 78.0   // drag clamp half-extent (the ground is 160 wide)

func newLab() *Lab { return &Lab{Sel: SelNone} }

func main() {
	a := app.New()
	a.AddPlugins(
		splititime.Plugin{TargetFrameRate: 0},
		render3d.Plugin{
			Width: 1280, Height: 720,
			Title:      "spliti — rf-lab",
			ClearColor: render3d.Color{R: 0.02, G: 0.03, B: 0.05, A: 1},
			Ambient:    m.Vec3{X: 0.06, Y: 0.07, Z: 0.09},
			Samples:    4,
			VSync:      true,
		},
		splitiui.Plugin{}, // Dear ImGui GPU UI; must follow render3d.Plugin
	)

	app.InsertResource(a, newLab())
	app.InsertResource(a, &Link{})
	app.InsertResource(a, newChannel())
	app.InsertResource(a, newField())
	app.InsertResource(a, &CamCtl{})
	app.InsertResource(a, newUI())
	app.InsertResource(a, newEditor())
	app.InsertResource(a, newDragDrop())

	if dir, ok := captureEnabled(); ok {
		must(os.MkdirAll(dir, 0o755))
		app.InsertResource(a, newCapturePlan(dir))
		a.AddSystems(schedule.Update, captureSystem) // drives the scripted screenshot run
	}

	a.AddSystems(schedule.Startup, setup)
	a.AddSystems(schedule.Update, controlsSystem)
	a.AddSystems(schedule.Update, hudUI)     // ImGui readout + config drawer
	a.AddSystems(schedule.Update, toolbarUI) // ImGui bottom object palette (drag to add/remove)
	a.AddSystems(schedule.Update, editorUI)  // ImGui node-graph editor (graph mode)
	a.AddSystems(schedule.Update, playbackSystem)
	a.AddSystems(schedule.Update, rfSystem)
	render3d.AddPreRender(a, fieldSystem)
	render3d.AddPreRender(a, raysSystem)  // gizmo overlay: direct + reflected rays
	render3d.AddPreRender(a, plotsSystem) // overlay2d scope + constellation plots
	a.AddSystems(schedule.Update, quitOnEscape)

	a.Run()
}

func quitOnEscape(c *app.Ctx) {
	// In graph mode Escape is handled by the editor (cancel edit / leave mode).
	if ui := app.GetResource[UI](c); ui != nil && ui.Mode == ModeGraph {
		return
	}
	// Don't quit while ImGui owns the keyboard (e.g. Escape cancelling a focused
	// slider/text field).
	if splitiui.WantCaptureKeyboard(c) {
		return
	}
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
