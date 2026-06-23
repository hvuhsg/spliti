package systems

import (
	"math"
	"math/rand"
	"time"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/audio"
)

const sndRate = 48000

// LoadSounds synthesizes the game's sound effects and starts the ambient drone,
// so the FPS ships audible with zero binary assets (drop WAV/OGG files in later
// via Registry.LoadFS to upgrade). Runs once at startup.
func LoadSounds(c *app.Ctx) {
	reg := app.GetResource[audio.Registry](c)
	if reg == nil {
		return
	}
	rng := rand.New(rand.NewSource(1))
	sounds := map[string][]float32{
		"gunshot": gunshot(rng),
		"hit":     blipSnd(1200, 60*time.Millisecond),
		"kill":    sweepSnd(600, 90, 320*time.Millisecond),
		"hurt":    sweepSnd(320, 120, 260*time.Millisecond),
		"ambient": ambientLoop(),
	}
	for ref, pcm := range sounds {
		_ = reg.LoadPCM(ref, pcm, 1, sndRate)
	}
	if au := app.GetResource[audio.Audio](c); au != nil {
		au.PlayMusic("ambient", audio.MusicOptions{Volume: 0.35})
	}
}

// playSound plays a one-shot effect, a no-op when the audio plugin is absent.
func playSound(c *app.Ctx, ref string) {
	if au := app.GetResource[audio.Audio](c); au != nil {
		au.Play(ref)
	}
}

// gunshot is a short filtered-noise burst with a fast exponential decay — a
// punchy crack.
func gunshot(rng *rand.Rand) []float32 {
	dur := 120 * time.Millisecond
	n := int(dur.Seconds() * sndRate)
	pcm := make([]float32, n)
	var lp float64 // one-pole low-pass to take the harsh edge off white noise
	for i := range pcm {
		t := float64(i) / sndRate
		env := math.Exp(-26 * t / dur.Seconds())
		noise := rng.Float64()*2 - 1
		lp += 0.35 * (noise - lp)
		pcm[i] = float32(0.6 * env * lp)
	}
	return pcm
}

// blipSnd is a short sine burst with exponential decay.
func blipSnd(freq float64, dur time.Duration) []float32 {
	n := int(dur.Seconds() * sndRate)
	pcm := make([]float32, n)
	for i := range pcm {
		t := float64(i) / sndRate
		env := math.Exp(-7 * t / dur.Seconds())
		pcm[i] = float32(0.4 * env * math.Sin(2*math.Pi*freq*t))
	}
	return pcm
}

// sweepSnd glides a sine from one pitch to another while fading out.
func sweepSnd(from, to float64, dur time.Duration) []float32 {
	n := int(dur.Seconds() * sndRate)
	pcm := make([]float32, n)
	phase := 0.0
	for i := range pcm {
		frac := float64(i) / float64(n)
		freq := from * math.Pow(to/from, frac)
		phase += 2 * math.Pi * freq / sndRate
		pcm[i] = float32(0.45 * (1 - frac) * math.Sin(phase))
	}
	return pcm
}

// ambientLoop renders a slow, seamless low drone for tension.
func ambientLoop() []float32 {
	dur := 4.0 // seconds; loops cleanly because the tones are integer cycles
	n := int(dur * sndRate)
	pcm := make([]float32, n)
	for i := range pcm {
		t := float64(i) / sndRate
		lfo := 0.5 + 0.5*math.Sin(2*math.Pi*0.25*t) // slow swell
		v := 0.5*math.Sin(2*math.Pi*55*t) + 0.3*math.Sin(2*math.Pi*82.5*t)
		pcm[i] = float32(0.18 * lfo * v)
	}
	return pcm
}
