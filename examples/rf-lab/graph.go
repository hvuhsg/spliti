package main

import (
	"math"

	"github.com/hvuhsg/spliti/examples/radiosim/sim"
)

// NodeKind is the type of a graph node in the signal-chain editor.
type NodeKind int

const (
	KindText          NodeKind = iota // message: source in a TX chain, sink (display) in an RX chain
	KindBits                          // ASCII ↔ bits: encoder in a TX chain, decoder in an RX chain
	KindErrorCorrect                  // Hamming(7,4) FEC: adds parity in TX, corrects bit errors in RX
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
	Code ECC            // KindErrorCorrect: the forward-error-correction scheme
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

// newTxGraph returns a default transmit chain — Text → Bits → Error-Correction →
// Constellation → Transmitter — already wired, so Play works immediately and shows
// the pattern. The Bits node turns the message into a bit stream, the
// Error-Correction node adds Hamming(7,4) parity, and the Constellation packs the
// coded bits into symbols.
func newTxGraph() *Graph {
	g := &Graph{Kind: GraphTx}
	txt := g.add(KindText, 20, 70)
	txt.Text = "HELLO RF"
	bits := g.add(KindBits, 250, 70)
	fec := g.add(KindErrorCorrect, 480, 70)
	con := g.add(KindConstellation, 710, 70)
	con.Mod = sim.QPSK
	tx := g.add(KindTransmitter, 940, 70)
	g.connect(txt.ID, bits.ID)
	g.connect(bits.ID, fec.ID)
	g.connect(fec.ID, con.ID)
	g.connect(con.ID, tx.ID)
	return g
}

// newRxGraph returns a default receive chain — Receiver → Constellation →
// Error-Correction → Bits → Text. The Constellation demodulates symbols into bits,
// the Error-Correction node repairs single-bit errors per Hamming block, the Bits
// node reassembles the data bits into ASCII, and the Text node displays the message.
// Removing the Bits node leaves the chain stopping at binary, so the Text node then
// shows the raw bit stream instead of characters.
func newRxGraph() *Graph {
	g := &Graph{Kind: GraphRx}
	rx := g.add(KindReceiver, 20, 70)
	con := g.add(KindConstellation, 250, 70)
	con.Mod = sim.QPSK
	fec := g.add(KindErrorCorrect, 480, 70)
	bits := g.add(KindBits, 710, 70)
	txt := g.add(KindText, 940, 70)
	txt.Text = ""
	g.connect(rx.ID, con.ID)
	g.connect(con.ID, fec.ID)
	g.connect(fec.ID, bits.ID)
	g.connect(bits.ID, txt.ID)
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

// chainNodes returns the wired nodes in signal-flow order (source → sink). It
// starts from the chain's fixed, unique endpoint — the Transmitter for a TX graph,
// the Receiver for an RX graph (neither can be added from the palette, so each is
// unique) — and follows the single wire path. The middle nodes (Bits,
// Error-Correction) are optional and user-arranged, so callers inspect the returned
// order to decide what transforms to apply. Returns nil if the endpoint is missing.
func (g *Graph) chainNodes() []*Node {
	var endpoint *Node
	endKind := KindReceiver
	if g.Kind == GraphTx {
		endKind = KindTransmitter
	}
	for _, n := range g.Nodes {
		if n.Kind == endKind {
			endpoint = n
			break
		}
	}
	if endpoint == nil {
		return nil
	}
	order := []*Node{endpoint}
	seen := map[int]bool{endpoint.ID: true}
	// A TX endpoint (Transmitter) is the sink, so walk upstream and reverse; an RX
	// endpoint (Receiver) is the source, so walk downstream directly.
	step := g.outputOf
	if g.Kind == GraphTx {
		step = g.inputOf
	}
	for {
		nxt := step(order[len(order)-1].ID)
		if nxt == nil || seen[nxt.ID] {
			break
		}
		order = append(order, nxt)
		seen[nxt.ID] = true
	}
	if g.Kind == GraphTx {
		for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
			order[i], order[j] = order[j], order[i]
		}
	}
	return order
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

// avgSymbolPower is the mean energy of a modulation's ideal points (|p|² averaged
// over the alphabet). Because the points are normalized to a ±1 corner this is below
// 1 for sparse sets (0.5 for QPSK) and above 1 for dense QAM. The receiver sizes the
// channel noise against this so the constellation scatter — and the EVM-measured SNR
// read off it — line up with the reported power SNR instead of a per-modulation
// offset.
func avgSymbolPower(mod sim.Modulation) float64 {
	pts := constellation(mod)
	if len(pts) == 0 {
		return 1
	}
	sum := 0.0
	for _, p := range pts {
		sum += real(p)*real(p) + imag(p)*imag(p)
	}
	return sum / float64(len(pts))
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

// textToBits converts a message to its bit stream: each ASCII byte spilled MSB
// first. This is the job of the Bits node in a TX chain.
func textToBits(text string) []int {
	bits := make([]int, 0, len(text)*8)
	for _, by := range []byte(text) {
		for k := 7; k >= 0; k-- {
			bits = append(bits, int(by>>uint(k))&1)
		}
	}
	return bits
}

// bitsToSymbols packs a bit stream into constellation points: groups of
// bitsPerSymbol bits → symbol value → ideal point. The last group is zero-padded.
// An empty stream yields a single zero symbol. This is the job of the
// Constellation node in a TX chain.
func bitsToSymbols(bits []int, mod sim.Modulation) []complex128 {
	pts := constellation(mod)
	b := bitsPerSymbol(mod)
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

// bitsPreview formats a bit stream as space-separated bytes ("01001000 …") for
// display inside a Bits node; it is clipped to at most maxBytes bytes.
func bitsPreview(bits []int, maxBytes int) string {
	out := make([]byte, 0, maxBytes*9)
	for i := 0; i < len(bits) && i < maxBytes*8; i++ {
		if i > 0 && i%8 == 0 {
			out = append(out, ' ')
		}
		out = append(out, byte('0'+bits[i]))
	}
	return string(out)
}

// bitsToString formats a whole bit stream as space-separated bytes, for the RX
// readout when no Bits node decodes it — a chain that stops at binary shows the
// binary it actually carries rather than characters it never produced.
func bitsToString(bits []int) string {
	out := make([]byte, 0, len(bits)+len(bits)/8)
	for i, b := range bits {
		if i > 0 && i%8 == 0 {
			out = append(out, ' ')
		}
		out = append(out, byte('0'+b))
	}
	return string(out)
}

// bitsToText reassembles a bit stream into ASCII, MSB first, eight bits per
// character. Non-printable bytes render as "·". This is the job of the Bits node in
// an RX chain (and at low SNR you watch wrong bits become wrong characters).
func bitsToText(bits []int) string {
	var out []byte
	for i := 0; i+8 <= len(bits); i += 8 {
		by := 0
		for j := 0; j < 8; j++ {
			by = by<<1 | bits[i+j]
		}
		if by >= 32 && by < 127 {
			out = append(out, byte(by))
		} else {
			out = append(out, []byte("·")...) // non-printable → dot
		}
	}
	return string(out)
}

// --- error correction ---
//
// An Error-Correction node carries one of several forward-error-correction schemes
// (selectable on the node). Each trades bandwidth (code rate) for robustness: in the
// lab you watch a stronger code fix, at a given noise level, the bit errors that a
// weaker one lets through — and every code eventually break once errors exceed what
// it can repair. TX and RX must agree on the scheme; a mismatch produces garbage,
// exactly like a real link.

// ECC is the forward-error-correction scheme on an Error-Correction node.
type ECC int

const (
	ECCHamming ECC = iota // Hamming(7,4): 1 correctable error per 7-bit block, rate 4/7
	ECCRep3               // repetition ×3, majority vote: 1 error per 3 bits, rate 1/3
	ECCRep5               // repetition ×5, majority vote: 2 errors per 5 bits, rate 1/5
)

// eccName is the human label for a scheme (shown on the node's cycle button).
func eccName(code ECC) string {
	switch code {
	case ECCRep3:
		return "Repetition x3"
	case ECCRep5:
		return "Repetition x5"
	default:
		return "Hamming(7,4)"
	}
}

// eccRate is the code rate (data bits / sent bits) as a display string.
func eccRate(code ECC) string {
	switch code {
	case ECCRep3:
		return "1/3"
	case ECCRep5:
		return "1/5"
	default:
		return "4/7"
	}
}

// eccRateFrac is the code rate (data bits / sent bits) as a number, for throughput.
func eccRateFrac(code ECC) float64 {
	switch code {
	case ECCRep3:
		return 1.0 / 3
	case ECCRep5:
		return 1.0 / 5
	default:
		return 4.0 / 7
	}
}

// nextECC cycles through the schemes (for the node's cycle button).
func nextECC(code ECC) ECC {
	switch code {
	case ECCHamming:
		return ECCRep3
	case ECCRep3:
		return ECCRep5
	default:
		return ECCHamming
	}
}

// eccEncode adds redundancy with the chosen scheme — the job of the Error-Correction
// node in a TX chain.
func eccEncode(code ECC, bits []int) []int {
	switch code {
	case ECCRep3:
		return repEncode(bits, 3)
	case ECCRep5:
		return repEncode(bits, 5)
	default:
		return hammingEncode(bits)
	}
}

// eccDecode removes the redundancy, correcting what the scheme can — the job of the
// Error-Correction node in an RX chain.
func eccDecode(code ECC, bits []int) []int {
	out, _ := eccDecodeCount(code, bits)
	return out
}

// eccDecodeCount is eccDecode plus the number of bit errors the scheme actually
// repaired — the count the lab surfaces as "errors fixed by FEC". It is the
// decoder's own tally (Hamming syndromes that fired, repetition minority votes), so
// it needs no knowledge of what was transmitted; a block hit by more errors than the
// code can handle still counts its one (mis-)correction, which is why the readout
// can show the code working hard while the decoded text breaks.
func eccDecodeCount(code ECC, bits []int) (out []int, corrected int) {
	switch code {
	case ECCRep3:
		return repDecodeCount(bits, 3)
	case ECCRep5:
		return repDecodeCount(bits, 5)
	default:
		return hammingDecodeCount(bits)
	}
}

// repEncode repeats each bit n times (a rate-1/n repetition code).
func repEncode(bits []int, n int) []int {
	out := make([]int, 0, len(bits)*n)
	for _, b := range bits {
		for i := 0; i < n; i++ {
			out = append(out, b)
		}
	}
	return out
}

// repDecode majority-votes each group of n bits back to one, repairing up to
// (n−1)/2 flips per group. Trailing bits that don't fill a group are dropped.
func repDecode(bits []int, n int) []int {
	out, _ := repDecodeCount(bits, n)
	return out
}

// repDecodeCount is repDecode plus the number of repaired flips: in each group, the
// bits that disagree with the majority decision are the ones the code corrected, so
// summing the minority over all groups is the corrected-error count.
func repDecodeCount(bits []int, n int) (out []int, corrected int) {
	out = make([]int, 0, len(bits)/n)
	for i := 0; i+n <= len(bits); i += n {
		sum := 0
		for j := 0; j < n; j++ {
			sum += bits[i+j]
		}
		decision := 0
		if 2*sum > n {
			decision = 1
		}
		out = append(out, decision)
		// Bits that differ from the decision are the minority the vote overrode.
		if decision == 1 {
			corrected += n - sum
		} else {
			corrected += sum
		}
	}
	return out, corrected
}

// Hamming(7,4) is a real single-error-correcting block code: every 4 data bits are
// sent as a 7-bit block carrying 3 parity bits, and the receiver can repair any one
// flipped bit per block. Block layout is the standard [p1 p2 d0 p3 d1 d2 d3] with
// parity positions 1,2,4.

// hammingEncode turns a data bit stream into a coded stream: each group of 4 data
// bits becomes a 7-bit block. The last group is zero-padded. This is the job of the
// Error-Correction node in a TX chain.
func hammingEncode(bits []int) []int {
	out := make([]int, 0, (len(bits)+3)/4*7)
	for i := 0; i < len(bits); i += 4 {
		var d [4]int
		for j := 0; j < 4; j++ {
			if i+j < len(bits) {
				d[j] = bits[i+j]
			}
		}
		p1 := d[0] ^ d[1] ^ d[3]
		p2 := d[0] ^ d[2] ^ d[3]
		p3 := d[1] ^ d[2] ^ d[3]
		out = append(out, p1, p2, d[0], p3, d[1], d[2], d[3])
	}
	return out
}

// hammingDecode inverts hammingEncode: for each 7-bit block it computes the
// syndrome, flips the single bit it points at (if any), and extracts the 4 data
// bits. Trailing bits that don't fill a whole block are dropped. This is the job of
// the Error-Correction node in an RX chain.
func hammingDecode(bits []int) []int {
	out, _ := hammingDecodeCount(bits)
	return out
}

// hammingDecodeCount is hammingDecode plus the number of corrections it made: each
// block whose syndrome is non-zero contributes one repaired bit. A block carrying
// two or more errors still has a non-zero syndrome, so it counts as one correction
// even though it flips the wrong bit — the honest behaviour of a single-error code
// pushed past its limit.
func hammingDecodeCount(bits []int) (out []int, corrected int) {
	out = make([]int, 0, len(bits)/7*4)
	for i := 0; i+7 <= len(bits); i += 7 {
		b := []int{bits[i], bits[i+1], bits[i+2], bits[i+3], bits[i+4], bits[i+5], bits[i+6]}
		// syndrome = error position (1-indexed), 0 = no detected error
		s1 := b[0] ^ b[2] ^ b[4] ^ b[6]
		s2 := b[1] ^ b[2] ^ b[5] ^ b[6]
		s3 := b[3] ^ b[4] ^ b[5] ^ b[6]
		if syn := s1 | s2<<1 | s3<<2; syn != 0 && syn <= 7 {
			b[syn-1] ^= 1
			corrected++
		}
		out = append(out, b[2], b[4], b[5], b[6]) // data at positions 3,5,6,7
	}
	return out, corrected
}
