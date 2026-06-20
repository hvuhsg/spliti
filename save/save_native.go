//go:build !js

package save

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fileBackend stores one JSON file per slot under a per-game directory in the OS
// user-config location (e.g. ~/Library/Application Support/<appID> on macOS,
// $XDG_CONFIG_HOME/<appID> or ~/.config/<appID> on Linux, %AppData%\<appID> on
// Windows).
type fileBackend struct {
	dir string
}

// openBackend resolves the per-user config directory and ensures <base>/<appID>
// exists.
func openBackend(appID string) (backend, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("save: locate user config dir: %w", err)
	}
	return openDir(filepath.Join(base, appID))
}

// openDir backs a store with an explicit directory, creating it if needed. It is
// the seam tests use (a temp dir) and the native implementation behind Open.
func openDir(dir string) (backend, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("save: create %s: %w", dir, err)
	}
	return &fileBackend{dir: dir}, nil
}

const slotExt = ".json"

func (f *fileBackend) path(slot string) string {
	return filepath.Join(f.dir, slot+slotExt)
}

func (f *fileBackend) read(slot string) ([]byte, error) {
	b, err := os.ReadFile(f.path(slot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("save: read %q: %w", slot, err)
	}
	return b, nil
}

// write is atomic: it writes a temp file in the same directory and renames it
// over the target, so a crash or full disk mid-write leaves the previous save
// intact rather than a truncated file.
func (f *fileBackend) write(slot string, data []byte) error {
	tmp, err := os.CreateTemp(f.dir, slot+".*.tmp")
	if err != nil {
		return fmt.Errorf("save: temp for %q: %w", slot, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("save: write %q: %w", slot, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save: flush %q: %w", slot, err)
	}
	if err := os.Rename(tmpName, f.path(slot)); err != nil {
		return fmt.Errorf("save: commit %q: %w", slot, err)
	}
	return nil
}

func (f *fileBackend) delete(slot string) error {
	err := os.Remove(f.path(slot))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("save: delete %q: %w", slot, err)
	}
	return nil
}

func (f *fileBackend) has(slot string) bool {
	_, err := os.Stat(f.path(slot))
	return err == nil
}

func (f *fileBackend) list() ([]string, error) {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return nil, fmt.Errorf("save: list %s: %w", f.dir, err)
	}
	var slots []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, slotExt) {
			slots = append(slots, strings.TrimSuffix(name, slotExt))
		}
	}
	sort.Strings(slots)
	return slots, nil
}
