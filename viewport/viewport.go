// Package viewport defines a clip-rect resource the tui renderer honors when
// drawing entities. Without a Viewport (or with Active=false), the tui plugin
// renders to the full terminal as before — every existing example keeps
// working unchanged.
//
// The editor uses Viewport to embed the live world inside its center panel:
// it inserts a Viewport resource matching the panel's screen rect, and the
// tui renderer clears, clips, and offsets all of its SetContent calls into
// that rect.
//
// Coordinates: Position{X,Y} on entities is always in world (viewport-local)
// coordinates. The renderer offsets by (X,Y) at draw time. Cells with
// world-coords outside [0,W) x [0,H) are skipped.
package viewport

// Viewport is the rect of screen cells the tui renderer should draw into.
// X,Y is the top-left in screen coords; W,H is the size of the rect. When
// Active is false, the tui renderer ignores the Viewport entirely and uses
// the full screen.
type Viewport struct {
	X, Y, W, H int
	Active     bool
}
