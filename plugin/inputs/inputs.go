// Package inputs defines spliti's canonical, backend-agnostic input events and
// key/button/modifier enums. The GPU render backends (plugin/webgpu and
// plugin/render3d) map their platform input — GLFW on native desktop, the DOM on
// js/wasm — into these neutral types, so game code reads one set of events and
// switches on one set of constants regardless of where it runs.
//
// The package deliberately imports nothing platform-specific (no glfw, no tcell,
// no syscall/js) so it compiles on every target, including GOOS=js GOARCH=wasm.
//
// The constant values mirror GLFW's so the native backends can convert with a
// plain cast and existing game code migrates by a one-to-one rename
// (glfw.KeyEscape → inputs.KeyEscape). They are nonetheless the canonical values
// — code should reference these constants, not assume the GLFW origin.
package inputs

// Action is the kind of a key or mouse-button transition.
type Action int

const (
	Release Action = 0
	Press   Action = 1
	Repeat  Action = 2
)

// Mod is a bitmask of modifier keys held during an event.
type Mod int

const (
	ModShift    Mod = 0x0001
	ModControl  Mod = 0x0002
	ModAlt      Mod = 0x0004
	ModSuper    Mod = 0x0008
	ModCapsLock Mod = 0x0010
	ModNumLock  Mod = 0x0020
)

// MouseButton identifies a mouse button. Left/Right/Middle are the common
// aliases; the numbered buttons cover extra buttons.
type MouseButton int

const (
	MouseButton1 MouseButton = 0
	MouseButton2 MouseButton = 1
	MouseButton3 MouseButton = 2
	MouseButton4 MouseButton = 3
	MouseButton5 MouseButton = 4
	MouseButton6 MouseButton = 5
	MouseButton7 MouseButton = 6
	MouseButton8 MouseButton = 7

	MouseButtonLeft   = MouseButton1
	MouseButtonRight  = MouseButton2
	MouseButtonMiddle = MouseButton3
)

// Key is a physical keyboard key, layout-independent (mirrors GLFW key codes).
// Typed text is delivered separately as KeyEvent.Rune.
type Key int

// KeyUnknown is reported when a platform key has no canonical mapping.
const KeyUnknown Key = -1

// Printable-key codes coincide with ASCII for the unshifted character.
const (
	KeySpace        Key = 32
	KeyApostrophe   Key = 39 // '
	KeyComma        Key = 44 // ,
	KeyMinus        Key = 45 // -
	KeyPeriod       Key = 46 // .
	KeySlash        Key = 47 // /
	Key0            Key = 48
	Key1            Key = 49
	Key2            Key = 50
	Key3            Key = 51
	Key4            Key = 52
	Key5            Key = 53
	Key6            Key = 54
	Key7            Key = 55
	Key8            Key = 56
	Key9            Key = 57
	KeySemicolon    Key = 59 // ;
	KeyEqual        Key = 61 // =
	KeyA            Key = 65
	KeyB            Key = 66
	KeyC            Key = 67
	KeyD            Key = 68
	KeyE            Key = 69
	KeyF            Key = 70
	KeyG            Key = 71
	KeyH            Key = 72
	KeyI            Key = 73
	KeyJ            Key = 74
	KeyK            Key = 75
	KeyL            Key = 76
	KeyM            Key = 77
	KeyN            Key = 78
	KeyO            Key = 79
	KeyP            Key = 80
	KeyQ            Key = 81
	KeyR            Key = 82
	KeyS            Key = 83
	KeyT            Key = 84
	KeyU            Key = 85
	KeyV            Key = 86
	KeyW            Key = 87
	KeyX            Key = 88
	KeyY            Key = 89
	KeyZ            Key = 90
	KeyLeftBracket  Key = 91 // [
	KeyBackslash    Key = 92 // \
	KeyRightBracket Key = 93 // ]
	KeyGraveAccent  Key = 96 // `
)

// Function and navigation keys.
const (
	KeyEscape      Key = 256
	KeyEnter       Key = 257
	KeyTab         Key = 258
	KeyBackspace   Key = 259
	KeyInsert      Key = 260
	KeyDelete      Key = 261
	KeyRight       Key = 262
	KeyLeft        Key = 263
	KeyDown        Key = 264
	KeyUp          Key = 265
	KeyPageUp      Key = 266
	KeyPageDown    Key = 267
	KeyHome        Key = 268
	KeyEnd         Key = 269
	KeyCapsLock    Key = 280
	KeyScrollLock  Key = 281
	KeyNumLock     Key = 282
	KeyPrintScreen Key = 283
	KeyPause       Key = 284
	KeyF1          Key = 290
	KeyF2          Key = 291
	KeyF3          Key = 292
	KeyF4          Key = 293
	KeyF5          Key = 294
	KeyF6          Key = 295
	KeyF7          Key = 296
	KeyF8          Key = 297
	KeyF9          Key = 298
	KeyF10         Key = 299
	KeyF11         Key = 300
	KeyF12         Key = 301
)

// Keypad keys.
const (
	KeyKP0        Key = 320
	KeyKP1        Key = 321
	KeyKP2        Key = 322
	KeyKP3        Key = 323
	KeyKP4        Key = 324
	KeyKP5        Key = 325
	KeyKP6        Key = 326
	KeyKP7        Key = 327
	KeyKP8        Key = 328
	KeyKP9        Key = 329
	KeyKPDecimal  Key = 330
	KeyKPDivide   Key = 331
	KeyKPMultiply Key = 332
	KeyKPSubtract Key = 333
	KeyKPAdd      Key = 334
	KeyKPEnter    Key = 335
	KeyKPEqual    Key = 336
)

// Modifier keys (the physical left/right keys).
const (
	KeyLeftShift    Key = 340
	KeyLeftControl  Key = 341
	KeyLeftAlt      Key = 342
	KeyLeftSuper    Key = 343
	KeyRightShift   Key = 344
	KeyRightControl Key = 345
	KeyRightAlt     Key = 346
	KeyRightSuper   Key = 347
	KeyMenu         Key = 348
)

// KeyEvent is emitted for keyboard activity. Two sources feed it:
//
//   - Key presses/releases/repeats: Key, Action, and Mods are set; Rune is 0.
//   - Typed text: Rune is set and Action is Press; Key is 0.
type KeyEvent struct {
	Key    Key
	Rune   rune
	Action Action
	Mods   Mod
}

// MouseButtonEvent is emitted on a mouse button press or release. X,Y is the
// cursor position in window (screen) coordinates at the time of the event — not
// framebuffer pixels and not world coordinates.
type MouseButtonEvent struct {
	Button MouseButton
	Action Action
	Mods   Mod
	X, Y   float64
}

// MouseMoveEvent is emitted when the cursor moves. X,Y are window (screen)
// coordinates.
type MouseMoveEvent struct {
	X, Y float64
}

// ResizeEvent is emitted when the drawable surface changes size, in pixels.
type ResizeEvent struct{ Width, Height int }

// CloseEvent is emitted once when the user requests the window/tab close. The
// backends also call App.Stop() on close, so games can rely on a clean shutdown
// without handling this; read it if you want to react first.
type CloseEvent struct{}
