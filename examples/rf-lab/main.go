// Command rf-lab is an interactive RF learning playground built on
// plugin/render3d. A ground plane carries a transmitter and a receiver, each of
// which you can click to select and drag to move. When one is selected a config
// panel pops up on the right: for the transmitter the arrow keys change its power
// and frequency, for the receiver they change its antenna gain and noise figure.
// A readout in the top-left shows the live link budget — received power (dBm),
// distance, and SNR — computed from the free-space (Friis) path-loss model, so
// you can watch how power, frequency, and distance trade off.
//
// Controls:
//
//	W/A/S/D     move the camera (horizontal), Q/E down/up
//	right-drag  look around
//	left-click  select the transmitter or receiver (click empty ground to deselect)
//	left-drag   move the selected marker along the ground
//	↑/↓ ←/→     adjust the selected marker's parameters (see the config panel)
//	Esc         quit
//
// Requires a GPU window, cgo, and a C toolchain:
//
//	CGO_ENABLED=1 go run ./examples/rf-lab
package main

import (
	"runtime"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"
)

// The GLFW window must live on the main OS thread.
func init() { runtime.LockOSThread() }

// --- components ---

// txTag marks the single transmitter marker entity (the pickable head sphere).
type txTag struct{}

// rxTag marks the single receiver marker entity (the pickable head sphere).
type rxTag struct{}

// --- selection ---

// Selection identifies which marker, if any, is currently selected.
type Selection int

const (
	SelNone Selection = iota
	SelTx
	SelRx
)

// --- resources ---

// Lab holds the scene-level state shared across devices: the transmitter and
// receiver positions and the current selection. Per-device radio settings and
// signal chains live on the TxDevice / RxDevice components (see device.go), so
// each device can differ. The marker transforms follow these positions.
type Lab struct {
	TxPos m.Vec3
	RxPos m.Vec3
	Sel   Selection
}

// Link holds the per-frame link budget computed by rfSystem from the Lab state.
type Link struct {
	PowerW float64 // received power, watts
	SNRdB  float64 // signal-to-noise ratio, dB
	DistM  float64 // Tx–Rx distance, meters
}

const markerHeight = 2.5 // antenna head height above the ground, meters
const planeHalf = 78.0   // drag clamp half-extent (the ground is 160 wide)

func newLab() *Lab {
	return &Lab{
		TxPos: m.Vec3{X: -30, Y: markerHeight, Z: 0},
		RxPos: m.Vec3{X: 30, Y: markerHeight, Z: 0},
		Sel:   SelNone,
	}
}

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
	)

	app.InsertResource(a, newLab())
	app.InsertResource(a, &Link{})
	app.InsertResource(a, newField())
	app.InsertResource(a, &CamCtl{})
	app.InsertResource(a, &HUD{})
	app.InsertResource(a, newUI())
	app.InsertResource(a, newEditor())

	a.AddSystems(schedule.Startup, setup)
	a.AddSystems(schedule.Update, editorSystem) // before controls: owns input in graph mode
	a.AddSystems(schedule.Update, controlsSystem)
	a.AddSystems(schedule.Update, playbackSystem)
	a.AddSystems(schedule.Update, rfSystem)
	render3d.AddPreRender(a, fieldSystem)
	render3d.AddPreRender(a, hudSystem)
	a.AddSystems(schedule.Update, quitOnEscape)

	a.Run()
}

func quitOnEscape(c *app.Ctx) {
	// In graph mode Escape is handled by the editor (cancel edit / leave mode).
	if ui := app.GetResource[UI](c); ui != nil && ui.Mode == ModeGraph {
		return
	}
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
