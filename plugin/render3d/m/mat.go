package m

import "math"

// Mat3 is a 3x3 matrix stored flat and column-major: element (row r, col c) is
// at index c*3+r. Used for normal matrices.
type Mat3 [9]float32

// Mat4 is a 4x4 matrix stored flat and column-major: element (row r, col c) is
// at index c*4+r. This matches WGSL's mat4x4<f32> memory layout, so a Mat4 can
// be uploaded with queue.WriteBuffer and used directly in a shader.
type Mat4 [16]float32

// Identity4 returns the 4x4 identity matrix.
func Identity4() Mat4 {
	return Mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

// Identity3 returns the 3x3 identity matrix.
func Identity3() Mat3 {
	return Mat3{
		1, 0, 0,
		0, 1, 0,
		0, 0, 1,
	}
}

// Mul returns a*b (column-major). Column j of the result is a applied to column
// j of b, i.e. the usual matrix product where transforming a vector is M*v.
func (a Mat4) Mul(b Mat4) Mat4 {
	var r Mat4
	for c := 0; c < 4; c++ {
		for row := 0; row < 4; row++ {
			r[c*4+row] = a[0*4+row]*b[c*4+0] +
				a[1*4+row]*b[c*4+1] +
				a[2*4+row]*b[c*4+2] +
				a[3*4+row]*b[c*4+3]
		}
	}
	return r
}

// MulVec4 returns a*v.
func (a Mat4) MulVec4(v Vec4) Vec4 {
	return Vec4{
		a[0]*v.X + a[4]*v.Y + a[8]*v.Z + a[12]*v.W,
		a[1]*v.X + a[5]*v.Y + a[9]*v.Z + a[13]*v.W,
		a[2]*v.X + a[6]*v.Y + a[10]*v.Z + a[14]*v.W,
		a[3]*v.X + a[7]*v.Y + a[11]*v.Z + a[15]*v.W,
	}
}

// Transpose returns the transpose of a.
func (a Mat4) Transpose() Mat4 {
	var r Mat4
	for c := 0; c < 4; c++ {
		for row := 0; row < 4; row++ {
			r[row*4+c] = a[c*4+row]
		}
	}
	return r
}

// Inverse returns the inverse of a and true, or the zero matrix and false when
// a is singular. Uses the standard cofactor/adjugate method.
func (a Mat4) Inverse() (Mat4, bool) {
	// Local index helper for readability: column-major (row, col) -> flat.
	m := a
	var inv Mat4

	inv[0] = m[5]*m[10]*m[15] - m[5]*m[11]*m[14] - m[9]*m[6]*m[15] +
		m[9]*m[7]*m[14] + m[13]*m[6]*m[11] - m[13]*m[7]*m[10]
	inv[4] = -m[4]*m[10]*m[15] + m[4]*m[11]*m[14] + m[8]*m[6]*m[15] -
		m[8]*m[7]*m[14] - m[12]*m[6]*m[11] + m[12]*m[7]*m[10]
	inv[8] = m[4]*m[9]*m[15] - m[4]*m[11]*m[13] - m[8]*m[5]*m[15] +
		m[8]*m[7]*m[13] + m[12]*m[5]*m[11] - m[12]*m[7]*m[9]
	inv[12] = -m[4]*m[9]*m[14] + m[4]*m[10]*m[13] + m[8]*m[5]*m[14] -
		m[8]*m[6]*m[13] - m[12]*m[5]*m[10] + m[12]*m[6]*m[9]

	inv[1] = -m[1]*m[10]*m[15] + m[1]*m[11]*m[14] + m[9]*m[2]*m[15] -
		m[9]*m[3]*m[14] - m[13]*m[2]*m[11] + m[13]*m[3]*m[10]
	inv[5] = m[0]*m[10]*m[15] - m[0]*m[11]*m[14] - m[8]*m[2]*m[15] +
		m[8]*m[3]*m[14] + m[12]*m[2]*m[11] - m[12]*m[3]*m[10]
	inv[9] = -m[0]*m[9]*m[15] + m[0]*m[11]*m[13] + m[8]*m[1]*m[15] -
		m[8]*m[3]*m[13] - m[12]*m[1]*m[11] + m[12]*m[3]*m[9]
	inv[13] = m[0]*m[9]*m[14] - m[0]*m[10]*m[13] - m[8]*m[1]*m[14] +
		m[8]*m[2]*m[13] + m[12]*m[1]*m[10] - m[12]*m[2]*m[9]

	inv[2] = m[1]*m[6]*m[15] - m[1]*m[7]*m[14] - m[5]*m[2]*m[15] +
		m[5]*m[3]*m[14] + m[13]*m[2]*m[7] - m[13]*m[3]*m[6]
	inv[6] = -m[0]*m[6]*m[15] + m[0]*m[7]*m[14] + m[4]*m[2]*m[15] -
		m[4]*m[3]*m[14] - m[12]*m[2]*m[7] + m[12]*m[3]*m[6]
	inv[10] = m[0]*m[5]*m[15] - m[0]*m[7]*m[13] - m[4]*m[1]*m[15] +
		m[4]*m[3]*m[13] + m[12]*m[1]*m[7] - m[12]*m[3]*m[5]
	inv[14] = -m[0]*m[5]*m[14] + m[0]*m[6]*m[13] + m[4]*m[1]*m[14] -
		m[4]*m[2]*m[13] - m[12]*m[1]*m[6] + m[12]*m[2]*m[5]

	inv[3] = -m[1]*m[6]*m[11] + m[1]*m[7]*m[10] + m[5]*m[2]*m[11] -
		m[5]*m[3]*m[10] - m[9]*m[2]*m[7] + m[9]*m[3]*m[6]
	inv[7] = m[0]*m[6]*m[11] - m[0]*m[7]*m[10] - m[4]*m[2]*m[11] +
		m[4]*m[3]*m[10] + m[8]*m[2]*m[7] - m[8]*m[3]*m[6]
	inv[11] = -m[0]*m[5]*m[11] + m[0]*m[7]*m[9] + m[4]*m[1]*m[11] -
		m[4]*m[3]*m[9] - m[8]*m[1]*m[7] + m[8]*m[3]*m[5]
	inv[15] = m[0]*m[5]*m[10] - m[0]*m[6]*m[9] - m[4]*m[1]*m[10] +
		m[4]*m[2]*m[9] + m[8]*m[1]*m[6] - m[8]*m[2]*m[5]

	det := m[0]*inv[0] + m[1]*inv[4] + m[2]*inv[8] + m[3]*inv[12]
	if det > -1e-12 && det < 1e-12 {
		return Mat4{}, false
	}
	invDet := 1 / det
	for i := range inv {
		inv[i] *= invDet
	}
	return inv, true
}

// Translation returns a matrix translating by t.
func Translation(t Vec3) Mat4 {
	r := Identity4()
	r[12], r[13], r[14] = t.X, t.Y, t.Z
	return r
}

// Scaling returns a matrix scaling by s per axis.
func Scaling(s Vec3) Mat4 {
	r := Identity4()
	r[0], r[5], r[10] = s.X, s.Y, s.Z
	return r
}

// TRS composes a model matrix as Translation * Rotation * Scale, the standard
// order: scale first, then rotate, then translate.
func TRS(t Vec3, r Quat, s Vec3) Mat4 {
	rot := r.ToMat4()
	// Pre-scale the rotation's basis columns, then drop in the translation —
	// equivalent to Translation(t).Mul(rot).Mul(Scaling(s)) but cheaper.
	var out Mat4
	out[0] = rot[0] * s.X
	out[1] = rot[1] * s.X
	out[2] = rot[2] * s.X
	out[3] = 0
	out[4] = rot[4] * s.Y
	out[5] = rot[5] * s.Y
	out[6] = rot[6] * s.Y
	out[7] = 0
	out[8] = rot[8] * s.Z
	out[9] = rot[9] * s.Z
	out[10] = rot[10] * s.Z
	out[11] = 0
	out[12] = t.X
	out[13] = t.Y
	out[14] = t.Z
	out[15] = 1
	return out
}

// Perspective builds a right-handed perspective projection mapping a frustum to
// WebGPU/Metal clip space, where clip z spans [0,1] (near->0, far->1) after the
// perspective divide. fovY is the vertical field of view in radians, aspect is
// width/height, near/far are positive distances. The camera looks down -Z.
func Perspective(fovY, aspect, near, far float32) Mat4 {
	f := float32(1 / math.Tan(float64(fovY)/2))
	var r Mat4
	r[0] = f / aspect
	r[5] = f
	// Column-major; this is the [0,1] depth (D3D/Metal/WebGPU) convention.
	r[10] = far / (near - far)
	r[11] = -1
	r[14] = (far * near) / (near - far)
	return r
}

// Ortho builds a right-handed orthographic projection mapping the box
// [left,right]x[bottom,top]x[-near,-far] (view space, camera looking down -Z) to
// WebGPU/Metal clip space: x,y -> [-1,1] and z -> [0,1] (near->0, far->1). No
// perspective divide (w stays 1). near/far are positive distances, matching
// Perspective.
func Ortho(left, right, bottom, top, near, far float32) Mat4 {
	var r Mat4
	r[0] = 2 / (right - left)
	r[5] = 2 / (top - bottom)
	r[10] = -1 / (far - near)
	r[12] = -(right + left) / (right - left)
	r[13] = -(top + bottom) / (top - bottom)
	r[14] = -near / (far - near)
	r[15] = 1
	return r
}

// LookAt builds a right-handed view matrix placing the camera at eye, looking
// toward target, with the given up direction. World space is transformed into
// the camera's space (camera at origin, looking down -Z).
func LookAt(eye, target, up Vec3) Mat4 {
	f := target.Sub(eye).Normalize() // forward
	s := f.Cross(up).Normalize()     // right
	u := s.Cross(f)                  // recomputed up

	var r Mat4
	// Rows are the camera basis; columns hold the basis vectors transposed.
	r[0], r[4], r[8] = s.X, s.Y, s.Z
	r[1], r[5], r[9] = u.X, u.Y, u.Z
	r[2], r[6], r[10] = -f.X, -f.Y, -f.Z
	r[12] = -s.Dot(eye)
	r[13] = -u.Dot(eye)
	r[14] = f.Dot(eye)
	r[15] = 1
	return r
}

// Mat3FromMat4 extracts the upper-left 3x3 of a Mat4.
func Mat3FromMat4(a Mat4) Mat3 {
	return Mat3{
		a[0], a[1], a[2],
		a[4], a[5], a[6],
		a[8], a[9], a[10],
	}
}

// Mul returns a*b for 3x3 matrices (column-major).
func (a Mat3) Mul(b Mat3) Mat3 {
	var r Mat3
	for c := 0; c < 3; c++ {
		for row := 0; row < 3; row++ {
			r[c*3+row] = a[0*3+row]*b[c*3+0] +
				a[1*3+row]*b[c*3+1] +
				a[2*3+row]*b[c*3+2]
		}
	}
	return r
}

// MulVec3 returns a*v.
func (a Mat3) MulVec3(v Vec3) Vec3 {
	return Vec3{
		a[0]*v.X + a[3]*v.Y + a[6]*v.Z,
		a[1]*v.X + a[4]*v.Y + a[7]*v.Z,
		a[2]*v.X + a[5]*v.Y + a[8]*v.Z,
	}
}

// Transpose returns the transpose of a.
func (a Mat3) Transpose() Mat3 {
	return Mat3{
		a[0], a[3], a[6],
		a[1], a[4], a[7],
		a[2], a[5], a[8],
	}
}

// Inverse returns the inverse of a and true, or the zero matrix and false when
// a is singular.
func (a Mat3) Inverse() (Mat3, bool) {
	// Cofactors (column-major indices).
	c00 := a[4]*a[8] - a[7]*a[5]
	c01 := a[7]*a[2] - a[1]*a[8]
	c02 := a[1]*a[5] - a[4]*a[2]
	det := a[0]*c00 + a[3]*c01 + a[6]*c02
	if det > -1e-12 && det < 1e-12 {
		return Mat3{}, false
	}
	inv := 1 / det
	return Mat3{
		c00 * inv,
		c01 * inv,
		c02 * inv,
		(a[6]*a[5] - a[3]*a[8]) * inv,
		(a[0]*a[8] - a[6]*a[2]) * inv,
		(a[3]*a[2] - a[0]*a[5]) * inv,
		(a[3]*a[7] - a[6]*a[4]) * inv,
		(a[6]*a[1] - a[0]*a[7]) * inv,
		(a[0]*a[4] - a[3]*a[1]) * inv,
	}, true
}

// NormalMatrix returns transpose(inverse(upper-left 3x3 of model)), the matrix
// that correctly transforms normals under non-uniform scale. For a pure
// rotation (or uniform scale) it reduces to the rotation itself. Falls back to
// the plain upper-left 3x3 if the model is singular.
func NormalMatrix(model Mat4) Mat3 {
	upper := Mat3FromMat4(model)
	inv, ok := upper.Inverse()
	if !ok {
		return upper
	}
	return inv.Transpose()
}
