// Package screenshot saves the rendered frame to an image file. It is a thin
// consumer of a render backend's frame read-back primitive
// (render3d.CaptureFrame): the backend hands back raw RGBA pixels and this
// package owns the file-format and filesystem concerns (PNG encoding, paths), so
// the renderer stays free of image/os dependencies.
//
// It needs no plugin setup — there is no state to install — so use it directly:
//
//	screenshot.Save(c, "frame.png")
//
// The file is written during the next frame's present; Save returns immediately
// after registering the request.
package screenshot

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
)

// Save requests that the next presented frame be written to path as a PNG. It
// reports whether the request was registered (false when the renderer is not
// ready); any encode/write error is reported asynchronously to stderr, since the
// file is produced later, at present time.
func Save(c *app.Ctx, path string) bool {
	return render3d.CaptureFrame(c, func(pix []byte, w, h int) {
		img := &image.RGBA{
			Pix:    pix,
			Stride: 4 * w,
			Rect:   image.Rect(0, 0, w, h),
		}
		if err := writePNG(path, img); err != nil {
			fmt.Fprintln(os.Stderr, "screenshot:", err)
		}
	})
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
