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
	"github.com/mlange-42/arche/generic"
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
	rxd, _, _ := focusRx(c, lab)

	readoutBottom := drawReadoutUI(link, txd, rxd)
	drawChannelUI(c, readoutBottom)
	drawConfigUI(c, lab)
}

// drawChannelUI is the always-on global propagation panel: the multipath toggle
// and its order/reflectivity controls, anchored just below the top-left link
// readout (top is that readout's bottom edge, in framebuffer px) so the two never
// overlap as the readout grows.
func drawChannelUI(c *app.Ctx, top float32) {
	ch := app.GetResource[Channel](c)
	if ch == nil {
		return
	}
	s := splitiui.Scale(c)
	imgui.SetNextWindowPosV(imgui.Vec2{X: margin, Y: top + margin*s}, imgui.CondAlways, imgui.Vec2{})
	if imgui.BeginV("Channel", nil, hudWindowFlags|imgui.WindowFlagsAlwaysAutoResize) {
		imgui.Checkbox("Multipath", &ch.Multipath)
		if ch.Multipath {
			imgui.SetNextItemWidth(140 * s)
			order := int32(ch.MaxOrder)
			if imgui.SliderInt("Max order", &order, 1, maxRefl) {
				ch.MaxOrder = int(order)
			}
			imgui.SetNextItemWidth(140 * s)
			refl := float32(ch.Reflectivity)
			if imgui.SliderFloatV("Reflectivity", &refl, 0, 1, "%.2f", 0) {
				ch.Reflectivity = float64(refl)
			}
		}
	}
	imgui.End()
}

const hudWindowFlags = imgui.WindowFlagsNoResize | imgui.WindowFlagsNoMove |
	imgui.WindowFlagsNoCollapse | imgui.WindowFlagsNoSavedSettings

// drawReadoutUI is the top-left link-budget panel. It returns the panel's bottom
// edge in framebuffer px so the Channel panel can sit just below it. The bit rates
// are measured at the receiver — the throughput its own chain actually recovers — so
// they reflect what this receiver demodulates, not how the far-end transmitter is
// configured, and drop to zero when the link stops delivering.
func drawReadoutUI(link *Link, txd *TxDevice, rxd *RxDevice) float32 {
	freq, power := 24e6, 0.0
	if txd != nil {
		freq, power = txd.FreqHz, txd.PowerW
	}
	var coded, data, corr float64
	ber := -1.0
	hasFEC := false
	if rxd != nil && rxd.Decode != nil {
		coded, data = rxd.Decode.codedRate, rxd.Decode.dataRate
		corr, ber = rxd.Decode.corrRate, rxd.Decode.berFrac()
		if rxd.Graph != nil {
			_, fec, _, _ := rxChain(rxd.Graph)
			hasFEC = fec != nil
		}
	}
	bottom := float32(margin)
	imgui.SetNextWindowPosV(imgui.Vec2{X: margin, Y: margin}, imgui.CondAlways, imgui.Vec2{})
	if imgui.BeginV("Receiver Link", nil, hudWindowFlags|imgui.WindowFlagsAlwaysAutoResize) {
		imgui.TextUnformatted(fmt.Sprintf("Pr     %8.1f dBm", sim.DBm(link.PowerW)))
		imgui.TextUnformatted(fmt.Sprintf("SNR    %8.1f dB", link.SNRdB))
		imgui.TextUnformatted(fmt.Sprintf("EVM    %8s dB", evmStr(link.EVMdB)))
		imgui.TextUnformatted(fmt.Sprintf("dist   %8.1f m", link.DistM))
		imgui.TextUnformatted(fmt.Sprintf("freq   %8.0f MHz", freq/1e6))
		imgui.TextUnformatted(fmt.Sprintf("lambda %8.1f m", sim.SpeedOfLight/freq))
		imgui.TextUnformatted(fmt.Sprintf("Ptx    %8.1f dBm", sim.DBm(power)))
		imgui.TextUnformatted(fmt.Sprintf("coded  %8.1f bit/s", coded))
		imgui.TextUnformatted(fmt.Sprintf("data   %8.1f bit/s", data))
		imgui.TextUnformatted(fmt.Sprintf("BER    %8s", berStr(ber)))
		// The FEC repair rate is only meaningful when an Error-Correction node is wired
		// in; without one there is nothing to correct, so omit the line entirely rather
		// than imply a zero rate.
		if hasFEC {
			imgui.TextUnformatted(fmt.Sprintf("fixed  %8.1f err/s", corr))
		}
		bottom = imgui.WindowPos().Y + imgui.WindowSize().Y
	}
	imgui.End()
	return bottom
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

	case SelBlock:
		tmap := generic.NewMap[render3d.Transform3D](c.World())
		if lab.Ent.IsZero() || !tmap.Has(lab.Ent) {
			return
		}
		tr := tmap.Get(lab.Ent)
		if imgui.BeginV("Block", nil, hudWindowFlags) {
			imgui.TextUnformatted("Line-of-sight obstacle.")
			imgui.TextUnformatted("Drag onto the toolbar to remove.")
			imgui.Separator()
			imgui.SetNextItemWidth(itemW)
			wv := tr.Scale.X
			if imgui.SliderFloatV("Width (m)", &wv, 2, 30, "%.0f", 0) {
				tr.Scale.X, tr.Scale.Z = wv, wv // keep the footprint square
			}
			imgui.SetNextItemWidth(itemW)
			hv := tr.Scale.Y
			if imgui.SliderFloatV("Height (m)", &hv, 2, 40, "%.0f", 0) {
				tr.Scale.Y = hv
				tr.Translation.Y = hv / 2 // keep it resting on the ground
			}
			imgui.Separator()
			if imgui.Button("Remove") {
				removeSelected(c, lab)
			}
		}
		imgui.End()
	}
}

// dbmToW converts decibel-milliwatts to watts (inverse of sim.DBm).
func dbmToW(dBm float64) float64 { return math.Pow(10, dBm/10) * 1e-3 }

// evmStr formats the EVM-measured SNR, showing a dash when there is no measurement
// yet (the receiver has not locked to a signal) or the value is otherwise non-finite.
func evmStr(dB float64) string {
	if math.IsInf(dB, 0) || math.IsNaN(dB) {
		return "--"
	}
	return fmt.Sprintf("%.1f", dB)
}

// berStr formats the measured pre-FEC channel bit-error rate as a percentage,
// showing a dash before the receiver has demodulated anything (berFrac returns < 0).
func berStr(frac float64) string {
	if frac < 0 {
		return "--"
	}
	return fmt.Sprintf("%.2f%%", frac*100)
}

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
