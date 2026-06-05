package render3d

import "github.com/go-gl/glfw/v3.3/glfw"

// MouseButtonEvent is emitted on a mouse button press or release. X,Y are the
// cursor position in window (screen) coordinates at the time of the event — not
// framebuffer pixels and not world coordinates.
type MouseButtonEvent struct {
	Button glfw.MouseButton
	Action glfw.Action
	Mods   glfw.ModifierKey
	X, Y   float64
}

// MouseMoveEvent is emitted when the cursor moves. X,Y are window (screen)
// coordinates.
type MouseMoveEvent struct {
	X, Y float64
}
