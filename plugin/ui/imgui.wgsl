// Renderer shader for the Dear ImGui wgpu backend. ImGui emits vertices in
// framebuffer pixels (origin top-left, y down) with an 8-bit sRGB RGBA color;
// the vertex stage maps pixels to clip space with an ortho scale+translate, and
// the fragment stage samples the bound texture (font atlas or a user texture)
// and multiplies by the per-vertex tint.

struct Uniforms {
    // xy = pixels->clip scale, zw = translate. clip = pos*scale + translate.
    scaleTranslate : vec4<f32>,
};

@group(0) @binding(0) var<uniform> u : Uniforms;
@group(0) @binding(1) var samp : sampler;
@group(1) @binding(0) var tex : texture_2d<f32>;

struct VsIn {
    @location(0) pos : vec2<f32>,
    @location(1) uv  : vec2<f32>,
    @location(2) col : vec4<f32>, // unorm8x4, sRGB-encoded
};

struct VsOut {
    @builtin(position) clip : vec4<f32>,
    @location(0)       uv   : vec2<f32>,
    @location(1)       col  : vec4<f32>, // linearized rgb, straight alpha
};

@vertex
fn vs_main(in : VsIn) -> VsOut {
    var out : VsOut;
    out.clip = vec4<f32>(in.pos * u.scaleTranslate.xy + u.scaleTranslate.zw, 0.0, 1.0);
    out.uv = in.uv;
    // The swapchain is sRGB, so the hardware re-encodes our linear output on write
    // (matching render3d's PBR pass). Linearize ImGui's sRGB vertex color here so
    // it round-trips back to the intended sRGB color; alpha stays linear.
    out.col = vec4<f32>(pow(in.col.rgb, vec3<f32>(2.2)), in.col.a);
    return out;
}

@fragment
fn fs_main(in : VsOut) -> @location(0) vec4<f32> {
    // Font atlas is uploaded as RGBA8 sRGB, so its sample is already linear here.
    return in.col * textureSample(tex, samp, in.uv);
}
