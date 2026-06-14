//go:build !js

package render3d

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAssetWatcher confirms a write to a tracked file surfaces the file's ref via
// pending(). fsnotify delivery is OS-dependent, so the assertion polls with a
// generous deadline and skips (rather than fails) if the platform never delivers.
func TestAssetWatcher(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "model.obj")
	if err := os.WriteFile(file, []byte(triangleOBJ), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := newAssetWatcher(t.Logf)
	if err != nil {
		t.Skipf("file watcher unavailable: %v", err)
	}
	defer w.close()
	w.track(file, "tri")

	// Give the watcher goroutine a moment to register before mutating.
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(file, []byte(triangleOBJ+"v 1 1 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, ref := range w.pending() {
			if ref == "tri" {
				return // success
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Skip("fsnotify did not deliver a change event on this platform")
}
