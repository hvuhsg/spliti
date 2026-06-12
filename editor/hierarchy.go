package editor

import (
	"fmt"
	"sort"
	"unsafe"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// hierarchyDragType tags ImGui drag-drop payloads originating from hierarchy
// rows; the payload itself lives in state (cimgui's payload data is a raw
// pointer, Go state is simpler and safer).
const hierarchyDragType = "spliti-instance"

// drawHierarchy lists the scene's named instances as a tree. Rows select on
// click, carry a context menu (rename / duplicate / delete / unparent), and
// reparent via drag and drop; dropping onto the panel background moves an
// instance to the scene root.
func drawHierarchy(c *app.Ctx, st *state) {
	imgui.Begin("Hierarchy")
	defer imgui.End()

	type node struct {
		e        ecs.Entity
		name     string
		children []*node
	}
	byEntity := map[ecs.Entity]*node{}
	var named []*node
	app.Query1[scene.Name](c, func(e ecs.Entity, n *scene.Name) {
		nd := &node{e: e, name: n.Value}
		byEntity[e] = nd
		named = append(named, nd)
	})
	pm := generic.NewMap[render3d.Parent](c.World())
	var roots []*node
	for _, nd := range named {
		if pm.Has(nd.e) {
			if p, ok := byEntity[pm.Get(nd.e).Entity]; ok {
				p.children = append(p.children, nd)
				continue
			}
		}
		roots = append(roots, nd)
	}
	sortNodes := func(ns []*node) {
		sort.Slice(ns, func(i, j int) bool { return ns[i].name < ns[j].name })
	}
	sortNodes(roots)

	var draw func(nd *node)
	draw = func(nd *node) {
		flags := imgui.TreeNodeFlagsOpenOnArrow | imgui.TreeNodeFlagsSpanAvailWidth |
			imgui.TreeNodeFlagsDefaultOpen
		if len(nd.children) == 0 {
			flags |= imgui.TreeNodeFlagsLeaf
		}
		if st.hasSelected && st.selected == nd.e {
			flags |= imgui.TreeNodeFlagsSelected
		}
		open := imgui.TreeNodeExStrV(fmt.Sprintf("%s##%v", nd.name, nd.e), flags)
		if imgui.IsItemClicked() && !imgui.IsItemToggledOpen() {
			st.selected, st.hasSelected = nd.e, true
		}

		if imgui.BeginDragDropSource() {
			st.dragInstance = nd.name
			imgui.SetDragDropPayload(hierarchyDragType, dummyPayload(), 1)
			imgui.TextUnformatted(nd.name)
			imgui.EndDragDropSource()
		}
		if imgui.BeginDragDropTarget() {
			if payloadDelivered(imgui.AcceptDragDropPayload(hierarchyDragType)) && st.dragInstance != "" {
				st.reparent(c, st.dragInstance, nd.name)
				st.dragInstance = ""
			}
			imgui.EndDragDropTarget()
		}

		hierarchyContextMenu(c, st, nd.name, nd.e)

		if open {
			sortNodes(nd.children)
			for _, ch := range nd.children {
				draw(ch)
			}
			imgui.TreePop()
		}
	}
	for _, nd := range roots {
		draw(nd)
	}

	if len(named) == 0 {
		imgui.TextDisabled("no scene.Spawn instances")
	}

	// Remaining panel space is the "scene root" drop zone (unparent).
	avail := imgui.ContentRegionAvail()
	if avail.Y < 24 {
		avail.Y = 24
	}
	imgui.InvisibleButton("##rootdrop", avail)
	if imgui.BeginDragDropTarget() {
		if payloadDelivered(imgui.AcceptDragDropPayload(hierarchyDragType)) && st.dragInstance != "" {
			st.reparent(c, st.dragInstance, "")
			st.dragInstance = ""
		}
		imgui.EndDragDropTarget()
	}

	drawRenamePopup(c, st)
}

func hierarchyContextMenu(c *app.Ctx, st *state, name string, e ecs.Entity) {
	if !imgui.BeginPopupContextItem() {
		return
	}
	st.selected, st.hasSelected = e, true
	if imgui.MenuItemBool("Rename...") {
		st.renameTarget, st.renameBuf = name, name
	}
	if imgui.MenuItemBool("Duplicate") {
		st.duplicateSelection(c)
	}
	pm := generic.NewMap[render3d.Parent](c.World())
	if pm.Has(e) && instanceName(c, pm.Get(e).Entity) != "" {
		if imgui.MenuItemBool("Unparent") {
			st.reparent(c, name, "")
		}
	}
	imgui.Separator()
	if imgui.MenuItemBool("Delete") {
		st.push(c, &cmdDelete{root: name})
	}
	imgui.EndPopup()
}

// drawRenamePopup shows the modal rename editor opened from the context menu.
func drawRenamePopup(c *app.Ctx, st *state) {
	if st.renameTarget == "" {
		return
	}
	imgui.OpenPopupStr("Rename instance")
	if !imgui.BeginPopupModalV("Rename instance", nil, imgui.WindowFlagsAlwaysAutoResize) {
		return
	}
	enter := imgui.InputTextWithHint("##rename", "instance name", &st.renameBuf, imgui.InputTextFlagsEnterReturnsTrue, nil)
	imgui.SetItemDefaultFocus()
	if imgui.Button("Rename") || enter {
		if st.renameBuf != st.renameTarget {
			st.push(c, &cmdRename{old: st.renameTarget, new: st.renameBuf})
		}
		st.renameTarget = ""
		imgui.CloseCurrentPopup()
	}
	imgui.SameLine()
	if imgui.Button("Cancel") {
		st.renameTarget = ""
		imgui.CloseCurrentPopup()
	}
	imgui.EndPopup()
}

// reparent pushes a reparent command, recomputing the local transform so the
// instance keeps its world placement under the new parent.
func (st *state) reparent(c *app.Ctx, instance, newParent string) {
	e, ok := entityByInstance(c, instance)
	if !ok || instance == newParent {
		return
	}
	w := c.World()
	pm := generic.NewMap[render3d.Parent](w)
	oldParent := ""
	if pm.Has(e) {
		oldParent = instanceName(c, pm.Get(e).Entity)
	}
	if oldParent == newParent {
		return
	}
	tm := generic.NewMap[render3d.Transform3D](w)
	gtm := generic.NewMap[render3d.GlobalTransform](w)
	if !tm.Has(e) || !gtm.Has(e) {
		return
	}
	oldLocal := *tm.Get(e)
	global := gtm.Get(e).Matrix

	newLocalMat := global
	if newParent != "" {
		pe, ok := entityByInstance(c, newParent)
		if !ok || !gtm.Has(pe) {
			st.status(fmt.Sprintf("cannot parent under %q", newParent))
			return
		}
		if pe == e || isDescendant(c, pe, e) {
			st.status("cannot parent an instance under its own subtree")
			return
		}
		inv, ok := gtm.Get(pe).Matrix.Inverse()
		if !ok {
			st.status("parent transform is degenerate")
			return
		}
		newLocalMat = inv.Mul(global)
	}
	var newLocal render3d.Transform3D
	newLocal.Translation, newLocal.Rotation, newLocal.Scale = decomposeTRS(newLocalMat)

	st.push(c, &cmdReparent{
		instance:  instance,
		oldParent: oldParent, newParent: newParent,
		oldLocal: oldLocal, newLocal: newLocal,
	})
}

// dummyPayload is the 1-byte ImGui drag payload — the real payload travels
// through editor state (dragInstance/dragPrefab); imgui copies the byte
// immediately, so the pointer's lifetime is the call.
var payloadByte byte

func dummyPayload() uintptr { return uintptr(unsafe.Pointer(&payloadByte)) }

// payloadDelivered reports an actual drop. cimgui-go's AcceptDragDropPayload
// wraps even a NULL C payload in a non-nil Go struct, so a plain `!= nil`
// check fires on every hovered frame of the drag; the C pointer (CData) is
// only set on delivery (mouse release). Calling Payload methods on the NULL
// wrapper would pass NULL into C — inspect the field directly.
func payloadDelivered(p *imgui.Payload) bool {
	return p != nil && p.CData != nil
}
