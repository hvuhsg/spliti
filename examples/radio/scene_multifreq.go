package main

import (
	"fmt"
	"math"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
)

// Multi-frequency (OFDM) overview: instead of sending one symbol at a time on a
// single carrier, split the data across many subcarriers — each a different
// frequency carrying its own QPSK symbol — and add them into one wave. Because
// the frequencies are orthogonal, the receiver can pull them apart again (an
// FFT). The payoff is bit rate: N carriers send 2N bits every symbol period
// instead of 2, so the throughput is N times higher — shown at the bottom.

const (
	mfMaxCarriers = 6
	mfSubM        = 200 // dots per subcarrier curve
	mfCompN       = 700 // dots for the summed wave

	mfSubX0  = 6.0
	mfSubW   = 78.0
	mfSubTop = 24.0
	mfSubBot = 60.0

	// throughput bit-cells
	mfCellW   = 7.0
	mfCellH   = 4.0
	mfCellGap = 1.5
	mfCellsX0 = 56.0
	mfRowOne  = 73.0 // single-carrier row Y
	mfRowMany = 79.0 // N-carrier row Y

	// series ids
	mfAxis = 50 // lane zero-lines
	mfComp = 60 // the summed (transmitted) wave
	mfBit1 = 70 // single-carrier bit cells (always 2)
	mfBitN = 71 // N-carrier bit cells (2N lit)
)

var (
	mfCompVP   = viewport{X: 92, Y: 24, W: 62, H: 30}
	mfCompData = dataRect{X0: 0, X1: 1, Y0: -2.6, Y1: 2.6}
	mfCellDim  = webgpu.Color{R: 0.16, G: 0.19, B: 0.27, A: 1}
	mfPalette  = []webgpu.Color{
		colI, colQ, colAccent, colSum,
		{R: 0.62, G: 0.42, B: 0.92, A: 1},
		{R: 0.20, G: 0.80, B: 0.82, A: 1},
	}
)

func mfCellX(i int) float32 { return mfCellsX0 + float32(i)*(mfCellW+mfCellGap) }

// mfSymbol is the (deterministic) QPSK symbol carried by subcarrier k for a seed.
// Go's &3 masks the low two bits even for a negative seed, so idx is always 0..3.
func mfSymbol(seed, k int) Symbol {
	return qpskMap(bitsOf((seed*5 + k*3 + 1) & 3))
}

func setupMultiFreq(c *app.Ctx) {
	cmds := c.Commands()
	ui := app.GetResource[uiState](c)
	if ui.mfN < 2 || ui.mfN > mfMaxCarriers {
		ui.mfN = 4
	}

	spawnBackButton(c)
	spawnLabel(c, "mf2:title", "MULTI-FREQUENCY  (OFDM)", worldW/2, 7, 5, colText, zLabel)

	// Flow strip across the top.
	flowStrip(c, []string{"BITS IN", "N FREQS", "SUM WAVE", "AIR", "BY FREQ", "BITS OUT"})

	// Subcarrier lanes (left) — colours and curves; zero-lines via markers.
	spawnMarkers(cmds, mfAxis, mfMaxCarriers, "sq", mfSubW, 0.16, colGrid, zAxis)
	for k := 0; k < mfMaxCarriers; k++ {
		spawnSeries(cmds, k, mfSubM, 0.9, mfPalette[k])
	}
	spawnLabelLeft(c, "mf2:laneL", "subcarriers: each its own frequency + symbol", mfSubX0, 20, 2.6, colMuted, zLabel)
	spawnLabelLeft(c, "mf2:laneL2", "(top = low freq, bottom = high freq)", mfSubX0, mfSubBot+3, 2.6, colMuted, zLabel)

	// The summed transmitted wave (right).
	spawnSprite(cmds, "sq", mfCompVP.X-3, mfCompVP.Y-3, mfCompVP.W+6, mfCompVP.H+6, colPanel, zPanel)
	_, zy := mfCompVP.mapPoint(mfCompData, 0, 0)
	spawnSprite(cmds, "sq", mfCompVP.X, zy-0.1, mfCompVP.W, 0.2, colAxis, zAxis)
	spawnSeries(cmds, mfComp, mfCompN, 1.2, colHi)
	spawnLabel(c, "mf2:sum", "=  one transmitted wave (their sum)", mfCompVP.X+mfCompVP.W/2, mfCompVP.Y-5, 2.7, colSum, zLabel)
	spawnLabel(c, "mf2:fft", "receiver separates them by frequency (FFT)", mfCompVP.X+mfCompVP.W/2, mfCompVP.Y+mfCompVP.H+5, 2.6, colMuted, zLabel)
	spawnLabel(c, "mf2:plus", "+", (mfSubX0+mfSubW+mfCompVP.X)/2, 40, 6, colMuted, zLabel)

	// Throughput comparison: lit bit-cells, one row per scheme.
	spawnLabel(c, "mf2:thr", "BIT RATE  -  bits delivered per symbol period", worldW/2, 67, 3, colText, zLabel)
	spawnLabelLeft(c, "mf2:r1", "1 carrier:   2 bits", mfSubX0, mfRowOne, 2.6, colSum, zLabel)
	spawnLabelLeft(c, "mf2:rN", "4 carriers:  8 bits  (x4)", mfSubX0, mfRowMany, 2.6, colQ, zLabel)
	spawnMarkers(cmds, mfBit1, 2, "sq", mfCellW, mfCellH, colSum, zLabel)
	spawnMarkers(cmds, mfBitN, 2*mfMaxCarriers, "sq", mfCellW, mfCellH, colQ, zLabel)

	spawnLabel(c, "mf2:hint", "Up/Down: number of carriers     Left/Right: change data     Esc: back", worldW/2, 87, 2.6, colMuted, zLabel)

	bakeMFInfo(c, ui)
}

// flowStrip draws a row of small non-clickable boxes joined by arrows.
func flowStrip(c *app.Ctx, labels []string) {
	cmds := c.Commands()
	n := len(labels)
	const bw, bh, gap, y = 22.0, 6.0, 5.0, 10.0
	total := float32(n)*bw + float32(n-1)*gap
	start := (worldW - total) / 2
	for i, lab := range labels {
		x := start + float32(i)*(bw+gap)
		spawnSprite(cmds, "sq", x, y, bw, bh, colPanel2, zPanel)
		spawnLabelFit(c, "mf2:fb"+itoa(i), lab, x+bw/2, y+bh/2, bw*0.84, bh*0.5, colText, zLabel)
		if i < n-1 {
			spawnSprite(cmds, "arrow", x+bw+gap/2-1.5, y+bh/2-1.5, 3.5, 3.5, colMuted, zAxis)
		}
	}
}

// bakeMFInfo re-renders the N-carrier throughput label (fixed widths so geometry
// stays constant).
func bakeMFInfo(c *app.Ctx, ui *uiState) {
	loadLabel(c, "mf2:rN", fmt.Sprintf("%d carriers:  %2d bits  (x%d)", ui.mfN, 2*ui.mfN, ui.mfN))
}

func multiFreqInput(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	changed := false
	for _, ev := range app.ReadEvents[webgpu.KeyEvent](c) {
		if ev.Action != glfw.Press && ev.Action != glfw.Repeat {
			continue
		}
		switch ev.Key {
		case glfw.KeyUp:
			if ui.mfN < mfMaxCarriers {
				ui.mfN++
				changed = true
			}
		case glfw.KeyDown:
			if ui.mfN > 2 {
				ui.mfN--
				changed = true
			}
		case glfw.KeyRight:
			ui.mfSeed++
			changed = true
		case glfw.KeyLeft:
			ui.mfSeed--
			changed = true
		}
	}
	if changed {
		bakeMFInfo(c, ui)
	}
}

func multiFreqDraw(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	n := ui.mfN
	t0 := elapsed(c) * 0.25
	laneH := float32(mfSubBot-mfSubTop) / float32(n)

	offscreen := func(int) (float32, float32) { return -50, -50 }

	for k := 0; k < mfMaxCarriers; k++ {
		k := k
		if k >= n {
			placeSeries(c, k, offscreen)
			continue
		}
		yc := mfSubTop + (float32(k)+0.5)*laneH
		amp := laneH * 0.34
		sym := mfSymbol(ui.mfSeed, k)
		freq := float64(k + 1)
		placeSeries(c, k, func(i int) (float32, float32) {
			u := float64(i) / float64(mfSubM-1)
			v := passband(sym, freq, u+t0)
			return mfSubX0 + float32(u)*mfSubW, yc - float32(v)*amp
		})
	}

	// Lane zero-lines.
	placeSeries(c, mfAxis, func(idx int) (float32, float32) {
		if idx >= n {
			return -50, -50
		}
		yc := mfSubTop + (float32(idx)+0.5)*laneH
		return mfSubX0 + mfSubW/2, yc
	})

	// Summed wave, normalized so its size stays comparable as carriers change.
	norm := math.Sqrt(float64(n))
	placeSeries(c, mfComp, func(i int) (float32, float32) {
		u := float64(i) / float64(mfCompN-1)
		sum := 0.0
		for k := 0; k < n; k++ {
			sum += passband(mfSymbol(ui.mfSeed, k), float64(k+1), u+t0)
		}
		return mfCompVP.mapPoint(mfCompData, u, clampf(sum/norm, -2.59, 2.59))
	})

	// Throughput bit-cells: a single carrier always lights 2; OFDM lights 2N.
	placeSeries(c, mfBit1, func(i int) (float32, float32) { return mfCellX(i), mfRowOne })
	placeSeries(c, mfBitN, func(i int) (float32, float32) { return mfCellX(i), mfRowMany })
	recolorSeries(c, mfBitN, func(i int) webgpu.Color {
		if i < 2*n {
			return colQ
		}
		return mfCellDim
	})
}
