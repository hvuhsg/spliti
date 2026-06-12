package editor

import (
	"fmt"
	"sort"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// drawHierarchy lists the scene's named instances as a tree (read-only in M1:
// selection only, no rename/reparent/delete).
func drawHierarchy(c *app.Ctx, st *state) {
	imgui.Begin("Hierarchy")
	defer imgui.End()

	// Collect named entities and their parent links.
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
}
