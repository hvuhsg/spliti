package main

import (
	"math/cmplx"

	"github.com/hvuhsg/spliti/examples/radiosim/sim"
)

// Decode is the receiver's running demodulator: it turns the stream of received
// (noisy) symbols back into bits and characters. It rebuilds the message once per
// loop, so at low SNR you watch wrong decisions become wrong characters.
type Decode struct {
	building string // text decoded so far this loop (the live readout)
	done     string // last fully decoded loop
	bits     []int
	lastIdx  int
}

func newDecode() *Decode { return &Decode{lastIdx: -1} }

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

// rxGraphMod returns the modulation chosen by the RX chain's constellation node.
func rxGraphMod(g *Graph) sim.Modulation {
	for _, n := range g.Nodes {
		if n.Kind == KindConstellation {
			return n.Mod
		}
	}
	return sim.QPSK
}

// textSink returns the RX chain's Text node (the decoded-message display), or nil.
func textSink(g *Graph) *Node {
	for _, n := range g.Nodes {
		if n.Kind == KindText {
			return n
		}
	}
	return nil
}
