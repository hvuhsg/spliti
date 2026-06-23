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

// NewNode creates a transform-only entity immediately and returns it: a node
// with Transform3D + GlobalTransform but no renderable. It is the scene-graph
// "empty" — a parent that groups children under one movable transform (the same
// shape as a model root). Prefab functions use it as the root of a composite
// subtree, then parent meshes/lights under it. The GlobalTransform is recomputed
// from the local transform every frame, so its initial value is just a
// placeholder. Do not call while a query is iterating.
func NewNode(c *app.Ctx, t Transform3D) ecs.Entity {
	mp := generic.NewMap2[Transform3D, GlobalTransform](c.World())
	return mp.NewWith(&t, &GlobalTransform{Matrix: m.Identity4()})
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
		instantiateModel(w, model, rootTransform, nil)
	})
}

// NewModel instantiates a glTF Model immediately (unlike SpawnModel, which
// queues through Commands) and returns the synthetic root entity, so callers can
// drive it: move/rotate the root's Transform3D to move the whole model, query
// the root, or remove the instance with DespawnModelTree. Every entity in the
// instance is tagged ModelTree{Root}. Do not call while a query is iterating.
func NewModel(c *app.Ctx, model *Model, rootTransform Transform3D) ecs.Entity {
	if model == nil {
		return ecs.Entity{}
	}
	w := c.World()
	var all []ecs.Entity
	root := instantiateModel(w, model, rootTransform, func(e ecs.Entity) { all = append(all, e) })
	tag := generic.NewMap1[ModelTree](w)
	for _, e := range all {
		tag.Assign(e, &ModelTree{Root: root})
	}
	return root
}

// DespawnModelTree removes every entity of the model instance whose root is the
// given entity (as tagged by NewModel). It is queued through Commands, so it is
// safe to call from a system.
func DespawnModelTree(c *app.Ctx, root ecs.Entity) {
	var del []ecs.Entity
	app.Query1[ModelTree](c, func(e ecs.Entity, mt *ModelTree) {
		if mt.Root == root {
			del = append(del, e)
		}
	})
	for _, e := range del {
		c.Commands().Despawn(e)
	}
}

// instantiateModel builds the entity tree for a model under a synthetic root and
// returns the root. If onSpawn is non-nil it is called for every created entity
// (root, nodes, primitives), in creation order — used by NewModel to tag and
// collect the instance.
func instantiateModel(w *ecs.World, model *Model, rootTransform Transform3D, onSpawn func(ecs.Entity)) ecs.Entity {
	rootMap := generic.NewMap2[Transform3D, GlobalTransform](w)
	nodeMap := generic.NewMap3[Transform3D, GlobalTransform, Parent](w)
	primMap := generic.NewMap5[Transform3D, GlobalTransform, MeshRenderer, MaterialRef, Parent](w)
	skinnedPrimMap := generic.NewMap6[Transform3D, GlobalTransform, MeshRenderer, MaterialRef, Parent, SkinnedMesh](w)
	ident := func() *GlobalTransform { return &GlobalTransform{Matrix: m.Identity4()} }
	emit := func(e ecs.Entity) {
		if onSpawn != nil {
			onSpawn(e)
		}
	}

	rt := rootTransform
	root := rootMap.NewWith(&rt, ident())
	emit(root)

	// Track each node's spawned entity so an Animator can address them by index.
	nodeEntities := make([]ecs.Entity, len(model.Nodes))

	var spawnNode func(idx int, parent ecs.Entity)
	spawnNode = func(idx int, parent ecs.Entity) {
		if idx < 0 || idx >= len(model.Nodes) {
			return
		}
		n := model.Nodes[idx]
		t := n.Transform
		p := Parent{Entity: parent}
		e := nodeMap.NewWith(&t, ident(), &p)
		emit(e)
		nodeEntities[idx] = e
		for i, mesh := range n.MeshRefs {
			mat := ""
			if i < len(n.MatRefs) {
				mat = n.MatRefs[i]
			}
			pt := NewTransform3D(m.Vec3{})
			pp := Parent{Entity: e}
			mr := MeshRenderer{Mesh: mesh}
			ref := MaterialRef{Material: mat}
			if n.Skin >= 0 && n.Skin < len(model.Skins) {
				// Skinned primitive: carry a SkinnedMesh so computeJointMatrices
				// and the skinned pipeline pick it up. Rig is the model root.
				sm := SkinnedMesh{Rig: root, SkinIdx: n.Skin, Model: model}
				emit(skinnedPrimMap.NewWith(&pt, ident(), &mr, &ref, &pp, &sm))
			} else {
				emit(primMap.NewWith(&pt, ident(), &mr, &ref, &pp))
			}
		}
		for _, child := range n.Children {
			spawnNode(child, e)
		}
	}
	for _, r := range model.Roots {
		spawnNode(r, root)
	}

	// Models with clips or skins get an AnimationRig (node->entity map) on the
	// root so the Animator can drive node transforms and computeJointMatrices can
	// resolve joint world matrices. The Animator plays the first clip looped; it
	// is inert when the model has no animations (a skin-only model).
	if len(model.Animations) > 0 || len(model.Skins) > 0 {
		animMap := generic.NewMap2[Animator, AnimationRig](w)
		animMap.Assign(root,
			&Animator{Model: model, Clip: 0, Speed: 1, Loop: true, Playing: len(model.Animations) > 0},
			&AnimationRig{NodeEntities: nodeEntities})
	}
	return root
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

// SpawnDirectionalLight queues a directional (sun-like) light. Position is
// irrelevant, but the transform's rotation aims it: the light casts along the
// forward (-Z) axis, so build t with EulerDeg or Facing. Animating the
// transform turns the light.
func SpawnDirectionalLight(c *app.Commands, t Transform3D, light DirectionalLight) {
	c.Add(func(w *ecs.World) {
		mp := generic.NewMap3[Transform3D, GlobalTransform, DirectionalLight](w)
		mp.NewWith(&t, &GlobalTransform{Matrix: m.Identity4()}, &light)
	})
}

// NewDirectionalLight creates a directional-light entity immediately and returns
// it — the prefab-friendly counterpart of SpawnDirectionalLight. The light casts
// along the transform's forward (-Z) axis.
func NewDirectionalLight(c *app.Ctx, t Transform3D, light DirectionalLight) ecs.Entity {
	mp := generic.NewMap3[Transform3D, GlobalTransform, DirectionalLight](c.World())
	return mp.NewWith(&t, &GlobalTransform{Matrix: m.Identity4()}, &light)
}

// NewCamera creates a camera entity immediately and returns it — the
// prefab-friendly form. The camera looks along the transform's forward (-Z)
// axis, so build t with EulerDeg or Facing to aim it. When cam.Active the
// entity drives the Camera3D resource (see Camera).
func NewCamera(c *app.Ctx, t Transform3D, cam Camera) ecs.Entity {
	mp := generic.NewMap3[Transform3D, GlobalTransform, Camera](c.World())
	return mp.NewWith(&t, &GlobalTransform{Matrix: m.Identity4()}, &cam)
}

// SpawnCamera is the prefab-shaped (func(c, t) ecs.Entity) camera the editor
// spawns when adding a camera to a scene: a camera entity at t with a default
// perspective projection, Active so it immediately drives the view. It looks
// along the transform's forward (-Z) axis.
func SpawnCamera(c *app.Ctx, t Transform3D) ecs.Entity {
	return NewCamera(c, t, Camera{FovYDeg: 60, Near: 0.1, Far: 1000, OrthoSize: 10, Active: true})
}
