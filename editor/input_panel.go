package editor

import (
	"fmt"
	"path/filepath"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/srcmodel"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/inputs/actions"
)

// The Input panel edits the game's //spliti:input binding table (game/input.go
// by convention): actions with their key/mouse/pad sources, axis actions with
// button pairs and pad axes. Every edit writes the source file AND rebinds the
// live actions.Map, so Play picks the change up without a rebuild.

// captureState is the "press any input..." modal's progress.
type captureState struct {
	active bool
	action string
	axis   bool // capturing for an axis action
	stage  int  // axis button pair: 0 = waiting for negative, 1 = positive
	neg    srcmodel.SourceRef
}

// loadInput locates and parses the //spliti:input function; optional.
func (st *state) loadInput() {
	path, ok := srcmodel.FindInputFile(filepath.Join(st.cfg.ProjectRoot, "game"))
	if !ok {
		st.input, st.inputErr = nil, nil
		return
	}
	st.input, st.inputErr = srcmodel.ParseInputFile(path)
}

// saveInput persists the binding table and rebinds the live map.
func (st *state) saveInput(c *app.Ctx, removed ...string) {
	if st.input == nil {
		return
	}
	if err := st.input.Save(); err != nil {
		st.status(err.Error())
		return
	}
	st.rebindLive(c, removed...)
	st.status(fmt.Sprintf("saved %s", filepath.Base(st.input.Path)))
}

// rebindLive replays the model into the live actions.Map (when the game
// installed one) so Play mode uses the edited bindings immediately.
func (st *state) rebindLive(c *app.Ctx, removed ...string) {
	mp := app.GetResource[actions.Map](c)
	if mp == nil || st.input == nil {
		return
	}
	for _, name := range removed {
		mp.Unbind(actions.Action(name))
		mp.UnbindAxis(actions.Action(name))
	}
	for _, ab := range st.input.Actions {
		if !ab.Editable {
			continue
		}
		mp.Unbind(actions.Action(ab.Name))
		for _, s := range ab.Sources {
			if src, ok := liveSource(s); ok {
				mp.Bind(actions.Action(ab.Name), src)
			}
		}
	}
	for _, ax := range st.input.Axes {
		if !ax.Editable {
			continue
		}
		mp.UnbindAxis(actions.Action(ax.Name))
		for _, s := range ax.Sources {
			if src, ok := liveAxisSource(s); ok {
				mp.BindAxis(actions.Action(ax.Name), src)
			}
		}
	}
}

func liveSource(s srcmodel.SourceRef) (actions.Source, bool) {
	switch s.Kind {
	case "key":
		if k, ok := inputs.KeyByName(s.Name); ok {
			return actions.Key(k), true
		}
	case "mouse":
		if b, ok := inputs.MouseButtonByName(s.Name); ok {
			return actions.Mouse(b), true
		}
	case "pad":
		if b, ok := inputs.GamepadButtonByName(s.Name); ok {
			return actions.Pad(b), true
		}
	}
	return actions.Source{}, false
}

func liveAxisSource(s srcmodel.AxisSourceRef) (actions.AxisSource, bool) {
	var src actions.AxisSource
	switch s.Kind {
	case "buttons":
		neg, ok1 := liveSource(s.Neg)
		pos, ok2 := liveSource(s.Pos)
		if !ok1 || !ok2 {
			return src, false
		}
		src = actions.ButtonAxis(neg, pos)
	case "pad":
		a, ok := inputs.GamepadAxisByName(s.Axis)
		if !ok {
			return src, false
		}
		src = actions.PadAxis(a)
	default:
		return src, false
	}
	if s.Inverted {
		src = src.Inverted()
	}
	return src, true
}

func sourceLabel(s srcmodel.SourceRef) string { return s.Name }

func axisSourceLabel(s srcmodel.AxisSourceRef) string {
	var l string
	switch s.Kind {
	case "buttons":
		l = s.Neg.Name + "/" + s.Pos.Name
	case "pad":
		l = s.Axis
	}
	if s.Inverted {
		l += " (inv)"
	}
	return l
}

// drawInput is the Input panel.
func drawInput(c *app.Ctx, st *state) {
	if !imgui.Begin("Input") {
		imgui.End()
		return
	}
	defer imgui.End()
	if st.inputErr != nil {
		imgui.TextColored(imgui.Vec4{X: 1, Y: 0.35, Z: 0.3, W: 1}, st.inputErr.Error())
		return
	}
	if st.input == nil {
		imgui.TextDisabled("no //spliti:input function found")
		imgui.TextDisabled("(declare BuildActions in game/input.go)")
		return
	}

	imgui.SeparatorText("Actions")
	var removeAction string
	for _, ab := range st.input.Actions {
		imgui.PushIDStr("act-" + ab.Name)
		imgui.TextUnformatted(ab.Name)
		if !ab.Editable {
			imgui.SameLine()
			imgui.TextDisabled("(hand-written, read-only)")
			imgui.PopID()
			continue
		}
		var removeSrc = -1
		for i, s := range ab.Sources {
			imgui.SameLine()
			imgui.PushIDInt(int32(i))
			if imgui.SmallButton(sourceLabel(s) + " x") {
				removeSrc = i
			}
			imgui.SetItemTooltip("Remove this binding")
			imgui.PopID()
		}
		imgui.SameLine()
		if imgui.SmallButton("+ bind") {
			st.capture = captureState{active: true, action: ab.Name}
		}
		imgui.SameLine()
		if imgui.SmallButton("delete") {
			removeAction = ab.Name
		}
		if removeSrc >= 0 {
			srcs := append(append([]srcmodel.SourceRef{}, ab.Sources[:removeSrc]...), ab.Sources[removeSrc+1:]...)
			if err := st.input.SetBinding(ab.Name, srcs); err != nil {
				st.status(err.Error())
			} else {
				st.saveInput(c)
			}
		}
		imgui.PopID()
	}
	if removeAction != "" {
		st.input.RemoveBinding(removeAction)
		st.saveInput(c, removeAction)
	}

	imgui.SeparatorText("Axes")
	var removeAxis string
	for _, ax := range st.input.Axes {
		imgui.PushIDStr("axis-" + ax.Name)
		imgui.TextUnformatted(ax.Name)
		if !ax.Editable {
			imgui.SameLine()
			imgui.TextDisabled("(hand-written, read-only)")
			imgui.PopID()
			continue
		}
		var removeSrc = -1
		var invertSrc = -1
		for i, s := range ax.Sources {
			imgui.SameLine()
			imgui.PushIDInt(int32(i))
			if imgui.SmallButton(axisSourceLabel(s) + " x") {
				removeSrc = i
			}
			if imgui.BeginPopupContextItem() {
				if imgui.MenuItemBool("Invert") {
					invertSrc = i
				}
				imgui.EndPopup()
			}
			imgui.PopID()
		}
		imgui.SameLine()
		if imgui.SmallButton("+ bind") {
			st.capture = captureState{active: true, action: ax.Name, axis: true}
		}
		imgui.SameLine()
		if imgui.SmallButton("delete") {
			removeAxis = ax.Name
		}
		if removeSrc >= 0 || invertSrc >= 0 {
			srcs := append([]srcmodel.AxisSourceRef{}, ax.Sources...)
			if invertSrc >= 0 {
				srcs[invertSrc].Inverted = !srcs[invertSrc].Inverted
			}
			if removeSrc >= 0 {
				srcs = append(srcs[:removeSrc], srcs[removeSrc+1:]...)
			}
			if err := st.input.SetAxisBinding(ax.Name, srcs); err != nil {
				st.status(err.Error())
			} else {
				st.saveInput(c)
			}
		}
		imgui.PopID()
	}
	if removeAxis != "" {
		st.input.RemoveAxisBinding(removeAxis)
		st.saveInput(c, removeAxis)
	}

	imgui.Separator()
	imgui.SetNextItemWidth(160)
	imgui.InputTextWithHint("##newaction", "new action name", &st.newActionBuf, 0, nil)
	imgui.SameLine()
	imgui.Checkbox("axis", &st.newActionAxis)
	imgui.SameLine()
	if imgui.Button("+ add") && st.newActionBuf != "" {
		name := st.newActionBuf
		var err error
		if st.newActionAxis {
			if st.input.Axis(name) == nil {
				err = st.input.SetAxisBinding(name, nil)
			}
		} else if st.input.Action(name) == nil {
			err = st.input.SetBinding(name, nil)
		}
		if err != nil {
			st.status(err.Error())
		} else {
			st.newActionBuf = ""
			st.saveInput(c)
		}
	}

	drawCaptureModal(c, st)
}

// drawCaptureModal waits for a physical input and appends it to the action
// being bound. Keys and gamepad input are captured live; mouse buttons are
// offered as explicit buttons (the modal itself swallows mouse clicks).
func drawCaptureModal(c *app.Ctx, st *state) {
	if !st.capture.active {
		return
	}
	imgui.OpenPopupStr("Bind input")
	if !imgui.BeginPopupModalV("Bind input", nil, imgui.WindowFlagsAlwaysAutoResize) {
		return
	}
	defer imgui.EndPopup()

	cap := &st.capture
	switch {
	case cap.axis && cap.stage == 1:
		imgui.TextUnformatted(fmt.Sprintf("Action %q: press the POSITIVE key (had %s)...", cap.action, cap.neg.Name))
	case cap.axis:
		imgui.TextUnformatted(fmt.Sprintf("Axis %q: press the NEGATIVE key,", cap.action))
		imgui.TextUnformatted("move a stick, or pull a trigger...")
	default:
		imgui.TextUnformatted(fmt.Sprintf("Action %q: press a key or pad button...", cap.action))
	}
	imgui.Separator()

	if src, ok := pollCapturedSource(c); ok {
		if src.Kind == "key" && src.Name == "KeyEscape" {
			cap.active = false
			imgui.CloseCurrentPopup()
			return
		}
		st.applyCapturedSource(c, src)
		if !st.capture.active {
			imgui.CloseCurrentPopup()
		}
		return
	}
	if cap.axis {
		if axis, ok := pollCapturedPadAxis(c); ok {
			st.appendAxisSource(c, srcmodel.AxisSourceRef{Kind: "pad", Axis: axis})
			cap.active = false
			imgui.CloseCurrentPopup()
			return
		}
	}

	if !cap.axis {
		imgui.TextDisabled("or a mouse button:")
		for _, mb := range []string{"MouseButtonLeft", "MouseButtonRight", "MouseButtonMiddle"} {
			imgui.SameLine()
			if imgui.SmallButton(mb) {
				st.applyCapturedSource(c, srcmodel.SourceRef{Kind: "mouse", Name: mb})
				imgui.CloseCurrentPopup()
				return
			}
		}
	}
	if imgui.Button("Cancel (Esc)") {
		cap.active = false
		imgui.CloseCurrentPopup()
	}
}

// pollCapturedSource reads this frame's key presses and pad buttons.
func pollCapturedSource(c *app.Ctx) (srcmodel.SourceRef, bool) {
	for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
		if ev.Rune == 0 && ev.Action == inputs.Press {
			if name := inputs.KeyName(ev.Key); name != "" {
				return srcmodel.SourceRef{Kind: "key", Name: name}, true
			}
		}
	}
	if pads := app.GetResource[inputs.Gamepads](c); pads != nil {
		for i := range pads.Pads {
			p := &pads.Pads[i]
			if !p.Connected {
				continue
			}
			for b := range p.Buttons {
				if p.Buttons[b] {
					if name := inputs.GamepadButtonName(inputs.GamepadButton(b)); name != "" {
						return srcmodel.SourceRef{Kind: "pad", Name: name}, true
					}
				}
			}
		}
	}
	return srcmodel.SourceRef{}, false
}

// pollCapturedPadAxis reports a strongly-deflected gamepad axis.
func pollCapturedPadAxis(c *app.Ctx) (string, bool) {
	pads := app.GetResource[inputs.Gamepads](c)
	if pads == nil {
		return "", false
	}
	for i := range pads.Pads {
		p := &pads.Pads[i]
		if !p.Connected {
			continue
		}
		for a := range p.Axes {
			v := p.Axes[a]
			if v > 0.6 || v < -0.6 {
				if name := inputs.GamepadAxisName(inputs.GamepadAxis(a)); name != "" {
					return name, true
				}
			}
		}
	}
	return "", false
}

// applyCapturedSource routes a captured button-like source: appended directly
// for button actions, staged into a neg/pos pair for axis actions.
func (st *state) applyCapturedSource(c *app.Ctx, src srcmodel.SourceRef) {
	cap := &st.capture
	if !cap.axis {
		ab := st.input.Action(cap.action)
		if ab != nil {
			if err := st.input.SetBinding(cap.action, append(append([]srcmodel.SourceRef{}, ab.Sources...), src)); err != nil {
				st.status(err.Error())
			} else {
				st.saveInput(c)
			}
		}
		cap.active = false
		return
	}
	if cap.stage == 0 {
		cap.neg, cap.stage = src, 1
		return
	}
	st.appendAxisSource(c, srcmodel.AxisSourceRef{Kind: "buttons", Neg: cap.neg, Pos: src})
	cap.active = false
}

func (st *state) appendAxisSource(c *app.Ctx, src srcmodel.AxisSourceRef) {
	ax := st.input.Axis(st.capture.action)
	if ax == nil {
		return
	}
	if err := st.input.SetAxisBinding(ax.Name, append(append([]srcmodel.AxisSourceRef{}, ax.Sources...), src)); err != nil {
		st.status(err.Error())
		return
	}
	st.saveInput(c)
}
