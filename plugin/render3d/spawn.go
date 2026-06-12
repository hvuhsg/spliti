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

// NewMesh creates a renderable entity immediately (unlike SpawnMesh, which
// queues through Commands) and returns it. Prefab functions — the
// //spliti:entity functions the editor spawns from scene files — use it so the
// entity can be named and parented in the same setup pass. Do not call while a
// query is iterating.
func NewMesh(c *app.Ctx, t Transform3D, mesh, material string) ecs.Entity {
	mp := generic.NewMap4[Transform3D, GlobalTransform, MeshRenderer, MaterialRef](c.World())
	return mp.NewWith(&t, &GlobalTransform{Matrix: m.Identity4()},
		&MeshRenderer{Mesh: mesh}, &MaterialRef{Material: material})
}

// NewPointLight creates a point-light entity immediately and returns it — the
// prefab-friendly counterpart of SpawnPointLight.
func NewPointLight(c *app.Ctx, t Transform3D, light PointLight) ecs.Entity {
	mp := generic.NewMap3[Transform3D, GlobalTransform, PointLight](c.World())
	return mp.NewWith(&t, &GlobalTransform{Matrix: m.Identity4()}, &light)
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

// SpawnModel instantiates a loaded glTF Model as an entity tree under a single
// synthetic root carrying rootTransform, so moving/scaling rootTransform moves
// the whole model. Each glTF node becomes an entity with its local transform
// parented into the hierarchy; every primitive on a node becomes a child entity
// with MeshRenderer + MaterialRef. The renderer's propagateTransforms composes
// the chain each frame.
//
// Queued via Commands like the other spawn helpers: the whole tree is built in
// one deferred op so child Parent links reference live entity IDs.
func SpawnModel(c *app.Commands, model *Model, rootTransform Transform3D) {
	if model == nil {
		return
	}
	c.Add(func(w *ecs.World) {
		rootMap := generic.NewMap2[Transform3D, GlobalTransform](w)
		nodeMap := generic.NewMap3[Transform3D, GlobalTransform, Parent](w)
		primMap := generic.NewMap5[Transform3D, GlobalTransform, MeshRenderer, MaterialRef, Parent](w)
		ident := func() *GlobalTransform { return &GlobalTransform{Matrix: m.Identity4()} }

		rt := rootTransform
		root := rootMap.NewWith(&rt, ident())

		var spawnNode func(idx int, parent ecs.Entity)
		spawnNode = func(idx int, parent ecs.Entity) {
			if idx < 0 || idx >= len(model.Nodes) {
				return
			}
			n := model.Nodes[idx]
			t := n.Transform
			p := Parent{Entity: parent}
			e := nodeMap.NewWith(&t, ident(), &p)
			for i, mesh := range n.MeshRefs {
				mat := ""
				if i < len(n.MatRefs) {
					mat = n.MatRefs[i]
				}
				pt := NewTransform3D(m.Vec3{})
				pp := Parent{Entity: e}
				mr := MeshRenderer{Mesh: mesh}
				ref := MaterialRef{Material: mat}
				primMap.NewWith(&pt, ident(), &mr, &ref, &pp)
			}
			for _, child := range n.Children {
				spawnNode(child, e)
			}
		}
		for _, r := range model.Roots {
			spawnNode(r, root)
		}
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
