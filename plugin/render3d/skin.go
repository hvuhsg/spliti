package render3d

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// SkinVertex is a Vertex plus glTF skinning attributes: four joint indices
// (JOINTS_0) and their blend weights (WEIGHTS_0). The first 48 bytes are byte-
// identical to Vertex so the skinned vertex layout reuses @location 0..3, then
// JOINTS_0 (Uint16x4, offset 48) and WEIGHTS_0 (Float32x4, offset 56).
type SkinVertex struct {
	PX, PY, PZ     float32
	NX, NY, NZ     float32
	U, V           float32
	TX, TY, TZ, TW float32
	J0, J1, J2, J3 uint16
	W0, W1, W2, W3 float32
}

const skinVertexStride = 72 // bytes; sizeof(SkinVertex)

const (
	// maxSkinJoints caps the joints per skinned mesh (mirrors the point-light cap).
	maxSkinJoints = 256
	// jointAlignMats is the storage-buffer dynamic-offset alignment expressed in
	// mat4 units: 256-byte alignment / 64 bytes per mat4.
	jointAlignMats = 4
	// jointWindowBytes is the fixed binding window for the dynamic-offset joint
	// bind group: one full skin's worth of matrices.
	jointWindowBytes = maxSkinJoints * 64
)

// SkinnedMesh marks a primitive entity whose vertices deform with a skeleton. Rig
// is the model-root entity holding the AnimationRig (node->entity map); SkinIdx
// selects Model.Skins. The remaining fields are per-frame scratch maintained by
// computeJointMatrices and read by the skinned draw.
type SkinnedMesh struct {
	Rig     ecs.Entity
	SkinIdx int
	Model   *Model

	jointOffset uint32   // byte offset of this entity's joint block in the joint buffer
	scratch     []m.Mat4 // computed joint matrices, reused
	jwScratch   []m.Mat4 // gathered joint world matrices, reused
}

const jointLabel = "__render3d_joints"

// computeJointMatrices builds every skinned entity's joint matrices from the
// current world transforms and packs them into the shared joint storage buffer at
// 256-byte-aligned offsets (recorded per entity for the draw). Runs in PostUpdate
// after transform propagation and before the render pass.
func computeJointMatrices(c *app.Ctx) {
	g := app.GetResource[GPU](c)
	if g == nil {
		return
	}
	world := c.World()
	gtMap := generic.NewMap[GlobalTransform](world)
	rigMap := generic.NewMap[AnimationRig](world)

	g.jointScratch = g.jointScratch[:0]
	app.Query2[SkinnedMesh, GlobalTransform](c, func(_ ecs.Entity, sm *SkinnedMesh, gt *GlobalTransform) {
		if sm.Model == nil || sm.SkinIdx < 0 || sm.SkinIdx >= len(sm.Model.Skins) || !rigMap.Has(sm.Rig) {
			return
		}
		rig := rigMap.Get(sm.Rig)
		skin := &sm.Model.Skins[sm.SkinIdx]

		jw := sm.jwScratch[:0]
		for _, nodeIdx := range skin.Joints {
			wm := m.Identity4()
			if nodeIdx >= 0 && nodeIdx < len(rig.NodeEntities) {
				if ne := rig.NodeEntities[nodeIdx]; gtMap.Has(ne) {
					wm = gtMap.Get(ne).Matrix
				}
			}
			jw = append(jw, wm)
		}
		sm.jwScratch = jw
		sm.scratch = jointMatrices(gt.Matrix, jw, skin.InverseBinds, sm.scratch)

		// Pad the packed buffer up to the alignment boundary, then record this
		// entity's block offset and append its matrices.
		start := len(g.jointScratch)
		if r := start % jointAlignMats; r != 0 {
			for i := 0; i < jointAlignMats-r; i++ {
				g.jointScratch = append(g.jointScratch, m.Identity4())
			}
			start = len(g.jointScratch)
		}
		sm.jointOffset = uint32(start * 64)
		n := len(sm.scratch)
		if n > maxSkinJoints {
			n = maxSkinJoints // clamp; the window binds at most maxSkinJoints
		}
		g.jointScratch = append(g.jointScratch, sm.scratch[:n]...)
	})

	if len(g.jointScratch) == 0 {
		return
	}
	// Headroom so the last block's fixed binding window stays within the buffer.
	ensureJointCap(g, len(g.jointScratch)+maxSkinJoints)
	_ = g.queue.WriteBuffer(g.jointBuf, 0, wgpu.ToBytes(g.jointScratch))
}

// newJointBuffer allocates the joint storage buffer for capacity mat4s.
func newJointBuffer(g *GPU, capacity int) *wgpu.Buffer {
	if capacity < maxSkinJoints {
		capacity = maxSkinJoints
	}
	buf, err := g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "spliti.render3d.joints",
		Size:  uint64(capacity * 64),
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		panic("render3d: joint buffer: " + err.Error())
	}
	return buf
}

// newJointBindGroup builds the group-2 bind group (dynamic-offset joint storage).
func newJointBindGroup(g *GPU) *wgpu.BindGroup {
	bg, err := g.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "spliti.render3d.joints.bg",
		Layout: g.jointBGL,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: g.jointBuf, Offset: 0, Size: jointWindowBytes},
		},
	})
	if err != nil {
		panic("render3d: joint bind group: " + err.Error())
	}
	return bg
}

// ensureJointCap grows the joint buffer to hold at least n mat4s, rebuilding the
// joint bind group (which references the buffer) when it grows.
func ensureJointCap(g *GPU, n int) {
	if n <= g.jointCap {
		return
	}
	newCap := g.jointCap * 2
	if newCap == 0 {
		newCap = maxSkinJoints
	}
	for newCap < n {
		newCap *= 2
	}
	if g.jointBuf != nil {
		g.jointBuf.Release()
	}
	g.jointBuf = newJointBuffer(g, newCap)
	g.jointCap = newCap
	if g.jointBindGroup != nil {
		g.jointBindGroup.Release()
	}
	g.jointBindGroup = newJointBindGroup(g)
}
