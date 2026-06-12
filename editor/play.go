package editor

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
)

// Play mode runs the game's own systems inside the editor. Entering Play
// snapshots every live entity; Stop despawns the played world and restores the
// snapshot, so gameplay can never corrupt the scene being edited. While the
// mode is not Edit, every editor mutation is live-only: nothing is written to
// the scene source and nothing enters the undo stack (it would be stale the
// moment Stop reverts the world).

// playMode is the editor's lifecycle state.
type playMode int

const (
	modeEdit playMode = iota
	modePlaying
	modePaused
)

// gameSystem is one game system seen by the registration interceptor; the
// Systems panel lists these and toggles enabled at runtime.
type gameSystem struct {
	stage   schedule.Stage
	name    string
	enabled bool
}

// interceptGameSystem gates one game system registration behind Play mode and
// records it for the Systems panel. Startup stages pass through untouched —
// asset loading and other one-shot setup must run in Edit mode too.
func (st *state) interceptGameSystem(stage schedule.Stage, cfg *app.SystemConfig) *app.SystemConfig {
	switch stage {
	case schedule.Startup, schedule.PreStartup, schedule.PostStartup:
		return cfg
	}
	gs := &gameSystem{stage: stage, name: systemDisplayName(cfg), enabled: true}
	st.gameSystems = append(st.gameSystems, gs)
	return cfg.RunIf(func(*app.Ctx) bool { return st.gameRunning() && gs.enabled })
}

// systemDisplayName names a system for the panel: its label when set, the
// function's own name otherwise.
func systemDisplayName(cfg *app.SystemConfig) string {
	if l := cfg.GetLabel(); l != "" {
		return l
	}
	pc := reflect.ValueOf(cfg.Fn).Pointer()
	if f := runtime.FuncForPC(pc); f != nil {
		name := f.Name() // e.g. "mygame/game/systems.Spin"
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			name = name[i+1:]
		}
		return name
	}
	return "(anonymous)"
}

// gameRunning reports whether game systems should run this frame: in Play, or
// for the single armed frame of a paused Step.
func (st *state) gameRunning() bool {
	return st.mode == modePlaying || (st.mode == modePaused && st.stepActive)
}

// startPlay flushes pending saves, snapshots the world, and starts the game.
func (st *state) startPlay(c *app.Ctx) {
	if st.mode != modeEdit {
		return
	}
	st.saveNow(c, true)
	st.snapshot = takeSnapshot(c, st.reg)
	st.mode = modePlaying
	st.logf(logInfo, "play: %d entities snapshotted", len(st.snapshot.entities))
}

// togglePause switches between Playing and Paused.
func (st *state) togglePause() {
	switch st.mode {
	case modePlaying:
		st.mode = modePaused
	case modePaused:
		st.mode = modePlaying
	}
}

// requestStep arms one frame of game systems while paused. The request is
// promoted to stepActive at the end of the current frame and cleared at the
// end of the next, so every stage sees exactly one running frame.
func (st *state) requestStep() {
	if st.mode == modePaused {
		st.stepPending = true
	}
}

// stepClock runs in schedule.Last after the game's own Last systems (the
// editor registers it later, and same-stage order follows insertion order).
func stepClock(c *app.Ctx) {
	st := app.GetResource[state](c)
	st.stepActive = st.stepPending
	st.stepPending = false
}

// stopPlay despawns the played world, restores the pre-play snapshot, and
// returns to Edit mode. An external scene edit that arrived during play is
// applied afterwards (disk wins, as in Edit mode).
func (st *state) stopPlay(c *app.Ctx) {
	if st.mode == modeEdit || st.snapshot == nil {
		return
	}
	selected := instanceName(c, st.selected) // survives the despawn by name
	st.snapshot.restore(c)
	st.snapshot = nil
	st.mode = modeEdit
	st.stepPending, st.stepActive = false, false
	st.hasSelected = false
	if selected != "" {
		if e, ok := entityByInstance(c, selected); ok {
			st.selected, st.hasSelected = e, true
		}
	}
	st.logf(logInfo, "play stopped: world restored")
	if st.reloadAfterPlay {
		st.reloadAfterPlay = false
		reloadScene(c, st)
	}
}

func (st *state) logf(kind logKind, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	st.status(msg)
	st.console.add(kind, msg)
}
