package editor

import (
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
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
	// Resources are not snapshotted, but the game camera is engine-owned and
	// user-visible in the Game panel — keep it so Stop puts it back.
	if gc := app.GetResource[render3d.Camera3D](c); gc != nil {
		st.gameCamBefore = *gc
	}
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
	// The selection survives the despawn by instance name.
	var selected []string
	for _, e := range st.sel {
		if n := instanceName(c, e); n != "" {
			selected = append(selected, n)
		}
	}
	// With "keep transforms" on, capture where play moved the named instances
	// before the snapshot puts them back.
	var playT map[string]render3d.Transform3D
	tm := generic.NewMap[render3d.Transform3D](c.World())
	if st.keepPlayTransforms {
		playT = map[string]render3d.Transform3D{}
		app.Query1[scene.Name](c, func(e ecs.Entity, n *scene.Name) {
			if tm.Has(e) {
				playT[n.Value] = *tm.Get(e)
			}
		})
	}
	st.snapshot.restore(c)
	st.snapshot = nil
	if gc := app.GetResource[render3d.Camera3D](c); gc != nil {
		*gc = st.gameCamBefore
	}
	st.mode = modeEdit
	st.stepPending, st.stepActive = false, false
	st.clearSelection()
	for _, name := range selected {
		if e, ok := entityByInstance(c, name); ok {
			st.sel = append(st.sel, e)
		}
	}
	st.logf(logInfo, "play stopped: world restored")
	// Re-apply the captured play transforms as one undoable edit (mode is Edit
	// again, so this writes the scene source like any other transform edit).
	if len(playT) > 0 {
		names := make([]string, 0, len(playT))
		for name := range playT {
			names = append(names, name)
		}
		sort.Strings(names)
		var cmds []editorCmd
		for _, name := range names {
			e, ok := entityByInstance(c, name)
			if !ok || !tm.Has(e) {
				continue
			}
			before := *tm.Get(e)
			if before == playT[name] {
				continue
			}
			cmds = append(cmds, &cmdTransform{instance: name, entity: e, before: before, after: playT[name]})
		}
		if len(cmds) > 0 {
			st.push(c, batch("keep play transforms", cmds))
			st.logf(logInfo, "kept play transforms on %d instance(s)", len(cmds))
		}
	}
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
