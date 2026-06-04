package main

import (
	"math"

	"github.com/hvuhsg/spliti/app"
)

// Main flow scene: the whole transmit→receive pipeline as a row of clickable
// process stages with arrows between them and a packet of data travelling along.

const seriesPacket = 0

// stage describes one clickable box in the pipeline.
type stage struct {
	label   string
	caption string // the data form entering this stage
	target  Scene
	built   bool // true = a fully interactive detail scene exists
}

var pipeline = []stage{
	{"BITS>SYM", "1011 0010", Constellation, true},
	{"SYM>WAVE", "I/Q point", IQWave, true},
	{"AIR", "radio wave", Channel, true},
	{"WAVE>SYM", "noisy wave", Demod, true},
	{"SYM>BITS", "I/Q point", Decode, true},
}

const (
	boxW    = 22.0
	boxH    = 22.0
	boxGap  = 8.0
	rowY    = 38.0 // box top
	rowMidY = rowY + boxH/2
)

func boxX(i int) float32 {
	total := float32(len(pipeline))*boxW + float32(len(pipeline)-1)*boxGap
	start := (worldW - total) / 2
	return start + float32(i)*(boxW+boxGap)
}

func setupMainFlow(c *app.Ctx) {
	cmds := c.Commands()

	// Title + instructions.
	spawnLabel(c, "mf:title", "RADIO TRANSMISSION", worldW/2, 9, 6, colText, zLabel)
	spawnLabel(c, "mf:sub", "click a stage to explore   -   Esc to quit", worldW/2, 17, 3, colMuted, zLabel)

	// Stage boxes + captions + Clickable hit areas.
	for i, st := range pipeline {
		x := boxX(i)
		fill := colPanel2
		if st.built {
			fill = colAccent
		}
		// Box background (also the click target).
		spawnButton(c, "mf:box"+itoa(i), x, rowY, boxW, boxH, st.label, st.target, fill)
		// Caption under the box describing the data entering it.
		spawnLabel(c, "mf:cap"+itoa(i), st.caption, x+boxW/2, rowY+boxH+5, 3, colMuted, zLabel)
		// "soon" badge on stub stages.
		if !st.built {
			spawnLabel(c, "mf:soon"+itoa(i), "(soon)", x+boxW/2, rowY-4, 2.6, colMuted, zLabel)
		}
	}

	// Arrows in the gaps between boxes.
	for i := 0; i < len(pipeline)-1; i++ {
		gapL := boxX(i) + boxW
		gapR := boxX(i + 1)
		cx := (gapL + gapR) / 2
		// shaft
		spawnSprite(cmds, "sq", gapL+1, rowMidY-0.4, gapR-gapL-2, 0.8, colMuted, zAxis)
		// arrowhead
		spawnSprite(cmds, "arrow", cx, rowMidY-2.5, 4, 5, colMuted, zAxis)
	}

	// Travelling data packet (placed each frame by mainFlowUpdate).
	spawnSeries(cmds, seriesPacket, 1, 3.2, colSum)

	// Legend.
	spawnLabel(c, "mf:leg", "follow a bit's round trip: transmit on the left, receive on the right", worldW/2, 72, 3, colMuted, zLabel)

	// Capstones: canned message, write your own, denser modulation, many carriers.
	spawnButton(c, "mf:msg", 6, 80, 35, 7, "FULL MSG  >", Message, colSum)
	spawnButton(c, "mf:cmp", 44, 80, 35, 7, "WRITE MSG  >", Compose, colAccent)
	spawnButton(c, "mf:qam", 82, 80, 35, 7, "16-QAM  >", QAM16, colI)
	spawnButton(c, "mf:mfq", 120, 80, 35, 7, "MULTI-FREQ  >", MultiFreq, colQ)
}

// mainFlowUpdate sweeps the packet left to right across the pipeline, looping.
func mainFlowUpdate(c *app.Ctx) {
	t := elapsed(c)
	x0, x1 := boxX(0)+boxW/2, boxX(len(pipeline)-1)+boxW/2
	const period = 5.0
	frac := math.Mod(t, period) / period
	px := x0 + (x1-x0)*float32(frac)
	placeSeries(c, seriesPacket, func(int) (float32, float32) { return px, rowMidY })
}

// itoa is a tiny no-import integer-to-string for stable texture refs (0..9 only,
// which is all the indices this example needs).
func itoa(i int) string {
	if i < 0 || i > 9 {
		return "x"
	}
	return string(rune('0' + i))
}
