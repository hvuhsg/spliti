package webgpu

import (
	_ "embed"

	"github.com/cogentcore/webgpu/wgpu"
)

//go:embed shader.wgsl
var shaderCode string

// instanceData is one sprite's per-instance vertex input. Field order and size
// must match the @location bindings in shader.wgsl: offset(8) + size(8) +
// tint(16) = 32 bytes.
type instanceData struct {
	OffX, OffY   float32
	SizeX, SizeY float32
	R, G, B, A   float32
}

const instanceStride = 32 // bytes; sizeof(instanceData)

// unitQuad is two triangles covering [0,1]x[0,1] as vec2 corners. CullMode is
// None so winding order is irrelevant.
var unitQuad = []float32{
	0, 0, 1, 0, 1, 1,
	0, 0, 1, 1, 0, 1,
}

const initialInstanceCap = 256

// maxLOD caps the sampler's mip selection. 32 is far past any real texture
// (level 32 is a 4-billion-pixel base), so it just means "use the whole chain".
const maxLOD = 32

// buildPipeline creates the shader module, bind group layouts, render pipeline,
// sampler, the static quad buffer, the (growable) instance buffer, and the
// camera uniform buffer + bind group. Populates the corresponding GPU fields.
func buildPipeline(g *GPU, format wgpu.TextureFormat) {
	var err error

	// Bind group 0: camera uniform.
	g.camBGL, err = g.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "spliti.webgpu.camera.bgl",
		Entries: []wgpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: wgpu.ShaderStageVertex,
			Buffer: wgpu.BufferBindingLayout{
				Type:           wgpu.BufferBindingTypeUniform,
				MinBindingSize: 64,
			},
		}},
	})
	if err != nil {
		panic("webgpu: camera bgl: " + err.Error())
	}

	// Bind group 1: texture + sampler (one per registered texture).
	g.texBGL, err = g.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "spliti.webgpu.texture.bgl",
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
		panic("webgpu: texture bgl: " + err.Error())
	}

	g.format = format
	g.pipeline, err = buildRenderPipeline(g)
	if err != nil {
		panic("webgpu: render pipeline: " + err.Error())
	}

	// Default to Nearest for crisp pixel art. When Smooth is set, switch to
	// Linear so textured-sprite edges — whose shape lives in the texture's alpha
	// — are smoothed instead of stair-stepped. Independent of Samples (MSAA): the
	// two address different edges (texture alpha vs. quad geometry).
	//
	// Either way the sampler ranges over the full mip chain (LodMaxClamp is the
	// max level, not 1), so minified sprites sample a downscaled level instead of
	// undersampling the base texture and shimmering. Smooth additionally enables
	// trilinear (MipmapFilterLinear) and anisotropic filtering; anisotropy is only
	// legal when all of min/mag/mip are Linear, so it stays 1 in Nearest mode.
	magFilter, minFilter := wgpu.FilterModeNearest, wgpu.FilterModeNearest
	mipFilter := wgpu.MipmapFilterModeNearest
	var maxAniso uint16 = 1
	if g.smooth {
		magFilter, minFilter = wgpu.FilterModeLinear, wgpu.FilterModeLinear
		mipFilter = wgpu.MipmapFilterModeLinear
		maxAniso = 16
	}
	g.sampler, err = g.device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:         "spliti.webgpu.sampler",
		AddressModeU:  wgpu.AddressModeClampToEdge,
		AddressModeV:  wgpu.AddressModeClampToEdge,
		AddressModeW:  wgpu.AddressModeClampToEdge,
		MagFilter:     magFilter,
		MinFilter:     minFilter,
		MipmapFilter:  mipFilter,
		LodMinClamp:   0,
		LodMaxClamp:   maxLOD,
		MaxAnisotropy: maxAniso,
	})
	if err != nil {
		panic("webgpu: sampler: " + err.Error())
	}

	g.quadBuf, err = g.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "spliti.webgpu.quad",
		Contents: wgpu.ToBytes(unitQuad),
		Usage:    wgpu.BufferUsageVertex,
	})
	if err != nil {
		panic("webgpu: quad buffer: " + err.Error())
	}

	g.instanceCap = initialInstanceCap
	g.instanceBuf = newInstanceBuffer(g, g.instanceCap)

	g.cameraBuf, err = g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "spliti.webgpu.camera",
		Size:  64,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		panic("webgpu: camera buffer: " + err.Error())
	}
	g.cameraBindGroup, err = g.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "spliti.webgpu.camera.bg",
		Layout: g.camBGL,
		Entries: []wgpu.BindGroupEntry{{
			Binding: 0,
			Buffer:  g.cameraBuf,
			Offset:  0,
			Size:    64,
		}},
	})
	if err != nil {
		panic("webgpu: camera bind group: " + err.Error())
	}
}

// buildRenderPipeline creates the shader module, pipeline layout, and render
// pipeline using the current g.samples and g.format. It's split out of
// buildPipeline so the MSAA fallback in ensureMSAATarget can rebuild the
// pipeline at a different sample count (the pipeline's MultisampleState.Count
// must match the attachment's). The shader and layout are transient — only the
// returned pipeline is kept. Requires g.camBGL and g.texBGL to already exist.
func buildRenderPipeline(g *GPU) (*wgpu.RenderPipeline, error) {
	shader, err := g.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "spliti.webgpu.shader",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaderCode},
	})
	if err != nil {
		return nil, err
	}
	defer shader.Release()

	layout, err := g.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "spliti.webgpu.layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{g.camBGL, g.texBGL},
	})
	if err != nil {
		return nil, err
	}
	defer layout.Release()

	return g.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "spliti.webgpu.pipeline",
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []wgpu.VertexBufferLayout{
				{ // buffer 0: unit quad corner
					ArrayStride: 8,
					StepMode:    wgpu.VertexStepModeVertex,
					Attributes: []wgpu.VertexAttribute{
						{Format: wgpu.VertexFormatFloat32x2, Offset: 0, ShaderLocation: 0},
					},
				},
				{ // buffer 1: per-instance sprite data
					ArrayStride: instanceStride,
					StepMode:    wgpu.VertexStepModeInstance,
					Attributes: []wgpu.VertexAttribute{
						{Format: wgpu.VertexFormatFloat32x2, Offset: 0, ShaderLocation: 1},  // offset
						{Format: wgpu.VertexFormatFloat32x2, Offset: 8, ShaderLocation: 2},  // size
						{Format: wgpu.VertexFormatFloat32x4, Offset: 16, ShaderLocation: 3}, // tint
					},
				},
			},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopologyTriangleList,
			FrontFace: wgpu.FrontFaceCCW,
			CullMode:  wgpu.CullModeNone,
		},
		// MSAA anti-aliases sprite-quad geometry edges. Sprite shapes defined by
		// texture alpha (the cutout case) are instead smoothed by the linear
		// sampler below plus anti-aliased source textures — not by alpha-to-
		// coverage, which conflicts with the alpha blending in Fragment.
		Multisample: wgpu.MultisampleState{Count: g.samples, Mask: 0xFFFFFFFF},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{{
				Format: g.format,
				// Premultiplied-alpha blend: the fragment already outputs rgb scaled
				// by alpha (see fs_main and the premultiply on texture upload), so
				// the source factor is One, not SrcAlpha. This keeps linear filtering
				// across alpha edges correct — straight alpha would bleed transparent
				// texels' rgb inward and fringe colored sprites.
				Blend: &wgpu.BlendState{
					Color: wgpu.BlendComponent{
						Operation: wgpu.BlendOperationAdd,
						SrcFactor: wgpu.BlendFactorOne,
						DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
					},
					Alpha: wgpu.BlendComponent{
						Operation: wgpu.BlendOperationAdd,
						SrcFactor: wgpu.BlendFactorOne,
						DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
					},
				},
				WriteMask: wgpu.ColorWriteMaskAll,
			}},
		},
	})
}

// newInstanceBuffer allocates an instance vertex buffer with room for capacity
// instances.
func newInstanceBuffer(g *GPU, capacity int) *wgpu.Buffer {
	buf, err := g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "spliti.webgpu.instances",
		Size:  uint64(capacity * instanceStride),
		Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		panic("webgpu: instance buffer: " + err.Error())
	}
	return buf
}

// normalizeSamples clamps a requested sample count to a portable MSAA value.
// WebGPU only guarantees support for 1 and 4 samples, so anything above 1 is
// mapped to 4 and anything else to 1 (MSAA off).
func normalizeSamples(n int) uint32 {
	if n <= 1 {
		return 1
	}
	return 4
}

// ensureMSAATarget (re)creates the multisampled color texture the render pass
// renders into before resolving to the swapchain. It releases any previous one,
// sizing the new texture to the current surface config. No-op when MSAA is off
// (g.samples <= 1), leaving msaaView nil so the render pass draws straight to
// the swapchain.
func ensureMSAATarget(g *GPU) {
	if g.msaaView != nil {
		g.msaaView.Release()
		g.msaaView = nil
	}
	if g.msaaTex != nil {
		g.msaaTex.Release()
		g.msaaTex = nil
	}
	if g.samples <= 1 {
		return
	}
	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "spliti.webgpu.msaa",
		Usage:         wgpu.TextureUsageRenderAttachment,
		Dimension:     wgpu.TextureDimension2D,
		Size:          wgpu.Extent3D{Width: g.config.Width, Height: g.config.Height, DepthOrArrayLayers: 1},
		Format:        g.config.Format,
		MipLevelCount: 1,
		SampleCount:   g.samples,
	})
	if err != nil {
		// Don't take the process down because a multisample target couldn't be
		// allocated (e.g. out of VRAM on a large resize). Degrade to no MSAA.
		disableMSAA(g)
		return
	}
	view, err := tex.CreateView(nil)
	if err != nil {
		tex.Release()
		disableMSAA(g)
		return
	}
	g.msaaTex, g.msaaView = tex, view
}

// disableMSAA drops the renderer to no multisampling after a failed MSAA-target
// allocation. msaaView stays nil so the render pass draws straight to the
// swapchain, and the pipeline is rebuilt at 1 sample so its MultisampleState
// matches that 1-sample attachment. If the rebuild itself fails the old pipeline
// is kept — the next frame's GetCurrentTexture/Configure path recovers.
func disableMSAA(g *GPU) {
	if g.samples == 1 {
		return
	}
	g.samples = 1
	p, err := buildRenderPipeline(g)
	if err != nil {
		return
	}
	g.pipeline.Release()
	g.pipeline = p
}

// ensureInstanceCap grows the instance buffer to hold at least n instances,
// reallocating (release + recreate) when needed. No-op when already big enough.
func ensureInstanceCap(g *GPU, n int) {
	if n <= g.instanceCap {
		return
	}
	newCap := g.instanceCap * 2
	for newCap < n {
		newCap *= 2
	}
	g.instanceBuf.Release()
	g.instanceBuf = newInstanceBuffer(g, newCap)
	g.instanceCap = newCap
}
