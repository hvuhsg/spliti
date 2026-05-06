package app

import "github.com/mlange-42/arche/ecs"

// Ctx is the per-system view of the engine. Systems receive a *Ctx and use it
// to access the World, Commands, resources, and events.
type Ctx struct {
	world    *ecs.World
	app      *App
	commands *Commands
}

// World returns the underlying arche World.
func (c *Ctx) World() *ecs.World { return c.world }

// App returns the owning App. Use sparingly — prefer dedicated accessors.
func (c *Ctx) App() *App { return c.app }

// Commands returns the deferred-mutation queue. Mutations are applied at the
// end of each stage.
func (c *Ctx) Commands() *Commands { return c.commands }
