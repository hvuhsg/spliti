package audio

import "time"

// PlayMusic starts ref on the music bus as a protected voice (it is never
// stolen when the voice pool fills). Music loops unless opt.Once is set.
//
// If music is already playing and opt.Crossfade > 0, the old track fades out
// over that duration while the new one fades in (sample-accurate inside the
// mixer — two voices with complementary envelopes). With no Crossfade the old
// track is stopped with the usual short ramp.
//
// ref is typically registered with Registry.LoadStream so a long track
// decodes incrementally, but buffer refs work too.
func (a *Audio) PlayMusic(ref string, opt MusicOptions) Handle {
	buf, sv, ok := a.resolve(ref)
	if !ok {
		return NoHandle
	}

	po := PlayOptions{
		Volume: opt.Volume,
		Loop:   !opt.Once,
		Bus:    BusMusic,
		FadeIn: opt.FadeIn,
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if old := a.voiceFor(a.music); old != nil {
		if opt.Crossfade > 0 {
			old.fadeFree = true
			old.fade.set(0, a.framesOf(opt.Crossfade))
			if po.FadeIn == 0 {
				po.FadeIn = opt.Crossfade
			}
		} else {
			old.mode = modeStopping
			old.ctl.set(0, a.rampFrames())
		}
	}

	h := a.startVoice(ref, buf, sv, po, true, spatialState{})
	a.music = h
	return h
}

// StopMusic fades the current music out over fade (0 = short ramp) and
// frees it. No-op when nothing is playing.
func (a *Audio) StopMusic(fade time.Duration) {
	a.mu.Lock()
	h := a.music
	a.music = NoHandle
	a.mu.Unlock()
	if fade > 0 {
		a.StopFade(h, fade)
	} else {
		a.Stop(h)
	}
}

// CurrentMusic returns the live music voice, or NoHandle when silent. The
// handle from a track that is crossfading out is no longer "current".
func (a *Audio) CurrentMusic() Handle {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.voiceFor(a.music) == nil {
		return NoHandle
	}
	return a.music
}
