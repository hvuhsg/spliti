// Pong — two paddles, one ball, no mercy.
//
// Controls:
//   left paddle:  w / s
//   right paddle: arrow up / down (or i / k)
//   q or ctrl-c:  quit
//   r:            restart after game over
package main

import (
	"fmt"
	"math/rand"
	gotime "time"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/defaultplugins"
	"github.com/hvuhsg/spliti/plugin/input"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

const (
	boardW       = 60
	boardH       = 22
	hudRows      = 1
	paddleH      = 4
	winningScore = 7
	holdTicks    = 4 // how many fixed steps a key-press keeps a paddle moving
)

// --- Resources -----------------------------------------------------------

type Ball struct {
	Entity ecs.Entity
	// Position is tracked as ints; aspect ratio is faked by stepping X twice
	// per FixedUpdate while stepping Y once.
	X, Y   int
	VX, VY int
}

type Paddle struct {
	X, Y     int // X is fixed, Y is the row of the top of the paddle
	Entities []ecs.Entity
}

type Paddles struct {
	Left, Right Paddle
}

// PaddleHold tracks how many more fixed steps each direction-key keeps
// "pressed" — terminals don't send key-up events, so we refresh this
// counter every time the OS auto-repeats the key.
type PaddleHold struct {
	LeftUp, LeftDown   int
	RightUp, RightDown int
}

type Score struct{ Left, Right int }

type GameMode int

const (
	Playing GameMode = iota
	GameOver
)

// --- Styles --------------------------------------------------------------

var (
	stylePaddle = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	styleBall   = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	styleHUD    = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	styleNet    = tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	styleOver   = tcell.StyleDefault.Foreground(tcell.ColorLightGreen).Bold(true)
)

// Net is a marker for the dashed midline so we can clean it up cleanly.
type Net struct{}

// --- Setup / teardown ----------------------------------------------------

func setupGame(c *app.Ctx) {
	score := app.GetResource[Score](c)
	score.Left, score.Right = 0, 0

	paddles := app.GetResource[Paddles](c)
	*paddles = Paddles{
		Left:  Paddle{X: 2, Y: hudRows + boardH/2 - paddleH/2},
		Right: Paddle{X: boardW - 3, Y: hudRows + boardH/2 - paddleH/2},
	}
	mapper := generic.NewMap2[tui.Position, tui.Glyph](c.World())
	for side := 0; side < 2; side++ {
		p := &paddles.Left
		if side == 1 {
			p = &paddles.Right
		}
		for i := 0; i < paddleH; i++ {
			e := mapper.NewWith(
				&tui.Position{X: p.X, Y: p.Y + i},
				&tui.Glyph{Char: '█', Style: stylePaddle},
			)
			p.Entities = append(p.Entities, e)
		}
	}

	// Net: dashed line down the middle.
	netMapper := generic.NewMap3[tui.Position, tui.Glyph, Net](c.World())
	for y := hudRows; y < hudRows+boardH; y++ {
		if y%2 == 0 {
			netMapper.NewWith(
				&tui.Position{X: boardW / 2, Y: y},
				&tui.Glyph{Char: '│', Style: styleNet},
				&Net{},
			)
		}
	}

	// Ball.
	ball := app.GetResource[Ball](c)
	ball.X = boardW / 2
	ball.Y = hudRows + boardH/2
	ball.VX = 1
	if rand.Intn(2) == 0 {
		ball.VX = -1
	}
	ball.VY = 1
	if rand.Intn(2) == 0 {
		ball.VY = -1
	}
	ball.Entity = mapper.NewWith(
		&tui.Position{X: ball.X, Y: ball.Y},
		&tui.Glyph{Char: '●', Style: styleBall},
	)

	hold := app.GetResource[PaddleHold](c)
	*hold = PaddleHold{}
}

func teardownGame(c *app.Ctx) {
	paddles := app.GetResource[Paddles](c)
	for _, e := range paddles.Left.Entities {
		if c.World().Alive(e) {
			c.World().RemoveEntity(e)
		}
	}
	for _, e := range paddles.Right.Entities {
		if c.World().Alive(e) {
			c.World().RemoveEntity(e)
		}
	}
	paddles.Left.Entities = nil
	paddles.Right.Entities = nil

	ball := app.GetResource[Ball](c)
	if c.World().Alive(ball.Entity) {
		c.World().RemoveEntity(ball.Entity)
	}

	var nets []ecs.Entity
	app.Query1[Net](c, func(e ecs.Entity, _ *Net) {
		nets = append(nets, e)
	})
	for _, e := range nets {
		c.World().RemoveEntity(e)
	}
}

// --- Systems -------------------------------------------------------------

func handleInput(c *app.Ctx) {
	state := app.GetState[GameMode](c)
	hold := app.GetResource[PaddleHold](c)
	for _, ev := range app.ReadEvents[input.KeyEvent](c) {
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
		switch {
		case ev.Rune == 'w' || ev.Rune == 'W':
			hold.LeftUp = holdTicks
		case ev.Rune == 's' || ev.Rune == 'S':
			hold.LeftDown = holdTicks
		case ev.Key == tcell.KeyUp || ev.Rune == 'i' || ev.Rune == 'I':
			hold.RightUp = holdTicks
		case ev.Key == tcell.KeyDown || ev.Rune == 'k' || ev.Rune == 'K':
			hold.RightDown = holdTicks
		}
	}
}

func step(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}
	movePaddles(c)
	moveBall(c)
}

func movePaddles(c *app.Ctx) {
	paddles := app.GetResource[Paddles](c)
	hold := app.GetResource[PaddleHold](c)
	gMap := generic.NewMap1[tui.Position](c.World())

	apply := func(p *Paddle, dy int) {
		newY := p.Y + dy
		if newY < hudRows {
			newY = hudRows
		}
		if newY+paddleH > hudRows+boardH {
			newY = hudRows + boardH - paddleH
		}
		if newY == p.Y {
			return
		}
		p.Y = newY
		for i, e := range p.Entities {
			pos := gMap.Get(e)
			pos.Y = p.Y + i
		}
	}

	if hold.LeftUp > 0 {
		hold.LeftUp--
		apply(&paddles.Left, -1)
	}
	if hold.LeftDown > 0 {
		hold.LeftDown--
		apply(&paddles.Left, 1)
	}
	if hold.RightUp > 0 {
		hold.RightUp--
		apply(&paddles.Right, -1)
	}
	if hold.RightDown > 0 {
		hold.RightDown--
		apply(&paddles.Right, 1)
	}
}

func moveBall(c *app.Ctx) {
	ball := app.GetResource[Ball](c)
	paddles := app.GetResource[Paddles](c)
	score := app.GetResource[Score](c)

	// Step X twice and Y once to compensate for terminal cell aspect ratio.
	for sub := 0; sub < 2; sub++ {
		dy := 0
		if sub == 0 {
			dy = ball.VY
		}
		stepOnce(ball, paddles, score, ball.VX, dy)
	}

	// Push update to the entity.
	gMap := generic.NewMap1[tui.Position](c.World())
	pos := gMap.Get(ball.Entity)
	pos.X = ball.X
	pos.Y = ball.Y

	if score.Left >= winningScore || score.Right >= winningScore {
		app.GetState[GameMode](c).Set(GameOver)
	}
}

// stepOnce advances the ball by one cell on each axis (or zero), handling
// wall, paddle, and goal collisions inline.
func stepOnce(ball *Ball, paddles *Paddles, score *Score, dx, dy int) {
	nx := ball.X + dx
	ny := ball.Y + dy

	// Top / bottom wall.
	if ny < hudRows {
		ny = hudRows + (hudRows - ny)
		ball.VY = -ball.VY
	}
	if ny >= hudRows+boardH {
		ny = (hudRows + boardH - 1) - (ny - (hudRows + boardH - 1))
		ball.VY = -ball.VY
	}

	// Goals — left/right wall = point for the other side.
	if nx < 0 {
		score.Right++
		resetBall(ball, +1)
		return
	}
	if nx >= boardW {
		score.Left++
		resetBall(ball, -1)
		return
	}

	// Paddle hits — check the column the ball is entering.
	hit := func(p *Paddle) bool {
		if nx != p.X {
			return false
		}
		return ny >= p.Y && ny < p.Y+paddleH
	}
	if dx < 0 && hit(&paddles.Left) {
		nx = paddles.Left.X + 1
		ball.VX = 1
		// English: deflect based on where on the paddle the ball hit.
		ball.VY = paddleSpin(ny, paddles.Left.Y, ball.VY)
	} else if dx > 0 && hit(&paddles.Right) {
		nx = paddles.Right.X - 1
		ball.VX = -1
		ball.VY = paddleSpin(ny, paddles.Right.Y, ball.VY)
	}

	ball.X = nx
	ball.Y = ny
}

// paddleSpin keeps VY in {-1, +1}: top half deflects up, bottom half down,
// middle preserves the current direction. Trivial but enough to make rallies
// non-deterministic.
func paddleSpin(by, py, currentVY int) int {
	rel := by - py
	switch {
	case rel <= 0:
		return -1
	case rel >= paddleH-1:
		return 1
	}
	if currentVY == 0 {
		return 1
	}
	return currentVY
}

func resetBall(ball *Ball, towards int) {
	ball.X = boardW / 2
	ball.Y = hudRows + boardH/2
	ball.VX = towards
	ball.VY = 1
	if rand.Intn(2) == 0 {
		ball.VY = -1
	}
}

func renderHUD(c *app.Ctx) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	score := app.GetResource[Score](c)
	header := fmt.Sprintf(" SPLITI PONG    %d  :  %d    first to %d wins   q: quit ",
		score.Left, score.Right, winningScore)
	drawText(s, 1, 0, styleHUD, header)

	if app.GetState[GameMode](c).Get() == GameOver {
		var msg string
		if score.Left > score.Right {
			msg = "  LEFT WINS  "
		} else {
			msg = "  RIGHT WINS  "
		}
		hint := "  press 'r' to restart, 'q' to quit  "
		w, _ := s.Size()
		drawText(s, (w-len(msg))/2, hudRows+boardH/2, styleOver, msg)
		drawText(s, (w-len(hint))/2, hudRows+boardH/2+1, styleOver, hint)
	}
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
			FixedTimestep:   60 * gotime.Millisecond,
			TargetFrameRate: 60,
		},
	})

	app.InsertResource(a, &Ball{})
	app.InsertResource(a, &Paddles{})
	app.InsertResource(a, &PaddleHold{})
	app.InsertResource(a, &Score{})
	app.InitState(a, Playing)

	app.OnEnter(a, Playing, setupGame)
	app.OnExit(a, Playing, teardownGame)

	a.AddSystems(schedule.Update, handleInput)
	a.AddSystems(schedule.FixedUpdate, step)
	tui.AddOverlay(a, renderHUD)

	a.Run()
}
