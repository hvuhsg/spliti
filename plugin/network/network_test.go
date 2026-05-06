package network

import (
	"encoding/gob"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
)

// --- wire ---------------------------------------------------------------

func TestWireRoundTripAllKinds(t *testing.T) {
	cases := []frame{
		{Kind: kHello, HelloProtoVersion: protoVersion, HelloClientName: "alice"},
		{Kind: kWelcome, AssignedID: 3, TotalPeers: 4, Seed: 0x1234567890ABCDEF},
		{Kind: kReady},
		{Kind: kInput, Player: 1, InputTick: 42, Inputs: []rawKey{
			{Key: tcell.KeyRune, Rune: 'a'},
			{Key: tcell.KeyUp},
		}},
		{Kind: kInput, Player: 2, InputTick: 7}, // idle tick: no Inputs
		{Kind: kBye, Reason: "shutdown"},
	}
	for i, in := range cases {
		got, err := roundTripFrame(in)
		if err != nil {
			t.Fatalf("case %d (%v): roundTrip: %v", i, in.Kind, err)
		}
		if !framesEqual(in, got) {
			t.Fatalf("case %d (%v): got %+v, want %+v", i, in.Kind, got, in)
		}
	}
}

func framesEqual(a, b frame) bool {
	if a.Kind != b.Kind || a.Player != b.Player {
		return false
	}
	if a.HelloProtoVersion != b.HelloProtoVersion || a.HelloClientName != b.HelloClientName {
		return false
	}
	if a.AssignedID != b.AssignedID || a.TotalPeers != b.TotalPeers || a.Seed != b.Seed {
		return false
	}
	if a.InputTick != b.InputTick || a.Reason != b.Reason {
		return false
	}
	if len(a.Inputs) != len(b.Inputs) {
		return false
	}
	for i := range a.Inputs {
		if a.Inputs[i] != b.Inputs[i] {
			return false
		}
	}
	return true
}

// --- handshake ----------------------------------------------------------

func TestHandshakeHostAndClientConverge(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	hostPlugin := (Plugin{Mode: Host, Players: 2}).applyDefaults()
	clientPlugin := (Plugin{
		Mode:    Client,
		Connect: ln.Addr().String(),
	}).applyDefaults()

	var hostRes, clientRes *handshakeResult
	var hostErr, clientErr error
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		hostRes, hostErr = acceptAndGreet(hostPlugin, ln)
	}()
	go func() {
		defer wg.Done()
		// Brief sleep so the host's Accept has a chance to be ready;
		// not strictly required, but reduces flakiness.
		time.Sleep(20 * time.Millisecond)
		clientRes, clientErr = runClientHandshake(clientPlugin)
	}()
	wg.Wait()

	if hostErr != nil {
		t.Fatalf("host handshake: %v", hostErr)
	}
	if clientErr != nil {
		t.Fatalf("client handshake: %v", clientErr)
	}

	if hostRes.seed != clientRes.seed {
		t.Fatalf("seed mismatch: host=%d client=%d", hostRes.seed, clientRes.seed)
	}
	if hostRes.self != 0 {
		t.Fatalf("host self=%d, want 0", hostRes.self)
	}
	if clientRes.self != 1 {
		t.Fatalf("client self=%d, want 1", clientRes.self)
	}
	if hostRes.total != 2 || clientRes.total != 2 {
		t.Fatalf("total mismatch: host=%d client=%d", hostRes.total, clientRes.total)
	}

	for _, pc := range hostRes.peers {
		pc.close()
	}
	for _, pc := range clientRes.peers {
		pc.close()
	}
}

// --- gate ---------------------------------------------------------------

// pipePeer pairs a local peerConn with the matching server-side gob
// codec, simulating a remote across an in-process pipe.
type pipePeer struct {
	pc  *peerConn
	enc *gob.Encoder
	dec *gob.Decoder
}

func newPipePeer(t *testing.T, id PlayerID) *pipePeer {
	t.Helper()
	cli, srv := net.Pipe()
	t.Cleanup(func() { cli.Close(); srv.Close() })

	pc := newPeerConn(cli, id)
	t.Cleanup(func() { pc.close() })

	return &pipePeer{
		pc:  pc,
		enc: gob.NewEncoder(srv),
		dec: gob.NewDecoder(srv),
	}
}

// drainOne reads one frame from the pipe (the local-side ack the gate sends
// to the remote). Returns the decoded frame.
func (p *pipePeer) drainOne(t *testing.T) frame {
	t.Helper()
	type result struct {
		f   frame
		err error
	}
	done := make(chan result, 1)
	go func() {
		var f frame
		err := p.dec.Decode(&f)
		done <- result{f, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("drainOne: %v", r.err)
		}
		return r.f
	case <-time.After(2 * time.Second):
		t.Fatalf("drainOne: timed out")
		return frame{}
	}
}

func (p *pipePeer) injectInput(t *testing.T, tick uint64, keys ...rawKey) {
	t.Helper()
	if err := p.enc.Encode(frame{
		Kind: kInput, Player: p.pc.id, InputTick: tick, Inputs: keys,
	}); err != nil {
		t.Fatalf("injectInput: %v", err)
	}
}

func newGateApp(t *testing.T, plugin Plugin, peers ...*peerConn) (*app.App, *netState) {
	t.Helper()
	a := app.New()
	app.InsertResource(a, &NetClock{Tick: 0})
	app.InsertResource(a, &Status{Phase: Running})
	state := &netState{plugin: plugin.applyDefaults(), self: 0, peers: peers}
	return a, state
}

func TestGate_NormalFlowEmitsLocalThenRemoteKeys(t *testing.T) {
	pp := newPipePeer(t, 1)
	a, state := newGateApp(t, Plugin{}, pp.pc)

	state.pendingInputs = []rawKey{{Key: tcell.KeyRune, Rune: 'a'}}

	// In a goroutine: drain the local ack, reply with a remote ack.
	go func() {
		_ = pp.drainOne(t)
		pp.injectInput(t, 0, rawKey{Key: tcell.KeyRune, Rune: 'x'})
	}()

	runGate(a.Ctx(), state)

	keys := app.ReadEvents[PlayerKey](a.Ctx())
	if len(keys) != 2 {
		t.Fatalf("got %d PlayerKey events, want 2: %+v", len(keys), keys)
	}
	if keys[0].Player != 0 || keys[0].Rune != 'a' {
		t.Fatalf("first key wrong: %+v", keys[0])
	}
	if keys[1].Player != 1 || keys[1].Rune != 'x' {
		t.Fatalf("second key wrong: %+v", keys[1])
	}

	clk := app.GetResource[NetClock](a.Ctx())
	if clk.Tick != 1 {
		t.Fatalf("tick=%d, want 1", clk.Tick)
	}
}

func TestGate_IdleTickIsNotAStall(t *testing.T) {
	pp := newPipePeer(t, 1)
	a, state := newGateApp(t, Plugin{}, pp.pc)

	// Both peers send empty inputs.
	go func() {
		_ = pp.drainOne(t)
		pp.injectInput(t, 0) // no keys
	}()

	runGate(a.Ctx(), state)

	keys := app.ReadEvents[PlayerKey](a.Ctx())
	if len(keys) != 0 {
		t.Fatalf("got %d keys on an idle tick, want 0", len(keys))
	}
	if app.GetResource[Status](a.Ctx()).Phase != Running {
		t.Fatalf("phase=%v, want Running", app.GetResource[Status](a.Ctx()).Phase)
	}
	if app.GetResource[NetClock](a.Ctx()).Tick != 1 {
		t.Fatalf("tick should still advance on idle, got %d", app.GetResource[NetClock](a.Ctx()).Tick)
	}
}

func TestGate_ConsecutiveTicksClearPriorPlayerKeys(t *testing.T) {
	pp := newPipePeer(t, 1)
	a, state := newGateApp(t, Plugin{}, pp.pc)

	// Tick 0: player 0 presses 'a', remote sends 'x'.
	state.pendingInputs = []rawKey{{Rune: 'a'}}
	go func() {
		_ = pp.drainOne(t)
		pp.injectInput(t, 0, rawKey{Rune: 'x'})
	}()
	runGate(a.Ctx(), state)
	if got := len(app.ReadEvents[PlayerKey](a.Ctx())); got != 2 {
		t.Fatalf("tick 0: got %d keys, want 2", got)
	}

	// Tick 1: nobody presses anything.
	state.pendingInputs = nil
	go func() {
		_ = pp.drainOne(t)
		pp.injectInput(t, 1)
	}()
	runGate(a.Ctx(), state)
	if got := len(app.ReadEvents[PlayerKey](a.Ctx())); got != 0 {
		t.Fatalf("tick 1: got %d keys, want 0 (events should clear between fixed iterations)", got)
	}
}

func TestGate_PauseThenDropFiresPeerLeft(t *testing.T) {
	pp := newPipePeer(t, 1)
	plugin := Plugin{StallGrace: 50 * time.Millisecond, StallPolicy: PauseThenDrop}
	a, state := newGateApp(t, plugin, pp.pc)

	// Drain the local ack but never send a reply — gate stalls and grace fires.
	go func() { _ = pp.drainOne(t) }()

	runGate(a.Ctx(), state)

	left := app.ReadEvents[PeerLeft](a.Ctx())
	if len(left) != 1 || left[0].ID != 1 {
		t.Fatalf("PeerLeft=%+v, want one event for id 1", left)
	}
	if !pp.pc.lost {
		t.Fatalf("peer should be marked lost")
	}
	st := app.GetResource[Status](a.Ctx())
	if st.Phase != PeerLost || st.LostPeer != 1 {
		t.Fatalf("status=%+v, want Phase=PeerLost LostPeer=1", st)
	}
}

func TestGate_LostPeerIsSkippedOnSubsequentTick(t *testing.T) {
	pp := newPipePeer(t, 1)
	plugin := Plugin{StallGrace: 50 * time.Millisecond, StallPolicy: PauseThenDrop}
	a, state := newGateApp(t, plugin, pp.pc)

	go func() { _ = pp.drainOne(t) }()
	runGate(a.Ctx(), state) // marks lost

	// Next tick: gate should run through without blocking on the dead peer.
	start := time.Now()
	runGate(a.Ctx(), state)
	if elapsed := time.Since(start); elapsed > 25*time.Millisecond {
		t.Fatalf("gate took %v on dead peer, expected near-instant", elapsed)
	}
	if app.GetResource[NetClock](a.Ctx()).Tick != 2 {
		t.Fatalf("tick should advance to 2, got %d", app.GetResource[NetClock](a.Ctx()).Tick)
	}
}

// --- plugin smoke ------------------------------------------------------

func TestPluginInstallsResourcesAndSystems(t *testing.T) {
	a := app.New()
	// Don't actually call Build with a real Mode: Host listen — that would
	// block on Accept. Instead, exercise applyDefaults and the resource
	// scaffolding directly.

	p := (Plugin{Mode: Host, Players: 4, StallGrace: 250 * time.Millisecond}).applyDefaults()
	if p.Players != 4 || p.StallGrace != 250*time.Millisecond {
		t.Fatalf("applyDefaults clobbered set fields: %+v", p)
	}
	zero := (Plugin{}).applyDefaults()
	if zero.Players != 2 {
		t.Fatalf("default Players=%d, want 2", zero.Players)
	}
	if zero.StallGrace != 5*time.Second {
		t.Fatalf("default StallGrace=%v, want 5s", zero.StallGrace)
	}
	if zero.HandshakeTimeout != 30*time.Second {
		t.Fatalf("default HandshakeTimeout=%v, want 30s", zero.HandshakeTimeout)
	}

	// Schedule wiring: register the systems manually and run an empty
	// startup to verify the labels resolve without the Mode-aware bits.
	a.AddSystems(schedule.PreUpdate, app.System(func(*app.Ctx) {}).Label("__spliti_net_capture"))
	a.AddSystems(schedule.FixedFirst, app.System(func(*app.Ctx) {}).Label("__spliti_net_gate"))
	a.SetMaxFrames(1).Run()
}
