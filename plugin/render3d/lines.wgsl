// Immediate-mode line shader for the render3d gizmo system. Draws colored line
// segments in world space, transformed by the shared frame camera (group 0,
// binding 0). Only the view/proj prefix of the Frame uniform is referenced; the
// declared struct is smaller than the bound buffer, which WebGPU permits.

struct Frame {
    view : mat4x4<f32>,
    proj : mat4x4<f32>,
};
@group(0) @binding(0) var<uniform> frame : Frame;

struct VsIn {
    @location(0) position : vec3<f32>,
    @location(1) color    : vec4<f32>,
};

struct VsOut {
    @builtin(position) clip  : vec4<f32>,
    @location(0)       color : vec4<f32>,
};

@vertex
fn vs_main(in : VsIn) -> VsOut {
    var out : VsOut;
    out.clip = frame.proj * frame.view * vec4<f32>(in.position, 1.0);
    out.color = in.color;
    return out;
}

@fragment
fn fs_main(in : VsOut) -> @location(0) vec4<f32> {
    return in.color;
}
