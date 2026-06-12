// Package scene defines the runtime vocabulary of editor-managed scene files.
//
// A scene is an ordinary Go setup function (run as a startup system) whose
// statements use the helpers here; the spliti editor parses those statements
// back out of the source, so they double as a machine-editable grammar:
//
//	//spliti:scene Main
//	func Main(c *app.Ctx) {
//	    crate := scene.Spawn(c, "crate1", entities.SpawnCrate(c,
//	        render3d.XForm().At(-1.4, 0.6, 0).EulerDeg(0, 30, 0)))
//	    lamp := scene.Spawn(c, "lamp", entities.SpawnLamp(c, render3d.XForm().At(0, 1.8, 0)))
//	    scene.Parent(c, lamp, crate)
//	    scene.Set(c, crate, components.Health{Max: 50, Current: 50})
//	}
//
// The string passed to Spawn is the instance's stable identity within its
// scene: the editor matches live entities to source lines through the Name
// component, never through entity IDs (which the ECS reuses). Names must be
// unique within one scene function.
//
// The package is wasm-safe and intentionally tiny — shipped games run the same
// scene functions the editor edits.
package scene

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Name identifies a spawned scene instance by the string literal in its
// scene.Spawn call. The editor's hierarchy shows it and all live↔source
// mapping goes through it.
type Name struct{ Value string }

// Spawn tags a freshly spawned entity with its scene instance name and returns
// it, so it wraps a prefab call: scene.Spawn(c, "crate1", entities.SpawnCrate(...)).
func Spawn(c *app.Ctx, name string, e ecs.Entity) ecs.Entity {
	nm := generic.NewMap1[Name](c.World())
	nm.Assign(e, &Name{Value: name})
	return e
}

// Parent makes child's transform local to parent (adding or replacing a
// render3d.Parent link).
func Parent(c *app.Ctx, child, parent ecs.Entity) {
	Set(c, child, render3d.Parent{Entity: parent})
}

// Set adds component v to e, overwriting any existing value of the same type —
// the scene-file form of a per-instance component override.
func Set[T any](c *app.Ctx, e ecs.Entity, v T) {
	w := c.World()
	mp := generic.NewMap[T](w)
	if mp.Has(e) {
		mp.Set(e, &v)
		return
	}
	m1 := generic.NewMap1[T](w)
	m1.Assign(e, &v)
}

// Remove strips component T from e if present — the scene-file form of
// removing a component a prefab added.
func Remove[T any](c *app.Ctx, e ecs.Entity) {
	w := c.World()
	mp := generic.NewMap[T](w)
	if mp.Has(e) {
		m1 := generic.NewMap1[T](w)
		m1.Remove(e)
	}
}
