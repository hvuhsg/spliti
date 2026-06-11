// Package actions maps physical input (keys, mouse buttons, gamepad buttons
// and axes) to named logical actions, so game code asks "is jump held?"
// instead of switching on key codes, and players can rebind controls by
// editing one table.
//
// Bind sources to actions on a Map, install it with Plugin, then poll it from
// any system:
//
//	m := actions.NewMap()
//	m.Bind("jump", actions.Key(inputs.KeySpace), actions.Pad(inputs.GamepadA))
//	m.Bind("fire", actions.Mouse(inputs.MouseButtonLeft))
//	m.BindAxis("move-x",
//		actions.ButtonAxis(actions.Key(inputs.KeyA), actions.Key(inputs.KeyD)),
//		actions.PadAxis(inputs.AxisLeftX))
//	app.New().AddPlugins(gpuBackend, actions.Plugin{Map: m}, game)
//
//	// in a system:
//	in := actions.Get(c)
//	if in.JustPressed("jump") { ... }
//	vx := in.Axis("move-x") // -1..1
//
// The Map updates once per frame in PreUpdate from the backend-agnostic
// plugin/inputs events that the GPU backends (plugin/render3d, plugin/webgpu)
// publish in First, and from the Gamepads resource maintained by
// plugin/gamepad. Held/JustPressed/JustReleased are stable for the whole
// frame, including every FixedUpdate iteration within it. The terminal
// backend can't deliver key releases, so held-state semantics need a GPU
// backend.
//
// Bind and BindAxis may be called at any time (e.g. from a settings menu);
// changes take effect at the next frame's update.
package actions

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/schedule"
)

// Action names a logical input ("jump", "fire", "move-x"). Plain strings so
// games can define their own constants.
type Action string

type sourceKind int

const (
	srcKey sourceKind = iota
	srcMouse
	srcPad
)

// Source is one physical button-like input that can drive an action. Build
// one with Key, Mouse, or Pad.
type Source struct {
	kind  sourceKind
	key   inputs.Key
	mouse inputs.MouseButton
	pad   inputs.GamepadButton
}

// Key binds a keyboard key.
func Key(k inputs.Key) Source { return Source{kind: srcKey, key: k} }

// Mouse binds a mouse button.
func Mouse(b inputs.MouseButton) Source { return Source{kind: srcMouse, mouse: b} }

// Pad binds a gamepad button (any connected pad activates it).
func Pad(b inputs.GamepadButton) Source { return Source{kind: srcPad, pad: b} }

type axisKind int

const (
	axButtons axisKind = iota
	axPad
)

// AxisSource is one physical input that can drive a -1..1 axis action. Build
// one with ButtonAxis or PadAxis.
type AxisSource struct {
	kind     axisKind
	neg, pos Source
	axis     inputs.GamepadAxis
	scale    float32
}

// ButtonAxis derives an axis from a button pair: -1 while neg is held, +1
// while pos is held, 0 when neither or both are held.
func ButtonAxis(neg, pos Source) AxisSource {
	return AxisSource{kind: axButtons, neg: neg, pos: pos, scale: 1}
}

// PadAxis reads a gamepad analog axis (the first connected pad whose value
// passes the dead zone). Stick axes report -1..1 with +Y down; pass the
// result through Inverted for +Y-up movement.
func PadAxis(a inputs.GamepadAxis) AxisSource {
	return AxisSource{kind: axPad, axis: a, scale: 1}
}

// Inverted returns the source with its sign flipped.
func (s AxisSource) Inverted() AxisSource {
	s.scale = -s.scale
	return s
}

// Map holds the action bindings and the per-frame action state. Create with
// NewMap, install with Plugin, and read with Get. Not safe for use outside
// the app's systems (the engine is single-threaded; that's fine).
type Map struct {
	bindings map[Action][]Source
	axes     map[Action][]AxisSource
	deadZone float32

	keysDown  map[inputs.Key]bool
	mouseDown map[inputs.MouseButton]bool

	held     map[Action]bool
	prevHeld map[Action]bool

	pads *inputs.Gamepads // captured each update; nil when no gamepad plugin
}

// NewMap returns an empty Map with the default stick dead zone (0.15).
func NewMap() *Map {
	return &Map{
		bindings:  map[Action][]Source{},
		axes:      map[Action][]AxisSource{},
		deadZone:  0.15,
		keysDown:  map[inputs.Key]bool{},
		mouseDown: map[inputs.MouseButton]bool{},
		held:      map[Action]bool{},
		prevHeld:  map[Action]bool{},
	}
}

// SetDeadZone sets the radius (0..1) below which pad stick/trigger values
// read as zero in Axis and don't count as held.
func (m *Map) SetDeadZone(dz float32) { m.deadZone = dz }

// Bind adds button-like sources to an action, keeping existing ones.
func (m *Map) Bind(a Action, sources ...Source) {
	m.bindings[a] = append(m.bindings[a], sources...)
}

// BindAxis adds axis sources to an axis action, keeping existing ones.
func (m *Map) BindAxis(a Action, sources ...AxisSource) {
	m.axes[a] = append(m.axes[a], sources...)
}

// Unbind removes every button-like binding of the action.
func (m *Map) Unbind(a Action) { delete(m.bindings, a) }

// UnbindAxis removes every axis binding of the action.
func (m *Map) UnbindAxis(a Action) { delete(m.axes, a) }

// Reset drops all held key/mouse state (action state recomputes next frame).
// Call it when the window loses focus if missed key-releases are a concern.
func (m *Map) Reset() {
	clear(m.keysDown)
	clear(m.mouseDown)
}

// Held reports whether any source of the action is currently down.
func (m *Map) Held(a Action) bool { return m.held[a] }

// JustPressed reports whether the action went from up to down this frame.
func (m *Map) JustPressed(a Action) bool { return m.held[a] && !m.prevHeld[a] }

// JustReleased reports whether the action went from down to up this frame.
func (m *Map) JustReleased(a Action) bool { return !m.held[a] && m.prevHeld[a] }

// Axis returns the action's combined axis value, clamped to [-1, 1]. Button
// pairs contribute ±1; pad axes contribute their dead-zoned analog value.
func (m *Map) Axis(a Action) float32 {
	var v float32
	for _, s := range m.axes[a] {
		switch s.kind {
		case axButtons:
			if m.sourceDown(s.neg) {
				v -= s.scale
			}
			if m.sourceDown(s.pos) {
				v += s.scale
			}
		case axPad:
			v += m.padAxis(s.axis) * s.scale
		}
	}
	return min(1, max(-1, v))
}

// Get returns the installed Map, or nil if Plugin was not added.
func Get(c *app.Ctx) *Map { return app.GetResource[Map](c) }

// Plugin installs Map as a resource and a PreUpdate system (label
// "__spliti_actions_update") that refreshes action state from the frame's
// input events and gamepad state. A nil Map installs an empty NewMap that
// game code can Bind to later via Get.
type Plugin struct {
	Map *Map
}

// Build implements app.Plugin.
func (p Plugin) Build(a *app.App) {
	m := p.Map
	if m == nil {
		m = NewMap()
	}
	app.InsertResource(a, m)
	a.AddSystems(schedule.PreUpdate, app.System(m.update).Label("__spliti_actions_update"))
}

func (m *Map) update(c *app.Ctx) {
	// Track raw key/button held state from this frame's events. Rune-only
	// events are typed text, not physical key transitions.
	for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
		if ev.Rune == 0 && ev.Action != inputs.Repeat {
			m.keysDown[ev.Key] = ev.Action == inputs.Press
		}
	}
	for _, ev := range app.ReadEvents[inputs.MouseButtonEvent](c) {
		m.mouseDown[ev.Button] = ev.Action == inputs.Press
	}
	m.pads = app.GetResource[inputs.Gamepads](c)

	// Recompute per-action held state; edges fall out of the prev/cur pair.
	m.prevHeld, m.held = m.held, m.prevHeld
	clear(m.held)
	for a, sources := range m.bindings {
		for _, s := range sources {
			if m.sourceDown(s) {
				m.held[a] = true
				break
			}
		}
	}
}

func (m *Map) sourceDown(s Source) bool {
	switch s.kind {
	case srcKey:
		return m.keysDown[s.key]
	case srcMouse:
		return m.mouseDown[s.mouse]
	case srcPad:
		if m.pads == nil {
			return false
		}
		for i := range m.pads.Pads {
			p := &m.pads.Pads[i]
			if p.Connected && p.Buttons[s.pad] {
				return true
			}
		}
	}
	return false
}

// padAxis returns the largest-magnitude dead-zoned value of the axis across
// connected pads, so an idle second controller doesn't mute the active one.
func (m *Map) padAxis(a inputs.GamepadAxis) float32 {
	if m.pads == nil {
		return 0
	}
	var v float32
	for i := range m.pads.Pads {
		p := &m.pads.Pads[i]
		if !p.Connected {
			continue
		}
		x := p.Axes[a]
		if abs(x) > m.deadZone && abs(x) > abs(v) {
			v = x
		}
	}
	return v
}

func abs(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
