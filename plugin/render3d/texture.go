package render3d

import (
	"fmt"
	"image"
	"image/draw"

	"github.com/cogentcore/webgpu/wgpu"
)

// texture is one uploaded 2D texture and its view. Materials hold views; the
// registry holds a few shared defaults. No mipmaps are generated (this wgpu
// binding ships no mip generator), so all textures are single-level.
type texture struct {
	tex  *wgpu.Texture
	view *wgpu.TextureView
}

func (t *texture) release() {
	if t == nil {
		return
	}
	if t.view != nil {
		t.view.Release()
	}
	if t.tex != nil {
		t.tex.Release()
	}
}

// defaultTextures are the 1x1 fallbacks bound when a material lacks a given map,
// plus the shared sampler. They are created once per GPU and never released by
// individual materials (only by releaseAll). White base color and white
// metallic-roughness make the texture a no-op multiplier (the uniform values
// pass through); the flat normal (0,0,1 encoded as 128,128,255) leaves the
// geometric normal unchanged.
type defaultTextures struct {
	white    *texture // sRGB white, for missing base color
	whiteLin *texture // linear white, for missing metallic-roughness
	flatNorm *texture // linear (128,128,255), for missing normal map
	sampler  *wgpu.Sampler
}

func newDefaultTextures(g *GPU) *defaultTextures {
	d := &defaultTextures{
		white:    solidTexture(g, 255, 255, 255, 255, true),
		whiteLin: solidTexture(g, 255, 255, 255, 255, false),
		flatNorm: solidTexture(g, 128, 128, 255, 255, false),
	}
	s, err := g.device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:         "spliti.render3d.sampler",
		AddressModeU:  wgpu.AddressModeRepeat,
		AddressModeV:  wgpu.AddressModeRepeat,
		AddressModeW:  wgpu.AddressModeRepeat,
		MagFilter:     wgpu.FilterModeLinear,
		MinFilter:     wgpu.FilterModeLinear,
		MipmapFilter:  wgpu.MipmapFilterModeLinear,
		LodMaxClamp:   1,
		MaxAnisotropy: 1,
	})
	if err != nil {
		panic("render3d: default sampler: " + err.Error())
	}
	d.sampler = s
	return d
}

func (d *defaultTextures) release() {
	if d == nil {
		return
	}
	d.white.release()
	d.whiteLin.release()
	d.flatNorm.release()
	if d.sampler != nil {
		d.sampler.Release()
	}
}

// solidTexture creates a 1x1 texture of the given RGBA color. srgb selects the
// sRGB color format (for base color, hardware-linearized on sample) vs. linear.
func solidTexture(g *GPU, r, gg, b, a uint8, srgb bool) *texture {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3] = r, gg, b, a
	t, err := uploadNRGBA(g, img, srgb)
	if err != nil {
		panic("render3d: solid texture: " + err.Error())
	}
	return t
}

// uploadTexture decodes an arbitrary image.Image into an RGBA8 GPU texture.
// srgb=true uses the sRGB format for base-color maps; metallic-roughness and
// normal maps must pass srgb=false (linear data).
func uploadTexture(g *GPU, img image.Image, srgb bool) (*texture, error) {
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		b := img.Bounds()
		nrgba = image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(nrgba, nrgba.Bounds(), img, b.Min, draw.Src)
	}
	return uploadNRGBA(g, nrgba, srgb)
}

// uploadNRGBA uploads an NRGBA image (tightly packed, 4 bytes/pixel) as a 2D
// texture and returns it with a default view.
func uploadNRGBA(g *GPU, img *image.NRGBA, srgb bool) (*texture, error) {
	w := uint32(img.Bounds().Dx())
	h := uint32(img.Bounds().Dy())
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("render3d: empty texture")
	}
	format := wgpu.TextureFormatRGBA8Unorm
	if srgb {
		format = wgpu.TextureFormatRGBA8UnormSrgb
	}
	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "spliti.render3d.texture",
		Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
		Dimension:     wgpu.TextureDimension2D,
		Size:          wgpu.Extent3D{Width: w, Height: h, DepthOrArrayLayers: 1},
		Format:        format,
		MipLevelCount: 1,
		SampleCount:   1,
	})
	if err != nil {
		return nil, err
	}
	// NRGBA may have Stride > 4*w; repack tightly when needed so BytesPerRow is
	// exact (WebGPU requires BytesPerRow to match the copy region for a 1-row
	// image and to be the actual row pitch otherwise).
	data := img.Pix
	if img.Stride != int(4*w) {
		data = make([]byte, 4*w*h)
		for y := 0; y < int(h); y++ {
			copy(data[y*int(4*w):], img.Pix[y*img.Stride:y*img.Stride+int(4*w)])
		}
	}
	err = g.queue.WriteTexture(
		&wgpu.ImageCopyTexture{Texture: tex, MipLevel: 0, Origin: wgpu.Origin3D{}, Aspect: wgpu.TextureAspectAll},
		data,
		&wgpu.TextureDataLayout{Offset: 0, BytesPerRow: 4 * w, RowsPerImage: h},
		&wgpu.Extent3D{Width: w, Height: h, DepthOrArrayLayers: 1},
	)
	if err != nil {
		tex.Release()
		return nil, err
	}
	view, err := tex.CreateView(nil)
	if err != nil {
		tex.Release()
		return nil, err
	}
	return &texture{tex: tex, view: view}, nil
}
