package render3d

import (
	"fmt"
	"math"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// Mesh is CPU-side geometry: an interleaved vertex list and a triangle index
// list. Triangles must wind counter-clockwise when viewed from outside (the
// renderer culls back faces). Built by the primitive generators or, later, a
// model loader.
type Mesh struct {
	Vertices []Vertex
	Indices  []uint32
}

// meshGPU is one uploaded mesh: its vertex and index buffers plus the index
// count to draw. The CPU geometry (cpu) is retained so picking can raycast
// against the original triangles — the renderer itself only needs the buffers,
// but keeping a compact copy here avoids re-uploading or re-deriving it.
type meshGPU struct {
	vbuf       *wgpu.Buffer
	ibuf       *wgpu.Buffer
	vbufSize   uint64 // byte size of vbuf, tracked for portable SetVertexBuffer
	ibufSize   uint64 // byte size of ibuf, tracked for portable SetIndexBuffer
	indexCount uint32
	cpu        *Mesh

	// Model-space bounding sphere, computed at Load and used for frustum culling.
	boundsCenter m.Vec3
	boundsRadius float32
}

// MeshRegistry maps a string ref to an uploaded GPU mesh. Games populate it
// (usually from a Startup system, after the GPU resource exists) and entities
// reference meshes by ref via the MeshRenderer component. Retrieve it with
// app.GetResource[MeshRegistry].
type MeshRegistry struct {
	gpu   *GPU
	byRef map[string]*meshGPU
}

func newMeshRegistry(g *GPU) *MeshRegistry {
	return &MeshRegistry{gpu: g, byRef: make(map[string]*meshGPU)}
}

// Load uploads a mesh to the GPU under ref, replacing any existing mesh for that
// ref. Call after the plugin's Build (i.e. from Startup or later), since it needs
// the device.
func (r *MeshRegistry) Load(ref string, mesh *Mesh) error {
	if r == nil || r.gpu == nil {
		return fmt.Errorf("render3d: mesh registry not initialized")
	}
	if mesh == nil || len(mesh.Vertices) == 0 || len(mesh.Indices) == 0 {
		return fmt.Errorf("render3d: mesh %q is empty", ref)
	}
	g := r.gpu

	vbytes := wgpu.ToBytes(mesh.Vertices)
	vbuf, err := g.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "spliti.render3d.vbuf." + ref,
		Contents: vbytes,
		Usage:    wgpu.BufferUsageVertex,
	})
	if err != nil {
		return fmt.Errorf("render3d: vertex buffer %q: %w", ref, err)
	}
	ibytes := wgpu.ToBytes(mesh.Indices)
	ibuf, err := g.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "spliti.render3d.ibuf." + ref,
		Contents: ibytes,
		Usage:    wgpu.BufferUsageIndex,
	})
	if err != nil {
		vbuf.Release()
		return fmt.Errorf("render3d: index buffer %q: %w", ref, err)
	}

	if old := r.byRef[ref]; old != nil {
		old.release()
	}
	center, radius := boundingSphere(mesh.Vertices)
	r.byRef[ref] = &meshGPU{
		vbuf:         vbuf,
		ibuf:         ibuf,
		vbufSize:     uint64(len(vbytes)),
		ibufSize:     uint64(len(ibytes)),
		indexCount:   uint32(len(mesh.Indices)),
		cpu:          mesh,
		boundsCenter: center,
		boundsRadius: radius,
	}
	return nil
}

// boundingSphere returns a model-space sphere enclosing all vertices: the AABB
// center and the maximum distance from it to any vertex. Cheap and tight enough
// for broad-phase frustum culling.
func boundingSphere(verts []Vertex) (m.Vec3, float32) {
	if len(verts) == 0 {
		return m.Vec3{}, 0
	}
	mn := m.Vec3{X: verts[0].PX, Y: verts[0].PY, Z: verts[0].PZ}
	mx := mn
	for _, v := range verts {
		mn.X, mn.Y, mn.Z = min(mn.X, v.PX), min(mn.Y, v.PY), min(mn.Z, v.PZ)
		mx.X, mx.Y, mx.Z = max(mx.X, v.PX), max(mx.Y, v.PY), max(mx.Z, v.PZ)
	}
	center := mn.Add(mx).Scale(0.5)
	var r2 float32
	for _, v := range verts {
		d := m.Vec3{X: v.PX, Y: v.PY, Z: v.PZ}.Sub(center)
		if s := d.LengthSq(); s > r2 {
			r2 = s
		}
	}
	return center, float32(math.Sqrt(float64(r2)))
}

// CPU returns the retained CPU geometry for ref, or nil if not registered. Used
// by picking; callers must not mutate the returned mesh.
func (r *MeshRegistry) CPU(ref string) *Mesh {
	gm := r.get(ref)
	if gm == nil {
		return nil
	}
	return gm.cpu
}

// get returns the uploaded mesh for ref, or nil if not registered.
func (r *MeshRegistry) get(ref string) *meshGPU {
	if r == nil {
		return nil
	}
	return r.byRef[ref]
}

// releaseAll frees every uploaded mesh. Called from the GPU teardown.
func (r *MeshRegistry) releaseAll() {
	if r == nil {
		return
	}
	for _, m := range r.byRef {
		m.release()
	}
	r.byRef = nil
}

func (m *meshGPU) release() {
	if m.vbuf != nil {
		m.vbuf.Release()
	}
	if m.ibuf != nil {
		m.ibuf.Release()
	}
}
