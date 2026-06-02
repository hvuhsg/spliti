// Package canvas provides a truecolor RGB pixel framebuffer that blits to a
// tcell screen using upper-half-block runes. Because each terminal cell can
// show two colors (foreground + background), drawing '▀' with the top pixel as
// the foreground and the bottom pixel as the background packs two vertical
// pixels into one cell — doubling the effective vertical resolution.
//
// Canvas is deliberately decoupled from the ECS: it depends only on tcell, so
// any game (or overlay) that wants smooth, colorful, sub-row rendering can use
// it. The raycaster in examples/doom is the motivating use case.
package canvas

import "github.com/gdamore/tcell/v2"

// upperHalfBlock fills the top half of a cell; the bottom half shows through as
// the cell's background color.
const upperHalfBlock = '▀'

// RGB is a 24-bit color. tcell down-samples to the terminal's palette on
// screens that don't support truecolor.
type RGB struct{ R, G, B uint8 }

// Canvas is a CW x PH grid of pixels. PH ("pixel height") is always even and
// equals twice the number of terminal rows the canvas occupies, since each cell
// row holds two pixel rows.
type Canvas struct {
	CW, PH int   // width in pixels (== cells wide), height in pixels (== 2*rows)
	Pix    []RGB // row-major, len == CW*PH
}

// New allocates a canvas CW pixels wide and cellRows*2 pixels tall.
func New(cw, cellRows int) *Canvas {
	ph := cellRows * 2
	if cw < 0 {
		cw = 0
	}
	if ph < 0 {
		ph = 0
	}
	return &Canvas{CW: cw, PH: ph, Pix: make([]RGB, cw*ph)}
}

// Resize reallocates the pixel buffer when dimensions change; it's a no-op when
// the size is unchanged, so callers can invoke it every frame cheaply.
func (c *Canvas) Resize(cw, cellRows int) {
	ph := cellRows * 2
	if cw == c.CW && ph == c.PH {
		return
	}
	if cw < 0 {
		cw = 0
	}
	if ph < 0 {
		ph = 0
	}
	c.CW, c.PH = cw, ph
	c.Pix = make([]RGB, cw*ph)
}

// Clear fills the whole buffer with col.
func (c *Canvas) Clear(col RGB) {
	for i := range c.Pix {
		c.Pix[i] = col
	}
}

// Set writes one pixel. Out-of-bounds coordinates are ignored.
func (c *Canvas) Set(x, y int, col RGB) {
	if x < 0 || y < 0 || x >= c.CW || y >= c.PH {
		return
	}
	c.Pix[y*c.CW+x] = col
}

// VLine fills the inclusive vertical span [y0,y1] in column x with col. The
// endpoints are clamped to the buffer, so callers may pass spans that overflow
// the top or bottom.
func (c *Canvas) VLine(x, y0, y1 int, col RGB) {
	if x < 0 || x >= c.CW {
		return
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	if y0 < 0 {
		y0 = 0
	}
	if y1 >= c.PH {
		y1 = c.PH - 1
	}
	for y := y0; y <= y1; y++ {
		c.Pix[y*c.CW+x] = col
	}
}

// Blit pushes the pixel buffer to the screen at cell offset (offX, offY). It
// writes to the back buffer only — the caller (or the tui present system) is
// responsible for Screen.Show. Each cell maps pixel row 2*cy to the foreground
// and 2*cy+1 to the background of an upper-half-block glyph.
func (c *Canvas) Blit(s tcell.Screen, offX, offY int) {
	rows := c.PH / 2
	for cy := 0; cy < rows; cy++ {
		topRow := (2 * cy) * c.CW
		botRow := (2*cy + 1) * c.CW
		for x := 0; x < c.CW; x++ {
			top := c.Pix[topRow+x]
			bot := c.Pix[botRow+x]
			style := tcell.StyleDefault.
				Foreground(tcell.NewRGBColor(int32(top.R), int32(top.G), int32(top.B))).
				Background(tcell.NewRGBColor(int32(bot.R), int32(bot.G), int32(bot.B)))
			s.SetContent(offX+x, offY+cy, upperHalfBlock, nil, style)
		}
	}
}
