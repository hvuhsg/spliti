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
}

// flushWriteback runs in schedule.Last: once an instance's debounce deadline
// passes, its live transform is written into the scene source. One Save per
// frame batch.
func flushWriteback(c *app.Ctx) {
	st := app.GetResource[state](c)
	if len(st.dirty) == 0 || st.src == nil {
		return
	}
	now := time.Now()
	sc := st.src.Scene(st.cfg.Scene)
	wrote := false
	for inst, deadline := range st.dirty {
		if now.Before(deadline) {
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
		wrote = true
	}
	if !wrote {
		return
	}
	if err := st.src.Save(); err != nil {
		st.status(fmt.Sprintf("save failed: %v", err))
		return
	}
	st.status(fmt.Sprintf("saved %s", st.cfg.SceneFile))
}

// reloadScene re-reads the scene file from disk and applies every editable
// spawn transform to its live entity. M1 scope: transform sync only —
// added/removed spawn lines are reported but need a restart.
func reloadScene(c *app.Ctx, st *state) {
	// Drop pending writes: disk wins on an explicit reload.
	st.dirty = make(map[string]time.Time)
	st.loadSceneSource()
	if st.srcErr != nil {
		return
	}
	sc := st.src.Scene(st.cfg.Scene)
	applied, missing := 0, 0
	tm := generic.NewMap[render3d.Transform3D](c.World())
	for _, sp := range sc.Spawns {
		if sp.Transform == nil || !sp.Transform.Editable {
			continue
		}
		e, ok := entityByInstance(c, sp.Instance)
		if !ok {
			missing++
			continue
		}
		if !tm.Has(e) {
			continue
		}
		*tm.Get(e) = sp.Transform.Value()
		applied++
	}
	msg := fmt.Sprintf("reloaded: %d transform(s) applied", applied)
	if missing > 0 {
		msg += fmt.Sprintf("; %d new spawn line(s) need a restart", missing)
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
