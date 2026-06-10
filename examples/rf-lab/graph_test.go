//go:build !js

package main

import (
	"reflect"
	"testing"

	"github.com/hvuhsg/spliti/examples/radiosim/sim"
)

// allMods is the set of modulations the lab supports, used to assert properties
// that must hold for every constellation.
var allMods = []sim.Modulation{sim.BPSK, sim.QPSK, sim.QAM16, sim.QAM64}

// --- modulation / conversions ---

// TestConstellationDemodRoundTrip verifies the demodulator is the exact inverse of
// the symbol map at zero noise: every constellation point decides back to its own
// index. This is the property the whole receive chain rests on.
func TestConstellationDemodRoundTrip(t *testing.T) {
	for _, mod := range allMods {
		pts := constellation(mod)
		if want := 1 << bitsPerSymbol(mod); len(pts) != want {
			t.Errorf("%s: %d points, want %d", modName(mod), len(pts), want)
		}
		for v, p := range pts {
			if got := nearestSymbolValue(mod, p); got != v {
				t.Errorf("%s: point %d (%v) demodulated to %d", modName(mod), v, p, got)
			}
		}
	}
}

// TestTextBitsRoundTrip verifies textToBits and bitsToText invert each other for
// printable ASCII (8 bits per character, MSB first).
func TestTextBitsRoundTrip(t *testing.T) {
	const msg = "HELLO RF 42!"
	if got := bitsToText(textToBits(msg)); got != msg {
		t.Errorf("round trip: got %q, want %q", got, msg)
	}
}

// TestBitsToSymbolsRoundTrip verifies packing bits into symbols and demodulating
// them back recovers the original bits (the stream is a whole number of symbols, so
// there is no padding to drop).
func TestBitsToSymbolsRoundTrip(t *testing.T) {
	for _, mod := range allMods {
		bps := bitsPerSymbol(mod)
		bits := make([]int, 4*bps) // 4 full symbols
		for i := range bits {
			bits[i] = (i*7 + 3) & 1 // arbitrary but deterministic pattern
		}
		syms := bitsToSymbols(bits, mod)
		var got []int
		for _, s := range syms {
			v := nearestSymbolValue(mod, s)
			for k := bps - 1; k >= 0; k-- {
				got = append(got, (v>>uint(k))&1)
			}
		}
		if !reflect.DeepEqual(got, bits) {
			t.Errorf("%s: round trip got %v, want %v", modName(mod), got, bits)
		}
	}
}

// --- error correction ---

// TestHammingRoundTrip verifies a clean Hamming(7,4) channel recovers the data
// exactly and reports zero corrections.
func TestHammingRoundTrip(t *testing.T) {
	data := []int{1, 0, 1, 1, 0, 0, 1, 0} // two full 4-bit blocks
	dec, corrected := hammingDecodeCount(hammingEncode(data))
	if !reflect.DeepEqual(dec, data) {
		t.Errorf("clean decode got %v, want %v", dec, data)
	}
	if corrected != 0 {
		t.Errorf("clean channel corrected %d, want 0", corrected)
	}
}

// TestHammingCorrectsSingleError verifies the code repairs any one flipped bit per
// 7-bit block — its defining guarantee — and counts exactly one correction.
func TestHammingCorrectsSingleError(t *testing.T) {
	data := []int{1, 0, 1, 1}
	for pos := 0; pos < 7; pos++ {
		coded := hammingEncode(data)
		coded[pos] ^= 1 // inject one error
		dec, corrected := hammingDecodeCount(coded)
		if !reflect.DeepEqual(dec, data) {
			t.Errorf("flip at %d: got %v, want %v", pos, dec, data)
		}
		if corrected != 1 {
			t.Errorf("flip at %d: corrected %d, want 1", pos, corrected)
		}
	}
}

// TestHammingTwoErrorsExceedLimit verifies the honest failure mode of a
// single-error-correcting code: two flips in one block defeat it (the recovered data
// is wrong), yet it still counts the one correction it attempted.
func TestHammingTwoErrorsExceedLimit(t *testing.T) {
	data := []int{1, 0, 1, 1}
	coded := hammingEncode(data)
	coded[1] ^= 1
	coded[4] ^= 1
	dec, corrected := hammingDecodeCount(coded)
	if reflect.DeepEqual(dec, data) {
		t.Error("two errors in one block should not decode correctly")
	}
	if corrected != 1 {
		t.Errorf("corrected %d, want 1 (one mis-correction attempted)", corrected)
	}
}

// TestRepetitionCorrects verifies majority-vote repetition repairs up to ⌊n/2⌋ flips
// per group and counts each overridden minority bit as a correction.
func TestRepetitionCorrects(t *testing.T) {
	data := []int{1, 0, 1}
	// Rep×3 survives one flip per group; Rep×5 survives two.
	if dec, corrected := repDecodeCount([]int{1, 1, 0 /* →1 */, 0, 1, 0 /* →0 */, 1, 0, 1 /* →1 */}, 3); !reflect.DeepEqual(dec, data) || corrected != 3 {
		t.Errorf("rep3: got %v corrected=%d, want %v corrected=3", dec, corrected, data)
	}
	// Rep×5 with two flips in the first group still decodes to 1, with 2 corrections.
	if dec, corrected := repDecodeCount([]int{1, 1, 1, 0, 0}, 5); len(dec) != 1 || dec[0] != 1 || corrected != 2 {
		t.Errorf("rep5: got %v corrected=%d, want [1] corrected=2", dec, corrected)
	}
}

// TestRepetitionExceedsLimit verifies too many flips flip the decision: three of
// five bits wrong in a Rep×5 group decode to the wrong value.
func TestRepetitionExceedsLimit(t *testing.T) {
	dec, _ := repDecodeCount([]int{1, 1, 0, 0, 0}, 5) // 3 zeros win
	if len(dec) != 1 || dec[0] != 0 {
		t.Errorf("rep5 with 3/5 flipped: got %v, want [0]", dec)
	}
}

// TestECCEncodeRoundTripAllSchemes verifies every scheme's encode/decode pair is a
// clean round trip through eccDecodeCount with no errors and no corrections.
func TestECCEncodeRoundTripAllSchemes(t *testing.T) {
	data := []int{1, 0, 1, 1, 0, 1, 0, 0}
	for _, code := range []ECC{ECCHamming, ECCRep3, ECCRep5} {
		dec, corrected := eccDecodeCount(code, eccEncode(code, data))
		if !reflect.DeepEqual(dec, data) {
			t.Errorf("%s: round trip got %v, want %v", eccName(code), dec, data)
		}
		if corrected != 0 {
			t.Errorf("%s: clean channel corrected %d, want 0", eccName(code), corrected)
		}
	}
}

// --- chain walking ---

// kinds extracts the kind sequence of a node list, for comparing chain order.
func kinds(ns []*Node) []NodeKind {
	out := make([]NodeKind, len(ns))
	for i, n := range ns {
		out[i] = n.Kind
	}
	return out
}

// findKind returns the first node of the given kind in a graph, or nil.
func findKind(g *Graph, k NodeKind) *Node {
	for _, n := range g.Nodes {
		if n.Kind == k {
			return n
		}
	}
	return nil
}

// TestChainNodesOrder verifies chainNodes returns the wired nodes in signal-flow
// order — source first, sink last — for both the default TX and RX graphs.
func TestChainNodesOrder(t *testing.T) {
	tx := kinds(newTxGraph().chainNodes())
	wantTx := []NodeKind{KindText, KindBits, KindErrorCorrect, KindConstellation, KindTransmitter}
	if !reflect.DeepEqual(tx, wantTx) {
		t.Errorf("tx order: got %v, want %v", tx, wantTx)
	}
	rx := kinds(newRxGraph().chainNodes())
	wantRx := []NodeKind{KindReceiver, KindConstellation, KindErrorCorrect, KindBits, KindText}
	if !reflect.DeepEqual(rx, wantRx) {
		t.Errorf("rx order: got %v, want %v", rx, wantRx)
	}
}

// TestTxChainComplete verifies a fully wired transmit chain reports ok and returns
// its message, constellation, and (optional) FEC nodes.
func TestTxChainComplete(t *testing.T) {
	g := newTxGraph()
	txt, con, fec, ok := txChain(g)
	if !ok {
		t.Fatal("default tx graph should be a complete chain")
	}
	if txt == nil || txt.Kind != KindText || con == nil || con.Kind != KindConstellation || fec == nil {
		t.Errorf("txChain returned wrong nodes: txt=%v con=%v fec=%v", txt, con, fec)
	}
	if !txRadiates(g) {
		t.Error("a complete chain should radiate")
	}
}

// TestTxChainSeveredIsSilent verifies that cutting any node out of the transmit
// chain makes it incomplete, so the transmitter radiates nothing.
func TestTxChainSeveredIsSilent(t *testing.T) {
	g := newTxGraph()
	g.remove(findKind(g, KindConstellation).ID) // drop a required stage
	if _, _, _, ok := txChain(g); ok {
		t.Error("a chain missing its constellation should not be complete")
	}
	if txRadiates(g) {
		t.Error("a severed transmit chain must not radiate")
	}
}

// TestTxChainNeedsBits verifies the Bits node is required: a Text → Constellation →
// Transmitter chain with no Bits encoder is not a valid transmit chain.
func TestTxChainNeedsBits(t *testing.T) {
	g := newTxGraph()
	bits := findKind(g, KindBits)
	txt := findKind(g, KindText)
	fec := findKind(g, KindErrorCorrect)
	g.remove(bits.ID)
	g.connect(txt.ID, fec.ID) // re-wire around the gap so the path is otherwise whole
	if _, _, _, ok := txChain(g); ok {
		t.Error("a transmit chain without a Bits node should be incomplete")
	}
}

// TestRxChainComplete verifies the default receive chain resolves its constellation,
// FEC, bits-to-text flag, and sink.
func TestRxChainComplete(t *testing.T) {
	con, fec, toText, sink := rxChain(newRxGraph())
	if con == nil || con.Kind != KindConstellation {
		t.Errorf("rxChain con = %v, want a Constellation", con)
	}
	if fec == nil {
		t.Error("rxChain should find the Error-Correction node")
	}
	if !toText {
		t.Error("rxChain should report a Bits node decodes to text")
	}
	if sink == nil || sink.Kind != KindText {
		t.Errorf("rxChain sink = %v, want a Text node", sink)
	}
}

// TestRxChainCutDropsDownstream verifies a cut wire drops every stage past it: with
// no constellation wired to the receiver, nothing demodulates the antenna.
func TestRxChainCutDropsDownstream(t *testing.T) {
	g := newRxGraph()
	g.remove(findKind(g, KindConstellation).ID)
	con, fec, toText, sink := rxChain(g)
	if con != nil || fec != nil || toText || sink != nil {
		t.Errorf("severed rx chain returned con=%v fec=%v toText=%v sink=%v", con, fec, toText, sink)
	}
}

// TestRxChainNoBitsStaysBinary verifies that without a Bits node the chain stops at
// binary: toText is false (the readout shows the bit stream, not characters).
func TestRxChainNoBitsStaysBinary(t *testing.T) {
	g := newRxGraph()
	bits := findKind(g, KindBits)
	fec := findKind(g, KindErrorCorrect)
	txt := findKind(g, KindText)
	g.remove(bits.ID)
	g.connect(fec.ID, txt.ID) // FEC straight to the sink, no ASCII decode
	_, _, toText, sink := rxChain(g)
	if toText {
		t.Error("a chain without a Bits node should not decode to text")
	}
	if sink == nil {
		t.Error("the Text sink should still be reachable")
	}
}
