package render3d

import (
	"fmt"
	"image"
	"image/draw"
	"math"

	"github.com/cogentcore/webgpu/wgpu"
)

// texture is one uploaded 2D texture and its view. Materials hold views; the
// registry holds a few shared defaults. Mip levels are generated on the CPU
// (this wgpu binding ships no GPU mip generator) by box-downsampling — averaged
// in linear space for sRGB maps — so minified surfaces no longer alias.
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
		// Allow sampling the full generated mip chain (uploadNRGBA builds all
		// levels). 32 covers textures up to 2^31 on a side.
		LodMaxClamp:   32,
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
	levels := mipLevelCount(w, h)
	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "spliti.render3d.texture",
		Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
		Dimension:     wgpu.TextureDimension2D,
		Size:          wgpu.Extent3D{Width: w, Height: h, DepthOrArrayLayers: 1},
		Format:        format,
		MipLevelCount: levels,
		SampleCount:   1,
	})
	if err != nil {
		return nil, err
	}
	// NRGBA may have Stride > 4*w; repack tightly when needed so BytesPerRow is
	// exact (WebGPU requires BytesPerRow to match the copy region for a 1-row
	// image and to be the actual row pitch otherwise). queue.WriteTexture has no
	// 256-byte BytesPerRow alignment requirement, so 4*w is valid per level.
	data := img.Pix
	if img.Stride != int(4*w) {
		data = make([]byte, 4*w*h)
		for y := 0; y < int(h); y++ {
			copy(data[y*int(4*w):], img.Pix[y*img.Stride:y*img.Stride+int(4*w)])
		}
	}
	// Upload level 0, then box-downsample to each successive level and upload it.
	lw, lh := w, h
	for level := uint32(0); ; level++ {
		if err = g.queue.WriteTexture(
			&wgpu.ImageCopyTexture{Texture: tex, MipLevel: level, Origin: wgpu.Origin3D{}, Aspect: wgpu.TextureAspectAll},
			data,
			&wgpu.TextureDataLayout{Offset: 0, BytesPerRow: 4 * lw, RowsPerImage: lh},
			&wgpu.Extent3D{Width: lw, Height: lh, DepthOrArrayLayers: 1},
		); err != nil {
			tex.Release()
			return nil, err
		}
		if level+1 >= levels {
			break
		}
		data, lw, lh = downsampleRGBA(data, lw, lh, srgb)
	}
	view, err := tex.CreateView(nil)
	if err != nil {
		tex.Release()
		return nil, err
	}
	return &texture{tex: tex, view: view}, nil
}

// mipLevelCount returns the full mip-chain length for a w×h texture: levels
// halve each dimension (flooring, min 1) until both reach 1.
func mipLevelCount(w, h uint32) uint32 {
	n := uint32(1)
	for w > 1 || h > 1 {
		if w > 1 {
			w >>= 1
		}
		if h > 1 {
			h >>= 1
		}
		n++
	}
	return n
}

// srgbToLinearLUT maps an 8-bit sRGB channel to linear [0,1]; built once.
var srgbToLinearLUT = func() [256]float64 {
	var lut [256]float64
	for i := 0; i < 256; i++ {
		s := float64(i) / 255
		if s <= 0.04045 {
			lut[i] = s / 12.92
		} else {
			lut[i] = math.Pow((s+0.055)/1.055, 2.4)
		}
	}
	return lut
}()

// linearToSRGB encodes a linear [0,1] value to an 8-bit sRGB channel.
func linearToSRGB(l float64) uint8 {
	var s float64
	if l <= 0.0031308 {
		s = l * 12.92
	} else {
		s = 1.055*math.Pow(l, 1/2.4) - 0.055
	}
	v := s*255 + 0.5
	if v < 0 {
		v = 0
	} else if v > 255 {
		v = 255
	}
	return uint8(v)
}

// downsampleRGBA box-filters a tightly packed RGBA8 image to the next mip level
// (dimensions halved, flooring, min 1). For an sRGB image the RGB channels are
// averaged in linear space then re-encoded; alpha is always linear. Linear
// images (normal / metallic-roughness maps) are averaged directly.
func downsampleRGBA(src []byte, sw, sh uint32, srgb bool) ([]byte, uint32, uint32) {
	dw := sw / 2
	if dw < 1 {
		dw = 1
	}
	dh := sh / 2
	if dh < 1 {
		dh = 1
	}
	dst := make([]byte, 4*dw*dh)
	for dy := uint32(0); dy < dh; dy++ {
		// Clamp the 2×2 source footprint to bounds so odd dimensions stay valid.
		y0 := dy * 2
		y1 := min32(y0+1, sh-1)
		for dx := uint32(0); dx < dw; dx++ {
			x0 := dx * 2
			x1 := min32(x0+1, sw-1)
			o00 := 4 * (y0*sw + x0)
			o01 := 4 * (y0*sw + x1)
			o10 := 4 * (y1*sw + x0)
			o11 := 4 * (y1*sw + x1)
			di := 4 * (dy*dw + dx)
			if srgb {
				for ch := uint32(0); ch < 3; ch++ {
					l := (srgbToLinearLUT[src[o00+ch]] + srgbToLinearLUT[src[o01+ch]] +
						srgbToLinearLUT[src[o10+ch]] + srgbToLinearLUT[src[o11+ch]]) / 4
					dst[di+ch] = linearToSRGB(l)
				}
			} else {
				for ch := uint32(0); ch < 3; ch++ {
					dst[di+ch] = avg4(src[o00+ch], src[o01+ch], src[o10+ch], src[o11+ch])
				}
			}
			dst[di+3] = avg4(src[o00+3], src[o01+3], src[o10+3], src[o11+3])
		}
	}
	return dst, dw, dh
}

func min32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

func avg4(a, b, c, d byte) byte {
	return byte((uint32(a) + uint32(b) + uint32(c) + uint32(d) + 2) / 4)
}
