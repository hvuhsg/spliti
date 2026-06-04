package webgpu

// Camera defines the visible world rectangle. World coordinates [0,WorldW] ×
// [0,WorldH] map to the whole window, with (0,0) at the top-left. Set it via
// the Plugin's WorldW/WorldH fields; a zero size defaults to the framebuffer's
// pixel dimensions (so one world unit == one pixel).
type Camera struct{ WorldW, WorldH float32 }

// ortho builds a column-major 4x4 orthographic projection mapping the rect
// [0,w] × [0,h] (top-left origin, Y down) to clip space [-1,1] × [-1,1] with Y
// flipped, and z passed through at 0 (valid in wgpu's [0,1] depth range).
//
// The result is column-major to match WGSL's mat4x4<f32>, so proj * vec4(...)
// in the shader is the standard transform. Returned as a flat [16]float32 ready
// to upload with queue.WriteBuffer.
func ortho(w, h float32) [16]float32 {
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	var m [16]float32
	// column 0
	m[0] = 2 / w
	// column 1 (Y flipped)
	m[5] = -2 / h
	// column 2 (z identity)
	m[10] = 1
	// column 3 (translation + w)
	m[12] = -1
	m[13] = 1
	m[15] = 1
	return m
}
