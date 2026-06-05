package render3d

import (
	_ "embed"
	"fmt"
	"image"
	"image/draw"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/app"
)

//go:embed quad2d.wgsl
var quad2dShaderCode string

// overlayVertex is one corner of a screen-space quad: an NDC position and a UV.
type overlayVertex struct {
	X, Y float32
	U, V float32
}

const overlayVertexStride = 16 // bytes; sizeof(overlayVertex)

// panelGPU is one uploaded HUD panel texture and its bind group.
type panelGPU struct {
	tex       *wgpu.Texture
	view      *wgpu.TextureView
	bindGroup *wgpu.BindGroup
	w, h      int
}

// panelDraw is one queued blit: a registered panel drawn at a pixel rect (origin
// top-left of the window).
type panelDraw struct {
	ref        string
	x, y, w, h int
}

// Overlay2D is the 2D HUD host: it owns a screen-space textured-quad pipeline and
// a registry of CPU-rasterized panel textures. Apps LoadPanel(ref, img) when a
// panel image changes, then DrawPanel(ref, x, y, w, h) each frame from an overlay
// system; a single system (drawOverlay2D) blits all queued panels on top of the
// 3D frame. Retrieve it with app.GetResource[Overlay2D].
type Overlay2D struct {
	gpu      *GPU
	pipeline *wgpu.RenderPipeline
	builtFor uint32
	bgl      *wgpu.BindGroupLayout
	sampler  *wgpu.Sampler
	vbuf     *wgpu.Buffer
	vcap     int // capacity in vertices

	panels map[string]*panelGPU
	queue  []panelDraw
	verts  []overlayVertex
}

func newOverlay2D(g *GPU) *Overlay2D {
	return &Overlay2D{gpu: g, panels: make(map[string]*panelGPU)}
}

func (o *Overlay2D) release() {
	if o == nil {
		return
	}
	for _, p := range o.panels {
		p.release()
	}
	o.panels = nil
	if o.vbuf != nil {
		o.vbuf.Release()
	}
	if o.sampler != nil {
		o.sampler.Release()
	}
	if o.bgl != nil {
		o.bgl.Release()
	}
	if o.pipeline != nil {
		o.pipeline.Release()
	}
}

func (p *panelGPU) release() {
	if p.bindGroup != nil {
		p.bindGroup.Release()
	}
	if p.view != nil {
		p.view.Release()
	}
	if p.tex != nil {
		p.tex.Release()
	}
}

// LoadPanel uploads img as the panel texture under ref, replacing any existing
// one. Rebake and reload only when the panel's contents change (rasterizing every
// frame is wasteful). The image is converted to premultiplied RGBA8.
func LoadPanel(c *app.Ctx, ref string, img image.Image) error {
	o := app.GetResource[Overlay2D](c)
	if o == nil {
		return fmt.Errorf("render3d: overlay2d not initialized")
	}
	return o.load(ref, img)
}

func (o *Overlay2D) load(ref string, img image.Image) error {
	o.ensureSampler()
	o.ensureBGL()
	rgba := overlayToRGBA(img)
	w, h := rgba.Rect.Dx(), rgba.Rect.Dy()
	if w == 0 || h == 0 {
		return fmt.Errorf("render3d: panel %q has zero size", ref)
	}
	pix := make([]byte, len(rgba.Pix))
	copy(pix, rgba.Pix)
	overlayPremultiply(pix)

	g := o.gpu
	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "spliti.render3d.panel." + ref,
		Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
		Dimension:     wgpu.TextureDimension2D,
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		Format:        wgpu.TextureFormatRGBA8UnormSrgb,
		MipLevelCount: 1,
		SampleCount:   1,
	})
	if err != nil {
		return fmt.Errorf("render3d: panel texture %q: %w", ref, err)
	}
	if err := g.queue.WriteTexture(
		tex.AsImageCopy(),
		wgpu.ToBytes(pix),
		&wgpu.TextureDataLayout{Offset: 0, BytesPerRow: uint32(4 * w), RowsPerImage: uint32(h)},
		&wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
	); err != nil {
		tex.Release()
		return fmt.Errorf("render3d: panel write %q: %w", ref, err)
	}
	view, err := tex.CreateView(nil)
	if err != nil {
		tex.Release()
		return fmt.Errorf("render3d: panel view %q: %w", ref, err)
	}
	bg, err := g.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "spliti.render3d.panel.bg." + ref,
		Layout: o.bgl,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: view},
			{Binding: 1, Sampler: o.sampler},
		},
	})
	if err != nil {
		view.Release()
		tex.Release()
		return fmt.Errorf("render3d: panel bind group %q: %w", ref, err)
	}
	if old := o.panels[ref]; old != nil {
		old.release()
	}
	o.panels[ref] = &panelGPU{tex: tex, view: view, bindGroup: bg, w: w, h: h}
	return nil
}

// HasPanel reports whether a panel is registered under ref.
func HasPanel(c *app.Ctx, ref string) bool {
	o := app.GetResource[Overlay2D](c)
	return o != nil && o.panels[ref] != nil
}

// PanelSize returns the pixel dimensions of a registered panel, or 0,0.
func PanelSize(c *app.Ctx, ref string) (int, int) {
	o := app.GetResource[Overlay2D](c)
	if o == nil {
		return 0, 0
	}
	if p := o.panels[ref]; p != nil {
		return p.w, p.h
	}
	return 0, 0
}

// DrawPanel queues a blit of panel ref at the pixel rect (x,y,w,h), origin at the
// window top-left. Call from an AddOverlay system each frame. A zero or negative
// w/h falls back to the panel's native pixel size.
func DrawPanel(c *app.Ctx, ref string, x, y, w, h int) {
	o := app.GetResource[Overlay2D](c)
	if o == nil {
		return
	}
	if w <= 0 || h <= 0 {
		if p := o.panels[ref]; p != nil {
			if w <= 0 {
				w = p.w
			}
			if h <= 0 {
				h = p.h
			}
		}
	}
	o.queue = append(o.queue, panelDraw{ref: ref, x: x, y: y, w: w, h: h})
}

// drawOverlay2D blits all queued panels into the open render pass, then clears
// the queue. Registered as an overlay system after the gizmo pass.
func drawOverlay2D(c *app.Ctx) {
	g := app.GetResource[GPU](c)
	o := app.GetResource[Overlay2D](c)
	if g == nil || o == nil {
		return
	}
	defer func() { o.queue = o.queue[:0] }()
	if !g.frameActive || len(o.queue) == 0 {
		return
	}
	o.ensureSampler()
	o.ensureBGL()
	o.ensurePipeline(g)

	fw := float32(g.config.Width)
	fh := float32(g.config.Height)
	if fw <= 0 || fh <= 0 {
		return
	}

	// Build all quad vertices first (6 per visible panel) so one upload covers the
	// frame; record which buffer slice each draw uses.
	o.verts = o.verts[:0]
	type drawRange struct {
		bg    *wgpu.BindGroup
		start int
	}
	var draws []drawRange
	for _, d := range o.queue {
		p := o.panels[d.ref]
		if p == nil {
			continue
		}
		left := float32(d.x)/fw*2 - 1
		right := float32(d.x+d.w)/fw*2 - 1
		top := 1 - float32(d.y)/fh*2
		bottom := 1 - float32(d.y+d.h)/fh*2
		tl := overlayVertex{left, top, 0, 0}
		tr := overlayVertex{right, top, 1, 0}
		br := overlayVertex{right, bottom, 1, 1}
		bl := overlayVertex{left, bottom, 0, 1}
		draws = append(draws, drawRange{bg: p.bindGroup, start: len(o.verts)})
		o.verts = append(o.verts, tl, bl, br, tl, br, tr)
	}
	if len(o.verts) == 0 {
		return
	}
	o.ensureVbuf(len(o.verts))
	if err := g.queue.WriteBuffer(o.vbuf, 0, wgpu.ToBytes(o.verts)); err != nil {
		return
	}

	pass := g.curPass
	pass.SetPipeline(o.pipeline)
	for _, dr := range draws {
		pass.SetBindGroup(0, dr.bg, nil)
		off := uint64(dr.start * overlayVertexStride)
		pass.SetVertexBuffer(0, o.vbuf, off, uint64(6*overlayVertexStride))
		pass.Draw(6, 1, 0, 0)
	}
}

func (o *Overlay2D) ensureSampler() {
	if o.sampler != nil {
		return
	}
	s, err := o.gpu.device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:         "spliti.render3d.overlay.sampler",
		AddressModeU:  wgpu.AddressModeClampToEdge,
		AddressModeV:  wgpu.AddressModeClampToEdge,
		AddressModeW:  wgpu.AddressModeClampToEdge,
		MagFilter:     wgpu.FilterModeLinear,
		MinFilter:     wgpu.FilterModeLinear,
		MipmapFilter:  wgpu.MipmapFilterModeNearest,
		LodMaxClamp:   1,
		MaxAnisotropy: 1,
	})
	if err != nil {
		panic("render3d: overlay sampler: " + err.Error())
	}
	o.sampler = s
}

func (o *Overlay2D) ensureBGL() {
	if o.bgl != nil {
		return
	}
	bgl, err := o.gpu.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "spliti.render3d.overlay.bgl",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageFragment,
				Sampler:    wgpu.SamplerBindingLayout{Type: wgpu.SamplerBindingTypeFiltering},
			},
		},
	})
	if err != nil {
		panic("render3d: overlay bgl: " + err.Error())
	}
	o.bgl = bgl
}

func (o *Overlay2D) ensurePipeline(g *GPU) {
	if o.pipeline != nil && o.builtFor == g.samples {
		return
	}
	if o.pipeline != nil {
		o.pipeline.Release()
	}
	o.pipeline = buildOverlay2DPipeline(g, o.bgl)
	o.builtFor = g.samples
}

func (o *Overlay2D) ensureVbuf(n int) {
	if o.vbuf != nil && n <= o.vcap {
		return
	}
	newCap := o.vcap
	if newCap < 64 {
		newCap = 64
	}
	for newCap < n {
		newCap *= 2
	}
	if o.vbuf != nil {
		o.vbuf.Release()
	}
	buf, err := o.gpu.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "spliti.render3d.overlay.vbuf",
		Size:  uint64(newCap * overlayVertexStride),
		Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		panic("render3d: overlay vbuf: " + err.Error())
	}
	o.vbuf, o.vcap = buf, newCap
}

// buildOverlay2DPipeline creates the screen-space quad pipeline: alpha-blended
// (premultiplied), depth test always (drawn on top), no depth write.
func buildOverlay2DPipeline(g *GPU, bgl *wgpu.BindGroupLayout) *wgpu.RenderPipeline {
	shader, err := g.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "spliti.render3d.overlay.shader",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: quad2dShaderCode},
	})
	if err != nil {
		panic("render3d: overlay shader: " + err.Error())
	}
	defer shader.Release()

	layout, err := g.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "spliti.render3d.overlay.layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl},
	})
	if err != nil {
		panic("render3d: overlay layout: " + err.Error())
	}
	defer layout.Release()

	pipe, err := g.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "spliti.render3d.overlay.pipeline",
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []wgpu.VertexBufferLayout{{
				ArrayStride: overlayVertexStride,
				StepMode:    wgpu.VertexStepModeVertex,
				Attributes: []wgpu.VertexAttribute{
					{Format: wgpu.VertexFormatFloat32x2, Offset: 0, ShaderLocation: 0},
					{Format: wgpu.VertexFormatFloat32x2, Offset: 8, ShaderLocation: 1},
				},
			}},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopologyTriangleList,
			FrontFace: wgpu.FrontFaceCCW,
			CullMode:  wgpu.CullModeNone,
		},
		DepthStencil: &wgpu.DepthStencilState{
			Format:            g.depthFormat,
			DepthWriteEnabled: false,
			DepthCompare:      wgpu.CompareFunctionAlways,
			StencilFront:      wgpu.StencilFaceState{Compare: wgpu.CompareFunctionAlways},
			StencilBack:       wgpu.StencilFaceState{Compare: wgpu.CompareFunctionAlways},
		},
		Multisample: wgpu.MultisampleState{Count: g.samples, Mask: 0xFFFFFFFF},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{{
				Format: g.format,
				Blend: &wgpu.BlendState{
					Color: wgpu.BlendComponent{
						SrcFactor: wgpu.BlendFactorOne,
						DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
						Operation: wgpu.BlendOperationAdd,
					},
					Alpha: wgpu.BlendComponent{
						SrcFactor: wgpu.BlendFactorOne,
						DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
						Operation: wgpu.BlendOperationAdd,
					},
				},
				WriteMask: wgpu.ColorWriteMaskAll,
			}},
		},
	})
	if err != nil {
		panic("render3d: overlay pipeline: " + err.Error())
	}
	return pipe
}

// overlayToRGBA returns img as a zero-origin *image.RGBA, copying when needed.
func overlayToRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok && rgba.Rect.Min == (image.Point{}) {
		return rgba
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

// overlayPremultiply scales each texel's rgb by its alpha, in place, so the
// premultiplied blend on the pipeline composites correctly.
func overlayPremultiply(pix []byte) {
	for i := 0; i+3 < len(pix); i += 4 {
		a := uint32(pix[i+3])
		pix[i+0] = byte(uint32(pix[i+0]) * a / 255)
		pix[i+1] = byte(uint32(pix[i+1]) * a / 255)
		pix[i+2] = byte(uint32(pix[i+2]) * a / 255)
	}
}
