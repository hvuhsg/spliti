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

// buildPipeline creates the shader module, bind group layouts, render pipeline,
// sampler, the static quad buffer, the (growable) instance buffer, and the
// camera uniform buffer + bind group. Populates the corresponding GPU fields.
func buildPipeline(g *GPU, format wgpu.TextureFormat) {
	shader, err := g.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "spliti.webgpu.shader",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaderCode},
	})
	if err != nil {
		panic("webgpu: shader module: " + err.Error())
	}
	defer shader.Release()

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

	layout, err := g.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "spliti.webgpu.layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{g.camBGL, g.texBGL},
	})
	if err != nil {
		panic("webgpu: pipeline layout: " + err.Error())
	}
	defer layout.Release()

	g.pipeline, err = g.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
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
		Multisample: wgpu.MultisampleState{Count: 1, Mask: 0xFFFFFFFF},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{{
				Format: format,
				Blend: &wgpu.BlendState{
					Color: wgpu.BlendComponent{
						Operation: wgpu.BlendOperationAdd,
						SrcFactor: wgpu.BlendFactorSrcAlpha,
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
	if err != nil {
		panic("webgpu: render pipeline: " + err.Error())
	}

	g.sampler, err = g.device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:         "spliti.webgpu.sampler",
		AddressModeU:  wgpu.AddressModeClampToEdge,
		AddressModeV:  wgpu.AddressModeClampToEdge,
		AddressModeW:  wgpu.AddressModeClampToEdge,
		MagFilter:     wgpu.FilterModeNearest, // crisp pixel art
		MinFilter:     wgpu.FilterModeNearest,
		MipmapFilter:  wgpu.MipmapFilterModeNearest,
		LodMaxClamp:   1,
		MaxAnisotropy: 1,
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
