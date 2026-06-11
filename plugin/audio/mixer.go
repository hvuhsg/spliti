package audio

import (
	"encoding/binary"
	"math"
	"sync"
	"time"
)

const (
	defaultRate   = 48000
	defaultVoices = 32

	// rampDur is the click-killer: every audible gain step is spread over
	// this long so no instantaneous discontinuity reaches the output.
	rampDur = 8 * time.Millisecond

	// maxChunkFrames bounds one mix pass so Read can satisfy any buffer size
	// from a fixed scratch allocation.
	maxChunkFrames = 2048

	bytesPerFrame = 8 // stereo float32
)

// Audio is the mixer resource. Retrieve it with app.GetResource[audio.Audio].
//
// It implements io.Reader — the output device pulls interleaved stereo
// float32 LE frames from Read on its own goroutine — and every control method
// is safe to call concurrently from game systems. Control calls are O(1)
// field writes under a short mutex; nothing under the mutex blocks.
type Audio struct {
	mu     sync.Mutex
	rate   int
	voices []voice
	buses  map[Bus]*ramp
	mixBuf []float32
	reg    *Registry

	finished []Finished
	frame    uint64 // total frames mixed, for steal-age tiebreaks
	music    Handle
	listener listenerState

	logf   func(format string, args ...any)
	warned map[string]struct{}
}

// NewAudio builds a standalone mixer with its own Registry and no device.
// Plugin.Build calls this and wires the device; tests drive Read/Advance
// directly.
func NewAudio(sampleRate, maxVoices int) *Audio {
	if sampleRate <= 0 {
		sampleRate = defaultRate
	}
	if maxVoices <= 0 {
		maxVoices = defaultVoices
	}
	a := &Audio{
		rate:   sampleRate,
		voices: make([]voice, maxVoices),
		buses:  make(map[Bus]*ramp),
		mixBuf: make([]float32, maxChunkFrames*2),
		warned: make(map[string]struct{}),
		logf:   func(string, ...any) {},
	}
	a.reg = newRegistry(sampleRate)
	a.listener = defaultListener()
	for _, b := range []Bus{BusMaster, BusSFX, BusMusic} {
		a.buses[b] = &ramp{cur: 1, target: 1}
	}
	return a
}

// Registry returns the asset registry this mixer plays from. Plugin inserts
// the same object as its own resource.
func (a *Audio) Registry() *Registry { return a.reg }

// SampleRate returns the mixer output rate in Hz.
func (a *Audio) SampleRate() int { return a.rate }

func (a *Audio) rampFrames() int {
	return int(rampDur.Seconds() * float64(a.rate))
}

func (a *Audio) framesOf(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(d.Seconds() * float64(a.rate))
}

// ── playback ────────────────────────────────────────────────────────────────

// Play starts ref on the SFX bus with default options.
func (a *Audio) Play(ref string) Handle { return a.PlayWith(ref, PlayOptions{}) }

// PlayWith starts ref with the given options. Unknown refs log a warning
// (once per ref) and return NoHandle, which every control method ignores.
func (a *Audio) PlayWith(ref string, opt PlayOptions) Handle {
	buf, sv, ok := a.resolve(ref)
	if !ok {
		return NoHandle
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startVoice(ref, buf, sv, opt, false, spatialState{})
}

// resolve looks ref up and, for streamed entries, builds the decoder — on the
// game goroutine, so decoder setup never runs under the mix lock.
func (a *Audio) resolve(ref string) (*Buffer, *streamVoice, bool) {
	ent := a.reg.lookup(ref)
	if ent == nil {
		a.warnOnce(ref, "audio: unknown ref %q", ref)
		return nil, nil, false
	}
	if ent.buf != nil {
		return ent.buf, nil, true
	}
	src, err := newStreamSource(ent.stream)
	if err != nil {
		a.warnOnce(ref, "audio: ref %q: %v", ref, err)
		return nil, nil, false
	}
	return nil, &streamVoice{src: src}, true
}

type spatialState struct {
	enabled    bool
	is3D       bool
	px, py, pz float64
	opts       SpatialOptions
}

// startVoice allocates a slot and initializes it. Caller holds a.mu.
func (a *Audio) startVoice(ref string, buf *Buffer, sv *streamVoice, opt PlayOptions, protected bool, sp spatialState) Handle {
	idx := a.allocVoice(protected)
	if idx < 0 {
		return NoHandle
	}
	v := &a.voices[idx]
	gen := v.gen
	*v = voice{active: true, gen: gen, ref: ref, protected: protected}

	v.bus = opt.Bus
	if v.bus == "" {
		v.bus = BusSFX
	}
	a.ensureBus(v.bus)

	v.buf = buf
	v.stream = sv
	v.looping = opt.Loop
	if sv != nil {
		sv.looping = opt.Loop
	}

	pitch := opt.Pitch
	if pitch == 0 {
		pitch = 1
	}
	v.step = pitch
	if sv != nil {
		v.step = pitch * float64(sv.src.sampleRate()) / float64(a.rate)
	}

	v.userVol = opt.Volume
	if v.userVol == 0 {
		v.userVol = 1
	}
	v.userVol = max(v.userVol, 0)

	v.spGain = 1
	pan := opt.Pan
	if sp.enabled {
		v.spatial = true
		v.is3D = sp.is3D
		v.px, v.py, v.pz = sp.px, sp.py, sp.pz
		v.spOpts = sp.opts.withDefaults()
		v.spGain, pan = a.listener.gainPan(v)
	}

	// Voices start at their target levels — no ramp-in (FadeIn is the
	// explicit way to ease a sound in).
	v.vol.jump(v.userVol * v.spGain)
	v.fade.jump(1)
	if opt.FadeIn > 0 {
		v.fade.jump(0)
		v.fade.set(1, a.framesOf(opt.FadeIn))
	}
	v.ctl.jump(1)
	l, r := panGains(pan)
	v.panL.jump(l)
	v.panR.jump(r)
	if opt.Paused {
		v.mode = modePaused
		v.ctl.jump(0)
	}
	v.startFrame = a.frame
	return makeHandle(idx, gen)
}

// allocVoice returns a free slot, stealing the quietest non-protected voice
// when the pool is full. Returns -1 only when every slot is protected.
func (a *Audio) allocVoice(forProtected bool) int {
	best, bestGain := -1, math.Inf(1)
	for i := range a.voices {
		v := &a.voices[i]
		if !v.active {
			if v.gen == 0 {
				v.gen = 1 // generation 0 would collide with NoHandle
			}
			return i
		}
		if v.protected {
			continue
		}
		g := v.vol.cur * v.fade.cur * v.ctl.cur
		if g < bestGain || (g == bestGain && best >= 0 && v.startFrame < a.voices[best].startFrame) {
			best, bestGain = i, g
		}
	}
	if best < 0 {
		a.logf("audio: voice pool exhausted (all %d voices protected)", len(a.voices))
		return -1
	}
	v := &a.voices[best]
	v.active = false
	v.gen++
	if v.gen == 0 {
		v.gen = 1
	}
	_ = forProtected
	return best
}

// ── voice control ───────────────────────────────────────────────────────────

// voiceFor resolves a handle to its live voice, or nil if the handle is
// stale. Caller holds a.mu.
func (a *Audio) voiceFor(h Handle) *voice {
	idx, gen := h.index(), h.generation()
	if gen == 0 || idx >= len(a.voices) {
		return nil
	}
	v := &a.voices[idx]
	if !v.active || v.gen != gen {
		return nil
	}
	return v
}

// Stop ends the voice with a short ramp-out (no click). Safe on stale handles.
func (a *Audio) Stop(h Handle) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v := a.voiceFor(h); v != nil {
		v.mode = modeStopping
		v.ctl.set(0, a.rampFrames())
	}
}

// StopFade fades the voice out over d, then frees it. d <= 0 acts like Stop.
func (a *Audio) StopFade(h Handle, d time.Duration) {
	if d <= 0 {
		a.Stop(h)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if v := a.voiceFor(h); v != nil {
		v.fadeFree = true
		v.fade.set(0, a.framesOf(d))
	}
}

// Pause ramps the voice out and freezes its position; Resume continues it.
func (a *Audio) Pause(h Handle) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v := a.voiceFor(h); v != nil && v.mode != modeStopping {
		v.mode = modePausing
		v.ctl.set(0, a.rampFrames())
	}
}

// Resume continues a paused voice with a ramp-in.
func (a *Audio) Resume(h Handle) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v := a.voiceFor(h); v != nil && (v.mode == modePausing || v.mode == modePaused) {
		v.mode = modePlaying
		v.ctl.set(1, a.rampFrames())
	}
}

// IsPlaying reports whether the handle's voice is still alive (paused voices
// included). Never gate networked game logic on this — audio is outside the
// determinism contract.
func (a *Audio) IsPlaying(h Handle) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.voiceFor(h) != nil
}

// SetVolume retargets the voice's volume (ramped; 0 is genuinely silent here,
// unlike PlayOptions.Volume).
func (a *Audio) SetVolume(h Handle, vol float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v := a.voiceFor(h); v != nil {
		v.userVol = max(vol, 0)
		v.vol.set(v.userVol*v.spGain, a.rampFrames())
	}
}

// SetPan retargets the voice's stereo pan in [-1, 1] (ramped).
func (a *Audio) SetPan(h Handle, pan float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v := a.voiceFor(h); v != nil {
		v.setPan(pan, a.rampFrames())
	}
}

// SetPitch retargets the playback-rate multiplier (takes effect immediately;
// pitch is a position step, not a gain, so it needs no de-click ramp).
func (a *Audio) SetPitch(h Handle, pitch float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v := a.voiceFor(h); v != nil && pitch > 0 {
		v.step = pitch
		if v.stream != nil {
			v.step = pitch * float64(v.stream.src.sampleRate()) / float64(a.rate)
		}
	}
}

// FadeTo ramps the voice's fade envelope to vol (clamped to [0,1]) over d.
// Unlike StopFade, the voice keeps playing when it lands on 0.
func (a *Audio) FadeTo(h Handle, vol float64, d time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v := a.voiceFor(h); v != nil {
		v.fade.set(min(max(vol, 0), 1), a.framesOf(d))
	}
}

// StopAll stops every voice, music included, with the usual short ramp.
func (a *Audio) StopAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.voices {
		v := &a.voices[i]
		if v.active {
			v.mode = modeStopping
			v.ctl.set(0, a.rampFrames())
		}
	}
	a.music = NoHandle
}

// ── buses ───────────────────────────────────────────────────────────────────

// SetBusVolume retargets a bus volume (ramped). Unknown buses are created on
// first use.
func (a *Audio) SetBusVolume(b Bus, vol float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureBus(b).set(max(vol, 0), a.rampFrames())
}

// BusVolume returns the bus's target volume.
func (a *Audio) BusVolume(b Bus) float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ensureBus(b).target
}

func (a *Audio) ensureBus(b Bus) *ramp {
	if r, ok := a.buses[b]; ok {
		return r
	}
	r := &ramp{cur: 1, target: 1}
	a.buses[b] = r
	return r
}

// ── mixing ──────────────────────────────────────────────────────────────────

// Read mixes len(p)/8 stereo float32 LE frames into p. It is the io.Reader
// the output device pulls from; it never blocks beyond the short control
// mutex and always satisfies the full request.
func (a *Audio) Read(p []byte) (int, error) {
	frames := len(p) / bytesPerFrame
	a.mu.Lock()
	defer a.mu.Unlock()
	for total := 0; total < frames; {
		n := min(frames-total, maxChunkFrames)
		a.mixChunk(n)
		for i, s := range a.mixBuf[:n*2] {
			if s > 1 {
				s = 1
			} else if s < -1 {
				s = -1
			}
			binary.LittleEndian.PutUint32(p[(total*2+i)*4:], math.Float32bits(s))
		}
		total += n
	}
	return frames * bytesPerFrame, nil
}

// Advance mixes and discards d worth of frames. The plugin's pump system
// drives this when no device is available (the null sink), so loops, fades,
// IsPlaying, and Finished behave identically headless; tests use it to move
// time forward.
func (a *Audio) Advance(d time.Duration) {
	frames := a.framesOf(d)
	a.mu.Lock()
	defer a.mu.Unlock()
	for frames > 0 {
		n := min(frames, maxChunkFrames)
		a.mixChunk(n)
		frames -= n
	}
}

// mixChunk renders n frames of every active voice into mixBuf (accumulating;
// not yet clamped). Caller holds a.mu.
func (a *Audio) mixChunk(n int) {
	clear(a.mixBuf[:n*2])
	master := a.buses[BusMaster]
	for i := range a.voices {
		v := &a.voices[i]
		if !v.active {
			continue
		}
		// A voice stopped while paused never renders again; free it here.
		if v.mode == modeStopping && v.ctl.cur == 0 && v.ctl.target == 0 {
			a.free(i)
			continue
		}
		bus := a.buses[v.bus]
		if v.bus == BusMaster {
			bus = &ramp{cur: 1, target: 1} // don't apply master twice
		}
		// Pass by-value snapshots: each voice advances its own copies
		// identically; the shared ramps advance once below.
		if v.render(a.mixBuf[:n*2], n, *bus, *master) {
			a.free(i)
		}
	}
	for _, r := range a.buses {
		r.advance(n)
	}
	a.frame += uint64(n)
}

// free retires the voice in slot i and queues its Finished event.
// Caller holds a.mu.
func (a *Audio) free(i int) {
	v := &a.voices[i]
	a.finished = append(a.finished, Finished{Handle: makeHandle(i, v.gen), Ref: v.ref})
	if a.music == makeHandle(i, v.gen) {
		a.music = NoHandle
	}
	v.active = false
	v.buf = nil
	v.stream = nil
	v.gen++
	if v.gen == 0 {
		v.gen = 1
	}
}

// drainFinished moves the queued Finished events out of the mixer.
func (a *Audio) drainFinished(into []Finished) []Finished {
	a.mu.Lock()
	defer a.mu.Unlock()
	into = append(into, a.finished...)
	a.finished = a.finished[:0]
	return into
}

func (a *Audio) warnOnce(key, format string, args ...any) {
	a.mu.Lock()
	_, seen := a.warned[key]
	a.warned[key] = struct{}{}
	a.mu.Unlock()
	if !seen {
		a.logf(format, args...)
	}
}
