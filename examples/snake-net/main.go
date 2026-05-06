// snake-net — two-player networked Snake using the spliti network plugin.
//
// Run two terminals on one machine:
//
//	go run ./examples/snake-net -mode=host   -addr=:7777
//	go run ./examples/snake-net -mode=client -addr=localhost:7777
//
// Each peer steers its own snake. Player 0 is the host (green '@' / 'o'),
// player 1 is the client (cyan '#' / 'x'). Eat '*' to grow. Game over on
// any wall hit, body hit, or head-on-head collision. Press 'r' to restart,
// 'q' / Esc / Ctrl-C to quit.
package main

import (
	"flag"
	"fmt"
	"os"
	gotime "time"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/defaultplugins"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/network"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

const (
	boardW  = 50
	boardH  = 20
	hudRows = 1
)

// --- Components ---------------------------------------------------------

type Food struct{}

// --- Direction helpers --------------------------------------------------

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

func (d Dir) opposite() Dir { return [...]Dir{DirLeft, DirRight, DirDown, DirUp}[d] }

// --- Snake (per player) -------------------------------------------------

type Snake struct {
	Owner     network.PlayerID
	Segments  []ecs.Entity
	Positions []tui.Position
	Dir       Dir
	NextDir   Dir
	Grow      bool
	Alive     bool
	HeadGlyph tui.Glyph
	BodyGlyph tui.Glyph
}

// --- Resources ----------------------------------------------------------

type Snakes struct {
	Items []*Snake
}

func (s *Snakes) ByOwner(id network.PlayerID) *Snake {
	for _, sn := range s.Items {
		if sn != nil && sn.Owner == id {
			return sn
		}
	}
	return nil
}

type Scores struct {
	Values []int
}

// --- States -------------------------------------------------------------

type GameMode int

const (
	Playing GameMode = iota
	GameOver
)

// --- Glyph styles -------------------------------------------------------

var (
	p0Head = tui.Glyph{Char: '@', Style: tcell.StyleDefault.Foreground(tcell.ColorLightGreen).Bold(true)}
	p0Body = tui.Glyph{Char: 'o', Style: tcell.StyleDefault.Foreground(tcell.ColorGreen)}
	p1Head = tui.Glyph{Char: '#', Style: tcell.StyleDefault.Foreground(tcell.ColorLightCyan).Bold(true)}
	p1Body = tui.Glyph{Char: 'x', Style: tcell.StyleDefault.Foreground(tcell.ColorBlue)}

	foodStyle = tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	hudStyle  = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	overStyle = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	netStyle  = tcell.StyleDefault.Foreground(tcell.ColorOrange).Bold(true)
)

func glyphsForPlayer(id network.PlayerID) (head, body tui.Glyph) {
	if id == 0 {
		return p0Head, p0Body
	}
	return p1Head, p1Body
}

// --- Setup / teardown ---------------------------------------------------

func setupGame(c *app.Ctx) {
	snakes := app.GetResource[Snakes](c)
	scores := app.GetResource[Scores](c)

	total := int(app.GetResource[network.LocalPlayer](c).Total)
	if total < 2 {
		total = 2
	}

	scores.Values = make([]int, total)
	snakes.Items = make([]*Snake, total)

	cy := hudRows + boardH/2
	mapper := generic.NewMap2[tui.Position, tui.Glyph](c.World())

	for pid := 0; pid < total; pid++ {
		head, body := glyphsForPlayer(network.PlayerID(pid))
		var startX, deltaX int
		var dir Dir
		if pid%2 == 0 {
			startX, deltaX, dir = 8, -1, DirRight
		} else {
			startX, deltaX, dir = boardW-9, 1, DirLeft
		}
		sn := &Snake{
			Owner:     network.PlayerID(pid),
			HeadGlyph: head,
			BodyGlyph: body,
			Dir:       dir,
			NextDir:   dir,
			Alive:     true,
		}
		for i := 0; i < 4; i++ {
			p := tui.Position{X: startX + i*deltaX, Y: cy}
			g := body
			if i == 0 {
				g = head
			}
			e := mapper.NewWith(&tui.Position{X: p.X, Y: p.Y}, &tui.Glyph{Char: g.Char, Style: g.Style})
			sn.Segments = append(sn.Segments, e)
			sn.Positions = append(sn.Positions, p)
		}
		snakes.Items[pid] = sn
	}
	spawnFood(c)
}

func teardownGame(c *app.Ctx) {
	snakes := app.GetResource[Snakes](c)
	for _, sn := range snakes.Items {
		if sn == nil {
			continue
		}
		for _, e := range sn.Segments {
			if c.World().Alive(e) {
				c.World().RemoveEntity(e)
			}
		}
	}
	var foodEnts []ecs.Entity
	app.Query1[Food](c, func(e ecs.Entity, _ *Food) {
		foodEnts = append(foodEnts, e)
	})
	for _, e := range foodEnts {
		c.World().RemoveEntity(e)
	}
	snakes.Items = nil
}

func spawnFood(c *app.Ctx) {
	r := app.GetResource[network.Random](c)
	occupied := map[tui.Position]bool{}
	app.Query1[tui.Position](c, func(_ ecs.Entity, p *tui.Position) {
		occupied[*p] = true
	})
	mapper := generic.NewMap3[tui.Position, tui.Glyph, Food](c.World())
	for tries := 0; tries < 200; tries++ {
		p := tui.Position{
			X: r.Intn(boardW),
			Y: hudRows + r.Intn(boardH),
		}
		if occupied[p] {
			continue
		}
		mapper.NewWith(
			&tui.Position{X: p.X, Y: p.Y},
			&tui.Glyph{Char: '*', Style: foodStyle},
			&Food{},
		)
		return
	}
}

// --- Systems ------------------------------------------------------------

// Local quit handling — never networked. Each peer can quit independently.
func handleLocalQuit(c *app.Ctx) {
	for _, ev := range app.ReadEvents[input.KeyEvent](c) {
		if ev.Key == tcell.KeyCtrlC || ev.Key == tcell.KeyEscape || ev.Rune == 'q' {
			c.App().Stop()
			return
		}
	}
}

// Steering + restart — runs in FixedUpdate, consumes deterministic
// network.PlayerKey events so both peers stay in sync.
func handleNetworkedInput(c *app.Ctx) {
	state := app.GetState[GameMode](c)
	snakes := app.GetResource[Snakes](c)

	for _, ev := range app.ReadEvents[network.PlayerKey](c) {
		if ev.Rune == 'r' && state.Get() == GameOver {
			state.Set(Playing)
			return
		}
		if state.Get() != Playing {
			continue
		}
		sn := snakes.ByOwner(ev.Player)
		if sn == nil || !sn.Alive {
			continue
		}
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
		if matched && d != sn.Dir.opposite() {
			sn.NextDir = d
		}
	}
}

// snakeStep — FixedUpdate. Advances every alive snake one cell.
func snakeStep(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}
	snakes := app.GetResource[Snakes](c)
	scores := app.GetResource[Scores](c)

	// Compute new heads.
	newHeads := make(map[network.PlayerID]tui.Position)
	for _, sn := range snakes.Items {
		if sn == nil || !sn.Alive {
			continue
		}
		sn.Dir = sn.NextDir
		dx, dy := sn.Dir.delta()
		h := sn.Positions[0]
		newHeads[sn.Owner] = tui.Position{X: h.X + dx, Y: h.Y + dy}
	}

	died := map[network.PlayerID]bool{}

	// Wall.
	for pid, h := range newHeads {
		if h.X < 0 || h.X >= boardW || h.Y < hudRows || h.Y >= hudRows+boardH {
			died[pid] = true
		}
	}
	// Body collision (own + others).
	for pid, h := range newHeads {
		if died[pid] {
			continue
		}
		for _, sn := range snakes.Items {
			if sn == nil || !sn.Alive {
				continue
			}
			isOwn := sn.Owner == pid
			tail := len(sn.Positions) - 1
			for i, p := range sn.Positions {
				if isOwn && !sn.Grow && i == tail {
					continue
				}
				if p == h {
					died[pid] = true
					break
				}
			}
			if died[pid] {
				break
			}
		}
	}
	// Head-on-head: same target cell from multiple snakes.
	headTargets := map[tui.Position][]network.PlayerID{}
	for pid, h := range newHeads {
		if died[pid] {
			continue
		}
		headTargets[h] = append(headTargets[h], pid)
	}
	for _, ids := range headTargets {
		if len(ids) > 1 {
			for _, pid := range ids {
				died[pid] = true
			}
		}
	}

	if len(died) > 0 {
		for pid := range died {
			if sn := snakes.ByOwner(pid); sn != nil {
				sn.Alive = false
			}
		}
		app.GetState[GameMode](c).Set(GameOver)
		return
	}

	// Eating — iterate snakes in deterministic owner order.
	for pid := 0; pid < len(snakes.Items); pid++ {
		sn := snakes.Items[pid]
		if sn == nil || !sn.Alive {
			continue
		}
		h := newHeads[sn.Owner]
		var foodEnt ecs.Entity
		ate := false
		app.Query2[tui.Position, Food](c, func(e ecs.Entity, p *tui.Position, _ *Food) {
			if !ate && *p == h {
				ate = true
				foodEnt = e
			}
		})
		if ate {
			c.World().RemoveEntity(foodEnt)
			scores.Values[pid]++
			sn.Grow = true
			spawnFood(c)
		}
	}

	// Move.
	for _, sn := range snakes.Items {
		if sn == nil || !sn.Alive {
			continue
		}
		h := newHeads[sn.Owner]

		if len(sn.Segments) > 0 {
			gMap := generic.NewMap1[tui.Glyph](c.World())
			g := gMap.Get(sn.Segments[0])
			*g = sn.BodyGlyph
		}

		mapper := generic.NewMap2[tui.Position, tui.Glyph](c.World())
		newE := mapper.NewWith(
			&tui.Position{X: h.X, Y: h.Y},
			&tui.Glyph{Char: sn.HeadGlyph.Char, Style: sn.HeadGlyph.Style},
		)
		sn.Segments = append([]ecs.Entity{newE}, sn.Segments...)
		sn.Positions = append([]tui.Position{h}, sn.Positions...)

		if !sn.Grow {
			tail := sn.Segments[len(sn.Segments)-1]
			sn.Segments = sn.Segments[:len(sn.Segments)-1]
			sn.Positions = sn.Positions[:len(sn.Positions)-1]
			c.World().RemoveEntity(tail)
		}
		sn.Grow = false
	}
}

// HUD overlay.
func renderHUD(c *app.Ctx) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	scores := app.GetResource[Scores](c)
	self := app.GetResource[network.LocalPlayer](c)
	netStat := app.GetResource[network.Status](c)

	var hud string
	if len(scores.Values) >= 2 {
		hud = fmt.Sprintf(" P0 (you=%v): %d   P1: %d   q: quit ", self.ID == 0, scores.Values[0], scores.Values[1])
	} else {
		hud = " SPLITI SNAKE-NET — connecting "
	}
	drawText(s, 1, 0, hudStyle, hud)

	if app.GetState[GameMode](c).Get() == GameOver {
		msg := "  GAME OVER  "
		hint := "  press 'r' to restart, 'q' to quit  "
		w, _ := s.Size()
		drawText(s, (w-len(msg))/2, hudRows+boardH/2, overStyle, msg)
		drawText(s, (w-len(hint))/2, hudRows+boardH/2+1, overStyle, hint)
	}

	switch netStat.Phase {
	case network.Stalled:
		drawText(s, 1, hudRows+boardH+1, netStyle, " . network stall — waiting for opponent ")
	case network.PeerLost:
		drawText(s, 1, hudRows+boardH+1, netStyle, fmt.Sprintf(" x  player %d left ", netStat.LostPeer))
	}
	// No s.Show() — the tui plugin's present system flushes once per frame.
}

func drawText(s tcell.Screen, x, y int, style tcell.Style, text string) {
	for i, r := range text {
		s.SetContent(x+i, y, r, nil, style)
	}
}

// --- Main ---------------------------------------------------------------

func main() {
	mode := flag.String("mode", "host", "host or client")
	addr := flag.String("addr", ":7777", "listen address (host) or remote address (client)")
	players := flag.Int("players", 2, "expected total players (host only)")
	flag.Parse()

	var nm network.Mode
	var listen, connect string
	switch *mode {
	case "host":
		nm = network.Host
		listen = *addr
	case "client":
		nm = network.Client
		connect = *addr
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q (want host or client)\n", *mode)
		flag.Usage()
		os.Exit(2)
	}

	a := app.New()
	a.AddPlugins(defaultplugins.Plugins{
		Time: splititime.Plugin{
			FixedTimestep:   100 * gotime.Millisecond,
			TargetFrameRate: 60,
		},
	})
	a.AddPlugins(network.Plugin{
		Mode:    nm,
		Listen:  listen,
		Connect: connect,
		Players: *players,
	})

	app.InsertResource(a, &Snakes{})
	app.InsertResource(a, &Scores{})
	app.InitState(a, Playing)

	app.OnEnter(a, Playing, setupGame)
	app.OnExit(a, Playing, teardownGame)

	a.AddSystems(schedule.Update, handleLocalQuit)
	a.AddSystems(schedule.FixedUpdate, app.Chain(
		app.System(handleNetworkedInput).Label("input"),
		app.System(snakeStep).Label("step"),
	))
	tui.AddOverlay(a, renderHUD)

	a.Run()
}
