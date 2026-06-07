package main

import (
	"fmt"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
	"github.com/hvuhsg/spliti/plugin/render3d"
	splitiui "github.com/hvuhsg/spliti/plugin/ui"
)

// Node-editor layout in logical units (DPI-independent). Node positions
// (Node.X/Y) are canvas-local logical coords; everything is multiplied by the UI
// scale (edLayout.s) into framebuffer pixels so the editor grows with the rest of
// the UI on HiDPI displays. Defaults are sized for a ~1280-logical-wide canvas.
const (
	edNodeW   = 240
	edNodeH   = 150
	edTitleH  = 28
	edPortR   = 8
	edOriginX = 40
	edOriginY = 56
)

// edLayout converts logical editor coordinates to framebuffer pixels at the
// current UI scale.
type edLayout struct {
	s                           float32
	origin                      imgui.Vec2
	nodeW, nodeH, titleH, portR float32
}

func newEdLayout(s float32) edLayout {
	return edLayout{
		s:      s,
		origin: imgui.Vec2{X: edOriginX * s, Y: edOriginY * s},
		nodeW:  edNodeW * s, nodeH: edNodeH * s, titleH: edTitleH * s, portR: edPortR * s,
	}
}

func (l edLayout) nodePos(n *Node) imgui.Vec2 {
	return imgui.Vec2{X: l.origin.X + n.X*l.s, Y: l.origin.Y + n.Y*l.s}
}
func (l edLayout) outPort(n *Node) imgui.Vec2 {
	p := l.nodePos(n)
	return imgui.Vec2{X: p.X + l.nodeW, Y: p.Y + l.nodeH/2}
}
func (l edLayout) inPort(n *Node) imgui.Vec2 {
	p := l.nodePos(n)
	return imgui.Vec2{X: p.X, Y: p.Y + l.nodeH/2}
}

// editorUI renders the signal-chain node editor as a Dear ImGui surface while the
// app is in graph mode. It replaces the old CPU-rasterized editor (and its manual
// hit-testing): nodes drag via ImGui's active-item state, ports wire with a live
// bezier, the text node edits through InputText, and the toolbar/close are real
// buttons — all crisp, GPU-rendered, and DPI-scaled.
func editorUI(c *app.Ctx) {
	ui := app.GetResource[UI](c)
	ed := app.GetResource[Editor](c)
	if ui == nil || ed == nil || ui.Mode != ModeGraph {
		return
	}
	g, play := boundGraph(c, ed)
	if g == nil {
		ui.Mode = ModeExplore
		return
	}
	fbW, fbH := render3d.Size(c)
	lay := newEdLayout(splitiui.Scale(c))

	// Full-screen surface that dims the 3D scene behind it.
	imgui.SetNextWindowPosV(imgui.Vec2{}, imgui.CondAlways, imgui.Vec2{})
	imgui.SetNextWindowSizeV(imgui.Vec2{X: float32(fbW), Y: float32(fbH)}, imgui.CondAlways)
	imgui.PushStyleColorVec4(imgui.ColWindowBg, imgui.Vec4{X: 0.02, Y: 0.03, Z: 0.05, W: 0.96})
	flags := imgui.WindowFlagsNoTitleBar | imgui.WindowFlagsNoResize | imgui.WindowFlagsNoMove |
		imgui.WindowFlagsNoCollapse | imgui.WindowFlagsNoSavedSettings | imgui.WindowFlagsNoScrollbar |
		imgui.WindowFlagsNoBringToFrontOnFocus
	open := imgui.BeginV("##graph", nil, flags)
	imgui.PopStyleColor()
	if !open {
		imgui.End()
		return
	}

	dl := imgui.WindowDrawList()
	s := lay.s

	header := "TRANSMIT CHAIN  -  drag a port to wire, then Play"
	bar := col(120, 200, 255, 255)
	if g.Kind == GraphRx {
		header = "RECEIVE CHAIN  -  decodes live while the transmitter sends"
		bar = col(150, 255, 190, 255)
	}
	dl.AddRectFilled(imgui.Vec2{}, imgui.Vec2{X: float32(fbW), Y: 4 * s}, bar)
	dl.AddTextVec2(imgui.Vec2{X: 16 * s, Y: 12 * s}, bar, header)

	// Wires under the nodes.
	for _, e := range g.Edges {
		a, b := g.node(e.From), g.node(e.To)
		if a == nil || b == nil {
			continue
		}
		drawWire(dl, lay.outPort(a), lay.inPort(b), col(120, 200, 255, 255), s)
	}
	if ed.wireFrom != 0 {
		if a := g.node(ed.wireFrom); a != nil {
			drawWire(dl, lay.outPort(a), imgui.MousePos(), col(255, 220, 120, 255), s)
		}
	}

	// Nodes. Removal is deferred so we don't mutate g.Nodes mid-iteration.
	remove := 0
	for _, n := range g.Nodes {
		if nodeWidget(dl, lay, ed, g, play, n) {
			remove = n.ID
		}
	}
	if remove != 0 {
		if ed.wireFrom == remove {
			ed.wireFrom = 0
		}
		g.remove(remove)
		markDirty(g, play)
	}

	// Resolve a wire drop onto an input port.
	if ed.wireFrom != 0 && imgui.IsMouseReleased(0) {
		if to := inputPortUnder(lay, g, imgui.MousePos()); to != 0 && to != ed.wireFrom {
			g.connect(ed.wireFrom, to)
			markDirty(g, play)
		}
		ed.wireFrom = 0
	}

	editorToolbar(ui, ed, g, play, fbH, s)

	imgui.End()
}

// nodeWidget submits one node's interaction items and draws it. Returns true if
// the node's close button was pressed (the caller removes it after the loop).
func nodeWidget(dl *imgui.DrawList, lay edLayout, ed *Editor, g *Graph, play *Play, n *Node) bool {
	imgui.PushIDStr(fmt.Sprintf("node%d", n.ID))
	defer imgui.PopID()

	s := lay.s
	pos := lay.nodePos(n)

	// Body drag handle. AllowOverlap lets the widgets drawn on top of it (text
	// field, buttons, port) receive input instead of the drag handle swallowing it.
	imgui.SetNextItemAllowOverlap()
	imgui.SetCursorScreenPos(pos)
	imgui.InvisibleButton("body", imgui.Vec2{X: lay.nodeW, Y: lay.nodeH})
	if imgui.IsItemActive() {
		d := imgui.CurrentIO().MouseDelta()
		n.X += d.X / s
		n.Y += d.Y / s
	}

	title, barCol := nodeTitle(n)
	dl.AddRectFilled(pos, addv(pos, lay.nodeW, lay.nodeH), col(26, 28, 38, 245))
	dl.AddRectFilled(pos, addv(pos, lay.nodeW, lay.titleH), barCol)
	dl.AddTextVec2(addv(pos, 12*s, 7*s), col(15, 18, 24, 255), title)

	nodeBody(dl, lay, g, play, n, pos)
	nodePorts(dl, lay, ed, g, n)

	// Close button (top-right of the title bar).
	imgui.SetCursorScreenPos(addv(pos, lay.nodeW-30*s, 4*s))
	return imgui.Button("x")
}

// nodeBody draws the kind-specific contents and submits any inner widgets.
func nodeBody(dl *imgui.DrawList, lay edLayout, g *Graph, play *Play, n *Node, pos imgui.Vec2) {
	s := lay.s
	white := col(235, 240, 245, 255)
	switch n.Kind {
	case KindText:
		if g.Kind == GraphTx {
			imgui.SetCursorScreenPos(addv(pos, 12*s, lay.titleH+16*s))
			imgui.SetNextItemWidth(lay.nodeW - 24*s)
			if imgui.InputTextWithHint("##txt", "message", &n.Text, 0, nil) {
				markDirty(g, play)
			}
		} else {
			dl.AddTextVec2(addv(pos, 12*s, lay.titleH+12*s), col(150, 200, 170, 255), "decoded:")
			dl.AddTextVec2(addv(pos, 12*s, lay.titleH+36*s), white, clip(n.Text, 22))
		}
	case KindConstellation:
		drawConstellationPreview(dl, n.Mod, pos, lay)
		imgui.SetCursorScreenPos(addv(pos, 12*s, lay.nodeH-32*s))
		// Stable ### id so cycling the label doesn't change the button's identity.
		if imgui.Button(modName(n.Mod) + "   >###cyc") {
			n.Mod = nextMod(n.Mod)
			markDirty(g, play)
		}
	case KindTransmitter:
		dl.AddTextVec2(addv(pos, 14*s, lay.titleH+22*s), col(150, 200, 170, 255), "carrier out")
	case KindReceiver:
		dl.AddTextVec2(addv(pos, 14*s, lay.titleH+22*s), col(150, 200, 170, 255), "antenna in")
	}
}

// nodePorts draws the input/output ports and, for outputs, the InvisibleButton
// that starts a wire drag.
func nodePorts(dl *imgui.DrawList, lay edLayout, ed *Editor, g *Graph, n *Node) {
	portCol := col(210, 220, 230, 255)
	if hasInput(n, g.Kind) {
		dl.AddCircleFilled(lay.inPort(n), lay.portR, portCol)
	}
	if hasOutput(n, g.Kind) {
		ctr := lay.outPort(n)
		imgui.SetCursorScreenPos(imgui.Vec2{X: ctr.X - lay.portR, Y: ctr.Y - lay.portR})
		imgui.InvisibleButton("out", imgui.Vec2{X: 2 * lay.portR, Y: 2 * lay.portR})
		if imgui.IsItemActivated() {
			ed.wireFrom = n.ID
		}
		c := portCol
		if imgui.IsItemHovered() || ed.wireFrom == n.ID {
			c = col(255, 220, 120, 255)
		}
		dl.AddCircleFilled(ctr, lay.portR, c)
	}
}

// editorToolbar draws the bottom-left action bar.
func editorToolbar(ui *UI, ed *Editor, g *Graph, play *Play, fbH int, s float32) {
	imgui.SetCursorScreenPos(imgui.Vec2{X: 16 * s, Y: float32(fbH) - 44*s})
	if imgui.Button("+ Text") {
		n := g.add(KindText, 40, 40)
		if g.Kind == GraphTx {
			n.Text = "MSG"
		} else {
			n.Text = ""
		}
		markDirty(g, play)
	}
	imgui.SameLine()
	if imgui.Button("+ Const") {
		g.add(KindConstellation, 40, 240)
		markDirty(g, play)
	}
	imgui.SameLine()
	if g.Kind == GraphTx && play != nil {
		label := "Play"
		if play.Playing {
			label = "Stop"
		}
		if imgui.Button(label) {
			play.Playing = !play.Playing
			if play.Playing {
				play.recompile(g)
			}
		}
	} else {
		imgui.Button("Live")
	}
	imgui.SameLine()
	if imgui.Button("Close") {
		ui.Mode = ModeExplore
		ed.wireFrom = 0
	}
}

// --- geometry + drawing helpers ---

// inputPortUnder returns the id of the node whose input port is under p (within a
// grab radius), or 0.
func inputPortUnder(lay edLayout, g *Graph, p imgui.Vec2) int {
	grab := lay.portR + 6*lay.s
	for _, n := range g.Nodes {
		if !hasInput(n, g.Kind) {
			continue
		}
		ctr := lay.inPort(n)
		dx, dy := p.X-ctr.X, p.Y-ctr.Y
		if dx*dx+dy*dy <= grab*grab {
			return n.ID
		}
	}
	return 0
}

// drawWire draws a horizontal-tangent cubic bezier between two ports.
func drawWire(dl *imgui.DrawList, a, b imgui.Vec2, c uint32, s float32) {
	dx := (b.X - a.X) * 0.5
	dl.AddBezierCubic(a, imgui.Vec2{X: a.X + dx, Y: a.Y}, imgui.Vec2{X: b.X - dx, Y: b.Y}, b, c, 2.5*s)
}

// drawConstellationPreview plots the ideal symbol points for mod inside a node.
func drawConstellationPreview(dl *imgui.DrawList, mod sim.Modulation, pos imgui.Vec2, lay edLayout) {
	cx := pos.X + lay.nodeW/2
	cy := pos.Y + lay.titleH + (lay.nodeH-lay.titleH)/2 - 8*lay.s
	half := (lay.nodeH - lay.titleH) / 2 * 0.62
	axis := col(70, 84, 96, 255)
	dl.AddLine(imgui.Vec2{X: cx - half, Y: cy}, imgui.Vec2{X: cx + half, Y: cy}, axis)
	dl.AddLine(imgui.Vec2{X: cx, Y: cy - half}, imgui.Vec2{X: cx, Y: cy + half}, axis)
	dot := col(150, 200, 230, 255)
	for _, p := range constellation(mod) {
		px := cx + float32(real(p))*half*0.82
		py := cy - float32(imag(p))*half*0.82
		dl.AddCircleFilled(imgui.Vec2{X: px, Y: py}, 2.5*lay.s, dot)
	}
}

// nodeTitle returns a node's title-bar label and color.
func nodeTitle(n *Node) (string, uint32) {
	switch n.Kind {
	case KindText:
		return "TEXT", col(60, 130, 220, 255)
	case KindConstellation:
		return "CONSTELLATION", col(220, 160, 60, 255)
	case KindTransmitter:
		return "TRANSMITTER", col(70, 190, 120, 255)
	case KindReceiver:
		return "RECEIVER", col(70, 190, 120, 255)
	}
	return "NODE", col(120, 120, 130, 255)
}

// col packs an 8-bit RGBA color into ImGui's IM_COL32 (R | G<<8 | B<<16 | A<<24).
func col(r, g, b, a uint8) uint32 {
	return uint32(r) | uint32(g)<<8 | uint32(b)<<16 | uint32(a)<<24
}

func addv(p imgui.Vec2, dx, dy float32) imgui.Vec2 {
	return imgui.Vec2{X: p.X + dx, Y: p.Y + dy}
}
