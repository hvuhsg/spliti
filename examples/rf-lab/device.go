package main

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
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
	GainDBi     float64
	NoiseFigDB  float64
	BandwidthHz float64
	TempK       float64
	Graph       *Graph  // this receiver's RX (decode) signal chain
	Decode      *Decode // the running demodulator/decoder
}

func newRxDevice() *RxDevice {
	return &RxDevice{
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

// txDevice / rxDevice return the (single, for now) transmitter / receiver device.
func txDevice(c *app.Ctx) *TxDevice {
	var d *TxDevice
	app.Query2[TxDevice, txTag](c, func(_ ecs.Entity, dd *TxDevice, _ *txTag) { d = dd })
	return d
}

func rxDevice(c *app.Ctx) *RxDevice {
	var d *RxDevice
	app.Query2[RxDevice, rxTag](c, func(_ ecs.Entity, dd *RxDevice, _ *rxTag) { d = dd })
	return d
}
