package webgpu

import "github.com/go-gl/glfw/v3.3/glfw"

// MouseButtonEvent is emitted for mouse-button activity. Button/Action/Mods are
// the raw GLFW values; X,Y is the cursor position at the time of the click, in
// GLFW window (screen) coordinates — not framebuffer pixels and not world units.
//
// Converting to world coordinates is the game's job, because only the game knows
// the Camera. The robust conversion divides by the window size (not the
// framebuffer size), so the HiDPI scale cancels:
//
//	winW, winH := webgpu.Window(c).GetSize()
//	worldX := float32(ev.X) / float32(winW) * cam.WorldW
//	worldY := float32(ev.Y) / float32(winH) * cam.WorldH
//
// Like KeyEvent, this uses GLFW types so the GPU backend stays tcell-free.
type MouseButtonEvent struct {
	Button glfw.MouseButton
	Action glfw.Action
	Mods   glfw.ModifierKey
	X, Y   float64
}

// MouseMoveEvent is emitted when the cursor moves. X,Y is the cursor position in
// GLFW window (screen) coordinates, same as MouseButtonEvent.
type MouseMoveEvent struct {
	X, Y float64
}
