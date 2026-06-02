// Package ui owns the editor's panel layout, focus, and routing.
//
// A Layout is a resource on the editor's spliti App. It stores a list of
// Panels and a name→Rect map recomputed on every ResizeEvent. One overlay
// system walks the layout and calls Render on each panel; one routing
// system dispatches MouseEvents and KeyEvents to the correct panel.
//
// The viewport panel is special: it does NOT render its own content, only
// chrome. The world is drawn inside its rect by the tui plugin via the
// Viewport resource (set by editor wiring code at the start of every frame).
package ui

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/input"
)

// Rect is a screen-coordinate rectangle. (X,Y) is the top-left cell;
// (X+W-1, Y+H-1) is the bottom-right inclusive.
type Rect struct{ X, Y, W, H int }

// Contains reports whether (x,y) lies inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Inset returns r reduced by margin on every side. Useful for "draw inside
// the border, not on it". Result is clamped to non-negative size.
func (r Rect) Inset(margin int) Rect {
	w, h := r.W-margin*2, r.H-margin*2
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return Rect{X: r.X + margin, Y: r.Y + margin, W: w, H: h}
}

// Panel is a discrete UI region. Panels render themselves, accept routed
// mouse/key input, and own their internal state. The editor's layout holds
// a flat list of panels and assigns each one a Rect every frame.
type Panel interface {
	Name() string
	Title() string
	Render(c *app.Ctx, r Rect, focused bool)
	OnMouse(c *app.Ctx, localX, localY int, ev input.MouseEvent)
	OnKey(c *app.Ctx, ev input.KeyEvent) (handled bool)
}

// Layout owns panels, their assigned rects, and which one is focused. It
// is registered as a resource so any system can read it (panels often need
// to look up other panels' rects, e.g. an asset browser opening a sprite
// editor needs the sprite-editor panel's rect to size the canvas).
type Layout struct {
	panels   []Panel
	rects    map[string]Rect
	Focused  string
	W, H     int
	// SpriteEditorOpen toggles whether the sprite editor takes a strip
	// at the bottom. When closed, the viewport gets the freed rows.
	SpriteEditorOpen bool
}

// NewLayout constructs a Layout owning the given panels. Panels are
// rendered in registration order; later panels draw on top of earlier
// ones (which only matters when rects overlap, which they don't in our
// rectilinear layout).
func NewLayout(panels ...Panel) *Layout {
	l := &Layout{rects: make(map[string]Rect)}
	for _, p := range panels {
		l.panels = append(l.panels, p)
	}
	if len(panels) > 0 {
		l.Focused = panels[0].Name()
	}
	return l
}

// Panels returns the registered panels in order.
func (l *Layout) Panels() []Panel { return l.panels }

// Rect returns the assigned rect for panelName. Zero-value Rect if unknown.
func (l *Layout) Rect(panelName string) Rect { return l.rects[panelName] }

// PanelByName looks up a panel by its Name().
func (l *Layout) PanelByName(name string) Panel {
	for _, p := range l.panels {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// PanelAt returns the panel whose rect contains (x,y), or nil. Used by
// mouse routing. Linear scan is fine for our small panel count.
func (l *Layout) PanelAt(x, y int) Panel {
	for _, p := range l.panels {
		if r := l.rects[p.Name()]; r.Contains(x, y) {
			return p
		}
	}
	return nil
}

// Layout slot constants. Panel implementations must use these names so
// Recompute knows where to place them.
const (
	NameMenuBar    = "menubar"
	NameStatus     = "status"
	NameHierarchy  = "hierarchy"
	NameAssets     = "assets"
	NameInspector  = "inspector"
	NameToolbar    = "toolbar"
	NameViewport   = "viewport"
	NameSpriteEdit = "spriteedit"
)

// Recompute assigns rects to every panel based on (w,h). Layout is
// rigid for v1 — left column 24 wide, right column 30 wide, top/bottom
// rows 1 each, optional bottom strip 12 high when sprite editor is open.
//
// Panels whose names aren't in the layout-slot table are skipped; they
// can use Layout.SetRect to size themselves manually.
func (l *Layout) Recompute(w, h int) {
	l.W, l.H = w, h
	const (
		menuH         = 1
		statusH       = 1
		toolbarH      = 1
		leftW         = 24
		rightW        = 30
		spriteEditH   = 12
	)
	// Top: menubar
	l.rects[NameMenuBar] = Rect{X: 0, Y: 0, W: w, H: menuH}
	// Bottom: status
	l.rects[NameStatus] = Rect{X: 0, Y: h - statusH, W: w, H: statusH}

	// Body: rows between menubar and status.
	bodyY := menuH
	bodyH := h - menuH - statusH
	if bodyH < 0 {
		bodyH = 0
	}

	// Optional sprite editor strip at the bottom of the body.
	if l.SpriteEditorOpen {
		stripH := spriteEditH
		if stripH > bodyH-3 { // keep at least a few rows for the rest
			stripH = bodyH - 3
			if stripH < 0 {
				stripH = 0
			}
		}
		l.rects[NameSpriteEdit] = Rect{X: 0, Y: bodyY + bodyH - stripH, W: w, H: stripH}
		bodyH -= stripH
	} else {
		l.rects[NameSpriteEdit] = Rect{}
	}

	// Left column: hierarchy (top half), assets (bottom half).
	hierH := bodyH / 2
	if hierH < 0 {
		hierH = 0
	}
	assetsH := bodyH - hierH
	l.rects[NameHierarchy] = Rect{X: 0, Y: bodyY, W: leftW, H: hierH}
	l.rects[NameAssets] = Rect{X: 0, Y: bodyY + hierH, W: leftW, H: assetsH}

	// Right column: inspector.
	l.rects[NameInspector] = Rect{X: w - rightW, Y: bodyY, W: rightW, H: bodyH}

	// Center: toolbar (1 row), then viewport.
	centerX := leftW
	centerW := w - leftW - rightW
	if centerW < 0 {
		centerW = 0
	}
	l.rects[NameToolbar] = Rect{X: centerX, Y: bodyY, W: centerW, H: toolbarH}
	l.rects[NameViewport] = Rect{X: centerX, Y: bodyY + toolbarH, W: centerW, H: bodyH - toolbarH}
}

// SetRect overrides a panel's assigned rect. Useful for panels that aren't
// in the named-slot layout (e.g. transient popups).
func (l *Layout) SetRect(name string, r Rect) { l.rects[name] = r }

// FocusPanelAt sets focus to the panel under (x,y), if any. Returns true
// when focus changed. Used by the mouse-click handler.
func (l *Layout) FocusPanelAt(x, y int) bool {
	p := l.PanelAt(x, y)
	if p == nil {
		return false
	}
	if l.Focused == p.Name() {
		return false
	}
	l.Focused = p.Name()
	return true
}
