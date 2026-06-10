package render3d

import (
	_ "embed"

	"github.com/cogentcore/webgpu/wgpu"
)

//go:embed pbr.wgsl
var shaderCode string

// Vertex is one mesh vertex. Field order and size must match the @location
// bindings in pbr.wgsl: position(12) + normal(12) + uv(8) = 32 bytes.
//
// A tangent (vec4) location is intentionally reserved in the shader for future
// normal mapping; adding it here changes the stride and every primitive
// generator, so it is left out of this foundation.
type Vertex struct {
	PX, PY, PZ float32 // position
	NX, NY, NZ float32 // normal
	U, V       float32 // texture coordinate
}

const vertexStride = 32 // bytes; sizeof(Vertex)

// instanceStride is the size of one per-instance record: a model matrix (mat4 =
// 16 floats = 64 B) followed by an RGBA tint (vec4 = 16 B). Must match
// instanceData in render.go and the @location(4..8) instance attributes below.
const instanceStride = 80

const (
	initialInstanceCap = 256
	initialPointCap    = 16
)

// frameUBOFloats is the size, in float32s, of the frame uniform block. Layout
// (each row a vec4-aligned slot; see writeFrameUniforms and pbr.wgsl's Frame):
//
//	[ 0..15] view        mat4
//	[16..31] proj        mat4
//	[32..35] cameraPos   vec4 (xyz, 1)
//	[36..39] ambient     vec4 (rgb, _)
//	[40..43] dirDir      vec4 (xyz direction, w = intensity)
//	[44..47] dirColor    vec4 (rgb, _)
//	[48..51] pointCount  vec4 (x = count as f32, rest pad)
const frameUBOFloats = 52
const frameUBOBytes = frameUBOFloats * 4 // 208

// buildPipeline creates the bind group layouts, render pipeline, frame uniform +
// point-light storage buffers and their bind group, and the growable instance
// buffer. Populates the corresponding GPU fields.
func buildPipeline(g *GPU) {
	var err error

	// Bind group 0: frame uniform (binding 0) + point-light storage (binding 1).
	g.frameBGL, err = g.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "spliti.render3d.frame.bgl",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeUniform,
					MinBindingSize: frameUBOBytes,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0, // runtime-sized array
				},
			},
		},
	})
	if err != nil {
		panic("render3d: frame bgl: " + err.Error())
	}

	// Bind group 1: per-material uniform (one bind group per registered material).
	g.materialBGL, err = g.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "spliti.render3d.material.bgl",
		Entries: []wgpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: wgpu.ShaderStageFragment,
			Buffer: wgpu.BufferBindingLayout{
				Type:           wgpu.BufferBindingTypeUniform,
				MinBindingSize: materialUBOBytes,
			},
		}},
	})
	if err != nil {
		panic("render3d: material bgl: " + err.Error())
	}

	g.pipeline, err = buildRenderPipeline(g, false)
	if err != nil {
		panic("render3d: render pipeline: " + err.Error())
	}
	g.transparentPipeline, err = buildRenderPipeline(g, true)
	if err != nil {
		panic("render3d: transparent pipeline: " + err.Error())
	}

	g.instanceCap = initialInstanceCap
	g.instanceBuf = newInstanceBuffer(g, g.instanceCap)

	g.frameBuf, err = g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "spliti.render3d.frame",
		Size:  frameUBOBytes,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		panic("render3d: frame buffer: " + err.Error())
	}

	g.pointCap = initialPointCap
	g.pointBuf = newPointBuffer(g, g.pointCap)

	g.frameBindGroup = newFrameBindGroup(g)
}

// newFrameBindGroup builds the group-0 bind group from the current frame and
// point-light buffers. Must be rebuilt whenever pointBuf is reallocated.
func newFrameBindGroup(g *GPU) *wgpu.BindGroup {
	bg, err := g.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "spliti.render3d.frame.bg",
		Layout: g.frameBGL,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: g.frameBuf, Offset: 0, Size: frameUBOBytes},
			{Binding: 1, Buffer: g.pointBuf, Offset: 0, Size: uint64(g.pointCap * pointLightStride)},
		},
	})
	if err != nil {
		panic("render3d: frame bind group: " + err.Error())
	}
	return bg
}

// buildRenderPipeline creates the shader module, pipeline layout, and render
// pipeline using the current g.samples / g.format / g.depthFormat. Split out so
// the MSAA fallback can rebuild at a different sample count. Requires g.frameBGL
// and g.materialBGL to already exist.
//
// When transparent is true the pipeline uses premultiplied-alpha blending with
// depth-test ON but depth-write OFF, and no back-face culling — the configuration
// for translucent shells drawn back-to-front over the opaque scene.
func buildRenderPipeline(g *GPU, transparent bool) (*wgpu.RenderPipeline, error) {
	shader, err := g.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "spliti.render3d.shader",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaderCode},
	})
	if err != nil {
		return nil, err
	}
	defer shader.Release()

	layout, err := g.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "spliti.render3d.layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{g.frameBGL, g.materialBGL},
	})
	if err != nil {
		return nil, err
	}
	defer layout.Release()

	return g.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "spliti.render3d.pipeline",
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []wgpu.VertexBufferLayout{
				{ // buffer 0: per-vertex mesh data
					ArrayStride: vertexStride,
					StepMode:    wgpu.VertexStepModeVertex,
					Attributes: []wgpu.VertexAttribute{
						{Format: wgpu.VertexFormatFloat32x3, Offset: 0, ShaderLocation: 0},  // position
						{Format: wgpu.VertexFormatFloat32x3, Offset: 12, ShaderLocation: 1}, // normal
						{Format: wgpu.VertexFormatFloat32x2, Offset: 24, ShaderLocation: 2}, // uv
					},
				},
				{ // buffer 1: per-instance model matrix (4x vec4) + RGBA tint
					ArrayStride: instanceStride,
					StepMode:    wgpu.VertexStepModeInstance,
					Attributes: []wgpu.VertexAttribute{
						{Format: wgpu.VertexFormatFloat32x4, Offset: 0, ShaderLocation: 4},
						{Format: wgpu.VertexFormatFloat32x4, Offset: 16, ShaderLocation: 5},
						{Format: wgpu.VertexFormatFloat32x4, Offset: 32, ShaderLocation: 6},
						{Format: wgpu.VertexFormatFloat32x4, Offset: 48, ShaderLocation: 7},
						{Format: wgpu.VertexFormatFloat32x4, Offset: 64, ShaderLocation: 8}, // tint
					},
				},
			},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopologyTriangleList,
			FrontFace: wgpu.FrontFaceCCW,
			CullMode:  cullMode(transparent),
		},
		DepthStencil: &wgpu.DepthStencilState{
			Format:            g.depthFormat,
			DepthWriteEnabled: !transparent, // translucent geometry tests but does not write depth
			DepthCompare:      wgpu.CompareFunctionLess,
			StencilFront:      wgpu.StencilFaceState{Compare: wgpu.CompareFunctionAlways},
			StencilBack:       wgpu.StencilFaceState{Compare: wgpu.CompareFunctionAlways},
		},
		Multisample: wgpu.MultisampleState{Count: g.samples, Mask: 0xFFFFFFFF},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{{
				Format:    g.format,
				Blend:     blendState(transparent),
				WriteMask: wgpu.ColorWriteMaskAll,
			}},
		},
	})
}

// cullMode returns back-face culling for opaque solids and no culling for
// transparent shells (whose inner faces should still contribute).
func cullMode(transparent bool) wgpu.CullMode {
	if transparent {
		return wgpu.CullModeNone
	}
	return wgpu.CullModeBack
}

// blendState returns premultiplied-alpha blending for the transparent pipeline
// (src + dst*(1-srcAlpha)) and nil (opaque overwrite) otherwise. The shader emits
// premultiplied color for transparent draws so this matches.
func blendState(transparent bool) *wgpu.BlendState {
	if !transparent {
		return nil
	}
	return &wgpu.BlendState{
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
	}
}

// newInstanceBuffer allocates an instance vertex buffer for capacity model matrices.
func newInstanceBuffer(g *GPU, capacity int) *wgpu.Buffer {
	buf, err := g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "spliti.render3d.instances",
		Size:  uint64(capacity * instanceStride),
		Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		panic("render3d: instance buffer: " + err.Error())
	}
	return buf
}

// newPointBuffer allocates the point-light storage buffer for capacity lights.
func newPointBuffer(g *GPU, capacity int) *wgpu.Buffer {
	if capacity < 1 {
		capacity = 1
	}
	buf, err := g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "spliti.render3d.pointlights",
		Size:  uint64(capacity * pointLightStride),
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		panic("render3d: point light buffer: " + err.Error())
	}
	return buf
}

// normalizeSamples clamps a requested sample count to a portable MSAA value:
// WebGPU only guarantees 1 and 4, so anything above 1 maps to 4.
func normalizeSamples(n int) uint32 {
	if n <= 1 {
		return 1
	}
	return 4
}

// ensureMSAATarget (re)creates the multisampled color texture the render pass
// renders into before resolving to the swapchain. No-op when MSAA is off.
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
		Label:         "spliti.render3d.msaa",
		Usage:         wgpu.TextureUsageRenderAttachment,
		Dimension:     wgpu.TextureDimension2D,
		Size:          wgpu.Extent3D{Width: g.config.Width, Height: g.config.Height, DepthOrArrayLayers: 1},
		Format:        g.config.Format,
		MipLevelCount: 1,
		SampleCount:   g.samples,
	})
	if err != nil {
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

// ensureDepthTarget (re)creates the depth texture sized to the surface, at the
// current sample count (which must match the color attachment). Recreated on
// resize and after an MSAA sample-count change.
func ensureDepthTarget(g *GPU) {
	if g.depthView != nil {
		g.depthView.Release()
		g.depthView = nil
	}
	if g.depthTex != nil {
		g.depthTex.Release()
		g.depthTex = nil
	}
	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "spliti.render3d.depth",
		Usage:         wgpu.TextureUsageRenderAttachment,
		Dimension:     wgpu.TextureDimension2D,
		Size:          wgpu.Extent3D{Width: g.config.Width, Height: g.config.Height, DepthOrArrayLayers: 1},
		Format:        g.depthFormat,
		MipLevelCount: 1,
		SampleCount:   g.samples,
	})
	if err != nil {
		panic("render3d: depth texture: " + err.Error())
	}
	view, err := tex.CreateView(nil)
	if err != nil {
		tex.Release()
		panic("render3d: depth view: " + err.Error())
	}
	g.depthTex, g.depthView = tex, view
}

// disableMSAA drops the renderer to no multisampling after a failed MSAA-target
// allocation, rebuilding the pipeline AND the depth target at 1 sample so all
// three (color, depth, pipeline) agree on the sample count.
func disableMSAA(g *GPU) {
	if g.samples == 1 {
		return
	}
	g.samples = 1
	p, err := buildRenderPipeline(g, false)
	if err != nil {
		return
	}
	tp, err := buildRenderPipeline(g, true)
	if err != nil {
		p.Release()
		return
	}
	g.pipeline.Release()
	g.pipeline = p
	g.transparentPipeline.Release()
	g.transparentPipeline = tp
	ensureDepthTarget(g)
}

// ensureInstanceCap grows the instance buffer to hold at least n model matrices.
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

// ensurePointCap grows the point-light storage buffer to hold at least n lights,
// rebuilding the frame bind group (which references the buffer) when it grows.
func ensurePointCap(g *GPU, n int) {
	if n <= g.pointCap {
		return
	}
	newCap := g.pointCap * 2
	for newCap < n {
		newCap *= 2
	}
	g.pointBuf.Release()
	g.pointBuf = newPointBuffer(g, newCap)
	g.pointCap = newCap
	g.frameBindGroup.Release()
	g.frameBindGroup = newFrameBindGroup(g)
}
