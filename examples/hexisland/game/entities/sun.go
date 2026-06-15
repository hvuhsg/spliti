package entities

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// SpawnSun makes a directional (sun) light. The light casts along the
// transform's forward (-Z) axis, so the scene's XForm().EulerDeg(...) aims it —
// rotate it with the editor gizmo to change the time of day.
//
//spliti:entity
func SpawnSun(c *app.Ctx, t render3d.Transform3D) ecs.Entity {
	mp := generic.NewMap3[render3d.Transform3D, render3d.GlobalTransform, render3d.DirectionalLight](c.World())
	return mp.NewWith(&t, &render3d.GlobalTransform{Matrix: m.Identity4()},
		&render3d.DirectionalLight{
			Color:     m.Vec3{X: 1, Y: 0.98, Z: 0.92},
			Intensity: 3,
		})
}
