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

// txChain walks Transmitter ← Constellation ← Text and returns the three nodes,
// or all nils when the path is incomplete. The full chain is what gates both
// symbol compilation and physical emission, so a transmitter with any wire cut
// radiates nothing at all (see txRadiates / fieldSystem).
func txChain(g *Graph) (txt, con, tx *Node) {
	for _, n := range g.Nodes {
		if n.Kind == KindTransmitter {
			tx = n
			break
		}
	}
	if tx == nil {
		return nil, nil, nil
	}
	con = g.inputOf(tx.ID)
	if con == nil || con.Kind != KindConstellation {
		return nil, nil, nil
	}
	txt = g.inputOf(con.ID)
	if txt == nil || txt.Kind != KindText {
		return nil, nil, nil
	}
	return txt, con, tx
}

// txRadiates reports whether the transmit chain is fully wired
// (Text → Constellation → Transmitter). A transmitter whose chain is severed
// emits nothing — not even an unmodulated carrier.
func txRadiates(g *Graph) bool {
	_, _, tx := txChain(g)
	return tx != nil
}

// recompile walks Transmitter ← Constellation ← Text and rebuilds the symbol
// stream. If the chain is incomplete, Symbols is cleared (nothing to send).
func (p *Play) recompile(g *Graph) {
	p.Symbols = nil
	txt, con, tx := txChain(g)
	if tx == nil {
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

// playbackSystem recompiles each transmitter's chain when its graph has changed.
func playbackSystem(c *app.Ctx) {
	app.Query2[TxDevice, txTag](c, func(_ ecs.Entity, d *TxDevice, _ *txTag) {
		if d.Play != nil && d.Play.dirty {
			d.Play.recompile(d.Graph)
			d.Play.dirty = false
		}
	})
}
