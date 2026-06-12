package m

import "math"

// Quat is a unit quaternion representing a rotation, with W the scalar part. The
// zero value is NOT a valid rotation; use IdentityQuat for "no rotation".
type Quat struct{ X, Y, Z, W float32 }

// IdentityQuat returns the rotation that does nothing.
func IdentityQuat() Quat { return Quat{0, 0, 0, 1} }

// FromAxisAngle returns the quaternion rotating by angle (radians) about axis.
// The axis is normalized internally.
func FromAxisAngle(axis Vec3, angle float32) Quat {
	a := axis.Normalize()
	half := float64(angle) / 2
	s := float32(math.Sin(half))
	return Quat{a.X * s, a.Y * s, a.Z * s, float32(math.Cos(half))}
}

// FromEuler returns the quaternion for intrinsic Tait-Bryan angles (radians)
// applied in Y (yaw), X (pitch), Z (roll) order.
func FromEuler(pitch, yaw, roll float32) Quat {
	cp := float32(math.Cos(float64(pitch) / 2))
	sp := float32(math.Sin(float64(pitch) / 2))
	cy := float32(math.Cos(float64(yaw) / 2))
	sy := float32(math.Sin(float64(yaw) / 2))
	cr := float32(math.Cos(float64(roll) / 2))
	sr := float32(math.Sin(float64(roll) / 2))

	// q = qy * qx * qz
	return Quat{
		X: cy*sp*cr + sy*cp*sr,
		Y: sy*cp*cr - cy*sp*sr,
		Z: cy*cp*sr - sy*sp*cr,
		W: cy*cp*cr + sy*sp*sr,
	}.Normalize()
}

// ToEuler extracts the intrinsic Tait-Bryan angles (radians, Y-X-Z order) that
// FromEuler would compose back into this rotation. Near gimbal lock
// (|pitch| → 90°) yaw and roll are not unique; roll is forced to 0 and yaw
// absorbs the remaining rotation, which still round-trips through FromEuler.
func (a Quat) ToEuler() (pitch, yaw, roll float32) {
	r := a.Normalize().ToMat3()
	// R = Ry(yaw)·Rx(pitch)·Rz(roll); element (row,col) at col*3+row.
	sp := -r[7] // -R[1][2] = sin(pitch)
	// |cos(pitch)| from row 1 = (cp·sr, cp·cr, -sp); atan2 stays
	// well-conditioned where asin(sp) would not be (sp near ±1).
	cp := math.Hypot(float64(r[1]), float64(r[4]))
	pitch = float32(math.Atan2(float64(sp), cp))
	if cp < 1e-4 {
		// cos(pitch) ≈ 0: yaw and roll rotate around the same world axis, so
		// only yaw∓roll is observable. With roll pinned to 0,
		// R[0] = [cos(yaw∓roll), ±sin(yaw∓roll), 0].
		yaw = float32(math.Atan2(float64(sp*r[3]), float64(r[0]))) // sp·R[0][1], R[0][0]
		return pitch, yaw, 0
	}
	yaw = float32(math.Atan2(float64(r[6]), float64(r[8])))  // R[0][2], R[2][2]
	roll = float32(math.Atan2(float64(r[1]), float64(r[4]))) // R[1][0], R[1][1]
	return pitch, yaw, roll
}

// Mul returns the composed rotation a then b applied as (a*b), i.e. applying the
// result rotates a vector by b first, then a (standard quaternion convention).
func (a Quat) Mul(b Quat) Quat {
	return Quat{
		W: a.W*b.W - a.X*b.X - a.Y*b.Y - a.Z*b.Z,
		X: a.W*b.X + a.X*b.W + a.Y*b.Z - a.Z*b.Y,
		Y: a.W*b.Y - a.X*b.Z + a.Y*b.W + a.Z*b.X,
		Z: a.W*b.Z + a.X*b.Y - a.Y*b.X + a.Z*b.W,
	}
}

// Normalize returns the unit quaternion in a's direction, or IdentityQuat if a
// has (near) zero length.
func (a Quat) Normalize() Quat {
	l := float32(math.Sqrt(float64(a.X*a.X + a.Y*a.Y + a.Z*a.Z + a.W*a.W)))
	if l < 1e-8 {
		return IdentityQuat()
	}
	inv := 1 / l
	return Quat{a.X * inv, a.Y * inv, a.Z * inv, a.W * inv}
}

// Conjugate returns the inverse rotation of a unit quaternion.
func (a Quat) Conjugate() Quat { return Quat{-a.X, -a.Y, -a.Z, a.W} }

// RotateVec3 rotates v by the quaternion.
func (a Quat) RotateVec3(v Vec3) Vec3 {
	// v' = v + 2*q.w*(u x v) + 2*(u x (u x v)), with u = q.xyz.
	u := Vec3{a.X, a.Y, a.Z}
	t := u.Cross(v).Scale(2)
	return v.Add(t.Scale(a.W)).Add(u.Cross(t))
}

// ToMat3 returns the 3x3 rotation matrix for a unit quaternion.
func (a Quat) ToMat3() Mat3 {
	x, y, z, w := a.X, a.Y, a.Z, a.W
	xx, yy, zz := x*x, y*y, z*z
	xy, xz, yz := x*y, x*z, y*z
	wx, wy, wz := w*x, w*y, w*z
	return Mat3{
		1 - 2*(yy+zz), 2 * (xy + wz), 2 * (xz - wy),
		2 * (xy - wz), 1 - 2*(xx+zz), 2 * (yz + wx),
		2 * (xz + wy), 2 * (yz - wx), 1 - 2*(xx+yy),
	}
}

// ToMat4 returns the 4x4 rotation matrix for a unit quaternion.
func (a Quat) ToMat4() Mat4 {
	r := a.ToMat3()
	return Mat4{
		r[0], r[1], r[2], 0,
		r[3], r[4], r[5], 0,
		r[6], r[7], r[8], 0,
		0, 0, 0, 1,
	}
}

// Slerp spherically interpolates from a to b by t (0..1). Both must be unit
// quaternions; the result is unit.
func (a Quat) Slerp(b Quat, t float32) Quat {
	dot := a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W
	// Take the shorter arc.
	if dot < 0 {
		b = Quat{-b.X, -b.Y, -b.Z, -b.W}
		dot = -dot
	}
	if dot > 0.9995 {
		// Nearly parallel: linear interpolate and renormalize.
		return Quat{
			a.X + (b.X-a.X)*t,
			a.Y + (b.Y-a.Y)*t,
			a.Z + (b.Z-a.Z)*t,
			a.W + (b.W-a.W)*t,
		}.Normalize()
	}
	theta0 := math.Acos(float64(dot))
	theta := theta0 * float64(t)
	sinTheta := math.Sin(theta)
	sinTheta0 := math.Sin(theta0)
	s0 := float32(math.Cos(theta) - float64(dot)*sinTheta/sinTheta0)
	s1 := float32(sinTheta / sinTheta0)
	return Quat{
		a.X*s0 + b.X*s1,
		a.Y*s0 + b.Y*s1,
		a.Z*s0 + b.Z*s1,
		a.W*s0 + b.W*s1,
	}
}
