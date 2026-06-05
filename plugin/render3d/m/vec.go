// Package m is the small, GPU-free linear-algebra library backing the render3d
// plugin: 2/3/4-component vectors, 3x3/4x4 matrices, and quaternions, with the
// operations a 3D renderer needs (perspective/lookAt/TRS, normal matrices,
// quaternion rotation).
//
// It deliberately imports nothing outside the standard library so it stays a
// leaf package: tests run without cgo or a GPU, and game code can use the types
// for camera control and gameplay without pulling in GLFW/wgpu.
//
// Conventions: matrices are stored flat and column-major, matching WGSL's
// mat4x4<f32> and the orthographic helper in plugin/webgpu. Element (row r,
// col c) of a Mat4 lives at index c*4+r. The renderer is right-handed and
// targets WebGPU/Metal clip space, where the projected depth range is [0,1].
package m

import "math"

// Vec2 is a 2-component vector.
type Vec2 struct{ X, Y float32 }

// Vec3 is a 3-component vector. It is the workhorse type: positions, normals,
// directions, and colors all use it.
type Vec3 struct{ X, Y, Z float32 }

// Vec4 is a 4-component vector, used for homogeneous points and shader-facing
// data that must align to 16 bytes.
type Vec4 struct{ X, Y, Z, W float32 }

// --- Vec2 ---

func (a Vec2) Add(b Vec2) Vec2      { return Vec2{a.X + b.X, a.Y + b.Y} }
func (a Vec2) Sub(b Vec2) Vec2      { return Vec2{a.X - b.X, a.Y - b.Y} }
func (a Vec2) Scale(s float32) Vec2 { return Vec2{a.X * s, a.Y * s} }
func (a Vec2) Dot(b Vec2) float32   { return a.X*b.X + a.Y*b.Y }

// --- Vec3 ---

func (a Vec3) Add(b Vec3) Vec3      { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a Vec3) Sub(b Vec3) Vec3      { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec3) Scale(s float32) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }
func (a Vec3) Neg() Vec3            { return Vec3{-a.X, -a.Y, -a.Z} }

// Mul is the component-wise (Hadamard) product, handy for tinting colors.
func (a Vec3) Mul(b Vec3) Vec3 { return Vec3{a.X * b.X, a.Y * b.Y, a.Z * b.Z} }

func (a Vec3) Dot(b Vec3) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{
		a.Y*b.Z - a.Z*b.Y,
		a.Z*b.X - a.X*b.Z,
		a.X*b.Y - a.Y*b.X,
	}
}

func (a Vec3) LengthSq() float32 { return a.Dot(a) }
func (a Vec3) Length() float32   { return float32(math.Sqrt(float64(a.LengthSq()))) }

// Normalize returns the unit vector in a's direction, or the zero vector if a
// has (near) zero length.
func (a Vec3) Normalize() Vec3 {
	l := a.Length()
	if l < 1e-8 {
		return Vec3{}
	}
	inv := 1 / l
	return Vec3{a.X * inv, a.Y * inv, a.Z * inv}
}

// Lerp linearly interpolates from a to b by t (0..1).
func (a Vec3) Lerp(b Vec3, t float32) Vec3 {
	return Vec3{
		a.X + (b.X-a.X)*t,
		a.Y + (b.Y-a.Y)*t,
		a.Z + (b.Z-a.Z)*t,
	}
}

// --- Vec4 ---

func (a Vec4) Add(b Vec4) Vec4      { return Vec4{a.X + b.X, a.Y + b.Y, a.Z + b.Z, a.W + b.W} }
func (a Vec4) Sub(b Vec4) Vec4      { return Vec4{a.X - b.X, a.Y - b.Y, a.Z - b.Z, a.W - b.W} }
func (a Vec4) Scale(s float32) Vec4 { return Vec4{a.X * s, a.Y * s, a.Z * s, a.W * s} }
func (a Vec4) Dot(b Vec4) float32   { return a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W }

// XYZ drops the w component.
func (a Vec4) XYZ() Vec3 { return Vec3{a.X, a.Y, a.Z} }

// --- scalar helpers ---

// DegToRad converts degrees to radians.
func DegToRad(deg float32) float32 { return deg * (math.Pi / 180) }

// RadToDeg converts radians to degrees.
func RadToDeg(rad float32) float32 { return rad * (180 / math.Pi) }
