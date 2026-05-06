package app_test

import (
	"strings"
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
)

type KeyEvent struct{ Key rune }

func TestEventsRoundtrip(t *testing.T) {
	var seen []rune
	app.New().
		AddSystems(schedule.Update, app.System(func(c *app.Ctx) {
			app.SendEvent(c, KeyEvent{Key: 'q'})
		}).Label("send")).
		AddSystems(schedule.Update, app.System(func(c *app.Ctx) {
			for _, ev := range app.ReadEvents[KeyEvent](c) {
				seen = append(seen, ev.Key)
			}
		}).Label("read").After("send")).
		SetMaxFrames(1).
		Run()

	if string(seen) != "q" {
		t.Fatalf("seen=%q, want %q", string(seen), "q")
	}
}

func TestEventsClearedBetweenFrames(t *testing.T) {
	var counts []int
	frame := 0
	app.New().
		AddSystems(schedule.First, func(*app.Ctx) { frame++ }).
		AddSystems(schedule.Update, app.System(func(c *app.Ctx) {
			if frame == 1 {
				app.SendEvent(c, KeyEvent{Key: 'a'})
			}
		}).Label("send")).
		AddSystems(schedule.PostUpdate, app.System(func(c *app.Ctx) {
			counts = append(counts, len(app.ReadEvents[KeyEvent](c)))
		}).Label("count")).
		SetMaxFrames(3).
		Run()

	if len(counts) != 3 || counts[0] != 1 || counts[1] != 0 || counts[2] != 0 {
		t.Fatalf("counts=%v, want [1 0 0]", counts)
	}
}

type GameState int

const (
	GameMenu GameState = iota
	GamePlaying
	GameOver
)

func TestStateTransitionsFireOnEnterOnExit(t *testing.T) {
	var log []string
	a := app.New()
	app.InitState(a, GameMenu)
	app.OnEnter(a, GameMenu, func(*app.Ctx) { log = append(log, "enter:menu") })
	app.OnExit(a, GameMenu, func(*app.Ctx) { log = append(log, "exit:menu") })
	app.OnEnter(a, GamePlaying, func(*app.Ctx) { log = append(log, "enter:playing") })
	app.OnExit(a, GamePlaying, func(*app.Ctx) { log = append(log, "exit:playing") })
	app.OnEnter(a, GameOver, func(*app.Ctx) { log = append(log, "enter:over") })

	frame := 0
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		frame++
		s := app.GetState[GameState](c)
		switch frame {
		case 1:
			s.Set(GamePlaying)
		case 2:
			s.Set(GameOver)
		}
	}).SetMaxFrames(3).Run()

	got := strings.Join(log, ",")
	want := "enter:menu,exit:menu,enter:playing,exit:playing,enter:over"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGetStateReadsCurrent(t *testing.T) {
	a := app.New()
	app.InitState(a, GameMenu)

	var observed []GameState
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		s := app.GetState[GameState](c)
		observed = append(observed, s.Get())
		if s.Get() == GameMenu {
			s.Set(GamePlaying)
		}
	}).SetMaxFrames(2).Run()

	if observed[0] != GameMenu || observed[1] != GamePlaying {
		t.Fatalf("observed=%v, want [Menu Playing]", observed)
	}
}

func TestInsertAndGetResource(t *testing.T) {
	type Score struct{ Value int }
	a := app.New()
	app.InsertResource(a, &Score{Value: 42})

	var got int
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		s := app.GetResource[Score](c)
		got = s.Value
		s.Value++
	}).SetMaxFrames(3).Run()

	if got != 44 {
		t.Fatalf("after 3 frames got %d, want 44", got)
	}
}
