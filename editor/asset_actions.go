package editor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/project"
	"github.com/hvuhsg/spliti/editor/state"
	"github.com/hvuhsg/spliti/editor/ui"
	"github.com/hvuhsg/spliti/editor/ui/panels"
	"github.com/hvuhsg/spliti/plugin/sprite"
	"github.com/mlange-42/arche/ecs"
)

// applyAssetOpen reads the panels.AssetOpenAction resource and dispatches
// it: opening a sprite swaps the SpriteEdit panel's Ref and toggles the
// Layout's SpriteEditorOpen flag. Opening a scene reloads the world from
// that scene file (Phase H).
//
// We keep this in a separate file from editor.go so the cross-panel
// orchestration is easy to find and extend.
func applyAssetOpen(c *app.Ctx) {
	req := app.GetResource[panels.AssetOpenAction](c)
	if req == nil || req.Kind == "" {
		return
	}
	kind, path := req.Kind, req.Path
	req.Kind, req.Path = "", "" // consume immediately

	switch kind {
	case "sprite":
		openSprite(c, path)
	case "scene":
		openScene(c, path)
	}
}

func openSprite(c *app.Ctx, path string) {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	layout := app.GetResource[ui.Layout](c)
	if layout != nil {
		layout.SpriteEditorOpen = true
		layout.Recompute(layout.W, layout.H)
		// Sync viewport rect because the sprite editor steals rows from
		// the body, shrinking the viewport.
		syncViewport(c)
	}
	// Find the panel and set its Ref. Panels are passed by interface, so
	// we type-assert to the concrete *SpriteEdit pointer.
	if layout != nil {
		if p, ok := layout.PanelByName(ui.NameSpriteEdit).(*panels.SpriteEdit); ok {
			p.Ref = stem
			p.Cursor.X, p.Cursor.Y = 0, 0
			p.Dirty = false
		}
	}
	// Make sure the registry has the asset loaded; if not, load it now.
	registry := app.GetResource[sprite.SpriteRegistry](c)
	if registry != nil && registry.Get(stem) == nil {
		sf, err := project.LoadSpriteFile(path)
		if err != nil {
			setStatus(c, fmt.Sprintf("open sprite: %v", err), tcell.StyleDefault.Foreground(tcell.ColorRed))
			return
		}
		asset, err := project.AssetFromFile(sf)
		if err != nil {
			setStatus(c, fmt.Sprintf("decode sprite: %v", err), tcell.StyleDefault.Foreground(tcell.ColorRed))
			return
		}
		registry.Set(sf.Ref, asset)
	}
	setStatus(c, fmt.Sprintf("opened sprite %q", stem), tcell.StyleDefault)
}

func openScene(c *app.Ctx, path string) {
	// Despawn current EditorMeta entities before loading the new scene
	// so we don't pile two scenes worth of entities onto the world.
	cmds := c.Commands()
	app.Query1[state.EditorMeta](c, func(e ecs.Entity, _ *state.EditorMeta) {
		cmds.Despawn(e)
	})
	if err := LoadScene(c, path); err != nil {
		setStatus(c, fmt.Sprintf("load scene: %v", err), tcell.StyleDefault.Foreground(tcell.ColorRed))
		return
	}
	setStatus(c, fmt.Sprintf("loaded scene %q", filepath.Base(path)), tcell.StyleDefault)
}

// applySpriteSave writes the in-memory sprite asset back to disk
// through editor/project. Looks up the project's sprite directory via
// the *project.Project resource; without one, surfaces an error to the
// status bar.
func applySpriteSave(c *app.Ctx) {
	req := app.GetResource[panels.SpriteSaveAction](c)
	if req == nil || req.Ref == "" {
		return
	}
	ref := req.Ref
	req.Ref = ""

	registry := app.GetResource[sprite.SpriteRegistry](c)
	if registry == nil {
		setStatus(c, "save: no sprite registry", tcell.StyleDefault.Foreground(tcell.ColorRed))
		return
	}
	asset := registry.Get(ref)
	if asset == nil {
		setStatus(c, fmt.Sprintf("save: ref %q not in registry", ref), tcell.StyleDefault.Foreground(tcell.ColorRed))
		return
	}
	p := app.GetResource[project.Project](c)
	if p == nil {
		setStatus(c, "save: no project loaded", tcell.StyleDefault.Foreground(tcell.ColorRed))
		return
	}
	sf := project.FileFromAsset(ref, asset)
	path := filepath.Join(p.Dir, "sprites", ref+".sprite")
	if err := project.SaveSpriteFile(path, sf); err != nil {
		setStatus(c, fmt.Sprintf("save: %v", err), tcell.StyleDefault.Foreground(tcell.ColorRed))
		return
	}
	setStatus(c, fmt.Sprintf("saved %s", ref), tcell.StyleDefault.Foreground(tcell.ColorGreen))
}
