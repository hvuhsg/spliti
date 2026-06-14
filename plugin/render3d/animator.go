package render3d

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/time"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Animator plays one of a Model's animation clips. SpawnModel attaches it to the
// model root for any model that ships animations; game code can switch Clip,
// pause via Playing, or scrub Time directly. Speed scales playback (default 1).
type Animator struct {
	Model   *Model // source clips (and node count); read-only
	Clip    int    // index into Model.Animations
	Time    float32
	Speed   float32
	Loop    bool
	Playing bool
}

func (a *Animator) speedOr1() float32 {
	if a.Speed == 0 {
		return 1
	}
	return a.Speed
}

// AnimationRig maps a Model node index to the entity SpawnModel created for it, so
// the animator writes sampled transforms by index instead of scanning the world.
// Lives on the same (root) entity as the Animator.
type AnimationRig struct {
	NodeEntities []ecs.Entity
}

const animLabel = "__render3d_anim"

// advanceAnimations samples each Animator's active clip at its current time and
// writes the result into the rig's node Transform3Ds. Runs in PostUpdate before
// transform propagation, so the world matrices handed to the renderer are current.
func advanceAnimations(c *app.Ctx) {
	var dt float32
	if t := app.GetResource[time.Time](c); t != nil {
		dt = float32(t.Delta().Seconds())
	}
	world := c.World()
	xform := generic.NewMap[Transform3D](world)
	rigs := generic.NewMap[AnimationRig](world)

	app.Query1[Animator](c, func(e ecs.Entity, a *Animator) {
		if a.Model == nil || a.Clip < 0 || a.Clip >= len(a.Model.Animations) || !rigs.Has(e) {
			return
		}
		rig := rigs.Get(e)
		clip := &a.Model.Animations[a.Clip]
		if a.Playing {
			a.Time += dt * a.speedOr1()
		}
		a.Time = wrapClipTime(a.Time, clip.Duration, a.Loop)
		applyClip(clip, a.Time, rig.NodeEntities, &xform)
	})
}

// applyClip writes one clip's sampled channel values at time t into the node
// transforms. Pure aside from the transform writes; the sampling itself is in
// animation.go and unit-tested independently.
func applyClip(clip *Animation, t float32, nodes []ecs.Entity, xform *generic.Map[Transform3D]) {
	for _, ch := range clip.Channels {
		if ch.NodeIndex < 0 || ch.NodeIndex >= len(nodes) || ch.Sampler < 0 || ch.Sampler >= len(clip.Samplers) {
			continue
		}
		e := nodes[ch.NodeIndex]
		if !xform.Has(e) {
			continue
		}
		tr := xform.Get(e)
		smp := &clip.Samplers[ch.Sampler]
		switch ch.Path {
		case PathTranslation:
			tr.Translation = smp.sampleVec3(t)
		case PathRotation:
			tr.Rotation = smp.sampleQuat(t)
		case PathScale:
			tr.Scale = smp.sampleVec3(t)
		}
	}
}

// wrapClipTime keeps the playhead within [0,dur]: looping wraps modulo dur,
// otherwise it clamps to the ends.
func wrapClipTime(t, dur float32, loop bool) float32 {
	if dur <= 0 {
		return 0
	}
	if loop {
		t = float32(math.Mod(float64(t), float64(dur)))
		if t < 0 {
			t += dur
		}
		return t
	}
	switch {
	case t < 0:
		return 0
	case t > dur:
		return dur
	default:
		return t
	}
}
