package audio

import (
	"math"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// SpatialOptions shapes distance attenuation for PlayAt2D/PlayAt3D voices.
// Zero values mean defaults: MinDist 1, MaxDist 50, linear rolloff.
//
// The m vector package it pairs with is render3d's stdlib-only math leaf, so
// terminal games using 2D spatial audio pull in no GPU dependencies.
type SpatialOptions struct {
	MinDist float64 // full volume inside this radius; 0 → 1
	MaxDist float64 // silent beyond this radius; 0 → 50
	Rolloff Rolloff
}

// Rolloff selects the attenuation curve between MinDist and MaxDist.
type Rolloff int

const (
	// RolloffLinear fades linearly from 1 at MinDist to 0 at MaxDist.
	RolloffLinear Rolloff = iota
	// RolloffInverse follows MinDist/dist (natural 1/r falloff), with a
	// short linear taper to true 0 near MaxDist so sounds never pop out.
	RolloffInverse
)

func (o SpatialOptions) withDefaults() SpatialOptions {
	if o.MinDist <= 0 {
		o.MinDist = 1
	}
	if o.MaxDist <= 0 {
		o.MaxDist = 50
	}
	if o.MaxDist <= o.MinDist {
		o.MaxDist = o.MinDist * 2
	}
	return o
}

// listenerState is the mixer's ear: a position for 2D, plus an orientation
// frame for 3D panning. The defaults match render3d's camera convention —
// at the origin looking down -Z with +Y up, so +X is the right ear.
type listenerState struct {
	x, y, z    float64
	rx, ry, rz float64 // right axis, normalized (3D pan = projection onto it)
}

func defaultListener() listenerState {
	return listenerState{rx: 1}
}

// gainPan computes the voice's distance attenuation and stereo pan from the
// current listener. Pure math; caller holds the mixer lock.
func (ls *listenerState) gainPan(v *voice) (gain, pan float64) {
	dx, dy, dz := v.px-ls.x, v.py-ls.y, v.pz-ls.z
	var dist, lateral float64
	if v.is3D {
		dist = math.Sqrt(dx*dx + dy*dy + dz*dz)
		lateral = dx*ls.rx + dy*ls.ry + dz*ls.rz
	} else {
		dist = math.Hypot(dx, dy)
		lateral = dx
	}
	return attenuate(dist, v.spOpts), panFromLateral(lateral, dist, v.spOpts)
}

func attenuate(dist float64, o SpatialOptions) float64 {
	switch o.Rolloff {
	case RolloffInverse:
		g := o.MinDist / max(dist, o.MinDist)
		// Taper the tail so the sound reaches true silence at MaxDist
		// instead of popping out when a voice crosses it.
		taper := min((o.MaxDist-dist)/(0.1*o.MaxDist), 1)
		return g * min(max(taper, 0), 1)
	default: // RolloffLinear
		return min(max(1-(dist-o.MinDist)/(o.MaxDist-o.MinDist), 0), 1)
	}
}

// panFromLateral maps the source's sideways offset to a pan position. The
// proximity factor relaxes panning inside MinDist so a source sliding past
// the listener's head sweeps smoothly instead of flipping hard left-to-right.
func panFromLateral(lateral, dist float64, o SpatialOptions) float64 {
	if dist < 1e-9 {
		return 0
	}
	proximity := min(dist/o.MinDist, 1)
	return min(max(lateral/dist*proximity, -1), 1)
}

// ── listener ────────────────────────────────────────────────────────────────

// SetListener2D places the 2D listener (the player / camera center). All
// spatial voices retarget their gain and pan with the usual short ramp.
func (a *Audio) SetListener2D(x, y float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listener.x, a.listener.y = x, y
	a.retargetSpatial()
}

// SetListener3D places and orients the 3D listener. forward is the look
// direction (for a render3d camera: Target − Position), up the world up.
func (a *Audio) SetListener3D(pos, forward, up m.Vec3) {
	right := forward.Cross(up).Normalize()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listener.x, a.listener.y, a.listener.z = float64(pos.X), float64(pos.Y), float64(pos.Z)
	if right.LengthSq() > 0 {
		a.listener.rx, a.listener.ry, a.listener.rz = float64(right.X), float64(right.Y), float64(right.Z)
	}
	a.retargetSpatial()
}

// retargetSpatial refreshes gain/pan targets of every spatial voice after the
// listener moved. Caller holds a.mu.
func (a *Audio) retargetSpatial() {
	rf := a.rampFrames()
	for i := range a.voices {
		v := &a.voices[i]
		if !v.active || !v.spatial {
			continue
		}
		gain, pan := a.listener.gainPan(v)
		v.spGain = gain
		v.vol.set(v.userVol*gain, rf)
		v.setPan(pan, rf)
	}
}

// ── spatial playback ────────────────────────────────────────────────────────

// PlayAt2D starts ref positioned in the 2D world: pan from the X offset to
// the listener, attenuation from euclidean distance. opt.Pan is ignored.
func (a *Audio) PlayAt2D(ref string, x, y float64, opt PlayOptions, sp SpatialOptions) Handle {
	buf, sv, ok := a.resolve(ref)
	if !ok {
		return NoHandle
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startVoice(ref, buf, sv, opt, false, spatialState{enabled: true, px: x, py: y, opts: sp})
}

// PlayAt3D starts ref positioned in 3D world space relative to the listener
// set by SetListener3D. opt.Pan is ignored.
func (a *Audio) PlayAt3D(ref string, pos m.Vec3, opt PlayOptions, sp SpatialOptions) Handle {
	buf, sv, ok := a.resolve(ref)
	if !ok {
		return NoHandle
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startVoice(ref, buf, sv, opt, false, spatialState{
		enabled: true, is3D: true,
		px: float64(pos.X), py: float64(pos.Y), pz: float64(pos.Z),
		opts: sp,
	})
}

// SetVoicePos2D moves a spatial voice; gain and pan retarget with a ramp.
func (a *Audio) SetVoicePos2D(h Handle, x, y float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.moveVoice(h, x, y, 0)
}

// SetVoicePos3D moves a spatial voice; gain and pan retarget with a ramp.
func (a *Audio) SetVoicePos3D(h Handle, pos m.Vec3) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.moveVoice(h, float64(pos.X), float64(pos.Y), float64(pos.Z))
}

func (a *Audio) moveVoice(h Handle, x, y, z float64) {
	v := a.voiceFor(h)
	if v == nil || !v.spatial {
		return
	}
	v.px, v.py, v.pz = x, y, z
	gain, pan := a.listener.gainPan(v)
	v.spGain = gain
	rf := a.rampFrames()
	v.vol.set(v.userVol*gain, rf)
	v.setPan(pan, rf)
}
