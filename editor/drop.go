package editor

import (
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
)

// installDropCallback wires the window's OS file-drop event to a queue drained
// on the main thread (the glfw callback fires during event polling, off the
// system goroutine). Dragging files from the OS onto the editor window imports
// them. No-op when the window isn't available.
func (st *state) installDropCallback(c *app.Ctx) {
	win := render3d.Window(c)
	if win == nil {
		return
	}
	win.SetDropCallback(func(_ *glfw.Window, names []string) {
		st.dropMu.Lock()
		st.dropQueue = append(st.dropQueue, names...)
		st.dropMu.Unlock()
	})
}

// drainDrops imports any files dropped onto the window since the last frame.
// Runs in schedule.First on the main thread, so it can touch GPU registries and
// the scene source safely.
func drainDrops(c *app.Ctx) {
	st := app.GetResource[state](c)
	st.dropMu.Lock()
	paths := st.dropQueue
	st.dropQueue = nil
	st.dropMu.Unlock()
	for _, p := range paths {
		st.importAssetFile(c, p)
	}
}
