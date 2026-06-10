package render3d

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/app"
)

// copyAlign is the required row alignment (bytes) for a texture-to-buffer copy:
// WebGPU mandates BytesPerRow be a multiple of 256.
const copyAlign = 256

// FrameSink receives one read-back frame: tightly packed, top-left-origin RGBA8
// pixels (len == 4*w*h) plus the dimensions. The slice is only valid for the
// duration of the call — copy out anything you need to keep (PNG encoders read it
// synchronously, so that is fine).
type FrameSink func(pix []byte, w, h int)

// CaptureFrame reads back the next fully-composited frame (3D scene + gizmos +
// 2D overlay panels, exactly as presented) and delivers its pixels to sink
// exactly once, during that frame's present. This is the low-level primitive;
// PNG encoding and file output live in a separate package (plugin/screenshot) so
// this renderer stays free of image/file concerns.
//
// Read-back works because the surface texture is configured with COPY_SRC (see
// Build): the present pass renders into it, then presentSystem copies it to a
// mappable buffer before presenting and invokes the sink with the result.
func CaptureFrame(c *app.Ctx, sink FrameSink) bool {
	g := app.GetResource[GPU](c)
	if g == nil || sink == nil {
		return false
	}
	g.captureSink = sink
	return true
}

// captureBuffer holds the in-flight read-back state between recording the copy
// (before the command buffer is finished) and reading it back (after submit).
type captureBuffer struct {
	buf       *wgpu.Buffer
	w, h      uint32
	bytesRow  uint32 // padded bytes per row in the buffer
	srcFormat wgpu.TextureFormat
}

// recordCapture allocates the read-back buffer and records a texture-to-buffer
// copy of the just-rendered surface texture into enc. Call after the render pass
// has ended but before the encoder is finished. Returns nil on failure (read-back
// is best-effort and never blocks presentation).
func recordCapture(g *GPU, enc *wgpu.CommandEncoder) *captureBuffer {
	if g.curTex == nil {
		return nil
	}
	w, h := g.config.Width, g.config.Height
	bytesRow := (w*4 + copyAlign - 1) / copyAlign * copyAlign
	buf, err := g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "spliti.render3d.framebuffer.readback",
		Size:  uint64(bytesRow * h),
		Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead,
	})
	if err != nil {
		return nil
	}
	err = enc.CopyTextureToBuffer(
		&wgpu.ImageCopyTexture{
			Texture: g.curTex,
			Origin:  wgpu.Origin3D{},
			Aspect:  wgpu.TextureAspectAll,
		},
		&wgpu.ImageCopyBuffer{
			Buffer: buf,
			Layout: wgpu.TextureDataLayout{Offset: 0, BytesPerRow: bytesRow, RowsPerImage: h},
		},
		&wgpu.Extent3D{Width: w, Height: h, DepthOrArrayLayers: 1},
	)
	if err != nil {
		buf.Release()
		return nil
	}
	return &captureBuffer{buf: buf, w: w, h: h, bytesRow: bytesRow, srcFormat: g.format}
}

// deliver maps the read-back buffer (which the submitted copy has filled),
// converts it to tightly packed RGBA8, and hands it to sink. Must be called after
// the copy command has been submitted. Blocks until the GPU copy completes.
func (cb *captureBuffer) deliver(g *GPU, sink FrameSink) {
	defer cb.buf.Release()

	size := uint64(cb.bytesRow * cb.h)
	mapped, ok := false, false
	cb.buf.MapAsync(wgpu.MapModeRead, 0, size, func(status wgpu.BufferMapAsyncStatus) {
		ok = status == wgpu.BufferMapAsyncStatusSuccess
		mapped = true
	})
	for !mapped {
		g.device.Poll(true, nil)
	}
	if !ok {
		return
	}
	defer cb.buf.Unmap()

	data := cb.buf.GetMappedRange(0, uint(size))
	w, h := int(cb.w), int(cb.h)
	pix := make([]byte, 4*w*h)
	// BGRA swizzle for the common Metal/Vulkan surface format; the bytes are
	// already sRGB-encoded (the swapchain format is *Srgb), which is exactly what
	// a PNG expects, so no gamma conversion is needed.
	bgra := cb.srcFormat == wgpu.TextureFormatBGRA8UnormSrgb
	for y := 0; y < h; y++ {
		src := data[y*int(cb.bytesRow):]
		dst := pix[y*4*w:]
		for x := 0; x < w; x++ {
			i := x * 4
			r, gn, b, a := src[i], src[i+1], src[i+2], src[i+3]
			if bgra {
				r, b = b, r
			}
			dst[i], dst[i+1], dst[i+2], dst[i+3] = r, gn, b, a
		}
	}
	sink(pix, w, h)
}
