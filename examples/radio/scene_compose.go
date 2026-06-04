package main

import (
	"fmt"
	"math"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
)

// Compose scene: type your own message and watch its wave appear. Each character
// becomes 8 bits, every 2 bits become a QPSK symbol, and the symbols are drawn as
// one continuous wave scaled to fit — so a longer message packs more (and faster)
// wiggles into the same space. This is the whole transmit chain, driven live by
// the keyboard.

const (
	cmpWave   = 0 // the message waveform
	cmpPlay   = 1 // playhead
	cmpCursor = 2 // the text cursor

	cmpMaxChars  = 18
	cmpWaveN     = 1800 // dense enough that even a full, fast message stays smooth
	cmpCyclesPer = 1.5  // carrier cycles per symbol slot

	cmpFieldX = 14.0 // left edge of the text field
	cmpCellW  = 6.0  // width of one character cell
	cmpFieldY = 21.0 // vertical centre of the text row
)

var (
	cmpWaveVP = viewport{X: 8, Y: 44, W: 144, H: 28}
	cmpWaveDt = dataRect{X0: 0, X1: 1, Y0: -1.5, Y1: 1.5} // headroom for 16-QAM peaks
)

func cellRef(i int) string { return fmt.Sprintf("cmp:c%d", i) }

// modName names the modulation for a given bits-per-symbol.
func modName(bps int) string {
	switch bps {
	case 3:
		return "8-PSK"
	case 4:
		return "16-QAM"
	default:
		return "QPSK"
	}
}

// mapBits turns one group of `bps` bits into a constellation point. 2 bits ->
// QPSK, 3 bits -> 8-PSK (8 phases on the unit circle), 4 bits -> 16-QAM.
func mapBits(bps, g int) Symbol {
	switch bps {
	case 3:
		ang := float64(g) * (2 * math.Pi / 8)
		return Symbol{I: math.Cos(ang), Q: math.Sin(ang)}
	case 4:
		return qam16Point((g>>2)&3, g&3)
	default:
		return qpskMap((g>>1)&1, g&1)
	}
}

// composeSymbols turns the typed text into constellation points: the bit stream
// (8 bits per character, MSB first) grouped into chunks of `bps` bits, each
// mapped through the current modulation. A trailing partial group is zero-padded.
func composeSymbols(ui *uiState) []Symbol {
	bps := ui.bps
	var stream []int
	for _, ch := range []byte(ui.compose) {
		for b := 7; b >= 0; b-- {
			stream = append(stream, int(ch>>uint(b))&1)
		}
	}
	var syms []Symbol
	for i := 0; i < len(stream); i += bps {
		g := 0
		for j := 0; j < bps; j++ {
			g <<= 1
			if i+j < len(stream) {
				g |= stream[i+j]
			}
		}
		syms = append(syms, mapBits(bps, g))
	}
	return syms
}

func setupCompose(c *app.Ctx) {
	cmds := c.Commands()
	ui := app.GetResource[uiState](c)
	if ui.bps < 2 || ui.bps > 4 {
		ui.bps = 2
	}

	spawnBackButton(c)
	spawnLabel(c, "cmp:title", "WRITE A MESSAGE", worldW/2, 8, 5, colText, zLabel)
	spawnLabelLeft(c, "cmp:prompt", "type a message:", cmpFieldX, 15, 3, colMuted, zLabel)

	// Text field: a panel, one label per character cell, and a blinking cursor.
	spawnSprite(cmds, "sq", cmpFieldX-2, 17, cmpCellW*cmpMaxChars+4, 8, colPanel, zPanel)
	for i := 0; i < cmpMaxChars; i++ {
		spawnLabel(c, cellRef(i), " ", cmpFieldX+cmpCellW*(float32(i)+0.5), cmpFieldY, 5, colText, zLabel)
	}
	spawnMarkers(cmds, cmpCursor, 1, "sq", 0.6, 7, colHi, zLabel)

	spawnLabelLeft(c, "cmp:info", " 0 chars =   0 bits =   0 symbols", cmpFieldX, 30, 3, colSum, zLabel)
	spawnLabelLeft(c, "cmp:mode", "modulation: QPSK    2 bits/symbol", cmpFieldX, 36, 3, colI, zLabel)

	// Waveform panel + zero axis + the wave + a playhead.
	spawnSprite(cmds, "sq", cmpWaveVP.X-2, cmpWaveVP.Y-2, cmpWaveVP.W+4, cmpWaveVP.H+4, colPanel, zPanel)
	_, zy := cmpWaveVP.mapPoint(cmpWaveDt, 0, 0)
	spawnSprite(cmds, "sq", cmpWaveVP.X, zy-0.1, cmpWaveVP.W, 0.2, colAxis, zAxis)
	spawnSeries(cmds, cmpWave, cmpWaveN, 1.0, colSum)
	spawnMarkers(cmds, cmpPlay, 1, "sq", 0.4, cmpWaveVP.H+3, colHi, zLabel)

	spawnLabel(c, "cmp:note", "your text -> bits -> symbols -> this wave", worldW/2, 76, 2.8, colMuted, zLabel)
	spawnLabel(c, "cmp:hint", "type to edit    Backspace: delete    Up/Down: bits per symbol    Esc: back", worldW/2, 88, 2.6, colMuted, zLabel)

	bakeComposeCells(c, ui)
	bakeComposeInfo(c, ui)
	bakeComposeMode(c, ui)
}

// bakeComposeCells re-renders each character cell (a space where empty).
func bakeComposeCells(c *app.Ctx, ui *uiState) {
	for i := 0; i < cmpMaxChars; i++ {
		ch := " "
		if i < len(ui.compose) {
			ch = string(ui.compose[i])
		}
		loadLabel(c, cellRef(i), ch)
	}
}

// bakeComposeInfo re-renders the chars/bits/symbols counter (fixed widths so the
// sprite geometry stays constant). The symbol count depends on the modulation.
func bakeComposeInfo(c *app.Ctx, ui *uiState) {
	n := len(ui.compose)
	nSym := (n*8 + ui.bps - 1) / ui.bps
	loadLabel(c, "cmp:info", fmt.Sprintf("%2d chars = %3d bits = %3d symbols", n, n*8, nSym))
}

// bakeComposeMode re-renders the modulation label (fixed width via %-6s).
func bakeComposeMode(c *app.Ctx, ui *uiState) {
	loadLabel(c, "cmp:mode", fmt.Sprintf("modulation: %-6s  %d bits/symbol", modName(ui.bps), ui.bps))
}

func composeInput(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	textCh, modeCh := false, false
	for _, ev := range app.ReadEvents[webgpu.KeyEvent](c) {
		pressed := ev.Action == glfw.Press || ev.Action == glfw.Repeat
		switch {
		case ev.Rune >= 32 && ev.Rune < 127: // a printable character was typed
			if len(ui.compose) < cmpMaxChars {
				ui.compose += string(ev.Rune)
				textCh = true
			}
		case ev.Key == glfw.KeyBackspace && pressed:
			if len(ui.compose) > 0 {
				ui.compose = ui.compose[:len(ui.compose)-1]
				textCh = true
			}
		case ev.Key == glfw.KeyUp && pressed:
			if ui.bps < 4 {
				ui.bps++
				modeCh = true
			}
		case ev.Key == glfw.KeyDown && pressed:
			if ui.bps > 2 {
				ui.bps--
				modeCh = true
			}
		}
	}
	if textCh {
		bakeComposeCells(c, ui)
	}
	if textCh || modeCh {
		bakeComposeInfo(c, ui)
	}
	if modeCh {
		bakeComposeMode(c, ui)
	}
}

func composeDraw(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	now := elapsed(c)
	syms := composeSymbols(ui)
	n := len(syms)

	// The full message waveform, scaled to fit the panel width.
	placeSeries(c, cmpWave, func(i int) (float32, float32) {
		u := float64(i) / float64(cmpWaveN-1)
		v := 0.0
		if n > 0 {
			k := int(u * float64(n))
			if k >= n {
				k = n - 1
			}
			local := u*float64(n) - float64(k)
			v = passband(syms[k], cmpCyclesPer, local)
		}
		return cmpWaveVP.mapPoint(cmpWaveDt, u, clampf(v, -1.49, 1.49))
	})

	// Playhead sweeps the message (hidden when empty).
	if n > 0 {
		loop := math.Max(1.5, float64(n)*0.25)
		prog := math.Mod(now, loop) / loop
		px := cmpWaveVP.X + float32(prog)*cmpWaveVP.W
		placeSeries(c, cmpPlay, func(int) (float32, float32) { return px, cmpWaveVP.Y + cmpWaveVP.H/2 })
	} else {
		placeSeries(c, cmpPlay, func(int) (float32, float32) { return -50, -50 })
	}

	// Blinking text cursor at the end of the typed text.
	curX := cmpFieldX + cmpCellW*float32(len(ui.compose))
	placeSeries(c, cmpCursor, func(int) (float32, float32) { return curX, cmpFieldY })
	blink := math.Mod(now, 1.0) < 0.5
	recolorSeries(c, cmpCursor, func(int) webgpu.Color {
		if blink {
			return colHi
		}
		return webgpu.Color{R: 1, G: 1, B: 1, A: 0} // transparent (zero Color would be opaque white)
	})
}
