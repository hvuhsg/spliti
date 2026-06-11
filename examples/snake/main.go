// Snake — the spliti acceptance test, and a passable game.
//
// Controls: arrow keys / wasd to steer, q or ctrl-c to quit, r to restart.
package main

import (
	"fmt"
	"math"
	"math/rand"
	gotime "time"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/audio"
	"github.com/hvuhsg/spliti/plugin/defaultplugins"
	"github.com/hvuhsg/spliti/plugin/input"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

const (
	boardW  = 40
	boardH  = 20
	hudRows = 1 // top row reserved for HUD
)

// --- Components ----------------------------------------------------------

// Food marks an entity as edible. The render-relevant components Position +
// Glyph come from the tui plugin.
type Food struct{}

// --- Resources -----------------------------------------------------------

type Dir int

const (
	DirRight Dir = iota
	DirLeft
	DirUp
	DirDown
)

func (d Dir) delta() (int, int) {
	switch d {
	case DirUp:
		return 0, -1
	case DirDown:
		return 0, 1
	case DirLeft:
		return -1, 0
	case DirRight:
		return 1, 0
	}
	return 0, 0
}

func (d Dir) opposite() Dir {
	return [...]Dir{DirLeft, DirRight, DirDown, DirUp}[d]
}

type Snake struct {
	Segments  []ecs.Entity   // head at [0]
	Positions []tui.Position // parallel to Segments
	Dir       Dir
	NextDir   Dir
	Grow      bool
}

type Score struct{ Value int }

type GameMode int

const (
	Playing GameMode = iota
	GameOver
)

// --- Styles --------------------------------------------------------------

var (
	styleHead = tcell.StyleDefault.Foreground(tcell.ColorLightGreen).Bold(true)
	styleBody = tcell.StyleDefault.Foreground(tcell.ColorGreen)
	styleFood = tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	styleHUD  = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	styleOver = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
)

// --- Setup / teardown ----------------------------------------------------

func setupGame(c *app.Ctx) {
	s := app.GetResource[Snake](c)
	score := app.GetResource[Score](c)
	score.Value = 0
	s.Dir = DirRight
	s.NextDir = DirRight
	s.Grow = false
	s.Segments = s.Segments[:0]
	s.Positions = s.Positions[:0]

	mapper := generic.NewMap2[tui.Position, tui.Glyph](c.World())
	cx, cy := boardW/2, hudRows+boardH/2
	for i := 0; i < 4; i++ {
		p := tui.Position{X: cx - i, Y: cy}
		style := styleBody
		ch := 'o'
		if i == 0 {
			style = styleHead
			ch = '@'
		}
		e := mapper.NewWith(&tui.Position{X: p.X, Y: p.Y}, &tui.Glyph{Char: ch, Style: style})
		s.Segments = append(s.Segments, e)
		s.Positions = append(s.Positions, p)
	}
	spawnFood(c)
}

func teardownGame(c *app.Ctx) {
	s := app.GetResource[Snake](c)
	for _, e := range s.Segments {
		if c.World().Alive(e) {
			c.World().RemoveEntity(e)
		}
	}
	s.Segments = s.Segments[:0]
	s.Positions = s.Positions[:0]
	// Wipe any food entities.
	var toRemove []ecs.Entity
	app.Query1[Food](c, func(e ecs.Entity, _ *Food) {
		toRemove = append(toRemove, e)
	})
	for _, e := range toRemove {
		c.World().RemoveEntity(e)
	}
}

func spawnFood(c *app.Ctx) {
	occupied := map[tui.Position]bool{}
	app.Query1[tui.Position](c, func(_ ecs.Entity, p *tui.Position) {
		occupied[*p] = true
	})

	for tries := 0; tries < 200; tries++ {
		p := tui.Position{
			X: rand.Intn(boardW),
			Y: hudRows + rand.Intn(boardH),
		}
		if occupied[p] {
			continue
		}
		mapper := generic.NewMap3[tui.Position, tui.Glyph, Food](c.World())
		mapper.NewWith(&tui.Position{X: p.X, Y: p.Y}, &tui.Glyph{Char: '*', Style: styleFood}, &Food{})
		return
	}
}

// --- Systems -------------------------------------------------------------

// handleInput translates KeyEvents into snake direction or state changes.
func handleInput(c *app.Ctx) {
	state := app.GetState[GameMode](c)
	for _, ev := range app.ReadEvents[input.KeyEvent](c) {
		// Quit
		if ev.Key == tcell.KeyCtrlC || ev.Key == tcell.KeyEscape || ev.Rune == 'q' {
			c.App().Stop()
			return
		}
		if state.Get() == GameOver {
			if ev.Rune == 'r' {
				state.Set(Playing)
			}
			continue
		}
		// Steering during play
		s := app.GetResource[Snake](c)
		var d Dir
		matched := true
		switch {
		case ev.Key == tcell.KeyUp || ev.Rune == 'w':
			d = DirUp
		case ev.Key == tcell.KeyDown || ev.Rune == 's':
			d = DirDown
		case ev.Key == tcell.KeyLeft || ev.Rune == 'a':
			d = DirLeft
		case ev.Key == tcell.KeyRight || ev.Rune == 'd':
			d = DirRight
		default:
			matched = false
		}
		if matched && d != s.Dir.opposite() {
			s.NextDir = d
		}
	}
}

// snakeStep advances the snake one cell. Runs in FixedUpdate.
func snakeStep(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}

	s := app.GetResource[Snake](c)
	score := app.GetResource[Score](c)

	s.Dir = s.NextDir

	headPos := s.Positions[0]
	dx, dy := s.Dir.delta()
	newHead := tui.Position{X: headPos.X + dx, Y: headPos.Y + dy}

	// Wall collision
	if newHead.X < 0 || newHead.X >= boardW ||
		newHead.Y < hudRows || newHead.Y >= hudRows+boardH {
		app.GetState[GameMode](c).Set(GameOver)
		return
	}
	// Self collision (the tail moves out of the way unless growing)
	tailIdx := len(s.Positions) - 1
	for i, p := range s.Positions {
		if !s.Grow && i == tailIdx {
			continue
		}
		if p == newHead {
			app.GetState[GameMode](c).Set(GameOver)
			return
		}
	}

	// Eat food?
	var ateFood bool
	var foodEnt ecs.Entity
	app.Query2[tui.Position, Food](c, func(e ecs.Entity, p *tui.Position, _ *Food) {
		if *p == newHead {
			ateFood = true
			foodEnt = e
		}
	})
	if ateFood {
		c.World().RemoveEntity(foodEnt)
		score.Value++
		s.Grow = true
		spawnFood(c)
		// Fine from FixedUpdate in a single-player game; networked games
		// should trigger audio from Update instead (see docs/network.md).
		app.GetResource[audio.Audio](c).Play("eat")
	}

	// Demote previous head's glyph to body style.
	if len(s.Segments) > 0 {
		gMap := generic.NewMap1[tui.Glyph](c.World())
		g := gMap.Get(s.Segments[0])
		g.Char = 'o'
		g.Style = styleBody
	}

	// Spawn new head entity.
	hMap := generic.NewMap2[tui.Position, tui.Glyph](c.World())
	headEnt := hMap.NewWith(
		&tui.Position{X: newHead.X, Y: newHead.Y},
		&tui.Glyph{Char: '@', Style: styleHead},
	)

	// Prepend.
	s.Segments = append([]ecs.Entity{headEnt}, s.Segments...)
	s.Positions = append([]tui.Position{newHead}, s.Positions...)

	if !s.Grow {
		tailE := s.Segments[len(s.Segments)-1]
		s.Segments = s.Segments[:len(s.Segments)-1]
		s.Positions = s.Positions[:len(s.Positions)-1]
		c.World().RemoveEntity(tailE)
	}
	s.Grow = false
}

// renderHUD draws score + game-over overlay. Registered via tui.AddOverlay,
// which handles the ordering against the entity render.
func renderHUD(c *app.Ctx) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	score := app.GetResource[Score](c).Value
	drawText(s, 1, 0, styleHUD, fmt.Sprintf(" SPLITI SNAKE   score: %d   q: quit ", score))

	if app.GetState[GameMode](c).Get() == GameOver {
		msg := "  GAME OVER  "
		hint := "  press 'r' to restart, 'q' to quit  "
		w, _ := s.Size()
		drawText(s, (w-len(msg))/2, hudRows+boardH/2, styleOver, msg)
		drawText(s, (w-len(hint))/2, hudRows+boardH/2+1, styleOver, hint)
	}
	// No s.Show() — the tui plugin's present system flushes once per frame.
}

func drawText(s tcell.Screen, x, y int, style tcell.Style, text string) {
	for i, r := range text {
		s.SetContent(x+i, y, r, nil, style)
	}
}

// --- Wiring --------------------------------------------------------------

func main() {
	a := app.New()
	a.AddPlugins(defaultplugins.Plugins{
		Time: splititime.Plugin{
			FixedTimestep:   100 * gotime.Millisecond,
			TargetFrameRate: 60,
		},
	})
	a.AddPlugins(audio.Plugin{})

	app.InsertResource(a, &Snake{})
	app.InsertResource(a, &Score{})
	app.InitState(a, Playing)

	a.AddSystems(schedule.Startup, loadSounds)

	app.OnEnter(a, Playing, setupGame)
	app.OnExit(a, Playing, teardownGame)
	app.OnEnter(a, Playing, func(c *app.Ctx) {
		au := app.GetResource[audio.Audio](c)
		au.PlayMusic("music", audio.MusicOptions{Volume: 0.4, FadeIn: 300 * gotime.Millisecond})
	})
	app.OnEnter(a, GameOver, func(c *app.Ctx) {
		au := app.GetResource[audio.Audio](c)
		au.Play("gameover")
		au.StopMusic(500 * gotime.Millisecond)
	})

	a.AddSystems(schedule.Update, handleInput)
	a.AddSystems(schedule.FixedUpdate, snakeStep)
	tui.AddOverlay(a, renderHUD)

	a.Run()
}

// --- Sounds ----------------------------------------------------------------
//
// All three sounds are synthesized here and registered via LoadPCM, so the
// example ships no binary assets. Real games more typically embed WAV/OGG
// files and use Registry.Load / LoadFS / LoadStream.

const sndRate = 48000

func loadSounds(c *app.Ctx) {
	reg := app.GetResource[audio.Registry](c)
	for ref, pcm := range map[string][]float32{
		"eat":      blip(880, 90*gotime.Millisecond),
		"gameover": sweep(440, 110, 500*gotime.Millisecond),
		"music":    chiptuneLoop(),
	} {
		if err := reg.LoadPCM(ref, pcm, 1, sndRate); err != nil {
			panic(err)
		}
	}
}

// blip is a short sine burst with an exponential decay — the classic coin.
func blip(freq float64, dur gotime.Duration) []float32 {
	n := int(dur.Seconds() * sndRate)
	pcm := make([]float32, n)
	for i := range pcm {
		t := float64(i) / sndRate
		env := math.Exp(-6 * t / dur.Seconds())
		pcm[i] = float32(0.5 * env * math.Sin(2*math.Pi*freq*t))
	}
	return pcm
}

// sweep glides a sine from one pitch down to another while fading out — the
// sad trombone of game over.
func sweep(from, to float64, dur gotime.Duration) []float32 {
	n := int(dur.Seconds() * sndRate)
	pcm := make([]float32, n)
	phase := 0.0
	for i := range pcm {
		frac := float64(i) / float64(n)
		freq := from * math.Pow(to/from, frac)
		phase += 2 * math.Pi * freq / sndRate
		pcm[i] = float32(0.5 * (1 - frac) * math.Sin(phase))
	}
	return pcm
}

// chiptuneLoop renders a couple of bars of square-wave arpeggio sized to loop
// seamlessly (PlayMusic loops by default).
func chiptuneLoop() []float32 {
	notes := []int{57, 60, 64, 69, 67, 64, 60, 62, 57, 60, 64, 69, 72, 69, 64, 60} // A-minor arpeggio, MIDI
	const noteDur = 0.15
	perNote := int(noteDur * sndRate)
	pcm := make([]float32, len(notes)*perNote)
	for ni, midi := range notes {
		freq := 440 * math.Pow(2, float64(midi-69)/12)
		for i := 0; i < perNote; i++ {
			t := float64(i) / sndRate
			// Short attack/release inside each note so the loop and the
			// note boundaries never click.
			env := math.Min(1, t/0.005) * math.Min(1, (noteDur-t)/0.01) * math.Exp(-2*t/noteDur)
			sq := 1.0
			if math.Mod(freq*t, 1) > 0.25 { // 25% duty square
				sq = -1
			}
			pcm[ni*perNote+i] = float32(0.22 * env * sq)
		}
	}
	return pcm
}
