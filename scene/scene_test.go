package scene

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

type health struct{ Max, Current int }

func newCtx(t *testing.T) *app.Ctx {
	t.Helper()
	return app.New().Ctx()
}

func spawnBare(c *app.Ctx) ecs.Entity {
	mp := generic.NewMap1[render3d.Transform3D](c.World())
	tr := render3d.XForm()
	return mp.NewWith(&tr)
}

func TestSpawnAttachesName(t *testing.T) {
	c := newCtx(t)
	e := Spawn(c, "crate1", spawnBare(c))
	nm := generic.NewMap[Name](c.World())
	if !nm.Has(e) {
		t.Fatal("Spawn did not attach Name")
	}
	if got := nm.Get(e).Value; got != "crate1" {
		t.Fatalf("Name = %q", got)
	}
}

func TestSetAddsThenOverwrites(t *testing.T) {
	c := newCtx(t)
	e := spawnBare(c)
	Set(c, e, health{Max: 50, Current: 50})
	Set(c, e, health{Max: 80, Current: 20})
	hp := generic.NewMap[health](c.World())
	if got := *hp.Get(e); got != (health{Max: 80, Current: 20}) {
		t.Fatalf("health = %+v", got)
	}
}

func TestRemoveIsIdempotent(t *testing.T) {
	c := newCtx(t)
	e := spawnBare(c)
	Set(c, e, health{Max: 1})
	Remove[health](c, e)
	Remove[health](c, e) // absent: must not panic
	hp := generic.NewMap[health](c.World())
	if hp.Has(e) {
		t.Fatal("health still present after Remove")
	}
}

func TestParentLinksAndRelinks(t *testing.T) {
	c := newCtx(t)
	a, b, child := spawnBare(c), spawnBare(c), spawnBare(c)
	Parent(c, child, a)
	Parent(c, child, b) // re-parent must overwrite, not panic
	pm := generic.NewMap[render3d.Parent](c.World())
	if got := pm.Get(child).Entity; got != b {
		t.Fatalf("parent = %v, want %v", got, b)
	}
}
