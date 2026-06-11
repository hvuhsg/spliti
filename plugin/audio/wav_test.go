package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// buildWAV assembles a syntactically valid WAV file around raw sample bytes.
func buildWAV(format, channels, rate, bits int, sampleData []byte) []byte {
	var b bytes.Buffer
	w := func(v any) { binary.Write(&b, binary.LittleEndian, v) }

	blockAlign := channels * bits / 8
	b.WriteString("RIFF")
	w(uint32(4 + 24 + 8 + len(sampleData)))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	w(uint32(16))
	w(uint16(format))
	w(uint16(channels))
	w(uint32(rate))
	w(uint32(rate * blockAlign))
	w(uint16(blockAlign))
	w(uint16(bits))
	b.WriteString("data")
	w(uint32(len(sampleData)))
	b.Write(sampleData)
	return b.Bytes()
}

func TestDecodeWAV16BitMono(t *testing.T) {
	samples := []int16{0, 16384, -16384, 32767, -32768}
	var data bytes.Buffer
	binary.Write(&data, binary.LittleEndian, samples)

	pcm, ch, rate, err := decodeWAV(buildWAV(1, 1, 44100, 16, data.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if ch != 1 || rate != 44100 || len(pcm) != len(samples) {
		t.Fatalf("ch=%d rate=%d len=%d", ch, rate, len(pcm))
	}
	want := []float32{0, 0.5, -0.5, 32767.0 / 32768, -1}
	for i := range want {
		if math.Abs(float64(pcm[i]-want[i])) > 1e-6 {
			t.Errorf("sample %d = %g, want %g", i, pcm[i], want[i])
		}
	}
}

func TestDecodeWAV24BitStereo(t *testing.T) {
	// Two frames: (max, min), (half, zero) in 24-bit signed LE.
	put24 := func(b *bytes.Buffer, v int32) {
		b.WriteByte(byte(v))
		b.WriteByte(byte(v >> 8))
		b.WriteByte(byte(v >> 16))
	}
	var data bytes.Buffer
	put24(&data, 0x7FFFFF)
	put24(&data, -0x800000)
	put24(&data, 0x400000)
	put24(&data, 0)

	pcm, ch, rate, err := decodeWAV(buildWAV(1, 2, 48000, 24, data.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if ch != 2 || rate != 48000 || len(pcm) != 4 {
		t.Fatalf("ch=%d rate=%d len=%d", ch, rate, len(pcm))
	}
	want := []float32{8388607.0 / 8388608, -1, 0.5, 0}
	for i := range want {
		if math.Abs(float64(pcm[i]-want[i])) > 1e-6 {
			t.Errorf("sample %d = %g, want %g", i, pcm[i], want[i])
		}
	}
}

func TestDecodeWAVFloat32(t *testing.T) {
	samples := []float32{0, 0.25, -0.75, 1}
	var data bytes.Buffer
	binary.Write(&data, binary.LittleEndian, samples)

	pcm, ch, rate, err := decodeWAV(buildWAV(3, 1, 22050, 32, data.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if ch != 1 || rate != 22050 {
		t.Fatalf("ch=%d rate=%d", ch, rate)
	}
	for i := range samples {
		if pcm[i] != samples[i] {
			t.Errorf("sample %d = %g, want %g", i, pcm[i], samples[i])
		}
	}
}

func TestDecodeWAV8Bit(t *testing.T) {
	pcm, _, _, err := decodeWAV(buildWAV(1, 1, 8000, 8, []byte{128, 255, 0}))
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{0, 127.0 / 128, -1}
	for i := range want {
		if math.Abs(float64(pcm[i]-want[i])) > 1e-6 {
			t.Errorf("sample %d = %g, want %g", i, pcm[i], want[i])
		}
	}
}

func TestDecodeWAVMalformed(t *testing.T) {
	cases := map[string][]byte{
		"empty":         {},
		"not riff":      []byte("OggS this is not a wav file at all"),
		"truncated":     buildWAV(1, 1, 44100, 16, make([]byte, 64))[:20],
		"no data chunk": buildWAV(1, 1, 44100, 16, nil)[:36], // cut before "data"
		"bad channels":  buildWAV(1, 6, 44100, 16, make([]byte, 12)),
		"bad format":    buildWAV(7, 1, 44100, 16, make([]byte, 12)),
	}
	for name, data := range cases {
		if _, _, _, err := decodeWAV(data); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestDecodeWAVChunkOverrun(t *testing.T) {
	// A data chunk whose declared size exceeds the file must error, not panic.
	w := buildWAV(1, 1, 44100, 16, make([]byte, 16))
	binary.LittleEndian.PutUint32(w[len(w)-20:], 1<<30)
	if _, _, _, err := decodeWAV(w); err == nil {
		t.Fatal("expected overrun error")
	}
}
