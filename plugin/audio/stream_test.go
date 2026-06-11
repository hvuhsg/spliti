package audio

import (
	"math"
	"os"
	"testing"
	"time"
)

// fakeSource is a deterministic frameSource: a rising ramp of n frames, then
// EOF. It counts rewinds so loop behavior is observable.
type fakeSource struct {
	n, pos  int
	rate    int
	rewinds int
}

func (f *fakeSource) pull() (float32, float32, bool) {
	if f.pos >= f.n {
		return 0, 0, false
	}
	s := float32(f.pos) / float32(f.n)
	f.pos++
	return s, s, true
}
func (f *fakeSource) rewind() bool { f.rewinds++; f.pos = 0; return true }
func (f *fakeSource) sampleRate() int { return f.rate }
func (f *fakeSource) channels() int   { return 1 }

// startFakeStream injects a stream voice the way resolve would, bypassing the
// registry (which only hands out real decoders).
func startFakeStream(a *Audio, src frameSource, opt PlayOptions) Handle {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startVoice("fake", nil, &streamVoice{src: src}, opt, false, spatialState{})
}

func TestStreamLoopRewind(t *testing.T) {
	src := &fakeSource{n: 1000, rate: testRate}
	a := NewAudio(testRate, 4)
	h := startFakeStream(a, src, PlayOptions{Loop: true})

	l, _ := readFrames(t, a, 3500) // crosses EOF three times
	if src.rewinds < 3 {
		t.Fatalf("rewinds = %d, want >= 3", src.rewinds)
	}
	if !a.IsPlaying(h) {
		t.Fatal("looping stream ended")
	}
	// The ramp is continuous except at the intentional wrap (1 -> 0); no
	// other sample-to-sample jump may exceed the ramp slope.
	slope := 1.0/1000*centerPan + 1e-4
	wraps := 0
	for i := 1; i < len(l); i++ {
		d := float64(l[i] - l[i-1])
		if -d > 0.5 {
			wraps++ // the sawtooth seam itself
		} else if math.Abs(d) > slope {
			t.Fatalf("discontinuity %g at frame %d", d, i)
		}
	}
	if wraps != 3 {
		t.Fatalf("saw %d wraps, want 3", wraps)
	}
}

func TestStreamEOFEndsVoice(t *testing.T) {
	src := &fakeSource{n: 500, rate: testRate}
	a := NewAudio(testRate, 4)
	h := startFakeStream(a, src, PlayOptions{})
	readFrames(t, a, 600)
	if a.IsPlaying(h) {
		t.Fatal("non-looping stream should end at EOF")
	}
	if src.rewinds != 0 {
		t.Fatalf("non-looping stream rewound %d times", src.rewinds)
	}
}

func TestStreamResample(t *testing.T) {
	// A 22050 Hz source at 48000 Hz output must advance at ~0.46x: a 1000-
	// frame source lasts ~2177 output frames.
	src := &fakeSource{n: 1000, rate: 22050}
	a := NewAudio(testRate, 4)
	h := startFakeStream(a, src, PlayOptions{})
	readFrames(t, a, 2050)
	if !a.IsPlaying(h) {
		t.Fatal("resampled stream ended early")
	}
	readFrames(t, a, 256)
	if a.IsPlaying(h) {
		t.Fatal("resampled stream should have ended")
	}
}

func TestSniffFormat(t *testing.T) {
	ogg, err := os.ReadFile("testdata/tone.ogg")
	if err != nil {
		t.Fatal(err)
	}
	mp3, err := os.ReadFile("testdata/tone.mp3")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		data []byte
		want fileFormat
	}{
		{"wav", buildWAV(1, 1, 44100, 16, make([]byte, 8)), formatWAV},
		{"ogg", ogg, formatOgg},
		{"mp3", mp3, formatMP3},
		{"mp3 sync word", []byte{0xFF, 0xFB, 0x90, 0x00}, formatMP3},
		{"garbage", []byte("not audio at all"), formatUnknown},
		{"empty", nil, formatUnknown},
	}
	for _, tc := range cases {
		if got := sniffFormat(tc.data); got != tc.want {
			t.Errorf("%s: sniffed %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestLoadOggAndMP3FullDecode(t *testing.T) {
	for _, name := range []string{"testdata/tone.ogg", "testdata/tone.mp3"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		a := NewAudio(testRate, 4)
		if err := a.Registry().Load(name, data); err != nil {
			t.Fatalf("Load(%s): %v", name, err)
		}
		h := a.Play(name)
		// Measure peak over 100ms — codec priming/encoder delay makes the
		// first few hundred frames quiet.
		l, _ := readFrames(t, a, testRate/10)
		var peak float64
		for _, s := range l {
			peak = max(peak, math.Abs(float64(s)))
		}
		if peak < 0.05 {
			t.Errorf("%s: peak %g, expected an audible 440 Hz tone", name, peak)
		}
		// 0.15 s fixture: gone after 0.35 s of output (codec padding slack).
		readFrames(t, a, testRate/4)
		if a.IsPlaying(h) {
			t.Errorf("%s: 0.15s one-shot still playing after 0.35s", name)
		}
	}
}

func TestLoadStreamPlaysAndLoops(t *testing.T) {
	for _, name := range []string{"testdata/tone.ogg", "testdata/tone.mp3"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		a := NewAudio(testRate, 4)
		if err := a.Registry().LoadStream(name, data); err != nil {
			t.Fatalf("LoadStream(%s): %v", name, err)
		}
		h := a.PlayMusic(name, MusicOptions{})
		l, _ := readFrames(t, a, testRate/10)
		var peak float64
		for _, s := range l {
			peak = max(peak, math.Abs(float64(s)))
		}
		if peak < 0.05 {
			t.Errorf("%s: stream peak %g, expected an audible tone", name, peak)
		}
		// Looping music must survive several wraps of the 0.15s fixture.
		a.Advance(time.Second)
		if !a.IsPlaying(h) {
			t.Errorf("%s: looping music ended", name)
		}
		a.StopMusic(0)
		a.Advance(100 * time.Millisecond)
		if a.IsPlaying(h) {
			t.Errorf("%s: music alive after StopMusic", name)
		}
	}
}

func TestLoadStreamRejectsWAVAndGarbage(t *testing.T) {
	a := NewAudio(testRate, 4)
	if err := a.Registry().LoadStream("w", buildWAV(1, 1, 44100, 16, make([]byte, 8))); err == nil {
		t.Error("LoadStream(wav) should error (uncompressed; use Load)")
	}
	if err := a.Registry().LoadStream("g", []byte("garbage")); err == nil {
		t.Error("LoadStream(garbage) should error")
	}
	if err := a.Registry().Load("g", []byte("garbage")); err == nil {
		t.Error("Load(garbage) should error")
	}
}
