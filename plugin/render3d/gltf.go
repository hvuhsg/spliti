package render3d

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.Decode
	_ "image/png"  // register PNG decoder for image.Decode
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// Model is a loaded glTF asset whose meshes and materials have already been
// uploaded into the registries under generated refs. It describes the node
// hierarchy so SpawnModel can recreate it as an entity tree. Callers normally do
// LoadGLTF(...) then SpawnModel(...).
type Model struct {
	Name  string
	Nodes []ModelNode // parallel to the glTF node list; Children index into this slice
	Roots []int       // indices of the scene's root nodes
}

// ModelNode is one node in a loaded model: a local transform, the mesh
// primitives drawn at this node (each a parallel (meshRef, matRef) pair), and
// child node indices.
type ModelNode struct {
	Name      string
	Transform Transform3D
	MeshRefs  []string // one per glTF primitive on this node's mesh
	MatRefs   []string // parallel to MeshRefs ("" => default material)
	Children  []int
}

// LoadGLTF decodes a .gltf or .glb file from disk, uploads every triangle
// primitive as a mesh and every material (with textures) into the registries
// under refs prefixed by name, and returns a Model for SpawnModel. It must run
// after the render3d plugin's Build (it needs the GPU device).
func LoadGLTF(meshes *MeshRegistry, materials *MaterialRegistry, name, file string) (*Model, error) {
	doc, err := gltf.Open(file)
	if err != nil {
		return nil, fmt.Errorf("render3d: open glTF %q: %w", file, err)
	}
	dir := filepath.Dir(file)
	readURI := func(uri string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, uri)) }
	return buildModel(meshes, materials, name, doc, readURI)
}

// LoadGLTFFS is LoadGLTF reading from an fs.FS (e.g. an embed.FS), so models can
// be embedded in the binary. External buffer/image URIs are resolved relative to
// the glTF file's directory within the FS; self-contained .glb files need no
// external access.
func LoadGLTFFS(meshes *MeshRegistry, materials *MaterialRegistry, name string, fsys fs.FS, file string) (*Model, error) {
	f, err := fsys.Open(file)
	if err != nil {
		return nil, fmt.Errorf("render3d: open glTF %q: %w", file, err)
	}
	defer f.Close()
	doc := new(gltf.Document)
	if err := gltf.NewDecoderFS(f, fsys).Decode(doc); err != nil {
		return nil, fmt.Errorf("render3d: decode glTF %q: %w", file, err)
	}
	dir := path.Dir(file)
	readURI := func(uri string) ([]byte, error) { return fs.ReadFile(fsys, path.Join(dir, uri)) }
	return buildModel(meshes, materials, name, doc, readURI)
}

// buildModel uploads meshes + materials and assembles the node tree. readURI
// resolves external (non-embedded, non-bufferView) image URIs to bytes.
func buildModel(meshes *MeshRegistry, materials *MaterialRegistry, name string, doc *gltf.Document, readURI func(string) ([]byte, error)) (*Model, error) {
	if meshes == nil || materials == nil {
		return nil, fmt.Errorf("render3d: nil registry")
	}

	// Materials first, so primitives can reference their refs.
	matRefs := make([]string, len(doc.Materials))
	for i, gm := range doc.Materials {
		ref := fmt.Sprintf("%s/mat%d", name, i)
		mat, err := convertMaterial(doc, gm, readURI)
		if err != nil {
			return nil, fmt.Errorf("render3d: material %d: %w", i, err)
		}
		if err := materials.Load(ref, mat); err != nil {
			return nil, err
		}
		matRefs[i] = ref
	}

	// Meshes: one engine mesh per glTF primitive. Track each glTF mesh's
	// primitive refs so nodes can reference them.
	meshPrims := make([][]primRef, len(doc.Meshes))
	for mi, gmesh := range doc.Meshes {
		for pi, prim := range gmesh.Primitives {
			if prim.Mode != gltf.PrimitiveTriangles {
				continue // only triangle lists are renderable by this pipeline
			}
			mesh, err := convertPrimitive(doc, prim)
			if err != nil {
				return nil, fmt.Errorf("render3d: mesh %d primitive %d: %w", mi, pi, err)
			}
			ref := fmt.Sprintf("%s/mesh%d_prim%d", name, mi, pi)
			if err := meshes.Load(ref, mesh); err != nil {
				return nil, err
			}
			matRef := ""
			if prim.Material != nil && *prim.Material < len(matRefs) {
				matRef = matRefs[*prim.Material]
			}
			meshPrims[mi] = append(meshPrims[mi], primRef{mesh: ref, material: matRef})
		}
	}

	// Nodes -> ModelNodes.
	model := &Model{Name: name, Nodes: make([]ModelNode, len(doc.Nodes))}
	for i, n := range doc.Nodes {
		mn := ModelNode{Name: n.Name, Transform: nodeTransform(n), Children: n.Children}
		if n.Mesh != nil && *n.Mesh < len(meshPrims) {
			for _, pr := range meshPrims[*n.Mesh] {
				mn.MeshRefs = append(mn.MeshRefs, pr.mesh)
				mn.MatRefs = append(mn.MatRefs, pr.material)
			}
		}
		model.Nodes[i] = mn
	}

	// Roots from the active scene (default 0).
	sceneIdx := 0
	if doc.Scene != nil {
		sceneIdx = *doc.Scene
	}
	if sceneIdx < len(doc.Scenes) {
		model.Roots = doc.Scenes[sceneIdx].Nodes
	}
	return model, nil
}

type primRef struct{ mesh, material string }

// convertPrimitive reads a glTF primitive's attributes into an engine Mesh,
// synthesizing normals/indices/tangents when the asset omits them.
func convertPrimitive(doc *gltf.Document, prim *gltf.Primitive) (*Mesh, error) {
	posIdx, ok := prim.Attributes[gltf.POSITION]
	if !ok {
		return nil, fmt.Errorf("primitive has no POSITION")
	}
	positions, err := modeler.ReadPosition(doc, doc.Accessors[posIdx], nil)
	if err != nil {
		return nil, err
	}
	n := len(positions)

	var normals [][3]float32
	if idx, ok := prim.Attributes[gltf.NORMAL]; ok {
		if normals, err = modeler.ReadNormal(doc, doc.Accessors[idx], nil); err != nil {
			return nil, err
		}
	}
	var uvs [][2]float32
	if idx, ok := prim.Attributes[gltf.TEXCOORD_0]; ok {
		if uvs, err = modeler.ReadTextureCoord(doc, doc.Accessors[idx], nil); err != nil {
			return nil, err
		}
	}
	var tangents [][4]float32
	if idx, ok := prim.Attributes[gltf.TANGENT]; ok {
		if tangents, err = modeler.ReadTangent(doc, doc.Accessors[idx], nil); err != nil {
			return nil, err
		}
	}

	mesh := &Mesh{Vertices: make([]Vertex, n)}
	for i := 0; i < n; i++ {
		v := Vertex{PX: positions[i][0], PY: positions[i][1], PZ: positions[i][2]}
		if i < len(normals) {
			v.NX, v.NY, v.NZ = normals[i][0], normals[i][1], normals[i][2]
		}
		if i < len(uvs) {
			v.U, v.V = uvs[i][0], uvs[i][1]
		}
		if i < len(tangents) {
			v.TX, v.TY, v.TZ, v.TW = tangents[i][0], tangents[i][1], tangents[i][2], tangents[i][3]
		}
		mesh.Vertices[i] = v
	}

	if prim.Indices != nil {
		if mesh.Indices, err = modeler.ReadIndices(doc, doc.Accessors[*prim.Indices], nil); err != nil {
			return nil, err
		}
	} else {
		mesh.Indices = make([]uint32, n)
		for i := range mesh.Indices {
			mesh.Indices[i] = uint32(i)
		}
	}

	if len(normals) == 0 {
		computeNormals(mesh)
	}
	if len(tangents) == 0 {
		computeTangents(mesh)
	}
	return mesh, nil
}

// convertMaterial maps a glTF material (factors + textures) to an engine
// Material, decoding any referenced image maps.
func convertMaterial(doc *gltf.Document, gm *gltf.Material, readURI func(string) ([]byte, error)) (Material, error) {
	mat := Material{
		BaseColor:   Color{R: 1, G: 1, B: 1, A: 1},
		Metallic:    1,
		Roughness:   1,
		NormalScale: 1,
	}
	if pbr := gm.PBRMetallicRoughness; pbr != nil {
		bc := pbr.BaseColorFactorOrDefault()
		mat.BaseColor = Color{R: float32(bc[0]), G: float32(bc[1]), B: float32(bc[2]), A: float32(bc[3])}
		mat.Metallic = float32(pbr.MetallicFactorOrDefault())
		mat.Roughness = float32(pbr.RoughnessFactorOrDefault())
		if pbr.BaseColorTexture != nil {
			img, err := decodeTexture(doc, pbr.BaseColorTexture.Index, readURI)
			if err != nil {
				return mat, err
			}
			mat.BaseColorTex = img
		}
		if pbr.MetallicRoughnessTexture != nil {
			img, err := decodeTexture(doc, pbr.MetallicRoughnessTexture.Index, readURI)
			if err != nil {
				return mat, err
			}
			mat.MetalRoughTex = img
		}
	}
	mat.Emissive = m.Vec3{X: float32(gm.EmissiveFactor[0]), Y: float32(gm.EmissiveFactor[1]), Z: float32(gm.EmissiveFactor[2])}
	mat.DoubleSided = gm.DoubleSided
	if gm.NormalTexture != nil && gm.NormalTexture.Index != nil {
		img, err := decodeTexture(doc, *gm.NormalTexture.Index, readURI)
		if err != nil {
			return mat, err
		}
		mat.NormalTex = img
		mat.NormalScale = float32(gm.NormalTexture.ScaleOrDefault())
	}
	switch gm.AlphaMode {
	case gltf.AlphaBlend:
		mat.Alpha = AlphaBlend
	case gltf.AlphaMask:
		mat.Alpha = AlphaMask
		mat.AlphaCutoff = float32(gm.AlphaCutoffOrDefault())
	default:
		mat.Alpha = AlphaOpaque
	}
	return mat, nil
}

// decodeTexture resolves a glTF texture index to its source image bytes and
// decodes them. Image bytes come from a bufferView (GLB / embedded), a
// data-URI, or an external file via readURI.
func decodeTexture(doc *gltf.Document, texIdx int, readURI func(string) ([]byte, error)) (image.Image, error) {
	if texIdx < 0 || texIdx >= len(doc.Textures) {
		return nil, fmt.Errorf("texture index %d out of range", texIdx)
	}
	src := doc.Textures[texIdx].Source
	if src == nil || *src >= len(doc.Images) {
		return nil, fmt.Errorf("texture %d has no image source", texIdx)
	}
	img := doc.Images[*src]

	var data []byte
	var err error
	switch {
	case img.BufferView != nil:
		data, err = modeler.ReadBufferView(doc, doc.BufferViews[*img.BufferView])
	case img.IsEmbeddedResource():
		data, err = img.MarshalData()
	case img.URI != "":
		data, err = readURI(img.URI)
	default:
		err = fmt.Errorf("image %d has no data", *src)
	}
	if err != nil {
		return nil, err
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image %d: %w", *src, err)
	}
	return decoded, nil
}

// nodeTransform converts a glTF node's transform (TRS fields, or a matrix it
// decomposes) into a Transform3D. glTF and the engine share the right-handed
// Y-up convention and the [x,y,z,w] quaternion order, so no axis conversion is
// needed.
func nodeTransform(n *gltf.Node) Transform3D {
	if n.Matrix != ([16]float64{}) && n.Matrix != gltf.DefaultMatrix {
		return decomposeMatrix(n.Matrix)
	}
	t := n.TranslationOrDefault()
	r := n.RotationOrDefault()
	s := n.ScaleOrDefault()
	return Transform3D{
		Translation: m.Vec3{X: float32(t[0]), Y: float32(t[1]), Z: float32(t[2])},
		Rotation:    m.Quat{X: float32(r[0]), Y: float32(r[1]), Z: float32(r[2]), W: float32(r[3])},
		Scale:       m.Vec3{X: float32(s[0]), Y: float32(s[1]), Z: float32(s[2])},
	}
}

// decomposeMatrix splits a column-major affine matrix into translation, a
// rotation quaternion, and per-axis scale (TRS). Shear is ignored.
func decomposeMatrix(mat [16]float64) Transform3D {
	c0 := m.Vec3{X: float32(mat[0]), Y: float32(mat[1]), Z: float32(mat[2])}
	c1 := m.Vec3{X: float32(mat[4]), Y: float32(mat[5]), Z: float32(mat[6])}
	c2 := m.Vec3{X: float32(mat[8]), Y: float32(mat[9]), Z: float32(mat[10])}
	sx, sy, sz := c0.Length(), c1.Length(), c2.Length()
	scale := m.Vec3{X: sx, Y: sy, Z: sz}
	if sx != 0 {
		c0 = c0.Scale(1 / sx)
	}
	if sy != 0 {
		c1 = c1.Scale(1 / sy)
	}
	if sz != 0 {
		c2 = c2.Scale(1 / sz)
	}
	return Transform3D{
		Translation: m.Vec3{X: float32(mat[12]), Y: float32(mat[13]), Z: float32(mat[14])},
		Rotation:    quatFromBasis(c0, c1, c2),
		Scale:       scale,
	}
}

// quatFromBasis builds a unit quaternion from an orthonormal column basis
// (c0,c1,c2 are the rotated x,y,z axes). Standard branchful conversion.
func quatFromBasis(c0, c1, c2 m.Vec3) m.Quat {
	m00, m10, m20 := c0.X, c0.Y, c0.Z
	m01, m11, m21 := c1.X, c1.Y, c1.Z
	m02, m12, m22 := c2.X, c2.Y, c2.Z
	trace := m00 + m11 + m22
	var q m.Quat
	switch {
	case trace > 0:
		s := float32(math.Sqrt(float64(trace+1))) * 2 // 4w
		q.W = 0.25 * s
		q.X = (m21 - m12) / s
		q.Y = (m02 - m20) / s
		q.Z = (m10 - m01) / s
	case m00 > m11 && m00 > m22:
		s := float32(math.Sqrt(float64(1+m00-m11-m22))) * 2 // 4x
		q.W = (m21 - m12) / s
		q.X = 0.25 * s
		q.Y = (m01 + m10) / s
		q.Z = (m02 + m20) / s
	case m11 > m22:
		s := float32(math.Sqrt(float64(1+m11-m00-m22))) * 2 // 4y
		q.W = (m02 - m20) / s
		q.X = (m01 + m10) / s
		q.Y = 0.25 * s
		q.Z = (m12 + m21) / s
	default:
		s := float32(math.Sqrt(float64(1+m22-m00-m11))) * 2 // 4z
		q.W = (m10 - m01) / s
		q.X = (m02 + m20) / s
		q.Y = (m12 + m21) / s
		q.Z = 0.25 * s
	}
	return q.Normalize()
}

// computeNormals fills flat-ish per-vertex normals by accumulating triangle face
// normals, for primitives that omit the NORMAL attribute.
func computeNormals(mesh *Mesh) {
	acc := make([]m.Vec3, len(mesh.Vertices))
	for i := 0; i+2 < len(mesh.Indices); i += 3 {
		i0, i1, i2 := mesh.Indices[i], mesh.Indices[i+1], mesh.Indices[i+2]
		p0 := m.Vec3{X: mesh.Vertices[i0].PX, Y: mesh.Vertices[i0].PY, Z: mesh.Vertices[i0].PZ}
		p1 := m.Vec3{X: mesh.Vertices[i1].PX, Y: mesh.Vertices[i1].PY, Z: mesh.Vertices[i1].PZ}
		p2 := m.Vec3{X: mesh.Vertices[i2].PX, Y: mesh.Vertices[i2].PY, Z: mesh.Vertices[i2].PZ}
		fn := p1.Sub(p0).Cross(p2.Sub(p0))
		acc[i0] = acc[i0].Add(fn)
		acc[i1] = acc[i1].Add(fn)
		acc[i2] = acc[i2].Add(fn)
	}
	for i := range mesh.Vertices {
		n := acc[i]
		if n.LengthSq() > 0 {
			n = n.Normalize()
		} else {
			n = m.Vec3{Y: 1}
		}
		mesh.Vertices[i].NX, mesh.Vertices[i].NY, mesh.Vertices[i].NZ = n.X, n.Y, n.Z
	}
}
