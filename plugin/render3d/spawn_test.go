package render3d

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
)

// TestNewNodeIsTransformOnly checks NewNode spawns a transform-only entity (a
// scene-graph "empty"): it carries Transform3D + GlobalTransform but no
// MeshRenderer, so it groups children under one movable transform without
// rendering anything itself.
func TestNewNodeIsTransformOnly(t *testing.T) {
	var node ecs.Entity
	var hasTransform, hasGlobal, hasMesh bool
	app.New().
		AddSystems(schedule.Startup, func(c *app.Ctx) {
			node = NewNode(c, XForm().At(2, 0, 0))
		}).
		AddSystems(schedule.Update, func(c *app.Ctx) {
			app.Query1[Transform3D](c, func(e ecs.Entity, _ *Transform3D) {
				if e == node {
					hasTransform = true
				}
			})
			app.Query1[GlobalTransform](c, func(e ecs.Entity, _ *GlobalTransform) {
				if e == node {
					hasGlobal = true
				}
			})
			app.Query1[MeshRenderer](c, func(e ecs.Entity, _ *MeshRenderer) {
				if e == node {
					hasMesh = true
				}
			})
		}).
		SetMaxFrames(1).Run()

	if !hasTransform || !hasGlobal {
		t.Errorf("NewNode entity missing Transform3D/GlobalTransform (got %v/%v)", hasTransform, hasGlobal)
	}
	if hasMesh {
		t.Error("NewNode entity must not have a MeshRenderer (it is transform-only)")
	}
}
