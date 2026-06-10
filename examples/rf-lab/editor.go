//go:build !js

package main

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Mode is the global interaction mode. Systems consult it instead of fighting
// over raw input: in ModeGraph the 3D controls simply do nothing.
type Mode int

const (
	ModeExplore Mode = iota // fly camera, select/drag/configure markers
	ModeGraph               // node-graph editor is up; 3D controls are inert
)

// UI holds the current interaction mode (shared across systems).
type UI struct{ Mode Mode }

func newUI() *UI { return &UI{Mode: ModeExplore} }

// Editor holds the node-graph editor's cross-frame state: which device's graph is
// open and the in-progress wire drag. Node positions live on the graph's Nodes,
// and ImGui owns transient interaction (hover/active/focus), so little needs to
// persist here. Whether the editor is shown is UI.Mode.
type Editor struct {
	target    Selection  // which kind of device's graph is open (SelTx / SelRx)
	targetEnt ecs.Entity // the specific device entity whose graph is open

	wireFrom int // node id a wire is being dragged from (output), 0 = none

	// Palette drag-to-create: while palAdding is true a new node of palKind is being
	// dragged out of the bottom palette, and is dropped onto the canvas on release.
	palAdding bool
	palKind   NodeKind
}

func newEditor() *Editor { return &Editor{} }

// boundGraph resolves the graph (and TX playback, if any) of the bound device.
func boundGraph(c *app.Ctx, ed *Editor) (*Graph, *Play) {
	if ed.targetEnt.IsZero() {
		return nil, nil
	}
	switch ed.target {
	case SelTx:
		mp := generic.NewMap[TxDevice](c.World())
		if mp.Has(ed.targetEnt) {
			d := mp.Get(ed.targetEnt)
			return d.Graph, d.Play
		}
	case SelRx:
		mp := generic.NewMap[RxDevice](c.World())
		if mp.Has(ed.targetEnt) {
			return mp.Get(ed.targetEnt).Graph, nil
		}
	}
	return nil, nil
}

// hasInput/hasOutput give a node's port directions, which depend on the chain
// kind (a Text node is a source in TX, a sink in RX).
func hasInput(n *Node, gk GraphKind) bool {
	switch n.Kind {
	case KindReceiver:
		return false
	case KindText:
		return gk == GraphRx
	default:
		return true
	}
}

func hasOutput(n *Node, gk GraphKind) bool {
	switch n.Kind {
	case KindTransmitter:
		return false
	case KindText:
		return gk == GraphTx
	default:
		return true
	}
}

// markDirty flags the TX playback for recompile (no-op for an RX chain).
func markDirty(g *Graph, play *Play) {
	if play != nil && g.Kind == GraphTx {
		play.dirty = true
	}
}

// clip truncates s to at most n runes (for node display).
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
