package main

import (
	"image"
	"image/color"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
)

// Mode is the global interaction mode. Systems consult it instead of fighting
// over raw input: in ModeGraph the 3D controls simply do nothing.
type Mode int

const (
	ModeExplore Mode = iota // fly camera, select/drag/configure markers
	ModeGraph               // node-graph editor is up; 3D controls are inert
)

// UI holds the current interaction mode (shared across systems).
type UI struct{ Mode Mode }

func newUI() *UI { return &UI{Mode: ModeExplore} }

// Editor holds the node-graph editor's interaction state. Which graph it edits is
// the selected device's chain (Editor.target); whether it's shown is UI.Mode.
type Editor struct {
	target Selection // which device's graph is open (SelTx / SelRx)

	dragID             int // node being dragged (0 = none)
	dragOffX, dragOffY float32
	wireFrom           int // node id we're dragging a wire from (output), 0 = none
	editID             int // text node being edited, 0 = none

	mx, my  float32 // cursor in framebuffer px
	redraw  bool    // editor image needs rebaking
	builtFB int     // framebuffer height the editor image was last built for
}

func newEditor() *Editor { return &Editor{redraw: true} }

// editor layout constants, in framebuffer pixels.
const (
	edPad = 60 // canvas inset from the screen edge
	nodeW = 280
	nodeH = 120
	portR = 13 // port half-size
)

// rect is an axis-aligned pixel rectangle with a hit test.
type rect struct{ x, y, w, h float32 }

func (r rect) hit(px, py float32) bool {
	return px >= r.x && px < r.x+r.w && py >= r.y && py < r.y+r.h
}

func canvasRect(fbW, fbH int) rect {
	return rect{edPad, edPad, float32(fbW - 2*edPad), float32(fbH - 2*edPad)}
}

// openGraphButtonRect is the "OPEN GRAPH" button inside the device config drawer.
func openGraphButtonRect(fbW, fbH int) rect {
	return rect{float32(fbW-configW) + 24, 300, configW - 48, 56}
}

// node geometry, canvas-local --------------------------------------------------

func nodeRect(n *Node) rect   { return rect{n.X, n.Y, nodeW, nodeH} }
func inPortRect(n *Node) rect { return rect{n.X - portR, n.Y + nodeH/2 - portR, 2 * portR, 2 * portR} }
func outPortRect(n *Node) rect {
	return rect{n.X + nodeW - portR, n.Y + nodeH/2 - portR, 2 * portR, 2 * portR}
}
func closeRect(n *Node) rect { return rect{n.X + nodeW - 32, n.Y + 8, 24, 24} }
func cycleRect(n *Node) rect { return rect{n.X + 16, n.Y + nodeH - 42, nodeW - 32, 30} }

// hasInput/hasOutput give a node's port directions, which depend on the chain
// kind (a Text node is a source in TX, a sink in RX).
func hasInput(n *Node, gk GraphKind) bool {
	switch n.Kind {
	case KindReceiver:
		return false
	case KindText:
		return gk == GraphRx
	default:
		return true
	}
}

func hasOutput(n *Node, gk GraphKind) bool {
	switch n.Kind {
	case KindTransmitter:
		return false
	case KindText:
		return gk == GraphTx
	default:
		return true
	}
}

// toolbar buttons, canvas-local ------------------------------------------------

type toolbar struct{ addText, addConst, play, close rect }

func toolbarOf(cw, ch float32) toolbar {
	y := ch - 62
	bw, bh, gap := float32(160), float32(42), float32(14)
	x := float32(20)
	mk := func() rect { r := rect{x, y, bw, bh}; x += bw + gap; return r }
	return toolbar{addText: mk(), addConst: mk(), play: mk(), close: mk()}
}

// boundGraph resolves the graph (and TX playback, if any) of the open device.
func boundGraph(c *app.Ctx, ed *Editor) (*Graph, *Play) {
	switch ed.target {
	case SelTx:
		if d := txDevice(c); d != nil {
			return d.Graph, d.Play
		}
	case SelRx:
		if d := rxDevice(c); d != nil {
			return d.Graph, nil
		}
	}
	return nil, nil
}

// editorSystem handles the panel "OPEN GRAPH" button and (in graph mode) all
// editor input. It never touches the camera; controlsSystem stays out via UI.Mode.
func editorSystem(c *app.Ctx) {
	ui := app.GetResource[UI](c)
	ed := app.GetResource[Editor](c)
	lab := app.GetResource[Lab](c)
	if ui == nil || ed == nil || lab == nil {
		return
	}
	fbW, fbH := windowSize(c)
	scale := uiScale(c)

	var g *Graph
	var play *Play
	if ui.Mode == ModeGraph {
		if g, play = boundGraph(c, ed); g == nil {
			ui.Mode = ModeExplore
		}
	}

	for _, ev := range app.ReadEvents[inputs.MouseMoveEvent](c) {
		ed.mx, ed.my = float32(ev.X*scale), float32(ev.Y*scale)
	}

	for _, ev := range app.ReadEvents[inputs.MouseButtonEvent](c) {
		if ev.Button != inputs.MouseButtonLeft {
			continue
		}
		px, py := float32(ev.X*scale), float32(ev.Y*scale)
		ed.mx, ed.my = px, py
		switch {
		case ui.Mode == ModeExplore:
			if ev.Action == inputs.Press && lab.Sel != SelNone && openGraphButtonRect(fbW, fbH).hit(px, py) {
				ui.Mode, ed.target, ed.editID, ed.redraw = ModeGraph, lab.Sel, 0, true
			}
		case g == nil:
			// nothing to edit
		case ev.Action == inputs.Press:
			editorPress(ui, ed, g, play, fbW, fbH, px, py)
		case ev.Action == inputs.Release:
			editorRelease(ed, g, play, fbW, fbH, px, py)
		}
	}

	if ui.Mode != ModeGraph || g == nil {
		return
	}
	editorKeys(c, ui, ed, g, play)

	cv := canvasRect(fbW, fbH)
	if ed.dragID != 0 {
		if n := g.node(ed.dragID); n != nil {
			n.X, n.Y = ed.mx-cv.x-ed.dragOffX, ed.my-cv.y-ed.dragOffY
			ed.redraw = true
		}
	}
	if ed.wireFrom != 0 {
		ed.redraw = true
	}
}

func editorPress(ui *UI, ed *Editor, g *Graph, play *Play, fbW, fbH int, px, py float32) {
	cv := canvasRect(fbW, fbH)
	lx, ly := px-cv.x, py-cv.y

	tb := toolbarOf(cv.w, cv.h)
	switch {
	case tb.addText.hit(lx, ly):
		n := g.add(KindText, 60, 60)
		if g.Kind == GraphTx {
			n.Text = "MSG"
		} else {
			n.Text = ""
		}
		ed.redraw = true
		markDirty(g, play)
		return
	case tb.addConst.hit(lx, ly):
		g.add(KindConstellation, 60, 260)
		ed.redraw = true
		markDirty(g, play)
		return
	case tb.play.hit(lx, ly) && g.Kind == GraphTx && play != nil:
		play.Playing = !play.Playing
		if play.Playing {
			play.recompile(g)
		}
		ed.redraw = true
		return
	case tb.close.hit(lx, ly):
		ui.Mode, ed.editID, ed.redraw = ModeExplore, 0, true
		return
	}

	for i := len(g.Nodes) - 1; i >= 0; i-- {
		n := g.Nodes[i]
		switch {
		case closeRect(n).hit(lx, ly):
			g.remove(n.ID)
			if ed.editID == n.ID {
				ed.editID = 0
			}
			ed.redraw = true
			markDirty(g, play)
			return
		case hasOutput(n, g.Kind) && outPortRect(n).hit(lx, ly):
			ed.wireFrom, ed.redraw = n.ID, true
			return
		case n.Kind == KindConstellation && cycleRect(n).hit(lx, ly):
			n.Mod = nextMod(n.Mod)
			ed.redraw = true
			markDirty(g, play)
			return
		case nodeRect(n).hit(lx, ly):
			if n.Kind == KindText && g.Kind == GraphTx {
				ed.editID = n.ID
			}
			ed.dragID = n.ID
			ed.dragOffX, ed.dragOffY = lx-n.X, ly-n.Y
			ed.redraw = true
			return
		}
	}
	ed.editID, ed.redraw = 0, true
}

func editorRelease(ed *Editor, g *Graph, play *Play, fbW, fbH int, px, py float32) {
	cv := canvasRect(fbW, fbH)
	lx, ly := px-cv.x, py-cv.y
	if ed.wireFrom != 0 {
		for _, n := range g.Nodes {
			if n.ID != ed.wireFrom && hasInput(n, g.Kind) && inPortRect(n).hit(lx, ly) {
				g.connect(ed.wireFrom, n.ID)
				markDirty(g, play)
				break
			}
		}
		ed.wireFrom, ed.redraw = 0, true
	}
	ed.dragID = 0
}

// editorKeys handles text entry (TX message) and Escape.
func editorKeys(c *app.Ctx, ui *UI, ed *Editor, g *Graph, play *Play) {
	for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
		if ev.Key == inputs.KeyEscape && ev.Action == inputs.Press {
			if ed.editID != 0 {
				ed.editID = 0
			} else {
				ui.Mode = ModeExplore
			}
			ed.redraw = true
			continue
		}
		if ed.editID == 0 {
			continue
		}
		n := g.node(ed.editID)
		if n == nil || n.Kind != KindText {
			ed.editID = 0
			continue
		}
		switch {
		case ev.Rune >= 32 && ev.Action == inputs.Press:
			n.Text += string(ev.Rune)
			ed.redraw = true
			markDirty(g, play)
		case ev.Key == inputs.KeyBackspace && (ev.Action == inputs.Press || ev.Action == inputs.Repeat):
			if r := []rune(n.Text); len(r) > 0 {
				n.Text = string(r[:len(r)-1])
				ed.redraw = true
				markDirty(g, play)
			}
		case (ev.Key == inputs.KeyEnter || ev.Key == inputs.KeyKPEnter) && ev.Action == inputs.Press:
			ed.editID, ed.redraw = 0, true
		}
	}
}

// markDirty flags the TX playback for recompile (no-op for an RX chain).
func markDirty(g *Graph, play *Play) {
	if play != nil && g.Kind == GraphTx {
		play.dirty = true
	}
}

// uiScale returns framebuffer-px / window-point, so mouse coords (window points)
// hit-test correctly against the framebuffer-px overlay layout (Retina differs).
func uiScale(c *app.Ctx) float64 {
	win := render3d.Window(c)
	if win == nil {
		return 1
	}
	fw, _ := win.GetFramebufferSize()
	lw, _ := win.GetSize()
	if lw == 0 {
		return 1
	}
	return float64(fw) / float64(lw)
}

// --- drawing ------------------------------------------------------------------

func drawEditor(c *app.Ctx) {
	ed := app.GetResource[Editor](c)
	if ed == nil {
		return
	}
	g, play := boundGraph(c, ed)
	if g == nil {
		return
	}
	fbW, fbH := windowSize(c)
	cv := canvasRect(fbW, fbH)

	ensureDim(c)
	render3d.DrawPanel(c, "ed_dim", 0, 0, fbW, fbH)

	// The decoded text in an RX chain changes constantly, so rebake every frame
	// for RX; for TX rebake only on change.
	if ed.redraw || ed.builtFB != fbH || g.Kind == GraphRx {
		render3d.LoadPanel(c, "editor", buildEditorImage(ed, g, play, int(cv.w), int(cv.h)))
		ed.redraw, ed.builtFB = false, fbH
	}
	render3d.DrawPanel(c, "editor", int(cv.x), int(cv.y), int(cv.w), int(cv.h))
}

func buildEditorImage(ed *Editor, g *Graph, play *Play, cw, ch int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, cw, ch))
	fillRect(img, 0, 0, cw, ch, color.RGBA{14, 16, 22, 238})
	header := "TRANSMIT CHAIN  -  drag a port to wire, then PLAY"
	bar := color.RGBA{120, 200, 255, 255}
	if g.Kind == GraphRx {
		header = "RECEIVE CHAIN  -  decodes live while the transmitter sends"
		bar = color.RGBA{150, 255, 190, 255}
	}
	fillRect(img, 0, 0, cw, 4, bar)
	drawText(img, 20, 16, header, bar, 2)

	for _, e := range g.Edges {
		a, b := g.node(e.From), g.node(e.To)
		if a == nil || b == nil {
			continue
		}
		op, ip := outPortRect(a), inPortRect(b)
		drawWire(img, op.x+portR, op.y+portR, ip.x+portR, ip.y+portR, color.RGBA{120, 200, 255, 255})
	}
	if ed.wireFrom != 0 {
		if a := g.node(ed.wireFrom); a != nil {
			op := outPortRect(a)
			drawWire(img, op.x+portR, op.y+portR, ed.mx-edPad, ed.my-edPad, color.RGBA{255, 220, 120, 255})
		}
	}

	for _, n := range g.Nodes {
		drawNode(img, n, g.Kind, ed.editID == n.ID, ed.wireFrom == n.ID)
	}

	tb := toolbarOf(float32(cw), float32(ch))
	drawButton(img, tb.addText, "+ TEXT", color.RGBA{40, 70, 110, 255})
	drawButton(img, tb.addConst, "+ CONST", color.RGBA{110, 80, 30, 255})
	if g.Kind == GraphTx {
		if play != nil && play.Playing {
			drawButton(img, tb.play, "STOP", color.RGBA{150, 50, 50, 255})
		} else {
			drawButton(img, tb.play, "PLAY", color.RGBA{40, 120, 70, 255})
		}
	} else {
		drawButton(img, tb.play, "LIVE", color.RGBA{40, 90, 70, 255})
	}
	drawButton(img, tb.close, "CLOSE", color.RGBA{60, 60, 70, 255})
	return img
}

func drawNode(img *image.RGBA, n *Node, gk GraphKind, editing, wiring bool) {
	r := nodeRect(n)
	x, y := int(r.x), int(r.y)
	fillRect(img, x, y, nodeW, nodeH, color.RGBA{26, 28, 38, 255})

	var title string
	var bar color.RGBA
	switch n.Kind {
	case KindText:
		title, bar = "TEXT", color.RGBA{60, 130, 220, 255}
	case KindConstellation:
		title, bar = "CONSTELLATION", color.RGBA{220, 160, 60, 255}
	case KindTransmitter:
		title, bar = "TRANSMITTER", color.RGBA{70, 190, 120, 255}
	case KindReceiver:
		title, bar = "RECEIVER", color.RGBA{70, 190, 120, 255}
	}
	fillRect(img, x, y, nodeW, 30, bar)
	drawText(img, x+12, y+8, title, color.RGBA{15, 18, 24, 255}, 2)

	white := color.RGBA{235, 240, 245, 255}
	switch n.Kind {
	case KindText:
		if gk == GraphTx {
			s := n.Text
			if editing {
				s += "_"
			}
			drawText(img, x+16, y+52, clip(s, 20), white, 2)
		} else {
			drawText(img, x+16, y+46, "decoded:", color.RGBA{150, 200, 170, 255}, 1)
			drawText(img, x+16, y+62, clip(n.Text, 20), white, 2)
		}
	case KindConstellation:
		drawText(img, x+16, y+50, modName(n.Mod), white, 3)
		drawButton(img, rect{r.x + 16, r.y + nodeH - 42, nodeW - 32, 30}, "MOD >", color.RGBA{90, 70, 30, 255})
	case KindTransmitter:
		drawText(img, x+16, y+56, "carrier out", color.RGBA{150, 200, 170, 255}, 2)
	case KindReceiver:
		drawText(img, x+16, y+56, "antenna in", color.RGBA{150, 200, 170, 255}, 2)
	}

	cb := closeRect(n)
	fillRect(img, int(cb.x), int(cb.y), int(cb.w), int(cb.h), color.RGBA{120, 40, 40, 255})
	drawText(img, int(cb.x)+6, int(cb.y)+4, "x", white, 2)

	port := color.RGBA{210, 220, 230, 255}
	if hasInput(n, gk) {
		p := inPortRect(n)
		fillRect(img, int(p.x), int(p.y), int(p.w), int(p.h), port)
	}
	if hasOutput(n, gk) {
		p := outPortRect(n)
		oc := port
		if wiring {
			oc = color.RGBA{255, 220, 120, 255}
		}
		fillRect(img, int(p.x), int(p.y), int(p.w), int(p.h), oc)
	}
}

func drawButton(img *image.RGBA, r rect, label string, col color.RGBA) {
	fillRect(img, int(r.x), int(r.y), int(r.w), int(r.h), col)
	fillRect(img, int(r.x), int(r.y), int(r.w), 3, color.RGBA{255, 255, 255, 70})
	drawText(img, int(r.x)+12, int(r.y)+int(r.h)/2-7, label, color.RGBA{240, 244, 250, 255}, 2)
}

// drawWire draws a horizontal-tangent cubic bezier between two ports.
func drawWire(img *image.RGBA, x0, y0, x1, y1 float32, col color.RGBA) {
	dx := (x1 - x0) * 0.5
	c0x, c1x := x0+dx, x1-dx
	const seg = 24
	px, py := int(x0), int(y0)
	for i := 1; i <= seg; i++ {
		t := float32(i) / seg
		u := 1 - t
		bx := u*u*u*x0 + 3*u*u*t*c0x + 3*u*t*t*c1x + t*t*t*x1
		by := u*u*u*y0 + 3*u*u*t*y0 + 3*u*t*t*y1 + t*t*t*y1
		plotLine(img, px, py, int(bx), int(by), col)
		px, py = int(bx), int(by)
	}
}

// clip truncates s to at most n runes (for node display).
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ensureDim registers the stretched translucent backdrop once.
func ensureDim(c *app.Ctx) {
	if render3d.HasPanel(c, "ed_dim") {
		return
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+0], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 3, 5, 9, 205
	}
	render3d.LoadPanel(c, "ed_dim", img)
}
