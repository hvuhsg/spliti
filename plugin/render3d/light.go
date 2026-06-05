package render3d

import "github.com/hvuhsg/spliti/plugin/render3d/m"

// MaxPointLights caps how many point lights are uploaded per frame. Extra lights
// (beyond this many) are dropped; packLights reports the truncation to callers
// via the returned count vs. the input length.
const MaxPointLights = 256

// pointLightStride is the byte size of one GPU point light: 8 float32s, matching
// the std430 PointLight struct in pbr.wgsl (two vec4s).
const pointLightStride = 32

// pointLightGPU is the GPU-facing layout of a point light. Field order and size
// must match PointLight in pbr.wgsl: posRange(16) + colorIntensity(16).
type pointLightGPU struct {
	PX, PY, PZ, Range  float32 // world position, w = range
	R, G, B, Intensity float32 // color, w = intensity
}

// collectedPoint is a point light gathered from the ECS (world position +
// parameters) before packing into GPU layout.
type collectedPoint struct {
	Pos       m.Vec3
	Color     m.Vec3
	Intensity float32
	Range     float32
}

// packLights converts collected point lights into GPU layout, truncating to
// limit and reusing dst (pass it pre-truncated). Pure: no GPU calls, so it is
// unit-testable. A non-positive range defaults to 10 world units.
func packLights(points []collectedPoint, limit int, dst []pointLightGPU) []pointLightGPU {
	n := len(points)
	if n > limit {
		n = limit
	}
	for i := 0; i < n; i++ {
		p := points[i]
		rng := p.Range
		if rng <= 0 {
			rng = 10
		}
		dst = append(dst, pointLightGPU{
			PX: p.Pos.X, PY: p.Pos.Y, PZ: p.Pos.Z, Range: rng,
			R: p.Color.X, G: p.Color.Y, B: p.Color.Z, Intensity: p.Intensity,
		})
	}
	return dst
}
