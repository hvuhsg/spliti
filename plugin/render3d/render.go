package render3d

import (
	"sort"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// System labels. Users extend the pipeline via AddPreRender/AddOverlay rather
// than raw ordering strings, mirroring plugin/tui and plugin/webgpu.
const (
	inputLabel     = "__render3d_input"
	transformLabel = "__render3d_transform"
	frameLabel     = "__render3d_frame"
	renderLabel    = "__render3d_render"
	gizmoLabel     = "__render3d_gizmo"
	overlayLabel   = "__render3d_overlay"
	presentLabel   = "__render3d_present"
)

// renderItem is one collected drawable before grouping.
type renderItem struct {
	mesh     string
	material string
	model    m.Mat4
	color    m.Vec4  // per-instance tint (default white when no InstanceColor)
	depth    float32 // squared camera distance, used to sort the transparent pass
}

// instanceData is the per-instance GPU record: a model matrix followed by an
// RGBA tint. Its size must equal instanceStride (80 bytes); the layout matches
// the @location(4..8) instance vertex attributes in pbr.wgsl.
type instanceData struct {
	model m.Mat4
	color m.Vec4
}

// meshBatch is a contiguous run of instances sharing a mesh and material, drawn
// with one instanced DrawIndexed call.
type meshBatch struct {
	mesh     string
	material string
	start    int // first instance index into the packed model-matrix buffer
	count    int
}

// installSystems wires the input poll (First) and the transform/frame/render/
// present chain (PostUpdate). Called once from Build.
func installSystems(a *app.App) {
	a.AddSystems(schedule.First, app.System(pollInput).Label(inputLabel))
	a.AddSystems(schedule.PostUpdate,
		app.System(propagateTransforms).Label(transformLabel).Before(renderLabel))
	a.AddSystems(schedule.PostUpdate,
		app.System(writeFrameUniforms).Label(frameLabel).Before(renderLabel).After(transformLabel))
	a.AddSystems(schedule.PostUpdate, app.System(renderSystem).Label(renderLabel))
	a.AddSystems(schedule.PostUpdate,
		app.System(drawGizmos).Label(gizmoLabel).After(renderLabel).Before(overlayLabel))
	a.AddSystems(schedule.PostUpdate,
		app.System(drawOverlay2D).Label(overlayLabel).After(gizmoLabel).Before(presentLabel))
	a.AddSystems(schedule.PostUpdate,
		app.System(presentSystem).Label(presentLabel).After(renderLabel))
}

// writeFrameUniforms builds the camera + light data and uploads it to the GPU
// before the render pass. Runs after transform propagation so point-light world
// positions are current.
func writeFrameUniforms(c *app.Ctx) {
	g := app.GetResource[GPU](c)
	cam := app.GetResource[Camera3D](c)
	if g == nil || cam == nil {
		return
	}

	view := cam.View()
	proj := cam.Projection()
	copy(g.frameUBO[0:16], view[:])
	copy(g.frameUBO[16:32], proj[:])
	// cameraPos
	g.frameUBO[32], g.frameUBO[33], g.frameUBO[34], g.frameUBO[35] =
		cam.Position.X, cam.Position.Y, cam.Position.Z, 1
	// ambient
	g.frameUBO[36], g.frameUBO[37], g.frameUBO[38], g.frameUBO[39] =
		g.ambient.X, g.ambient.Y, g.ambient.Z, 0

	// Directional light: last one queried wins (the renderer supports one).
	var dir DirectionalLight
	app.Query1[DirectionalLight](c, func(_ ecs.Entity, d *DirectionalLight) {
		dir = *d
	})
	g.frameUBO[40], g.frameUBO[41], g.frameUBO[42], g.frameUBO[43] =
		dir.Direction.X, dir.Direction.Y, dir.Direction.Z, dir.Intensity
	g.frameUBO[44], g.frameUBO[45], g.frameUBO[46], g.frameUBO[47] =
		dir.Color.X, dir.Color.Y, dir.Color.Z, 0

	// Point lights: gather world position + params, pack, upload.
	g.collected = g.collected[:0]
	app.Query2[PointLight, GlobalTransform](c, func(_ ecs.Entity, p *PointLight, gt *GlobalTransform) {
		pos := m.Vec3{X: gt.Matrix[12], Y: gt.Matrix[13], Z: gt.Matrix[14]}
		g.collected = append(g.collected, collectedPoint{
			Pos: pos, Color: p.Color, Intensity: p.Intensity, Range: p.Range,
		})
	})
	g.lights = packLights(g.collected, MaxPointLights, g.lights[:0])
	count := len(g.lights)
	g.frameUBO[48], g.frameUBO[49], g.frameUBO[50], g.frameUBO[51] = float32(count), 0, 0, 0

	_ = g.queue.WriteBuffer(g.frameBuf, 0, wgpu.ToBytes(g.frameUBO[:]))
	if count > 0 {
		ensurePointCap(g, count)
		_ = g.queue.WriteBuffer(g.pointBuf, 0, wgpu.ToBytes(g.lights))
	}
}

// renderSystem acquires the surface texture, opens a render pass with depth, and
// records all mesh draws. It leaves the pass open so overlay systems (After
// render, Before present) can append draws; presentSystem closes it.
func renderSystem(c *app.Ctx) {
	g := app.GetResource[GPU](c)
	if g == nil {
		return
	}
	g.frameActive = false

	tex, err := g.surface.GetCurrentTexture()
	if err != nil {
		// Surface lost/outdated (often a resize mid-flight). Reconfigure and skip
		// this frame; the next one recovers.
		g.surface.Configure(g.adapter, g.device, g.config)
		return
	}
	view, err := tex.CreateView(nil)
	if err != nil {
		tex.Release()
		return
	}
	encoder, err := g.device.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{
		Label: "spliti.render3d.encoder",
	})
	if err != nil {
		view.Release()
		tex.Release()
		return
	}

	color := wgpu.RenderPassColorAttachment{
		View:       view,
		LoadOp:     wgpu.LoadOpClear,
		StoreOp:    wgpu.StoreOpStore,
		ClearValue: g.clearColor,
	}
	if g.msaaView != nil {
		color.View = g.msaaView
		color.ResolveTarget = view
		color.StoreOp = wgpu.StoreOpDiscard
	}
	depth := wgpu.RenderPassDepthStencilAttachment{
		View:            g.depthView,
		DepthLoadOp:     wgpu.LoadOpClear,
		DepthStoreOp:    wgpu.StoreOpStore,
		DepthClearValue: 1.0,
	}
	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments:       []wgpu.RenderPassColorAttachment{color},
		DepthStencilAttachment: &depth,
	})

	g.curTex, g.curView, g.curEncoder, g.curPass = tex, view, encoder, pass
	g.frameActive = true

	pass.SetPipeline(g.pipeline)
	pass.SetBindGroup(0, g.frameBindGroup, nil)

	drawMeshes(c, g, pass)
}

// drawMeshes collects MeshRenderer+GlobalTransform entities (optional
// MaterialRef / InstanceColor / Transparent), draws opaque ones grouped by
// mesh+material into instanced batches, then draws Transparent ones back-to-front
// with the blended pipeline. All instance records (model matrix + tint) are
// uploaded once into the shared instance buffer: opaque batches first, then the
// sorted transparent instances.
func drawMeshes(c *app.Ctx, g *GPU, pass *wgpu.RenderPassEncoder) {
	meshes := app.GetResource[MeshRegistry](c)
	materials := app.GetResource[MaterialRegistry](c)
	if meshes == nil || materials == nil {
		return
	}
	cam := app.GetResource[Camera3D](c)
	world := c.World()
	matRefMap := generic.NewMap[MaterialRef](world)
	colorMap := generic.NewMap[InstanceColor](world)
	transparentMap := generic.NewMap[Transparent](world)

	g.items = g.items[:0]
	g.tItems = g.tItems[:0]
	app.Query2[MeshRenderer, GlobalTransform](c, func(e ecs.Entity, mr *MeshRenderer, gt *GlobalTransform) {
		if meshes.get(mr.Mesh) == nil {
			return // unknown mesh: skip
		}
		mat := ""
		if matRefMap.Has(e) {
			mat = matRefMap.Get(e).Material
		}
		col := m.Vec4{X: 1, Y: 1, Z: 1, W: 1}
		if colorMap.Has(e) {
			col = colorMap.Get(e).orWhite()
		}
		it := renderItem{mesh: mr.Mesh, material: mat, model: gt.Matrix, color: col}
		if transparentMap.Has(e) {
			it.depth = cameraDepthSq(cam, gt.Matrix)
			g.tItems = append(g.tItems, it)
		} else {
			g.items = append(g.items, it)
		}
	})

	g.scratch = g.scratch[:0]
	g.batches = g.batches[:0]

	// Opaque: batch by (mesh, material); depth buffer handles ordering.
	opaqueCount := 0
	if len(g.items) > 0 {
		g.scratch, g.batches = packMeshInstances(g.items, g.scratch, g.batches)
		opaqueCount = len(g.scratch)
	}

	// Transparent: sort back-to-front (far first) so blending composites correctly
	// without depth writes; a sorted set can't be instance-batched, so each draws
	// as a single instance appended after the opaque records.
	sort.SliceStable(g.tItems, func(i, j int) bool { return g.tItems[i].depth > g.tItems[j].depth })
	for _, it := range g.tItems {
		g.scratch = append(g.scratch, instanceData{model: it.model, color: it.color})
	}

	if len(g.scratch) == 0 {
		return
	}
	ensureInstanceCap(g, len(g.scratch))
	if err := g.queue.WriteBuffer(g.instanceBuf, 0, wgpu.ToBytes(g.scratch)); err != nil {
		return
	}

	// Opaque draws use the pipeline + frame bind group already bound by renderSystem.
	for _, b := range g.batches {
		gm := meshes.get(b.mesh)
		if gm == nil {
			continue
		}
		mat := materials.get(b.material)
		off := uint64(b.start * instanceStride)
		size := uint64(b.count * instanceStride)
		pass.SetBindGroup(1, mat.bindGroup, nil)
		pass.SetVertexBuffer(0, gm.vbuf, 0, gm.vbuf.GetSize())
		// The instance buffer binding is already offset to this batch's start, so
		// firstInstance is 0 (not b.start) — otherwise the base instance would be
		// counted twice and read past the bound range.
		pass.SetVertexBuffer(1, g.instanceBuf, off, size)
		pass.SetIndexBuffer(gm.ibuf, wgpu.IndexFormatUint32, 0, gm.ibuf.GetSize())
		pass.DrawIndexed(gm.indexCount, uint32(b.count), 0, 0, 0)
	}

	// Transparent draws: switch to the blended pipeline (group-0 frame bind group
	// is layout-compatible and stays bound) and draw each sorted instance.
	if len(g.tItems) > 0 {
		pass.SetPipeline(g.transparentPipeline)
		for i, it := range g.tItems {
			gm := meshes.get(it.mesh)
			if gm == nil {
				continue
			}
			mat := materials.get(it.material)
			off := uint64((opaqueCount + i) * instanceStride)
			pass.SetBindGroup(1, mat.bindGroup, nil)
			pass.SetVertexBuffer(0, gm.vbuf, 0, gm.vbuf.GetSize())
			pass.SetVertexBuffer(1, g.instanceBuf, off, uint64(instanceStride))
			pass.SetIndexBuffer(gm.ibuf, wgpu.IndexFormatUint32, 0, gm.ibuf.GetSize())
			pass.DrawIndexed(gm.indexCount, 1, 0, 0, 0)
		}
	}
}

// cameraDepthSq returns the squared distance from the camera to an instance's
// world-space origin (model translation), used to sort the transparent pass.
func cameraDepthSq(cam *Camera3D, model m.Mat4) float32 {
	if cam == nil {
		return 0
	}
	d := m.Vec3{X: model[12], Y: model[13], Z: model[14]}.Sub(cam.Position)
	return d.LengthSq()
}

// packMeshInstances sorts items by (mesh, material) so identical pairs are
// adjacent, packs their model matrices into draw order, and returns the matrices
// and the contiguous same-(mesh,material) batches. Pure: the destination slices
// are reused (pass them pre-truncated). Depth ordering is handled by the depth
// buffer, so draw order within the frame is free to optimize for batching.
func packMeshInstances(items []renderItem, scratch []instanceData, batches []meshBatch) ([]instanceData, []meshBatch) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].mesh != items[j].mesh {
			return items[i].mesh < items[j].mesh
		}
		return items[i].material < items[j].material
	})
	for i, it := range items {
		scratch = append(scratch, instanceData{model: it.model, color: it.color})
		if i > 0 && it.mesh == items[i-1].mesh && it.material == items[i-1].material {
			batches[len(batches)-1].count++
		} else {
			batches = append(batches, meshBatch{mesh: it.mesh, material: it.material, start: i, count: 1})
		}
	}
	return scratch, batches
}

// presentSystem closes the render pass opened by renderSystem, submits, presents,
// and releases per-frame resources.
func presentSystem(c *app.Ctx) {
	g := app.GetResource[GPU](c)
	if g == nil || !g.frameActive {
		return
	}
	defer func() {
		// Release the view and encoder we created, but NOT the surface texture:
		// wgpu owns surface textures and Present returns the drawable.
		g.curPass.Release()
		g.curEncoder.Release()
		g.curView.Release()
		g.curPass, g.curEncoder, g.curView, g.curTex = nil, nil, nil, nil
		g.frameActive = false
	}()

	_ = g.curPass.End()

	// Frame read-back: record a copy of the surface texture (still valid
	// pre-Present) into this frame's encoder, then deliver it after submit.
	var cap *captureBuffer
	if g.captureSink != nil {
		cap = recordCapture(g, g.curEncoder)
	}

	cmd, err := g.curEncoder.Finish(nil)
	if err != nil {
		return
	}
	defer cmd.Release()
	g.queue.Submit(cmd)

	if cap != nil {
		cap.deliver(g, g.captureSink)
		g.captureSink = nil
	}

	g.surface.Present()
}

// AddOverlay registers a system that draws on top of the mesh render, after the
// entity pass and before present. Overlay systems record draws into the open
// render pass returned by Pass(c); they must not present.
func AddOverlay(a *app.App, sys app.SystemFunc) {
	a.AddSystems(schedule.PostUpdate,
		app.System(sys).After(renderLabel).Before(presentLabel))
}

// AddPreRender registers a system that runs before the mesh render in PostUpdate
// — useful for camera updates that should be visible this frame.
func AddPreRender(a *app.App, sys app.SystemFunc) {
	a.AddSystems(schedule.PostUpdate, app.System(sys).Before(transformLabel))
}

// Pass returns the render pass open for the current frame, or nil when no frame
// is active. Use it from an AddOverlay system to record extra draws.
func Pass(c *app.Ctx) *wgpu.RenderPassEncoder {
	g := app.GetResource[GPU](c)
	if g == nil || !g.frameActive {
		return nil
	}
	return g.curPass
}
