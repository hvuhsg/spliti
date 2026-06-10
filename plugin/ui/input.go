//go:build !js

package ui

import (
	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
)

// feedInput drains this frame's input events into ImGui's IO. Window
// coordinates are scaled to framebuffer pixels (the space we report as
// DisplaySize) so hit-testing and scissor rects line up on Retina displays.
func feedInput(c *app.Ctx, io *imgui.IO) {
	scale := float32(uiScale(c))

	for _, ev := range app.ReadEvents[inputs.MouseMoveEvent](c) {
		io.AddMousePosEvent(float32(ev.X)*scale, float32(ev.Y)*scale)
	}
	for _, ev := range app.ReadEvents[inputs.MouseButtonEvent](c) {
		// Anchor the pointer at the event position before the click so ImGui
		// associates the press with the right widget.
		io.AddMousePosEvent(float32(ev.X)*scale, float32(ev.Y)*scale)
		btn := int32(ev.Button) // inputs left/right/middle = 0/1/2, same as ImGui
		if btn >= 0 && btn <= 4 {
			io.AddMouseButtonEvent(btn, ev.Action == inputs.Press)
		}
	}

	for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
		if ev.Rune != 0 { // typed text (Key is 0 for these)
			io.AddInputCharacter(uint32(ev.Rune))
			continue
		}
		// Keep the aggregate modifier chord in sync from the event's mod mask.
		io.AddKeyEvent(imgui.ModCtrl, ev.Mods&inputs.ModControl != 0)
		io.AddKeyEvent(imgui.ModShift, ev.Mods&inputs.ModShift != 0)
		io.AddKeyEvent(imgui.ModAlt, ev.Mods&inputs.ModAlt != 0)
		io.AddKeyEvent(imgui.ModSuper, ev.Mods&inputs.ModSuper != 0)
		if k, ok := toImguiKey(ev.Key); ok {
			io.AddKeyEvent(k, ev.Action == inputs.Press || ev.Action == inputs.Repeat)
		}
	}
}

// uiScale is framebuffer-px per window-point (1 on non-Retina, 2 on Retina), so
// window-space input maps onto the framebuffer-px UI space.
func uiScale(c *app.Ctx) float64 {
	fw, _ := render3d.Size(c)       // framebuffer pixels
	lw, _ := render3d.WindowSize(c) // window points
	if lw == 0 {
		return 1
	}
	return float64(fw) / float64(lw)
}

// toImguiKey maps the keys ImGui cares about (editing/navigation, letters,
// digits, modifiers) to their ImGui equivalents. Unmapped keys return false and
// are ignored — character input flows through AddInputCharacter.
func toImguiKey(k inputs.Key) (imgui.Key, bool) {
	switch {
	case k >= inputs.KeyA && k <= inputs.KeyZ:
		return imgui.KeyA + imgui.Key(k-inputs.KeyA), true
	case k >= inputs.Key0 && k <= inputs.Key9:
		return imgui.Key0 + imgui.Key(k-inputs.Key0), true
	}
	if ik, ok := keyMap[k]; ok {
		return ik, true
	}
	return 0, false
}

var keyMap = map[inputs.Key]imgui.Key{
	inputs.KeyTab:          imgui.KeyTab,
	inputs.KeyLeft:         imgui.KeyLeftArrow,
	inputs.KeyRight:        imgui.KeyRightArrow,
	inputs.KeyUp:           imgui.KeyUpArrow,
	inputs.KeyDown:         imgui.KeyDownArrow,
	inputs.KeyHome:         imgui.KeyHome,
	inputs.KeyEnd:          imgui.KeyEnd,
	inputs.KeyDelete:       imgui.KeyDelete,
	inputs.KeyBackspace:    imgui.KeyBackspace,
	inputs.KeySpace:        imgui.KeySpace,
	inputs.KeyEnter:        imgui.KeyEnter,
	inputs.KeyKPEnter:      imgui.KeyKeypadEnter,
	inputs.KeyEscape:       imgui.KeyEscape,
	inputs.KeyLeftControl:  imgui.KeyLeftCtrl,
	inputs.KeyRightControl: imgui.KeyRightCtrl,
	inputs.KeyLeftShift:    imgui.KeyLeftShift,
	inputs.KeyRightShift:   imgui.KeyRightShift,
	inputs.KeyLeftAlt:      imgui.KeyLeftAlt,
	inputs.KeyRightAlt:     imgui.KeyRightAlt,
	inputs.KeyLeftSuper:    imgui.KeyLeftSuper,
	inputs.KeyRightSuper:   imgui.KeyRightSuper,
}
