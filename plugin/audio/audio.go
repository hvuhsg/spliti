// Package audio is spliti's sound plugin: a software mixer with per-voice
// volume/pan/pitch, looping, master/sfx/music buses, streamed music with
// fades and crossfade, and 2D/3D spatial helpers, played through a single
// oto output device (native and GOOS=js browser builds alike).
//
// Add Plugin{} to the app, load assets into the Registry resource from a
// Startup system, then drive playback through the Audio resource:
//
//	au := app.GetResource[audio.Audio](c)
//	au.Play("explosion")
//	au.PlayMusic("theme", audio.MusicOptions{FadeIn: time.Second})
//
// Audio is intentionally outside the fixed-timestep determinism contract
// (see docs/network.md): call it from Update/PostUpdate/Last, and never gate
// game logic on IsPlaying.
package audio

import "time"

// Bus names a mixer bus. Voice gain is multiplied by its bus volume and the
// master volume. Unknown bus names are created lazily at first use and route
// straight to master.
type Bus string

// The built-in buses.
const (
	BusMaster Bus = "master" // applied to everything
	BusSFX    Bus = "sfx"    // default for Play/PlayWith
	BusMusic  Bus = "music"  // used by PlayMusic
)

// PlayOptions configures a voice started by PlayWith / PlayAt2D / PlayAt3D.
//
// Zero values mean defaults, following the engine's zero-value convention:
// Volume 0 plays at full volume (1), Pitch 0 plays at normal speed (1), and
// Bus "" routes to BusSFX. To start a voice silent, pass a negative Volume
// (it clamps to 0) or use FadeIn.
type PlayOptions struct {
	Volume float64       // 0 → 1; multiplied with bus and master volumes
	Pan    float64       // -1 hard left … +1 hard right; 0 center (equal-power law)
	Pitch  float64       // playback-rate multiplier; 0 → 1
	Loop   bool          // loop until stopped
	Bus    Bus           // "" → BusSFX
	FadeIn time.Duration // ramp volume from 0 over this duration
	Paused bool          // start paused; Resume to begin
}

// MusicOptions configures PlayMusic. The zero value loops at full volume with
// no fades.
type MusicOptions struct {
	Volume    float64       // 0 → 1
	Once      bool          // play once instead of looping
	FadeIn    time.Duration // ramp the new track in over this duration
	Crossfade time.Duration // fade the currently playing music out over this
}

// Finished is sent (frame-buffered, via the plugin's pump system) when a
// voice reaches its natural end or is stopped. It is not sent on loop wraps.
type Finished struct {
	Handle Handle
	Ref    string
}
