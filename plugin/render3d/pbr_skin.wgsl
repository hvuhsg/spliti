// Skinned vertex stage for the render3d backend. It blends up to four joint
// matrices per vertex (linear blend skinning), then feeds the same VsOut the
// static vs_main produces, so the shared fs_main (in pbr.wgsl) lights it
// unchanged. The skinned render pipeline pairs this module's vs_skinned with
// pbr.wgsl's fs_main.
//
// Bind group 0: frame uniform (view/proj) — only the matrices are used here.
// Bind group 2: the joint matrix storage array, bound with a per-entity dynamic
//   offset so each skinned mesh sees its own joints at index 0.

struct Frame {
    view       : mat4x4<f32>,
    proj       : mat4x4<f32>,
    cameraPos  : vec4<f32>,
    ambient    : vec4<f32>,
    dirDir     : vec4<f32>,
    dirColor   : vec4<f32>,
    pointCount : vec4<f32>,
};
@group(0) @binding(0) var<uniform> frame : Frame;

@group(2) @binding(0) var<storage, read> joints : array<mat4x4<f32>>;

struct VsIn {
    @location(0) position : vec3<f32>,
    @location(1) normal   : vec3<f32>,
    @location(2) uv       : vec2<f32>,
    @location(3) tangent  : vec4<f32>,
    @location(4) m0   : vec4<f32>,
    @location(5) m1   : vec4<f32>,
    @location(6) m2   : vec4<f32>,
    @location(7) m3   : vec4<f32>,
    @location(8) tint : vec4<f32>,
    @location(9)  jidx : vec4<u32>, // JOINTS_0
    @location(10) jw   : vec4<f32>, // WEIGHTS_0
};

struct VsOut {
    @builtin(position) clip     : vec4<f32>,
    @location(0)       worldPos : vec3<f32>,
    @location(1)       normal   : vec3<f32>,
    @location(2)       uv       : vec2<f32>,
    @location(3)       tint     : vec4<f32>,
    @location(4)       tangent  : vec3<f32>,
    @location(5)       tbnSign  : f32,
};

fn normalMatrix(m : mat3x3<f32>) -> mat3x3<f32> {
    let a = m[0];
    let b = m[1];
    let c = m[2];
    let invdet = 1.0 / dot(a, cross(b, c));
    return mat3x3<f32>(cross(b, c) * invdet, cross(c, a) * invdet, cross(a, b) * invdet);
}

@vertex
fn vs_skinned(in : VsIn) -> VsOut {
    // Linear blend skinning. Weights normally sum to 1; if all four are zero
    // (an unrigged vertex) fall back to the identity so it is not collapsed.
    var skin = in.jw.x * joints[in.jidx.x]
             + in.jw.y * joints[in.jidx.y]
             + in.jw.z * joints[in.jidx.z]
             + in.jw.w * joints[in.jidx.w];
    if (in.jw.x + in.jw.y + in.jw.z + in.jw.w <= 0.0) {
        skin = mat4x4<f32>(1.0, 0.0, 0.0, 0.0,
                           0.0, 1.0, 0.0, 0.0,
                           0.0, 0.0, 1.0, 0.0,
                           0.0, 0.0, 0.0, 1.0);
    }

    let model = mat4x4<f32>(in.m0, in.m1, in.m2, in.m3);
    let skinnedPos = (skin * vec4<f32>(in.position, 1.0)).xyz;
    let world = model * vec4<f32>(skinnedPos, 1.0);

    let skin3 = mat3x3<f32>(skin[0].xyz, skin[1].xyz, skin[2].xyz);
    let upper = mat3x3<f32>(model[0].xyz, model[1].xyz, model[2].xyz);

    var out : VsOut;
    out.worldPos = world.xyz;
    out.normal = normalize(normalMatrix(upper) * (skin3 * in.normal));
    out.uv = in.uv;
    out.tint = in.tint;
    out.tangent = upper * (skin3 * in.tangent.xyz);
    out.tbnSign = in.tangent.w;
    out.clip = frame.proj * frame.view * world;
    return out;
}
