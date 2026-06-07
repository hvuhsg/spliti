package main

import (
	"fmt"
	"math"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
	"github.com/hvuhsg/spliti/plugin/render3d"
	splitiui "github.com/hvuhsg/spliti/plugin/ui"
	"github.com/mlange-42/arche/ecs"
)

// hudUI draws the always-on link readout and, when a device is selected, its
// config drawer — both as Dear ImGui windows (GPU-rendered, crisp at any DPI),
// replacing the old CPU-rasterized image panels. Parameter tuning that used to
// be arrow-key driven is now live sliders. Skipped in graph mode, where the node
// editor owns the screen.
func hudUI(c *app.Ctx) {
	if ui := app.GetResource[UI](c); ui != nil && ui.Mode == ModeGraph {
		return
	}
	lab := app.GetResource[Lab](c)
	link := app.GetResource[Link](c)
	if lab == nil || link == nil {
		return
	}
	txd, _, _ := focusTx(c, lab)

	drawReadoutUI(link, txd)
	drawConfigUI(c, lab)
}

const hudWindowFlags = imgui.WindowFlagsNoResize | imgui.WindowFlagsNoMove |
	imgui.WindowFlagsNoCollapse | imgui.WindowFlagsNoSavedSettings

// drawReadoutUI is the top-left link-budget panel.
func drawReadoutUI(link *Link, txd *TxDevice) {
	freq, power := 24e6, 0.0
	if txd != nil {
		freq, power = txd.FreqHz, txd.PowerW
	}
	imgui.SetNextWindowPosV(imgui.Vec2{X: margin, Y: margin}, imgui.CondAlways, imgui.Vec2{})
	if imgui.BeginV("Receiver Link", nil, hudWindowFlags|imgui.WindowFlagsAlwaysAutoResize) {
		imgui.TextUnformatted(fmt.Sprintf("Pr     %8.1f dBm", sim.DBm(link.PowerW)))
		imgui.TextUnformatted(fmt.Sprintf("SNR    %8.1f dB", link.SNRdB))
		imgui.TextUnformatted(fmt.Sprintf("dist   %8.1f m", link.DistM))
		imgui.TextUnformatted(fmt.Sprintf("freq   %8.0f MHz", freq/1e6))
		imgui.TextUnformatted(fmt.Sprintf("lambda %8.1f m", sim.SpeedOfLight/freq))
		imgui.TextUnformatted(fmt.Sprintf("Ptx    %8.1f dBm", sim.DBm(power)))
	}
	imgui.End()
}

// configDrawerW is the drawer width in logical units; it scales with the UI.
const configDrawerW = 300

// drawConfigUI is the right-edge config drawer for the selected device, with
// live sliders and the button that opens its signal-chain graph.
func drawConfigUI(c *app.Ctx, lab *Lab) {
	if lab.Sel == SelNone {
		return
	}
	fbW, fbH := render3d.Size(c)
	s := splitiui.Scale(c)
	drawerW := configDrawerW * s
	itemW := drawerW - 130*s // leave room for the slider's value/label
	imgui.SetNextWindowPosV(imgui.Vec2{X: float32(fbW) - drawerW, Y: 0}, imgui.CondAlways, imgui.Vec2{})
	imgui.SetNextWindowSizeV(imgui.Vec2{X: drawerW, Y: float32(fbH)}, imgui.CondAlways)

	switch lab.Sel {
	case SelTx:
		txd := selectedTx(c, lab)
		if txd == nil {
			return
		}
		if imgui.BeginV("Transmitter", nil, hudWindowFlags) {
			imgui.SetNextItemWidth(itemW)
			pdbm := float32(sim.DBm(txd.PowerW))
			if imgui.SliderFloatV("Power (dBm)", &pdbm, -90, 0, "%.1f", 0) {
				txd.PowerW = dbmToW(float64(pdbm))
				markTxDirty(txd)
			}
			imgui.SetNextItemWidth(itemW)
			fmhz := float32(txd.FreqHz / 1e6)
			if imgui.SliderFloatV("Freq (MHz)", &fmhz, 10, 45, "%.0f", 0) {
				txd.FreqHz = float64(fmhz) * 1e6
				markTxDirty(txd)
			}
			imgui.Separator()
			if imgui.Button("Open Graph") {
				openGraphFor(c, SelTx, lab.Ent)
			}
		}
		imgui.End()

	case SelRx:
		rxd := selectedRx(c, lab)
		if rxd == nil {
			return
		}
		if imgui.BeginV("Receiver", nil, hudWindowFlags) {
			imgui.SetNextItemWidth(itemW)
			tune := float32(rxd.TuneHz / 1e6)
			if imgui.SliderFloatV("Tune (MHz)", &tune, 10, 45, "%.0f", 0) {
				rxd.TuneHz = float64(tune) * 1e6
			}
			imgui.SetNextItemWidth(itemW)
			gain := float32(rxd.GainDBi)
			if imgui.SliderFloatV("Gain (dBi)", &gain, -10, 30, "%.0f", 0) {
				rxd.GainDBi = float64(gain)
			}
			imgui.SetNextItemWidth(itemW)
			nf := float32(rxd.NoiseFigDB)
			if imgui.SliderFloatV("Noise (dB)", &nf, 0, 20, "%.1f", 0) {
				rxd.NoiseFigDB = float64(nf)
			}
			imgui.Separator()
			if imgui.Button("Open Graph") {
				openGraphFor(c, SelRx, lab.Ent)
			}
		}
		imgui.End()
	}
}

// dbmToW converts decibel-milliwatts to watts (inverse of sim.DBm).
func dbmToW(dBm float64) float64 { return math.Pow(10, dBm/10) * 1e-3 }

// markTxDirty flags a transmitter's playback for recompile after a parameter edit.
func markTxDirty(txd *TxDevice) {
	if txd != nil && txd.Play != nil {
		txd.Play.dirty = true
	}
}

// openGraphFor opens the node-graph editor on device e's chain.
func openGraphFor(c *app.Ctx, sel Selection, e ecs.Entity) {
	ui := app.GetResource[UI](c)
	ed := app.GetResource[Editor](c)
	if ui == nil || ed == nil {
		return
	}
	ed.target, ed.targetEnt = sel, e
	ui.Mode = ModeGraph
}
