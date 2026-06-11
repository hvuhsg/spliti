package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	mp3 "github.com/hajimehoshi/go-mp3"
	"github.com/jfreymuth/oggvorbis"
)

// streamDecoder is the minimal seam over the OGG and MP3 decoders: read fills
// dst with interleaved float32 samples (not frames) and reports io.EOF at the
// end; rewind seeks back to the first sample for looping.
type streamDecoder interface {
	read(dst []float32) (int, error)
	rewind() error
	sampleRate() int
	channels() int
}

// streamSource adapts a streamDecoder to the voice's frameSource: it stages
// decoded samples in a fixed scratch buffer and hands them out one frame at a
// time. All of this runs under the mixer lock on the device goroutine, so the
// scratch is allocated once up front and read never touches the filesystem —
// the compressed bytes live in memory.
type streamSource struct {
	dec     streamDecoder
	scratch []float32
	rd, n   int // consumed / valid floats in scratch
}

const streamScratchFloats = 4096

// newStreamSource builds an incremental decoder over in-memory compressed
// bytes. The format is sniffed from the leading magic bytes.
func newStreamSource(data []byte) (frameSource, error) {
	dec, err := newStreamDecoder(data)
	if err != nil {
		return nil, err
	}
	return &streamSource{dec: dec, scratch: make([]float32, streamScratchFloats)}, nil
}

func newStreamDecoder(data []byte) (streamDecoder, error) {
	switch sniffFormat(data) {
	case formatOgg:
		r, err := oggvorbis.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("audio: ogg: %w", err)
		}
		if ch := r.Channels(); ch != 1 && ch != 2 {
			return nil, fmt.Errorf("audio: ogg has %d channels (want mono or stereo)", ch)
		}
		return &oggDecoder{r: r}, nil
	case formatMP3:
		d, err := mp3.NewDecoder(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("audio: mp3: %w", err)
		}
		return &mp3Decoder{d: d, buf: make([]byte, streamScratchFloats*2)}, nil
	case formatWAV:
		return nil, fmt.Errorf("audio: wav is uncompressed; use Load instead of LoadStream")
	default:
		return nil, fmt.Errorf("audio: unrecognized format (want WAV, OGG Vorbis, or MP3)")
	}
}

func (s *streamSource) sampleRate() int { return s.dec.sampleRate() }
func (s *streamSource) channels() int   { return s.dec.channels() }

// pull yields the next source frame. Returns ok=false at EOF; the voice
// decides whether to rewind (looping) or end.
func (s *streamSource) pull() (l, r float32, ok bool) {
	ch := s.dec.channels()
	for s.n-s.rd < ch {
		// Carry a split frame (stereo pair straddling two reads) forward.
		left := s.n - s.rd
		copy(s.scratch, s.scratch[s.rd:s.n])
		n, _ := s.dec.read(s.scratch[left:])
		s.rd, s.n = 0, left+n
		if n == 0 {
			// EOF, a zero-progress read, or a mid-stream decode error:
			// all end the stream (the voice may rewind us to loop).
			return 0, 0, false
		}
	}
	l = s.scratch[s.rd]
	r = l
	if ch == 2 {
		r = s.scratch[s.rd+1]
	}
	s.rd += ch
	return l, r, true
}

// rewind seeks the decoder back to the start for looping playback.
func (s *streamSource) rewind() bool {
	if err := s.dec.rewind(); err != nil {
		return false
	}
	s.rd, s.n = 0, 0
	return true
}

// ── decoder adapters ────────────────────────────────────────────────────────

type oggDecoder struct {
	r *oggvorbis.Reader
}

func (o *oggDecoder) read(dst []float32) (int, error) { return o.r.Read(dst) }
func (o *oggDecoder) rewind() error                   { return o.r.SetPosition(0) }
func (o *oggDecoder) sampleRate() int                 { return o.r.SampleRate() }
func (o *oggDecoder) channels() int                   { return o.r.Channels() }

// mp3Decoder adapts go-mp3, which always emits 16-bit LE stereo at the
// file's sample rate.
type mp3Decoder struct {
	d   *mp3.Decoder
	buf []byte
}

func (m *mp3Decoder) read(dst []float32) (int, error) {
	want := len(dst) * 2 // one int16 (2 bytes) per float32 sample
	if want > len(m.buf) {
		want = len(m.buf)
	}
	want -= want % 4 // whole stereo frames
	n, err := io.ReadFull(m.d, m.buf[:want])
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	n -= n % 4
	for i := 0; i < n/2; i++ {
		s := int16(binary.LittleEndian.Uint16(m.buf[i*2:]))
		dst[i] = float32(s) / 32768
	}
	return n / 2, err
}

func (m *mp3Decoder) rewind() error {
	_, err := m.d.Seek(0, io.SeekStart)
	return err
}

func (m *mp3Decoder) sampleRate() int { return m.d.SampleRate() }
func (m *mp3Decoder) channels() int   { return 2 }
