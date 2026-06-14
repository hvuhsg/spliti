package editor

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/srcmodel"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// sceneFilePath resolves the configured scene file against the project root.
func (st *state) sceneFilePath() string {
	if filepath.IsAbs(st.cfg.SceneFile) {
		return st.cfg.SceneFile
	}
	return filepath.Join(st.cfg.ProjectRoot, st.cfg.SceneFile)
}

// loadSceneSource (re)parses the scene file into the in-memory source model.
// On failure the scene goes read-only (srcErr is shown in the toolbar) and the
// previous model, if any, is kept for reference but no longer written.
func (st *state) loadSceneSource() {
	f, err := srcmodel.ParseSceneFile(st.sceneFilePath())
	if err != nil {
		st.src, st.srcErr = nil, err
		return
	}
	if f.Scene(st.cfg.Scene) == nil {
		st.src, st.srcErr = nil, fmt.Errorf("no //spliti:scene %s in %s", st.cfg.Scene, st.cfg.SceneFile)
		return
	}
	st.src, st.srcErr = f, nil
	st.applySymbolicLayers()
}

// flushWriteback runs in schedule.Last: expired live-transform scrubs are
// folded into the source model, then the model is saved once its own debounce
// deadline passes.
func flushWriteback(c *app.Ctx) {
	st := app.GetResource[state](c)
	st.drainDirty(c, false)
	if st.pendingSave && !time.Now().Before(st.saveAt) {
		st.saveNow(c, false)
	}
}

// drainDirty writes expired (or, when forced, all) live-transform edits into
// the scene model and schedules a save.
func (st *state) drainDirty(c *app.Ctx, force bool) {
	if len(st.dirty) == 0 || st.src == nil || st.srcErr != nil {
		return
	}
	now := time.Now()
	sc := st.src.Scene(st.cfg.Scene)
	for inst, deadline := range st.dirty {
		if !force && now.Before(deadline) {
			continue
		}
		delete(st.dirty, inst)
		e, ok := entityByInstance(c, inst)
		if !ok {
			continue
		}
		tm := generic.NewMap[render3d.Transform3D](c.World())
		if !tm.Has(e) {
			continue
		}
		if err := sc.SetTransform(inst, srcmodel.FromTransform3D(*tm.Get(e))); err != nil {
			st.status(err.Error())
			continue
		}
		st.pendingSave = true
		st.saveAt = now
	}
}

// saveNow persists the scene model to disk. force also folds in any pending
// live-transform scrubs first (the Ctrl+S path).
func (st *state) saveNow(c *app.Ctx, force bool) {
	if force {
		st.drainDirty(c, true)
	}
	if st.src == nil || st.srcErr != nil || !st.pendingSave {
		return
	}
	if err := st.src.Save(); err != nil {
		st.status(fmt.Sprintf("save failed: %v", err))
		return
	}
	st.pendingSave = false
	st.status(fmt.Sprintf("saved %s", st.cfg.SceneFile))
}

// reloadScene re-reads the scene file from disk and makes the live world match
// it: transforms and component overrides are applied to existing instances,
// instances missing live are spawned from their prefab, and live instances
// whose spawn line is gone are despawned. Unsaved editor changes and the undo
// history are discarded — disk wins on an explicit reload (and on watcher-
// detected external edits, which share this path).
func reloadScene(c *app.Ctx, st *state) {
	st.dirty = make(map[string]time.Time)
	st.pendingSave = false
	st.loadSceneSource()
	st.undo.Clear()
	if st.srcErr != nil {
		return
	}
	sc := st.src.Scene(st.cfg.Scene)

	// Despawn live instances whose spawn line disappeared.
	inModel := map[string]bool{}
	for _, sp := range sc.Spawns {
		inModel[sp.Instance] = true
	}
	type doomed struct {
		e    ecs.Entity
		name string
	}
	var gone []doomed
	app.Query1[scene.Name](c, func(e ecs.Entity, n *scene.Name) {
		if !inModel[n.Value] {
			gone = append(gone, doomed{e, n.Value})
		}
	})
	for _, d := range gone {
		// The entity may already be dead: a parent earlier in the list takes
		// its subtree with it.
		if c.World().Alive(d.e) {
			despawnSubtree(c, d.e)
			st.deselectIf(d.e)
		}
	}

	// Sync every model spawn into the live world, parents before children
	// (source order guarantees a parent line's instances precede it). Build the
	// name index once, after the despawns above, so per-instance lookups are
	// O(1) instead of an O(n) scan each (O(n²) over the whole scene).
	applied, failed := 0, 0
	idx := buildNameIndex(c)
	for _, sp := range sc.Spawns {
		if err := syncInstanceFromModelInto(c, st, sp, idx); err != nil {
			failed++
			st.status(fmt.Sprintf("%s: %v", sp.Instance, err))
			continue
		}
		applied++
	}
	msg := fmt.Sprintf("reloaded: %d instance(s) synced", applied)
	if len(gone) > 0 {
		msg += fmt.Sprintf(", %d despawned", len(gone))
	}
	if failed > 0 {
		msg += fmt.Sprintf(", %d failed", failed)
	}
	st.status(msg)
}

// entityByInstance finds the live entity tagged with the given scene.Name.
func entityByInstance(c *app.Ctx, instance string) (ecs.Entity, bool) {
	var found ecs.Entity
	ok := false
	app.Query1[scene.Name](c, func(e ecs.Entity, n *scene.Name) {
		if !ok && n.Value == instance {
			found, ok = e, true
		}
	})
	return found, ok
}
