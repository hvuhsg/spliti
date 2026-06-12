package editor

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/registry"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
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

	if !st.hasSelected || !c.World().Alive(st.selected) {
		imgui.TextDisabled("nothing selected")
		return
	}
	e := st.selected
	inst := instanceName(c, e)
	if inst != "" {
		imgui.TextUnformatted(inst)
	} else {
		imgui.TextDisabled("(unnamed entity - edits are live-only)")
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

// drawComponent renders one component's fields and handles gesture capture.
func drawComponent(c *app.Ctx, st *state, e ecs.Entity, inst string, ti *registry.TypeInfo) {
	w := c.World()
	// Pre-draw snapshot: the gesture's "before" value must be read before any
	// widget writes into live storage this frame.
	pre := ti.Value(w, e)
	gestureKey := fmt.Sprintf("%v/%s", e, ti.Name)

	comp := ti.Get(w, e)
	for _, f := range ti.Fields {
		drawField(st, e, ti, f, f.Value(comp))
		if imgui.IsItemActivated() {
			if _, ok := st.editBefore[gestureKey]; !ok {
				st.editBefore[gestureKey] = pre
			}
		}
		if imgui.IsItemDeactivatedAfterEdit() {
			before, ok := st.editBefore[gestureKey]
			if !ok {
				before = pre
			}
			delete(st.editBefore, gestureKey)
			st.commitComponentEdit(c, e, inst, ti, before)
		} else if imgui.IsItemDeactivated() {
			delete(st.editBefore, gestureKey)
		}
	}
}

// commitComponentEdit pushes the finished gesture as one undo command.
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
	st.push(c, st.newSetComponent(inst, e, ti, before, after))
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

// drawField renders the widget for one field and returns whether it changed
// the value this frame. Values write straight into live component storage.
func drawField(st *state, e ecs.Entity, ti *registry.TypeInfo, f registry.Field, v reflect.Value) bool {
	switch f.Kind {
	case registry.KindFloat32, registry.KindFloat64:
		f32 := float32(v.Float())
		if imgui.DragFloatV(f.Name, &f32, dragSpeed(f), 0, 0, "%.3f", 0) {
			v.SetFloat(float64(f32))
			return true
		}
	case registry.KindInt:
		iv := int32(intOf(v))
		if imgui.DragInt(f.Name, &iv) {
			setInt(v, int64(iv))
			return true
		}
	case registry.KindBool:
		b := v.Bool()
		if imgui.Checkbox(f.Name, &b) {
			v.SetBool(b)
			return true
		}
	case registry.KindString:
		s := v.String()
		if imgui.InputTextWithHint(f.Name, "", &s, 0, nil) {
			v.SetString(s)
			return true
		}
	case registry.KindVec2:
		vec := v.Interface().(m.Vec2)
		arr := [2]float32{vec.X, vec.Y}
		if imgui.DragFloat2(f.Name, &arr) {
			v.Set(reflect.ValueOf(m.Vec2{X: arr[0], Y: arr[1]}))
			return true
		}
	case registry.KindVec3:
		vec := v.Interface().(m.Vec3)
		arr := [3]float32{vec.X, vec.Y, vec.Z}
		if imgui.DragFloat3V(f.Name, &arr, dragSpeed(f), 0, 0, "%.3f", 0) {
			v.Set(reflect.ValueOf(m.Vec3{X: arr[0], Y: arr[1], Z: arr[2]}))
			return true
		}
	case registry.KindVec4:
		vec := v.Interface().(m.Vec4)
		arr := [4]float32{vec.X, vec.Y, vec.Z, vec.W}
		if imgui.DragFloat4(f.Name, &arr) {
			v.Set(reflect.ValueOf(m.Vec4{X: arr[0], Y: arr[1], Z: arr[2], W: arr[3]}))
			return true
		}
	case registry.KindQuat:
		return drawQuatField(st, e, ti, f, v)
	case registry.KindEntity:
		ent := v.Interface().(ecs.Entity)
		imgui.TextDisabled(fmt.Sprintf("%s: entity %v", f.Name, ent))
	default:
		imgui.TextDisabled(fmt.Sprintf("%s: (not editable)", f.Name))
	}
	return false
}

// drawQuatField edits a quaternion as Euler degrees. The displayed angles come
// from an activation-scoped cache: while the user is dragging, the cache —
// not the lossy quat→Euler conversion of the value just written — feeds the
// widget, so the numbers never feed back or jump mid-drag.
func drawQuatField(st *state, e ecs.Entity, ti *registry.TypeInfo, f registry.Field, v reflect.Value) bool {
	key := fmt.Sprintf("%v/%s/%s", e, ti.Name, f.Name)
	q := v.Interface().(m.Quat)

	arr, cached := st.eulerCache[key]
	if !cached {
		p, y, r := q.ToEuler()
		arr = [3]float32{degOf(p), degOf(y), degOf(r)}
	}
	changed := imgui.DragFloat3V(f.Name+" (deg)", &arr, 0.5, 0, 0, "%.2f", 0)
	if imgui.IsItemActive() {
		st.eulerCache[key] = arr
	} else {
		delete(st.eulerCache, key)
	}
	if changed {
		v.Set(reflect.ValueOf(m.FromEuler(
			m.DegToRad(arr[0]), m.DegToRad(arr[1]), m.DegToRad(arr[2]))))
		return true
	}
	return false
}

func dragSpeed(f registry.Field) float32 {
	return 0.05
}

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
