package ui_test

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/ui"
	"github.com/hvuhsg/spliti/plugin/input"
)

func TestRect_Contains(t *testing.T) {
	r := ui.Rect{X: 5, Y: 5, W: 3, H: 2}
	if !r.Contains(5, 5) {
		t.Fatal("top-left corner")
	}
	if !r.Contains(7, 6) {
		t.Fatal("bottom-right inclusive")
	}
	if r.Contains(8, 6) {
		t.Fatal("right edge exclusive")
	}
	if r.Contains(4, 5) {
		t.Fatal("outside left")
	}
}

func TestRect_Inset(t *testing.T) {
	r := ui.Rect{X: 0, Y: 0, W: 10, H: 4}.Inset(1)
	if r.X != 1 || r.Y != 1 || r.W != 8 || r.H != 2 {
		t.Fatalf("inset 1: got %+v", r)
	}
	r = ui.Rect{X: 0, Y: 0, W: 1, H: 1}.Inset(2)
	if r.W != 0 || r.H != 0 {
		t.Fatalf("inset clamps to zero: got %+v", r)
	}
}

// namedPanel is a trivial ui.Panel implementation used only to verify
// layout assignment without involving terminal/input subsystems.
type namedPanel struct{ name string }

func (p namedPanel) Name() string                                       { return p.name }
func (p namedPanel) Title() string                                      { return p.name }
func (namedPanel) Render(_ *app.Ctx, _ ui.Rect, _ bool)                 {}
func (namedPanel) OnMouse(_ *app.Ctx, _, _ int, _ input.MouseEvent)     {}
func (namedPanel) OnKey(_ *app.Ctx, _ input.KeyEvent) bool              { return false }

func TestLayout_RecomputeAssignsAllSlots(t *testing.T) {
	l := ui.NewLayout(
		namedPanel{ui.NameMenuBar},
		namedPanel{ui.NameStatus},
		namedPanel{ui.NameHierarchy},
		namedPanel{ui.NameAssets},
		namedPanel{ui.NameInspector},
		namedPanel{ui.NameToolbar},
		namedPanel{ui.NameViewport},
	)
	l.Recompute(80, 24)

	for _, name := range []string{
		ui.NameMenuBar, ui.NameStatus, ui.NameHierarchy, ui.NameAssets,
		ui.NameInspector, ui.NameToolbar, ui.NameViewport,
	} {
		r := l.Rect(name)
		if r.W <= 0 || r.H <= 0 {
			t.Fatalf("slot %s got zero rect: %+v", name, r)
		}
	}

	mb := l.Rect(ui.NameMenuBar)
	if mb.Y != 0 || mb.H != 1 || mb.X != 0 || mb.W != 80 {
		t.Fatalf("menubar = %+v", mb)
	}
	st := l.Rect(ui.NameStatus)
	if st.Y != 23 || st.H != 1 {
		t.Fatalf("status = %+v", st)
	}
	vp := l.Rect(ui.NameViewport)
	tb := l.Rect(ui.NameToolbar)
	if vp.Y != tb.Y+tb.H {
		t.Fatalf("viewport (%v) should sit immediately below toolbar (%v)", vp, tb)
	}
}

func TestLayout_PanelAt_HitTestsRect(t *testing.T) {
	l := ui.NewLayout(
		namedPanel{ui.NameMenuBar},
		namedPanel{ui.NameViewport},
		namedPanel{ui.NameStatus},
	)
	l.Recompute(80, 24)
	if got := l.PanelAt(40, 0); got == nil || got.Name() != ui.NameMenuBar {
		t.Fatalf("PanelAt menubar = %v", got)
	}
	if got := l.PanelAt(40, 12); got == nil || got.Name() != ui.NameViewport {
		t.Fatalf("PanelAt center = %v", got)
	}
	if got := l.PanelAt(40, 23); got == nil || got.Name() != ui.NameStatus {
		t.Fatalf("PanelAt status = %v", got)
	}
}
