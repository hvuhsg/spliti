package render3d

import (
	_ "embed"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

//go:embed lines.wgsl
var lineShaderCode string

// gizmoVertex is one endpoint of a colored line segment: a world position plus a
// linear RGBA color. Size must equal gizmoVertexStride and match the vertex
// attribute layout in lines.wgsl.
type gizmoVertex struct {
	PX, PY, PZ float32
	R, G, B, A float32
}

const gizmoVertexStride = 28 // bytes; sizeof(gizmoVertex)

// crossHalf is half the size, in world units, of the little axis cross drawn for
// a Point (WebGPU point primitives are always one pixel, so points are drawn as
// three short crossing segments instead).
const crossHalf = 0.15

// GizmoBuffer is the immediate-mode line/point overlay. Each frame, gameplay and
// viz systems call Line/Ray/Point/Polyline to enqueue colored segments; a single
// overlay system (drawGizmos) uploads them once, draws them into the open render
// pass with depth-test on (so they are occluded by solid geometry) and depth-
// write off, then clears the queue. Retrieve it with app.GetResource[GizmoBuffer].
type GizmoBuffer struct {
	gpu      *GPU
	pipeline *wgpu.RenderPipeline
	builtFor uint32 // MSAA sample count the pipeline was built for
	buf      *wgpu.Buffer
	cap      int // capacity in vertices
	verts    []gizmoVertex
}

func newGizmoBuffer(g *GPU) *GizmoBuffer { return &GizmoBuffer{gpu: g} }

// reset clears the per-frame segment queue, retaining the backing array.
func (gz *GizmoBuffer) reset() { gz.verts = gz.verts[:0] }

func (gz *GizmoBuffer) release() {
	if gz == nil {
		return
	}
	if gz.buf != nil {
		gz.buf.Release()
		gz.buf = nil
	}
	if gz.pipeline != nil {
		gz.pipeline.Release()
		gz.pipeline = nil
	}
}

// push appends a single segment a->b with one color.
func (gz *GizmoBuffer) push(a, b m.Vec3, c m.Vec4) {
	gz.verts = append(gz.verts,
		gizmoVertex{a.X, a.Y, a.Z, c.X, c.Y, c.Z, c.W},
		gizmoVertex{b.X, b.Y, b.Z, c.X, c.Y, c.Z, c.W})
}

// Line enqueues a colored segment from a to b for this frame.
func Line(c *app.Ctx, a, b m.Vec3, color m.Vec4) {
	gz := app.GetResource[GizmoBuffer](c)
	if gz == nil {
		return
	}
	gz.push(a, b, color)
}

// Ray enqueues a segment from origin along dir of the given length. dir need not
// be normalized; a zero dir draws nothing.
func Ray(c *app.Ctx, origin, dir m.Vec3, length float32, color m.Vec4) {
	d := dir.Normalize()
	if d == (m.Vec3{}) {
		return
	}
	Line(c, origin, origin.Add(d.Scale(length)), color)
}

// Polyline enqueues a connected chain of segments through pts.
func Polyline(c *app.Ctx, pts []m.Vec3, color m.Vec4) {
	gz := app.GetResource[GizmoBuffer](c)
	if gz == nil || len(pts) < 2 {
		return
	}
	for i := 1; i < len(pts); i++ {
		gz.push(pts[i-1], pts[i], color)
	}
}

// Point enqueues a small axis cross centered at p (WebGPU has no point size, so a
// point is drawn as three short crossing segments).
func Point(c *app.Ctx, p m.Vec3, color m.Vec4) {
	gz := app.GetResource[GizmoBuffer](c)
	if gz == nil {
		return
	}
	gz.push(p.Sub(m.Vec3{X: crossHalf}), p.Add(m.Vec3{X: crossHalf}), color)
	gz.push(p.Sub(m.Vec3{Y: crossHalf}), p.Add(m.Vec3{Y: crossHalf}), color)
	gz.push(p.Sub(m.Vec3{Z: crossHalf}), p.Add(m.Vec3{Z: crossHalf}), color)
}

// drawGizmos uploads and draws the queued segments into the open render pass,
// then clears the queue. Registered as an overlay system (after the mesh render,
// before present).
func drawGizmos(c *app.Ctx) {
	g := app.GetResource[GPU](c)
	gz := app.GetResource[GizmoBuffer](c)
	if g == nil || gz == nil {
		return
	}
	defer gz.reset()
	if !g.frameActive || len(gz.verts) == 0 {
		return
	}
	gz.ensurePipeline(g)
	gz.ensureCap(len(gz.verts))
	if err := g.queue.WriteBuffer(gz.buf, 0, wgpu.ToBytes(gz.verts)); err != nil {
		return
	}
	pass := g.curPass
	pass.SetPipeline(gz.pipeline)
	pass.SetBindGroup(0, g.frameBindGroup, nil)
	pass.SetVertexBuffer(0, gz.buf, 0, uint64(len(gz.verts)*gizmoVertexStride))
	pass.Draw(uint32(len(gz.verts)), 1, 0, 0)
}

// ensurePipeline (re)builds the line pipeline if it is missing or was built for a
// different MSAA sample count (the MSAA fallback can change g.samples after the
// gizmo pipeline is first built).
func (gz *GizmoBuffer) ensurePipeline(g *GPU) {
	if gz.pipeline != nil && gz.builtFor == g.samples {
		return
	}
	if gz.pipeline != nil {
		gz.pipeline.Release()
	}
	gz.pipeline = buildLinePipeline(g)
	gz.builtFor = g.samples
}

// ensureCap grows the vertex buffer to hold at least n vertices.
func (gz *GizmoBuffer) ensureCap(n int) {
	if gz.buf != nil && n <= gz.cap {
		return
	}
	newCap := gz.cap
	if newCap < 256 {
		newCap = 256
	}
	for newCap < n {
		newCap *= 2
	}
	if gz.buf != nil {
		gz.buf.Release()
	}
	buf, err := gz.gpu.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "spliti.render3d.gizmo",
		Size:  uint64(newCap * gizmoVertexStride),
		Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		panic("render3d: gizmo buffer: " + err.Error())
	}
	gz.buf, gz.cap = buf, newCap
}

// buildLinePipeline creates the LineList pipeline for the gizmo overlay, reusing
// the frame bind group layout (group 0) for the camera uniform. Depth test is on
// (segments hide behind solids) but depth write is off (segments don't occlude).
func buildLinePipeline(g *GPU) *wgpu.RenderPipeline {
	shader, err := g.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "spliti.render3d.lines.shader",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: lineShaderCode},
	})
	if err != nil {
		panic("render3d: line shader: " + err.Error())
	}
	defer shader.Release()

	layout, err := g.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "spliti.render3d.lines.layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{g.frameBGL},
	})
	if err != nil {
		panic("render3d: line layout: " + err.Error())
	}
	defer layout.Release()

	pipe, err := g.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "spliti.render3d.lines.pipeline",
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []wgpu.VertexBufferLayout{{
				ArrayStride: gizmoVertexStride,
				StepMode:    wgpu.VertexStepModeVertex,
				Attributes: []wgpu.VertexAttribute{
					{Format: wgpu.VertexFormatFloat32x3, Offset: 0, ShaderLocation: 0},
					{Format: wgpu.VertexFormatFloat32x4, Offset: 12, ShaderLocation: 1},
				},
			}},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopologyLineList,
			FrontFace: wgpu.FrontFaceCCW,
			CullMode:  wgpu.CullModeNone,
		},
		DepthStencil: &wgpu.DepthStencilState{
			Format:            g.depthFormat,
			DepthWriteEnabled: false,
			DepthCompare:      wgpu.CompareFunctionLessEqual,
			StencilFront:      wgpu.StencilFaceState{Compare: wgpu.CompareFunctionAlways},
			StencilBack:       wgpu.StencilFaceState{Compare: wgpu.CompareFunctionAlways},
		},
		Multisample: wgpu.MultisampleState{Count: g.samples, Mask: 0xFFFFFFFF},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{{
				Format:    g.format,
				Blend:     nil,
				WriteMask: wgpu.ColorWriteMaskAll,
			}},
		},
	})
	if err != nil {
		panic("render3d: line pipeline: " + err.Error())
	}
	return pipe
}
