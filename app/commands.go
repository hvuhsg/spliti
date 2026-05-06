package app

import "github.com/mlange-42/arche/ecs"

// Commands is a queue of deferred world mutations. Mutations recorded during a
// stage are applied at the end of that stage, before the next one runs.
type Commands struct {
	ops []func(*ecs.World)
}

func newCommands() *Commands { return &Commands{} }

// Add queues an arbitrary world-mutating function. Most callers prefer the
// typed Spawn/Despawn/Insert/Remove helpers.
func (c *Commands) Add(op func(*ecs.World)) {
	c.ops = append(c.ops, op)
}

// Despawn queues removal of an entity. Safe to call with a stale entity; arche
// will report a panic only if the entity ID is from a different world.
func (c *Commands) Despawn(e ecs.Entity) {
	c.ops = append(c.ops, func(w *ecs.World) {
		if w.Alive(e) {
			w.RemoveEntity(e)
		}
	})
}

// apply drains the queue against the world.
func (c *Commands) apply(w *ecs.World) {
	if len(c.ops) == 0 {
		return
	}
	ops := c.ops
	c.ops = nil
	for _, op := range ops {
		op(w)
	}
}
