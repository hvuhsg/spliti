package audio

import (
	"bytes"
	"fmt"
	"io"
	"time"

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

// DecodeWaveform decodes a WAV/OGG/MP3 file and reduces it to bins peak
// amplitudes in [0,1] (the max absolute sample across channels in each time
// slice) plus the clip's duration. It is the data an editor draws as a
// waveform; bins is clamped to at least 1.
func DecodeWaveform(data []byte, bins int) (peaks []float32, dur time.Duration, err error) {
	pcm, channels, rate, err := decodeAll(data)
	if err != nil {
		return nil, 0, err
	}
	if channels < 1 {
		channels = 1
	}
	if bins < 1 {
		bins = 1
	}
	frames := len(pcm) / channels
	if rate > 0 {
		dur = time.Duration(float64(frames) / float64(rate) * float64(time.Second))
	}
	peaks = make([]float32, bins)
	if frames == 0 {
		return peaks, dur, nil
	}
	for b := 0; b < bins; b++ {
		start := b * frames / bins
		end := (b + 1) * frames / bins
		if end <= start {
			end = start + 1
		}
		var peak float32
		for f := start; f < end && f < frames; f++ {
			for ch := 0; ch < channels; ch++ {
				v := pcm[f*channels+ch]
				if v < 0 {
					v = -v
				}
				if v > peak {
					peak = v
				}
			}
		}
		if peak > 1 {
			peak = 1
		}
		peaks[b] = peak
	}
	return peaks, dur, nil
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
