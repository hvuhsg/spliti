// PBR metallic-roughness shader for the spliti render3d backend (Cook-Torrance).
//
// Vertex buffer 0 (per-vertex): position, normal, uv, tangent (xyz + handedness).
// Vertex buffer 1 (per-instance): the model matrix as 4 columns (@location 4..7).
// Bind group 0: frame uniform (camera + directional light + ambient) and the
//   point-light storage array.
// Bind group 1: the material uniform (binding 0), base-color / metallic-
//   roughness / normal textures (bindings 1..3), and a shared sampler (4).

struct Frame {
    view       : mat4x4<f32>,
    proj       : mat4x4<f32>,
    cameraPos  : vec4<f32>, // xyz, w unused
    ambient    : vec4<f32>, // rgb ambient, w unused
    dirDir     : vec4<f32>, // xyz direction (toward the scene), w = intensity
    dirColor   : vec4<f32>, // rgb, w unused
    pointCount : vec4<f32>, // x = number of point lights, rest pad
};
@group(0) @binding(0) var<uniform> frame : Frame;

struct PointLight {
    posRange       : vec4<f32>, // xyz world position, w = range
    colorIntensity : vec4<f32>, // rgb color, w = intensity
};
@group(0) @binding(1) var<storage, read> points : array<PointLight>;

struct Material {
    baseColor : vec4<f32>, // rgba
    mr        : vec4<f32>, // x = metallic, y = roughness, z = normalScale, w = alphaCutoff
    emissive  : vec4<f32>, // rgb, w unused
    flags     : vec4<f32>, // x = hasBaseColorTex, y = hasMRTex, z = hasNormalTex, w = alphaMode
};
@group(1) @binding(0) var<uniform> material : Material;
@group(1) @binding(1) var baseColorTex : texture_2d<f32>;
@group(1) @binding(2) var mrTex        : texture_2d<f32>;
@group(1) @binding(3) var normalTex    : texture_2d<f32>;
@group(1) @binding(4) var texSampler   : sampler;

struct VsIn {
    @location(0) position : vec3<f32>,
    @location(1) normal   : vec3<f32>,
    @location(2) uv       : vec2<f32>,
    @location(3) tangent  : vec4<f32>, // xyz tangent, w handedness sign
    // Per-instance model matrix columns + RGBA tint.
    @location(4) m0   : vec4<f32>,
    @location(5) m1   : vec4<f32>,
    @location(6) m2   : vec4<f32>,
    @location(7) m3   : vec4<f32>,
    @location(8) tint : vec4<f32>,
};

struct VsOut {
    @builtin(position) clip     : vec4<f32>,
    @location(0)       worldPos : vec3<f32>,
    @location(1)       normal   : vec3<f32>,
    @location(2)       uv       : vec2<f32>,
    @location(3)       tint     : vec4<f32>,
    @location(4)       tangent  : vec3<f32>, // world-space tangent
    @location(5)       tbnSign  : f32,       // handedness for the bitangent
};

// normalMatrix returns transpose(inverse(m)), the matrix that transforms normals
// correctly under non-uniform scale. Built directly from cofactor cross products.
fn normalMatrix(m : mat3x3<f32>) -> mat3x3<f32> {
    let a = m[0];
    let b = m[1];
    let c = m[2];
    let invdet = 1.0 / dot(a, cross(b, c));
    return mat3x3<f32>(cross(b, c) * invdet, cross(c, a) * invdet, cross(a, b) * invdet);
}

@vertex
fn vs_main(in : VsIn) -> VsOut {
    let model = mat4x4<f32>(in.m0, in.m1, in.m2, in.m3);
    let world = model * vec4<f32>(in.position, 1.0);

    let upper = mat3x3<f32>(model[0].xyz, model[1].xyz, model[2].xyz);
    let nm = normalMatrix(upper);

    var out : VsOut;
    out.worldPos = world.xyz;
    out.normal = normalize(nm * in.normal);
    out.uv = in.uv;
    out.tint = in.tint;
    // Tangents transform by the model's upper-3x3 (not the inverse-transpose);
    // they live in the surface plane and follow shear/scale with positions.
    out.tangent = upper * in.tangent.xyz;
    out.tbnSign = in.tangent.w;
    out.clip = frame.proj * frame.view * world;
    return out;
}

const PI : f32 = 3.14159265359;

fn distributionGGX(N : vec3<f32>, H : vec3<f32>, rough : f32) -> f32 {
    let a = rough * rough;
    let a2 = a * a;
    let NdotH = max(dot(N, H), 0.0);
    let d = NdotH * NdotH * (a2 - 1.0) + 1.0;
    return a2 / max(PI * d * d, 1e-7);
}

fn geometrySchlickGGX(NdotV : f32, rough : f32) -> f32 {
    let r = rough + 1.0;
    let k = (r * r) / 8.0;
    return NdotV / (NdotV * (1.0 - k) + k);
}

fn geometrySmith(N : vec3<f32>, V : vec3<f32>, L : vec3<f32>, rough : f32) -> f32 {
    let ggxV = geometrySchlickGGX(max(dot(N, V), 0.0), rough);
    let ggxL = geometrySchlickGGX(max(dot(N, L), 0.0), rough);
    return ggxV * ggxL;
}

fn fresnelSchlick(cosTheta : f32, F0 : vec3<f32>) -> vec3<f32> {
    return F0 + (vec3<f32>(1.0) - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);
}

// shade accumulates one light's outgoing radiance toward the camera.
fn shade(N : vec3<f32>, V : vec3<f32>, L : vec3<f32>, radiance : vec3<f32>,
         albedo : vec3<f32>, metallic : f32, roughness : f32, F0 : vec3<f32>) -> vec3<f32> {
    let H = normalize(V + L);
    let NdotL = max(dot(N, L), 0.0);
    if (NdotL <= 0.0) {
        return vec3<f32>(0.0);
    }
    let NDF = distributionGGX(N, H, roughness);
    let G = geometrySmith(N, V, L, roughness);
    let F = fresnelSchlick(max(dot(H, V), 0.0), F0);

    let numerator = NDF * G * F;
    let denom = 4.0 * max(dot(N, V), 0.0) * NdotL + 1e-4;
    let specular = numerator / denom;

    let kd = (vec3<f32>(1.0) - F) * (1.0 - metallic);
    return (kd * albedo / PI + specular) * radiance * NdotL;
}

@fragment
fn fs_main(in : VsOut) -> @location(0) vec4<f32> {
    // Per-instance tint multiplies the material albedo (and alpha); default tint
    // is opaque white, leaving the material unchanged.
    var albedo = material.baseColor.rgb * in.tint.rgb;
    var alpha  = material.baseColor.a * in.tint.a;
    // Base-color texture (sRGB; hardware-linearized on sample). Gate on the flag
    // so uniform-only materials (default white texture) are unchanged.
    if (material.flags.x > 0.5) {
        let t = textureSample(baseColorTex, texSampler, in.uv);
        albedo *= t.rgb;
        alpha  *= t.a;
    }
    // Alpha-mask cutoff: alphaMode == MASK (flags.w == 2).
    if (material.flags.w > 1.5 && alpha < material.mr.w) {
        discard;
    }

    var metallic = clamp(material.mr.x, 0.0, 1.0);
    var roughness = clamp(material.mr.y, 0.04, 1.0);
    // Metallic-roughness texture (linear): glTF packs roughness in G, metallic in B.
    if (material.flags.y > 0.5) {
        let mrSample = textureSample(mrTex, texSampler, in.uv);
        roughness = clamp(roughness * mrSample.g, 0.04, 1.0);
        metallic  = clamp(metallic * mrSample.b, 0.0, 1.0);
    }

    var N = normalize(in.normal);
    // Tangent-space normal mapping. Re-orthonormalize the interpolated tangent
    // against N (Gram-Schmidt) and build the bitangent with the stored handedness.
    if (material.flags.z > 0.5) {
        let T = normalize(in.tangent - N * dot(N, in.tangent));
        let B = cross(N, T) * in.tbnSign;
        var tn = textureSample(normalTex, texSampler, in.uv).xyz * 2.0 - 1.0;
        tn = vec3<f32>(tn.xy * material.mr.z, tn.z);
        N = normalize(mat3x3<f32>(T, B, N) * tn);
    }

    let V = normalize(frame.cameraPos.xyz - in.worldPos);
    let F0 = mix(vec3<f32>(0.04), albedo, metallic);

    var Lo = vec3<f32>(0.0);

    // Directional light.
    let dirIntensity = frame.dirDir.w;
    if (dirIntensity > 0.0) {
        let L = normalize(-frame.dirDir.xyz);
        let radiance = frame.dirColor.rgb * dirIntensity;
        Lo += shade(N, V, L, radiance, albedo, metallic, roughness, F0);
    }

    // Point lights.
    let count = u32(frame.pointCount.x);
    for (var i : u32 = 0u; i < count; i = i + 1u) {
        let p = points[i];
        let toLight = p.posRange.xyz - in.worldPos;
        let dist = length(toLight);
        if (dist <= 0.0) {
            continue;
        }
        let L = toLight / dist;
        // Inverse-square falloff with a smooth range window (UE-style).
        let range = max(p.posRange.w, 1e-3);
        let s = clamp(1.0 - pow(dist / range, 4.0), 0.0, 1.0);
        let attenuation = (s * s) / (dist * dist + 1.0);
        let radiance = p.colorIntensity.rgb * p.colorIntensity.w * attenuation;
        Lo += shade(N, V, L, radiance, albedo, metallic, roughness, F0);
    }

    let ambient = frame.ambient.rgb * albedo;
    var color = ambient + Lo + material.emissive.rgb * in.tint.rgb;

    // Reinhard tonemap maps HDR radiance into [0,1] linear. The sRGB swapchain
    // format applies the display gamma encode on write, so the shader must NOT
    // gamma-correct here (doing both washes the image out).
    color = color / (color + vec3<f32>(1.0));

    // Output premultiplied alpha: rgb is scaled by the final alpha so the
    // transparent pipeline's (ONE, ONE_MINUS_SRC_ALPHA) blend is correct. Opaque
    // draws have alpha 1, so this is a no-op for them. alpha was resolved above
    // (material base alpha * tint, optionally * base-color texture alpha).
    return vec4<f32>(color * alpha, alpha);
}
