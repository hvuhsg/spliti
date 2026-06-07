package ui

import (
	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
)

// feedInput drains this frame's render3d input events into ImGui's IO. Window
// coordinates are scaled to framebuffer pixels (the space we report as
// DisplaySize) so hit-testing and scissor rects line up on Retina displays.
func feedInput(c *app.Ctx, io *imgui.IO) {
	scale := float32(uiScale(c))

	for _, ev := range app.ReadEvents[render3d.MouseMoveEvent](c) {
		io.AddMousePosEvent(float32(ev.X)*scale, float32(ev.Y)*scale)
	}
	for _, ev := range app.ReadEvents[render3d.MouseButtonEvent](c) {
		// Anchor the pointer at the event position before the click so ImGui
		// associates the press with the right widget.
		io.AddMousePosEvent(float32(ev.X)*scale, float32(ev.Y)*scale)
		btn := int32(ev.Button) // GLFW left/right/middle = 0/1/2, same as ImGui
		if btn >= 0 && btn <= 4 {
			io.AddMouseButtonEvent(btn, ev.Action == glfw.Press)
		}
	}

	for _, ev := range app.ReadEvents[render3d.KeyEvent](c) {
		if ev.Rune != 0 { // typed text (Key is 0 for these)
			io.AddInputCharacter(uint32(ev.Rune))
			continue
		}
		// Keep the aggregate modifier chord in sync from the event's mod mask.
		io.AddKeyEvent(imgui.ModCtrl, ev.Mods&glfw.ModControl != 0)
		io.AddKeyEvent(imgui.ModShift, ev.Mods&glfw.ModShift != 0)
		io.AddKeyEvent(imgui.ModAlt, ev.Mods&glfw.ModAlt != 0)
		io.AddKeyEvent(imgui.ModSuper, ev.Mods&glfw.ModSuper != 0)
		if k, ok := glfwToImguiKey(ev.Key); ok {
			io.AddKeyEvent(k, ev.Action == glfw.Press || ev.Action == glfw.Repeat)
		}
	}
}

// uiScale is framebuffer-px per window-point (1 on non-Retina, 2 on Retina), so
// window-space input maps onto the framebuffer-px UI space. Mirrors the helper
// rf-lab's editor already uses.
func uiScale(c *app.Ctx) float64 {
	win := render3d.Window(c)
	if win == nil {
		return 1
	}
	fw, _ := win.GetFramebufferSize()
	lw, _ := win.GetSize()
	if lw == 0 {
		return 1
	}
	return float64(fw) / float64(lw)
}

// glfwToImguiKey maps the GLFW keys ImGui cares about (editing/navigation,
// letters, digits, modifiers) to their ImGui equivalents. Unmapped keys return
// false and are ignored — character input flows through AddInputCharacter.
func glfwToImguiKey(k glfw.Key) (imgui.Key, bool) {
	switch {
	case k >= glfw.KeyA && k <= glfw.KeyZ:
		return imgui.KeyA + imgui.Key(k-glfw.KeyA), true
	case k >= glfw.Key0 && k <= glfw.Key9:
		return imgui.Key0 + imgui.Key(k-glfw.Key0), true
	}
	if ik, ok := keyMap[k]; ok {
		return ik, true
	}
	return 0, false
}

var keyMap = map[glfw.Key]imgui.Key{
	glfw.KeyTab:          imgui.KeyTab,
	glfw.KeyLeft:         imgui.KeyLeftArrow,
	glfw.KeyRight:        imgui.KeyRightArrow,
	glfw.KeyUp:           imgui.KeyUpArrow,
	glfw.KeyDown:         imgui.KeyDownArrow,
	glfw.KeyHome:         imgui.KeyHome,
	glfw.KeyEnd:          imgui.KeyEnd,
	glfw.KeyDelete:       imgui.KeyDelete,
	glfw.KeyBackspace:    imgui.KeyBackspace,
	glfw.KeySpace:        imgui.KeySpace,
	glfw.KeyEnter:        imgui.KeyEnter,
	glfw.KeyKPEnter:      imgui.KeyKeypadEnter,
	glfw.KeyEscape:       imgui.KeyEscape,
	glfw.KeyLeftControl:  imgui.KeyLeftCtrl,
	glfw.KeyRightControl: imgui.KeyRightCtrl,
	glfw.KeyLeftShift:    imgui.KeyLeftShift,
	glfw.KeyRightShift:   imgui.KeyRightShift,
	glfw.KeyLeftAlt:      imgui.KeyLeftAlt,
	glfw.KeyRightAlt:     imgui.KeyRightAlt,
	glfw.KeyLeftSuper:    imgui.KeyLeftSuper,
	glfw.KeyRightSuper:   imgui.KeyRightSuper,
}
