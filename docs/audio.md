# Audio

`plugin/audio` is spliti's sound system: a software mixer with per-voice
volume/pan/pitch, looping, handle-based control, master/SFX/music buses,
streamed music with fades and crossfade, and 2D/3D spatial helpers. Output
goes through one [oto](https://github.com/ebitengine/oto) device, so the same
game code plays sound natively **and in the browser** (`GOOS=js`).

## Quickstart

```go
a.AddPlugins(audio.Plugin{}) // after defaultplugins / the render plugin

a.AddSystems(schedule.Startup, func(c *app.Ctx) {
    reg := app.GetResource[audio.Registry](c)
    reg.Load("explosion", explosionWAV)                       // []byte: WAV, OGG, or MP3
    reg.LoadStream("theme", themeOGG)                         // music: decoded on the fly
    reg.LoadPCM("blip", pcm, 1, 48000)                        // raw synthesized samples
})

a.AddSystems(schedule.Update, func(c *app.Ctx) {
    au := app.GetResource[audio.Audio](c)
    au.Play("explosion")
    au.PlayMusic("theme", audio.MusicOptions{FadeIn: time.Second})
})
```

Assets typically come from `go:embed`:

```go
//go:embed assets
var assets embed.FS
...
reg.LoadFS(assets, "assets/theme.ogg") // ref = the path
```

## Formats

| Format     | Loader              | Notes                                          |
| ---------- | ------------------- | ---------------------------------------------- |
| WAV        | `Load`              | PCM 8/16/24-bit + float32; hand-rolled parser  |
| OGG Vorbis | `Load`/`LoadStream` | the usual choice for music                     |
| MP3        | `Load`/`LoadStream` | via go-mp3 (always decodes to stereo)          |
| raw PCM    | `LoadPCM`           | interleaved float32, mono or stereo, any rate  |

`Load` fully decodes to PCM at the mixer rate — right for SFX. `LoadStream`
keeps the compressed bytes and each playing voice decodes its own position
incrementally — right for music (a five-minute track stays ~5 MB instead of
~100 MB). Formats are sniffed from magic bytes; you never say which is which.

## Voices and handles

Every `Play*` call starts a *voice* and returns a `Handle`:

```go
h := au.PlayWith("engine", audio.PlayOptions{Loop: true, Volume: 0.7, Pitch: 1.2})
au.SetPitch(h, 1.5)     // rev up
au.Pause(h); au.Resume(h)
au.StopFade(h, time.Second)
```

Handles are generation-encoded: once a voice ends, every call through its
handle is a safe no-op — store them freely in components, no lifetime
bookkeeping. When a voice ends (naturally or stopped) the plugin publishes a
frame-buffered `audio.Finished{Handle, Ref}` event.

**Zero values are defaults** in `PlayOptions`, matching the engine convention:
`Volume: 0` means full volume and `Pitch: 0` means normal speed. To start a
voice silent, use `FadeIn` (or a negative Volume, which clamps to 0).

The pool holds 32 voices by default (`Plugin.MaxVoices`). When it is full the
quietest non-music voice is stolen; music voices are never stolen.

Every audible change — volume, pan, stop, pause, bus volume — ramps over ~8 ms
inside the mixer, so there are no clicks, ever. `FadeTo`/`StopFade`/`FadeIn`
are the long, musical fades on top of that.

## Buses

Voice gain is `voice × bus × master`. `Play` routes to `BusSFX`, `PlayMusic`
to `BusMusic`; both route through `BusMaster`:

```go
au.SetBusVolume(audio.BusMusic, 0.5)  // settings-menu music slider
au.SetBusVolume(audio.BusMaster, 0)   // mute everything
```

Unknown bus names are created on first use, so `PlayOptions{Bus: "voice"}`
gives dialogue its own slider for free.

## Music

```go
au.PlayMusic("theme", audio.MusicOptions{Volume: 0.6})                  // loops by default
au.PlayMusic("boss", audio.MusicOptions{Crossfade: 2 * time.Second})    // beat-free swap
au.StopMusic(time.Second)
```

`CurrentMusic()` returns the live track's handle (`NoHandle` when silent).

## Spatial audio

2D (terminal or webgpu games — any coordinate space works):

```go
au.SetListener2D(playerX, playerY)                       // each frame
h := au.PlayAt2D("growl", wolfX, wolfY, audio.PlayOptions{},
    audio.SpatialOptions{MinDist: 2, MaxDist: 30})
au.SetVoicePos2D(h, wolfX, wolfY)                        // as it moves
```

Pan follows the X offset, attenuation the distance: linear falloff by
default, `RolloffInverse` for natural 1/r. Inside `MinDist` the sound is at
full volume and panning relaxes to center, so walking over a sound source
never hard-flips the stereo image.

For render3d games, add `plugin/audio/spatial3d` and forget about the
listener entirely — the camera is the ear:

```go
a.AddPlugins(render3d.Plugin{...}, audio.Plugin{}, spatial3d.Plugin{})

h := au.PlayAt3D("engine", pos, audio.PlayOptions{Loop: true},
    audio.SpatialOptions{MaxDist: 40})
// attach to the entity; its GlobalTransform drives the voice every frame
mapper.Assign(e, &spatial3d.Emitter{Handle: h})
```

`spatial3d` lives in its own package so terminal games importing
`plugin/audio` pull in no GPU dependencies.

## Headless, CI, and failure behavior

If no audio device exists (CI, ssh), the plugin logs one warning and runs
the mixer into a silent null sink **at wall-clock rate** — `IsPlaying`,
loops, fades, and `Finished` events behave exactly as with a device, so
game logic never branches on audio availability. Set `Plugin{Required: true}`
to panic instead.

The mixer itself is pure Go and fully unit-testable without a device — see
`plugin/audio/mixer_test.go` for the pattern (drive `Read`/`Advance`
directly).

## Platform notes

- **macOS / Windows / browser**: no cgo (`CGO_ENABLED=0` works; macOS goes
  through purego → AudioToolbox).
- **Linux**: needs cgo and ALSA headers (`libasound2-dev`), like the GPU
  plugins.
- **Browser (`GOOS=js`)**: plays through Web Audio. Browsers keep audio
  muted until the first user gesture; oto resumes automatically on it. A
  key press or click *is* a gesture, so games that start sounds from input
  are unaffected — only startup music begins audibly late (it keeps
  "playing" silently meanwhile). The default device buffer is 200 ms on js
  (vs 50 ms native) because the wasm loop only yields between rAF frames.
  A backgrounded tab stops rAF entirely and audio will starve; that is a
  platform limit.

## Determinism

Audio is deliberately **outside** the lockstep determinism contract
(docs/network.md): call it from `Update`/`PostUpdate`/`Last` (or anywhere in
single-player), and never gate game logic on `IsPlaying` in a networked
game — peers' mixers are not synchronized.
