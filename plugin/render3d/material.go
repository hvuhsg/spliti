package render3d

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// Material describes a PBR metallic-roughness surface. BaseColor is the albedo
// (and alpha); Metallic and Roughness are in [0,1]; Emissive adds light the
// surface gives off regardless of lighting. Texture maps (albedo / metallic-
// roughness / normal) are a planned addition — the shader reserves bind slots
// for them.
type Material struct {
	BaseColor Color
	Metallic  float32
	Roughness float32
	Emissive  m.Vec3
}

// materialUBOBytes is the size of the material uniform block: baseColor(16) +
// metallicRoughness(16) + emissive(16) = 48 bytes (each a vec4 slot).
const materialUBOBytes = 48

// materialGPU is one uploaded material: its uniform buffer and bind group.
type materialGPU struct {
	buf       *wgpu.Buffer
	bindGroup *wgpu.BindGroup
}

// MaterialRegistry maps a string ref to an uploaded GPU material. Entities select
// a material via MaterialRef; an unknown or empty ref falls back to the default
// material (a neutral dielectric). Retrieve it with app.GetResource[MaterialRegistry].
type MaterialRegistry struct {
	gpu        *GPU
	byRef      map[string]*materialGPU
	defaultMat *materialGPU
}

func newMaterialRegistry(g *GPU) *MaterialRegistry {
	r := &MaterialRegistry{gpu: g, byRef: make(map[string]*materialGPU)}
	r.defaultMat = r.upload(Material{
		BaseColor: Color{R: 0.8, G: 0.8, B: 0.8, A: 1},
		Metallic:  0,
		Roughness: 0.6,
	})
	return r
}

// Load uploads a material to the GPU under ref, replacing any existing one. Call
// after the plugin's Build.
func (r *MaterialRegistry) Load(ref string, mat Material) error {
	if r == nil || r.gpu == nil {
		return fmt.Errorf("render3d: material registry not initialized")
	}
	gm := r.upload(mat)
	if old := r.byRef[ref]; old != nil {
		old.release()
	}
	r.byRef[ref] = gm
	return nil
}

// upload creates the uniform buffer + bind group for a material.
func (r *MaterialRegistry) upload(mat Material) *materialGPU {
	g := r.gpu
	bc := mat.BaseColor
	if bc == (Color{}) {
		bc = Color{R: 0.8, G: 0.8, B: 0.8, A: 1}
	}
	data := [12]float32{
		bc.R, bc.G, bc.B, bc.A,
		mat.Metallic, mat.Roughness, 0, 0,
		mat.Emissive.X, mat.Emissive.Y, mat.Emissive.Z, 0,
	}
	buf, err := g.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "spliti.render3d.material",
		Contents: wgpu.ToBytes(data[:]),
		Usage:    wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		panic("render3d: material buffer: " + err.Error())
	}
	bg, err := g.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "spliti.render3d.material.bg",
		Layout: g.materialBGL,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: buf, Offset: 0, Size: materialUBOBytes},
		},
	})
	if err != nil {
		buf.Release()
		panic("render3d: material bind group: " + err.Error())
	}
	return &materialGPU{buf: buf, bindGroup: bg}
}

// get returns the material for ref, or the default material if ref is empty or
// unknown.
func (r *MaterialRegistry) get(ref string) *materialGPU {
	if r == nil {
		return nil
	}
	if ref != "" {
		if m, ok := r.byRef[ref]; ok {
			return m
		}
	}
	return r.defaultMat
}

// releaseAll frees every uploaded material plus the default. Called from teardown.
func (r *MaterialRegistry) releaseAll() {
	if r == nil {
		return
	}
	for _, m := range r.byRef {
		m.release()
	}
	r.byRef = nil
	if r.defaultMat != nil {
		r.defaultMat.release()
		r.defaultMat = nil
	}
}

func (m *materialGPU) release() {
	if m.bindGroup != nil {
		m.bindGroup.Release()
	}
	if m.buf != nil {
		m.buf.Release()
	}
}
