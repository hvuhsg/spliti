//go:build !js

package ui

import (
	_ "embed"
	"unsafe"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
)

//go:embed imgui.wgsl
var imguiShaderCode string

const (
	// imDrawVertStride is sizeof(ImDrawVert): pos vec2 (8) + uv vec2 (8) + col u32 (4).
	imDrawVertStride = 20
	// imDrawIdxSize is sizeof(ImDrawIdx); cimgui-go builds with 16-bit indices.
	imDrawIdxSize = 2
)

// texEntry is one GPU texture managed on ImGui's behalf (the font atlas, or a
// user texture registered via RegisterTexture) plus its single-binding group.
type texEntry struct {
	tex  *wgpu.Texture
	view *wgpu.TextureView
	bg   *wgpu.BindGroup
	w, h int32
}

func (t *texEntry) release() {
	if t.bg != nil {
		t.bg.Release()
	}
	if t.view != nil {
		t.view.Release()
	}
	if t.tex != nil {
		t.tex.Release()
	}
}

// Backend is the ImGui wgpu renderer resource. It lazily builds its pipeline and
// buffers on the first frame (the GPU device is only reachable from a system
// context, not from Plugin.Build), then each frame processes ImGui's texture
// updates and records its draw lists into render3d's open pass. Retrieve it with
// app.GetResource[Backend]; treat fields as opaque.
type Backend struct {
	ready        bool
	builtSamples uint32

	pipeline   *wgpu.RenderPipeline
	bgl0, bgl1 *wgpu.BindGroupLayout
	sampler    *wgpu.Sampler
	uniformBuf *wgpu.Buffer
	group0     *wgpu.BindGroup

	vbuf, ibuf *wgpu.Buffer
	vcap, icap int // capacity in bytes

	textures  map[imgui.TextureID]*texEntry
	nextTexID uint64

	// UI scale (font + style) applied once on the first frame. wantScale is the
	// caller's override (0 = auto from the window content scale); uiScale is the
	// value actually applied, exposed via Scale.
	scaled    bool
	wantScale float32
	uiScale   float32

	// Reusable per-frame scratch concatenating every command list's geometry.
	vtxData, idxData []byte
}

func newBackend() *Backend {
	return &Backend{textures: make(map[imgui.TextureID]*texEntry), nextTexID: 1, uiScale: 1}
}

func (b *Backend) release() {
	for _, t := range b.textures {
		t.release()
	}
	b.textures = nil
	for _, r := range []interface{ Release() }{
		b.vbuf, b.ibuf, b.uniformBuf, b.group0, b.sampler, b.bgl0, b.bgl1, b.pipeline,
	} {
		// Typed-nil guard: a nil *wgpu.X boxed in the interface is non-nil here, so
		// only release the ones that were actually built.
		switch v := r.(type) {
		case *wgpu.Buffer:
			if v != nil {
				v.Release()
			}
		case *wgpu.BindGroup:
			if v != nil {
				v.Release()
			}
		case *wgpu.Sampler:
			if v != nil {
				v.Release()
			}
		case *wgpu.BindGroupLayout:
			if v != nil {
				v.Release()
			}
		case *wgpu.RenderPipeline:
			if v != nil {
				v.Release()
			}
		}
	}
}

// ensureGPU builds the sample-count-independent GPU objects once, and (re)builds
// the pipeline whenever the render pass's MSAA sample count changes.
func (b *Backend) ensureGPU(c *app.Ctx) bool {
	dev := render3d.Device(c)
	if dev == nil {
		return false
	}
	if !b.ready {
		b.buildStatic(c, dev)
		b.ready = true
	}
	samples := render3d.Samples(c)
	if b.pipeline == nil || b.builtSamples != samples {
		if b.pipeline != nil {
			b.pipeline.Release()
		}
		b.pipeline = b.buildPipeline(c, dev, samples)
		b.builtSamples = samples
	}
	return true
}

func (b *Backend) buildStatic(c *app.Ctx, dev *wgpu.Device) {
	sampler, err := dev.CreateSampler(&wgpu.SamplerDescriptor{
		Label:         "spliti.ui.sampler",
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
		panic("ui: sampler: " + err.Error())
	}
	b.sampler = sampler

	b.bgl0, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "spliti.ui.bgl0",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageVertex,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform, MinBindingSize: 16},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageFragment,
				Sampler:    wgpu.SamplerBindingLayout{Type: wgpu.SamplerBindingTypeFiltering},
			},
		},
	})
	if err != nil {
		panic("ui: bgl0: " + err.Error())
	}
	b.bgl1, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "spliti.ui.bgl1",
		Entries: []wgpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: wgpu.ShaderStageFragment,
			Texture: wgpu.TextureBindingLayout{
				SampleType:    wgpu.TextureSampleTypeFloat,
				ViewDimension: wgpu.TextureViewDimension2D,
			},
		}},
	})
	if err != nil {
		panic("ui: bgl1: " + err.Error())
	}

	b.uniformBuf, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "spliti.ui.uniform",
		Size:  16, // vec4<f32>
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		panic("ui: uniform buffer: " + err.Error())
	}
	b.group0, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "spliti.ui.group0",
		Layout: b.bgl0,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: b.uniformBuf, Size: 16},
			{Binding: 1, Sampler: b.sampler},
		},
	})
	if err != nil {
		panic("ui: group0: " + err.Error())
	}
}

func (b *Backend) buildPipeline(c *app.Ctx, dev *wgpu.Device, samples uint32) *wgpu.RenderPipeline {
	shader, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "spliti.ui.shader",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: imguiShaderCode},
	})
	if err != nil {
		panic("ui: shader: " + err.Error())
	}
	defer shader.Release()

	layout, err := dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "spliti.ui.layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{b.bgl0, b.bgl1},
	})
	if err != nil {
		panic("ui: pipeline layout: " + err.Error())
	}
	defer layout.Release()

	pipe, err := dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "spliti.ui.pipeline",
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []wgpu.VertexBufferLayout{{
				ArrayStride: imDrawVertStride,
				StepMode:    wgpu.VertexStepModeVertex,
				Attributes: []wgpu.VertexAttribute{
					{Format: wgpu.VertexFormatFloat32x2, Offset: 0, ShaderLocation: 0},
					{Format: wgpu.VertexFormatFloat32x2, Offset: 8, ShaderLocation: 1},
					{Format: wgpu.VertexFormatUnorm8x4, Offset: 16, ShaderLocation: 2},
				},
			}},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopologyTriangleList,
			FrontFace: wgpu.FrontFaceCCW,
			CullMode:  wgpu.CullModeNone,
		},
		// Match the open pass so the pipeline is compatible: same depth format, but
		// always-pass with no writes (UI draws on top of the 3D frame).
		DepthStencil: &wgpu.DepthStencilState{
			Format:            render3d.DepthFormat(c),
			DepthWriteEnabled: false,
			DepthCompare:      wgpu.CompareFunctionAlways,
			StencilFront:      wgpu.StencilFaceState{Compare: wgpu.CompareFunctionAlways},
			StencilBack:       wgpu.StencilFaceState{Compare: wgpu.CompareFunctionAlways},
		},
		Multisample: wgpu.MultisampleState{Count: samples, Mask: 0xFFFFFFFF},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{{
				Format: render3d.SurfaceFormat(c),
				// Straight (non-premultiplied) alpha — ImGui's vertex colors are not
				// premultiplied. (overlay2d uses premultiplied; this pipeline differs.)
				Blend: &wgpu.BlendState{
					Color: wgpu.BlendComponent{
						SrcFactor: wgpu.BlendFactorSrcAlpha,
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
		panic("ui: pipeline: " + err.Error())
	}
	return pipe
}

// render processes ImGui's pending texture updates, uploads this frame's geometry,
// and records the draw lists into the open render pass. Called from the overlay
// system after imgui.Render().
func (b *Backend) render(c *app.Ctx, dd *imgui.DrawData) {
	pass := render3d.Pass(c)
	if pass == nil || !b.ensureGPU(c) {
		return
	}
	queue := render3d.Queue(c)

	// 1) Satisfy ImGui's texture lifecycle (font atlas + any user textures).
	b.updateTextures(c, dd)

	// 2) Concatenate every command list's vertices/indices into one upload. With
	// RendererHasVtxOffset each draw uses a base-vertex, so list-local 16-bit
	// indices stay valid inside the shared buffers.
	b.vtxData = b.vtxData[:0]
	b.idxData = b.idxData[:0]
	lists := dd.CommandLists()
	type listSpan struct{ vtxBase, idxBase uint32 }
	spans := make([]listSpan, len(lists))
	for i, dl := range lists {
		spans[i] = listSpan{
			vtxBase: uint32(len(b.vtxData) / imDrawVertStride),
			idxBase: uint32(len(b.idxData) / imDrawIdxSize),
		}
		vptr, vbytes := dl.GetVertexBuffer()
		iptr, ibytes := dl.GetIndexBuffer()
		if vbytes > 0 {
			b.vtxData = append(b.vtxData, unsafe.Slice((*byte)(vptr), vbytes)...)
		}
		if ibytes > 0 {
			b.idxData = append(b.idxData, unsafe.Slice((*byte)(iptr), ibytes)...)
		}
	}
	if len(b.vtxData) == 0 || len(b.idxData) == 0 {
		return
	}
	b.ensureBuffers(c, len(b.vtxData), len(b.idxData))
	_ = queue.WriteBuffer(b.vbuf, 0, b.vtxData)
	_ = queue.WriteBuffer(b.ibuf, 0, b.idxData)

	// 3) Uniform: map framebuffer pixels -> clip space (y down -> y up).
	dispPos := dd.DisplayPos()
	dispSize := dd.DisplaySize()
	if dispSize.X <= 0 || dispSize.Y <= 0 {
		return
	}
	sx := 2.0 / dispSize.X
	sy := -2.0 / dispSize.Y
	u := [4]float32{sx, sy, -1 - dispPos.X*sx, 1 - dispPos.Y*sy}
	_ = queue.WriteBuffer(b.uniformBuf, 0, wgpu.ToBytes(u[:]))

	// 4) Record draws.
	pass.SetPipeline(b.pipeline)
	pass.SetBindGroup(0, b.group0, nil)
	pass.SetVertexBuffer(0, b.vbuf, 0, uint64(len(b.vtxData)))
	pass.SetIndexBuffer(b.ibuf, wgpu.IndexFormatUint16, 0, uint64(len(b.idxData)))

	fbW, fbH := dispSize.X, dispSize.Y
	for i, dl := range lists {
		for _, cmd := range dl.Commands() {
			if cmd.HasUserCallback() {
				continue // user callbacks aren't supported in this backend
			}
			clip := cmd.ClipRect() // (minX, minY, maxX, maxY) in ImGui space
			x0 := clamp(clip.X-dispPos.X, 0, fbW)
			y0 := clamp(clip.Y-dispPos.Y, 0, fbH)
			x1 := clamp(clip.Z-dispPos.X, 0, fbW)
			y1 := clamp(clip.W-dispPos.Y, 0, fbH)
			if x1 <= x0 || y1 <= y0 {
				continue
			}
			te := b.textures[cmd.TexID()]
			if te == nil {
				continue
			}
			pass.SetScissorRect(uint32(x0), uint32(y0), uint32(x1-x0), uint32(y1-y0))
			pass.SetBindGroup(1, te.bg, nil)
			pass.DrawIndexed(
				cmd.ElemCount(), 1,
				spans[i].idxBase+cmd.IdxOffset(),
				int32(spans[i].vtxBase+cmd.VtxOffset()),
				0,
			)
		}
	}
	// Restore a full-framebuffer scissor so later overlay systems aren't clipped.
	pass.SetScissorRect(0, 0, uint32(fbW), uint32(fbH))
}

// updateTextures walks DrawData.Textures and creates/updates/destroys GPU
// textures to match ImGui's requests (the 1.92+ dynamic texture protocol).
func (b *Backend) updateTextures(c *app.Ctx, dd *imgui.DrawData) {
	for _, td := range dd.Textures().Slice() {
		switch td.Status() {
		case imgui.TextureStatusWantCreate:
			b.createTexture(c, &td)
		case imgui.TextureStatusWantUpdates:
			b.uploadPixels(c, b.textures[td.TexID()], &td)
			td.SetStatus(imgui.TextureStatusOK)
		case imgui.TextureStatusWantDestroy:
			id := td.TexID()
			if te := b.textures[id]; te != nil {
				te.release()
				delete(b.textures, id)
			}
			td.SetTexID(0)
			td.SetStatus(imgui.TextureStatusDestroyed)
		}
	}
}

func (b *Backend) createTexture(c *app.Ctx, td *imgui.TextureData) {
	dev := render3d.Device(c)
	w, h := td.Width(), td.Height()
	tex, err := dev.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "spliti.ui.imgui.tex",
		Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
		Dimension:     wgpu.TextureDimension2D,
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		Format:        wgpu.TextureFormatRGBA8UnormSrgb,
		MipLevelCount: 1,
		SampleCount:   1,
	})
	if err != nil {
		panic("ui: imgui texture: " + err.Error())
	}
	view, err := tex.CreateView(nil)
	if err != nil {
		panic("ui: imgui texture view: " + err.Error())
	}
	bg, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:   "spliti.ui.imgui.tex.bg",
		Layout:  b.bgl1,
		Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: view}},
	})
	if err != nil {
		panic("ui: imgui texture bind group: " + err.Error())
	}
	te := &texEntry{tex: tex, view: view, bg: bg, w: w, h: h}
	id := imgui.TextureID(b.nextTexID)
	b.nextTexID++
	b.textures[id] = te

	b.uploadPixels(c, te, td)
	td.SetTexID(id)
	td.SetStatus(imgui.TextureStatusOK)
}

// bgl1Group creates a texture bind group (group 1 layout) for an externally
// owned view, used by RegisterTexture.
func (b *Backend) bgl1Group(c *app.Ctx, view *wgpu.TextureView) (*wgpu.BindGroup, error) {
	return render3d.Device(c).CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:   "spliti.ui.user.tex.bg",
		Layout:  b.bgl1,
		Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: view}},
	})
}

func (b *Backend) uploadPixels(c *app.Ctx, te *texEntry, td *imgui.TextureData) {
	if te == nil {
		return
	}
	queue := render3d.Queue(c)
	w, h := td.Width(), td.Height()
	pitch := uint32(td.Pitch())
	// td.Pixels() is a uintptr to ImGui's C-side atlas buffer (not Go-managed
	// memory, so it never moves); read it as bytes for the upload. go vet flags the
	// uintptr->Pointer conversion generically — it is safe for this C allocation.
	data := unsafe.Slice((*byte)(unsafe.Pointer(td.Pixels())), int(td.SizeInBytes()))
	_ = queue.WriteTexture(
		te.tex.AsImageCopy(),
		data,
		&wgpu.TextureDataLayout{Offset: 0, BytesPerRow: pitch, RowsPerImage: uint32(h)},
		&wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
	)
}

func (b *Backend) ensureBuffers(c *app.Ctx, vbytes, ibytes int) {
	dev := render3d.Device(c)
	if b.vbuf == nil || vbytes > b.vcap {
		if b.vbuf != nil {
			b.vbuf.Release()
		}
		cap := grow(b.vcap, vbytes)
		buf, err := dev.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "spliti.ui.vbuf",
			Size:  uint64(cap),
			Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			panic("ui: vbuf: " + err.Error())
		}
		b.vbuf, b.vcap = buf, cap
	}
	if b.ibuf == nil || ibytes > b.icap {
		if b.ibuf != nil {
			b.ibuf.Release()
		}
		cap := grow(b.icap, ibytes)
		buf, err := dev.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "spliti.ui.ibuf",
			Size:  uint64(cap),
			Usage: wgpu.BufferUsageIndex | wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			panic("ui: ibuf: " + err.Error())
		}
		b.ibuf, b.icap = buf, cap
	}
}

// grow returns a new capacity (doubling) that holds at least need bytes.
func grow(cap, need int) int {
	if cap < 4096 {
		cap = 4096
	}
	for cap < need {
		cap *= 2
	}
	return cap
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
