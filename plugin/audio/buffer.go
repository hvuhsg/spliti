package audio

import "fmt"

// Buffer is a fully decoded sound: interleaved float32 PCM at the mixer's
// output rate (assets at other rates are resampled once at load). SFX play
// from Buffers; long music should use Registry.LoadStream instead so it is
// decoded incrementally.
type Buffer struct {
	pcm      []float32 // interleaved, len = frames*channels
	channels int       // 1 or 2
	frames   int
}

// Frames returns the buffer length in sample frames at the mixer rate.
func (b *Buffer) Frames() int { return b.frames }

// Channels returns 1 (mono) or 2 (stereo).
func (b *Buffer) Channels() int { return b.channels }

func newBuffer(pcm []float32, channels, srcRate, outRate int) (*Buffer, error) {
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("audio: %d channels unsupported (want mono or stereo)", channels)
	}
	if srcRate <= 0 {
		return nil, fmt.Errorf("audio: invalid sample rate %d", srcRate)
	}
	if len(pcm)%channels != 0 {
		return nil, fmt.Errorf("audio: pcm length %d not a multiple of %d channels", len(pcm), channels)
	}
	if srcRate != outRate {
		pcm = resampleLinear(pcm, channels, srcRate, outRate)
	}
	return &Buffer{pcm: pcm, channels: channels, frames: len(pcm) / channels}, nil
}

// resampleLinear converts interleaved PCM from srcRate to outRate by linear
// interpolation. Done once at load; runtime pitch shifting reuses the same
// fractional-position lerp inside the voice.
func resampleLinear(pcm []float32, channels, srcRate, outRate int) []float32 {
	srcFrames := len(pcm) / channels
	if srcFrames == 0 {
		return nil
	}
	outFrames := int(float64(srcFrames) * float64(outRate) / float64(srcRate))
	if outFrames < 1 {
		outFrames = 1
	}
	out := make([]float32, outFrames*channels)
	step := float64(srcRate) / float64(outRate)
	pos := 0.0
	for i := 0; i < outFrames; i++ {
		si := int(pos)
		if si >= srcFrames-1 {
			si = srcFrames - 1
		}
		frac := float32(pos - float64(si))
		sj := si + 1
		if sj >= srcFrames {
			sj = si
		}
		for ch := 0; ch < channels; ch++ {
			a := pcm[si*channels+ch]
			b := pcm[sj*channels+ch]
			out[i*channels+ch] = a + (b-a)*frac
		}
		pos += step
	}
	return out
}
