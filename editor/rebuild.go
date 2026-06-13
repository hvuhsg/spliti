package editor

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/gen"
)

// Structural game-code changes (new components, prefabs, systems) cannot be
// hot-loaded into a running Go process: the file watcher flags them, the user
// clicks "Rebuild & Restart", and the editor regenerates its own target,
// recompiles it, and re-execs the fresh binary. The session (camera,
// selection) and the ImGui layout survive under .spliti/.

// rebuildState is the cross-goroutine handshake: the build runs detached
// (compiles take seconds; the UI must keep drawing), the main loop polls.
type rebuildState struct {
	running atomic.Bool
	done    atomic.Bool // build finished; ok says how
	ok      atomic.Bool
}

// editorTargetDir is where the generated editor target lives (mirrors
// gen.Project.EditorDir without needing a loaded project).
func editorTargetDir(root string) string {
	return filepath.Join(root, ".spliti", "editor")
}

// editorBinaryPath is the stable path the editor target is built to —
// `spliti edit` builds here first, and every rebuild replaces it, so re-exec
// always has a real binary (os.Executable would point into go-run's tmp dir
// on a first launch).
func editorBinaryPath(root string) string {
	return filepath.Join(editorTargetDir(root), "spliti-editor")
}

// startRebuild kicks off gen + go build in a goroutine, streaming output to
// the Console. checkRebuild (schedule.First) picks up the result.
func (st *state) startRebuild(c *app.Ctx) {
	if st.rebuild.running.Load() {
		return
	}
	if st.mode != modeEdit {
		st.stopPlay(c)
	}
	st.saveNow(c, true)
	st.saveSession(c)
	st.rebuild.running.Store(true)
	st.rebuild.done.Store(false)
	st.logf(logInfo, "rebuilding editor target...")

	root := st.cfg.ProjectRoot
	go func() {
		ok := runRebuild(root, &st.console)
		st.rebuild.ok.Store(ok)
		st.rebuild.done.Store(true)
	}()
}

// runRebuild regenerates .spliti/editor and compiles it to the stable binary
// path. All output lands in the console; returns whether the build succeeded.
func runRebuild(root string, co *console) bool {
	p, err := gen.Load(root)
	if err != nil {
		co.add(logError, fmt.Sprintf("gen: %v", err))
		return false
	}
	if err := p.Generate(); err != nil {
		co.add(logError, fmt.Sprintf("gen: %v", err))
		return false
	}
	dir := p.EditorDir()
	if !streamCommand(co, dir, "go", "mod", "tidy") {
		return false
	}
	return streamCommand(co, dir, "go", "build", "-o", editorBinaryPath(root), ".")
}

// streamCommand runs a command with cgo enabled, feeding its combined output
// line-by-line into the console as it appears.
func streamCommand(co *console, dir string, name string, args ...string) bool {
	return streamCommandEnv(co, dir, []string{"CGO_ENABLED=1"}, name, args...)
}

// streamCommandEnv is streamCommand with caller-supplied env overlaid on the
// process environment (e.g. GOOS=js GOARCH=wasm for the wasm export target).
func streamCommandEnv(co *console, dir string, env []string, name string, args ...string) bool {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		co.add(logError, err.Error())
		return false
	}
	cmd.Stderr = cmd.Stdout
	co.add(logBuild, fmt.Sprintf("$ %s %v", name, args))
	if err := cmd.Start(); err != nil {
		co.add(logError, err.Error())
		return false
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		co.add(logBuild, sc.Text())
	}
	if err := cmd.Wait(); err != nil {
		co.add(logError, fmt.Sprintf("%s: %v", name, err))
		return false
	}
	return true
}

// checkRebuild (schedule.First) finishes a rebuild: on success it stops the
// app and re-execs the new binary from the exit hook; on failure the errors
// are already in the Console.
func checkRebuild(c *app.Ctx) {
	st := app.GetResource[state](c)
	if !st.rebuild.done.Load() {
		return
	}
	st.rebuild.done.Store(false)
	st.rebuild.running.Store(false)
	if !st.rebuild.ok.Load() {
		st.logf(logError, "rebuild failed - see Console")
		return
	}
	st.rebuildNeeded = false
	st.logf(logInfo, "rebuild ok - restarting...")
	st.execOnExit = editorBinaryPath(st.cfg.ProjectRoot)
	c.App().Stop()
}
