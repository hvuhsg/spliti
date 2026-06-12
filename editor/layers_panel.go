package editor

import (
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/srcmodel"
)

// loadLayers locates and parses the game's //spliti:layers const block. The
// block is optional; without one the Layers panel shows a hint and Collider3D
// falls back to plain numeric fields.
func (st *state) loadLayers() {
	dir := filepath.Join(st.cfg.ProjectRoot, "game")
	path, ok := srcmodel.FindLayersFile(dir)
	if !ok {
		st.layers, st.layersErr = nil, nil
		return
	}
	st.layers, st.layersErr = srcmodel.ParseLayersFile(path)
}

// saveLayers persists a layers edit immediately (no debounce — these edits
// are rare and renames invalidate game code, which the rebuild banner already
// reports through the watcher).
func (st *state) saveLayers() {
	if st.layers == nil {
		return
	}
	if err := st.layers.Save(); err != nil {
		st.status(err.Error())
		return
	}
	st.status(fmt.Sprintf("saved %s", filepath.Base(st.layers.Path)))
}

// drawLayers is the collision-layers panel: one row per named bit, editable
// in place, plus an append row. Bits cannot be removed — that would silently
// renumber every later layer in compiled game code.
func drawLayers(c *app.Ctx, st *state) {
	if !imgui.Begin("Layers") {
		imgui.End()
		return
	}
	defer imgui.End()
	if st.layersErr != nil {
		imgui.TextColored(imgui.Vec4{X: 1, Y: 0.35, Z: 0.3, W: 1}, st.layersErr.Error())
		return
	}
	if st.layers == nil {
		imgui.TextDisabled("no //spliti:layers const block found")
		imgui.TextDisabled("(declare one in game/layers.go)")
		return
	}
	editing := st.mode == modeEdit
	if !editing {
		imgui.BeginDisabled()
	}
	for bit, name := range st.layers.Names {
		imgui.PushIDInt(int32(bit))
		imgui.TextDisabled(fmt.Sprintf("%2d", bit))
		imgui.SameLine()
		val := name
		if buf, ok := st.layerEdit[bit]; ok {
			val = buf
		}
		imgui.SetNextItemWidth(-1)
		imgui.InputTextWithHint("##name", "(unnamed)", &val, 0, nil)
		if imgui.IsItemActive() {
			st.layerEdit[bit] = val
		} else if imgui.IsItemDeactivatedAfterEdit() {
			delete(st.layerEdit, bit)
			if val != name {
				if err := st.layers.Rename(bit, val); err != nil {
					st.status(err.Error())
				} else {
					st.saveLayers()
				}
			}
		} else {
			delete(st.layerEdit, bit)
		}
		imgui.PopID()
	}
	if len(st.layers.Names) < srcmodel.MaxLayers {
		imgui.Separator()
		imgui.SetNextItemWidth(-80)
		enter := imgui.InputTextWithHint("##newlayer", "new layer name", &st.newLayerBuf, imgui.InputTextFlagsEnterReturnsTrue, nil)
		imgui.SameLine()
		if (imgui.Button("+ add") || enter) && st.newLayerBuf != "" {
			if err := st.layers.Add(st.newLayerBuf); err != nil {
				st.status(err.Error())
			} else {
				st.newLayerBuf = ""
				st.saveLayers()
			}
		}
	}
	if !editing {
		imgui.EndDisabled()
	}
}

// drawColliderFields is the Collider3D inspector widget when named layers
// exist: half extents as usual, Layer as a combo, Mask as a checkbox list.
func drawColliderFields(fc fieldCtx, comp reflect.Value) {
	for _, f := range fc.ti.Fields {
		switch f.Name {
		case "Layer":
			drawLayerCombo(fc, f.Name, f.Value(comp))
		case "Mask":
			drawMaskChecks(fc, f.Name, f.Value(comp))
		default:
			drawField(fc, f.Name, f.Kind, f.Value(comp))
		}
	}
}

// layerLabel renders a bitset for display: a single named bit shows its name.
func layerLabel(st *state, bits uint32) string {
	if bits == 0 {
		return "(none)"
	}
	for bit, name := range st.layers.Names {
		if name != "" && bits == 1<<bit {
			return name
		}
	}
	return fmt.Sprintf("0x%X", bits)
}

func drawLayerCombo(fc fieldCtx, name string, v reflect.Value) {
	st := fc.st
	cur := uint32(v.Uint())
	if !imgui.BeginCombo(name, layerLabel(st, cur)) {
		return
	}
	if imgui.SelectableBool("(none)") && cur != 0 {
		v.SetUint(0)
		fc.commitNow()
	}
	for bit, lname := range st.layers.Names {
		if lname == "" {
			continue
		}
		val := uint32(1) << bit
		if imgui.SelectableBoolV(lname, cur == val, 0, imgui.Vec2{}) && cur != val {
			v.SetUint(uint64(val))
			fc.commitNow()
		}
	}
	imgui.EndCombo()
}

func drawMaskChecks(fc fieldCtx, name string, v reflect.Value) {
	st := fc.st
	cur := uint32(v.Uint())
	open := imgui.TreeNodeExStr(fmt.Sprintf("%s: %s###%s", name, layerLabel(st, cur), name))
	if !open {
		return
	}
	for bit, lname := range st.layers.Names {
		if lname == "" {
			continue
		}
		mask := uint32(1) << bit
		on := cur&mask != 0
		if imgui.Checkbox(lname, &on) {
			cur ^= mask
			v.SetUint(uint64(cur))
			fc.commitNow()
		}
	}
	imgui.TreePop()
}
