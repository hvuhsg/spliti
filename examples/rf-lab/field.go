package main

import (
	"math"
	"math/rand"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
	"github.com/hvuhsg/spliti/plugin/render3d"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/mlange-42/arche/ecs"
)

// The field is a regular grid of flat, emissive quads painted on the ground. Each
// quad shows the instantaneous transmitted field at its location, so animating the
// colors makes circular wavefronts spread outward from the transmitter — the
// wave you can actually watch.
const (
	fieldN      = 100  // cells per side
	fieldExtent = 80.0 // half-width of the field, meters (matches the 160 m ground)
	cellSize    = 2 * fieldExtent / fieldN

	visualSpeed = 12.0 // wavefront animation speed, m/s (NOT real c — see file note)
	rMin        = 2.0  // clamp near the source to avoid the 1/r singularity
	refAmp      = 1.0  // reference field amplitude for the ground viz (power-independent)
	fieldGain   = 12.0 // maps field amplitude into the [-1,1] color range
	scopeLen    = 320  // oscilloscope ring-buffer length (samples)
)

// fieldCell tags one ground quad with its world XZ center, so the field system can
// color it from its distance to the transmitter without re-deriving the position.
type fieldCell struct{ X, Z float32 }

// Field holds the animation clock and the receiver's sampled-signal ring buffer
// (the oscilloscope trace).
type Field struct {
	T     float64 // animation time, seconds
	scope [scopeLen]float64
	head  int
}

func newField() *Field { return &Field{} }

// push records one receiver sample into the ring buffer.
func (f *Field) push(v float64) {
	f.scope[f.head] = v
	f.head = (f.head + 1) % scopeLen
}

// ordered copies the ring buffer into dst oldest-to-newest (dst must be scopeLen).
func (f *Field) ordered(dst []float64) {
	for i := 0; i < scopeLen; i++ {
		dst[i] = f.scope[(f.head+i)%scopeLen]
	}
}

// fieldSystem advances the wave clock, repaints every ground cell with the
// instantaneous field Re{(A/r)·e^{j(ωt−kr)}}, and samples the field at the
// receiver into the oscilloscope buffer. Reflection-free: a single point source.
func fieldSystem(c *app.Ctx) {
	f := app.GetResource[Field](c)
	lab := app.GetResource[Lab](c)
	link := app.GetResource[Link](c)
	txd := txDevice(c)
	rxd := rxDevice(c)
	tm := app.GetResource[splititime.Time](c)
	if f == nil || lab == nil {
		return
	}
	if tm != nil {
		f.T += tm.Delta().Seconds()
	}

	// Pull the (single, for now) transmitter's settings and message.
	freq, powerW := 24e6, 1e-9
	var play *Play
	if txd != nil {
		freq, powerW, play = txd.FreqHz, txd.PowerW, txd.Play
	}

	lambda := sim.SpeedOfLight / freq // real wavelength (VHF → metres)
	k := 2 * math.Pi / lambda
	ct := visualSpeed * f.T // phase reference; wavefronts move at visualSpeed
	tx := lab.TxPos

	// Ground cells show the wave shape at a fixed reference amplitude, so the
	// pattern stays visible regardless of transmit power. When a message is
	// playing, each cell carries the symbol that left the transmitter at time
	// (t − r/visualSpeed): the data ripples outward as rings of modulation.
	app.Query2[render3d.InstanceColor, fieldCell](c,
		func(_ ecs.Entity, col *render3d.InstanceColor, fc *fieldCell) {
			r := math.Hypot(float64(fc.X-tx.X), float64(fc.Z-tx.Z))
			if r < rMin {
				r = rMin
			}
			th := k * (ct - r)
			sym := modSymbol(play, f.T-r/visualSpeed)
			// Re{sym · e^{jθ}} = symRe·cosθ − symIm·sinθ.
			modReal := real(sym)*math.Cos(th) - imag(sym)*math.Sin(th)
			r8, g8, b8 := bipolar(math.Tanh(fieldGain * (refAmp / r) * modReal))
			*col = render3d.InstanceColor{R: r8, G: g8, B: b8, A: 1}
		})

	// The receiver samples the *real* field (amplitude ∝ √power, with 1/r loss)
	// plus thermal noise, so the oscilloscope shows the true signal-to-noise ratio.
	r := math.Hypot(float64(lab.RxPos.X-tx.X), float64(lab.RxPos.Z-tx.Z))
	if r < rMin {
		r = rMin
	}
	thRx := k * (ct - r)
	arriving := modSymbol(play, f.T-r/visualSpeed)
	signal := math.Sqrt(powerW) / r * (real(arriving)*math.Cos(thRx) - imag(arriving)*math.Sin(thRx))
	// Noise std, in the same units as the sampled field, calibrated so the scope's
	// visible signal-to-noise matches the link budget's SNR (= Pr/NoiseFloor):
	// σ = (4π/λ)·√(N/2). See SNRdB in the HUD.
	noiseStd := 0.0
	if rxd != nil {
		noiseStd = (4 * math.Pi / lambda) * math.Sqrt(rxd.rx(lab.RxPos).NoiseFloorW()/2)
	}
	f.push(signal + rand.NormFloat64()*noiseStd)

	// While a message is transmitting: feed the received constellation scatter and
	// run the receiver's decoder.
	if play != nil && play.Playing && len(play.Symbols) > 0 {
		play.CurTx = play.symIndex(f.T)
		snrLin := math.Inf(1)
		if link != nil {
			snrLin = math.Pow(10, link.SNRdB/10)
		}
		sigma := 0.0
		if snrLin > 0 && !math.IsInf(snrLin, 1) {
			sigma = math.Sqrt(1 / (2 * snrLin))
		}
		recv := arriving + complex(rand.NormFloat64()*sigma, rand.NormFloat64()*sigma)
		play.pushRx(recv)

		// Receiver decode: sample one symbol per arriving index, decode with the RX
		// chain's modulation, and write the recovered text into its sink node.
		if rxd != nil && rxd.Decode != nil {
			te := f.T - r/visualSpeed
			if te >= 0 {
				arrIdx := int(te*play.SymbolRate) % len(play.Symbols)
				rxd.Decode.step(arrIdx, recv, rxGraphMod(rxd.Graph))
				if sink := textSink(rxd.Graph); sink != nil {
					sink.Text = rxd.Decode.text()
				}
			}
		}
	}
}

// modSymbol returns the transmitted symbol at emission time te, or the
// unmodulated carrier (1) when no Play resource exists.
func modSymbol(play *Play, te float64) complex128 {
	if play == nil {
		return 1
	}
	return play.symbolAt(te)
}

// bipolar maps a signed, normalized field value to a diverging color ramp:
// positive crests warm (orange), negative troughs cool (blue), zero near-black.
func bipolar(v float64) (r, g, b float32) {
	const ambient = 0.04
	pos := math.Max(v, 0)
	neg := math.Max(-v, 0)
	r = float32(ambient + pos*0.95)
	g = float32(ambient + pos*0.42 + neg*0.42)
	b = float32(ambient + neg*0.95)
	return r, g, b
}
