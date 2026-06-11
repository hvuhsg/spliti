package render3d

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Children returns the entities whose Parent is e, in iteration order. There
// is no inverse index — this scans every Parent component — so treat it as an
// occasional helper (UI, tools, teardown), not a per-entity-per-frame call.
func Children(c *app.Ctx, e ecs.Entity) []ecs.Entity {
	var out []ecs.Entity
	app.Query1[Parent](c, func(child ecs.Entity, p *Parent) {
		if p.Entity == e {
			out = append(out, child)
		}
	})
	return out
}

// DespawnRecursive queues removal of an entity and every descendant reachable
// through Parent links (e.g. the tree SpawnModel builds). Like
// Commands.Despawn it is applied at the end of the stage; the whole subtree
// is collected first and then removed, so partial teardown is never observed.
// Safe to call with a stale entity.
func DespawnRecursive(c *app.Commands, root ecs.Entity) {
	c.Add(func(w *ecs.World) {
		if !w.Alive(root) {
			return
		}
		// Parent links point child→parent, so build the inverse adjacency in
		// one pass, then walk down from root. The visited set guards against
		// malformed cyclic chains.
		children := map[ecs.Entity][]ecs.Entity{}
		f := generic.NewFilter1[Parent]()
		q := f.Query(w)
		for q.Next() {
			p := q.Get().Entity
			children[p] = append(children[p], q.Entity())
		}

		visited := map[ecs.Entity]bool{root: true}
		doomed := []ecs.Entity{root}
		for i := 0; i < len(doomed); i++ {
			for _, ch := range children[doomed[i]] {
				if !visited[ch] {
					visited[ch] = true
					doomed = append(doomed, ch)
				}
			}
		}
		for _, e := range doomed {
			if w.Alive(e) {
				w.RemoveEntity(e)
			}
		}
	})
}
