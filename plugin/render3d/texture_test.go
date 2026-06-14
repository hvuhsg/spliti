package render3d

import "testing"

func TestMipLevelCount(t *testing.T) {
	cases := []struct {
		w, h, want uint32
	}{
		{1, 1, 1},
		{2, 2, 2},
		{4, 4, 3},
		{256, 256, 9},
		{8, 1, 4},   // non-square: driven by the larger dimension
		{5, 3, 3},   // 5x3 -> 2x1 -> 1x1
		{1024, 1, 11},
	}
	for _, c := range cases {
		if got := mipLevelCount(c.w, c.h); got != c.want {
			t.Errorf("mipLevelCount(%d,%d) = %d, want %d", c.w, c.h, got, c.want)
		}
	}
}

func TestDownsampleRGBA_LinearAverage(t *testing.T) {
	// 2x2 linear image with a known per-channel average. avg4 rounds to nearest.
	src := []byte{
		0, 0, 0, 0, 100, 100, 100, 100,
		200, 200, 200, 200, 0, 0, 0, 0,
	}
	dst, dw, dh := downsampleRGBA(src, 2, 2, false)
	if dw != 1 || dh != 1 {
		t.Fatalf("dims = %dx%d, want 1x1", dw, dh)
	}
	// (0+100+200+0+2)/4 = 75 for every channel.
	for ch := 0; ch < 4; ch++ {
		if dst[ch] != 75 {
			t.Errorf("channel %d = %d, want 75", ch, dst[ch])
		}
	}
}

func TestDownsampleRGBA_OddDimsClampInBounds(t *testing.T) {
	// 3x3 must not panic (footprint clamps to bounds) and yields 1x1.
	src := make([]byte, 4*3*3)
	for i := range src {
		src[i] = 128
	}
	dst, dw, dh := downsampleRGBA(src, 3, 3, false)
	if dw != 1 || dh != 1 {
		t.Fatalf("dims = %dx%d, want 1x1", dw, dh)
	}
	if dst[0] != 128 {
		t.Errorf("uniform image downsampled to %d, want 128", dst[0])
	}
}

func TestSRGBRoundTrip(t *testing.T) {
	// Every 8-bit sRGB value should survive linear->sRGB encode of its decode.
	for i := 0; i < 256; i++ {
		got := linearToSRGB(srgbToLinearLUT[i])
		if d := int(got) - i; d < -1 || d > 1 {
			t.Errorf("sRGB round trip %d -> %d (off by %d)", i, got, d)
		}
	}
}

func TestDownsampleRGBA_SRGBAveragesInLinearSpace(t *testing.T) {
	// Averaging black and white directly in sRGB space gives ~128, but the
	// correct linear-space average of 0.0 and 1.0 is 0.5, which re-encodes to
	// ~188 in sRGB. downsampleRGBA(srgb=true) must produce the latter.
	src := []byte{
		0, 0, 0, 255, 255, 255, 255, 255,
		0, 0, 0, 255, 255, 255, 255, 255,
	}
	dst, _, _ := downsampleRGBA(src, 2, 2, true)
	if dst[0] < 180 || dst[0] > 196 {
		t.Errorf("sRGB linear-space avg of 0 and 255 = %d, want ~188", dst[0])
	}
	if dst[3] != 255 {
		t.Errorf("alpha = %d, want 255 (linear average, not sRGB)", dst[3])
	}
}
