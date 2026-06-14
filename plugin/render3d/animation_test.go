package render3d

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

func approxVec3(a, b m.Vec3, eps float32) bool {
	return abs32(a.X-b.X) < eps && abs32(a.Y-b.Y) < eps && abs32(a.Z-b.Z) < eps
}

func TestSampleVec3LinearAndStep(t *testing.T) {
	s := AnimationSampler{Times: []float32{0, 1}, Values: []float32{0, 0, 0, 10, 0, 0}, Stride: 3, Interp: InterpLinear}
	if got := s.sampleVec3(0); !approxVec3(got, m.Vec3{}, 1e-5) {
		t.Errorf("t=0: %+v, want origin", got)
	}
	if got := s.sampleVec3(0.5); !approxVec3(got, m.Vec3{X: 5}, 1e-4) {
		t.Errorf("t=0.5: %+v, want (5,0,0)", got)
	}
	if got := s.sampleVec3(2); !approxVec3(got, m.Vec3{X: 10}, 1e-5) {
		t.Errorf("t=2 (clamped): %+v, want (10,0,0)", got)
	}
	step := s
	step.Interp = InterpStep
	if got := step.sampleVec3(0.9); !approxVec3(got, m.Vec3{}, 1e-5) {
		t.Errorf("STEP t=0.9: %+v, want held (0,0,0)", got)
	}
}

func TestSampleQuatSlerpEndpoints(t *testing.T) {
	a := m.IdentityQuat()
	b := m.FromAxisAngle(m.Vec3{Y: 1}, 1.0)
	s := AnimationSampler{
		Times:  []float32{0, 1},
		Values: []float32{a.X, a.Y, a.Z, a.W, b.X, b.Y, b.Z, b.W},
		Stride: 4,
		Interp: InterpLinear,
	}
	if got := s.sampleQuat(0); abs32(got.W-a.W) > 1e-5 {
		t.Errorf("t=0 quat=%+v, want identity", got)
	}
	got := s.sampleQuat(1)
	if abs32(got.Y-b.Y) > 1e-4 || abs32(got.W-b.W) > 1e-4 {
		t.Errorf("t=1 quat=%+v, want %+v", got, b)
	}
}

func TestMat4FromGLTFRoundTrip(t *testing.T) {
	src := m.TRS(m.Vec3{X: 1, Y: 2, Z: 3}, m.FromAxisAngle(m.Vec3{Z: 1}, 0.5), m.Vec3{X: 2, Y: 2, Z: 2})
	// The gltf modeler hands back matrices as [row][col]; mat4FromGLTF must turn
	// that into the engine's column-major layout (src[c*4+r] = element row r, col c).
	var gm [4][4]float32
	for c := 0; c < 4; c++ {
		for r := 0; r < 4; r++ {
			gm[r][c] = src[c*4+r]
		}
	}
	if got := mat4FromGLTF(gm); got != src {
		t.Errorf("mat4FromGLTF round-trip mismatch:\n got %v\nwant %v", got, src)
	}
}

func TestJointMatrices(t *testing.T) {
	// Two joints, identity inverse-binds and identity mesh world: the skin matrix
	// is just each joint's world transform.
	jw := []m.Mat4{m.Translation(m.Vec3{X: 5}), m.Translation(m.Vec3{Y: 7})}
	ib := []m.Mat4{m.Identity4(), m.Identity4()}
	out := jointMatrices(m.Identity4(), jw, ib, nil)
	if len(out) != 2 {
		t.Fatalf("got %d joint matrices, want 2", len(out))
	}
	if out[0] != jw[0] || out[1] != jw[1] {
		t.Errorf("identity mesh/invbind should pass joint world through: %v", out)
	}
	// With a translated mesh world, the mesh transform must cancel out.
	meshWorld := m.Translation(m.Vec3{X: 5})
	out = jointMatrices(meshWorld, jw, ib, out)
	// joint 0 world == mesh world => result is identity.
	if out[0] != m.Identity4() {
		t.Errorf("mesh-relative joint 0 = %v, want identity", out[0])
	}
}

func TestConvertPrimitiveSkinned(t *testing.T) {
	doc := &gltf.Document{Buffers: []*gltf.Buffer{{}}}
	positions := [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}
	posIdx := modeler.WritePosition(doc, positions)
	jIdx := modeler.WriteJoints(doc, [][4]uint16{{0, 1, 0, 0}, {1, 0, 0, 0}, {2, 0, 0, 0}})
	wIdx := modeler.WriteWeights(doc, [][4]float32{{0.5, 0.5, 0, 0}, {1, 0, 0, 0}, {1, 0, 0, 0}})
	prim := &gltf.Primitive{
		Mode: gltf.PrimitiveTriangles,
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:  posIdx,
			gltf.JOINTS_0:  jIdx,
			gltf.WEIGHTS_0: wIdx,
		},
	}
	mesh, err := convertPrimitive(doc, prim)
	if err != nil {
		t.Fatalf("convertPrimitive: %v", err)
	}
	if len(mesh.SkinVertices) != 3 {
		t.Fatalf("got %d skin vertices, want 3", len(mesh.SkinVertices))
	}
	sv := mesh.SkinVertices[0]
	if sv.J0 != 0 || sv.J1 != 1 || sv.W0 != 0.5 || sv.W1 != 0.5 {
		t.Errorf("skin vertex 0 joints/weights = %d,%d / %v,%v, want 0,1 / 0.5,0.5", sv.J0, sv.J1, sv.W0, sv.W1)
	}
	if sv.PX != 0 || mesh.SkinVertices[1].PX != 1 {
		t.Errorf("positions not copied into skin vertices")
	}
	// Static Vertices must still be present (bind pose for bounds/picking).
	if len(mesh.Vertices) != 3 {
		t.Errorf("static Vertices dropped (%d)", len(mesh.Vertices))
	}
}

func TestParseAnimations(t *testing.T) {
	doc := &gltf.Document{Buffers: []*gltf.Buffer{{}}}
	timesIdx := modeler.WriteAccessor(doc, gltf.TargetNone, []float32{0, 1})
	outIdx := modeler.WriteAccessor(doc, gltf.TargetNone, [][3]float32{{0, 0, 0}, {10, 0, 0}})
	doc.Nodes = []*gltf.Node{{}}
	doc.Animations = []*gltf.Animation{{
		Name:     "move",
		Samplers: []*gltf.AnimationSampler{{Input: timesIdx, Output: outIdx, Interpolation: gltf.InterpolationLinear}},
		Channels: []*gltf.AnimationChannel{{
			Sampler: 0,
			Target:  gltf.AnimationChannelTarget{Node: gltf.Index(0), Path: gltf.TRSTranslation},
		}},
	}}

	anims, err := parseAnimations(doc)
	if err != nil {
		t.Fatalf("parseAnimations: %v", err)
	}
	if len(anims) != 1 || anims[0].Name != "move" {
		t.Fatalf("got %d anims (%+v), want 1 named move", len(anims), anims)
	}
	if anims[0].Duration != 1 {
		t.Errorf("duration = %v, want 1", anims[0].Duration)
	}
	if len(anims[0].Channels) != 1 || anims[0].Channels[0].Path != PathTranslation || anims[0].Channels[0].NodeIndex != 0 {
		t.Errorf("channel = %+v", anims[0].Channels)
	}
	if got := anims[0].Samplers[0].sampleVec3(0.5); !approxVec3(got, m.Vec3{X: 5}, 1e-4) {
		t.Errorf("sampled midpoint = %+v, want (5,0,0)", got)
	}
}

func TestParseSkins(t *testing.T) {
	doc := &gltf.Document{Buffers: []*gltf.Buffer{{}}}
	identity := [4][4]float32{{1, 0, 0, 0}, {0, 1, 0, 0}, {0, 0, 1, 0}, {0, 0, 0, 1}}
	// The modeler's [4][4] is indexed [row][col], so a translation (1,2,3) lives in
	// the last column (each row's 4th entry), not the last sub-array.
	trans := [4][4]float32{{1, 0, 0, 1}, {0, 1, 0, 2}, {0, 0, 1, 3}, {0, 0, 0, 1}}
	ibIdx := modeler.WriteInverseBindMatrices(doc, [][4][4]float32{identity, trans})
	doc.Skins = []*gltf.Skin{{Joints: []int{0, 1}, InverseBindMatrices: gltf.Index(ibIdx)}}

	skins, err := parseSkins(doc)
	if err != nil {
		t.Fatalf("parseSkins: %v", err)
	}
	if len(skins) != 1 || len(skins[0].Joints) != 2 || len(skins[0].InverseBinds) != 2 {
		t.Fatalf("skins = %+v", skins)
	}
	if skins[0].InverseBinds[0] != m.Identity4() {
		t.Errorf("inverse bind 0 = %v, want identity", skins[0].InverseBinds[0])
	}
	// Translation lives in column 3 (indices 12,13,14) of a column-major Mat4.
	tb := skins[0].InverseBinds[1]
	if tb[12] != 1 || tb[13] != 2 || tb[14] != 3 {
		t.Errorf("inverse bind 1 translation = (%v,%v,%v), want (1,2,3)", tb[12], tb[13], tb[14])
	}
}

// TestAdvanceAnimationsSystem runs the real system (no GPU) and confirms it writes
// the sampled translation into the target node's Transform3D.
func TestAdvanceAnimationsSystem(t *testing.T) {
	clip := Animation{
		Samplers: []AnimationSampler{{Times: []float32{0, 1}, Values: []float32{0, 0, 0, 10, 0, 0}, Stride: 3, Interp: InterpLinear}},
		Channels: []AnimationChannel{{NodeIndex: 0, Path: PathTranslation, Sampler: 0}},
		Duration: 1,
	}
	model := &Model{Animations: []Animation{clip}, Nodes: make([]ModelNode, 1)}

	a := app.New().AddPlugins(splititime.Plugin{})
	var node ecs.Entity
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		w := c.World()
		nodeMap := generic.NewMap2[Transform3D, GlobalTransform](w)
		node = nodeMap.NewWith(&Transform3D{Rotation: m.IdentityQuat(), Scale: m.Vec3{X: 1, Y: 1, Z: 1}}, &GlobalTransform{Matrix: m.Identity4()})
		animMap := generic.NewMap2[Animator, AnimationRig](w)
		// High Speed so even a sub-millisecond test frame clamps Time to the end.
		animMap.NewWith(
			&Animator{Model: model, Clip: 0, Speed: 1000, Loop: false, Playing: true},
			&AnimationRig{NodeEntities: []ecs.Entity{node}},
		)
	})
	a.AddSystems(schedule.PostUpdate, advanceAnimations)

	var x float32
	a.AddSystems(schedule.Last, func(c *app.Ctx) {
		xf := generic.NewMap[Transform3D](c.World())
		x = xf.Get(node).Translation.X
	})

	a.SetMaxFrames(2).Run()

	if x <= 0 || x > 10.001 {
		t.Fatalf("animated translation.X = %v, want in (0,10]", x)
	}
}
