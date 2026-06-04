// Textured-quad shader for the spliti webgpu renderer.
//
// Vertex buffer 0 (per-vertex): a unit quad, corner in [0,1] x [0,1].
// Vertex buffer 1 (per-instance): one sprite's world placement and tint.
// Bind group 0: the camera ortho matrix.
// Bind group 1: the sprite's texture + sampler.

struct Camera {
    proj : mat4x4<f32>,
};
@group(0) @binding(0) var<uniform> camera : Camera;

@group(1) @binding(0) var tex : texture_2d<f32>;
@group(1) @binding(1) var samp : sampler;

struct VsIn {
    @location(0) corner : vec2<f32>, // unit quad, 0..1
    @location(1) offset : vec2<f32>, // instance: world top-left
    @location(2) size   : vec2<f32>, // instance: world size
    @location(3) tint   : vec4<f32>, // instance: rgba tint
};

struct VsOut {
    @builtin(position) pos : vec4<f32>,
    @location(0) uv        : vec2<f32>,
    @location(1) tint      : vec4<f32>,
};

@vertex
fn vs_main(in : VsIn) -> VsOut {
    let world = in.offset + in.corner * in.size;
    var out : VsOut;
    out.pos = camera.proj * vec4<f32>(world, 0.0, 1.0);
    out.uv = in.corner;
    out.tint = in.tint;
    return out;
}

@fragment
fn fs_main(in : VsOut) -> @location(0) vec4<f32> {
    return textureSample(tex, samp, in.uv) * in.tint;
}
