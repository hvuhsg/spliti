package main

import (
	"math/cmplx"

	"github.com/hvuhsg/spliti/examples/radiosim/sim"
)

// Decode is the receiver's running demodulator: it turns the stream of received
// (noisy) symbols back into bits and characters. It rebuilds the message once per
// loop, so at low SNR you watch wrong decisions become wrong characters. It also
// keeps the received constellation scatter (the noisy samples the receiver saw),
// since that is a property of the receiver — sampled from the wave — not of any
// transmitter.
type Decode struct {
	building string // text decoded so far this loop (the live readout)
	done     string // last fully decoded loop
	bits     []int
	lastIdx  int

	rx       [rxBufLen]complex128 // received-constellation scatter ring buffer
	rxHead   int
	rxFilled int
}

func newDecode() *Decode { return &Decode{lastIdx: -1} }

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

// step consumes the symbol arriving at index arrIdx (0…nsym−1). It decodes one
// symbol per index change and resets the accumulator when the loop wraps.
func (d *Decode) step(arrIdx int, recv complex128, mod sim.Modulation) {
	if arrIdx == d.lastIdx {
		return
	}
	if arrIdx < d.lastIdx { // looped back to the start of the message
		d.done = d.building
		d.building = ""
		d.bits = d.bits[:0]
	}
	d.lastIdx = arrIdx

	v := nearestSymbolValue(mod, recv)
	b := bitsPerSymbol(mod)
	for k := b - 1; k >= 0; k-- {
		d.bits = append(d.bits, (v>>uint(k))&1)
	}
	for len(d.bits) >= 8 {
		by := 0
		for i := 0; i < 8; i++ {
			by = by<<1 | d.bits[i]
		}
		d.bits = d.bits[8:]
		if by >= 32 && by < 127 {
			d.building += string(rune(by))
		} else {
			d.building += "·" // non-printable → dot
		}
	}
}

// text returns the live decoded message (what to display in the RX sink node).
func (d *Decode) text() string { return d.building }

// reset clears the decoder and the received-scatter buffer. The receiver calls it
// when the chain is disconnected, so a severed link shows nothing downstream and a
// later reconnect starts the message fresh instead of resuming mid-stream.
func (d *Decode) reset() {
	d.building = ""
	d.done = ""
	d.bits = d.bits[:0]
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

// rxChain follows the wires out of the receiver — Receiver → Constellation → Text —
// so the graph topology actually gates the data flow. con is the constellation the
// receiver is wired to (nil if that link is missing, so nothing demodulates the
// antenna). sink is the Text node the constellation is wired to (nil if that link
// is missing, so the decoded message reaches no display). Cutting either wire — or
// deleting a node — stops the downstream node from updating.
func rxChain(g *Graph) (con, sink *Node) {
	var rx *Node
	for _, n := range g.Nodes {
		if n.Kind == KindReceiver {
			rx = n
			break
		}
	}
	if rx == nil {
		return nil, nil
	}
	con = g.outputOf(rx.ID)
	if con == nil || con.Kind != KindConstellation {
		return nil, nil
	}
	sink = g.outputOf(con.ID)
	if sink == nil || sink.Kind != KindText {
		return con, nil
	}
	return con, sink
}
