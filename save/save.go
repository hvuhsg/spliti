// Package save persists game-defined data (player progress, settings, high
// scores) at runtime and reads it back — the piece a shipped game needs that
// static scene content does not. Scenes live in Go source (the editor's
// code-as-truth format), so the world's initial state needs no loader; this
// package covers the other half: mutable per-player data that must survive
// between sessions.
//
// A Store is a small key/value space of JSON documents. A game opens one for its
// own ID and Writes/Reads its own structs by slot name:
//
//	store, _ := save.Open("mygame")
//	store.Write("slot1", &SaveData{Level: 3, Coins: 120})
//	var data SaveData
//	if err := store.Read("slot1", &data); errors.Is(err, save.ErrNotFound) {
//		data = NewGame() // first run
//	}
//
// It is deliberately not full-world serialization: the game decides what is worth
// persisting and owns the schema, which is portable, versionable, and cheap. The
// backend is platform-split — JSON files under the OS user-config dir on native,
// localStorage in the browser — behind one API.
package save

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound is returned by Read/ReadRaw when the requested slot has never been
// written (or was deleted). Test for it with errors.Is so first-run code can
// fall back to a fresh game.
var ErrNotFound = errors.New("save: slot not found")

// backend is the platform storage seam: raw byte slots keyed by name. Native
// stores one JSON file per slot; the browser uses localStorage. Implementations
// return ErrNotFound from read for a missing slot.
type backend interface {
	read(slot string) ([]byte, error)
	write(slot string, data []byte) error
	delete(slot string) error
	has(slot string) bool
	list() ([]string, error)
}

// Store is a namespaced space of save slots for one game. Open it once and reuse
// it; methods are safe to call from any system. A Store is not guarded for
// concurrent use — drive it from the (single-threaded) game loop like the rest of
// the engine.
type Store struct {
	b backend
}

// Open returns the Store for appID at the platform's default per-user location
// (the OS user-config directory on native, a localStorage namespace in the
// browser). appID names the game and must be a valid slot (it becomes a directory
// or key prefix); reuse the same appID across runs to find earlier saves.
func Open(appID string) (*Store, error) {
	if err := validateSlot(appID); err != nil {
		return nil, fmt.Errorf("save: appID %q: %w", appID, err)
	}
	b, err := openBackend(appID)
	if err != nil {
		return nil, err
	}
	return &Store{b: b}, nil
}

// Write JSON-encodes v and stores it under slot, replacing any existing value.
// On native the write is atomic (temp file + rename), so a crash mid-write never
// corrupts an existing save.
func (s *Store) Write(slot string, v any) error {
	if err := validateSlot(slot); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("save: marshal %q: %w", slot, err)
	}
	return s.b.write(slot, data)
}

// Read JSON-decodes the value stored under slot into v (a pointer). It returns
// ErrNotFound if slot was never written, so callers can branch to first-run
// defaults with errors.Is(err, save.ErrNotFound).
func (s *Store) Read(slot string, v any) error {
	data, err := s.ReadRaw(slot)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("save: unmarshal %q: %w", slot, err)
	}
	return nil
}

// ReadRaw returns the raw stored JSON for slot (ErrNotFound if absent), for
// callers that want to inspect or migrate the document before decoding.
func (s *Store) ReadRaw(slot string) ([]byte, error) {
	if err := validateSlot(slot); err != nil {
		return nil, err
	}
	return s.b.read(slot)
}

// Has reports whether slot currently holds a value.
func (s *Store) Has(slot string) bool {
	if validateSlot(slot) != nil {
		return false
	}
	return s.b.has(slot)
}

// Delete removes slot. Deleting a slot that does not exist is not an error.
func (s *Store) Delete(slot string) error {
	if err := validateSlot(slot); err != nil {
		return err
	}
	return s.b.delete(slot)
}

// List returns the names of every slot in the store, sorted.
func (s *Store) List() ([]string, error) {
	return s.b.list()
}

// validateSlot rejects names that would escape the store or break the backend
// key/path: empty, ".", "..", and anything with a path separator or other
// special character. Slots are restricted to letters, digits, '_', '-', and '.'.
func validateSlot(slot string) error {
	if slot == "" {
		return errors.New("save: slot is empty")
	}
	if slot == "." || slot == ".." {
		return fmt.Errorf("save: slot %q is reserved", slot)
	}
	if strings.ContainsAny(slot, `/\:`+"\x00") {
		return fmt.Errorf("save: slot %q contains a path separator", slot)
	}
	for _, r := range slot {
		ok := r == '_' || r == '-' || r == '.' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return fmt.Errorf("save: slot %q has an invalid character %q (allowed: letters, digits, _ - .)", slot, r)
		}
	}
	return nil
}
