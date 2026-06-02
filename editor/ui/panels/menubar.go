// Package panels contains one Panel implementation per editor screen
// region. Panels are registered with editor/ui.NewLayout and have rects
// assigned by Layout.Recompute every frame.
//
// Each panel is intentionally small: it owns no mutable global state, and
// reads everything it needs from the spliti App's resources (Project,
// Selection, Layout, etc.) Per-panel state lives on the Panel struct
// itself when needed (sprite editor brush, asset browser scroll).
package panels

import (
	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/ui"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/tui"
)

// MenuBar is the top row showing project name and global actions. v1
// renders a static row of "File  Edit  View  Help" labels — actual menu
// dropdowns are deferred. Mouse clicks on labels are recognised but
// don't open menus yet; we capture the rects so future code can plug in.
type MenuBar struct{}

// Name implements ui.Panel.
func (MenuBar) Name() string { return ui.NameMenuBar }

// Title implements ui.Panel.
func (MenuBar) Title() string { return "" }

// Render implements ui.Panel.
func (MenuBar) Render(c *app.Ctx, r ui.Rect, _ bool) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	bg := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorWhite)
	ui.DrawFill(s, r, ' ', bg)
	ui.DrawText(s, r.X+1, r.Y, bg.Bold(true), " spliti-editor ")
	// Menu items as plain labels for v1.
	items := []string{"File", "Edit", "View", "Help"}
	x := r.X + 18
	for _, it := range items {
		ui.DrawText(s, x, r.Y, bg, " "+it+" ")
		x += len(it) + 2
	}
}

// OnMouse implements ui.Panel.
func (MenuBar) OnMouse(_ *app.Ctx, _, _ int, _ input.MouseEvent) {}

// OnKey implements ui.Panel.
func (MenuBar) OnKey(_ *app.Ctx, _ input.KeyEvent) bool { return false }
