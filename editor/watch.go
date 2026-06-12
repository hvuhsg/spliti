package editor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/hvuhsg/spliti/app"
)

// sceneWatcher feeds external edits of the scene file back into the editor:
// hand-edit game/scenes/main.go in any IDE and the running editor re-parses
// and re-syncs the live world without a restart. The editor's own atomic
// saves are recognized by content (srcmodel records the bytes it wrote) and
// ignored.
type sceneWatcher struct {
	w      *fsnotify.Watcher
	events chan struct{} // coalesced "scene file changed" signal
}

// startWatcher begins watching the scene file's directory (watching the file
// itself breaks under the atomic-rename saves most editors use).
func (st *state) startWatcher() {
	path := st.sceneFilePath()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		st.status(fmt.Sprintf("file watcher unavailable: %v", err))
		return
	}
	if err := w.Add(filepath.Dir(path)); err != nil {
		st.status(fmt.Sprintf("file watcher unavailable: %v", err))
		w.Close()
		return
	}
	sw := &sceneWatcher{w: w, events: make(chan struct{}, 1)}
	go sw.run(filepath.Base(path))
	st.watch = sw
}

func (sw *sceneWatcher) run(base string) {
	for {
		select {
		case ev, ok := <-sw.w.Events:
			if !ok {
				return
			}
			if filepath.Base(ev.Name) != base {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			select {
			case sw.events <- struct{}{}:
			default: // already pending; coalesce
			}
		case _, ok := <-sw.w.Errors:
			if !ok {
				return
			}
		}
	}
}

// drainWatcher runs in schedule.First and applies external scene-file edits
// on the main thread.
func drainWatcher(c *app.Ctx) {
	st := app.GetResource[state](c)
	if st.watch == nil {
		return
	}
	select {
	case <-st.watch.events:
	default:
		return
	}
	// Self-write guard: the editor's own save comes back through the watcher;
	// recognize it by content and skip the reload.
	if st.src != nil {
		if data, err := os.ReadFile(st.sceneFilePath()); err == nil &&
			bytes.Equal(data, st.src.LastWritten()) {
			return
		}
	}
	if st.unsaved() {
		st.status("scene changed on disk - reloaded; pending editor changes were discarded")
	}
	reloadScene(c, st)
}
