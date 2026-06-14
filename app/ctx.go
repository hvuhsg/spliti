package app

import (
	"reflect"
	"sync"

	"github.com/mlange-42/arche/ecs"
)

// Ctx is the per-system view of the engine. Systems receive a *Ctx and use it
// to access the World, Commands, resources, and events.
type Ctx struct {
	world    *ecs.World
	app      *App
	commands *Commands

	// filters caches compiled generic query filters keyed by their component
	// type set. Arche filters are expensive to construct (per-call allocation +
	// reflect-keyed component-ID lookups + mask compilation) but cheap to reuse:
	// a compiled filter re-scans archetypes on every Query, so newly created
	// archetypes are still matched. The cache is per-Ctx because arche assigns
	// component IDs per-world, so a filter compiled against one world must never
	// be reused against another. Ctx.world is fixed for the Ctx's lifetime, so
	// this is safe.
	filterMu sync.Mutex
	filters  map[filterKey]any
	// maps caches compiled generic component Maps (used by QueryChanged) under
	// the same per-world invariant as filters.
	maps map[filterKey]any
}

// filterKey identifies a generic filter by its ordered component type set.
// Six slots covers Query1..Query6; unused trailing slots stay nil.
type filterKey [6]reflect.Type

// World returns the underlying arche World.
func (c *Ctx) World() *ecs.World { return c.world }

// App returns the owning App. Use sparingly — prefer dedicated accessors.
func (c *Ctx) App() *App { return c.app }

// Commands returns the deferred-mutation queue. Mutations are applied at the
// end of each stage.
func (c *Ctx) Commands() *Commands { return c.commands }
