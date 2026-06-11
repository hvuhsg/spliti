package audio

import (
	"bytes"
	"fmt"
	"io"

	mp3 "github.com/hajimehoshi/go-mp3"
	"github.com/jfreymuth/oggvorbis"
)

type fileFormat int

const (
	formatUnknown fileFormat = iota
	formatWAV
	formatOgg
	formatMP3
)

// sniffFormat identifies a sound file by its magic bytes: RIFF (WAV),
// OggS (OGG Vorbis), or an ID3 tag / MPEG sync word (MP3).
func sniffFormat(data []byte) fileFormat {
	switch {
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE":
		return formatWAV
	case len(data) >= 4 && string(data[0:4]) == "OggS":
		return formatOgg
	case len(data) >= 3 && string(data[0:3]) == "ID3":
		return formatMP3
	case len(data) >= 2 && data[0] == 0xFF && data[1]&0xE0 == 0xE0:
		return formatMP3
	default:
		return formatUnknown
	}
}

// decodeAll fully decodes a WAV/OGG/MP3 file into interleaved float32 PCM at
// its native rate.
func decodeAll(data []byte) (pcm []float32, channels, rate int, err error) {
	switch sniffFormat(data) {
	case formatWAV:
		return decodeWAV(data)
	case formatOgg:
		pcm, format, err := oggvorbis.ReadAll(bytes.NewReader(data))
		if err != nil {
			return nil, 0, 0, fmt.Errorf("audio: ogg: %w", err)
		}
		return pcm, format.Channels, format.SampleRate, nil
	case formatMP3:
		d, err := mp3.NewDecoder(bytes.NewReader(data))
		if err != nil {
			return nil, 0, 0, fmt.Errorf("audio: mp3: %w", err)
		}
		raw, err := io.ReadAll(d)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("audio: mp3: %w", err)
		}
		// go-mp3 always emits 16-bit LE stereo at the file's rate.
		n := len(raw) / 2
		pcm = make([]float32, n)
		for i := 0; i < n; i++ {
			s := int16(uint16(raw[i*2]) | uint16(raw[i*2+1])<<8)
			pcm[i] = float32(s) / 32768
		}
		return pcm, 2, d.SampleRate(), nil
	default:
		return nil, 0, 0, fmt.Errorf("audio: unrecognized format (want WAV, OGG Vorbis, or MP3)")
	}
}
