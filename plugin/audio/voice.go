package audio

import "math"

// ramp is a per-frame linear envelope: cur moves toward target by per each
// frame. Every audible gain change in the mixer goes through one of these so
// no instantaneous step ever reaches the output (the click-killer invariant).
type ramp struct {
	cur, target, per float64
}

// set retargets the ramp to land on target after frames ticks.
func (r *ramp) set(target float64, frames int) {
	r.target = target
	if frames <= 0 {
		r.cur = target
		r.per = 0
		return
	}
	r.per = math.Abs(target-r.cur) / float64(frames)
}

// jump snaps the ramp to v with no transition (voice start only).
func (r *ramp) jump(v float64) { r.cur, r.target, r.per = v, v, 0 }

// tick returns the current value, then advances one frame toward target.
func (r *ramp) tick() float64 {
	v := r.cur
	r.advance(1)
	return v
}

// advance moves n frames toward target.
func (r *ramp) advance(n int) {
	if r.cur == r.target {
		return
	}
	d := r.per * float64(n)
	if r.cur < r.target {
		r.cur = min(r.cur+d, r.target)
	} else {
		r.cur = max(r.cur-d, r.target)
	}
}

type voiceMode uint8

const (
	modePlaying voiceMode = iota
	modePausing           // ctl ramping to 0; halts (modePaused) when it lands
	modePaused            // not rendered, position frozen
	modeStopping          // ctl ramping to 0; freed when it lands
)

// frameSource yields successive source frames for a streaming voice. pull
// and rewind run under the mixer lock on the device goroutine, so
// implementations must not block or allocate in steady state. pull returns
// ok=false at EOF; a looping voice then calls rewind and pulls again.
type frameSource interface {
	pull() (l, r float32, ok bool)
	rewind() bool
	sampleRate() int
	channels() int
}

// streamVoice is the resample state of a voice backed by a frameSource: it
// lerps between the two most recent source frames as the fractional position
// advances, mirroring the indexed lerp buffer voices do.
type streamVoice struct {
	src                        frameSource
	looping                    bool
	frac                       float64
	prevL, prevR, nextL, nextR float32
	primed                     bool
	eof                        bool
}

type voice struct {
	active    bool
	gen       uint32
	ref       string
	bus       Bus
	protected bool // music: never stolen

	buf    *Buffer      // exactly one of buf / stream is non-nil
	stream *streamVoice

	pos  float64 // buffer voices: fractional source-frame position
	step float64 // source frames advanced per output frame

	looping  bool
	mode     voiceMode
	fadeFree bool // free the voice when fade lands on 0 (StopFade / crossfade)

	// Raw control values; vol ramps toward userVol*spGain.
	userVol float64
	spGain  float64

	vol        ramp // user volume × spatial attenuation (8 ms ramp)
	fade       ramp // long musical fades (FadeIn/FadeTo/StopFade/crossfade)
	ctl        ramp // play/pause/stop envelope (8 ms ramp)
	panL, panR ramp // equal-power pan gains (8 ms ramp)

	// Spatial state (see spatial.go).
	spatial    bool
	is3D       bool
	px, py, pz float64
	spOpts     SpatialOptions

	startFrame uint64 // mixer frame at Play, for steal-age tiebreak
}

// setPan retargets the pan ramps for pan p in [-1, 1] using the equal-power
// law: constant perceived power across the sweep, 1/√2 per side at center.
func (v *voice) setPan(p float64, frames int) {
	l, r := panGains(p)
	v.panL.set(l, frames)
	v.panR.set(r, frames)
}

func panGains(p float64) (l, r float64) {
	p = min(max(p, -1), 1)
	theta := (p + 1) * math.Pi / 4
	return math.Cos(theta), math.Sin(theta)
}

// render mixes up to frames stereo frames of this voice into dst
// (interleaved, accumulate-add) and reports whether the voice finished.
// bus and master are by-value ramp snapshots: every voice on the bus advances
// its own copy identically, and the mixer advances the shared ramps once.
func (v *voice) render(dst []float32, frames int, bus, master ramp) (done bool) {
	if v.mode == modePaused {
		return false
	}
	stereoSrc := false
	if v.buf != nil {
		stereoSrc = v.buf.channels == 2
	} else if v.stream != nil {
		stereoSrc = v.stream.src.channels() == 2
	}
	for i := 0; i < frames; i++ {
		ctl := v.ctl.tick()
		g := v.vol.tick() * v.fade.tick() * ctl * bus.tick() * master.tick()
		gl, gr := v.panL.tick(), v.panR.tick()
		if stereoSrc {
			// Pan acts as balance on stereo sources: center keeps both
			// channels at unity instead of the mono law's 1/√2.
			gl = min(gl*math.Sqrt2, 1)
			gr = min(gr*math.Sqrt2, 1)
		}

		var l, r float32
		var ok bool
		if v.buf != nil {
			l, r, ok = v.bufferSample()
		} else {
			l, r, ok = v.streamSample()
		}
		if !ok {
			return true // source exhausted (non-looping EOF)
		}
		dst[2*i] += l * float32(g*gl)
		dst[2*i+1] += r * float32(g*gr)

		if v.ctl.cur == 0 {
			switch v.mode {
			case modePausing:
				v.mode = modePaused
				return false
			case modeStopping:
				return true
			}
		}
	}
	return v.fadeFree && v.fade.cur == 0 && v.fade.target == 0
}

// bufferSample lerps the buffer at the current fractional position and
// advances it. The lerp neighbor at the loop seam wraps to the loop start so
// looping playback is continuous across the boundary.
func (v *voice) bufferSample() (l, r float32, ok bool) {
	b := v.buf
	n := b.frames
	i := int(v.pos)
	if i >= n {
		if !v.looping {
			return 0, 0, false
		}
		v.pos -= float64(n)
		i = int(v.pos)
	}
	frac := float32(v.pos - float64(i))
	j := i + 1
	if j >= n {
		if v.looping {
			j = 0
		} else {
			j = i
		}
	}
	if b.channels == 1 {
		s := b.pcm[i] + (b.pcm[j]-b.pcm[i])*frac
		l, r = s, s
	} else {
		l = b.pcm[2*i] + (b.pcm[2*j]-b.pcm[2*i])*frac
		r = b.pcm[2*i+1] + (b.pcm[2*j+1]-b.pcm[2*i+1])*frac
	}
	v.pos += v.step
	if v.looping && v.pos >= float64(n) {
		v.pos -= float64(n)
	}
	return l, r, true
}

// streamSample lerps between the two most recent source frames and pulls new
// ones as the fractional position crosses frame boundaries.
func (v *voice) streamSample() (l, r float32, ok bool) {
	sv := v.stream
	if !sv.primed {
		if !sv.pull() {
			return 0, 0, false
		}
		sv.prevL, sv.prevR = sv.nextL, sv.nextR
		if !sv.pull() {
			sv.eof = true
		}
		sv.primed = true
	}
	if sv.eof && sv.frac >= 1 {
		return 0, 0, false
	}
	f := float32(sv.frac)
	l = sv.prevL + (sv.nextL-sv.prevL)*f
	r = sv.prevR + (sv.nextR-sv.prevR)*f
	sv.frac += v.step
	for sv.frac >= 1 && !sv.eof {
		sv.frac--
		sv.prevL, sv.prevR = sv.nextL, sv.nextR
		if !sv.pull() {
			sv.eof = true
		}
	}
	return l, r, true
}

// pull fetches the next source frame into nextL/nextR, rewinding the source
// at EOF when looping.
func (sv *streamVoice) pull() bool {
	l, r, ok := sv.src.pull()
	if !ok && sv.looping && sv.src.rewind() {
		l, r, ok = sv.src.pull()
	}
	if !ok {
		// Hold a zero frame so the final lerp ramps to silence.
		sv.nextL, sv.nextR = 0, 0
		return false
	}
	sv.nextL, sv.nextR = l, r
	return true
}
