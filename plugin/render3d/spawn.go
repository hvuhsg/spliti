package render3d

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// SpawnMesh queues a renderable entity with the given local transform, mesh, and
// material. It bundles Transform3D + GlobalTransform + MeshRenderer + MaterialRef
// so callers can't forget the GlobalTransform the renderer needs. Pass an empty
// material to use the registry's default.
func SpawnMesh(c *app.Commands, t Transform3D, mesh, material string) {
	c.Add(func(w *ecs.World) {
		mp := generic.NewMap4[Transform3D, GlobalTransform, MeshRenderer, MaterialRef](w)
		mp.NewWith(&t, &GlobalTransform{Matrix: m.Identity4()},
			&MeshRenderer{Mesh: mesh}, &MaterialRef{Material: material})
	})
}

// SpawnMeshChild is SpawnMesh with a Parent: the entity's transform is composed
// on top of parent's world transform each frame.
func SpawnMeshChild(c *app.Commands, parent ecs.Entity, t Transform3D, mesh, material string) {
	c.Add(func(w *ecs.World) {
		mp := generic.NewMap5[Transform3D, GlobalTransform, MeshRenderer, MaterialRef, Parent](w)
		mp.NewWith(&t, &GlobalTransform{Matrix: m.Identity4()},
			&MeshRenderer{Mesh: mesh}, &MaterialRef{Material: material}, &Parent{Entity: parent})
	})
}

// SpawnPointLight queues a point light at the given transform. The light's world
// position is taken from its GlobalTransform each frame, so animating the
// transform moves the light.
func SpawnPointLight(c *app.Commands, t Transform3D, light PointLight) {
	c.Add(func(w *ecs.World) {
		mp := generic.NewMap3[Transform3D, GlobalTransform, PointLight](w)
		mp.NewWith(&t, &GlobalTransform{Matrix: m.Identity4()}, &light)
	})
}

// SpawnDirectionalLight queues a directional (sun-like) light. It has no
// position, only a direction, so it needs no transform.
func SpawnDirectionalLight(c *app.Commands, light DirectionalLight) {
	c.Add(func(w *ecs.World) {
		mp := generic.NewMap1[DirectionalLight](w)
		mp.NewWith(&light)
	})
}
