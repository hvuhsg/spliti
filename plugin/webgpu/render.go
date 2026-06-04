package webgpu

import (
	"sort"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// System labels, mirroring tui's private label convention so users extend via
// AddOverlay/AddPreRender rather than raw ordering strings.
const (
	inputLabel   = "__webgpu_input"
	renderLabel  = "__webgpu_render"
	presentLabel = "__webgpu_present"
)

// renderItem is one collected renderable before z-sorting.
type renderItem struct {
	ref  string
	z    int
	inst instanceData
}

// spriteBatch is a contiguous run of same-texture instances in the packed
// instance buffer, drawn with one instanced Draw call.
type spriteBatch struct {
	ref   string
	start int // first instance index into the packed buffer
	count int
}

// installSystems wires the input poll (First) and the render/present split
// (PostUpdate). Called once from Build.
func installSystems(a *app.App) {
	a.AddSystems(schedule.First, app.System(pollInput).Label(inputLabel))
	a.AddSystems(schedule.PostUpdate, app.System(renderSystem).Label(renderLabel))
	a.AddSystems(schedule.PostUpdate,
		app.System(presentSystem).Label(presentLabel).After(renderLabel))
}

// renderSystem acquires the surface texture, opens a render pass, and records
// all sprite draws. It deliberately leaves the pass open so overlay systems
// (After render, Before present) can append draws; presentSystem closes it.
func renderSystem(c *app.Ctx) {
	g := app.GetResource[GPU](c)
	if g == nil {
		return
	}
	g.frameActive = false

	tex, err := g.surface.GetCurrentTexture()
	if err != nil {
		// Surface lost/outdated (often a resize mid-flight). Reconfigure and
		// skip this frame; the next one recovers.
		g.surface.Configure(g.adapter, g.device, g.config)
		return
	}
	view, err := tex.CreateView(nil)
	if err != nil {
		tex.Release()
		return
	}
	encoder, err := g.device.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{
		Label: "spliti.webgpu.encoder",
	})
	if err != nil {
		view.Release()
		tex.Release()
		return
	}
	// With MSAA on, draw into the multisampled texture and resolve into the
	// swapchain view; the multisampled samples themselves need not be stored.
	// With MSAA off, draw straight into the swapchain view.
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
	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{color},
	})

	g.curTex, g.curView, g.curEncoder, g.curPass = tex, view, encoder, pass
	g.frameActive = true

	pass.SetPipeline(g.pipeline)
	pass.SetVertexBuffer(0, g.quadBuf, 0, g.quadBuf.GetSize())
	pass.SetBindGroup(0, g.cameraBindGroup, nil)

	drawSprites(c, g, pass)
}

// drawSprites collects Transform+Sprite entities (with optional Color/Layer),
// z-sorts them, packs all instances into one buffer, and issues one instanced
// Draw per contiguous same-texture run.
func drawSprites(c *app.Ctx, g *GPU, pass *wgpu.RenderPassEncoder) {
	reg := app.GetResource[TextureRegistry](c)
	if reg == nil {
		return
	}
	world := c.World()
	colorMap := generic.NewMap[Color](world)
	layerMap := generic.NewMap[Layer](world)

	g.items = g.items[:0]
	app.Query2[Transform, Sprite](c, func(e ecs.Entity, t *Transform, s *Sprite) {
		gt := reg.get(s.Ref)
		if gt == nil {
			return // unknown texture: skip, same tolerance as tui
		}
		w, h := t.W, t.H
		if w == 0 {
			w = float32(gt.w)
		}
		if h == 0 {
			h = float32(gt.h)
		}
		col := opaqueWhite
		if colorMap.Has(e) {
			col = colorMap.Get(e).orDefault()
		}
		z := 0
		if layerMap.Has(e) {
			z = layerMap.Get(e).Z
		}
		g.items = append(g.items, renderItem{
			ref: s.Ref,
			z:   z,
			inst: instanceData{
				OffX: t.X, OffY: t.Y, SizeX: w, SizeY: h,
				R: col.R, G: col.G, B: col.B, A: col.A,
			},
		})
	})
	if len(g.items) == 0 {
		return
	}

	g.scratch, g.batches = packInstances(g.items, g.scratch[:0], g.batches[:0])

	// One upload for the whole frame, then per-batch draws at distinct offsets —
	// reusing a single region across draws would make every draw read the last
	// write at submit time.
	ensureInstanceCap(g, len(g.scratch))
	if err := g.queue.WriteBuffer(g.instanceBuf, 0, wgpu.ToBytes(g.scratch)); err != nil {
		return
	}
	for _, b := range g.batches {
		gt := reg.get(b.ref)
		if gt == nil {
			continue
		}
		off := uint64(b.start * instanceStride)
		size := uint64(b.count * instanceStride)
		pass.SetBindGroup(1, gt.bindGroup, nil)
		pass.SetVertexBuffer(1, g.instanceBuf, off, size)
		pass.Draw(6, uint32(b.count), 0, 0)
	}
}

// packInstances z-sorts items (stable, ascending) and packs them into draw
// order, returning the instance data and the contiguous same-texture batches.
// Pure: the destination slices are reused (pass them pre-truncated) so the
// renderer avoids per-frame allocation. Same-ref runs are batched only where
// adjacent after sorting, which preserves z order across batches.
func packInstances(items []renderItem, scratch []instanceData, batches []spriteBatch) ([]instanceData, []spriteBatch) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].z < items[j].z })
	for i, it := range items {
		scratch = append(scratch, it.inst)
		if i > 0 && it.ref == items[i-1].ref {
			batches[len(batches)-1].count++
		} else {
			batches = append(batches, spriteBatch{ref: it.ref, start: i, count: 1})
		}
	}
	return scratch, batches
}

// presentSystem closes the render pass opened by renderSystem, submits the
// command buffer, presents the frame, and releases per-frame resources.
func presentSystem(c *app.Ctx) {
	g := app.GetResource[GPU](c)
	if g == nil || !g.frameActive {
		return
	}
	defer func() {
		// Release the view and encoder we created, but NOT the surface texture
		// itself: wgpu owns surface textures and releasing them corrupts the
		// swapchain's drawable pool (it faults after a few frames). Present
		// returns the drawable.
		g.curPass.Release()
		g.curEncoder.Release()
		g.curView.Release()
		g.curPass, g.curEncoder, g.curView, g.curTex = nil, nil, nil, nil
		g.frameActive = false
	}()

	_ = g.curPass.End()
	cmd, err := g.curEncoder.Finish(nil)
	if err != nil {
		return
	}
	defer cmd.Release()
	g.queue.Submit(cmd)
	g.surface.Present()
}

// AddOverlay registers a system that draws on top of the sprite render, after
// the entity pass and before present. Overlay systems should record draws into
// the open render pass returned by Pass(c); they must not present.
func AddOverlay(a *app.App, sys app.SystemFunc) {
	a.AddSystems(schedule.PostUpdate,
		app.System(sys).After(renderLabel).Before(presentLabel))
}

// AddPreRender registers a system that runs before the sprite render in
// PostUpdate — useful for camera updates that should be visible this frame.
func AddPreRender(a *app.App, sys app.SystemFunc) {
	a.AddSystems(schedule.PostUpdate, app.System(sys).Before(renderLabel))
}

// Pass returns the render pass open for the current frame, or nil when no frame
// is active (e.g. the surface texture couldn't be acquired). Use it from an
// AddOverlay system to record extra draws.
func Pass(c *app.Ctx) *wgpu.RenderPassEncoder {
	g := app.GetResource[GPU](c)
	if g == nil || !g.frameActive {
		return nil
	}
	return g.curPass
}
