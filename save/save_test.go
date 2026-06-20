//go:build !js

package save

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type saveData struct {
	Level int      `json:"level"`
	Coins int      `json:"coins"`
	Items []string `json:"items"`
}

// storeAt builds a Store backed by an explicit directory, the seam Open uses
// internally — so the tests never touch the real user-config location.
func storeAt(t *testing.T, dir string) *Store {
	t.Helper()
	b, err := openDir(dir)
	if err != nil {
		t.Fatalf("openDir: %v", err)
	}
	return &Store{b: b}
}

func TestWriteReadRoundTrip(t *testing.T) {
	s := storeAt(t, t.TempDir())
	want := saveData{Level: 3, Coins: 120, Items: []string{"sword", "shield"}}
	if err := s.Write("slot1", &want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	var got saveData
	if err := s.Read("slot1", &got); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip = %+v, want %+v", got, want)
	}
}

func TestReadMissingIsErrNotFound(t *testing.T) {
	s := storeAt(t, t.TempDir())
	var got saveData
	err := s.Read("nope", &got)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Read(missing) err = %v, want ErrNotFound", err)
	}
	if s.Has("nope") {
		t.Error("Has(missing) = true")
	}
}

func TestWriteReplacesAndHas(t *testing.T) {
	s := storeAt(t, t.TempDir())
	s.Write("p", &saveData{Level: 1})
	s.Write("p", &saveData{Level: 9})
	if !s.Has("p") {
		t.Fatal("Has after Write = false")
	}
	var got saveData
	if err := s.Read("p", &got); err != nil {
		t.Fatal(err)
	}
	if got.Level != 9 {
		t.Errorf("Level = %d, want 9 (overwrite)", got.Level)
	}
}

func TestDeleteThenList(t *testing.T) {
	s := storeAt(t, t.TempDir())
	s.Write("a", &saveData{Level: 1})
	s.Write("b", &saveData{Level: 2})
	s.Write("c", &saveData{Level: 3})

	if err := s.Delete("b"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Deleting a missing slot is not an error.
	if err := s.Delete("b"); err != nil {
		t.Fatalf("Delete(already gone): %v", err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"a", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestAtomicWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	s := storeAt(t, dir)
	if err := s.Write("x", &saveData{Level: 5}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" || filepath.Ext(e.Name()) != ".json" {
			t.Errorf("stray file after atomic write: %s", e.Name())
		}
	}
}

func TestInvalidSlotsRejected(t *testing.T) {
	s := storeAt(t, t.TempDir())
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "a:b", "save*1", "héllo"} {
		if err := s.Write(bad, &saveData{}); err == nil {
			t.Errorf("Write(%q) accepted an invalid slot", bad)
		}
	}
	// A valid slot with the allowed punctuation works.
	if err := s.Write("save-1.auto_v2", &saveData{Level: 1}); err != nil {
		t.Errorf("Write(valid punctuation slot): %v", err)
	}
}

func TestReadRawReturnsStoredJSON(t *testing.T) {
	s := storeAt(t, t.TempDir())
	s.Write("r", &saveData{Level: 7, Coins: 0})
	raw, err := s.ReadRaw("r")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || raw[0] != '{' {
		t.Errorf("ReadRaw returned non-JSON: %q", raw)
	}
}

func TestOpenRejectsBadAppID(t *testing.T) {
	if _, err := Open("bad/id"); err == nil {
		t.Error("Open accepted an appID with a path separator")
	}
}
