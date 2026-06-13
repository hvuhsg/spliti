package registry

import (
	"github.com/hvuhsg/spliti/plugin/collision"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"
)

// Builtin registers the engine component types every 3D scene uses. The
// generated registry_gen.go calls this before adding the game's own types.
func Builtin(r *Registry) {
	Register[render3d.Transform3D](r, "Transform3D", "render3d")
	Register[render3d.MeshRenderer](r, "MeshRenderer", "render3d")
	Register[render3d.MaterialRef](r, "MaterialRef", "render3d")
	Register[render3d.InstanceColor](r, "InstanceColor", "render3d")
	Register[render3d.PointLight](r, "PointLight", "render3d")
	Register[render3d.DirectionalLight](r, "DirectionalLight", "render3d")
	Register[render3d.Camera](r, "Camera", "render3d")
	Register[render3d.Parent](r, "Parent", "render3d")
	Register[render3d.GlobalTransform](r, "GlobalTransform", "render3d")
	Register[scene.Name](r, "Name", "scene")
	Register[collision.Collider3D](r, "Collider3D", "collision")

	// Engine plumbing: present on entities but managed by the engine/editor,
	// not meant for hand editing in the inspector.
	r.Hide("GlobalTransform")
	r.Hide("Name")
	r.Hide("Parent")
}
