package audio

import (
	"encoding/binary"
	"fmt"
	"math"
)

// decodeWAV parses a RIFF/WAVE file into interleaved float32 PCM at the
// file's native rate. Supported encodings: PCM 8-bit unsigned, 16/24-bit
// signed LE, and 32-bit IEEE float (including the WAVE_FORMAT_EXTENSIBLE
// wrappers around them), mono or stereo.
func decodeWAV(data []byte) (pcm []float32, channels, rate int, err error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, 0, fmt.Errorf("audio: not a RIFF/WAVE file")
	}

	const (
		fmtPCM        = 1
		fmtIEEEFloat  = 3
		fmtExtensible = 0xFFFE
	)
	var (
		format        uint16
		bits          int
		haveFmt       bool
		sampleData    []byte
		haveData      bool
	)

	// Walk the chunk list; chunks are word-aligned.
	for off := 12; off+8 <= len(data); {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		body := off + 8
		if size < 0 || body+size > len(data) {
			return nil, 0, 0, fmt.Errorf("audio: wav chunk %q overruns file", id)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, 0, fmt.Errorf("audio: wav fmt chunk too short (%d bytes)", size)
			}
			format = binary.LittleEndian.Uint16(data[body:])
			channels = int(binary.LittleEndian.Uint16(data[body+2:]))
			rate = int(binary.LittleEndian.Uint32(data[body+4:]))
			bits = int(binary.LittleEndian.Uint16(data[body+14:]))
			if format == fmtExtensible {
				// The real format is the first two bytes of the SubFormat GUID.
				if size < 40 {
					return nil, 0, 0, fmt.Errorf("audio: wav extensible fmt chunk too short")
				}
				format = binary.LittleEndian.Uint16(data[body+24:])
			}
			haveFmt = true
		case "data":
			sampleData = data[body : body+size]
			haveData = true
		}
		off = body + size + size%2
	}

	if !haveFmt || !haveData {
		return nil, 0, 0, fmt.Errorf("audio: wav missing fmt/data chunk")
	}
	if channels != 1 && channels != 2 {
		return nil, 0, 0, fmt.Errorf("audio: wav has %d channels (want mono or stereo)", channels)
	}
	if rate <= 0 {
		return nil, 0, 0, fmt.Errorf("audio: wav has invalid sample rate %d", rate)
	}

	switch {
	case format == fmtPCM && bits == 8:
		pcm = make([]float32, len(sampleData))
		for i, b := range sampleData {
			pcm[i] = (float32(b) - 128) / 128
		}
	case format == fmtPCM && bits == 16:
		n := len(sampleData) / 2
		pcm = make([]float32, n)
		for i := 0; i < n; i++ {
			s := int16(binary.LittleEndian.Uint16(sampleData[i*2:]))
			pcm[i] = float32(s) / 32768
		}
	case format == fmtPCM && bits == 24:
		n := len(sampleData) / 3
		pcm = make([]float32, n)
		for i := 0; i < n; i++ {
			b := sampleData[i*3:]
			s := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
			if s&0x800000 != 0 {
				s -= 1 << 24
			}
			pcm[i] = float32(s) / 8388608
		}
	case format == fmtIEEEFloat && bits == 32:
		n := len(sampleData) / 4
		pcm = make([]float32, n)
		for i := 0; i < n; i++ {
			pcm[i] = math.Float32frombits(binary.LittleEndian.Uint32(sampleData[i*4:]))
		}
	default:
		return nil, 0, 0, fmt.Errorf("audio: wav format %d / %d-bit unsupported", format, bits)
	}

	// Drop a trailing partial frame rather than erroring.
	pcm = pcm[:len(pcm)/channels*channels]
	return pcm, channels, rate, nil
}
