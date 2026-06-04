package main

import (
	"fmt"
	"math"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
)

// Message scene: putting it all together. A short text becomes a stream of bits,
// the bits are grouped into pairs, each pair becomes one QPSK symbol, and the
// symbols are sent back to back as one continuous wave — one symbol per time
// slot. A playhead sweeps the wave; as it crosses each slot the matching bit
// pair, constellation point, and (clean) decoded character light up, so you can
// watch a real message stream out and be rebuilt at the far end.

// All presets are 2 characters = 16 bits = 8 symbols, so the on-screen layout is
// fixed and only the values change when you switch messages.
var msgPresets = []string{"HI", "OK", "73", "GO"}

const (
	msgNsym      = 8   // symbols in a message (2 chars x 8 bits / 2)
	msgCyclesPer = 2.0 // carrier cycles drawn per symbol slot
	msgLoopSecs  = 5.0 // time for the playhead to cross the whole message
	msgWaveN     = 230

	// dynamic series
	mgWave  = 0 // the full transmitted waveform
	mgPlay  = 1 // the moving playhead
	mgSegHi = 2 // highlight box over the current symbol's wave slot
	mgBitHi = 3 // highlight box over the current bit pair
	mgCur   = 4 // the current symbol's constellation point
)

var (
	mgWaveVP    = viewport{X: 8, Y: 24, W: 144, H: 24}
	mgWaveData  = dataRect{X0: 0, X1: 1, Y0: -1.2, Y1: 1.2}
	mgPlaneVP   = viewport{X: 8, Y: 56, W: 26, H: 26}
	mgPlaneData = dataRect{X0: -1.5, X1: 1.5, Y0: -1.5, Y1: 1.5}
)

// msgPrevSym tracks the last slot the readouts were baked for (one scene at a
// time, so a package var is fine). -1 forces a refresh.
var msgPrevSym int

func curMessage(ui *uiState) string { return msgPresets[ui.msgIdx%len(msgPresets)] }

// msgSymbols returns the QPSK symbol indices for the current message (MSB-first
// bit pairs of each ASCII character).
func msgSymbols(ui *uiState) []int {
	syms := make([]int, 0, msgNsym)
	for _, ch := range []byte(curMessage(ui)) {
		for b := 6; b >= 0; b -= 2 {
			syms = append(syms, indexOf(int(ch>>uint(b+1))&1, int(ch>>uint(b))&1))
		}
	}
	return syms
}

func segW() float32            { return mgWaveVP.W / float32(msgNsym) }
func segCenterX(k int) float32 { return mgWaveVP.X + (float32(k)+0.5)*segW() }

func setupMessage(c *app.Ctx) {
	cmds := c.Commands()
	ui := app.GetResource[uiState](c)
	msgPrevSym = -1

	spawnBackButton(c)
	spawnLabel(c, "mg:title", "SENDING A MESSAGE", worldW/2, 8, 5, colText, zLabel)

	// Per-character labels (each spans 4 symbol slots).
	spawnLabel(c, "mg:c0", "?", segCenterX(1)+segW()/2, 13, 4.5, colAccent, zLabel)
	spawnLabel(c, "mg:c1", "?", segCenterX(5)+segW()/2, 13, 4.5, colAccent, zLabel)

	// Bit-pair labels (one per symbol slot) + the moving highlight behind them.
	spawnMarkers(cmds, mgBitHi, 1, "sq", segW()*0.86, 5, lowAlpha(colAccent, 0.22), zPanel+1)
	for k := 0; k < msgNsym; k++ {
		spawnLabel(c, "mg:p"+itoa(k), "00", segCenterX(k), 18, 3.2, colText, zLabel)
	}

	// Waveform panel, slot separators, current-slot highlight, the wave, playhead.
	spawnSprite(cmds, "sq", mgWaveVP.X-2, mgWaveVP.Y-2, mgWaveVP.W+4, mgWaveVP.H+4, colPanel, zPanel)
	_, zy := mgWaveVP.mapPoint(mgWaveData, 0, 0)
	spawnSprite(cmds, "sq", mgWaveVP.X, zy-0.1, mgWaveVP.W, 0.2, colAxis, zAxis)
	for k := 1; k < msgNsym; k++ {
		x := mgWaveVP.X + float32(k)*segW()
		spawnSprite(cmds, "sq", x-0.08, mgWaveVP.Y, 0.16, mgWaveVP.H, colGrid, zAxis)
	}
	spawnMarkers(cmds, mgSegHi, 1, "sq", segW(), mgWaveVP.H, lowAlpha(colSum, 0.16), zPanel+1)
	spawnSeries(cmds, mgWave, msgWaveN, 1.3, colSum)
	spawnMarkers(cmds, mgPlay, 1, "sq", 0.4, mgWaveVP.H+3, colHi, zLabel)
	spawnLabelLeft(c, "mg:wl", "one continuous wave, one symbol per slot", mgWaveVP.X, mgWaveVP.Y-2.6, 2.6, colMuted, zLabel)

	// Constellation (the symbols live here) + current point.
	setupIQPlane(c, mgPlaneVP, mgPlaneData, "mg", false)
	spawnSeries(cmds, mgCur, 1, 3.6, colSum)
	spawnLabel(c, "mg:plab", "current symbol", mgPlaneVP.X+mgPlaneVP.W/2, mgPlaneVP.Y+mgPlaneVP.H+4, 2.6, colMuted, zLabel)

	// Right column: live "now sending" + the receiver rebuilding the text.
	const rx = 44.0
	spawnLabelLeft(c, "mg:now", "sending slot 1/8   bits 00   phase 045", rx, 58, 3.2, colText, zLabel)
	spawnLabelLeft(c, "mg:rx", "RECEIVED:", rx, 66, 4.2, colI, zLabel)
	spawnLabelLeft(c, "mg:n1", "the receiver demods + decides each", rx, 74, 2.8, colMuted, zLabel)
	spawnLabelLeft(c, "mg:n2", "slot, rebuilding the text as it arrives.", rx, 78, 2.8, colMuted, zLabel)

	spawnLabel(c, "mg:hint", "Left / Right: change the message", worldW/2, 88, 2.8, colMuted, zLabel)

	bakeMessageLabels(c, ui)
}

// bakeMessageLabels re-renders the character and bit-pair labels for the current
// message (called on enter and whenever the message changes).
func bakeMessageLabels(c *app.Ctx, ui *uiState) {
	txt := curMessage(ui)
	loadLabel(c, "mg:c0", string(txt[0]))
	loadLabel(c, "mg:c1", string(txt[1]))
	for k, s := range msgSymbols(ui) {
		b0, b1 := bitsOf(s)
		loadLabel(c, "mg:p"+itoa(k), fmt.Sprintf("%d%d", b0, b1))
	}
}

func messageInput(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	for _, ev := range app.ReadEvents[webgpu.KeyEvent](c) {
		if ev.Action != glfw.Press && ev.Action != glfw.Repeat {
			continue
		}
		switch ev.Key {
		case glfw.KeyRight:
			ui.msgIdx = (ui.msgIdx + 1) % len(msgPresets)
			bakeMessageLabels(c, ui)
			msgPrevSym = -1
		case glfw.KeyLeft:
			ui.msgIdx = (ui.msgIdx + len(msgPresets) - 1) % len(msgPresets)
			bakeMessageLabels(c, ui)
			msgPrevSym = -1
		}
	}
}

func messageDraw(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	syms := msgSymbols(ui)
	now := elapsed(c)

	// Playhead progress 0..1 across the whole message, and the active slot.
	prog := math.Mod(now, msgLoopSecs) / msgLoopSecs
	cur := int(prog * msgNsym)
	if cur >= msgNsym {
		cur = msgNsym - 1
	}

	// The full transmitted waveform: each slot is one symbol's carrier.
	placeSeries(c, mgWave, func(i int) (float32, float32) {
		u := float64(i) / float64(msgWaveN-1)
		k := int(u * msgNsym)
		if k >= msgNsym {
			k = msgNsym - 1
		}
		local := u*msgNsym - float64(k) // 0..1 within the slot
		s := qpskMap(bitsOf(syms[k]))
		return mgWaveVP.mapPoint(mgWaveData, u, clampf(passband(s, msgCyclesPer, local), -1.19, 1.19))
	})

	// Playhead, slot highlight, bit-pair highlight, current constellation point.
	px := mgWaveVP.X + float32(prog)*mgWaveVP.W
	placeSeries(c, mgPlay, func(int) (float32, float32) { return px, mgWaveVP.Y + mgWaveVP.H/2 })
	placeSeries(c, mgSegHi, func(int) (float32, float32) { return segCenterX(cur), mgWaveVP.Y + mgWaveVP.H/2 })
	placeSeries(c, mgBitHi, func(int) (float32, float32) { return segCenterX(cur), 18 })
	sCur := qpskMap(bitsOf(syms[cur]))
	placeSeries(c, mgCur, func(int) (float32, float32) {
		return mgPlaneVP.mapPoint(mgPlaneData, sCur.I, sCur.Q)
	})

	// Readouts change only when the active slot changes.
	if cur != msgPrevSym {
		msgPrevSym = cur
		b0, b1 := bitsOf(syms[cur])
		deg := phaseDeg(sCur)
		loadLabel(c, "mg:now", fmt.Sprintf("sending slot %d/%d   bits %d%d   phase %03d", cur+1, msgNsym, b0, b1, deg))

		// Reveal characters as their 4 slots finish (clean channel here).
		done := (cur + 1) / 4
		txt := curMessage(ui)
		loadLabel(c, "mg:rx", "RECEIVED: "+padRight(txt[:done], len(txt)))
	}
}

// phaseDeg returns a symbol's phase in degrees, 0..359.
func phaseDeg(s Symbol) int {
	deg := int(math.Round(math.Atan2(s.Q, s.I) * 180 / math.Pi))
	if deg < 0 {
		deg += 360
	}
	return deg
}

// padRight pads s with spaces to width n (keeps label widths stable on re-bake).
func padRight(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}

// lowAlpha returns col with a reduced alpha (for translucent highlight fills).
func lowAlpha(col webgpu.Color, a float32) webgpu.Color { col.A = a; return col }
