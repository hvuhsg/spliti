package render3d

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// TestDespawnRecursiveRemovesSubtree builds root→(a→(a1,a2), b), plus an
// unrelated entity, despawns the root recursively, and checks only the
// unrelated entity survives.
func TestDespawnRecursiveRemovesSubtree(t *testing.T) {
	var root ecs.Entity
	var survivors []ecs.Entity
	frame := 0
	app.New().
		AddSystems(schedule.Startup, func(c *app.Ctx) {
			c.Commands().Add(func(w *ecs.World) {
				rootMap := generic.NewMap2[Transform3D, GlobalTransform](w)
				childMap := generic.NewMap3[Transform3D, GlobalTransform, Parent](w)
				ident := GlobalTransform{Matrix: m.Identity4()}
				tr := NewTransform3D(m.Vec3{})

				root = rootMap.NewWith(&tr, &ident)
				a := childMap.NewWith(&tr, &ident, &Parent{Entity: root})
				childMap.NewWith(&tr, &ident, &Parent{Entity: a}) // a1
				childMap.NewWith(&tr, &ident, &Parent{Entity: a}) // a2
				childMap.NewWith(&tr, &ident, &Parent{Entity: root}) // b
				rootMap.NewWith(&tr, &ident) // unrelated
			})
		}).
		AddSystems(schedule.Update, func(c *app.Ctx) {
			if frame == 0 {
				if got := len(Children(c, root)); got != 2 {
					t.Errorf("Children(root) = %d, want 2", got)
				}
				DespawnRecursive(c.Commands(), root)
			} else {
				survivors = survivors[:0]
				app.Query1[Transform3D](c, func(e ecs.Entity, _ *Transform3D) {
					survivors = append(survivors, e)
				})
			}
			frame++
		}).
		SetMaxFrames(2).
		Run()

	if len(survivors) != 1 {
		t.Fatalf("after DespawnRecursive %d entities remain, want 1 (the unrelated root)", len(survivors))
	}
}

// TestDespawnRecursiveStaleRootIsNoOp despawns an already-dead entity
// recursively and expects no panic and no effect.
func TestDespawnRecursiveStaleRootIsNoOp(t *testing.T) {
	var count int
	frame := 0
	var stale ecs.Entity
	app.New().
		AddSystems(schedule.Startup, func(c *app.Ctx) {
			c.Commands().Add(func(w *ecs.World) {
				mp := generic.NewMap2[Transform3D, GlobalTransform](w)
				tr := NewTransform3D(m.Vec3{})
				stale = mp.NewWith(&tr, &GlobalTransform{Matrix: m.Identity4()})
				w.RemoveEntity(stale)
				mp.NewWith(&tr, &GlobalTransform{Matrix: m.Identity4()})
			})
		}).
		AddSystems(schedule.Update, func(c *app.Ctx) {
			if frame == 0 {
				DespawnRecursive(c.Commands(), stale)
			} else {
				count = 0
				app.Query1[Transform3D](c, func(ecs.Entity, *Transform3D) { count++ })
			}
			frame++
		}).
		SetMaxFrames(2).
		Run()

	if count != 1 {
		t.Fatalf("%d entities remain, want 1", count)
	}
}
