// Package text rasterizes strings to images on the CPU, so the GPU backends
// can draw text without a font engine of their own: render the string once
// (or when it changes), upload it as a panel/texture, and blit it each frame.
// With render3d that's one call — render3d.LoadTextPanel — feeding the
// existing Overlay2D path; the resulting image also works anywhere else an
// image.Image is accepted.
//
// The package wraps golang.org/x/image/font with the Go fonts embedded, so it
// needs no font files, works on every build target (including GOOS=js), and
// accepts any TTF/OTF via NewFace for games that ship their own typeface.
//
// Rasterizing is not free — cache the image and rebuild only when the string
// changes, exactly like Overlay2D.LoadPanel asks. For fully dynamic per-frame
// UI text consider plugin/ui (Dear ImGui), which has its own font atlas.
package text

import (
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Face is a font loaded at a fixed size, ready to measure and render strings.
// Create one per (font, size) pair and reuse it; creation parses font tables.
// Like the rest of the engine it is meant for single-threaded use.
type Face struct {
	face       font.Face
	ascent     int
	lineHeight int
}

// NewFace parses TTF/OTF bytes and returns a Face at the given size in pixels
// (more precisely points at 72 DPI, which makes 1pt = 1px).
func NewFace(ttf []byte, size float64) (*Face, error) {
	f, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size: size, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	m := face.Metrics()
	return &Face{
		face:       face,
		ascent:     m.Ascent.Ceil(),
		lineHeight: m.Height.Ceil(),
	}, nil
}

// Default returns the embedded Go Regular face at the given pixel size. It
// cannot fail: the font is compiled in.
func Default(size float64) *Face {
	f, err := NewFace(goregular.TTF, size)
	if err != nil {
		panic("text: parse embedded goregular: " + err.Error()) // unreachable
	}
	return f
}

// Mono returns the embedded Go Mono face at the given pixel size, for
// readouts where digits must align.
func Mono(size float64) *Face {
	f, err := NewFace(gomono.TTF, size)
	if err != nil {
		panic("text: parse embedded gomono: " + err.Error()) // unreachable
	}
	return f
}

// LineHeight returns the vertical advance between baselines, in pixels.
func (f *Face) LineHeight() int { return f.lineHeight }

// Measure returns the pixel size of the image Render would produce for s.
// Newlines start new lines; the width is the widest line's.
func (f *Face) Measure(s string) (w, h int) {
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		if lw := font.MeasureString(f.face, line).Ceil(); lw > w {
			w = lw
		}
	}
	return w, len(lines) * f.lineHeight
}

// Render rasterizes s in the given color onto a transparent background,
// left-aligned, one line per newline. The result is sized by Measure and
// ready for render3d.LoadTextPanel / Overlay2D, or any image consumer.
// Rendering "" (or only newlines) yields a 1px-wide transparent image so
// callers don't have to special-case empty strings against zero-size panels.
func (f *Face) Render(s string, col color.Color) *image.RGBA {
	w, h := f.Measure(s)
	if w == 0 {
		w = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	d := font.Drawer{Dst: img, Src: image.NewUniform(col), Face: f.face}
	y := f.ascent
	for _, line := range strings.Split(s, "\n") {
		d.Dot = fixed.P(0, y)
		d.DrawString(line)
		y += f.lineHeight
	}
	return img
}
