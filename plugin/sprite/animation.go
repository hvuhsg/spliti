package sprite

import gotime "time"

// Clip is one named animation: an ordered list of asset Refs (the frames) and how
// long each is shown. FrameDur[i] sets frame i's duration; a zero/missing entry
// falls back to DefaultDur. Loop replays from frame 0 at the end; a non-looping
// clip clamps on its last frame and marks the SpriteAnimation Finished.
type Clip struct {
	Frames     []string
	FrameDur   []gotime.Duration
	DefaultDur gotime.Duration
	Loop       bool
}

// FrameDuration returns frame i's display duration, falling back to DefaultDur.
func (c *Clip) FrameDuration(i int) gotime.Duration {
	if i >= 0 && i < len(c.FrameDur) && c.FrameDur[i] > 0 {
		return c.FrameDur[i]
	}
	return c.DefaultDur
}

// SpriteAnimation is an additive component: an entity that also has a Sprite gets
// its Sprite.Ref overwritten each frame with the active clip's current frame. An
// entity without it renders a static Sprite exactly as before. Switch clips with
// Play (idempotent) for state-driven animation ("idle" -> "walk" -> ...).
type SpriteAnimation struct {
	Clips    map[string]*Clip
	Active   string          // key into Clips; "" => the system does nothing
	Frame    int             // current frame index into the active clip
	Elapsed  gotime.Duration // time spent on the current frame
	Playing  bool            // false => hold the current frame (still displayed)
	Finished bool            // true once a non-looping clip reaches its last frame
}

// Play switches to clip name and starts it, resetting playback. It is a no-op if
// name is already the active, playing clip — so a movement system can call
// Play("walk") unconditionally every frame without restarting the animation.
// An unknown name still becomes Active; Advance simply renders nothing until a
// matching clip is registered.
func (a *SpriteAnimation) Play(name string) {
	if a.Active == name && a.Playing && !a.Finished {
		return
	}
	a.SetClip(name)
}

// SetClip switches to clip name and resets playback unconditionally.
func (a *SpriteAnimation) SetClip(name string) {
	a.Active = name
	a.Frame = 0
	a.Elapsed = 0
	a.Finished = false
	a.Playing = true
}

// Pause holds the current frame. Resume continues playback.
func (a *SpriteAnimation) Pause()  { a.Playing = false }
func (a *SpriteAnimation) Resume() { a.Playing = true }

// Advance moves playback forward by dt and returns the asset Ref to display now.
// ok is false only when there is no usable active clip (so the caller leaves the
// Sprite untouched). It handles multi-frame catch-up when dt spans several frame
// durations, looping, and one-shot clamping. A paused or finished clip does not
// advance but still reports its current frame so the sprite stays visible.
func (a *SpriteAnimation) Advance(dt gotime.Duration) (string, bool) {
	clip := a.Clips[a.Active]
	if clip == nil || len(clip.Frames) == 0 {
		return "", false
	}
	if a.Frame < 0 {
		a.Frame = 0
	}
	if a.Frame >= len(clip.Frames) {
		a.Frame = len(clip.Frames) - 1
	}
	if !a.Playing || a.Finished {
		return clip.Frames[a.Frame], true
	}

	a.Elapsed += dt
	for {
		dur := clip.FrameDuration(a.Frame)
		if dur <= 0 || a.Elapsed < dur {
			break // zero-duration clips never auto-advance (avoids an infinite loop)
		}
		a.Elapsed -= dur
		switch {
		case a.Frame+1 < len(clip.Frames):
			a.Frame++
		case clip.Loop:
			a.Frame = 0
		default:
			a.Finished = true
			a.Elapsed = 0
		}
		if a.Finished {
			break
		}
	}
	return clip.Frames[a.Frame], true
}
