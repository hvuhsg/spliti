package render3d

import (
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
)

// Transform3D places an entity in 3D space with a translation, a rotation
// (quaternion), and a per-axis scale. It is the local transform; the world
// matrix (after applying any Parent chain) is computed each frame into
// GlobalTransform by the propagateTransforms system.
//
// The zero value is NOT directly usable — a zero Quat and zero Scale are
// invalid. Use NewTransform3D, or rely on the renderer's defensive handling
// (a zero-length rotation is treated as identity and a zero scale as 1).
type Transform3D struct {
	Translation m.Vec3
	Rotation    m.Quat
	Scale       m.Vec3
}

// NewTransform3D returns a Transform3D at pos with identity rotation and unit
// scale.
func NewTransform3D(pos m.Vec3) Transform3D {
	return Transform3D{
		Translation: pos,
		Rotation:    m.IdentityQuat(),
		Scale:       m.Vec3{X: 1, Y: 1, Z: 1},
	}
}

// matrix builds the local TRS matrix, defending against zero-valued rotation
// and scale so a partially-initialized Transform3D still renders sensibly.
func (t Transform3D) matrix() m.Mat4 {
	rot := t.Rotation
	if rot == (m.Quat{}) {
		rot = m.IdentityQuat()
	}
	scale := t.Scale
	if scale == (m.Vec3{}) {
		scale = m.Vec3{X: 1, Y: 1, Z: 1}
	}
	return m.TRS(t.Translation, rot, scale)
}

// Parent links an entity to a parent whose GlobalTransform this entity's local
// transform is applied on top of. Absence of a Parent means the entity is a
// root.
type Parent struct{ Entity ecs.Entity }

// GlobalTransform holds the world-space matrix computed each frame from an
// entity's Transform3D and its parent chain. The renderer reads it; gameplay
// and overlay systems may read it too (e.g. for a light's world position).
type GlobalTransform struct{ Matrix m.Mat4 }

// MeshRenderer marks an entity as drawable and selects which mesh to draw, by
// the key it was registered under in the MeshRegistry. An entity needs
// Transform3D + MeshRenderer to render; MaterialRef is optional.
type MeshRenderer struct{ Mesh string }

// MaterialRef selects which material to shade a MeshRenderer with, by its key in
// the MaterialRegistry. Absent (or an unknown key) falls back to the registry's
// default material.
type MaterialRef struct{ Material string }

// DirectionalLight is an infinitely-distant light (like the sun): all rays are
// parallel along Direction. Color is linear RGB and Intensity scales it. The
// renderer uses a single directional light (the last one queried wins).
type DirectionalLight struct {
	Direction m.Vec3
	Color     m.Vec3
	Intensity float32
}

// PointLight is an omnidirectional light positioned at its entity's
// GlobalTransform translation, with inverse-square falloff clamped to Range.
// Color is linear RGB and Intensity scales it.
type PointLight struct {
	Color     m.Vec3
	Intensity float32
	Range     float32
}
