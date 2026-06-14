package render3d

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/generic"
)

// drawState is one scene pass's per-frame collection buffers plus its GPU
// instance buffer. The main pass and every SceneView each own one: instance
// data is uploaded via queue.WriteBuffer, and all queued writes land before
// the frame's command buffer executes, so passes sharing a buffer would all
// see the last writer's data.
type drawState struct {
	items        []renderItem
	tItems       []renderItem // transparent items, sorted back-to-front each frame
	skinnedItems []renderItem // skeletal-skinned items, drawn one-per-instance
	scratch      []instanceData
	batches      []meshBatch

	instanceBuf *wgpu.Buffer
	instanceCap int // capacity in instances

	// frustum is the pass camera's six clipping planes, rebuilt each frame in
	// drawMeshes and used to cull entities whose world bounds fall outside it.
	frustum frustum

	// Cached optional-component maps for the per-entity probes in drawMeshes.
	// Built once (lazily) and reused: a generic Map embeds per-world component
	// IDs, and the world is fixed for a pass's lifetime, so rebuilding one every
	// frame only churns allocation. mapsReady guards first-use construction.
	mapsReady      bool
	matRefMap      generic.Map[MaterialRef]
	colorMap       generic.Map[InstanceColor]
	transparentMap generic.Map[Transparent]
	skinnedMap     generic.Map[SkinnedMesh]
}

// ensureCap grows the instance buffer to hold at least n instances.
func (d *drawState) ensureCap(g *GPU, n int) {
	if n <= d.instanceCap {
		return
	}
	newCap := d.instanceCap * 2
	if newCap == 0 {
		newCap = initialInstanceCap
	}
	for newCap < n {
		newCap *= 2
	}
	if d.instanceBuf != nil {
		d.instanceBuf.Release()
	}
	d.instanceBuf = newInstanceBuffer(g, newCap)
	d.instanceCap = newCap
}

// SceneView is an additional scene pass: the same world rendered each frame
// from its own camera into its own offscreen RenderTarget, before the main
// pass and inside the same command encoder (so the target is sampleable by
// overlay UIs the same frame). Unlike the main pass it never includes gizmo
// lines — it shows the world exactly as a shipped game would.
//
// Typical uses: an editor's "what the game camera sees" panel, minimaps,
// rear-view mirrors. The caller owns the target and camera; Release frees only
// the view's own GPU buffers.
type SceneView struct {
	rt      *RenderTarget
	cam     *Camera3D
	enabled bool

	frameUBO  [frameUBOFloats]float32
	frameBuf  *wgpu.Buffer
	bindGroup *wgpu.BindGroup

	draw drawState
}

// NewSceneView registers an extra scene pass rendering into rt from cam.
// Returns nil if the GPU resource is not ready or either argument is nil. The
// view starts enabled.
func NewSceneView(c *app.Ctx, rt *RenderTarget, cam *Camera3D) *SceneView {
	g := app.GetResource[GPU](c)
	if g == nil || rt == nil || cam == nil {
		return nil
	}
	frameBuf, err := g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "spliti.render3d.view.frame",
		Size:  frameUBOBytes,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return nil
	}
	v := &SceneView{rt: rt, cam: cam, enabled: true, frameBuf: frameBuf}
	v.bindGroup = newViewBindGroup(g, v)
	v.draw.ensureCap(g, initialInstanceCap)
	g.views = append(g.views, v)
	return v
}

// newViewBindGroup builds the view's group-0 bind group from its own frame
// buffer and the shared point-light buffer. Rebuilt by ensurePointCap whenever
// the light buffer is reallocated.
func newViewBindGroup(g *GPU, v *SceneView) *wgpu.BindGroup {
	bg, err := g.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "spliti.render3d.view.bg",
		Layout: g.frameBGL,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: v.frameBuf, Offset: 0, Size: frameUBOBytes},
			{Binding: 1, Buffer: g.pointBuf, Offset: 0, Size: uint64(g.pointCap * pointLightStride)},
		},
	})
	if err != nil {
		panic("render3d: view bind group: " + err.Error())
	}
	return bg
}

// SetEnabled toggles whether the view renders. A disabled view skips all GPU
// work for the frame (use it when the panel showing the target is hidden).
func (v *SceneView) SetEnabled(b bool) { v.enabled = b }

// Camera returns the camera this view renders from.
func (v *SceneView) Camera() *Camera3D { return v.cam }

// Target returns the render target this view renders into.
func (v *SceneView) Target() *RenderTarget { return v.rt }

// Release unregisters the view and frees its GPU buffers. The render target
// and camera belong to the caller and are untouched.
func (v *SceneView) Release(c *app.Ctx) {
	if g := app.GetResource[GPU](c); g != nil {
		for i, other := range g.views {
			if other == v {
				g.views = append(g.views[:i], g.views[i+1:]...)
				break
			}
		}
	}
	v.releaseGPU()
}

func (v *SceneView) releaseGPU() {
	if v.bindGroup != nil {
		v.bindGroup.Release()
		v.bindGroup = nil
	}
	if v.frameBuf != nil {
		v.frameBuf.Release()
		v.frameBuf = nil
	}
	if v.draw.instanceBuf != nil {
		v.draw.instanceBuf.Release()
		v.draw.instanceBuf = nil
		v.draw.instanceCap = 0
	}
}

// record renders the view's pass into the frame encoder: it rewrites the
// camera-dependent head of the frame UBO over the main pass's light data
// (already assembled by writeFrameUniforms) and records a full mesh pass into
// the view's target.
func (v *SceneView) record(c *app.Ctx, g *GPU, encoder *wgpu.CommandEncoder) {
	if !v.enabled || v.rt == nil || v.rt.colorView == nil || v.cam == nil {
		return
	}
	v.frameUBO = viewFrameUBO(g.frameUBO, v.cam.View(), v.cam.Projection(), v.cam.Position)
	_ = g.queue.WriteBuffer(v.frameBuf, 0, wgpu.ToBytes(v.frameUBO[:]))

	color := wgpu.RenderPassColorAttachment{
		View:       v.rt.colorView,
		LoadOp:     wgpu.LoadOpClear,
		StoreOp:    wgpu.StoreOpStore,
		ClearValue: g.clearColor,
	}
	if v.rt.msaaView != nil {
		color.View = v.rt.msaaView
		color.ResolveTarget = v.rt.colorView
		color.StoreOp = wgpu.StoreOpDiscard
	}
	depth := wgpu.RenderPassDepthStencilAttachment{
		View:            v.rt.depthView,
		DepthLoadOp:     wgpu.LoadOpClear,
		DepthStoreOp:    wgpu.StoreOpStore,
		DepthClearValue: 1.0,
	}
	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments:       []wgpu.RenderPassColorAttachment{color},
		DepthStencilAttachment: &depth,
	})
	pass.SetPipeline(g.pipeline)
	pass.SetBindGroup(0, v.bindGroup, nil)
	drawMeshes(c, g, pass, v.cam, &v.draw)
	pass.End()
	pass.Release()
}

// viewFrameUBO returns base (a fully assembled frame UBO, lights included)
// with its camera-dependent head — view, projection, cameraPos — replaced.
// Pure, so the layout contract is unit-testable.
func viewFrameUBO(base [frameUBOFloats]float32, view, proj m.Mat4, pos m.Vec3) [frameUBOFloats]float32 {
	out := base
	copy(out[0:16], view[:])
	copy(out[16:32], proj[:])
	out[32], out[33], out[34], out[35] = pos.X, pos.Y, pos.Z, 1
	return out
}

// SetSceneCamera overrides the camera driving the main scene pass (frame
// uniforms, frustum culling, transparent sorting). Pass nil to restore the
// Camera3D resource. Package-level picking helpers (ScreenToRay) keep using
// the resource — an editor that installs its own camera here does its own
// unprojection via Camera3D.ScreenToRay on that camera.
func SetSceneCamera(c *app.Ctx, cam *Camera3D) {
	g := app.GetResource[GPU](c)
	if g == nil {
		return
	}
	g.camOverride = cam
}
