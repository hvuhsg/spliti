package network

import (
	"encoding/gob"
	"errors"
	"net"
	"sync/atomic"
)

// peerConn is one TCP connection to a single remote peer plus the goroutine
// that converts the bytestream into a channel of frames.
//
// Concurrency contract:
//   - Exactly one writer goroutine: the App goroutine after handshake. send()
//     is therefore unsynchronised on purpose.
//   - Exactly one reader goroutine: readLoop, started in newPeerConn.
//   - The App goroutine consumes from in. close() is idempotent.
type peerConn struct {
	id   PlayerID
	conn net.Conn
	enc  *gob.Encoder
	dec  *gob.Decoder
	in   chan frame

	closed atomic.Bool
	lost   bool // set by the gate when the peer is dropped under StallPolicy
}

// newPeerConn wraps c and starts the read goroutine. id is the assigned
// PlayerID of the remote.
func newPeerConn(c net.Conn, id PlayerID) *peerConn {
	pc := &peerConn{
		id:   id,
		conn: c,
		enc:  gob.NewEncoder(c),
		dec:  gob.NewDecoder(c),
		in:   make(chan frame, 64),
	}
	go pc.readLoop()
	return pc
}

func (pc *peerConn) readLoop() {
	for {
		var f frame
		if err := pc.dec.Decode(&f); err != nil {
			close(pc.in)
			return
		}
		pc.in <- f
	}
}

// send encodes and writes one frame. Returns an error on socket failure.
func (pc *peerConn) send(f frame) error {
	if pc.closed.Load() {
		return errors.New("peer connection closed")
	}
	return pc.enc.Encode(f)
}

// close shuts the connection. Safe to call multiple times.
func (pc *peerConn) close() {
	if pc.closed.CompareAndSwap(false, true) {
		_ = pc.conn.Close()
	}
}
