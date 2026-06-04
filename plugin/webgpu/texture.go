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

	extent := wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1}
	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "spliti.webgpu.tex." + ref,
		Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
		Dimension:     wgpu.TextureDimension2D,
		Size:          extent,
		Format:        wgpu.TextureFormatRGBA8UnormSrgb,
		MipLevelCount: 1,
		SampleCount:   1,
	})
	if err != nil {
		return fmt.Errorf("webgpu: create texture %q: %w", ref, err)
	}

	if err := g.queue.WriteTexture(
		tex.AsImageCopy(),
		wgpu.ToBytes(rgba.Pix),
		&wgpu.TextureDataLayout{
			Offset:       0,
			BytesPerRow:  uint32(4 * w),
			RowsPerImage: uint32(h),
		},
		&extent,
	); err != nil {
		tex.Release()
		return fmt.Errorf("webgpu: write texture %q: %w", ref, err)
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
