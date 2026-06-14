package render3d

import (
	"fmt"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// Interp is a keyframe interpolation mode.
type Interp uint8

const (
	InterpLinear Interp = iota // lerp positions/scales, slerp rotations
	InterpStep                 // hold the previous keyframe (no blend)
)

// ChannelPath is the node transform property an animation channel drives.
type ChannelPath uint8

const (
	PathTranslation ChannelPath = iota
	PathRotation
	PathScale
)

// AnimationSampler is a keyframe curve: sorted Times and a flattened Values slice
// of Stride components per keyframe (3 for translation/scale, 4 for a rotation
// quaternion). CUBICSPLINE input is reduced to its keyframe values and sampled as
// LINEAR — a deliberate simplification (the tangents are dropped); STEP and LINEAR
// are exact.
type AnimationSampler struct {
	Times  []float32
	Values []float32
	Stride int
	Interp Interp
}

// AnimationChannel binds a sampler to one node's transform property.
type AnimationChannel struct {
	NodeIndex int
	Path      ChannelPath
	Sampler   int
}

// Animation is one named clip: a set of samplers and the channels that route them
// to nodes, plus the clip's total Duration (the largest keyframe time).
type Animation struct {
	Name     string
	Channels []AnimationChannel
	Samplers []AnimationSampler
	Duration float32
}

// Skin is a skeleton binding: the node indices used as joints, the inverse-bind
// matrix per joint (brings a vertex from model space into each joint's bind-pose
// space), and the skeleton root node index (-1 when unset).
type Skin struct {
	Joints       []int
	InverseBinds []m.Mat4
	Skeleton     int
}

// interval locates the keyframe pair bracketing t and the [0,1] blend fraction
// between them. Clamps at both ends (returns a degenerate pair with frac 0).
func (s *AnimationSampler) interval(t float32) (i0, i1 int, frac float32) {
	n := len(s.Times)
	if n == 0 {
		return 0, 0, 0
	}
	if t <= s.Times[0] {
		return 0, 0, 0
	}
	last := n - 1
	if t >= s.Times[last] {
		return last, last, 0
	}
	// Binary search for the largest index whose time is <= t.
	lo, hi := 0, last
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if s.Times[mid] <= t {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	i0 = lo
	i1 = lo + 1
	if dt := s.Times[i1] - s.Times[i0]; dt > 0 {
		frac = (t - s.Times[i0]) / dt
	}
	return i0, i1, frac
}

func (s *AnimationSampler) vec3At(i int) m.Vec3 {
	o := i * s.Stride
	return m.Vec3{X: s.Values[o], Y: s.Values[o+1], Z: s.Values[o+2]}
}

func (s *AnimationSampler) quatAt(i int) m.Quat {
	o := i * s.Stride
	return m.Quat{X: s.Values[o], Y: s.Values[o+1], Z: s.Values[o+2], W: s.Values[o+3]}
}

// sampleVec3 evaluates a translation/scale sampler at time t.
func (s *AnimationSampler) sampleVec3(t float32) m.Vec3 {
	if len(s.Times) == 0 || s.Stride < 3 {
		return m.Vec3{}
	}
	i0, i1, frac := s.interval(t)
	a := s.vec3At(i0)
	if i0 == i1 || s.Interp == InterpStep {
		return a
	}
	b := s.vec3At(i1)
	return a.Add(b.Sub(a).Scale(frac))
}

// sampleQuat evaluates a rotation sampler at time t (slerp for LINEAR).
func (s *AnimationSampler) sampleQuat(t float32) m.Quat {
	if len(s.Times) == 0 || s.Stride < 4 {
		return m.IdentityQuat()
	}
	i0, i1, frac := s.interval(t)
	a := s.quatAt(i0).Normalize()
	if i0 == i1 || s.Interp == InterpStep {
		return a
	}
	return a.Slerp(s.quatAt(i1), frac)
}

// parseAnimations converts every glTF animation into an Animation. Channels that
// target a nil node or the morph-weights property are skipped (weights are
// unsupported). Pure CPU; safe on the decode goroutine.
func parseAnimations(doc *gltf.Document) ([]Animation, error) {
	if len(doc.Animations) == 0 {
		return nil, nil
	}
	out := make([]Animation, 0, len(doc.Animations))
	for ai, ga := range doc.Animations {
		anim := Animation{Name: ga.Name}
		anim.Samplers = make([]AnimationSampler, len(ga.Samplers))
		for si, gs := range ga.Samplers {
			smp, err := convertSampler(doc, gs)
			if err != nil {
				return nil, fmt.Errorf("render3d: animation %d sampler %d: %w", ai, si, err)
			}
			anim.Samplers[si] = smp
			if len(smp.Times) > 0 {
				if d := smp.Times[len(smp.Times)-1]; d > anim.Duration {
					anim.Duration = d
				}
			}
		}
		for _, gc := range ga.Channels {
			if gc.Target.Node == nil {
				continue
			}
			var path ChannelPath
			switch gc.Target.Path {
			case gltf.TRSTranslation:
				path = PathTranslation
			case gltf.TRSRotation:
				path = PathRotation
			case gltf.TRSScale:
				path = PathScale
			default:
				continue // morph-target weights are not supported
			}
			if gc.Sampler < 0 || gc.Sampler >= len(anim.Samplers) {
				continue
			}
			anim.Channels = append(anim.Channels, AnimationChannel{
				NodeIndex: *gc.Target.Node,
				Path:      path,
				Sampler:   gc.Sampler,
			})
		}
		out = append(out, anim)
	}
	return out, nil
}

// convertSampler reads a glTF sampler's input (times) and output (values).
func convertSampler(doc *gltf.Document, gs *gltf.AnimationSampler) (AnimationSampler, error) {
	var smp AnimationSampler
	tin, err := modeler.ReadAccessor(doc, doc.Accessors[gs.Input], nil)
	if err != nil {
		return smp, err
	}
	times, ok := tin.([]float32)
	if !ok {
		return smp, fmt.Errorf("sampler input is %T, want []float32", tin)
	}
	smp.Times = times

	out, err := modeler.ReadAccessor(doc, doc.Accessors[gs.Output], nil)
	if err != nil {
		return smp, err
	}
	cubic := gs.Interpolation == gltf.InterpolationCubicSpline
	switch v := out.(type) {
	case [][3]float32:
		smp.Stride = 3
		smp.Values = flattenVec3(v, cubic, len(times))
	case [][4]float32:
		smp.Stride = 4
		smp.Values = flattenVec4(v, cubic, len(times))
	default:
		return smp, fmt.Errorf("sampler output is %T, unsupported", out)
	}
	if gs.Interpolation == gltf.InterpolationStep {
		smp.Interp = InterpStep
	} else {
		smp.Interp = InterpLinear // cubic is reduced to its values, sampled linearly
	}
	return smp, nil
}

// flattenVec3 flattens keyframe vec3 output. For CUBICSPLINE the output stores
// (inTangent, value, outTangent) per keyframe; we keep only the value (index 1).
func flattenVec3(v [][3]float32, cubic bool, nKeys int) []float32 {
	out := make([]float32, 0, nKeys*3)
	if cubic {
		for k := 0; k < nKeys; k++ {
			val := v[k*3+1]
			out = append(out, val[0], val[1], val[2])
		}
		return out
	}
	for _, val := range v {
		out = append(out, val[0], val[1], val[2])
	}
	return out
}

func flattenVec4(v [][4]float32, cubic bool, nKeys int) []float32 {
	out := make([]float32, 0, nKeys*4)
	if cubic {
		for k := 0; k < nKeys; k++ {
			val := v[k*3+1]
			out = append(out, val[0], val[1], val[2], val[3])
		}
		return out
	}
	for _, val := range v {
		out = append(out, val[0], val[1], val[2], val[3])
	}
	return out
}

// parseSkins converts every glTF skin, reading inverse-bind matrices (defaulting
// to identity when absent). Pure CPU.
func parseSkins(doc *gltf.Document) ([]Skin, error) {
	if len(doc.Skins) == 0 {
		return nil, nil
	}
	out := make([]Skin, len(doc.Skins))
	for i, gs := range doc.Skins {
		sk := Skin{Joints: append([]int(nil), gs.Joints...), Skeleton: -1}
		if gs.Skeleton != nil {
			sk.Skeleton = *gs.Skeleton
		}
		if gs.InverseBindMatrices != nil {
			mats, err := modeler.ReadInverseBindMatrices(doc, doc.Accessors[*gs.InverseBindMatrices], nil)
			if err != nil {
				return nil, fmt.Errorf("render3d: skin %d inverse binds: %w", i, err)
			}
			sk.InverseBinds = make([]m.Mat4, len(mats))
			for j, mm := range mats {
				sk.InverseBinds[j] = mat4FromGLTF(mm)
			}
		}
		if len(sk.InverseBinds) != len(sk.Joints) {
			// Pad/replace with identity so InverseBinds is always joint-parallel.
			ib := make([]m.Mat4, len(sk.Joints))
			for j := range ib {
				if j < len(sk.InverseBinds) {
					ib[j] = sk.InverseBinds[j]
				} else {
					ib[j] = m.Identity4()
				}
			}
			sk.InverseBinds = ib
		}
		out[i] = sk
	}
	return out, nil
}

// mat4FromGLTF converts a glTF accessor matrix ([col][row], column-major) into the
// engine's column-major m.Mat4 (index c*4+r). Both are column-major, so this is a
// straight transcription.
func mat4FromGLTF(mm [4][4]float32) m.Mat4 {
	var out m.Mat4
	for c := 0; c < 4; c++ {
		for r := 0; r < 4; r++ {
			out[c*4+r] = mm[c][r]
		}
	}
	return out
}

// jointMatrices computes the skinning matrix for each joint:
//
//	joint[j] = inverse(meshWorld) * jointWorld[j] * inverseBind[j]
//
// i.e. the transform that takes a bind-pose vertex (in model space) to its posed
// position, expressed relative to the skinned mesh node so the mesh node's own
// transform cancels (per the glTF skinning spec). out is filled and returned,
// reused if it already has the right length.
func jointMatrices(meshWorld m.Mat4, jointWorld, inverseBind []m.Mat4, out []m.Mat4) []m.Mat4 {
	n := len(jointWorld)
	if len(inverseBind) < n {
		n = len(inverseBind)
	}
	if cap(out) < n {
		out = make([]m.Mat4, n)
	}
	out = out[:n]
	meshInv, ok := meshWorld.Inverse()
	if !ok {
		meshInv = m.Identity4()
	}
	for j := 0; j < n; j++ {
		out[j] = meshInv.Mul(jointWorld[j]).Mul(inverseBind[j])
	}
	return out
}
