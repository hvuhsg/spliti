package editor

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/mlange-42/arche/ecs"
)

// Selection is an ordered set of entities; the last element is the primary
// (the inspector target and the gizmo anchor). Plain click replaces the set,
// ctrl/cmd-click toggles membership.

// primary returns the most recently selected live entity.
func (st *state) primary() (ecs.Entity, bool) {
	if len(st.sel) == 0 {
		return ecs.Entity{}, false
	}
	return st.sel[len(st.sel)-1], true
}

// selectOne replaces the selection with e.
func (st *state) selectOne(e ecs.Entity) {
	st.sel = append(st.sel[:0], e)
}

// toggleSelect removes e when present, otherwise appends it as the primary.
func (st *state) toggleSelect(e ecs.Entity) {
	for i, s := range st.sel {
		if s == e {
			st.sel = append(st.sel[:i], st.sel[i+1:]...)
			return
		}
	}
	st.sel = append(st.sel, e)
}

func (st *state) clearSelection() { st.sel = st.sel[:0] }

func (st *state) isSelected(e ecs.Entity) bool {
	for _, s := range st.sel {
		if s == e {
			return true
		}
	}
	return false
}

// deselectIf drops e from the selection (e.g. after a despawn).
func (st *state) deselectIf(e ecs.Entity) {
	for i, s := range st.sel {
		if s == e {
			st.sel = append(st.sel[:i], st.sel[i+1:]...)
			return
		}
	}
}

// pruneSelection drops entities that died since last frame (play-mode
// despawns, external reloads). Runs once per UI frame.
func (st *state) pruneSelection(c *app.Ctx) {
	w := c.World()
	live := st.sel[:0]
	for _, e := range st.sel {
		if w.Alive(e) {
			live = append(live, e)
		}
	}
	st.sel = live
}
