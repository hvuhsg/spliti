package panels

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/components"
	"github.com/hvuhsg/spliti/editor/state"
	"github.com/hvuhsg/spliti/editor/ui"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/tui"

	"github.com/mlange-42/arche/ecs"
)

// Inspector is the right-side panel showing the selected entity's
// components and their values. It builds a flat row list every frame
// from the components registry and the entity's actual archetype:
//
//	[Position]      ← header
//	X: 5
//	Y: 10
//	[Sprite]
//	Ref: paddle
//	...
//
// Headers aren't selectable; arrow keys move SelectedField across only
// the editable Field rows. While focused on a row:
//
//	Int   : ← / → decrement / increment by 1; digits replace
//	String: typeable; backspace deletes
//	Bool  : space toggles
//	Rune  : any printable rune replaces
//	Style : read-only summary in v1
//	KeyB. : read-only summary in v1
type Inspector struct {
	SelectedField int     // index into the flat editable-row list
	textBuffer    string  // partial in-progress text edit (string fields)
	editingFor    int     // SelectedField index that owns textBuffer; -1 = none
}

// Name implements ui.Panel.
func (i *Inspector) Name() string { return ui.NameInspector }

// Title implements ui.Panel.
func (i *Inspector) Title() string { return "Inspector" }

// inspectorRow represents one render line in the inspector. Headers are
// non-editable (Field == nil); field rows carry a Field+ComponentDesc
// pair so the key handler knows which Set to call.
type inspectorRow struct {
	IsHeader bool
	Title    string                       // header text or "  field: value"
	Desc     *components.ComponentDesc    // for field rows
	Field    *components.Field            // nil for headers
}

// buildRows iterates the registry, includes only components present on
// the selected entity, and produces a flat ordered list.
func (i *Inspector) buildRows(c *app.Ctx, e ecs.Entity) []inspectorRow {
	rows := []inspectorRow{}
	world := c.World()
	for _, d := range components.Registry() {
		if !d.Has(world, e) {
			continue
		}
		rows = append(rows, inspectorRow{IsHeader: true, Title: "[" + d.Name + "]"})
		for fi := range d.Fields {
			f := d.Fields[fi]
			rows = append(rows, inspectorRow{
				Desc:  d,
				Field: &d.Fields[fi],
				Title: formatFieldRow(c, e, &f),
			})
		}
	}
	return rows
}

// formatFieldRow renders the "name: value" string for one field.
func formatFieldRow(c *app.Ctx, e ecs.Entity, f *components.Field) string {
	v := f.Get(c, e)
	switch f.Kind {
	case components.FieldRune:
		return fmt.Sprintf("  %s: %c", f.Name, v.(rune))
	case components.FieldStyle:
		return fmt.Sprintf("  %s: <style>", f.Name)
	case components.FieldKeyBindings:
		// Show binding count so the user has at least a hint.
		if vs, ok := v.([]any); ok {
			return fmt.Sprintf("  %s: <%d bindings>", f.Name, len(vs))
		}
		// runtime.KeyBinding slice — count via reflection-free approach:
		return fmt.Sprintf("  %s: <bindings>", f.Name)
	default:
		return fmt.Sprintf("  %s: %v", f.Name, v)
	}
}

// editableFieldRows filters rows to only the field entries (no headers).
// Used by SelectedField indexing.
func editableFieldRows(rows []inspectorRow) []int {
	var out []int
	for i, r := range rows {
		if !r.IsHeader {
			out = append(out, i)
		}
	}
	return out
}

// Render implements ui.Panel.
func (i *Inspector) Render(c *app.Ctx, r ui.Rect, focused bool) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	ui.DrawFill(s, r, ' ', tcell.StyleDefault)
	ui.DrawBox(s, r, "Inspector", focused)

	inner := r.Inset(1)
	if inner.W <= 0 || inner.H <= 0 {
		return
	}

	sel := app.GetResource[state.Selection](c)
	if sel == nil || !sel.Active {
		ui.DrawTextClipped(s, inner.X, inner.Y, inner.W, ui.StyleTextDim, "(no selection)")
		return
	}
	if !c.World().Alive(sel.Entity) {
		ui.DrawTextClipped(s, inner.X, inner.Y, inner.W, ui.StyleTextDim, "(stale entity)")
		sel.Active = false
		return
	}

	header := fmt.Sprintf("Entity #%d", sel.Entity.ID())
	ui.DrawTextClipped(s, inner.X, inner.Y, inner.W, ui.StyleTitle, header)

	rows := i.buildRows(c, sel.Entity)
	if len(rows) == 0 {
		ui.DrawTextClipped(s, inner.X, inner.Y+2, inner.W, ui.StyleTextDim, "(no components)")
		return
	}
	editable := editableFieldRows(rows)
	if i.SelectedField < 0 {
		i.SelectedField = 0
	}
	if i.SelectedField >= len(editable) {
		i.SelectedField = len(editable) - 1
	}
	selectedRowIdx := -1
	if len(editable) > 0 {
		selectedRowIdx = editable[i.SelectedField]
	}

	startY := inner.Y + 2
	for ri, row := range rows {
		y := startY + ri
		if y >= inner.Y+inner.H {
			break
		}
		style := ui.StyleText
		if row.IsHeader {
			style = ui.StyleTitle
		}
		if ri == selectedRowIdx && focused {
			style = ui.StyleSelected
			ui.DrawHLine(s, inner.X, y, inner.W, ' ', style)
		}
		title := row.Title
		// Append in-progress text-buffer when actively editing this row.
		if !row.IsHeader && i.editingFor == i.SelectedField && ri == selectedRowIdx && row.Field != nil &&
			(row.Field.Kind == components.FieldString || row.Field.Kind == components.FieldInt) {
			title += " ←"
		}
		ui.DrawTextClipped(s, inner.X, y, inner.W, style, title)
	}
}

// OnMouse implements ui.Panel. Click on a field row selects it; future
// double-click could enter an explicit edit mode.
func (i *Inspector) OnMouse(c *app.Ctx, _, ly int, ev input.MouseEvent) {
	if ev.Buttons&tcell.Button1 == 0 {
		return
	}
	sel := app.GetResource[state.Selection](c)
	if sel == nil || !sel.Active {
		return
	}
	rows := i.buildRows(c, sel.Entity)
	editable := editableFieldRows(rows)
	// ly = 1 → border; ly=2 → "Entity #N" header; ly=3 → row index 0.
	rowIdx := ly - 1 - 2
	for k, idx := range editable {
		if idx == rowIdx {
			i.SelectedField = k
			i.editingFor = -1
			i.textBuffer = ""
			return
		}
	}
}

// OnKey implements ui.Panel. Up/Down navigates rows; per-Kind keys edit.
// Ctrl-A adds the next not-present built-in component to the selected
// entity; Ctrl-D removes the currently focused component.
func (i *Inspector) OnKey(c *app.Ctx, ev input.KeyEvent) bool {
	sel := app.GetResource[state.Selection](c)
	if sel == nil || !sel.Active {
		return false
	}

	// Add/remove first — they don't depend on a selected field.
	switch ev.Key {
	case tcell.KeyCtrlA:
		i.addNextMissingComponent(c, sel.Entity)
		return true
	case tcell.KeyCtrlD:
		i.removeFocusedComponent(c, sel.Entity)
		return true
	}

	rows := i.buildRows(c, sel.Entity)
	editable := editableFieldRows(rows)
	if len(editable) == 0 {
		return false
	}
	switch ev.Key {
	case tcell.KeyUp:
		if i.SelectedField > 0 {
			i.SelectedField--
		}
		i.editingFor = -1
		return true
	case tcell.KeyDown:
		if i.SelectedField < len(editable)-1 {
			i.SelectedField++
		}
		i.editingFor = -1
		return true
	}
	row := rows[editable[i.SelectedField]]
	if row.Field == nil {
		return false
	}
	return i.handleFieldKey(c, sel.Entity, row.Field, ev)
}

// addNextMissingComponent opens the picker modal listing every
// registry component the entity does NOT already have. The user picks
// one by arrow + Enter (or click); the callback calls Add.
//
// Falls back silently if there's no picker resource (e.g. tests that
// don't install one).
func (i *Inspector) addNextMissingComponent(c *app.Ctx, e ecs.Entity) {
	pk := app.GetResource[Picker](c)
	if pk == nil {
		// Fallback: cycle through and add the first missing one. Keeps
		// the test suite that doesn't install a picker working.
		for _, d := range components.Registry() {
			if !d.Has(c.World(), e) {
				d.Add(c, e)
				return
			}
		}
		return
	}
	var missing []string
	var missingDesc []*components.ComponentDesc
	for _, d := range components.Registry() {
		if !d.Has(c.World(), e) {
			missing = append(missing, d.Name)
			missingDesc = append(missingDesc, d)
		}
	}
	if len(missing) == 0 {
		return
	}
	pk.Open("Add component", missing, func(c *app.Ctx, idx int, _ string) {
		if idx < 0 || idx >= len(missingDesc) {
			return
		}
		missingDesc[idx].Add(c, e)
	}, nil)
}

// removeFocusedComponent removes the component that owns the currently
// focused field. No-op if nothing is focused.
func (i *Inspector) removeFocusedComponent(c *app.Ctx, e ecs.Entity) {
	rows := i.buildRows(c, e)
	editable := editableFieldRows(rows)
	if len(editable) == 0 || i.SelectedField >= len(editable) {
		return
	}
	row := rows[editable[i.SelectedField]]
	if row.Desc == nil {
		return
	}
	row.Desc.Remove(c, e)
	i.editingFor = -1
}

// handleFieldKey applies an edit to the given field based on its Kind.
// Returns true when the key was consumed.
func (i *Inspector) handleFieldKey(c *app.Ctx, e ecs.Entity, f *components.Field, ev input.KeyEvent) bool {
	switch f.Kind {
	case components.FieldInt:
		switch ev.Key {
		case tcell.KeyLeft:
			f.Set(c, e, f.Get(c, e).(int)-1)
			return true
		case tcell.KeyRight:
			f.Set(c, e, f.Get(c, e).(int)+1)
			return true
		}
		// Allow digit-input replacing the value when typed in sequence:
		// number-mode is a tiny FSM held in textBuffer.
		if ev.Rune >= '0' && ev.Rune <= '9' {
			if i.editingFor != i.SelectedField {
				i.textBuffer = ""
				i.editingFor = i.SelectedField
			}
			i.textBuffer += string(ev.Rune)
			n := 0
			for _, r := range i.textBuffer {
				n = n*10 + int(r-'0')
			}
			f.Set(c, e, n)
			return true
		}
		if ev.Rune == '-' && i.textBuffer == "" {
			f.Set(c, e, -f.Get(c, e).(int))
			return true
		}
	case components.FieldString:
		// Single-line text edit. Esc commits + exits.
		if i.editingFor != i.SelectedField {
			i.textBuffer = f.Get(c, e).(string)
			i.editingFor = i.SelectedField
		}
		if ev.Key == tcell.KeyBackspace || ev.Key == tcell.KeyBackspace2 {
			if len(i.textBuffer) > 0 {
				rs := []rune(i.textBuffer)
				i.textBuffer = string(rs[:len(rs)-1])
				f.Set(c, e, i.textBuffer)
			}
			return true
		}
		if ev.Rune != 0 && ev.Rune >= 32 {
			i.textBuffer += string(ev.Rune)
			f.Set(c, e, i.textBuffer)
			return true
		}
	case components.FieldBool:
		if ev.Rune == ' ' || ev.Key == tcell.KeyEnter {
			f.Set(c, e, !f.Get(c, e).(bool))
			return true
		}
	case components.FieldRune:
		if ev.Rune != 0 && ev.Rune >= 32 {
			f.Set(c, e, ev.Rune)
			return true
		}
	}
	return false
}
