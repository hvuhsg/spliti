package main

import (
	"math"
	"math/cmplx"
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

// clear blanks the oscilloscope trace. The field system calls it when the focused
// receiver has no antenna wired into a demodulator, so a severed RX chain shows a
// flat line instead of a frozen, stale waveform.
func (f *Field) clear() {
	for i := range f.scope {
		f.scope[i] = 0
	}
	f.head = 0
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
	cr, cg, cb float64   // hue for this carrier, so its wavefronts are recognizable
	nodes      []imgNode // precomputed reflection images (empty when multipath is off)
}

// fieldFrame is the per-frame propagation context shared by the two passes: the wave
// clock, every radiating source, the occluding blocks, and the multipath settings.
// Gathering it once keeps the ground-field painting and the receiver sampling
// consistent — they sum the same wave — and keeps each pass readable.
type fieldFrame struct {
	f          *Field
	t, ct      float64 // wave clock (s) and its phase reference (visualSpeed·t)
	srcs       []txSrc
	blocks     []blockBox
	mp         bool    // wall multipath enabled this frame
	refl       float64 // per-bounce wall reflection coefficient
	maxOrder   int     // reflection bounces for the single-point receiver
	fieldOrder int     // reflection bounces for the 10k-cell ground viz (clamped)
	ground     bool    // ground (two-ray) reflection enabled this frame
	groundRefl float64 // ground reflection magnitude
	taps       []rfTap // scratch reused per cell/receiver so the hot loops don't allocate
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

	fr := newFieldFrame(c, f)
	paintField(c, fr)

	// Light the "current symbol" indicator on every transmitting source, so a
	// selected transmitter's ideal-constellation panel can show what it is sending.
	for _, s := range fr.srcs {
		if s.play != nil && s.play.Playing && len(s.play.Symbols) > 0 {
			s.play.CurTx = s.play.symIndex(f.T)
		}
	}

	sampleReceiver(c, fr, lab)
}

// newFieldFrame gathers this frame's propagation context: every radiating source
// (a transmitter whose chain is not fully wired radiates nothing, so it is left out
// of the field, the receiver sampling, and the symbol indicator entirely — cutting
// the wire into a Transmitter node silences it), the occluding blocks, and the
// multipath images. The image tree depends only on the transmitter and the faces,
// so it is built once per source here and reused for all field cells and the
// receiver. The 10 000-cell ground viz is clamped to fieldOrder bounces to stay
// interactive; the single-point receiver uses the full order.
func newFieldFrame(c *app.Ctx, f *Field) *fieldFrame {
	fr := &fieldFrame{f: f, t: f.T, ct: visualSpeed * f.T}
	app.Query3[render3d.Transform3D, TxDevice, txTag](c,
		func(_ ecs.Entity, t *render3d.Transform3D, d *TxDevice, _ *txTag) {
			if !txRadiates(d.Graph) {
				return
			}
			cr, cg, cb := freqColor(d.FreqHz)
			fr.srcs = append(fr.srcs, txSrc{pos: t.Translation, freq: d.FreqHz, powerW: d.PowerW, play: d.Play, cr: cr, cg: cg, cb: cb})
		})

	fr.blocks = gatherBlocks(c)

	ch := app.GetResource[Channel](c)
	if ch != nil && ch.Ground {
		fr.ground, fr.groundRefl = true, ch.GroundRefl
	}
	fr.mp = ch != nil && ch.Multipath && len(fr.blocks) > 0
	if fr.mp {
		fr.refl, fr.maxOrder = ch.Reflectivity, ch.MaxOrder
		fr.fieldOrder = fr.maxOrder
		if fr.fieldOrder > 2 {
			fr.fieldOrder = 2
		}
		faces := blockFaces(fr.blocks)
		for i := range fr.srcs {
			fr.srcs[i].nodes = buildImages(p2{x: float64(fr.srcs[i].pos.X), z: float64(fr.srcs[i].pos.Z)}, faces, fr.maxOrder)
		}
	}
	return fr
}

// paintField repaints every ground cell with the instantaneous field summed over all
// transmitters. Ground cells show the wave shape at a fixed reference amplitude, so
// the pattern stays visible regardless of transmit power. Each transmitter's field is
// summed into its own hue (keyed to its carrier), so same-frequency sources interfere
// within a shared color while different frequencies show as different colors that mix
// where they cross. When a message is playing, each cell carries the symbol that left
// a transmitter at time (t − r/visualSpeed).
func paintField(c *app.Ctx, fr *fieldFrame) {
	const ambient = 0.04
	f := fr.f
	app.Query2[render3d.InstanceColor, fieldCell](c,
		func(_ ecs.Entity, col *render3d.InstanceColor, fc *fieldCell) {
			var cr, cg, cb float64
			for si := range fr.srcs {
				s := &fr.srcs[si]
				r := math.Hypot(float64(fc.X-s.pos.X), float64(fc.Z-s.pos.Z))
				if r < rMin {
					r = rMin
				}
				k := 2 * math.Pi * s.freq / sim.SpeedOfLight
				th := k * (fr.ct - r)
				sym := modSymbol(s.play, f.T-r/visualSpeed)
				// Re{sym · e^{jθ}} = symRe·cosθ − symIm·sinθ, tinted by the source's hue
				// and dimmed where a block casts a line-of-sight shadow on this cell
				// (deeper, and sharper at higher carriers, per the source's wavelength).
				los := losAmp(float64(s.pos.X), float64(s.pos.Z), float64(fc.X), float64(fc.Z), s.freq, fr.blocks)
				v := los * (refAmp / r) * (real(sym)*math.Cos(th) - imag(sym)*math.Sin(th))
				// Each reflected copy arrives later, having travelled its longer path, so
				// it carries the symbol emitted earlier and a carrier phase set by that
				// length — summing them into the same cell is the interference pattern.
				if fr.mp {
					fr.taps = fr.taps[:0]
					cellPt := p2{x: float64(fc.X), z: float64(fc.Z)}
					srcPt := p2{x: float64(s.pos.X), z: float64(s.pos.Z)}
					collectReflections(srcPt, cellPt, s.freq, fr.blocks, s.nodes, fr.refl, fr.fieldOrder, &fr.taps)
					for _, tp := range fr.taps {
						rr := tp.length
						if rr < rMin {
							rr = rMin
						}
						thr := k * (fr.ct - tp.length)
						symr := modSymbol(s.play, f.T-tp.length/visualSpeed)
						v += tp.atten * (refAmp / rr) * (real(symr)*math.Cos(thr) - imag(symr)*math.Sin(thr))
					}
				}
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
}

// sampleReceiver samples the superposed wave at the focused receiver — the actual sum
// of every transmitter, not any single one — and drives its oscilloscope, scatter,
// decoder, and the link readout from that one sample.
func sampleReceiver(c *app.Ctx, fr *fieldFrame, lab *Lab) {
	f := fr.f
	ct := fr.ct
	rxd, rxPos, okr := focusRx(c, lab)
	link := app.GetResource[Link](c)
	if !okr {
		clearLinkSignal(link)
		return
	}

	// Walk the receiver's wires up front. con is the constellation the antenna
	// feeds; fec is the Error-Correction node downstream (nil if none); toText says
	// whether a Bits node is; sink is the Text node at the end of the chain. Each is
	// nil/false when its link is cut, and data flows only as far as the wires reach.
	con, fec, toText, sink := rxChain(rxd.Graph)

	// Any Text node that isn't the wired sink gets no data, so it shows nothing.
	for _, n := range rxd.Graph.Nodes {
		if n.Kind == KindText && n != sink {
			n.Text = ""
		}
	}

	// No constellation wired to the receiver: the antenna feeds nothing, so there is
	// no input wave to show. Blank the oscilloscope and drop the scatter/message.
	if con == nil {
		f.clear()
		if rxd.Decode != nil {
			rxd.Decode.reset()
		}
		clearLinkSignal(link)
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
	kLO := 2 * math.Pi * tuneFreq / sim.SpeedOfLight // the receiver's local-oscillator wavenumber

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
	for si := range fr.srcs {
		s := &fr.srcs[si]
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
		// A receiver tuned off this source's carrier mixes it down to a non-zero offset
		// rather than true zero-IF, so its baseband — the constellation — rotates at the
		// carrier-frequency offset Δf = f_src − f_tune. On-tune (Δf = 0) the phasor is 1
		// and a clean link lands on its ideal point; detune within the passband and the
		// points spin, sweeping through the decision boundaries and lifting the BER.
		rot := cmplx.Exp(complex(0, (k-kLO)*ct))
		// A block between this transmitter and the receiver attenuates everything it
		// contributes — the trace, the constellation point, and its share of the SNR
		// — by an amount that grows with this source's carrier frequency.
		los := losAmp(float64(s.pos.X), float64(s.pos.Z), float64(rxPos.X), float64(rxPos.Z), s.freq, fr.blocks)
		amp := los * math.Sqrt(s.powerW) / r // received voltage amplitude (1/r loss)
		fieldSample += amp * (real(sym)*math.Cos(th) - imag(sym)*math.Sin(th))
		baseband += complex(amp, 0) * sym * rot
		ampSum += amp
		totalPr += freeSpacePowerW(s.powerW, s.freq, 1, grx, r) * los * los
		// Each reflected copy adds its own delayed symbol and a carrier phase set by
		// its longer path, referenced to the direct path (dDirect) so a clean
		// single-path link still lands on its ideal constellation point while the
		// relative delay of each bounce is what rotates and fades the sum.
		if fr.mp {
			dDirect := math.Hypot(float64(rxPos.X-s.pos.X), float64(rxPos.Z-s.pos.Z))
			fr.taps = fr.taps[:0]
			rxPt := p2{x: float64(rxPos.X), z: float64(rxPos.Z)}
			srcPt := p2{x: float64(s.pos.X), z: float64(s.pos.Z)}
			collectReflections(srcPt, rxPt, s.freq, fr.blocks, s.nodes, fr.refl, fr.maxOrder, &fr.taps)
			for _, tp := range fr.taps {
				rr := tp.length
				if rr < rMin {
					rr = rMin
				}
				thr := k * (ct - tp.length)
				symr := modSymbol(s.play, f.T-tp.length/visualSpeed)
				ampr := tp.atten * math.Sqrt(s.powerW) / rr
				fieldSample += ampr * (real(symr)*math.Cos(thr) - imag(symr)*math.Sin(thr))
				baseband += complex(ampr, 0) * symr * cmplx.Exp(complex(0, -k*(tp.length-dDirect))) * rot
				ampSum += ampr
				totalPr += freeSpacePowerW(s.powerW, s.freq, 1, grx, tp.length) * tp.atten * tp.atten
			}
		}
		// Ground reflection (two-ray): the wave also reaches the receiver by bouncing off
		// the ground. Both antennas stand at markerHeight, so the image source sits an
		// equal depth below ground and the reflected ray runs sqrt(r² + (2·markerHeight)²).
		// At grazing angles the ground flips the phase (Γ ≈ −groundRefl), so it interferes
		// with the direct path — the textbook fading that lays nulls and peaks along the
		// link as the receiver moves, with no obstacle needed. It is referenced to the
		// direct path (lg − r) like the wall taps so a clean link still lands on its point.
		if fr.ground {
			lg := math.Hypot(r, 2*markerHeight)
			if lg < rMin {
				lg = rMin
			}
			thg := k * (ct - lg)
			symg := modSymbol(s.play, f.T-lg/visualSpeed)
			ampg := -fr.groundRefl * los * math.Sqrt(s.powerW) / lg // Γ < 0: grazing phase flip
			fieldSample += ampg * (real(symg)*math.Cos(thg) - imag(symg)*math.Sin(thg))
			baseband += complex(ampg, 0) * symg * cmplx.Exp(complex(0, -k*(lg-r))) * rot
			ampSum += math.Abs(ampg)
			totalPr += freeSpacePowerW(s.powerW, s.freq, 1, grx, lg) * fr.groundRefl * fr.groundRefl * los * los
		}
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
		clearLinkSignal(link)
		return
	}

	var point complex128
	if ampSum > 0 {
		point = baseband / complex(ampSum, 0)
	}
	// Noise per complex sample, scaled to the actual symbol energy so the scatter's
	// SNR equals the reported power SNR (totalPr/noiseFloor) for every modulation —
	// which keeps the EVM measurement and the decoder's error rate honest.
	sigma := 0.0
	if totalPr > 0 && noiseFloorW > 0 {
		sigma = math.Sqrt(avgSymbolPower(con.Mod) * noiseFloorW / (2 * totalPr))
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
			// Measure SNR the way a real receiver does — from the spread of the received
			// samples around the constellation point it decides on (EVM).
			rxd.Decode.observeEVM(recv, con.Mod)
			// Measure the pre-FEC channel BER by comparing the receiver's hard decision
			// against the symbol the transmitter actually put on the air (its compiled
			// symbols are the ground truth here). It is only meaningful when both ends
			// pack the same number of bits per symbol; a modulation mismatch is its own
			// visible failure, so leave the estimate untouched in that case.
			if bitsPerSymbol(clk.Mod) == bitsPerSymbol(con.Mod) {
				txVal := nearestSymbolValue(clk.Mod, clk.Symbols[arrIdx])
				rxVal := nearestSymbolValue(con.Mod, recv)
				rxd.Decode.observeBER(txVal, rxVal, bitsPerSymbol(con.Mod))
			}
		}
	}

	// Render the raw demodulated bits through whatever stages the chain contains —
	// Hamming correction if an Error-Correction node is wired in, ASCII decoding if a
	// Bits node is — and show the result on the sink Text node. With no Bits node the
	// chain stops at binary, so the readout is the bit stream itself.
	disp := rxd.Decode.render(fec, toText)
	// Measure throughput from the bits just recovered, timestamped by the sim clock,
	// so the readout reflects the link actually delivering data rather than an assumed
	// rate (it falls to zero within a window when nothing arrives).
	rxd.Decode.measure(f.T)
	if sink != nil {
		sink.Text = disp
	}

	// Publish the link readout from this same sampled wave, so the panel shows what
	// the receiver actually experiences rather than a separate Friis calculation:
	// PowerW is the real in-band received power (direct + multipath, all co-channel
	// transmitters), SNRdB is that against the thermal floor, and EVMdB is the SNR
	// measured from the constellation scatter. (Distance stays geometry, set by
	// rfSystem.)
	if link != nil {
		link.PowerW = totalPr
		if totalPr > 0 && noiseFloorW > 0 {
			link.SNRdB = 10 * math.Log10(totalPr/noiseFloorW)
		} else {
			link.SNRdB = 0
		}
		link.EVMdB = rxd.Decode.evmSNRdB()
	}
}

// clearLinkSignal zeroes the measured signal portion of the link readout — received
// power and both SNR estimates — leaving the geometric distance (rfSystem's job)
// alone. The field system calls it whenever there is no receiver or no demodulation
// path, so a dead link reads zero instead of a stale value.
func clearLinkSignal(link *Link) {
	if link == nil {
		return
	}
	link.PowerW, link.SNRdB, link.EVMdB = 0, 0, math.Inf(1)
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
