package render3d

import (
	"math"

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

// XForm returns the identity transform (origin, no rotation, unit scale) for
// chaining: render3d.XForm().At(1, 0, -2).EulerDeg(0, 45, 0).Scaled(2, 2, 2).
//
// The chain forms are the editor's canonical transform grammar — scene files
// written by the spliti editor express every spawn transform this way, and the
// editor parses them back. Keep arguments literal in scene setup functions so
// they stay machine-editable.
func XForm() Transform3D { return NewTransform3D(m.Vec3{}) }

// At returns a copy of t translated to (x, y, z).
func (t Transform3D) At(x, y, z float32) Transform3D {
	t.Translation = m.Vec3{X: x, Y: y, Z: z}
	return t
}

// EulerDeg returns a copy of t rotated by intrinsic Tait-Bryan angles in
// degrees, applied in Y (yaw=y), X (pitch=x), Z (roll=z) order — the inverse of
// m.Quat.ToEuler.
func (t Transform3D) EulerDeg(x, y, z float32) Transform3D {
	t.Rotation = m.FromEuler(m.DegToRad(x), m.DegToRad(y), m.DegToRad(z))
	return t
}

// Scaled returns a copy of t with per-axis scale (x, y, z).
func (t Transform3D) Scaled(x, y, z float32) Transform3D {
	t.Scale = m.Vec3{X: x, Y: y, Z: z}
	return t
}

// Rot returns a copy of t with an explicit quaternion rotation — the lossless
// fallback for rotations that don't survive a round trip through Euler angles.
func (t Transform3D) Rot(x, y, z, w float32) Transform3D {
	t.Rotation = m.Quat{X: x, Y: y, Z: z, W: w}.Normalize()
	return t
}

// Facing returns a copy of t rotated so its forward (-Z) axis points along dir
// — the direction a directional light casts, or a model's "forward". dir need
// not be unit; a zero dir leaves the rotation unchanged. Roll is left at the
// shortest-arc minimum, which is all a direction needs. This is a code-side
// authoring convenience and is NOT part of the editor's transform grammar — the
// editor stores rotations as EulerDeg/Rot and edits them with the gizmo.
func (t Transform3D) Facing(dir m.Vec3) Transform3D {
	d := dir.Normalize()
	if d == (m.Vec3{}) {
		return t
	}
	fwd := m.Vec3{Z: -1}
	dot := fwd.Dot(d)
	switch {
	case dot > 0.999999: // already aligned with -Z
		t.Rotation = m.IdentityQuat()
	case dot < -0.999999: // antiparallel: 180° about an arbitrary perpendicular
		t.Rotation = m.FromAxisAngle(m.Vec3{Y: 1}, math.Pi)
	default:
		t.Rotation = m.FromAxisAngle(fwd.Cross(d), float32(math.Acos(float64(dot))))
	}
	return t
}

// Forward returns the world-space forward (-Z) axis of a transform matrix,
// normalized: the direction a camera looks or a directional light casts.
func Forward(mat m.Mat4) m.Vec3 {
	return m.Vec3{X: -mat[8], Y: -mat[9], Z: -mat[10]}.Normalize()
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

// ModelTree tags every entity NewModel creates for one model instance with its
// synthetic root, so the whole instance can be removed together with
// DespawnModelTree. (SpawnModel, the queued variant, does not tag.)
type ModelTree struct{ Root ecs.Entity }

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

// InstanceColor tints a MeshRenderer per instance: the shader multiplies it into
// the material's base color (RGB) and alpha. It lets thousands of entities that
// share one mesh+material be colored individually (heatmap cells, wavefront
// samples) without a material per color. Absent means no tint (opaque white).
//
// The zero value (all components 0) is treated as "no tint" — i.e. white,1 —
// since a literally-black, fully-transparent tint is never useful; use a small
// nonzero alpha if you genuinely want near-black.
type InstanceColor struct{ R, G, B, A float32 }

// orWhite returns the tint, mapping the zero value to opaque white so an
// uninitialized component renders the surface untinted.
func (c InstanceColor) orWhite() m.Vec4 {
	if c == (InstanceColor{}) {
		return m.Vec4{X: 1, Y: 1, Z: 1, W: 1}
	}
	return m.Vec4{X: c.R, Y: c.G, Z: c.B, W: c.A}
}

// Transparent tags a MeshRenderer for the translucent draw pass: it is rendered
// after all opaque geometry, sorted back-to-front, with alpha blending and depth
// writes disabled (so overlapping translucent surfaces don't occlude each other).
// Use it for wavefront shells and other see-through illustrative geometry. The
// alpha comes from the material base color and/or InstanceColor.
type Transparent struct{}

// DirectionalLight is an infinitely-distant light (like the sun): all rays are
// parallel. The direction it casts is the forward (-Z) axis of the entity's
// GlobalTransform, so rotating the entity (gizmo, animation, or a Facing
// transform) aims the light. Color is linear RGB and Intensity scales it. The
// renderer uses a single directional light (the last one queried wins).
type DirectionalLight struct {
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

// Camera marks an entity as a scene camera. The entity poses the view: the eye
// sits at its GlobalTransform translation and looks along the forward (-Z) axis,
// with the transform's up (+Y) axis as the view up — so moving or rotating the
// entity (gizmo, animation, a Facing transform) aims the camera. When Active,
// the entity drives the global Camera3D resource each frame (the camera the
// renderer and game read); among several Active cameras the last queried wins,
// so the editor keeps exactly one active. FovYDeg, Near, and Far are the
// projection parameters; aspect is supplied by the render target, not the
// entity. A non-positive size field leaves the resource's current value, so a
// zero-valued Camera means "pose only, keep the resource's projection".
//
// Projection is perspective by default (FovYDeg). Set Orthographic for an
// orthographic projection sized by OrthoSize (the visible world height).
type Camera struct {
	FovYDeg      float32
	Near         float32
	Far          float32
	Orthographic bool
	OrthoSize    float32
	Active       bool
}
