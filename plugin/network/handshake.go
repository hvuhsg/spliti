package network

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

// handshakeResult is what each handshake flow returns to the rest of the
// plugin: the remote peers, the local id, the total peer count, and the
// shared seed.
type handshakeResult struct {
	peers []*peerConn
	self  PlayerID
	total uint8
	seed  int64
}

// runHostHandshake listens, accepts Players-1 clients, generates the shared
// seed and assigns sequential PlayerIDs, then broadcasts Ready.
func runHostHandshake(p Plugin) (*handshakeResult, error) {
	ln, err := net.Listen("tcp", p.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen %q: %w", p.Listen, err)
	}
	defer ln.Close()
	return acceptAndGreet(p, ln)
}

// acceptAndGreet is the testable inner of the host handshake: it accepts on
// the supplied listener instead of binding one. Tests pass a pre-bound
// localhost listener.
func acceptAndGreet(p Plugin, ln net.Listener) (*handshakeResult, error) {
	seed, err := generateSeed()
	if err != nil {
		return nil, err
	}

	expected := p.Players - 1
	peers := make([]*peerConn, 0, expected)
	deadline := time.Now().Add(p.HandshakeTimeout)

	cleanup := func() {
		for _, pc := range peers {
			pc.close()
		}
	}

	for i := 0; i < expected; i++ {
		if tcpLn, ok := ln.(*net.TCPListener); ok {
			_ = tcpLn.SetDeadline(deadline)
		}
		conn, err := ln.Accept()
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("accept: %w", err)
		}
		clientID := PlayerID(i + 1)
		pc := newPeerConn(conn, clientID)

		// Wait for Hello.
		if err := awaitFrame(pc, kHello, deadline); err != nil {
			pc.close()
			cleanup()
			return nil, fmt.Errorf("client %d: %w", clientID, err)
		}

		if err := pc.send(frame{
			Kind:       kWelcome,
			AssignedID: clientID,
			TotalPeers: uint8(p.Players),
			Seed:       seed,
		}); err != nil {
			pc.close()
			cleanup()
			return nil, fmt.Errorf("send Welcome to client %d: %w", clientID, err)
		}
		peers = append(peers, pc)
	}

	// All peers connected — broadcast Ready.
	for _, pc := range peers {
		if err := pc.send(frame{Kind: kReady}); err != nil {
			cleanup()
			return nil, fmt.Errorf("send Ready: %w", err)
		}
	}

	return &handshakeResult{
		peers: peers,
		self:  0,
		total: uint8(p.Players),
		seed:  seed,
	}, nil
}

// runClientHandshake dials, sends Hello, awaits Welcome, then awaits Ready.
func runClientHandshake(p Plugin) (*handshakeResult, error) {
	conn, err := net.DialTimeout("tcp", p.Connect, p.HandshakeTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial %q: %w", p.Connect, err)
	}
	pc := newPeerConn(conn, 0) // host is always id 0

	if err := pc.send(frame{
		Kind:              kHello,
		HelloProtoVersion: protoVersion,
	}); err != nil {
		pc.close()
		return nil, fmt.Errorf("send Hello: %w", err)
	}

	deadline := time.Now().Add(p.HandshakeTimeout)
	welcome, err := awaitNamedFrame(pc, kWelcome, deadline)
	if err != nil {
		pc.close()
		return nil, fmt.Errorf("Welcome: %w", err)
	}
	if _, err := awaitNamedFrame(pc, kReady, deadline); err != nil {
		pc.close()
		return nil, fmt.Errorf("Ready: %w", err)
	}

	return &handshakeResult{
		peers: []*peerConn{pc},
		self:  welcome.AssignedID,
		total: welcome.TotalPeers,
		seed:  welcome.Seed,
	}, nil
}

// awaitFrame is a thin wrapper over awaitNamedFrame for the host's Hello
// reception, which also validates protoVersion.
func awaitFrame(pc *peerConn, want frameKind, deadline time.Time) error {
	f, err := awaitNamedFrame(pc, want, deadline)
	if err != nil {
		return err
	}
	if want == kHello && f.HelloProtoVersion != protoVersion {
		return fmt.Errorf("protocol version mismatch: peer=%d host=%d", f.HelloProtoVersion, protoVersion)
	}
	return nil
}

func awaitNamedFrame(pc *peerConn, want frameKind, deadline time.Time) (frame, error) {
	for {
		var timeout <-chan time.Time
		if d := time.Until(deadline); d > 0 {
			timer := time.NewTimer(d)
			defer timer.Stop()
			timeout = timer.C
		} else {
			return frame{}, errors.New("handshake deadline expired")
		}
		select {
		case f, ok := <-pc.in:
			if !ok {
				return frame{}, errors.New("connection closed")
			}
			if f.Kind != want {
				// Out-of-sequence during handshake is a protocol error.
				return frame{}, fmt.Errorf("expected %v, got %v", want, f.Kind)
			}
			return f, nil
		case <-timeout:
			return frame{}, errors.New("handshake deadline expired")
		}
	}
}

func generateSeed() (int64, error) {
	var b [8]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("seed: %w", err)
	}
	return int64(binary.LittleEndian.Uint64(b[:])), nil
}
