package editor

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/components"
	"github.com/hvuhsg/spliti/editor/project"
	"github.com/hvuhsg/spliti/editor/state"
	"github.com/hvuhsg/spliti/editor/ui"
	"github.com/hvuhsg/spliti/editor/ui/panels"
	"github.com/hvuhsg/spliti/plugin/sprite"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/viewport"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// CreateNewEntity spawns a new entity at the viewport's center with
// EditorMeta + Position + Glyph defaults and selects it. Auto-named
// "entity_N" where N is the count of existing EditorMeta entities + 1
// — collisions don't matter; SceneName is just a display label.
//
// Called from the hierarchy panel's 'n' shortcut and the global Ctrl-E.
func CreateNewEntity(c *app.Ctx) {
	if len(components.Registry()) == 0 {
		components.RegisterBuiltins()
	}
	w := c.World()

	// Pick a position at the centre of the active viewport.
	cx, cy := 5, 5
	if vp := app.GetResource[viewport.Viewport](c); vp != nil && vp.Active {
		cx, cy = vp.W/2, vp.H/2
	}

	// Count existing entities for a unique-ish default name.
	count := 0
	app.Query1[state.EditorMeta](c, func(_ ecs.Entity, _ *state.EditorMeta) {
		count++
	})

	e := w.NewEntity()
	mMeta := generic.NewMap1[state.EditorMeta](w)
	mMeta.Add(e)
	*mMeta.Get(e) = state.EditorMeta{SceneName: fmt.Sprintf("entity_%d", count+1)}
	mPos := generic.NewMap1[tui.Position](w)
	mPos.Add(e)
	*mPos.Get(e) = tui.Position{X: cx, Y: cy}
	mGlyph := generic.NewMap1[tui.Glyph](w)
	mGlyph.Add(e)
	*mGlyph.Get(e) = tui.Glyph{Char: '?', Style: tcell.StyleDefault.Foreground(tcell.ColorWhite)}

	if sel := app.GetResource[state.Selection](c); sel != nil {
		sel.Entity = e
		sel.Active = true
	}
	setStatus(c, fmt.Sprintf("created entity_%d", count+1), tcell.StyleDefault.Foreground(tcell.ColorGreen))
}

// CreateNewScene opens a prompt for a name; on submit, despawns every
// EditorMeta entity in the world, writes an empty scene file, and
// updates the project's DefaultScene to point at it.
//
// Cancelling the prompt is a no-op.
func CreateNewScene(c *app.Ctx) {
	pr := app.GetResource[panels.Prompt](c)
	if pr == nil {
		return
	}
	pr.Open(
		"New scene",
		"Enter a scene name (no spaces; '.scene' is appended)",
		func(c *app.Ctx, name string) {
			name = strings.TrimSpace(name)
			if name == "" {
				setStatus(c, "new scene: empty name", tcell.StyleDefault.Foreground(tcell.ColorRed))
				return
			}
			p := app.GetResource[project.Project](c)
			if p == nil {
				setStatus(c, "new scene: no project loaded", tcell.StyleDefault.Foreground(tcell.ColorRed))
				return
			}
			// Despawn current scene entities through the Commands queue
			// so it happens after this system finishes.
			cmds := c.Commands()
			app.Query1[state.EditorMeta](c, func(e ecs.Entity, _ *state.EditorMeta) {
				cmds.Despawn(e)
			})
			path := filepath.Join(p.Dir, "scenes", name+".scene")
			if err := SaveScene(c, path, name); err != nil {
				setStatus(c, "new scene: "+err.Error(), tcell.StyleDefault.Foreground(tcell.ColorRed))
				return
			}
			p.File.DefaultScene = name
			if err := project.SaveProject(p); err != nil {
				setStatus(c, "save project: "+err.Error(), tcell.StyleDefault.Foreground(tcell.ColorRed))
				return
			}
			// Refresh the project's scene-paths list.
			if p2, err := project.LoadProject(p.Dir); err == nil {
				*p = *p2
			}
			setStatus(c, fmt.Sprintf("created scene %q", name), tcell.StyleDefault.Foreground(tcell.ColorGreen))
		},
		nil,
	)
}

// CreateNewSprite opens a two-step prompt: name first, then "WxH".
// Creates an all-empty sprite of those dimensions, registers it, writes
// it to disk, and opens the sprite editor.
func CreateNewSprite(c *app.Ctx) {
	pr := app.GetResource[panels.Prompt](c)
	if pr == nil {
		return
	}
	pr.Open(
		"New sprite — step 1/2",
		"Enter sprite ref (e.g. \"paddle\", \"ball\")",
		func(c *app.Ctx, refIn string) {
			ref := strings.TrimSpace(refIn)
			if ref == "" {
				setStatus(c, "new sprite: empty name", tcell.StyleDefault.Foreground(tcell.ColorRed))
				return
			}
			pr2 := app.GetResource[panels.Prompt](c)
			if pr2 == nil {
				return
			}
			pr2.Open(
				fmt.Sprintf("New sprite %q — step 2/2", ref),
				"Dimensions as WxH (e.g. \"4x2\", \"1x4\")",
				func(c *app.Ctx, dimsIn string) {
					w, h, err := parseDims(strings.TrimSpace(dimsIn))
					if err != nil {
						setStatus(c, "new sprite: "+err.Error(), tcell.StyleDefault.Foreground(tcell.ColorRed))
						return
					}
					if err := finishCreateSprite(c, ref, w, h); err != nil {
						setStatus(c, "new sprite: "+err.Error(), tcell.StyleDefault.Foreground(tcell.ColorRed))
						return
					}
					setStatus(c, fmt.Sprintf("created sprite %q (%dx%d)", ref, w, h),
						tcell.StyleDefault.Foreground(tcell.ColorGreen))
				},
				nil,
			)
		},
		nil,
	)
}

func finishCreateSprite(c *app.Ctx, ref string, w, h int) error {
	p := app.GetResource[project.Project](c)
	if p == nil {
		return fmt.Errorf("no project loaded")
	}
	registry := app.GetResource[sprite.SpriteRegistry](c)
	if registry == nil {
		return fmt.Errorf("no sprite registry")
	}
	asset := &sprite.SpriteAsset{
		W: w, H: h,
		Cells: make([]sprite.Cell, w*h),
	}
	for i := range asset.Cells {
		asset.Cells[i] = sprite.Cell{Empty: true}
	}
	registry.Set(ref, asset)
	sf := project.FileFromAsset(ref, asset)
	path := filepath.Join(p.Dir, "sprites", ref+".sprite")
	if err := project.SaveSpriteFile(path, sf); err != nil {
		return err
	}
	// Refresh project so the asset browser sees the new file.
	if p2, err := project.LoadProject(p.Dir); err == nil {
		*p = *p2
	}
	// Open in sprite editor.
	layout := app.GetResource[ui.Layout](c)
	if layout != nil {
		layout.SpriteEditorOpen = true
		layout.Recompute(layout.W, layout.H)
		syncViewport(c)
		if se, ok := layout.PanelByName(ui.NameSpriteEdit).(*panels.SpriteEdit); ok {
			se.Ref = ref
			se.Cursor.X, se.Cursor.Y = 0, 0
			se.Dirty = false
		}
	}
	return nil
}

// CreateNewProject opens a two-step prompt (path, name) and creates a
// new project skeleton on disk. The new project is NOT loaded into the
// current editor session — switching projects in-place would require
// tearing down a lot of state. The user re-launches the editor with
// --project pointing at the new path.
func CreateNewProject(c *app.Ctx) {
	pr := app.GetResource[panels.Prompt](c)
	if pr == nil {
		return
	}
	pr.Open(
		"New project — step 1/2",
		"Filesystem path for the project directory (will be created)",
		func(c *app.Ctx, pathIn string) {
			path := strings.TrimSpace(pathIn)
			if path == "" {
				setStatus(c, "new project: empty path", tcell.StyleDefault.Foreground(tcell.ColorRed))
				return
			}
			pr2 := app.GetResource[panels.Prompt](c)
			if pr2 == nil {
				return
			}
			pr2.Open(
				"New project — step 2/2",
				fmt.Sprintf("Project name (will be saved at %s/project.toml)", path),
				func(c *app.Ctx, nameIn string) {
					name := strings.TrimSpace(nameIn)
					if name == "" {
						setStatus(c, "new project: empty name", tcell.StyleDefault.Foreground(tcell.ColorRed))
						return
					}
					if _, err := project.NewEmptyProject(path, name); err != nil {
						setStatus(c, "new project: "+err.Error(), tcell.StyleDefault.Foreground(tcell.ColorRed))
						return
					}
					setStatus(c, fmt.Sprintf("created project at %s — relaunch with --project %s", path, path),
						tcell.StyleDefault.Foreground(tcell.ColorGreen))
				},
				nil,
			)
		},
		nil,
	)
}

// applyCreateActions reads each per-frame request flag from the panels
// and dispatches the matching creation function. Centralised here so
// panel code stays free of dependencies on the top-level editor
// package.
func applyCreateActions(c *app.Ctx) {
	if req := app.GetResource[panels.NewEntityRequest](c); req != nil && req.Pending {
		req.Pending = false
		CreateNewEntity(c)
	}
	if req := app.GetResource[panels.NewSceneRequest](c); req != nil && req.Pending {
		req.Pending = false
		CreateNewScene(c)
	}
	if req := app.GetResource[panels.NewSpriteRequest](c); req != nil && req.Pending {
		req.Pending = false
		CreateNewSprite(c)
	}
	if req := app.GetResource[panels.DeleteEntityRequest](c); req != nil && req.Entity != (ecs.Entity{}) {
		e := req.Entity
		req.Entity = ecs.Entity{}
		c.Commands().Despawn(e)
		// Clear selection if it was the deleted entity.
		if sel := app.GetResource[state.Selection](c); sel != nil && sel.Entity == e {
			sel.Active = false
		}
	}
}

// parseDims parses a "WxH" string like "4x2" into integer dimensions.
// Lowercase 'x' only; we don't normalise the input — sprite dims are a
// terse spec the user types so a wider parser would invite typos.
func parseDims(s string) (int, int, error) {
	parts := strings.SplitN(s, "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected WxH, got %q", s)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil || w <= 0 {
		return 0, 0, fmt.Errorf("bad width %q", parts[0])
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil || h <= 0 {
		return 0, 0, fmt.Errorf("bad height %q", parts[1])
	}
	return w, h, nil
}
