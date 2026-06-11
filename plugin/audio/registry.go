package audio

import (
	"fmt"
	"io"
	"io/fs"
	"sync"
)

// Registry maps string refs to loaded sounds, mirroring the engine's other
// asset registries (webgpu.TextureRegistry, render3d.MeshRegistry). It is
// installed as a resource by Plugin; populate it from Startup systems.
//
// Load/LoadReader/LoadFS fully decode into a Buffer (right for SFX);
// LoadStream keeps the compressed bytes and decodes incrementally at playback
// (right for music). Loading an existing ref replaces it; voices already
// playing the old asset keep playing it to the end.
type Registry struct {
	mu    sync.Mutex
	rate  int // mixer output rate
	byRef map[string]*regEntry
}

type regEntry struct {
	buf    *Buffer
	stream []byte // compressed bytes for incremental decode
}

func newRegistry(rate int) *Registry {
	return &Registry{rate: rate, byRef: make(map[string]*regEntry)}
}

// Load registers a WAV, OGG Vorbis, or MP3 file (sniffed by magic bytes),
// fully decoded to PCM at the mixer rate. Right for SFX; long music should
// use LoadStream.
func (r *Registry) Load(ref string, data []byte) error {
	pcm, channels, rate, err := decodeAll(data)
	if err != nil {
		return fmt.Errorf("%w (ref %q)", err, ref)
	}
	return r.LoadPCM(ref, pcm, channels, rate)
}

// LoadReader reads rd to the end and registers it like Load.
func (r *Registry) LoadReader(ref string, rd io.Reader) error {
	data, err := io.ReadAll(rd)
	if err != nil {
		return fmt.Errorf("audio: read %q: %w", ref, err)
	}
	return r.Load(ref, data)
}

// LoadFS registers fsys's file at path under ref = path. Pairs with go:embed:
//
//	//go:embed assets/*.ogg
//	var assets embed.FS
//	reg.LoadFS(assets, "assets/theme.ogg")
func (r *Registry) LoadFS(fsys fs.FS, path string) error {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return fmt.Errorf("audio: read %q: %w", path, err)
	}
	return r.Load(path, data)
}

// LoadStream registers an OGG Vorbis or MP3 file for incremental decoding:
// the compressed bytes stay in memory and each playing voice decodes its own
// position on the fly. Right for music — a fully decoded five-minute track
// would be ~100 MB of PCM; its OGG stays ~5 MB.
//
// The data is validated by opening a decoder once, so corrupt files fail
// here rather than silently at playback.
func (r *Registry) LoadStream(ref string, data []byte) error {
	if _, err := newStreamDecoder(data); err != nil {
		return fmt.Errorf("%w (ref %q)", err, ref)
	}
	r.put(ref, &regEntry{stream: data})
	return nil
}

// LoadPCM registers raw interleaved float32 PCM under ref, resampling to the
// mixer rate if needed. channels must be 1 or 2. Handy for programmatic
// sounds — see examples/snake for generated chiptune blips.
func (r *Registry) LoadPCM(ref string, pcm []float32, channels, sampleRate int) error {
	b, err := newBuffer(pcm, channels, sampleRate, r.rate)
	if err != nil {
		return fmt.Errorf("%w (ref %q)", err, ref)
	}
	r.put(ref, &regEntry{buf: b})
	return nil
}

// Unload removes ref. Voices already playing it are unaffected.
func (r *Registry) Unload(ref string) {
	r.mu.Lock()
	delete(r.byRef, ref)
	r.mu.Unlock()
}

// Has reports whether ref is loaded.
func (r *Registry) Has(ref string) bool { return r.lookup(ref) != nil }

func (r *Registry) put(ref string, e *regEntry) {
	r.mu.Lock()
	r.byRef[ref] = e
	r.mu.Unlock()
}

func (r *Registry) lookup(ref string) *regEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byRef[ref]
}
