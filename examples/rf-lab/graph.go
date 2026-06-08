package main

import (
	"math"

	"github.com/hvuhsg/spliti/examples/radiosim/sim"
)

// NodeKind is the type of a graph node in the signal-chain editor.
type NodeKind int

const (
	KindText          NodeKind = iota // message: source in a TX chain, sink (display) in an RX chain
	KindConstellation                 // maps bits ↔ constellation symbols
	KindTransmitter                   // TX sink: drives the carrier with the symbol stream
	KindReceiver                      // RX source: the antenna's received symbol stream
)

// GraphKind marks a chain as transmit or receive, which sets node roles (port
// directions) and how it is compiled.
type GraphKind int

const (
	GraphTx GraphKind = iota // Text → Constellation → Transmitter
	GraphRx                  // Receiver → Constellation → Text
)

// Node is one box in the editor graph. X,Y are its canvas-local pixel position.
type Node struct {
	ID   int
	Kind NodeKind
	X, Y float32

	Text string         // KindText: the message
	Mod  sim.Modulation // KindConstellation: the modulation scheme
}

// Edge connects one node's output to another node's input (single port each).
type Edge struct{ From, To int }

// Graph is the editable signal chain: nodes plus the wires between them.
type Graph struct {
	Kind  GraphKind
	Nodes []*Node
	Edges []Edge
	next  int
}

// newTxGraph returns a default transmit chain — Text → Constellation →
// Transmitter — already wired, so Play works immediately and shows the pattern.
func newTxGraph() *Graph {
	g := &Graph{Kind: GraphTx}
	txt := g.add(KindText, 40, 70)
	txt.Text = "HELLO RF"
	con := g.add(KindConstellation, 360, 70)
	con.Mod = sim.QPSK
	tx := g.add(KindTransmitter, 680, 70)
	g.connect(txt.ID, con.ID)
	g.connect(con.ID, tx.ID)
	return g
}

// newRxGraph returns a default receive chain — Receiver → Constellation → Text —
// where the Text node is a sink that displays the decoded message.
func newRxGraph() *Graph {
	g := &Graph{Kind: GraphRx}
	rx := g.add(KindReceiver, 40, 70)
	con := g.add(KindConstellation, 360, 70)
	con.Mod = sim.QPSK
	txt := g.add(KindText, 680, 70)
	txt.Text = ""
	g.connect(rx.ID, con.ID)
	g.connect(con.ID, txt.ID)
	return g
}

func (g *Graph) add(kind NodeKind, x, y float32) *Node {
	g.next++
	n := &Node{ID: g.next, Kind: kind, X: x, Y: y}
	if kind == KindText {
		n.Text = "TEXT"
	}
	g.Nodes = append(g.Nodes, n)
	return n
}

func (g *Graph) node(id int) *Node {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

// remove deletes a node and any edges touching it.
func (g *Graph) remove(id int) {
	out := g.Nodes[:0]
	for _, n := range g.Nodes {
		if n.ID != id {
			out = append(out, n)
		}
	}
	g.Nodes = out
	es := g.Edges[:0]
	for _, e := range g.Edges {
		if e.From != id && e.To != id {
			es = append(es, e)
		}
	}
	g.Edges = es
}

// connect wires from→to, replacing any existing wire into to's single input.
func (g *Graph) connect(from, to int) {
	if from == to {
		return
	}
	es := g.Edges[:0]
	for _, e := range g.Edges {
		if e.To != to {
			es = append(es, e)
		}
	}
	g.Edges = append(es, Edge{From: from, To: to})
}

// inputOf returns the node feeding the given node's input, or nil.
func (g *Graph) inputOf(id int) *Node {
	for _, e := range g.Edges {
		if e.To == id {
			return g.node(e.From)
		}
	}
	return nil
}

// outputOf returns the node fed by the given node's output, or nil. A node drives
// a single downstream node in these chains, so the first matching wire wins.
func (g *Graph) outputOf(id int) *Node {
	for _, e := range g.Edges {
		if e.From == id {
			return g.node(e.To)
		}
	}
	return nil
}

// --- modulation / constellations ---

// bitsPerSymbol returns how many bits each constellation symbol carries.
func bitsPerSymbol(mod sim.Modulation) int {
	switch mod {
	case sim.BPSK:
		return 1
	case sim.QPSK:
		return 2
	case sim.QAM16:
		return 4
	case sim.QAM64:
		return 6
	}
	return 1
}

// modName is the human label for a modulation.
func modName(mod sim.Modulation) string {
	switch mod {
	case sim.BPSK:
		return "BPSK"
	case sim.QPSK:
		return "QPSK"
	case sim.QAM16:
		return "QAM16"
	case sim.QAM64:
		return "QAM64"
	}
	return "?"
}

// nextMod cycles through the supported schemes (for the constellation node UI).
func nextMod(mod sim.Modulation) sim.Modulation {
	switch mod {
	case sim.BPSK:
		return sim.QPSK
	case sim.QPSK:
		return sim.QAM16
	case sim.QAM16:
		return sim.QAM64
	default:
		return sim.BPSK
	}
}

// constellation returns the ideal symbol points, indexed by symbol value
// (0 … 2^bits−1), normalized so the outermost coordinate is ±1.
func constellation(mod sim.Modulation) []complex128 {
	switch mod {
	case sim.BPSK:
		return []complex128{-1, 1}
	case sim.QPSK:
		const s = math.Sqrt2 / 2
		// bit0 → I sign, bit1 → Q sign
		return []complex128{
			complex(-s, -s), complex(s, -s),
			complex(-s, s), complex(s, s),
		}
	case sim.QAM16:
		return squareQAM(2) // 2 bits/axis
	case sim.QAM64:
		return squareQAM(3) // 3 bits/axis
	}
	return []complex128{0}
}

// squareQAM builds a 2^bpa × 2^bpa square constellation. The symbol value packs
// the I bits in the low half and the Q bits in the high half; coordinates are the
// levels {−(L−1)…−1,1…(L−1)} normalized so the corner level is ±1.
func squareQAM(bpa int) []complex128 {
	L := 1 << bpa
	norm := float64(L - 1)
	pts := make([]complex128, L*L)
	for q := 0; q < L; q++ {
		for i := 0; i < L; i++ {
			vi := float64(2*i-(L-1)) / norm
			vq := float64(2*q-(L-1)) / norm
			pts[q*L+i] = complex(vi, vq)
		}
	}
	return pts
}

// textToSymbols converts a message to a stream of constellation symbols: ASCII
// bytes → bits (MSB first) → groups of bitsPerSymbol → constellation points. The
// last group is zero-padded. Empty text yields a single zero symbol.
func textToSymbols(text string, mod sim.Modulation) []complex128 {
	pts := constellation(mod)
	b := bitsPerSymbol(mod)
	bits := make([]int, 0, len(text)*8)
	for _, by := range []byte(text) {
		for k := 7; k >= 0; k-- {
			bits = append(bits, int(by>>uint(k))&1)
		}
	}
	if len(bits) == 0 {
		return []complex128{0}
	}
	var syms []complex128
	for i := 0; i < len(bits); i += b {
		v := 0
		for j := 0; j < b; j++ {
			v <<= 1
			if i+j < len(bits) {
				v |= bits[i+j]
			}
		}
		syms = append(syms, pts[v%len(pts)])
	}
	return syms
}
