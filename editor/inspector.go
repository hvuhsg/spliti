package editor

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/registry"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/scene"
	"github.com/mlange-42/arche/ecs"
)

// drawInspector shows the selected entity's components and edits them live.
// Edits write straight into world storage while a widget is active; when the
// gesture ends (IsItemDeactivatedAfterEdit) the whole change is committed as
// one undo command, which also writes the scene source: Transform3D goes into
// the spawn line's transform chain, every other component into a scene.Set
// override line.
func drawInspector(c *app.Ctx, st *state) {
	imgui.Begin("Inspector")
	defer imgui.End()

	e, ok := st.primary()
	if !ok || !c.World().Alive(e) {
		imgui.TextDisabled("nothing selected")
		return
	}
	inst := instanceName(c, e)
	if inst != "" {
		imgui.TextUnformatted(inst)
	} else {
		imgui.TextDisabled("(unnamed entity - edits are live-only)")
	}
	if n := len(st.sel); n > 1 {
		imgui.TextDisabled(fmt.Sprintf("%d selected - edits apply to all (Transform: primary only)", n))
	}
	imgui.Separator()

	var removeReq *registry.TypeInfo
	for _, ti := range st.reg.On(c.World(), e) {
		imgui.PushIDStr(ti.Name)
		open := imgui.CollapsingHeaderBoolPtrV(ti.Name, nil, imgui.TreeNodeFlagsDefaultOpen)
		if imgui.BeginPopupContextItem() {
			if imgui.MenuItemBool("Remove component") {
				removeReq = ti
			}
			imgui.EndPopup()
		}
		if open {
			drawComponent(c, st, e, inst, ti)
		}
		imgui.PopID()
	}
	if removeReq != nil {
		st.push(c, &cmdRemoveComponent{instance: inst, entity: e, typeName: removeReq.Name})
	}

	imgui.Separator()
	drawAddComponent(c, st, e, inst)
}

// fieldCtx carries what a field widget needs to commit its edit: the component
// identity plus the pre-draw value that becomes the undo "before".
type fieldCtx struct {
	c    *app.Ctx
	st   *state
	e    ecs.Entity
	inst string
	ti   *registry.TypeInfo
	pre  any    // component value before any widget wrote this frame
	key  string // gesture key: entity + component
}

// drawComponent renders one component's fields.
func drawComponent(c *app.Ctx, st *state, e ecs.Entity, inst string, ti *registry.TypeInfo) {
	w := c.World()
	// Pre-draw snapshot: the gesture's "before" value must be read before any
	// widget writes into live storage this frame.
	fc := fieldCtx{
		c: c, st: st, e: e, inst: inst, ti: ti,
		pre: ti.Value(w, e),
		key: fmt.Sprintf("%v/%s", e, ti.Name),
	}
	comp := ti.Get(w, e)
	if ti.Name == "Collider3D" && st.layers != nil && len(st.layers.Names) > 0 {
		drawColliderFields(fc, comp)
		return
	}
	if ti.Name == "Camera" {
		drawCameraFields(fc, comp)
		return
	}
	if ti.Name == "InstanceColor" {
		drawInstanceColorField(fc, comp)
		return
	}
	for _, f := range ti.Fields {
		if keys, ok := assetRefKeys(c, ti.Name, f.Name); ok {
			drawAssetRefField(fc, f.Name, f.Value(comp), keys)
			continue
		}
		drawField(fc, f.Name, f.Kind, f.Value(comp))
	}
}

// assetRefKeys reports whether a component field holds a registered-asset key
// (and returns the known keys for its dropdown). These are the string fields
// that reference assets by ref, so the inspector can offer a pick list rather
// than a free-text box that silently accepts unknown names.
func assetRefKeys(c *app.Ctx, comp, field string) ([]string, bool) {
	switch {
	case comp == "MeshRenderer" && field == "Mesh":
		if r := app.GetResource[render3d.MeshRegistry](c); r != nil {
			return r.Keys(), true
		}
	case comp == "MaterialRef" && field == "Material":
		if r := app.GetResource[render3d.MaterialRegistry](c); r != nil {
			return r.Keys(), true
		}
	}
	return nil, false
}

// drawAssetRefField edits a string asset key as a combo of registered keys.
// "(none)" clears the ref (the registry falls back to its default); an
// unregistered current value stays visible and selected rather than vanishing.
func drawAssetRefField(fc fieldCtx, name string, v reflect.Value, keys []string) {
	cur := v.String()
	label := cur
	if label == "" {
		label = "(none)"
	}
	if !imgui.BeginCombo(name, label) {
		return
	}
	defer imgui.EndCombo()
	if imgui.SelectableBoolV("(none)", cur == "", 0, imgui.Vec2{}) && cur != "" {
		v.SetString("")
		fc.commitNow()
	}
	known := false
	for _, k := range keys {
		sel := k == cur
		known = known || sel
		if imgui.SelectableBoolV(k, sel, 0, imgui.Vec2{}) && !sel {
			v.SetString(k)
			fc.commitNow()
		}
	}
	if cur != "" && !known {
		imgui.SelectableBoolV(cur+"  (unregistered)", true, 0, imgui.Vec2{})
	}
}

// gesture tracks the drag/scrub lifecycle of the most recent widget item:
// the "before" value is pinned on activation, and one undo command is pushed
// when the gesture ends with a change. Call it right after every leaf widget.
func (fc fieldCtx) gesture() {
	st := fc.st
	if imgui.IsItemActivated() {
		if _, ok := st.editBefore[fc.key]; !ok {
			st.editBefore[fc.key] = fc.pre
		}
	}
	if imgui.IsItemDeactivatedAfterEdit() {
		before, ok := st.editBefore[fc.key]
		if !ok {
			before = fc.pre
		}
		delete(st.editBefore, fc.key)
		st.commitComponentEdit(fc.c, fc.e, fc.inst, fc.ti, before)
	} else if imgui.IsItemDeactivated() {
		delete(st.editBefore, fc.key)
	}
}

// commitNow pushes an immediate (single-click) edit: the value was already
// written into live storage this frame, and the pre-draw snapshot is the
// "before". Used by buttons and pickers, which have no drag gesture.
func (fc fieldCtx) commitNow() {
	fc.st.commitComponentEdit(fc.c, fc.e, fc.inst, fc.ti, fc.pre)
}

// commitComponentEdit pushes the finished gesture as one undo command. With a
// multi-selection, the primary's new value is copied onto every other selected
// entity that shares the component, batched into a single undo step — except
// Transform3D, whose absolute copy would collapse the selection onto one spot
// (multi-transform is the viewport gizmo's job, applied as a world-space delta).
func (st *state) commitComponentEdit(c *app.Ctx, e ecs.Entity, inst string, ti *registry.TypeInfo, before any) {
	after := ti.Value(c.World(), e)
	if reflect.DeepEqual(before, after) {
		return
	}
	if ti.Name == "Transform3D" {
		st.push(c, &cmdTransform{
			instance: inst, entity: e,
			before: before.(render3d.Transform3D),
			after:  after.(render3d.Transform3D),
		})
		return
	}
	cmds := []editorCmd{st.newSetComponent(inst, e, ti, before, after)}
	if len(st.sel) > 1 {
		w := c.World()
		for _, other := range st.sel {
			if other == e || !w.Alive(other) || !ti.Has(w, other) {
				continue
			}
			ob := ti.Value(w, other)
			if reflect.DeepEqual(ob, after) {
				continue
			}
			cmds = append(cmds, st.newSetComponent(instanceName(c, other), other, ti, ob, after))
		}
	}
	st.push(c, batch("edit "+ti.Name, cmds))
}

// drawAddComponent is the "+ Add Component" button with a filterable popup of
// registered types not present on the entity.
func drawAddComponent(c *app.Ctx, st *state, e ecs.Entity, inst string) {
	if imgui.ButtonV("+ Add Component", imgui.Vec2{X: -1}) {
		st.addCompFilter = ""
		imgui.OpenPopupStr("##addcomp")
	}
	if !imgui.BeginPopup("##addcomp") {
		return
	}
	imgui.SetNextItemWidth(220)
	imgui.InputTextWithHint("##filter", "filter...", &st.addCompFilter, 0, nil)
	filter := strings.ToLower(st.addCompFilter)
	for _, ti := range st.reg.Types() {
		if ti.Hidden || ti.Has(c.World(), e) {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(ti.Name), filter) {
			continue
		}
		if imgui.SelectableBool(ti.Name) {
			st.push(c, &cmdAddComponent{instance: inst, entity: e, typeName: ti.Name})
			imgui.CloseCurrentPopup()
		}
	}
	imgui.EndPopup()
}

// drawField renders the widget for one field. Leaf values write straight into
// live component storage and commit through the gesture tracker; structural
// edits (slice add/remove, entity picks) commit immediately.
func drawField(fc fieldCtx, name string, kind registry.FieldKind, v reflect.Value) {
	switch kind {
	case registry.KindFloat32, registry.KindFloat64:
		f32 := float32(v.Float())
		if imgui.DragFloatV(name, &f32, dragSpeed, 0, 0, "%.3f", 0) {
			v.SetFloat(float64(f32))
		}
		fc.gesture()
	case registry.KindInt:
		iv := int32(intOf(v))
		if imgui.DragInt(name, &iv) {
			setInt(v, int64(iv))
		}
		fc.gesture()
	case registry.KindBool:
		b := v.Bool()
		if imgui.Checkbox(name, &b) {
			v.SetBool(b)
		}
		fc.gesture()
	case registry.KindString:
		s := v.String()
		if imgui.InputTextWithHint(name, "", &s, 0, nil) {
			v.SetString(s)
		}
		fc.gesture()
	case registry.KindVec2:
		vec := v.Interface().(m.Vec2)
		arr := [2]float32{vec.X, vec.Y}
		if imgui.DragFloat2(name, &arr) {
			v.Set(reflect.ValueOf(m.Vec2{X: arr[0], Y: arr[1]}))
		}
		fc.gesture()
	case registry.KindVec3:
		vec := v.Interface().(m.Vec3)
		arr := [3]float32{vec.X, vec.Y, vec.Z}
		if imgui.DragFloat3V(name, &arr, dragSpeed, 0, 0, "%.3f", 0) {
			v.Set(reflect.ValueOf(m.Vec3{X: arr[0], Y: arr[1], Z: arr[2]}))
		}
		fc.gesture()
	case registry.KindVec4:
		vec := v.Interface().(m.Vec4)
		arr := [4]float32{vec.X, vec.Y, vec.Z, vec.W}
		if imgui.DragFloat4(name, &arr) {
			v.Set(reflect.ValueOf(m.Vec4{X: arr[0], Y: arr[1], Z: arr[2], W: arr[3]}))
		}
		fc.gesture()
	case registry.KindColor:
		drawColorField(fc, name, v)
	case registry.KindQuat:
		drawQuatField(fc, name, v)
	case registry.KindEntity:
		drawEntityField(fc, name, v)
	case registry.KindSlice:
		drawSliceField(fc, name, v)
	default:
		imgui.TextDisabled(fmt.Sprintf("%s: (not editable)", name))
	}
}

// colorFlags configures the color widgets: float (0..1) display with an alpha
// bar, and HDR so emissive/light colors can exceed 1 without being clamped.
const colorFlags = imgui.ColorEditFlagsFloat | imgui.ColorEditFlagsAlphaBar | imgui.ColorEditFlagsHDR

// drawColorField edits a Vec3/Vec4 color field with a swatch that opens a full
// color picker (click the swatch). Vec3 fields keep RGB only; Vec4 carry alpha.
func drawColorField(fc fieldCtx, name string, v reflect.Value) {
	switch vec := v.Interface().(type) {
	case m.Vec3:
		arr := [3]float32{vec.X, vec.Y, vec.Z}
		if imgui.ColorEdit3V(name, &arr, colorFlags) {
			v.Set(reflect.ValueOf(m.Vec3{X: arr[0], Y: arr[1], Z: arr[2]}))
		}
	case m.Vec4:
		arr := [4]float32{vec.X, vec.Y, vec.Z, vec.W}
		if imgui.ColorEdit4V(name, &arr, colorFlags) {
			v.Set(reflect.ValueOf(m.Vec4{X: arr[0], Y: arr[1], Z: arr[2], W: arr[3]}))
		}
	}
	fc.gesture()
}

// drawInstanceColorField edits the InstanceColor component (R,G,B,A floats) as a
// single RGBA color picker rather than four separate drag inputs.
func drawInstanceColorField(fc fieldCtx, comp reflect.Value) {
	r := comp.FieldByName("R")
	g := comp.FieldByName("G")
	b := comp.FieldByName("B")
	a := comp.FieldByName("A")
	arr := [4]float32{
		float32(r.Float()), float32(g.Float()),
		float32(b.Float()), float32(a.Float()),
	}
	if imgui.ColorEdit4V("Color", &arr, colorFlags) {
		r.SetFloat(float64(arr[0]))
		g.SetFloat(float64(arr[1]))
		b.SetFloat(float64(arr[2]))
		a.SetFloat(float64(arr[3]))
	}
	fc.gesture()
}

// drawQuatField edits a quaternion as Euler degrees. The displayed angles come
// from an activation-scoped cache: while the user is dragging, the cache —
// not the lossy quat→Euler conversion of the value just written — feeds the
// widget, so the numbers never feed back or jump mid-drag.
func drawQuatField(fc fieldCtx, name string, v reflect.Value) {
	key := fmt.Sprintf("%s/%s", fc.key, name)
	q := v.Interface().(m.Quat)

	arr, cached := fc.st.eulerCache[key]
	if !cached {
		p, y, r := q.ToEuler()
		arr = [3]float32{degOf(p), degOf(y), degOf(r)}
	}
	changed := imgui.DragFloat3V(name+" (deg)", &arr, 0.5, 0, 0, "%.2f", 0)
	if imgui.IsItemActive() {
		fc.st.eulerCache[key] = arr
	} else {
		delete(fc.st.eulerCache, key)
	}
	if changed {
		v.Set(reflect.ValueOf(m.FromEuler(
			m.DegToRad(arr[0]), m.DegToRad(arr[1]), m.DegToRad(arr[2]))))
	}
	fc.gesture()
}

// drawEntityField is a combo of named scene instances (entity-ref fields can
// only persist references to named instances; "(none)" is the zero handle).
func drawEntityField(fc fieldCtx, name string, v reflect.Value) {
	cur := v.Interface().(ecs.Entity)
	label := "(none)"
	if n := instanceName(fc.c, cur); n != "" {
		label = n
	} else if cur != (ecs.Entity{}) {
		label = fmt.Sprintf("entity %v (unnamed)", cur)
	}
	if !imgui.BeginCombo(name, label) {
		return
	}
	if imgui.SelectableBool("(none)") && cur != (ecs.Entity{}) {
		v.Set(reflect.ValueOf(ecs.Entity{}))
		fc.commitNow()
	}
	for _, it := range namedInstances(fc.c) {
		sel := it.e == cur
		if imgui.SelectableBoolV(it.name, sel, 0, imgui.Vec2{}) && !sel {
			v.Set(reflect.ValueOf(it.e))
			fc.commitNow()
		}
	}
	imgui.EndCombo()
}

// drawSliceField renders a slice as a tree of per-element widgets with
// add/remove buttons. Element value edits ride the normal gesture commit;
// length changes commit immediately.
func drawSliceField(fc fieldCtx, name string, v reflect.Value) {
	// "###" keeps the tree's ID stable while the label shows the live length.
	if !imgui.TreeNodeExStr(fmt.Sprintf("%s [%d]###%s", name, v.Len(), name)) {
		return
	}
	elem := v.Type().Elem()
	elemKind := registry.KindOf(elem)
	remove := -1
	for i := 0; i < v.Len(); i++ {
		imgui.PushIDInt(int32(i))
		if imgui.SmallButton("x") {
			remove = i
		}
		imgui.SameLine()
		ev := v.Index(i)
		label := fmt.Sprintf("%d", i)
		switch {
		case elemKind != registry.KindOpaque:
			drawField(fc, label, elemKind, ev)
		case elem.Kind() == reflect.Struct:
			if imgui.TreeNodeExStr(label) {
				for _, sf := range registry.FieldsOf(elem) {
					drawField(fc, sf.Name, sf.Kind, sf.Value(ev))
				}
				imgui.TreePop()
			}
		default:
			imgui.TextDisabled("(not editable)")
		}
		imgui.PopID()
	}
	if imgui.SmallButton("+ add") {
		v.Set(reflect.Append(v, reflect.New(elem).Elem()))
		fc.commitNow()
	}
	if remove >= 0 {
		n := v.Len()
		out := reflect.MakeSlice(v.Type(), 0, n-1)
		out = reflect.AppendSlice(out, v.Slice(0, remove))
		out = reflect.AppendSlice(out, v.Slice(remove+1, n))
		v.Set(out)
		fc.commitNow()
	}
	imgui.TreePop()
}

// namedInstances lists the scene's named entities sorted by instance name.
func namedInstances(c *app.Ctx) []namedEntity {
	var out []namedEntity
	app.Query1[scene.Name](c, func(e ecs.Entity, n *scene.Name) {
		out = append(out, namedEntity{n.Value, e})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

type namedEntity struct {
	name string
	e    ecs.Entity
}

const dragSpeed = 0.05

func degOf(rad float32) float32 { return rad * (180 / 3.14159265358979) }

func intOf(v reflect.Value) int64 {
	switch v.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(v.Uint())
	default:
		return v.Int()
	}
}

func setInt(v reflect.Value, n int64) {
	switch v.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n < 0 {
			n = 0
		}
		v.SetUint(uint64(n))
	default:
		v.SetInt(n)
	}
}
