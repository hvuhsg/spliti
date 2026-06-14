package render3d

import (
	"math"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// TestSkinBindPoseIsIdentity is the regression test for the inverse-bind-matrix
// transpose bug: at the bind pose (default node transforms, no animation applied)
// every joint's skinning matrix must be the identity, because jointWorld[j] is
// exactly the matrix the inverse-bind undoes. If mat4FromGLTF transposes the
// inverse-bind matrices, the joints land far from identity and the mesh explodes
// into spikes once skinned. The earlier mat4FromGLTF round-trip test could not
// catch this — it fed the function data laid out with the same wrong convention.
func TestSkinBindPoseIsIdentity(t *testing.T) {
	md, err := DecodeGLTF("simpleskin", "testdata/SimpleSkin.gltf")
	if err != nil {
		t.Fatal(err)
	}
	if len(md.Skins) == 0 {
		t.Fatal("model has no skin")
	}

	// Bind-pose world matrix of every node (local transforms, no animation).
	world := make([]m.Mat4, len(md.Nodes))
	var walk func(idx int, parent m.Mat4)
	walk = func(idx int, parent m.Mat4) {
		if idx < 0 || idx >= len(md.Nodes) {
			return
		}
		w := parent.Mul(md.Nodes[idx].Transform.matrix())
		world[idx] = w
		for _, ch := range md.Nodes[idx].Children {
			walk(ch, w)
		}
	}
	for _, r := range md.Roots {
		walk(r, m.Identity4())
	}

	for si := range md.Skins {
		skin := md.Skins[si]
		// meshWorld: the node carrying this skin's renderable primitive.
		meshWorld := m.Identity4()
		for i := range md.Nodes {
			if md.Nodes[i].Skin == si && len(md.Nodes[i].PrimIdx) > 0 {
				meshWorld = world[i]
			}
		}
		jw := make([]m.Mat4, len(skin.Joints))
		for j, node := range skin.Joints {
			jw[j] = world[node]
		}
		out := jointMatrices(meshWorld, jw, skin.InverseBinds, nil)
		id := m.Identity4()
		for j, mtx := range out {
			for k := 0; k < 16; k++ {
				if d := math.Abs(float64(mtx[k] - id[k])); d > 1e-4 {
					t.Errorf("skin %d joint %d: bind-pose matrix not identity at [%d]: got %g (dev %g)\n%v",
						si, j, k, mtx[k], d, mtx)
					break
				}
			}
		}
	}
}
