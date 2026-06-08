package main

import (
	"math"
	"math/rand"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
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

// txSrc is one transmitter's contribution to the field for this frame.
type txSrc struct {
	pos        m.Vec3
	freq       float64
	powerW     float64
	play       *Play
	cr, cg, cb float64 // hue for this carrier, so its wavefronts are recognizable
}

// fieldSystem advances the wave clock, repaints every ground cell with the
// instantaneous field summed over all transmitters (so multiple sources visibly
// interfere), and samples that same superposed wave at the focused receiver for
// its oscilloscope trace, constellation scatter, and decoder.
func fieldSystem(c *app.Ctx) {
	f := app.GetResource[Field](c)
	lab := app.GetResource[Lab](c)
	tm := app.GetResource[splititime.Time](c)
	if f == nil || lab == nil {
		return
	}
	if tm != nil {
		f.T += tm.Delta().Seconds()
	}

	ct := visualSpeed * f.T // phase reference; wavefronts move at visualSpeed

	// Gather every transmitter's source parameters for the ground superposition.
	var srcs []txSrc
	app.Query3[render3d.Transform3D, TxDevice, txTag](c,
		func(_ ecs.Entity, t *render3d.Transform3D, d *TxDevice, _ *txTag) {
			cr, cg, cb := freqColor(d.FreqHz)
			srcs = append(srcs, txSrc{pos: t.Translation, freq: d.FreqHz, powerW: d.PowerW, play: d.Play, cr: cr, cg: cg, cb: cb})
		})

	// Obstacles that occlude line-of-sight, gathered once for this frame's tests.
	blocks := gatherBlocks(c)

	// Ground cells show the wave shape at a fixed reference amplitude, so the pattern
	// stays visible regardless of transmit power. Each transmitter's field is summed
	// into its own hue (keyed to its carrier), so same-frequency sources interfere
	// within a shared color while different frequencies show as different colors that
	// mix where they cross. When a message is playing, each cell carries the symbol
	// that left a transmitter at time (t − r/visualSpeed).
	const ambient = 0.04
	app.Query2[render3d.InstanceColor, fieldCell](c,
		func(_ ecs.Entity, col *render3d.InstanceColor, fc *fieldCell) {
			var cr, cg, cb float64
			for _, s := range srcs {
				r := math.Hypot(float64(fc.X-s.pos.X), float64(fc.Z-s.pos.Z))
				if r < rMin {
					r = rMin
				}
				k := 2 * math.Pi * s.freq / sim.SpeedOfLight
				th := k * (ct - r)
				sym := modSymbol(s.play, f.T-r/visualSpeed)
				// Re{sym · e^{jθ}} = symRe·cosθ − symIm·sinθ, tinted by the source's hue
				// and dimmed where a block casts a line-of-sight shadow on this cell
				// (deeper, and sharper at higher carriers, per the source's wavelength).
				los := losAmp(float64(s.pos.X), float64(s.pos.Z), float64(fc.X), float64(fc.Z), s.freq, blocks)
				v := los * (refAmp / r) * (real(sym)*math.Cos(th) - imag(sym)*math.Sin(th))
				cr += s.cr * v
				cg += s.cg * v
				cb += s.cb * v
			}
			// Wave crests light up in the source hue; troughs (negative) fall to the
			// dark ambient floor. Each channel is compressed independently into [0,1].
			*col = render3d.InstanceColor{
				R: float32(ambient + math.Max(0, math.Tanh(fieldGain*cr))),
				G: float32(ambient + math.Max(0, math.Tanh(fieldGain*cg))),
				B: float32(ambient + math.Max(0, math.Tanh(fieldGain*cb))),
				A: 1,
			}
		})

	// Light the "current symbol" indicator on every transmitting source, so a
	// selected transmitter's ideal-constellation panel can show what it is sending.
	for _, s := range srcs {
		if s.play != nil && s.play.Playing && len(s.play.Symbols) > 0 {
			s.play.CurTx = s.play.symIndex(f.T)
		}
	}

	// Everything below samples the *wave* at the focused receiver — the actual
	// superposition of every transmitter — rather than any single transmitter.
	rxd, rxPos, okr := focusRx(c, lab)
	if !okr {
		return
	}
	grx := math.Pow(10, rxd.GainDBi/10) // dBi → linear

	// A real receiver is tuned to one carrier and its front-end filter rejects
	// everything outside a bandwidth around that carrier. So a transmitter on a
	// *different* frequency is filtered out and leaves the link untouched; only a
	// transmitter on the *same* frequency (within the receiver's bandwidth) interferes.
	// The receiver listens on rxd.TuneHz (set in its config panel).
	tuneFreq := rxd.TuneHz
	if tuneFreq <= 0 {
		tuneFreq = 24e6
	}
	halfBW := rxd.BandwidthHz / 2

	// Accumulate only what the tuned front-end passes (within tuneFreq ± bandwidth/2):
	//   - fieldSample: the superposed real (passband) field → the oscilloscope trace.
	//   - baseband:    the superposed complex symbol, weighted by received amplitude
	//                  → the constellation decision. Co-channel transmitters blur into
	//                  each other here (interference); off-channel ones never get in.
	//   - totalPr:     in-band received power, for the noise/SNR.
	// The strongest in-band transmitter drives the symbol clock (timing recovery).
	var fieldSample float64
	var baseband complex128
	var ampSum, totalPr float64
	var clk *Play
	var clkR, clkAmp float64
	for _, s := range srcs {
		if math.Abs(s.freq-tuneFreq) > halfBW {
			continue // off-channel: rejected by the receiver's filter
		}
		r := math.Hypot(float64(rxPos.X-s.pos.X), float64(rxPos.Z-s.pos.Z))
		if r < rMin {
			r = rMin
		}
		k := 2 * math.Pi * s.freq / sim.SpeedOfLight
		th := k * (ct - r)
		sym := modSymbol(s.play, f.T-r/visualSpeed)
		// A block between this transmitter and the receiver attenuates everything it
		// contributes — the trace, the constellation point, and its share of the SNR
		// — by an amount that grows with this source's carrier frequency.
		los := losAmp(float64(s.pos.X), float64(s.pos.Z), float64(rxPos.X), float64(rxPos.Z), s.freq, blocks)
		amp := los * math.Sqrt(s.powerW) / r // received voltage amplitude (1/r loss)
		fieldSample += amp * (real(sym)*math.Cos(th) - imag(sym)*math.Sin(th))
		baseband += complex(amp, 0) * sym
		ampSum += amp
		totalPr += freeSpacePowerW(s.powerW, s.freq, 1, grx, r) * los * los
		if s.play != nil && s.play.Playing && len(s.play.Symbols) > 0 && amp > clkAmp {
			clk, clkR, clkAmp = s.play, r, amp
		}
	}

	// Oscilloscope: the real field plus thermal noise, so the trace shows the true
	// signal-to-noise ratio. σ = (4π/λ)·√(N/2) converts the noise floor into field
	// units (λ of the tuned carrier; exact for a single transmitter).
	noiseFloorW := rxd.rx(rxPos).NoiseFloorW()
	noiseStd := (4 * math.Pi * tuneFreq / sim.SpeedOfLight) * math.Sqrt(noiseFloorW/2)
	f.push(fieldSample + rand.NormFloat64()*noiseStd)

	// Constellation decision: the amplitude-weighted average of the arriving symbols
	// (so one source lands on its ideal point, several land between theirs) plus
	// noise sized by the total received SNR.
	if rxd.Decode == nil {
		return
	}

	// Walk the receiver's wires. con is the constellation the antenna feeds; sink is
	// the Text node the constellation feeds. Either is nil when its link is cut, and
	// data flows only as far as the wires reach.
	con, sink := rxChain(rxd.Graph)

	// Any Text node that isn't the wired sink gets no data, so it shows nothing.
	for _, n := range rxd.Graph.Nodes {
		if n.Kind == KindText && n != sink {
			n.Text = ""
		}
	}

	// No constellation wired to the receiver: nothing demodulates the antenna, so
	// drop the scatter and decoded message entirely.
	if con == nil {
		rxd.Decode.reset()
		return
	}

	var point complex128
	if ampSum > 0 {
		point = baseband / complex(ampSum, 0)
	}
	sigma := 0.0
	if totalPr > 0 && noiseFloorW > 0 {
		sigma = math.Sqrt(noiseFloorW / (2 * totalPr))
	}
	recv := point + complex(rand.NormFloat64()*sigma, rand.NormFloat64()*sigma)
	rxd.Decode.pushRx(recv)

	// Decode only while a transmitter is actually sending; the strongest one drives
	// the symbol clock. The recovered symbol comes from the wave, demodulated with
	// the constellation the receiver is wired to.
	if clk != nil {
		te := f.T - clkR/visualSpeed
		if te >= 0 {
			arrIdx := int(te*clk.SymbolRate) % len(clk.Symbols)
			rxd.Decode.step(arrIdx, recv, con.Mod)
		}
	}

	// The decoded message reaches the Text node only if the constellation is wired
	// to it; a severed Constellation → Text link leaves the display blank.
	if sink != nil {
		sink.Text = rxd.Decode.text()
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

// freqColor maps a carrier frequency to a distinct hue — red at the low end of the
// tunable band through to magenta at the high end — so each transmitter's
// wavefronts stay recognizable on the ground even where they overlap.
func freqColor(freqHz float64) (r, g, b float64) {
	t := clampf((freqHz-10e6)/(45e6-10e6), 0, 1) // 10–45 MHz, the transmitter span
	return hsvToRGB(t*300, 0.9, 1)
}

// hsvToRGB converts HSV (hue in degrees, sat/val in [0,1]) to RGB in [0,1].
func hsvToRGB(h, s, v float64) (r, g, b float64) {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return r + m, g + m, b + m
}
