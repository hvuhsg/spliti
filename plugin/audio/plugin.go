package audio

import (
	"log"
	"runtime"
	gotime "time"

	oto "github.com/ebitengine/oto/v3"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
)

// Plugin installs the Audio mixer resource, the asset Registry resource, the
// output device, and a per-frame pump system (label "__spliti_audio_pump",
// schedule.First — events sent there are visible for the whole frame, like
// the input plugin's) that publishes Finished events.
//
// When no audio device can be opened (CI, headless ssh) the plugin logs a
// warning and runs the mixer against a silent null sink driven by the pump,
// so playback, loops, fades, IsPlaying, and Finished behave identically —
// set Required to panic instead.
//
// Platform notes: macOS and Windows need no cgo; Linux needs cgo and ALSA
// headers (libasound2-dev) like the GPU plugins; GOOS=js plays through Web
// Audio, which browsers keep muted until the first user gesture (input is a
// gesture, so games that start sound from a key press are unaffected).
type Plugin struct {
	SampleRate int                // output rate in Hz; 0 = 48000
	MaxVoices  int                // simultaneous voices; 0 = 32
	BufferSize gotime.Duration    // device buffer; 0 = 50ms native, 200ms js
	Required   bool               // panic on device failure instead of going silent
	Logf       func(format string, args ...any) // warnings; nil = log.Printf
}

// Build implements app.Plugin.
func (p Plugin) Build(a *app.App) {
	if app.HasResource[Audio](a) {
		panic("audio: Plugin added twice (one mixer/device per app)")
	}

	logf := p.Logf
	if logf == nil {
		logf = log.Printf
	}

	mix := NewAudio(p.SampleRate, p.MaxVoices)
	mix.logf = logf
	app.InsertResource(a, mix)
	app.InsertResource(a, mix.reg)

	bufSize := p.BufferSize
	if bufSize == 0 {
		// js pulls audio between rAF frames only; a long frame starves a
		// small buffer, so trade latency for safety there.
		if runtime.GOOS == "js" {
			bufSize = 200 * gotime.Millisecond
		} else {
			bufSize = 50 * gotime.Millisecond
		}
	}

	nullSink := false
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   mix.rate,
		ChannelCount: 2,
		Format:       oto.FormatFloat32LE,
		BufferSize:   bufSize,
	})
	if err != nil {
		if p.Required {
			panic("audio: open device: " + err.Error())
		}
		logf("audio: device unavailable (%v); continuing with silent output", err)
		nullSink = true
	} else {
		player := ctx.NewPlayer(mix)
		// oto players pre-read 0.5s from their source by default; everything
		// in that buffer is mixed before a Play call, so it's pure added
		// latency. Match it to the device buffer instead (total output
		// latency ≈ 2×BufferSize).
		player.SetBufferSize(mix.framesOf(bufSize) * bytesPerFrame)
		// Don't wait on ready: on browsers it fires only after the first
		// user gesture, and oto buffers until then.
		_ = ready
		player.Play()
		a.AddOnExit(func() {
			_ = player.Close()
			_ = ctx.Suspend()
		})
	}

	a.AddSystems(schedule.First, app.System(pumpSystem(mix, nullSink)).Label("__spliti_audio_pump"))
}

// pumpSystem drains the mixer's finished-voice queue into frame-buffered
// Finished events and, when running headless, advances the mixer by wall
// clock so the null sink keeps mixer time honest.
func pumpSystem(mix *Audio, nullSink bool) func(*app.Ctx) {
	var scratch []Finished
	last := gotime.Now()
	return func(c *app.Ctx) {
		if nullSink {
			now := gotime.Now()
			d := now.Sub(last)
			last = now
			// Cap catch-up after a stall (debugger, suspend) so we don't
			// burn a huge mix in one frame.
			mix.Advance(min(d, 250*gotime.Millisecond))
		}
		scratch = mix.drainFinished(scratch[:0])
		for _, f := range scratch {
			app.SendEvent(c, f)
		}
	}
}
