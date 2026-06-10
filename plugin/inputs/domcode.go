package inputs

// KeyFromDOMCode maps a browser KeyboardEvent.code string (a physical, layout-
// independent key identifier, e.g. "KeyW", "ArrowUp", "Escape") to the canonical
// Key. Unmapped codes return KeyUnknown. The js render backends use this to
// translate DOM keyboard events; it lives here (rather than duplicated per
// backend) and is a plain lookup with no platform imports.
func KeyFromDOMCode(code string) Key {
	if k, ok := domCodeToKey[code]; ok {
		return k
	}
	return KeyUnknown
}

var domCodeToKey = map[string]Key{
	// Letters.
	"KeyA": KeyA, "KeyB": KeyB, "KeyC": KeyC, "KeyD": KeyD, "KeyE": KeyE,
	"KeyF": KeyF, "KeyG": KeyG, "KeyH": KeyH, "KeyI": KeyI, "KeyJ": KeyJ,
	"KeyK": KeyK, "KeyL": KeyL, "KeyM": KeyM, "KeyN": KeyN, "KeyO": KeyO,
	"KeyP": KeyP, "KeyQ": KeyQ, "KeyR": KeyR, "KeyS": KeyS, "KeyT": KeyT,
	"KeyU": KeyU, "KeyV": KeyV, "KeyW": KeyW, "KeyX": KeyX, "KeyY": KeyY,
	"KeyZ": KeyZ,

	// Number row.
	"Digit0": Key0, "Digit1": Key1, "Digit2": Key2, "Digit3": Key3, "Digit4": Key4,
	"Digit5": Key5, "Digit6": Key6, "Digit7": Key7, "Digit8": Key8, "Digit9": Key9,

	// Punctuation.
	"Space":        KeySpace,
	"Minus":        KeyMinus,
	"Equal":        KeyEqual,
	"BracketLeft":  KeyLeftBracket,
	"BracketRight": KeyRightBracket,
	"Backslash":    KeyBackslash,
	"Semicolon":    KeySemicolon,
	"Quote":        KeyApostrophe,
	"Backquote":    KeyGraveAccent,
	"Comma":        KeyComma,
	"Period":       KeyPeriod,
	"Slash":        KeySlash,

	// Editing and navigation.
	"Escape":      KeyEscape,
	"Enter":       KeyEnter,
	"Tab":         KeyTab,
	"Backspace":   KeyBackspace,
	"Insert":      KeyInsert,
	"Delete":      KeyDelete,
	"ArrowRight":  KeyRight,
	"ArrowLeft":   KeyLeft,
	"ArrowDown":   KeyDown,
	"ArrowUp":     KeyUp,
	"PageUp":      KeyPageUp,
	"PageDown":    KeyPageDown,
	"Home":        KeyHome,
	"End":         KeyEnd,
	"CapsLock":    KeyCapsLock,
	"ScrollLock":  KeyScrollLock,
	"NumLock":     KeyNumLock,
	"PrintScreen": KeyPrintScreen,
	"Pause":       KeyPause,

	// Function keys.
	"F1": KeyF1, "F2": KeyF2, "F3": KeyF3, "F4": KeyF4, "F5": KeyF5, "F6": KeyF6,
	"F7": KeyF7, "F8": KeyF8, "F9": KeyF9, "F10": KeyF10, "F11": KeyF11, "F12": KeyF12,

	// Keypad.
	"Numpad0": KeyKP0, "Numpad1": KeyKP1, "Numpad2": KeyKP2, "Numpad3": KeyKP3,
	"Numpad4": KeyKP4, "Numpad5": KeyKP5, "Numpad6": KeyKP6, "Numpad7": KeyKP7,
	"Numpad8": KeyKP8, "Numpad9": KeyKP9,
	"NumpadDecimal":  KeyKPDecimal,
	"NumpadDivide":   KeyKPDivide,
	"NumpadMultiply": KeyKPMultiply,
	"NumpadSubtract": KeyKPSubtract,
	"NumpadAdd":      KeyKPAdd,
	"NumpadEnter":    KeyKPEnter,
	"NumpadEqual":    KeyKPEqual,

	// Modifiers.
	"ShiftLeft":    KeyLeftShift,
	"ControlLeft":  KeyLeftControl,
	"AltLeft":      KeyLeftAlt,
	"MetaLeft":     KeyLeftSuper,
	"ShiftRight":   KeyRightShift,
	"ControlRight": KeyRightControl,
	"AltRight":     KeyRightAlt,
	"MetaRight":    KeyRightSuper,
	"ContextMenu":  KeyMenu,
}
