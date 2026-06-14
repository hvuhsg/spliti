//go:build !js

package render3d

import (
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// assetWatcher implements fileWatcher with fsnotify on native builds. It watches
// the directories of tracked asset files (watching a file directly breaks under
// the atomic-rename saves most tools use) and coalesces changes into a set of
// refs the loader drains once per frame. Mirrors editor/watch.go.
type assetWatcher struct {
	w    *fsnotify.Watcher
	logf func(format string, args ...any)

	mu      sync.Mutex
	byPath  map[string]string // absolute file path -> ref
	dirs    map[string]bool   // directories already added to fsnotify
	changed map[string]bool   // refs whose file changed since the last pending()
}

func newAssetWatcher(logf func(format string, args ...any)) (fileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	aw := &assetWatcher{
		w:       w,
		logf:    logf,
		byPath:  make(map[string]string),
		dirs:    make(map[string]bool),
		changed: make(map[string]bool),
	}
	go aw.run()
	return aw, nil
}

func (aw *assetWatcher) track(file, ref string) {
	abs, err := filepath.Abs(file)
	if err != nil {
		abs = file
	}
	dir := filepath.Dir(abs)
	aw.mu.Lock()
	aw.byPath[abs] = ref
	needAdd := !aw.dirs[dir]
	if needAdd {
		aw.dirs[dir] = true
	}
	aw.mu.Unlock()
	if needAdd {
		if err := aw.w.Add(dir); err != nil {
			aw.logf("render3d: watch %q: %v", dir, err)
		}
	}
}

func (aw *assetWatcher) run() {
	for {
		select {
		case ev, ok := <-aw.w.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			abs, err := filepath.Abs(ev.Name)
			if err != nil {
				abs = ev.Name
			}
			aw.mu.Lock()
			if ref, ok := aw.byPath[abs]; ok {
				aw.changed[ref] = true // coalesce: a set, drained per frame
			}
			aw.mu.Unlock()
		case _, ok := <-aw.w.Errors:
			if !ok {
				return
			}
		}
	}
}

func (aw *assetWatcher) pending() []string {
	aw.mu.Lock()
	defer aw.mu.Unlock()
	if len(aw.changed) == 0 {
		return nil
	}
	refs := make([]string, 0, len(aw.changed))
	for ref := range aw.changed {
		refs = append(refs, ref)
	}
	aw.changed = make(map[string]bool)
	return refs
}

func (aw *assetWatcher) close() { aw.w.Close() }
