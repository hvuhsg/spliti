//go:build !js

package main

import (
	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splitiui "github.com/hvuhsg/spliti/plugin/ui"
)

// DragDrop is the shared state for the bottom toolbar's drag-and-drop: dragging
// a palette button out onto the ground spawns that object, and dragging an
// existing marker back onto the toolbar removes it (the bar doubles as a trash).
//
// toolbarUI (the ImGui system) sets placing/kind when a palette button is
// pressed and records the bar's screen rectangle; controlsSystem watches for the
// mouse release and does the spawn or delete. Splitting it this way keeps all the
// world mutation in the controls path, next to the existing marker drag.
type DragDrop struct {
	placing bool      // a new-object drag (from the palette) is in progress
	kind    Selection // which kind to spawn on drop (SelTx / SelRx / SelBlock)

	// Toolbar rectangle in framebuffer pixels (ImGui's coordinate space, which is
	// what imgui.MousePos reports), refreshed each frame by toolbarUI.
	bx, by, bw, bh float32
}

func newDragDrop() *DragDrop { return &DragDrop{} }

// overBar reports whether the framebuffer-pixel point (mx,my) lies over the
// toolbar — the trash zone for marker removal and the cancel zone for placement.
func (d *DragDrop) overBar(mx, my float32) bool {
	return mx >= d.bx && mx <= d.bx+d.bw && my >= d.by && my <= d.by+d.bh
}

// palette is the ordered set of objects the toolbar offers.
var palette = []struct {
	label string
	kind  Selection
}{
	{"Transmitter", SelTx},
	{"Receiver", SelRx},
	{"Block", SelBlock},
}

// toolbarUI draws the bottom object palette and arms a placement drag when a
// button is pressed. While a drag (palette placement or marker removal) is in
// flight it shows a cursor hint. Skipped in graph mode, where the editor owns the
// screen.
func toolbarUI(c *app.Ctx) {
	if ui := app.GetResource[UI](c); ui != nil && ui.Mode != ModeExplore {
		return
	}
	dd := app.GetResource[DragDrop](c)
	if dd == nil {
		return
	}
	ctl := app.GetResource[CamCtl](c)

	fbW, fbH := render3d.Size(c)
	s := splitiui.Scale(c)
	btnW, btnH := 116*s, 40*s
	pad := 8 * s
	barW := btnW*float32(len(palette)) + pad*float32(len(palette)+1)
	barH := btnH + pad*2
	x := (float32(fbW) - barW) / 2
	y := float32(fbH) - barH - 12*s

	imgui.SetNextWindowPosV(imgui.Vec2{X: x, Y: y}, imgui.CondAlways, imgui.Vec2{})
	imgui.SetNextWindowSizeV(imgui.Vec2{X: barW, Y: barH}, imgui.CondAlways)
	flags := hudWindowFlags | imgui.WindowFlagsNoTitleBar | imgui.WindowFlagsNoScrollbar

	if imgui.BeginV("Palette", nil, flags) {
		pos, sz := imgui.WindowPos(), imgui.WindowSize()
		dd.bx, dd.by, dd.bw, dd.bh = pos.X, pos.Y, sz.X, sz.Y

		for i, item := range palette {
			if i > 0 {
				imgui.SameLine()
			}
			imgui.ButtonV(item.label, imgui.Vec2{X: btnW, Y: btnH})
			// Arm placement the moment a button is pressed; the actual spawn happens
			// in controlsSystem when the button is released over the ground.
			if imgui.IsItemActivated() {
				dd.placing, dd.kind = true, item.kind
			}
		}
	}
	imgui.End()

	// Cursor hints so the in-flight drag reads clearly.
	if dd.placing {
		imgui.SetTooltip("drop on the ground to place " + kindLabel(dd.kind))
	} else if ctl != nil && ctl.dragging {
		mp := imgui.MousePos()
		if dd.overBar(mp.X, mp.Y) {
			imgui.SetTooltip("release to remove")
		}
	}
}

// kindLabel is the human name of a placeable selection kind.
func kindLabel(k Selection) string {
	switch k {
	case SelTx:
		return "a transmitter"
	case SelRx:
		return "a receiver"
	case SelBlock:
		return "a block"
	}
	return "an object"
}

// placeNew spawns a freshly-dragged palette object on the ground under the cursor
// (window-pixel px,py), clamped to the plane. The new object becomes selected.
func placeNew(c *app.Ctx, lab *Lab, kind Selection, px, py float64) {
	origin, dir := render3d.ScreenToRay(c, px, py)
	hit, ok := rayPlaneY(origin, dir, 0)
	if !ok {
		return
	}
	x := clamp(hit.X, -planeHalf, planeHalf)
	z := clamp(hit.Z, -planeHalf, planeHalf)
	cmd := c.Commands()
	switch kind {
	case SelTx:
		spawnTx(cmd, m.Vec3{X: x, Y: markerHeight, Z: z}, lab, true)
	case SelRx:
		spawnRx(cmd, m.Vec3{X: x, Y: markerHeight, Z: z}, lab, true)
	case SelBlock:
		spawnBlock(cmd, m.Vec3{X: x, Y: blockH / 2, Z: z}, lab, true)
	}
}
