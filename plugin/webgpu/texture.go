package webgpu

import (
	"fmt"
	"image"
	"image/draw"

	"github.com/cogentcore/webgpu/wgpu"
)

// gpuTexture is one uploaded texture plus the resources the renderer binds for
// it. Pixel dimensions are kept so a sprite with zero Transform.W/H can default
// to drawing the texture 1:1.
type gpuTexture struct {
	texture   *wgpu.Texture
	view      *wgpu.TextureView
	bindGroup *wgpu.BindGroup
	w, h      int
}

// TextureRegistry maps a string ref to an uploaded GPU texture. Games populate
// it (usually from a Startup system, after the GPU resource exists) and entities
// reference textures by ref via the Sprite component. Retrieve it with
// app.GetResource[TextureRegistry].
type TextureRegistry struct {
	gpu   *GPU
	byRef map[string]*gpuTexture
}

func newTextureRegistry(g *GPU) *TextureRegistry {
	return &TextureRegistry{gpu: g, byRef: make(map[string]*gpuTexture)}
}

// Load uploads img to the GPU under ref, replacing any existing texture for that
// ref. The image is converted to RGBA8; any image.Image works. Call after the
// plugin's Build (i.e. from Startup or later), since it needs the device.
func (r *TextureRegistry) Load(ref string, img image.Image) error {
	if r == nil || r.gpu == nil {
		return fmt.Errorf("webgpu: texture registry not initialized")
	}
	rgba := toRGBA(img)
	w := rgba.Rect.Dx()
	h := rgba.Rect.Dy()
	if w == 0 || h == 0 {
		return fmt.Errorf("webgpu: texture %q has zero size", ref)
	}
	g := r.gpu

	// Premultiply alpha into a fresh copy (toRGBA may alias the caller's image,
	// so never mutate rgba.Pix in place). Premultiplied texels keep linear
	// filtering and blending correct across alpha edges, and let the mip chain be
	// built by a plain box filter — averaging premultiplied colors is the correct
	// downsample, whereas averaging straight-alpha colors bleeds transparent
	// texels inward. (Both happen in the sRGB-encoded byte domain; gamma-correct
	// linear-space resampling is the separate, larger color-management gap.)
	base := make([]byte, len(rgba.Pix))
	copy(base, rgba.Pix)
	premultiplyAlpha(base)

	levels := mipLevelCount(w, h)
	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "spliti.webgpu.tex." + ref,
		Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
		Dimension:     wgpu.TextureDimension2D,
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		Format:        wgpu.TextureFormatRGBA8UnormSrgb,
		MipLevelCount: uint32(levels),
		SampleCount:   1,
	})
	if err != nil {
		return fmt.Errorf("webgpu: create texture %q: %w", ref, err)
	}

	// Upload level 0, then box-downsample each successive level on the CPU and
	// upload it. WebGPU has no built-in mip generation, so we build the chain
	// here; a colored sprite scaled down now samples a matching mip instead of
	// shimmering on the full-res texels.
	cur, cw, ch := base, w, h
	for lvl := 0; lvl < levels; lvl++ {
		dst := tex.AsImageCopy()
		dst.MipLevel = uint32(lvl)
		if err := g.queue.WriteTexture(
			dst,
			wgpu.ToBytes(cur),
			&wgpu.TextureDataLayout{
				Offset:       0,
				BytesPerRow:  uint32(4 * cw),
				RowsPerImage: uint32(ch),
			},
			&wgpu.Extent3D{Width: uint32(cw), Height: uint32(ch), DepthOrArrayLayers: 1},
		); err != nil {
			tex.Release()
			return fmt.Errorf("webgpu: write texture %q level %d: %w", ref, lvl, err)
		}
		if lvl < levels-1 {
			nw, nh := max(1, cw/2), max(1, ch/2)
			cur = downsample(cur, cw, ch, nw, nh)
			cw, ch = nw, nh
		}
	}

	view, err := tex.CreateView(nil)
	if err != nil {
		tex.Release()
		return fmt.Errorf("webgpu: texture view %q: %w", ref, err)
	}

	bg, err := g.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "spliti.webgpu.tex.bg." + ref,
		Layout: g.texBGL,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: view},
			{Binding: 1, Sampler: g.sampler},
		},
	})
	if err != nil {
		view.Release()
		tex.Release()
		return fmt.Errorf("webgpu: texture bind group %q: %w", ref, err)
	}

	if old := r.byRef[ref]; old != nil {
		old.release()
	}
	r.byRef[ref] = &gpuTexture{texture: tex, view: view, bindGroup: bg, w: w, h: h}
	return nil
}

// get returns the uploaded texture for ref, or nil if not registered.
func (r *TextureRegistry) get(ref string) *gpuTexture {
	if r == nil {
		return nil
	}
	return r.byRef[ref]
}

// releaseAll frees every uploaded texture. Called from the GPU teardown.
func (r *TextureRegistry) releaseAll() {
	if r == nil {
		return
	}
	for _, t := range r.byRef {
		t.release()
	}
	r.byRef = nil
}

func (t *gpuTexture) release() {
	if t.bindGroup != nil {
		t.bindGroup.Release()
	}
	if t.view != nil {
		t.view.Release()
	}
	if t.texture != nil {
		t.texture.Release()
	}
}

// premultiplyAlpha scales each texel's rgb by its own alpha, in place. Operates
// on a tightly packed RGBA8 byte slice (4 bytes/texel).
func premultiplyAlpha(pix []byte) {
	for i := 0; i+3 < len(pix); i += 4 {
		a := uint32(pix[i+3])
		pix[i+0] = byte(uint32(pix[i+0]) * a / 255)
		pix[i+1] = byte(uint32(pix[i+1]) * a / 255)
		pix[i+2] = byte(uint32(pix[i+2]) * a / 255)
	}
}

// mipLevelCount returns the number of mip levels for a w×h texture: one per
// halving down to 1×1 (the standard floor(log2(max(w,h)))+1).
func mipLevelCount(w, h int) int {
	n := 1
	for w > 1 || h > 1 {
		w, h = max(1, w/2), max(1, h/2)
		n++
	}
	return n
}

// downsample box-filters a packed RGBA8 image from sw×sh to dw×dh (each roughly
// half), averaging 2×2 source texels per destination texel. The input must be
// premultiplied so the average is a correct color resample. Odd source
// dimensions clamp the far sample to the last row/column.
func downsample(src []byte, sw, sh, dw, dh int) []byte {
	dst := make([]byte, dw*dh*4)
	for y := 0; y < dh; y++ {
		sy0 := y * 2
		sy1 := min(sy0+1, sh-1)
		for x := 0; x < dw; x++ {
			sx0 := x * 2
			sx1 := min(sx0+1, sw-1)
			i00 := (sy0*sw + sx0) * 4
			i01 := (sy0*sw + sx1) * 4
			i10 := (sy1*sw + sx0) * 4
			i11 := (sy1*sw + sx1) * 4
			d := (y*dw + x) * 4
			for c := 0; c < 4; c++ {
				sum := uint32(src[i00+c]) + uint32(src[i01+c]) + uint32(src[i10+c]) + uint32(src[i11+c])
				dst[d+c] = byte((sum + 2) / 4)
			}
		}
	}
	return dst
}

// toRGBA returns img as an *image.RGBA with its origin at (0,0), copying when
// the input isn't already a zero-origin RGBA.
func toRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok && rgba.Rect.Min == (image.Point{}) {
		return rgba
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}
