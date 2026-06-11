package inputs

// GamepadID identifies a gamepad slot. IDs are stable while a pad stays
// connected; a pad reconnected later may get a different ID. Valid IDs are
// 0..MaxGamepads-1 (mirrors GLFW's sixteen joystick slots; browsers expose
// at most four).
type GamepadID int

// MaxGamepads is the number of gamepad slots tracked in Gamepads.
const MaxGamepads = 16

// GamepadButton is a digital gamepad button in the standard dual-stick layout.
// Values mirror GLFW's GLFW_GAMEPAD_BUTTON_* so the native backend converts
// with a plain cast; the browser's "standard" mapping is reordered to match.
type GamepadButton int

const (
	GamepadA           GamepadButton = 0 // bottom face button (Cross on PlayStation)
	GamepadB           GamepadButton = 1 // right face button (Circle)
	GamepadX           GamepadButton = 2 // left face button (Square)
	GamepadY           GamepadButton = 3 // top face button (Triangle)
	GamepadLeftBumper  GamepadButton = 4
	GamepadRightBumper GamepadButton = 5
	GamepadBack        GamepadButton = 6 // Select / Share / View
	GamepadStart       GamepadButton = 7 // Options / Menu
	GamepadGuide       GamepadButton = 8 // Xbox / PS logo button
	GamepadLeftThumb   GamepadButton = 9 // pressing the left stick
	GamepadRightThumb  GamepadButton = 10
	GamepadDpadUp      GamepadButton = 11
	GamepadDpadRight   GamepadButton = 12
	GamepadDpadDown    GamepadButton = 13
	GamepadDpadLeft    GamepadButton = 14

	// GamepadButtonCount is the number of tracked buttons.
	GamepadButtonCount = 15
)

// GamepadAxis is an analog gamepad axis. Stick axes are in [-1, 1] with +X
// right and +Y down (GLFW's convention, which the browser shares). Trigger
// axes are canonicalized to [0, 1] — 0 released, 1 fully pulled — on every
// platform (the native backend remaps GLFW's [-1, 1] triggers).
type GamepadAxis int

const (
	AxisLeftX        GamepadAxis = 0
	AxisLeftY        GamepadAxis = 1
	AxisRightX       GamepadAxis = 2
	AxisRightY       GamepadAxis = 3
	AxisLeftTrigger  GamepadAxis = 4
	AxisRightTrigger GamepadAxis = 5

	// GamepadAxisCount is the number of tracked axes.
	GamepadAxisCount = 6
)

// GamepadState is one pad's current sample: connection, identity, and the raw
// (no dead-zone) button/axis values. Copy it freely; it has no references.
type GamepadState struct {
	Connected bool
	Name      string
	Buttons   [GamepadButtonCount]bool
	Axes      [GamepadAxisCount]float32
}

// Gamepads is the polled state of every gamepad slot, maintained as an app
// resource by the gamepad plugin (plugin/gamepad) and refreshed once per frame
// before PreUpdate. Read it for held buttons and axis values; read the
// Gamepad*Event events for edges.
type Gamepads struct {
	Pads [MaxGamepads]GamepadState
}

// State returns the state of the pad in the given slot (the zero GamepadState,
// Connected=false, when id is out of range).
func (g *Gamepads) State(id GamepadID) GamepadState {
	if id < 0 || int(id) >= len(g.Pads) {
		return GamepadState{}
	}
	return g.Pads[id]
}

// First returns the lowest-numbered connected pad, for the common
// single-player case. ok is false when no pad is connected.
func (g *Gamepads) First() (id GamepadID, ok bool) {
	for i := range g.Pads {
		if g.Pads[i].Connected {
			return GamepadID(i), true
		}
	}
	return 0, false
}

// GamepadConnectionEvent is emitted when a pad connects (Connected=true,
// Name set) or disconnects.
type GamepadConnectionEvent struct {
	ID        GamepadID
	Connected bool
	Name      string
}

// GamepadButtonEvent is emitted on a button press or release edge, derived by
// the gamepad plugin from frame-to-frame state diffs (Action is Press or
// Release, never Repeat).
type GamepadButtonEvent struct {
	ID     GamepadID
	Button GamepadButton
	Action Action
}
