package main

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
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

	// live, updated each frame by fieldSystem:
	CurTx    int // index of the symbol being emitted now
	rx       [rxBufLen]complex128
	rxHead   int
	rxFilled int
}

func newPlay() *Play { return &Play{SymbolRate: 2, Mod: sim.QPSK} }

// recompile walks Transmitter ← Constellation ← Text and rebuilds the symbol
// stream. If the chain is incomplete, Symbols is cleared (nothing to send).
func (p *Play) recompile(g *Graph) {
	p.Symbols = nil
	var tx *Node
	for _, n := range g.Nodes {
		if n.Kind == KindTransmitter {
			tx = n
			break
		}
	}
	if tx == nil {
		return
	}
	con := g.inputOf(tx.ID)
	if con == nil || con.Kind != KindConstellation {
		return
	}
	txt := g.inputOf(con.ID)
	if txt == nil || txt.Kind != KindText {
		return
	}
	p.Mod = con.Mod
	p.Symbols = textToSymbols(txt.Text, con.Mod)
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

// pushRx records one received constellation sample (symbol + noise).
func (p *Play) pushRx(v complex128) {
	p.rx[p.rxHead] = v
	p.rxHead = (p.rxHead + 1) % rxBufLen
	if p.rxFilled < rxBufLen {
		p.rxFilled++
	}
}

// rxPoints copies the received scatter into dst (newest last); returns the count.
func (p *Play) rxPoints(dst []complex128) int {
	n := p.rxFilled
	start := (p.rxHead - n + rxBufLen) % rxBufLen
	for i := 0; i < n; i++ {
		dst[i] = p.rx[(start+i)%rxBufLen]
	}
	return n
}

// playbackSystem recompiles a transmitter's chain when its graph has changed.
func playbackSystem(c *app.Ctx) {
	d := txDevice(c)
	if d == nil || d.Play == nil {
		return
	}
	if d.Play.dirty {
		d.Play.recompile(d.Graph)
		d.Play.dirty = false
	}
}
