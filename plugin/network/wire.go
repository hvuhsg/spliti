package network

import (
	"bytes"
	"encoding/gob"
	"io"

	"github.com/gdamore/tcell/v2"
)

const protoVersion uint16 = 1

// frameKind is the discriminator for the frame envelope. Kept tiny so the
// gob wire size stays modest at high tick rates.
type frameKind uint8

const (
	kHello frameKind = iota
	kWelcome
	kReady
	kInput
	kBye
)

// rawKey is the on-wire form of a single key event. Mirrors input.KeyEvent
// but lives in this package so wire.go has no dependency on the input plugin.
type rawKey struct {
	Key  tcell.Key
	Rune rune
	Mod  tcell.ModMask
}

// frame is the single envelope used for every message. Fields irrelevant to
// the current Kind are left zero. Using one struct keeps the gob type
// registry small (one type per connection's lifetime) and round-trip cheap.
type frame struct {
	Kind   frameKind
	Player PlayerID

	// Hello (Client → Host)
	HelloProtoVersion uint16
	HelloClientName   string

	// Welcome (Host → Client)
	AssignedID PlayerID
	TotalPeers uint8
	Seed       int64

	// Input (bidirectional, every tick)
	InputTick uint64
	Inputs    []rawKey

	// Bye
	Reason string
}

// encodeFrame writes f to w using a fresh gob encoder. Only used by tests;
// the real transport keeps long-lived encoders per connection.
func encodeFrame(w io.Writer, f frame) error {
	return gob.NewEncoder(w).Encode(f)
}

// decodeFrame reads a single frame from r using a fresh gob decoder.
func decodeFrame(r io.Reader) (frame, error) {
	var f frame
	if err := gob.NewDecoder(r).Decode(&f); err != nil {
		return frame{}, err
	}
	return f, nil
}

// roundTripFrame is a tiny helper for tests.
func roundTripFrame(f frame) (frame, error) {
	var buf bytes.Buffer
	if err := encodeFrame(&buf, f); err != nil {
		return frame{}, err
	}
	return decodeFrame(&buf)
}
