package main

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// TxDevice is the per-transmitter state: its radio settings plus its own signal
// chain (graph) and playback. It lives as a component on the transmitter entity,
// so each transmitter can have different settings — add more entities to get
// more transmitters.
type TxDevice struct {
	PowerW float64 // transmit power, watts
	FreqHz float64 // carrier frequency, hertz
	Graph  *Graph  // this transmitter's TX signal chain
	Play   *Play   // this transmitter's compiled/playing message
}

func newTxDevice() *TxDevice {
	return &TxDevice{
		PowerW: 1e-9, // 1 nW beacon: SNR is explorable
		FreqHz: 24e6, // 24 MHz (VHF) → λ ≈ 12.5 m, visible across the field
		Graph:  newTxGraph(),
		Play:   newPlay(),
	}
}

// RxDevice is the per-receiver state: front-end parameters plus its own decode
// chain (graph) and the running decoder.
type RxDevice struct {
	TuneHz      float64 // carrier the receiver is tuned to (its center frequency)
	GainDBi     float64
	NoiseFigDB  float64
	BandwidthHz float64
	TempK       float64
	Graph       *Graph  // this receiver's RX (decode) signal chain
	Decode      *Decode // the running demodulator/decoder
}

func newRxDevice() *RxDevice {
	return &RxDevice{
		TuneHz:      24e6, // matches the default transmitter carrier
		GainDBi:     0,
		NoiseFigDB:  6,
		BandwidthHz: 1e6,
		TempK:       290,
		Graph:       newRxGraph(),
		Decode:      newDecode(),
	}
}

// rx builds a sim.Rx (for the thermal noise floor) from this receiver at pos.
func (d *RxDevice) rx(pos m.Vec3) sim.Rx {
	return sim.Rx{
		Pos:         pos,
		BandwidthHz: d.BandwidthHz,
		NoiseFigDB:  d.NoiseFigDB,
		TempK:       d.TempK,
	}
}

// selectedTx returns the TxDevice of the currently selected entity, or nil when a
// transmitter is not the current selection.
func selectedTx(c *app.Ctx, lab *Lab) *TxDevice {
	if lab.Sel != SelTx || lab.Ent.IsZero() {
		return nil
	}
	mp := generic.NewMap[TxDevice](c.World())
	if !mp.Has(lab.Ent) {
		return nil
	}
	return mp.Get(lab.Ent)
}

// selectedRx returns the RxDevice of the currently selected entity, or nil when a
// receiver is not the current selection.
func selectedRx(c *app.Ctx, lab *Lab) *RxDevice {
	if lab.Sel != SelRx || lab.Ent.IsZero() {
		return nil
	}
	mp := generic.NewMap[RxDevice](c.World())
	if !mp.Has(lab.Ent) {
		return nil
	}
	return mp.Get(lab.Ent)
}

// focusTx returns the transmitter the HUD readout, scope, and decoder focus on:
// the selected one if a transmitter is selected, otherwise the first transmitter
// in the scene. The bool is false when there is no transmitter at all.
func focusTx(c *app.Ctx, lab *Lab) (*TxDevice, m.Vec3, bool) {
	if d := selectedTx(c, lab); d != nil {
		return d, entityPos(c, lab.Ent), true
	}
	var d *TxDevice
	var pos m.Vec3
	found := false
	app.Query3[render3d.Transform3D, TxDevice, txTag](c,
		func(_ ecs.Entity, t *render3d.Transform3D, dd *TxDevice, _ *txTag) {
			if !found {
				d, pos, found = dd, t.Translation, true
			}
		})
	return d, pos, found
}

// focusRx returns the receiver the HUD readout, scope, and decoder focus on: the
// selected one if a receiver is selected, otherwise the first receiver in the
// scene. The bool is false when there is no receiver at all.
func focusRx(c *app.Ctx, lab *Lab) (*RxDevice, m.Vec3, bool) {
	if d := selectedRx(c, lab); d != nil {
		return d, entityPos(c, lab.Ent), true
	}
	var d *RxDevice
	var pos m.Vec3
	found := false
	app.Query3[render3d.Transform3D, RxDevice, rxTag](c,
		func(_ ecs.Entity, t *render3d.Transform3D, dd *RxDevice, _ *rxTag) {
			if !found {
				d, pos, found = dd, t.Translation, true
			}
		})
	return d, pos, found
}

// entityPos returns the world translation of an entity's Transform3D (zero vector
// if it has none).
func entityPos(c *app.Ctx, e ecs.Entity) m.Vec3 {
	mp := generic.NewMap[render3d.Transform3D](c.World())
	if e.IsZero() || !mp.Has(e) {
		return m.Vec3{}
	}
	return mp.Get(e).Translation
}
