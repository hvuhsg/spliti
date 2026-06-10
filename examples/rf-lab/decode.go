//go:build !js

package main

import (
	"math"
	"math/cmplx"

	"github.com/hvuhsg/spliti/examples/radiosim/sim"
)

// Decode is the receiver's running demodulator: it turns the stream of received
// (noisy) symbols back into bits, rebuilding the bit stream once per loop so at low
// SNR you watch wrong decisions ripple downstream. The Constellation node's job —
// symbol → bits — lives here; the later stages (Hamming correction, bits → ASCII)
// are applied in render according to which nodes the chain actually contains, so a
// chain that stops at binary shows binary. It also keeps the received constellation
// scatter (the noisy samples the receiver saw), since that is a property of the
// receiver — sampled from the wave — not of any transmitter.
type Decode struct {
	raw     []int  // bits demodulated from symbols this loop (pre-correction)
	display string // chain-rendered readout, recomputed each frame by render

	codedBits int     // cumulative coded bits demodulated this loop (set by render)
	dataBits  int     // cumulative payload bits recovered this loop (set by render)
	corrected int     // cumulative bit errors the FEC repaired this loop (set by render)
	codedRate float64 // measured coded throughput, bit/s (set by measure)
	dataRate  float64 // measured payload throughput, bit/s (set by measure)
	corrRate  float64 // measured FEC repair rate, errors/s (set by measure)
	codedM    bitMeter
	dataM     bitMeter
	corrM     bitMeter

	evmSig float64 // EWMA of decided-symbol power (EVM SNR numerator)
	evmErr float64 // EWMA of error-vector power (EVM SNR denominator)
	evmN   int     // samples folded into the EVM estimate this link

	berErr float64 // EWMA of bit errors per symbol (pre-FEC channel BER numerator)
	berTot float64 // EWMA of bits compared per symbol (BER denominator)
	berN   int     // symbols folded into the BER estimate this link

	lastIdx int

	rx       [rxBufLen]complex128 // received-constellation scatter ring buffer
	rxHead   int
	rxFilled int
}

// rateWindow is the sliding window (sim-seconds) the throughput is measured over.
// A few message symbols fit in it, so the reading is steady while transmitting yet
// falls to zero within a window once bits stop arriving.
const rateWindow = 4.0

func newDecode() *Decode {
	return &Decode{lastIdx: -1, codedM: bitMeter{win: rateWindow}, dataM: bitMeter{win: rateWindow}, corrM: bitMeter{win: rateWindow}}
}

// bitMeter measures a delivery rate over a sliding sim-time window: it records when
// bits are actually recovered and reports bits-in-window / window. This is a genuine
// measurement — it goes to zero when reception stops and reflects whatever the chain
// really delivered — rather than an assumed symbol-rate × bits/symbol product.
type bitMeter struct {
	win     float64
	ts      []float64 // delivery times, oldest first
	counts  []int     // bits delivered at each time
	last    int       // previous cumulative count, to derive per-frame deliveries
	started bool
}

// observe takes the current cumulative bit count (which drops back toward zero each
// message loop), at sim time t. It records any positive growth as a delivery and
// ages out events older than the window. A drop (loop wrap, or a reset) is not a
// delivery, so the rate reflects only bits genuinely recovered.
func (m *bitMeter) observe(t float64, cumulative int) {
	if m.started {
		if d := cumulative - m.last; d > 0 {
			m.ts = append(m.ts, t)
			m.counts = append(m.counts, d)
		}
	}
	m.last = cumulative
	m.started = true
	cut := t - m.win
	k := 0
	for k < len(m.ts) && m.ts[k] < cut {
		k++
	}
	if k > 0 { // compact in place, dropping aged-out events
		m.ts = append(m.ts[:0], m.ts[k:]...)
		m.counts = append(m.counts[:0], m.counts[k:]...)
	}
}

func (m *bitMeter) rate() float64 {
	if m.win <= 0 {
		return 0
	}
	sum := 0
	for _, n := range m.counts {
		sum += n
	}
	return float64(sum) / m.win
}

func (m *bitMeter) reset() {
	m.ts = m.ts[:0]
	m.counts = m.counts[:0]
	m.last = 0
	m.started = false
}

// pushRx records one received constellation sample (the wave at the antenna).
func (d *Decode) pushRx(v complex128) {
	d.rx[d.rxHead] = v
	d.rxHead = (d.rxHead + 1) % rxBufLen
	if d.rxFilled < rxBufLen {
		d.rxFilled++
	}
}

// rxPoints copies the received scatter into dst (newest last); returns the count.
func (d *Decode) rxPoints(dst []complex128) int {
	n := d.rxFilled
	start := (d.rxHead - n + rxBufLen) % rxBufLen
	for i := 0; i < n; i++ {
		dst[i] = d.rx[(start+i)%rxBufLen]
	}
	return n
}

// step consumes the symbol arriving at index arrIdx (0…nsym−1). It demodulates one
// symbol per index change into raw bits and resets the accumulator when the loop
// wraps. Turning those raw bits into the displayed readout is render's job.
func (d *Decode) step(arrIdx int, recv complex128, mod sim.Modulation) {
	if arrIdx == d.lastIdx {
		return
	}
	if arrIdx < d.lastIdx { // looped back to the start of the message
		d.raw = d.raw[:0]
	}
	d.lastIdx = arrIdx

	v := nearestSymbolValue(mod, recv)
	for k := bitsPerSymbol(mod) - 1; k >= 0; k-- {
		d.raw = append(d.raw, (v>>uint(k))&1)
	}
}

// render turns the raw demodulated bits into the readout shown downstream, applying
// the stages the chain actually contains: error correction when an Error-Correction
// node is wired in (fec, decoded with its scheme), then bits → ASCII when a Bits
// node is (toText). With no Bits node the chain stops at binary, so the raw
// (optionally corrected) bit stream is shown verbatim. The result is cached for
// text().
func (d *Decode) render(fec *Node, toText bool) string {
	bits := d.raw
	d.corrected = 0
	if fec != nil {
		bits, d.corrected = eccDecodeCount(fec.Code, bits)
	}
	d.codedBits = len(d.raw) // coded bits taken off the channel (incl. parity)
	d.dataBits = len(bits)   // payload bits left after correction
	if toText {
		d.display = bitsToText(bits)
	} else {
		d.display = bitsToString(bits)
	}
	return d.display
}

// measure updates the throughput readings from the bits the chain actually
// recovered, at sim time t. Called every frame after render, it ages the sliding
// window so the rate falls to zero when reception stops. The rates are a property of
// the receiver alone: they count the output of its own constellation and FEC, so the
// transmitter's settings move them only insofar as they change what is received.
func (d *Decode) measure(t float64) {
	d.codedM.observe(t, d.codedBits)
	d.dataM.observe(t, d.dataBits)
	d.corrM.observe(t, d.corrected)
	d.codedRate = d.codedM.rate()
	d.dataRate = d.dataM.rate()
	d.corrRate = d.corrM.rate()
}

// observeEVM folds one received sample into the error-vector-magnitude estimate of
// SNR: it compares the sample against the nearest ideal constellation point (the
// receiver's own hard decision) and exponentially averages the decided-symbol power
// and the error power. This is a genuine receiver-side measurement — it reflects
// multipath, interference, and even a wrong constellation choice — and, like real
// EVM, optimistically saturates once the noise is large enough to push samples past
// the decision boundary (the error is then measured against the wrong symbol).
func (d *Decode) observeEVM(recv complex128, mod sim.Modulation) {
	pts := constellation(mod)
	ideal := pts[nearestSymbolValue(mod, recv)]
	sig := real(ideal)*real(ideal) + imag(ideal)*imag(ideal)
	e := recv - ideal
	errp := real(e)*real(e) + imag(e)*imag(e)
	const a = 0.05 // EWMA weight: ~20-sample memory, smooth but responsive
	if d.evmN == 0 {
		d.evmSig, d.evmErr = sig, errp
	} else {
		d.evmSig += a * (sig - d.evmSig)
		d.evmErr += a * (errp - d.evmErr)
	}
	d.evmN++
}

// evmSNRdB returns the EVM-measured SNR in dB, or -Inf when nothing has been
// measured yet. The caller decides how to present an absent or saturated estimate.
func (d *Decode) evmSNRdB() float64 {
	if d.evmN == 0 || d.evmErr <= 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(d.evmSig/d.evmErr)
}

// observeBER folds one symbol into the pre-FEC channel bit-error estimate: it counts
// how many of the nbits the demodulator decided (rxVal) differ from the bits the
// transmitter actually sent (txVal, known because the clock source's compiled
// symbols are the ground truth), and exponentially averages the error and bit
// counts. The ratio is the raw bit-error rate the forward error correction then has
// to repair — a genuine measurement of the channel, before any correction.
func (d *Decode) observeBER(txVal, rxVal, nbits int) {
	errs := 0
	for k := 0; k < nbits; k++ {
		if (txVal>>uint(k))&1 != (rxVal>>uint(k))&1 {
			errs++
		}
	}
	const a = 0.05 // EWMA weight, matching observeEVM: smooth but responsive
	if d.berN == 0 {
		d.berErr, d.berTot = float64(errs), float64(nbits)
	} else {
		d.berErr += a * (float64(errs) - d.berErr)
		d.berTot += a * (float64(nbits) - d.berTot)
	}
	d.berN++
}

// berFrac returns the measured pre-FEC channel bit-error rate (0…1), or -1 when the
// receiver has not demodulated anything yet. The caller decides how to present an
// absent estimate.
func (d *Decode) berFrac() float64 {
	if d.berN == 0 || d.berTot <= 0 {
		return -1
	}
	return d.berErr / d.berTot
}

// text returns the live decoded readout (what render last produced).
func (d *Decode) text() string { return d.display }

// reset clears the decoder, the received-scatter buffer, and the throughput meters.
// The receiver calls it when the chain is disconnected, so a severed link shows
// nothing downstream, reports zero throughput, and a later reconnect starts the
// message fresh instead of resuming mid-stream.
func (d *Decode) reset() {
	d.raw = d.raw[:0]
	d.display = ""
	d.codedBits, d.dataBits, d.corrected = 0, 0, 0
	d.codedRate, d.dataRate, d.corrRate = 0, 0, 0
	d.codedM.reset()
	d.dataM.reset()
	d.corrM.reset()
	d.evmSig, d.evmErr, d.evmN = 0, 0, 0
	d.berErr, d.berTot, d.berN = 0, 0, 0
	d.lastIdx = -1
	d.rxHead = 0
	d.rxFilled = 0
}

// nearestSymbolValue returns the constellation symbol value closest to recv — the
// hard demodulation decision.
func nearestSymbolValue(mod sim.Modulation, recv complex128) int {
	pts := constellation(mod)
	best, bd := 0, cmplx.Abs(pts[0]-recv)
	for i := 1; i < len(pts); i++ {
		if d := cmplx.Abs(pts[i] - recv); d < bd {
			best, bd = i, d
		}
	}
	return best
}

// rxChain follows the wires out of the receiver — Receiver → Constellation →
// [Error-Correction] → [Bits] → Text — so the graph topology actually gates the
// data flow. con is the constellation the antenna feeds (nil if that link is
// missing, so nothing demodulates the antenna). fec is the Error-Correction node
// wired downstream of it, or nil (when present, its scheme corrects the bits), and
// toText reports whether a Bits node is wired in (decode the bits to ASCII; without
// it the readout stays binary). sink is the Text node that displays the result, or
// nil if the chain never reaches one. Cutting any wire — or deleting a node — drops
// the stages past the cut.
func rxChain(g *Graph) (con, fec *Node, toText bool, sink *Node) {
	order := g.chainNodes() // Receiver, then downstream nodes in flow order
	if len(order) < 2 || order[1].Kind != KindConstellation {
		return nil, nil, false, nil
	}
	con = order[1]
	for _, n := range order[2:] {
		switch n.Kind {
		case KindErrorCorrect:
			fec = n
		case KindBits:
			toText = true
		case KindText:
			sink = n
		}
	}
	return con, fec, toText, sink
}
