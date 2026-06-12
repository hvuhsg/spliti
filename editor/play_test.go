package editor

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/generic"
)

// TestSnapshotRestore exercises the Play snapshot: mutate, despawn, and spawn
// while "playing", then restore and verify the world is back, with Parent
// links remapped onto the new entity handles.
func TestSnapshotRestore(t *testing.T) {
	c, st := newCmdEditor(t)
	tm := generic.NewMap[render3d.Transform3D](c.World())

	crate1, _ := entityByInstance(c, "crate1")
	ti := st.reg.Lookup("Health")
	ti.SetValue(c.World(), crate1, Health{Max: 50, Current: 30})
	wantT := *tm.Get(crate1)

	// An entity reference + slice in a game component: the restore must remap
	// the handle and the snapshot must not share the slice's backing array.
	ground, _ := entityByInstance(c, "ground")
	sk := st.reg.Lookup("Seeker")
	sk.SetValue(c.World(), ground, Seeker{Target: crate1, Waypoints: []m.Vec3{{X: 1}}})

	snap := takeSnapshot(c, st.reg)

	// "Play": move crate1, kill lamp, spawn a play-only bullet, and scribble
	// over the Seeker's slice in place.
	tm.Get(crate1).Translation.X = 99
	sm := generic.NewMap[Seeker](c.World())
	sm.Get(ground).Waypoints[0].X = -42
	lamp, _ := entityByInstance(c, "lamp")
	despawnSubtree(c, lamp)
	bullet := c.World().NewEntity()

	snap.restore(c)

	crate1, ok := entityByInstance(c, "crate1")
	if !ok {
		t.Fatal("crate1 missing after restore")
	}
	if got := *tm.Get(crate1); got != wantT {
		t.Fatalf("crate1 transform = %+v, want %+v", got, wantT)
	}
	if got := ti.Value(c.World(), crate1).(Health); got != (Health{Max: 50, Current: 30}) {
		t.Fatalf("Health = %+v", got)
	}
	lamp, ok = entityByInstance(c, "lamp")
	if !ok {
		t.Fatal("lamp not restored")
	}
	pm := generic.NewMap[render3d.Parent](c.World())
	if !pm.Has(lamp) {
		t.Fatal("lamp lost its Parent")
	}
	if got := pm.Get(lamp).Entity; got != crate1 {
		t.Fatalf("lamp parent = %v, want remapped crate1 %v", got, crate1)
	}
	if c.World().Alive(bullet) {
		t.Fatal("play-created entity survived restore")
	}
	ground, ok = entityByInstance(c, "ground")
	if !ok {
		t.Fatal("ground missing after restore")
	}
	got := sk.Value(c.World(), ground).(Seeker)
	if got.Target != crate1 {
		t.Fatalf("Seeker.Target = %v, want remapped crate1 %v", got.Target, crate1)
	}
	if len(got.Waypoints) != 1 || got.Waypoints[0].X != 1 {
		t.Fatalf("Seeker.Waypoints not deep-copied: %+v", got.Waypoints)
	}
}

// TestPlayModeGating runs a real app loop: a game system registered through
// the interceptor must run only while playing, freeze on pause, advance
// exactly one frame per Step, and the world must be restored on Stop.
func TestPlayModeGating(t *testing.T) {
	a := app.New()
	st := newState(Plugin{})
	app.InsertResource(a, st)

	ran := 0
	a.SetSystemInterceptor(st.interceptGameSystem)
	a.AddSystems(schedule.Update, app.System(func(c *app.Ctx) {
		ran++
		tm := generic.NewMap[render3d.Transform3D](c.World())
		if e, ok := entityByInstance(c, "crate1"); ok && tm.Has(e) {
			tm.Get(e).Translation.X += 1
		}
	}).Label("mover"))
	a.SetSystemInterceptor(nil)

	if len(st.gameSystems) != 1 || st.gameSystems[0].name != "mover" {
		t.Fatalf("gameSystems = %+v", st.gameSystems)
	}

	// Startup systems must pass through the interceptor ungated.
	a.SetSystemInterceptor(st.interceptGameSystem)
	startupRan := false
	a.AddSystems(schedule.Startup, func(c *app.Ctx) { startupRan = true })
	a.SetSystemInterceptor(nil)

	a.AddSystems(schedule.First, func(c *app.Ctx) {
		switch c.App().Frame() {
		case 1:
			st.startPlay(c)
		case 3:
			st.togglePause()
		case 5:
			st.requestStep()
		case 9:
			st.stopPlay(c)
		}
	})
	a.AddSystems(schedule.Last, stepClock)

	a.SetMaxFrames(12)
	a.Run()

	if !startupRan {
		t.Fatal("startup system was gated")
	}
	// Playing on frames 1,2 (paused at frame 3's First, before Update), then
	// exactly one stepped frame (armed at frame 5's Last, run frame 6).
	if ran != 3 {
		t.Fatalf("game system ran %d times, want 3", ran)
	}
	if st.mode != modeEdit {
		t.Fatalf("mode = %v after stop", st.mode)
	}
}

// TestPlayEditsAreLiveOnly: while playing, pushes bypass the undo stack and
// never touch the scene source.
func TestPlayEditsAreLiveOnly(t *testing.T) {
	c, st := newCmdEditor(t)
	e, _ := entityByInstance(c, "crate1")
	tm := generic.NewMap[render3d.Transform3D](c.World())
	srcBefore := modelSrc(t, st)

	st.mode = modePlaying
	st.push(c, &cmdTransform{
		instance: "crate1", entity: e,
		before: *tm.Get(e),
		after:  render3d.XForm().At(7, 7, 7),
	})

	if got := tm.Get(e).Translation.X; got != 7 {
		t.Fatalf("live transform not applied: X = %v", got)
	}
	if got := modelSrc(t, st); got != srcBefore {
		t.Fatal("play edit reached the scene source")
	}
	if _, can := st.undo.CanUndo(); can {
		t.Fatal("play edit entered the undo stack")
	}
	if st.pendingSave {
		t.Fatal("play edit scheduled a save")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	c, st := newCmdEditor(t)
	e, _ := entityByInstance(c, "crate1")
	g, _ := entityByInstance(c, "ground")
	st.selectOne(g)
	st.toggleSelect(e) // ground + crate1, crate1 primary
	st.cam.pivot = m.Vec3{X: 1, Y: 2, Z: 3}
	st.cam.dist, st.cam.yaw, st.cam.pitch = 7, 0.25, -0.5

	st.saveSession(c)

	st.clearSelection()
	st.cam = defaultRig()
	st.restoreSession(c)

	if len(st.sel) != 2 {
		t.Fatalf("selection not restored: %v", st.sel)
	}
	if prim, ok := st.primary(); !ok || instanceName(c, prim) != "crate1" {
		t.Fatal("primary selection not restored")
	}
	if !st.isSelected(g) {
		t.Fatal("multi-selection member not restored")
	}
	if st.cam.pivot != (m.Vec3{X: 1, Y: 2, Z: 3}) || st.cam.dist != 7 || st.cam.yaw != 0.25 || st.cam.pitch != -0.5 {
		t.Fatalf("camera not restored: %+v", st.cam)
	}
}

func TestParseSourceLocation(t *testing.T) {
	cases := []struct {
		in   string
		file string
		line int
	}{
		{"../../game/components/health.go:12:5: undefined: foo", "../../game/components/health.go", 12},
		{"main.go:3: syntax error", "main.go", 3},
		{"# demo/game", "", 0},
		{"go: downloading something", "", 0},
	}
	for _, tc := range cases {
		f, l := parseSourceLocation(tc.in)
		if f != tc.file || l != tc.line {
			t.Errorf("parseSourceLocation(%q) = %q,%d want %q,%d", tc.in, f, l, tc.file, tc.line)
		}
	}
}
