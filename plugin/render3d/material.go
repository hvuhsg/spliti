package render3d

import (
	"fmt"
	"image"
	"sort"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// AlphaMode controls how a material's alpha is interpreted.
type AlphaMode uint8

const (
	AlphaOpaque AlphaMode = iota // alpha ignored (default)
	AlphaBlend                   // alpha blended (entity should also carry Transparent)
	AlphaMask                    // alpha-tested against AlphaCutoff (fragments discarded below it)
)

// Material describes a PBR metallic-roughness surface. BaseColor is the albedo
// (and alpha); Metallic and Roughness are in [0,1]; Emissive adds light the
// surface gives off regardless of lighting.
//
// Optional texture maps refine the uniform values: a base-color map (sampled as
// sRGB) multiplies BaseColor; a metallic-roughness map (linear; glTF packs
// roughness in G, metallic in B) multiplies Metallic/Roughness; a normal map
// (linear, tangent-space) perturbs the surface normal scaled by NormalScale. A
// nil map leaves that channel as its uniform value. Decode images yourself
// (image.Decode) or let the glTF loader populate them.
type Material struct {
	BaseColor Color
	Metallic  float32
	Roughness float32
	Emissive  m.Vec3

	BaseColorTex  image.Image // sRGB albedo map (optional)
	MetalRoughTex image.Image // linear; G=roughness, B=metallic (optional)
	NormalTex     image.Image // linear tangent-space normal map (optional)
	NormalScale   float32     // strength of the normal map (default 1 when a normal map is set)

	Alpha       AlphaMode
	AlphaCutoff float32 // threshold for AlphaMask (default 0.5)

	// DoubleSided disables back-face culling for opaque draws using this material,
	// so both faces of an open or single-surface mesh (a teapot, a leaf, a cloth)
	// are shaded instead of seen through from the inside. Mirrors glTF's
	// doubleSided. Has no effect on Transparent entities, which never cull.
	DoubleSided bool
}

// materialUBOBytes is the size of the material uniform block: baseColor(16) +
// mr(16) + emissive(16) + flags(16) = 80 bytes (each a vec4 slot). The mr slot
// carries metallic, roughness, normalScale, alphaCutoff; flags carries the
// per-map texture-presence booleans (1/0) and the alpha mode.
const materialUBOBytes = 80
const materialUBOFloats = materialUBOBytes / 4 // 20

// materialGPU is one uploaded material: its uniform buffer, bind group, and any
// textures it owns (the shared default textures are not owned and not released
// here).
type materialGPU struct {
	buf         *wgpu.Buffer
	bindGroup   *wgpu.BindGroup
	owned       []*texture
	doubleSided bool // selects the opaque no-cull pipeline for this material's batches
}

// MaterialRegistry maps a string ref to an uploaded GPU material. Entities select
// a material via MaterialRef; an unknown or empty ref falls back to the default
// material (a neutral dielectric). Retrieve it with app.GetResource[MaterialRegistry].
type MaterialRegistry struct {
	gpu        *GPU
	byRef      map[string]*materialGPU
	defaultMat *materialGPU
	defaults   *defaultTextures
}

func newMaterialRegistry(g *GPU) *MaterialRegistry {
	r := &MaterialRegistry{gpu: g, byRef: make(map[string]*materialGPU)}
	r.defaults = newDefaultTextures(g)
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

// Keys returns the refs of every registered material, sorted. The editor uses it
// to offer a dropdown of known materials instead of a free-text field. The
// fallback default material has no ref and is not listed.
func (r *MaterialRegistry) Keys() []string {
	if r == nil {
		return nil
	}
	keys := make([]string, 0, len(r.byRef))
	for k := range r.byRef {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// upload creates the uniform buffer, textures, and bind group for a material.
// Missing texture maps are filled with the registry's shared default textures so
// the bind group is always complete; the per-map flags in the UBO tell the
// shader which maps are real.
func (r *MaterialRegistry) upload(mat Material) *materialGPU {
	g := r.gpu
	bc := mat.BaseColor
	if bc == (Color{}) {
		bc = Color{R: 0.8, G: 0.8, B: 0.8, A: 1}
	}
	normalScale := mat.NormalScale
	if normalScale == 0 {
		normalScale = 1
	}
	cutoff := mat.AlphaCutoff
	if cutoff == 0 {
		cutoff = 0.5
	}

	// Resolve textures: a provided map is uploaded and owned; otherwise the
	// shared default view is used (white = no-op multiply, flat normal).
	var owned []*texture
	baseView, baseFlag := r.defaults.white.view, float32(0)
	if mat.BaseColorTex != nil {
		t, err := uploadTexture(g, mat.BaseColorTex, true)
		if err != nil {
			panic("render3d: base color texture: " + err.Error())
		}
		owned = append(owned, t)
		baseView, baseFlag = t.view, 1
	}
	mrView, mrFlag := r.defaults.whiteLin.view, float32(0)
	if mat.MetalRoughTex != nil {
		t, err := uploadTexture(g, mat.MetalRoughTex, false)
		if err != nil {
			panic("render3d: metallic-roughness texture: " + err.Error())
		}
		owned = append(owned, t)
		mrView, mrFlag = t.view, 1
	}
	normView, normFlag := r.defaults.flatNorm.view, float32(0)
	if mat.NormalTex != nil {
		t, err := uploadTexture(g, mat.NormalTex, false)
		if err != nil {
			panic("render3d: normal texture: " + err.Error())
		}
		owned = append(owned, t)
		normView, normFlag = t.view, 1
	}

	data := [materialUBOFloats]float32{
		bc.R, bc.G, bc.B, bc.A,
		mat.Metallic, mat.Roughness, normalScale, cutoff,
		mat.Emissive.X, mat.Emissive.Y, mat.Emissive.Z, 0,
		baseFlag, mrFlag, normFlag, float32(mat.Alpha),
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
			{Binding: 1, TextureView: baseView},
			{Binding: 2, TextureView: mrView},
			{Binding: 3, TextureView: normView},
			{Binding: 4, Sampler: r.defaults.sampler},
		},
	})
	if err != nil {
		buf.Release()
		for _, t := range owned {
			t.release()
		}
		panic("render3d: material bind group: " + err.Error())
	}
	return &materialGPU{buf: buf, bindGroup: bg, owned: owned, doubleSided: mat.DoubleSided}
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
	if r.defaults != nil {
		r.defaults.release()
		r.defaults = nil
	}
}

func (m *materialGPU) release() {
	if m.bindGroup != nil {
		m.bindGroup.Release()
	}
	if m.buf != nil {
		m.buf.Release()
	}
	for _, t := range m.owned {
		t.release()
	}
	m.owned = nil
}
