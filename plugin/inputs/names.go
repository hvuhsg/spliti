package inputs

// Bidirectional tables between input constants and their Go identifier names
// ("KeySpace", "MouseButtonLeft", "GamepadA", "AxisLeftX"). The spliti editor
// uses them to read and write input-binding source code; games can use them
// for settings menus and saved keybindings.

var keyNames = map[Key]string{
	KeySpace: "KeySpace", KeyApostrophe: "KeyApostrophe", KeyComma: "KeyComma",
	KeyMinus: "KeyMinus", KeyPeriod: "KeyPeriod", KeySlash: "KeySlash",
	Key0: "Key0", Key1: "Key1", Key2: "Key2", Key3: "Key3", Key4: "Key4",
	Key5: "Key5", Key6: "Key6", Key7: "Key7", Key8: "Key8", Key9: "Key9",
	KeySemicolon: "KeySemicolon", KeyEqual: "KeyEqual",
	KeyA: "KeyA", KeyB: "KeyB", KeyC: "KeyC", KeyD: "KeyD", KeyE: "KeyE",
	KeyF: "KeyF", KeyG: "KeyG", KeyH: "KeyH", KeyI: "KeyI", KeyJ: "KeyJ",
	KeyK: "KeyK", KeyL: "KeyL", KeyM: "KeyM", KeyN: "KeyN", KeyO: "KeyO",
	KeyP: "KeyP", KeyQ: "KeyQ", KeyR: "KeyR", KeyS: "KeyS", KeyT: "KeyT",
	KeyU: "KeyU", KeyV: "KeyV", KeyW: "KeyW", KeyX: "KeyX", KeyY: "KeyY",
	KeyZ:           "KeyZ",
	KeyLeftBracket: "KeyLeftBracket", KeyBackslash: "KeyBackslash",
	KeyRightBracket: "KeyRightBracket", KeyGraveAccent: "KeyGraveAccent",
	KeyEscape: "KeyEscape", KeyEnter: "KeyEnter", KeyTab: "KeyTab",
	KeyBackspace: "KeyBackspace", KeyInsert: "KeyInsert", KeyDelete: "KeyDelete",
	KeyRight: "KeyRight", KeyLeft: "KeyLeft", KeyDown: "KeyDown", KeyUp: "KeyUp",
	KeyPageUp: "KeyPageUp", KeyPageDown: "KeyPageDown", KeyHome: "KeyHome",
	KeyEnd: "KeyEnd", KeyCapsLock: "KeyCapsLock", KeyScrollLock: "KeyScrollLock",
	KeyNumLock: "KeyNumLock", KeyPrintScreen: "KeyPrintScreen", KeyPause: "KeyPause",
	KeyF1: "KeyF1", KeyF2: "KeyF2", KeyF3: "KeyF3", KeyF4: "KeyF4",
	KeyF5: "KeyF5", KeyF6: "KeyF6", KeyF7: "KeyF7", KeyF8: "KeyF8",
	KeyF9: "KeyF9", KeyF10: "KeyF10", KeyF11: "KeyF11", KeyF12: "KeyF12",
	KeyKP0: "KeyKP0", KeyKP1: "KeyKP1", KeyKP2: "KeyKP2", KeyKP3: "KeyKP3",
	KeyKP4: "KeyKP4", KeyKP5: "KeyKP5", KeyKP6: "KeyKP6", KeyKP7: "KeyKP7",
	KeyKP8: "KeyKP8", KeyKP9: "KeyKP9", KeyKPDecimal: "KeyKPDecimal",
	KeyKPDivide: "KeyKPDivide", KeyKPMultiply: "KeyKPMultiply",
	KeyKPSubtract: "KeyKPSubtract", KeyKPAdd: "KeyKPAdd", KeyKPEnter: "KeyKPEnter",
	KeyKPEqual:   "KeyKPEqual",
	KeyLeftShift: "KeyLeftShift", KeyLeftControl: "KeyLeftControl",
	KeyLeftAlt: "KeyLeftAlt", KeyLeftSuper: "KeyLeftSuper",
	KeyRightShift: "KeyRightShift", KeyRightControl: "KeyRightControl",
	KeyRightAlt: "KeyRightAlt", KeyRightSuper: "KeyRightSuper", KeyMenu: "KeyMenu",
}

var mouseButtonNames = map[MouseButton]string{
	MouseButtonLeft:   "MouseButtonLeft",
	MouseButtonRight:  "MouseButtonRight",
	MouseButtonMiddle: "MouseButtonMiddle",
	MouseButton4:      "MouseButton4",
	MouseButton5:      "MouseButton5",
	MouseButton6:      "MouseButton6",
	MouseButton7:      "MouseButton7",
	MouseButton8:      "MouseButton8",
}

var gamepadButtonNames = map[GamepadButton]string{
	GamepadA: "GamepadA", GamepadB: "GamepadB", GamepadX: "GamepadX",
	GamepadY: "GamepadY", GamepadLeftBumper: "GamepadLeftBumper",
	GamepadRightBumper: "GamepadRightBumper", GamepadBack: "GamepadBack",
	GamepadStart: "GamepadStart", GamepadGuide: "GamepadGuide",
	GamepadLeftThumb: "GamepadLeftThumb", GamepadRightThumb: "GamepadRightThumb",
	GamepadDpadUp: "GamepadDpadUp", GamepadDpadRight: "GamepadDpadRight",
	GamepadDpadDown: "GamepadDpadDown", GamepadDpadLeft: "GamepadDpadLeft",
}

var gamepadAxisNames = map[GamepadAxis]string{
	AxisLeftX: "AxisLeftX", AxisLeftY: "AxisLeftY",
	AxisRightX: "AxisRightX", AxisRightY: "AxisRightY",
	AxisLeftTrigger: "AxisLeftTrigger", AxisRightTrigger: "AxisRightTrigger",
}

var (
	keysByName           = invert(keyNames)
	mouseButtonsByName   = invert(mouseButtonNames)
	gamepadButtonsByName = invert(gamepadButtonNames)
	gamepadAxesByName    = invert(gamepadAxisNames)
)

func invert[K comparable](m map[K]string) map[string]K {
	out := make(map[string]K, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}

// KeyName returns the Go identifier of k ("KeySpace"), or "".
func KeyName(k Key) string { return keyNames[k] }

// KeyByName resolves a key identifier back to its constant.
func KeyByName(name string) (Key, bool) { k, ok := keysByName[name]; return k, ok }

// MouseButtonName returns the Go identifier of b ("MouseButtonLeft"), or "".
func MouseButtonName(b MouseButton) string { return mouseButtonNames[b] }

// MouseButtonByName resolves a mouse-button identifier back to its constant.
func MouseButtonByName(name string) (MouseButton, bool) {
	b, ok := mouseButtonsByName[name]
	return b, ok
}

// GamepadButtonName returns the Go identifier of b ("GamepadA"), or "".
func GamepadButtonName(b GamepadButton) string { return gamepadButtonNames[b] }

// GamepadButtonByName resolves a gamepad-button identifier to its constant.
func GamepadButtonByName(name string) (GamepadButton, bool) {
	b, ok := gamepadButtonsByName[name]
	return b, ok
}

// GamepadAxisName returns the Go identifier of a ("AxisLeftX"), or "".
func GamepadAxisName(a GamepadAxis) string { return gamepadAxisNames[a] }

// GamepadAxisByName resolves a gamepad-axis identifier to its constant.
func GamepadAxisByName(name string) (GamepadAxis, bool) {
	a, ok := gamepadAxesByName[name]
	return a, ok
}
