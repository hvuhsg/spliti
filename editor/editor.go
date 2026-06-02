package editor

import (
	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/components"
	"github.com/hvuhsg/spliti/editor/project"
	"github.com/hvuhsg/spliti/editor/state"
	"github.com/hvuhsg/spliti/editor/ui"
	"github.com/hvuhsg/spliti/editor/ui/panels"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/viewport"
	"github.com/hvuhsg/spliti/schedule"
)

// Aliases for callers that import only "editor" — preserves the old
// surface API while the canonical types live in editor/state.
type (
	// Mode mirrors state.Mode for callers that only depend on the
	// top-level editor package.
	Mode = state.Mode
	// EditorMeta mirrors state.EditorMeta similarly.
	EditorMeta = state.EditorMeta
	// Selection mirrors state.Selection similarly.
	Selection = state.Selection
)

const (
	Editing = state.Editing
	Playing = state.Playing
	Paused  = state.Paused
)

// Plugin installs the editor on top of an existing spliti App that
// already has terminal+input+tui+sprite+viewport plugins. The editor
// adds:
//
//   - Editing/Playing/Paused state machine (the runtime plugin should
//     be configured with RunIf returning true only for Playing).
//   - Layout resource owning all panels.
//   - Selection resource.
//   - Mouse routing system in First (after input drain).
//   - Key routing + global shortcuts in First.
//   - Panel render overlay before tui's present.
//   - Resize handler that recomputes the layout.
//   - Viewport sync system that keeps the Viewport resource matched to
//     the current viewport-panel rect every frame.
//
// Callers wire the plugin with App.AddPlugins(editor.Plugin{}). The
// runtime plugin must be added BEFORE the editor plugin so the editor's
// state-driven RunIf is honoured (we install our state machine first
// and the runtime reads it via the function we pass into runtime.Plugin).
type Plugin struct{}

// Build implements app.Plugin.
func (Plugin) Build(a *app.App) {
	// Install editor state.
	app.InitState[state.Mode](a, state.Editing)
	// PlaySnapshot resource holds the pre-play world state so OnExit
	// (Stop) can rewind reliably.
	app.InsertResource(a, &PlaySnapshot{})
	app.InsertResource(a, &state.Selection{})
	app.InsertResource(a, &panels.StatusMessage{})
	app.InsertResource(a, &panels.ToolbarAction{})
	app.InsertResource(a, &panels.AssetOpenAction{})
	app.InsertResource(a, &panels.SpriteSaveAction{})

	// Modal prompt: registered as a resource so any system can open it
	// (new entity, new scene, new sprite, new project all funnel through
	// here). Rendered manually in the overlay system.
	prompt := &panels.Prompt{}
	app.InsertResource(a, prompt)

	// Picker: a generic list-modal used for "Add component" and other
	// pick-from-list flows. Same modal-routing rules as Prompt.
	picker := &panels.Picker{}
	app.InsertResource(a, picker)

	// Per-frame request flags from panels — consumed and reset by
	// applyCreateActions each frame.
	app.InsertResource(a, &panels.NewEntityRequest{})
	app.InsertResource(a, &panels.NewSpriteRequest{})
	app.InsertResource(a, &panels.NewSceneRequest{})
	app.InsertResource(a, &panels.DeleteEntityRequest{})

	// Register the built-in component descriptors. Idempotent across
	// editor.Plugin instantiations because Register panics on
	// duplicates — but Plugin is meant to be applied once per App, so
	// hitting that panic indicates a misconfiguration we want loud.
	if len(components.Registry()) == 0 {
		components.RegisterBuiltins()
	}

	// Install Viewport resource — initially inactive; the layout-sync
	// system below activates and sizes it based on the current viewport
	// panel rect.
	app.InsertResource(a, &viewport.Viewport{})

	// Construct the layout with all panels.
	layout := ui.NewLayout(
		panels.MenuBar{},
		panels.StatusBar{},
		&panels.Hierarchy{},
		&panels.Assets{},
		&panels.Inspector{},
		panels.Toolbar{},
		&panels.Viewport{},
		&panels.SpriteEdit{},
	)
	// Default focus on the viewport so arrow keys interact with the world
	// out of the box. Users can click any panel to take focus.
	layout.Focused = ui.NameViewport
	app.InsertResource(a, layout)

	// Once at startup: query screen size and lay out panels. We can't do
	// this in PreStartup because the terminal plugin sets up its Screen
	// during Startup; running at PostStartup lets us see the real size.
	a.AddSystems(schedule.PostStartup, app.System(func(c *app.Ctx) {
		s := tui.Screen(c)
		if s == nil {
			return
		}
		w, h := s.Size()
		l := app.GetResource[ui.Layout](c)
		l.Recompute(w, h)
		syncViewport(c)
	}))

	// Resize handler: recompute layout when the terminal changes size.
	a.AddSystems(schedule.First, app.System(func(c *app.Ctx) {
		evs := app.ReadEvents[input.ResizeEvent](c)
		if len(evs) == 0 {
			return
		}
		last := evs[len(evs)-1]
		l := app.GetResource[ui.Layout](c)
		l.Recompute(last.Width, last.Height)
		syncViewport(c)
	}).Label("__editor_resize"))

	// Layout-sync: every frame, mirror the viewport-panel rect into the
	// Viewport resource so the tui renderer clips the world to the panel.
	// Cheap; lets the rest of the editor adjust panel sizes without an
	// explicit "tell the renderer" call.
	a.AddSystems(schedule.PreUpdate, app.System(syncViewport).Label("__editor_sync_viewport"))

	// Mouse routing: read MouseEvents, dispatch to panel under cursor,
	// update focus on click. Runs in First after input plugin drains.
	a.AddSystems(schedule.First, app.System(routeMouse).Label("__editor_mouse").After("__spliti_input_drain"))

	// Key routing: dispatch keys to the focused panel; if the panel
	// doesn't handle it, run global shortcuts.
	a.AddSystems(schedule.First, app.System(routeKeys).Label("__editor_keys").After("__editor_mouse"))

	// Toolbar action handler: reads the per-frame ToolbarAction request
	// and applies it (transition state, save, etc.).
	a.AddSystems(schedule.PreUpdate, app.System(applyToolbarAction).Label("__editor_toolbar_apply"))

	// Asset open / sprite save handlers — dispatched by their respective
	// panel-side requests.
	a.AddSystems(schedule.PreUpdate, app.System(applyAssetOpen).Label("__editor_asset_open"))
	a.AddSystems(schedule.PreUpdate, app.System(applySpriteSave).Label("__editor_sprite_save"))
	a.AddSystems(schedule.PreUpdate, app.System(applyCreateActions).Label("__editor_create"))

	// Render: one overlay system that walks the layout and calls Render
	// on every panel except the Viewport (whose interior is the live
	// world drawn by tui's existing render system). Viewport's chrome
	// IS still drawn here — we render its border but skip its interior.
	tui.AddOverlay(a, renderAllPanels)

	// Play/Stop snapshot wiring. Pause→Play and Editing→Play both go
	// through OnEnter[Playing]; we only want to capture on the
	// Editing→Play transition (so Pause→Play resumes from the same
	// frame). We do this by capturing only when the snapshot is empty
	// (set on Stop) and clearing it on Stop. This is one of the
	// trade-offs of arche's state-machine API not exposing the previous
	// state inside OnEnter.
	app.OnEnter(a, state.Playing, func(c *app.Ctx) {
		snap := app.GetResource[PlaySnapshot](c)
		if snap == nil {
			return
		}
		if len(snap.Entries) == 0 {
			*snap = *CaptureSnapshot(c)
		}
	})
	// OnExit[Playing] is intentionally NOT used. OnExit fires for both
	// Play→Pause and Play→Editing, and Pause must not restore. The
	// Stop action explicitly calls RestoreSnapshot in
	// applyToolbarAction so we know the transition is Play→Editing.
}

// syncViewport copies the viewport panel's rect into the Viewport
// resource, leaving room for the panel's 1-cell border on each side. The
// world thus renders inside the visible interior of the panel.
func syncViewport(c *app.Ctx) {
	l := app.GetResource[ui.Layout](c)
	vp := app.GetResource[viewport.Viewport](c)
	if l == nil || vp == nil {
		return
	}
	r := l.Rect(ui.NameViewport)
	if r.W <= 2 || r.H <= 2 {
		vp.Active = false
		return
	}
	*vp = viewport.Viewport{
		X:      r.X + 1,
		Y:      r.Y + 1,
		W:      r.W - 2,
		H:      r.H - 2,
		Active: true,
	}
}

// routeMouse reads MouseEvents and dispatches each to the panel under
// its cursor. Click-down updates focus. While the modal Prompt is
// active, mouse events are swallowed (the prompt has no clickable
// content in v1; clicks-outside don't dismiss).
func routeMouse(c *app.Ctx) {
	events := app.ReadEvents[input.MouseEvent](c)
	if len(events) == 0 {
		return
	}
	if pr := app.GetResource[panels.Prompt](c); pr != nil && pr.Active {
		return
	}
	l := app.GetResource[ui.Layout](c)
	if l == nil {
		return
	}
	if pk := app.GetResource[panels.Picker](c); pk != nil && pk.Active {
		// Route mouse to picker (centered modal) using its current rect.
		r := panels.PromptRect(l.W, l.H, 40, 12)
		for _, ev := range events {
			if r.Contains(ev.X, ev.Y) {
				pk.OnMouse(c, ev.X-r.X, ev.Y-r.Y, ev)
			}
		}
		return
	}
	for _, ev := range events {
		p := l.PanelAt(ev.X, ev.Y)
		if p == nil {
			continue
		}
		if ev.Buttons&tcell.Button1 != 0 {
			l.Focused = p.Name()
		}
		r := l.Rect(p.Name())
		p.OnMouse(c, ev.X-r.X, ev.Y-r.Y, ev)
	}
}

// routeKeys dispatches keys to the focused panel; unhandled keys hit
// global shortcuts. While the modal Prompt is active, ALL keys go to
// the prompt and global shortcuts are suppressed (prevents Esc/F-keys
// from interfering with modal text entry).
func routeKeys(c *app.Ctx) {
	events := app.ReadEvents[input.KeyEvent](c)
	if len(events) == 0 {
		return
	}
	if pr := app.GetResource[panels.Prompt](c); pr != nil && pr.Active {
		for _, ev := range events {
			pr.OnKey(c, ev)
		}
		return
	}
	if pk := app.GetResource[panels.Picker](c); pk != nil && pk.Active {
		for _, ev := range events {
			pk.OnKey(c, ev)
		}
		return
	}
	l := app.GetResource[ui.Layout](c)
	if l == nil {
		return
	}
	for _, ev := range events {
		// Global shortcuts run FIRST so they always work regardless of
		// which panel has focus.
		if globalShortcut(c, ev) {
			continue
		}
		focused := l.PanelByName(l.Focused)
		if focused == nil {
			continue
		}
		focused.OnKey(c, ev)
	}
}

// globalShortcut handles editor-wide hotkeys: Ctrl-Q quit, Ctrl-S save,
// F5 play, F6 pause, F7 step, F8 stop. Returns true if handled.
func globalShortcut(c *app.Ctx, ev input.KeyEvent) bool {
	switch ev.Key {
	case tcell.KeyCtrlQ, tcell.KeyCtrlC:
		c.App().Stop()
		return true
	case tcell.KeyCtrlS:
		req := app.GetResource[panels.ToolbarAction](c)
		if req != nil {
			req.Action = "save"
		}
		return true
	case tcell.KeyF5:
		setMode(c, state.Playing)
		return true
	case tcell.KeyF6:
		setMode(c, state.Paused)
		return true
	case tcell.KeyF7:
		// Step: only meaningful when Paused. Phase I implements actual stepping.
		req := app.GetResource[panels.ToolbarAction](c)
		if req != nil {
			req.Action = "step"
		}
		return true
	case tcell.KeyF8, tcell.KeyEscape:
		setMode(c, state.Editing)
		return true
	case tcell.KeyCtrlE:
		// "New entity" is global because it's a frequent action; users
		// shouldn't have to focus the hierarchy panel first.
		CreateNewEntity(c)
		return true
	case tcell.KeyCtrlN:
		// "New scene". Ctrl-Shift-N would be cleaner but tcell doesn't
		// expose Shift on Ctrl-letter combos uniformly across terminals.
		CreateNewScene(c)
		return true
	case tcell.KeyCtrlT:
		// "New type" — opens the custom-component-type creation prompt.
		CreateCustomComponentType(c)
		return true
	case tcell.KeyCtrlP:
		// "New project".
		CreateNewProject(c)
		return true
	}
	return false
}

// setMode requests a state transition. Actual OnEnter/OnExit hooks are
// installed in Phase I to drive snapshot/restore.
func setMode(c *app.Ctx, m state.Mode) {
	st := app.GetState[state.Mode](c)
	if st == nil {
		return
	}
	st.Set(m)
}

// applyToolbarAction reads ToolbarAction.Action, dispatches it, and clears
// it. v1 only handles state transitions and save; "save" is wired in
// Phase H, so it's a no-op message here.
func applyToolbarAction(c *app.Ctx) {
	req := app.GetResource[panels.ToolbarAction](c)
	if req == nil || req.Action == "" {
		return
	}
	action := req.Action
	req.Action = ""
	switch action {
	case "play":
		setMode(c, state.Playing)
	case "pause":
		setMode(c, state.Paused)
	case "stop":
		// Stop restores the pre-play snapshot. This is the "rewind to
		// the saved scene" semantics — distinct from Pause, which only
		// freezes the simulation.
		snap := app.GetResource[PlaySnapshot](c)
		if snap != nil && len(snap.Entries) > 0 {
			RestoreSnapshot(c, snap)
			snap.Entries = nil
		}
		setMode(c, state.Editing)
	case "step":
		// Manually run one FixedUpdate while paused.
		if app.GetState[state.Mode](c).Get() == state.Paused {
			c.App().RunStage(schedule.FixedUpdate)
		}
	case "save":
		saveCurrentScene(c)
	}
}

// saveCurrentScene writes every EditorMeta entity to the project's
// scene file. The destination is determined by the project's
// DefaultScene field; if no project is loaded, the action is a no-op
// with a status-bar warning.
func saveCurrentScene(c *app.Ctx) {
	p := app.GetResource[project.Project](c)
	if p == nil {
		setStatus(c, "save: no project loaded", tcell.StyleDefault.Foreground(tcell.ColorRed))
		return
	}
	name := p.File.DefaultScene
	if name == "" {
		name = "main"
	}
	path := p.ScenePathByName(name)
	if path == "" {
		// First-time save: place it under <project>/scenes/<name>.scene.
		path = p.Dir + "/scenes/" + name + ".scene"
	}
	if err := SaveScene(c, path, name); err != nil {
		setStatus(c, "save: "+err.Error(), tcell.StyleDefault.Foreground(tcell.ColorRed))
		return
	}
	// Refresh the project's scene-paths list so future "Reload" calls
	// see the new file.
	if p2, err := project.LoadProject(p.Dir); err == nil {
		*p = *p2
	}
	setStatus(c, "saved scene", tcell.StyleDefault.Foreground(tcell.ColorGreen))
}

// setStatus is a small helper for systems that want to report to the
// status bar. Centralised so the StatusMessage style/text shape stays
// consistent.
func setStatus(c *app.Ctx, text string, style tcell.Style) {
	msg := app.GetResource[panels.StatusMessage](c)
	if msg == nil {
		return
	}
	msg.Text = text
	msg.Style = style
}

// renderAllPanels iterates the layout and renders each panel. The
// viewport panel's interior is already drawn by the tui plugin; here we
// only render its chrome (border + title), so it's part of the same
// loop. The order of registration determines z-order; later panels draw
// on top.
//
// The modal Prompt, when active, is drawn LAST at a centered rect so
// it overlays everything else.
func renderAllPanels(c *app.Ctx) {
	l := app.GetResource[ui.Layout](c)
	if l == nil {
		return
	}
	for _, p := range l.Panels() {
		r := l.Rect(p.Name())
		if r.W == 0 || r.H == 0 {
			continue
		}
		focused := l.Focused == p.Name()
		p.Render(c, r, focused)
	}
	if pr := app.GetResource[panels.Prompt](c); pr != nil && pr.Active {
		pr.Render(c, panels.PromptRect(l.W, l.H, 50, 7), true)
	}
	if pk := app.GetResource[panels.Picker](c); pk != nil && pk.Active {
		pk.Render(c, panels.PromptRect(l.W, l.H, 40, 12), true)
	}
}

// MakeRuntimeRunIf returns the predicate the runtime plugin should use:
// "is editor in Playing state?". Wire by passing it to runtime.Plugin{}.
//
// Wiring this from the editor plugin would create a cyclic dependency
// (runtime imports editor → editor imports runtime), so we expose the
// predicate as a constructor and main.go composes it.
func MakeRuntimeRunIf() func(*app.Ctx) bool {
	return func(c *app.Ctx) bool {
		st := app.GetState[state.Mode](c)
		if st == nil {
			return false
		}
		return st.Get() == state.Playing
	}
}

