package main

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
	"github.com/mlange-42/arche/ecs"
)

const rxBufLen = 180 // received-constellation scatter ring buffer

// Play holds the compiled signal-chain and its live playback state. The symbol
// timeline is driven by the field clock (so the modulated wavefronts, the scope,
// and the constellations all stay in lock-step); this resource is the shared
// state between the graph editor, the field, and the HUD.
type Play struct {
	Playing    bool
	Mod        sim.Modulation
	Symbols    []complex128 // compiled message symbols
	SymbolRate float64      // symbols per second (visual)
	dirty      bool         // recompile requested (graph changed)

	CurTx int // live: index of the symbol being emitted now (set by fieldSystem)
}

func newPlay() *Play { return &Play{SymbolRate: 2, Mod: sim.QPSK} }

// txChain walks the transmit chain in flow order — Text → Bits → [Error-Correction]
// → Constellation → Transmitter — and reports whether it is fully wired. The Bits
// node (text → bits) and the Constellation are required; the Error-Correction node
// is optional and, when present, is returned as fec (nil otherwise) so its scheme
// can be read. txt is the message source and con the constellation; ok is false for
// any incomplete path, so a transmitter with any wire cut radiates nothing at all
// (see txRadiates / fieldSystem).
func txChain(g *Graph) (txt, con, fec *Node, ok bool) {
	order := g.chainNodes() // Text, middle nodes, then Transmitter
	if len(order) < 4 || order[0].Kind != KindText || order[len(order)-1].Kind != KindTransmitter {
		return nil, nil, nil, false
	}
	txt = order[0]
	var sawBits bool
	for _, n := range order[1 : len(order)-1] {
		switch n.Kind {
		case KindBits:
			sawBits = true
		case KindErrorCorrect:
			fec = n
		case KindConstellation:
			con = n
		}
	}
	if !sawBits || con == nil {
		return nil, nil, nil, false
	}
	return txt, con, fec, true
}

// txRadiates reports whether the transmit chain is fully wired
// (Text → Bits → [Error-Correction] → Constellation → Transmitter). A transmitter
// whose chain is severed emits nothing — not even an unmodulated carrier.
func txRadiates(g *Graph) bool {
	_, _, _, ok := txChain(g)
	return ok
}

// recompile walks the transmit chain and rebuilds the symbol stream: the Bits node
// turns the message into a bit stream, the Error-Correction node (if wired) adds
// redundancy with its chosen scheme, and the Constellation packs the coded bits into
// symbols. If the chain is incomplete, Symbols is cleared (nothing to send).
func (p *Play) recompile(g *Graph) {
	p.Symbols = nil
	txt, con, fec, ok := txChain(g)
	if !ok {
		return
	}
	p.Mod = con.Mod
	bits := textToBits(txt.Text)
	if fec != nil {
		bits = eccEncode(fec.Code, bits)
	}
	p.Symbols = bitsToSymbols(bits, con.Mod)
}

// dataRate reports the transmit chain's bit rates in bits per second: coded is the
// raw rate crossing the air (symbol clock × bits/symbol), and data is the useful
// payload rate left after the error-correction overhead (coded × code rate). Both
// are 0 when the chain isn't fully wired. Cycling the modulation or the FEC scheme
// moves these live, which is the throughput cost of robustness made visible.
func (p *Play) dataRate(g *Graph) (coded, data float64) {
	_, con, fec, ok := txChain(g)
	if !ok {
		return 0, 0
	}
	coded = p.SymbolRate * float64(bitsPerSymbol(con.Mod))
	data = coded
	if fec != nil {
		data = coded * eccRateFrac(fec.Code)
	}
	return coded, data
}

// symbolAt returns the symbol emitted at time te (seconds). Before playback
// reaches a point (te < 0) or when idle, it returns the unmodulated carrier (1).
func (p *Play) symbolAt(te float64) complex128 {
	if !p.Playing || len(p.Symbols) == 0 || te < 0 {
		return 1
	}
	i := int(te*p.SymbolRate) % len(p.Symbols)
	return p.Symbols[i]
}

// symIndex returns the index of the symbol emitted at time t.
func (p *Play) symIndex(t float64) int {
	if len(p.Symbols) == 0 {
		return 0
	}
	return int(t*p.SymbolRate) % len(p.Symbols)
}

// playbackSystem recompiles each transmitter's chain when its graph has changed.
func playbackSystem(c *app.Ctx) {
	app.Query2[TxDevice, txTag](c, func(_ ecs.Entity, d *TxDevice, _ *txTag) {
		if d.Play != nil && d.Play.dirty {
			d.Play.recompile(d.Graph)
			d.Play.dirty = false
		}
	})
}
