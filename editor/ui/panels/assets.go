package panels

import (
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/project"
	"github.com/hvuhsg/spliti/editor/ui"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/tui"
)

// Assets is the lower-left panel listing project sprites and scenes.
// Click a sprite to open the sprite editor; click a scene to load it
// (Phase H wires scene loading).
type Assets struct {
	// SelectedIndex tracks the focused row across frames. Renders use it;
	// arrow keys move it; Enter sets AssetOpenAction.
	SelectedIndex int
	scroll        int
}

// AssetOpenAction is set by the Assets panel when the user activates a
// row (Enter key or double-click). The editor's apply system reads it
// and dispatches: open sprite → SpriteEdit panel, open scene → reload.
//
// Kind is one of "sprite", "scene". Path is the absolute file path.
// Empty Kind means "no pending action".
type AssetOpenAction struct {
	Kind string
	Path string
}

// AssetEntry is one row: kind+path pair.
type AssetEntry struct {
	Kind string // "sprite" | "scene"
	Path string
}

// Name implements ui.Panel.
func (a *Assets) Name() string { return ui.NameAssets }

// Title implements ui.Panel.
func (a *Assets) Title() string { return "Assets" }

// list collects displayed entries from the loaded project.
func (a *Assets) list(c *app.Ctx) []AssetEntry {
	p := app.GetResource[project.Project](c)
	if p == nil {
		return nil
	}
	out := make([]AssetEntry, 0, len(p.SpritePaths)+len(p.ScenePaths))
	for _, sp := range p.SpritePaths {
		out = append(out, AssetEntry{Kind: "sprite", Path: sp})
	}
	for _, sc := range p.ScenePaths {
		out = append(out, AssetEntry{Kind: "scene", Path: sc})
	}
	return out
}

// Render implements ui.Panel.
func (a *Assets) Render(c *app.Ctx, r ui.Rect, focused bool) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	ui.DrawFill(s, r, ' ', tcell.StyleDefault)
	ui.DrawBox(s, r, "Assets", focused)

	inner := r.Inset(1)
	if inner.W <= 0 || inner.H <= 0 {
		return
	}

	entries := a.list(c)
	if len(entries) == 0 {
		ui.DrawTextClipped(s, inner.X, inner.Y, inner.W, ui.StyleTextDim, "(no assets)")
		return
	}

	for i, e := range entries {
		row := i - a.scroll
		if row < 0 || row >= inner.H {
			continue
		}
		style := ui.StyleText
		if i == a.SelectedIndex && focused {
			style = ui.StyleSelected
			ui.DrawHLine(s, inner.X, inner.Y+row, inner.W, ' ', style)
		}
		stem := strings.TrimSuffix(filepath.Base(e.Path), filepath.Ext(e.Path))
		marker := "•"
		if e.Kind == "scene" {
			marker = "▢"
		}
		ui.DrawTextClipped(s, inner.X, inner.Y+row, inner.W, style, marker+" "+stem)
	}
}

// OnMouse implements ui.Panel. Click selects a row; double-click would
// open in Phase G — for v1, single click is enough.
func (a *Assets) OnMouse(c *app.Ctx, lx, ly int, ev input.MouseEvent) {
	if ev.Buttons&tcell.Button1 == 0 {
		return
	}
	row := ly - 1
	if row < 0 {
		return
	}
	row += a.scroll
	if row >= len(a.list(c)) {
		return
	}
	a.SelectedIndex = row
}

// OnKey implements ui.Panel.
func (a *Assets) OnKey(c *app.Ctx, ev input.KeyEvent) bool {
	entries := a.list(c)
	if len(entries) == 0 {
		return false
	}
	switch ev.Key {
	case tcell.KeyUp:
		if a.SelectedIndex > 0 {
			a.SelectedIndex--
		}
		return true
	case tcell.KeyDown:
		if a.SelectedIndex < len(entries)-1 {
			a.SelectedIndex++
		}
		return true
	case tcell.KeyEnter:
		if a.SelectedIndex < 0 || a.SelectedIndex >= len(entries) {
			return false
		}
		entry := entries[a.SelectedIndex]
		req := app.GetResource[AssetOpenAction](c)
		if req != nil {
			req.Kind = entry.Kind
			req.Path = entry.Path
		}
		return true
	}
	// Letter shortcuts.
	switch ev.Rune {
	case 'n':
		// New sprite via prompt.
		req := app.GetResource[NewSpriteRequest](c)
		if req != nil {
			req.Pending = true
		}
		return true
	case 'N':
		// New scene.
		req := app.GetResource[NewSceneRequest](c)
		if req != nil {
			req.Pending = true
		}
		return true
	}
	return false
}

// NewSpriteRequest / NewSceneRequest signal from Assets panel to the
// editor's creation handlers. Resource pattern keeps panel code
// independent of the top-level editor package.
type NewSpriteRequest struct {
	Pending bool
}

// NewSceneRequest is the assets-panel-side trigger for CreateNewScene.
type NewSceneRequest struct {
	Pending bool
}
