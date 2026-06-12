package m

import (
	"math"
	"testing"
)

const eps = 1e-5

func approx(a, b float32) bool {
	d := a - b
	return d < eps && d > -eps
}

func vecApprox(a, b Vec3) bool {
	return approx(a.X, b.X) && approx(a.Y, b.Y) && approx(a.Z, b.Z)
}

func matApprox(a, b Mat4) bool {
	for i := range a {
		if !approx(a[i], b[i]) {
			return false
		}
	}
	return true
}

func TestMat4MulIdentity(t *testing.T) {
	id := Identity4()
	a := Mat4{
		1, 2, 3, 4,
		5, 6, 7, 8,
		9, 10, 11, 12,
		13, 14, 15, 16,
	}
	if !matApprox(a.Mul(id), a) {
		t.Errorf("a*I != a")
	}
	if !matApprox(id.Mul(a), a) {
		t.Errorf("I*a != a")
	}
}

func TestMat4MulAssociative(t *testing.T) {
	a := Translation(Vec3{1, 2, 3})
	b := Scaling(Vec3{2, 2, 2})
	c := FromAxisAngle(Vec3{0, 1, 0}, 0.7).ToMat4()
	left := a.Mul(b).Mul(c)
	right := a.Mul(b.Mul(c))
	if !matApprox(left, right) {
		t.Errorf("(a*b)*c != a*(b*c)")
	}
}

func TestMat4Inverse(t *testing.T) {
	a := TRS(Vec3{3, -2, 5}, FromEuler(0.3, 0.6, -0.2), Vec3{1.5, 2, 0.5})
	inv, ok := a.Inverse()
	if !ok {
		t.Fatal("expected invertible matrix")
	}
	if !matApprox(a.Mul(inv), Identity4()) {
		t.Errorf("a*inv != I")
	}
}

// TestPerspectiveDepthRange verifies the [0,1] clip-z convention: a point at -near
// maps to clip z 0 and -far to clip z 1 after the perspective divide.
func TestPerspectiveDepthRange(t *testing.T) {
	near, far := float32(0.5), float32(100)
	p := Perspective(DegToRad(60), 1.6, near, far)

	atNear := p.MulVec4(Vec4{0, 0, -near, 1})
	zNear := atNear.Z / atNear.W
	if !approx(zNear, 0) {
		t.Errorf("near plane clip z = %v, want 0", zNear)
	}

	atFar := p.MulVec4(Vec4{0, 0, -far, 1})
	zFar := atFar.Z / atFar.W
	if !approx(zFar, 1) {
		t.Errorf("far plane clip z = %v, want 1", zFar)
	}
}

// TestPerspectiveAspect verifies the aspect ratio scales x relative to y.
func TestPerspectiveAspect(t *testing.T) {
	aspect := float32(2)
	p := Perspective(DegToRad(90), aspect, 1, 10)
	// At 90deg vertical fov, f = 1, so y scale is 1 and x scale is 1/aspect.
	if !approx(p[5], 1) {
		t.Errorf("y scale = %v, want 1", p[5])
	}
	if !approx(p[0], 1/aspect) {
		t.Errorf("x scale = %v, want %v", p[0], 1/aspect)
	}
}

// TestLookAt checks the eye maps to the origin and the forward axis maps to -Z.
func TestLookAt(t *testing.T) {
	eye := Vec3{0, 0, 5}
	target := Vec3{0, 0, 0}
	view := LookAt(eye, target, Vec3{0, 1, 0})

	// Eye -> origin in view space.
	o := view.MulVec4(Vec4{eye.X, eye.Y, eye.Z, 1})
	if !vecApprox(o.XYZ(), Vec3{}) {
		t.Errorf("eye in view space = %v, want origin", o.XYZ())
	}

	// A point in front of the camera (toward target) has negative view z.
	front := view.MulVec4(Vec4{0, 0, 0, 1})
	if front.Z >= 0 {
		t.Errorf("target view z = %v, want negative (in front)", front.Z)
	}
}

// TestLookAtOrthonormal checks the rotation part of the view matrix is orthonormal.
func TestLookAtOrthonormal(t *testing.T) {
	view := LookAt(Vec3{3, 4, 5}, Vec3{0, 0, 0}, Vec3{0, 1, 0})
	rot := Mat3FromMat4(view)
	prod := rot.Mul(rot.Transpose())
	if !mat3Approx(prod, Identity3()) {
		t.Errorf("view rotation not orthonormal: R*R^T = %v", prod)
	}
}

func mat3Approx(a, b Mat3) bool {
	for i := range a {
		if !approx(a[i], b[i]) {
			return false
		}
	}
	return true
}

// TestQuatRoundTrip checks ToMat4 matches RotateVec3 for a representative rotation.
func TestQuatRoundTrip(t *testing.T) {
	q := FromAxisAngle(Vec3{0.3, 1, 0.2}, 1.2)
	v := Vec3{1, 2, 3}
	viaRotate := q.RotateVec3(v)
	mat := q.ToMat4()
	viaMat := mat.MulVec4(Vec4{v.X, v.Y, v.Z, 1}).XYZ()
	if !vecApprox(viaRotate, viaMat) {
		t.Errorf("RotateVec3 = %v, ToMat4 = %v", viaRotate, viaMat)
	}
}

// TestQuatAxisAngle90 checks a 90deg rotation about Y sends +Z to +X.
func TestQuatAxisAngle90(t *testing.T) {
	q := FromAxisAngle(Vec3{0, 1, 0}, float32(math.Pi/2))
	got := q.RotateVec3(Vec3{0, 0, 1})
	if !vecApprox(got, Vec3{1, 0, 0}) {
		t.Errorf("Y 90deg of +Z = %v, want {1,0,0}", got)
	}
}

func TestQuatMulIsComposition(t *testing.T) {
	a := FromAxisAngle(Vec3{0, 1, 0}, float32(math.Pi/2))
	b := FromAxisAngle(Vec3{1, 0, 0}, float32(math.Pi/2))
	v := Vec3{0, 0, 1}
	// (a*b) applied = a after b.
	viaQuat := a.Mul(b).RotateVec3(v)
	viaSeq := a.RotateVec3(b.RotateVec3(v))
	if !vecApprox(viaQuat, viaSeq) {
		t.Errorf("quat mul %v != sequential %v", viaQuat, viaSeq)
	}
}

// TestNormalMatrix checks that under non-uniform scale a normal stays
// perpendicular to a tangent after transformation by the normal/model matrices.
func TestNormalMatrix(t *testing.T) {
	model := TRS(Vec3{1, 2, 3}, FromAxisAngle(Vec3{0, 1, 0}, 0.5), Vec3{1, 3, 0.5})
	nm := NormalMatrix(model)

	// A surface with tangent t and normal n (perpendicular).
	tangent := Vec3{1, 0, 0}
	normal := Vec3{0, 1, 0}

	// Tangents transform by the model's linear part; normals by nm.
	mt := Mat3FromMat4(model).MulVec3(tangent)
	mn := nm.MulVec3(normal)
	if d := mt.Dot(mn); d > 1e-4 || d < -1e-4 {
		t.Errorf("transformed normal not perpendicular to tangent: dot = %v", d)
	}
}

func TestTRSMatchesComposition(t *testing.T) {
	tr := Vec3{4, -1, 2}
	rot := FromEuler(0.2, 0.5, 0.1)
	sc := Vec3{2, 0.5, 1.5}
	got := TRS(tr, rot, sc)
	want := Translation(tr).Mul(rot.ToMat4()).Mul(Scaling(sc))
	if !matApprox(got, want) {
		t.Errorf("TRS = %v, want T*R*S = %v", got, want)
	}
}

func TestSlerpEndpoints(t *testing.T) {
	a := FromAxisAngle(Vec3{0, 1, 0}, 0.2)
	b := FromAxisAngle(Vec3{0, 1, 0}, 1.5)
	if !quatApprox(a.Slerp(b, 0), a) {
		t.Errorf("slerp(0) != a")
	}
	if !quatApprox(a.Slerp(b, 1), b) {
		t.Errorf("slerp(1) != b")
	}
}

func quatApprox(a, b Quat) bool {
	return approx(a.X, b.X) && approx(a.Y, b.Y) && approx(a.Z, b.Z) && approx(a.W, b.W)
}

func TestToEulerRoundTrip(t *testing.T) {
	cases := [][3]float32{
		{0, 0, 0},
		{0.3, 0, 0}, {0, 0.7, 0}, {0, 0, -1.1},
		{0.4, -0.9, 0.2}, {-1.2, 2.8, -2.9}, {1.0, -3.0, 3.0},
	}
	for _, c := range cases {
		q := FromEuler(c[0], c[1], c[2])
		p, y, r := q.ToEuler()
		q2 := FromEuler(p, y, r)
		if !quatApprox(q, q2) {
			t.Errorf("euler %v: round trip %v -> (%v,%v,%v) -> %v", c, q, p, y, r, q2)
		}
	}
}

func TestToEulerGimbalLock(t *testing.T) {
	for _, pitch := range []float32{math.Pi / 2, -math.Pi / 2} {
		q := FromEuler(pitch, 0.8, -0.4)
		p, y, r := q.ToEuler()
		if r != 0 {
			t.Errorf("pitch %v: roll not pinned to 0 at gimbal lock: %v", pitch, r)
		}
		if !quatApprox(FromEuler(p, y, r), q) {
			t.Errorf("pitch %v: gimbal-lock extraction does not round trip", pitch)
		}
	}
}
