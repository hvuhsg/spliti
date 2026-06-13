package editor

import (
	"fmt"
	"path/filepath"
	"sync/atomic"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
)

// Export builds a shippable game artifact straight from the editor — the same
// thing `spliti build [--wasm]` produces, but without dropping to a terminal.
// The build runs detached (it takes seconds) with output streaming to the
// Console; checkExport (schedule.First) reports the result on the main thread.

// exportState is the cross-goroutine handshake for an in-flight export, mirroring
// rebuildState: the build runs in a goroutine, the main loop polls done/ok.
type exportState struct {
	running atomic.Bool
	done    atomic.Bool
	ok      atomic.Bool
	label   string // human name of the target, for the completion message
}

// exportTarget describes one buildable artifact.
type exportTarget struct {
	label  string   // menu entry / status text
	output string   // -o name relative to the project root; "" = default (root dir name)
	env    []string // extra env (GOOS/GOARCH for wasm); cgo on otherwise
}

var exportTargets = []exportTarget{
	{label: "native binary", output: "", env: []string{"CGO_ENABLED=1"}},
	{label: "wasm bundle (game.wasm)", output: "game.wasm", env: []string{"GOOS=js", "GOARCH=wasm"}},
}

// startExport kicks off a detached `go build` of the chosen target in the
// project root. checkExport picks up the result.
func (st *state) startExport(t exportTarget) {
	if st.export.running.Load() || st.rebuild.running.Load() {
		return
	}
	st.export.running.Store(true)
	st.export.done.Store(false)
	st.export.label = t.label
	st.logf(logInfo, "exporting %s...", t.label)

	root := st.cfg.ProjectRoot
	// An empty output names the binary after the project dir — what plain
	// `go build .` produces — which avoids colliding with the `game/` package
	// directory the scaffold creates.
	name := t.output
	if name == "" {
		name = filepath.Base(root)
	}
	out := filepath.Join(root, name)
	go func() {
		ok := streamCommandEnv(&st.console, root, t.env, "go", "build", "-o", out, ".")
		st.export.ok.Store(ok)
		st.export.done.Store(true)
	}()
}

// checkExport (schedule.First) finishes an export by logging its outcome.
func checkExport(c *app.Ctx) {
	st := app.GetResource[state](c)
	if !st.export.done.Load() {
		return
	}
	st.export.done.Store(false)
	st.export.running.Store(false)
	if st.export.ok.Load() {
		st.logf(logInfo, "exported %s", st.export.label)
	} else {
		st.logf(logError, "export of %s failed - see Console", st.export.label)
	}
}

// exportControls is the menu-bar "Export" entry: a button opening a popup that
// offers each build target. Disabled while a build (export or rebuild) runs.
func exportControls(st *state) {
	busy := st.export.running.Load() || st.rebuild.running.Load()
	if busy {
		imgui.BeginDisabled()
	}
	if imgui.Button("Export") {
		imgui.OpenPopupStr("export-menu")
	}
	imgui.SetItemTooltip("Build a shippable game artifact (same as `spliti build`)")
	if busy {
		imgui.EndDisabled()
	}
	if imgui.BeginPopup("export-menu") {
		for _, t := range exportTargets {
			if imgui.SelectableBool(t.label) {
				st.startExport(t)
			}
		}
		imgui.EndPopup()
	}
	if st.export.running.Load() {
		imgui.SameLine()
		imgui.TextColored(imgui.Vec4{X: 0.75, Y: 0.8, Z: 1, W: 1},
			fmt.Sprintf("exporting %s...", st.export.label))
	}
}
