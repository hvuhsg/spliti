// Package components holds the game's component structs. Every exported
// struct here is registered with the editor's inspector by spliti gen.
package components

// HexCell tags a board-marker entity with its axial coordinate, so the marker
// system can map the entity back to the wfc board cell it represents and color
// it (hidden / buildable / hovered) each frame.
type HexCell struct{ Q, R int }
