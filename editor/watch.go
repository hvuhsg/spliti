package editor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/hvuhsg/spliti/app"
)

// sceneWatcher feeds external file changes back into the editor. Two classes
// of change, two channels:
//
//   - the scene file itself: re-parsed and diff-applied to the live world
//     without a restart (hand-edit game/scenes/main.go in any IDE);
//   - any other .go file under game/: cannot be hot-loaded into a running Go
//     process, so it raises the rebuild-needed banner instead.
//
// The editor's own atomic saves are recognized by content (srcmodel records
// the bytes it wrote) and ignored.
type sceneWatcher struct {
	w          *fsnotify.Watcher
	sceneEvent chan struct{} // coalesced "scene file changed"
	codeEvent  chan struct{} // coalesced "game code changed"
}

// startWatcher watches the scene file's directory (watching a file directly
// breaks under the atomic-rename saves most editors use) plus the game code
// directories that require a rebuild when touched.
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
	// Rebuild-relevant directories; missing ones are fine (no systems yet).
	for _, dir := range []string{"game", "game/components", "game/entities", "game/systems"} {
		w.Add(filepath.Join(st.cfg.ProjectRoot, dir))
	}
	sw := &sceneWatcher{
		w:          w,
		sceneEvent: make(chan struct{}, 1),
		codeEvent:  make(chan struct{}, 1),
	}
	go sw.run(path)
	st.watch = sw
}

func (sw *sceneWatcher) run(scenePath string) {
	sceneBase := filepath.Base(scenePath)
	sceneDir := filepath.Dir(scenePath)
	for {
		select {
		case ev, ok := <-sw.w.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			name := filepath.Base(ev.Name)
			if !strings.HasSuffix(name, ".go") || strings.HasPrefix(name, ".") {
				continue // editors drop temp/swap files everywhere
			}
			ch := sw.codeEvent
			if name == sceneBase && filepath.Dir(ev.Name) == sceneDir {
				ch = sw.sceneEvent
			}
			select {
			case ch <- struct{}{}:
			default: // already pending; coalesce
			}
		case _, ok := <-sw.w.Errors:
			if !ok {
				return
			}
		}
	}
}

// drainWatcher runs in schedule.First and applies external changes on the
// main thread.
func drainWatcher(c *app.Ctx) {
	st := app.GetResource[state](c)
	if st.watch == nil {
		return
	}
	select {
	case <-st.watch.codeEvent:
		if !st.rebuildNeeded {
			st.rebuildNeeded = true
			st.logf(logWarn, "game code changed on disk - rebuild needed")
		}
	default:
	}
	select {
	case <-st.watch.sceneEvent:
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
	// A reload mid-play would stomp the running game; apply it on Stop.
	if st.mode != modeEdit {
		st.reloadAfterPlay = true
		st.status("scene changed on disk - will reload when play stops")
		return
	}
	if st.unsaved() {
		st.status("scene changed on disk - reloaded; pending editor changes were discarded")
	}
	reloadScene(c, st)
}
