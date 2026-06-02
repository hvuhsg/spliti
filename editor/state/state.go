// Package state holds editor-wide types referenced by both the top-level
// editor package and its UI panels. Keeping them here is what breaks the
// otherwise-cyclic editor ↔ editor/ui/panels dependency: panels need
// EditorMeta to filter the hierarchy and Mode to render the toolbar,
// while the editor's Plugin needs panels (to install them in the
// Layout). Both sides import this leaf package instead.
package state

import "github.com/mlange-42/arche/ecs"

// EditorMeta is attached to every entity loaded from a scene file. It
// distinguishes scene entities (which save/load round-trip, hierarchy
// lists, and snapshot-restore tracks) from internal entities the editor
// itself might spawn.
//
// SceneName is the user-typed name preserved across save/load. Display
// name in the hierarchy panel falls back through Tag.Name → SceneName →
// "entity_{id}".
type EditorMeta struct {
	SceneName string
}

// Selection is the editor-wide singleton resource for the current
// selected entity. Inspector reads it; hierarchy + viewport panels write
// it on click. Active=false distinguishes "deliberately empty selection"
// from "uninitialised".
type Selection struct {
	Entity ecs.Entity
	Active bool
}

// Mode is the editor's state-machine value. Editing pauses the runtime;
// Playing ticks it; Paused freezes the runtime mid-play.
type Mode int

const (
	Editing Mode = iota
	Playing
	Paused
)

// String returns "Editing", "Playing", or "Paused" for state-machine
// debug output and toolbar rendering.
func (m Mode) String() string {
	switch m {
	case Editing:
		return "Editing"
	case Playing:
		return "Playing"
	case Paused:
		return "Paused"
	}
	return "?"
}
