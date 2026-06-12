package render3d

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/app"
)

// RenderTarget is an offscreen color+depth target the scene can render into
// instead of the window surface (see SetSceneTarget). The resolved color
// texture is sampleable, so it can be shown inside an ImGui panel (via
// plugin/ui RegisterTexture) — the seam an editor viewport builds on.
//
// The target is created with the renderer's surface color format, depth format,
// and MSAA sample count, so every pipeline that draws into the main pass
// (meshes, gizmos) works against it unchanged.
type RenderTarget struct {
	w, h int

	color     *wgpu.Texture
	colorView *wgpu.TextureView
	depth     *wgpu.Texture
	depthView *wgpu.TextureView
	// msaa is the multisampled color attachment that resolves into color; only
	// allocated when the renderer runs with MSAA on.
	msaa     *wgpu.Texture
	msaaView *wgpu.TextureView
}

// NewRenderTarget allocates an offscreen target of w x h pixels. Returns nil if
// the GPU resource is not ready or the size is not positive.
func NewRenderTarget(c *app.Ctx, w, h int) *RenderTarget {
	g := app.GetResource[GPU](c)
	if g == nil || w <= 0 || h <= 0 {
		return nil
	}
	rt := &RenderTarget{}
	if !rt.build(g, w, h) {
		return nil
	}
	return rt
}

// Size returns the target's pixel dimensions.
func (rt *RenderTarget) Size() (int, int) { return rt.w, rt.h }

// View returns the resolved, sampleable color view (valid until the next
// Resize or Release).
func (rt *RenderTarget) View() *wgpu.TextureView { return rt.colorView }

// Resize reallocates the target at a new pixel size. No-op when the size is
// unchanged or not positive. Any view previously returned by View is invalid
// afterwards.
func (rt *RenderTarget) Resize(c *app.Ctx, w, h int) {
	g := app.GetResource[GPU](c)
	if g == nil || w <= 0 || h <= 0 || (w == rt.w && h == rt.h) {
		return
	}
	rt.releaseTextures()
	rt.build(g, w, h)
}

// Release frees the target's GPU resources. If it is the active scene target it
// is detached first.
func (rt *RenderTarget) Release(c *app.Ctx) {
	if g := app.GetResource[GPU](c); g != nil && g.target == rt {
		g.target = nil
	}
	rt.releaseTextures()
}

func (rt *RenderTarget) build(g *GPU, w, h int) bool {
	color, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:     "spliti.render3d.target.color",
		Usage:     wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding,
		Dimension: wgpu.TextureDimension2D,
		Size:      wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		Format:    g.format,
		MipLevelCount: 1,
		SampleCount:   1,
	})
	if err != nil {
		return false
	}
	colorView, err := color.CreateView(nil)
	if err != nil {
		color.Release()
		return false
	}
	depth, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:     "spliti.render3d.target.depth",
		Usage:     wgpu.TextureUsageRenderAttachment,
		Dimension: wgpu.TextureDimension2D,
		Size:      wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		Format:    g.depthFormat,
		MipLevelCount: 1,
		SampleCount:   g.samples,
	})
	if err != nil {
		colorView.Release()
		color.Release()
		return false
	}
	depthView, err := depth.CreateView(nil)
	if err != nil {
		depth.Release()
		colorView.Release()
		color.Release()
		return false
	}
	rt.color, rt.colorView = color, colorView
	rt.depth, rt.depthView = depth, depthView
	rt.w, rt.h = w, h

	if g.samples > 1 {
		msaa, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
			Label:     "spliti.render3d.target.msaa",
			Usage:     wgpu.TextureUsageRenderAttachment,
			Dimension: wgpu.TextureDimension2D,
			Size:      wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
			Format:    g.format,
			MipLevelCount: 1,
			SampleCount:   g.samples,
		})
		if err != nil {
			rt.releaseTextures()
			return false
		}
		msaaView, err := msaa.CreateView(nil)
		if err != nil {
			msaa.Release()
			rt.releaseTextures()
			return false
		}
		rt.msaa, rt.msaaView = msaa, msaaView
	}
	return true
}

func (rt *RenderTarget) releaseTextures() {
	for _, v := range []*wgpu.TextureView{rt.msaaView, rt.depthView, rt.colorView} {
		if v != nil {
			v.Release()
		}
	}
	for _, t := range []*wgpu.Texture{rt.msaa, rt.depth, rt.color} {
		if t != nil {
			t.Release()
		}
	}
	rt.color, rt.colorView = nil, nil
	rt.depth, rt.depthView = nil, nil
	rt.msaa, rt.msaaView = nil, nil
	rt.w, rt.h = 0, 0
}

// SetSceneTarget redirects the scene render pass (meshes + gizmo lines) into rt
// instead of the window surface. Overlay systems (ImGui, overlay2d) keep
// drawing to the surface, in a second pass the renderer opens after the scene
// pass ends. Pass nil to restore direct-to-surface rendering.
//
// While a target is set, the window resize handler stops forcing the camera
// aspect ratio to the window's; the caller owns it (Camera3D.SetAspect).
func SetSceneTarget(c *app.Ctx, rt *RenderTarget) {
	g := app.GetResource[GPU](c)
	if g == nil {
		return
	}
	if rt != nil && rt.colorView == nil {
		rt = nil
	}
	g.target = rt
}
