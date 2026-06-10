package render3d

import (
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// buildTestDoc assembles a minimal in-memory glTF document: a single triangle
// primitive with positions, normals, UVs, and indices, plus one node referencing
// it. No GPU is involved, so convertPrimitive / node mapping can be tested
// directly.
func buildTestDoc() *gltf.Document {
	doc := &gltf.Document{Buffers: []*gltf.Buffer{{}}}
	positions := [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}
	normals := [][3]float32{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}}
	uvs := [][2]float32{{0, 0}, {1, 0}, {0, 1}}
	indices := []uint32{0, 1, 2}

	posIdx := modeler.WritePosition(doc, positions)
	normIdx := modeler.WriteNormal(doc, normals)
	uvIdx := modeler.WriteTextureCoord(doc, uvs)
	idxIdx := modeler.WriteIndices(doc, indices)

	prim := &gltf.Primitive{
		Mode: gltf.PrimitiveTriangles,
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:   posIdx,
			gltf.NORMAL:     normIdx,
			gltf.TEXCOORD_0: uvIdx,
		},
		Indices: gltf.Index(idxIdx),
	}
	doc.Meshes = []*gltf.Mesh{{Primitives: []*gltf.Primitive{prim}}}
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	doc.Scenes = []*gltf.Scene{{Nodes: []int{0}}}
	doc.Scene = gltf.Index(0)
	return doc
}

func TestConvertPrimitive(t *testing.T) {
	doc := buildTestDoc()
	mesh, err := convertPrimitive(doc, doc.Meshes[0].Primitives[0])
	if err != nil {
		t.Fatalf("convertPrimitive: %v", err)
	}
	if len(mesh.Vertices) != 3 {
		t.Fatalf("got %d vertices, want 3", len(mesh.Vertices))
	}
	if len(mesh.Indices) != 3 {
		t.Fatalf("got %d indices, want 3", len(mesh.Indices))
	}
	// Position and UV should survive the round-trip.
	v1 := mesh.Vertices[1]
	if v1.PX != 1 || v1.PY != 0 || v1.PZ != 0 {
		t.Errorf("vertex 1 position = (%v,%v,%v), want (1,0,0)", v1.PX, v1.PY, v1.PZ)
	}
	if v1.U != 1 || v1.V != 0 {
		t.Errorf("vertex 1 uv = (%v,%v), want (1,0)", v1.U, v1.V)
	}
	// No TANGENT was supplied, so computeTangents must have filled a valid basis.
	for i, v := range mesh.Vertices {
		if v.TW != 1 && v.TW != -1 {
			t.Errorf("vertex %d has no derived tangent handedness (%v)", i, v.TW)
		}
	}
}

func TestConvertPrimitiveSynthesizesIndices(t *testing.T) {
	doc := &gltf.Document{Buffers: []*gltf.Buffer{{}}}
	positions := [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}
	posIdx := modeler.WritePosition(doc, positions)
	prim := &gltf.Primitive{
		Mode:       gltf.PrimitiveTriangles,
		Attributes: gltf.PrimitiveAttributes{gltf.POSITION: posIdx},
	}
	mesh, err := convertPrimitive(doc, prim)
	if err != nil {
		t.Fatalf("convertPrimitive: %v", err)
	}
	if len(mesh.Indices) != 3 {
		t.Errorf("synthesized %d indices, want 3", len(mesh.Indices))
	}
	// Normals were absent, so computeNormals should have produced unit normals.
	for i, v := range mesh.Vertices {
		nl := v.NX*v.NX + v.NY*v.NY + v.NZ*v.NZ
		if nl < 0.99 || nl > 1.01 {
			t.Errorf("vertex %d normal not unit length (len² = %v)", i, nl)
		}
	}
}

func TestConvertMaterialFactors(t *testing.T) {
	metallic := 0.25
	roughness := 0.75
	gm := &gltf.Material{
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorFactor: &[4]float64{0.1, 0.2, 0.3, 1},
			MetallicFactor:  &metallic,
			RoughnessFactor: &roughness,
		},
		EmissiveFactor: [3]float64{0, 0.5, 0},
		AlphaMode:      gltf.AlphaMask,
	}
	doc := &gltf.Document{}
	mat, err := convertMaterial(doc, gm, nil)
	if err != nil {
		t.Fatalf("convertMaterial: %v", err)
	}
	if mat.BaseColor.R != 0.1 || mat.BaseColor.G != 0.2 || mat.BaseColor.B != 0.3 {
		t.Errorf("baseColor = %+v", mat.BaseColor)
	}
	if mat.Metallic != 0.25 || mat.Roughness != 0.75 {
		t.Errorf("metallic/roughness = %v/%v, want 0.25/0.75", mat.Metallic, mat.Roughness)
	}
	if mat.Emissive.Y != 0.5 {
		t.Errorf("emissive = %+v, want G=0.5", mat.Emissive)
	}
	if mat.Alpha != AlphaMask {
		t.Errorf("alpha mode = %v, want AlphaMask", mat.Alpha)
	}
	if mat.AlphaCutoff != 0.5 {
		t.Errorf("alpha cutoff = %v, want default 0.5", mat.AlphaCutoff)
	}
}

func TestBuildModelNodeTree(t *testing.T) {
	// buildModel needs registries to upload into; convertPrimitive/material are
	// already covered, so here just verify node-tree assembly with nil-safe
	// registry stubs is not feasible (Load needs a GPU). Instead validate the
	// node→ModelNode mapping logic by checking nodeTransform on a TRS node.
	n := &gltf.Node{
		Translation: [3]float64{1, 2, 3},
		Rotation:    [4]float64{0, 0, 0, 1},
		Scale:       [3]float64{2, 2, 2},
	}
	tr := nodeTransform(n)
	if tr.Translation.X != 1 || tr.Translation.Y != 2 || tr.Translation.Z != 3 {
		t.Errorf("translation = %+v, want (1,2,3)", tr.Translation)
	}
	if tr.Scale.X != 2 {
		t.Errorf("scale = %+v, want (2,2,2)", tr.Scale)
	}
}
