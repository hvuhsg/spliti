// Screen-space textured-quad shader for the render3d 2D overlay (HUD). Vertices
// arrive already in normalized device coordinates (clip x,y in [-1,1]); each quad
// samples one RGBA panel texture. Used to blit CPU-rasterized HUD panels on top
// of the 3D frame.

struct VsIn {
    @location(0) pos : vec2<f32>, // NDC position
    @location(1) uv  : vec2<f32>,
};

struct VsOut {
    @builtin(position) clip : vec4<f32>,
    @location(0)       uv   : vec2<f32>,
};

@vertex
fn vs_main(in : VsIn) -> VsOut {
    var out : VsOut;
    out.clip = vec4<f32>(in.pos, 0.0, 1.0);
    out.uv = in.uv;
    return out;
}

@group(0) @binding(0) var panelTex : texture_2d<f32>;
@group(0) @binding(1) var panelSamp : sampler;

@fragment
fn fs_main(in : VsOut) -> @location(0) vec4<f32> {
    // The texture stores premultiplied alpha, matching the (ONE,
    // ONE_MINUS_SRC_ALPHA) blend on the pipeline.
    return textureSample(panelTex, panelSamp, in.uv);
}
