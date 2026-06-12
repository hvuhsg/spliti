package editor

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/registry"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/mlange-42/arche/ecs"
)

// worldSnapshot is a deep copy of every live entity, taken when Play starts
// and restored when it stops. Components are captured through the registry
// (plain-struct values, so a copy is a deep copy); a component type that is
// not registered is invisible here and will not survive the round trip — the
// registry covers the engine built-ins and every exported struct in the
// game's components package, so in practice that means exotic hand-registered
// types only. Resources are not snapshotted (documented v1 limitation).
type worldSnapshot struct {
	entities []entitySnap
}

type entitySnap struct {
	id    ecs.Entity // pre-play handle, used to remap Parent links on restore
	comps []compSnap
}

type compSnap struct {
	ti  *registry.TypeInfo
	val any
}

// takeSnapshot deep-copies all live entities and their registered components.
func takeSnapshot(c *app.Ctx, reg *registry.Registry) *worldSnapshot {
	w := c.World()
	snap := &worldSnapshot{}
	// Collect handles first: the world is locked while a query is open, and
	// Has/Value may need to register a component ID the world has not seen.
	var all []ecs.Entity
	q := w.Query(ecs.All())
	for q.Next() {
		all = append(all, q.Entity())
	}
	types := reg.Types()
	for _, e := range all {
		es := entitySnap{id: e}
		for _, ti := range types {
			if ti.Has(w, e) {
				es.comps = append(es.comps, compSnap{ti: ti, val: ti.Value(w, e)})
			}
		}
		snap.entities = append(snap.entities, es)
	}
	return snap
}

// restore despawns every live entity (snapshotted and play-created alike) and
// rebuilds the snapshot. Entity handles change; render3d.Parent links are
// remapped through the old->new table. Entity references inside game
// components are not remapped (v1 limitation).
func (s *worldSnapshot) restore(c *app.Ctx) {
	w := c.World()
	var doomed []ecs.Entity
	q := w.Query(ecs.All())
	for q.Next() {
		doomed = append(doomed, q.Entity())
	}
	for _, e := range doomed {
		if w.Alive(e) {
			w.RemoveEntity(e)
		}
	}

	remap := make(map[ecs.Entity]ecs.Entity, len(s.entities))
	for _, es := range s.entities {
		remap[es.id] = w.NewEntity()
	}
	for _, es := range s.entities {
		e := remap[es.id]
		for _, cs := range es.comps {
			val := cs.val
			if p, ok := val.(render3d.Parent); ok {
				if ne, ok := remap[p.Entity]; ok {
					val = render3d.Parent{Entity: ne}
				}
			}
			cs.ti.SetValue(w, e, val)
		}
	}
}
