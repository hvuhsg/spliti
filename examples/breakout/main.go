// Breakout — paddle, ball, brick wall.
//
// Controls:
//   left / right (or a / d):  move the paddle
//   space:                     launch the ball after a life lost
//   r:                         restart on game over / win
//   q or ctrl-c:               quit
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
	boardW    = 64
	boardH    = 22
	hudRows   = 1
	paddleW   = 8
	bricksW   = 14
	bricksH   = 6
	brickPx   = 4 // chars per brick horizontally
	startLives = 3
	holdTicks = 4
)

var (
	brickX0 = (boardW - bricksW*brickPx) / 2
	brickY0 = hudRows + 1
)

// --- Resources -----------------------------------------------------------

type Ball struct {
	Entity ecs.Entity
	X, Y   int
	VX, VY int
	Stuck  bool // sits on the paddle waiting for space
}

type Paddle struct {
	X, Y     int // top-left
	Entities [paddleW]ecs.Entity
}

type Bricks struct {
	Alive [bricksH][bricksW]bool
	Ents  [bricksH][bricksW][brickPx]ecs.Entity
	Left  int
}

type Hold struct {
	Left, Right int
}

type Stats struct {
	Score, Lives, Level int
}

type GameMode int

const (
	Playing GameMode = iota
	GameOver
	Won
)

// --- Markers -------------------------------------------------------------

type wall struct{}

// --- Styles --------------------------------------------------------------

var brickStyles = [bricksH]tcell.Style{
	tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true),
	tcell.StyleDefault.Foreground(tcell.ColorOrange).Bold(true),
	tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true),
	tcell.StyleDefault.Foreground(tcell.ColorGreen).Bold(true),
	tcell.StyleDefault.Foreground(tcell.ColorAqua).Bold(true),
	tcell.StyleDefault.Foreground(tcell.ColorPurple).Bold(true),
}

var brickPoints = [bricksH]int{50, 40, 30, 20, 10, 5}

var (
	stylePaddle = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	styleBall   = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	styleHUD    = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	styleWall   = tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	styleOver   = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	styleWon    = tcell.StyleDefault.Foreground(tcell.ColorLightGreen).Bold(true)
)

// --- Setup / teardown ----------------------------------------------------

func setupGame(c *app.Ctx) {
	stats := app.GetResource[Stats](c)
	*stats = Stats{Lives: startLives, Level: 1}
	*app.GetResource[Hold](c) = Hold{}

	// Side walls so the player has a visible play area.
	wallMap := generic.NewMap3[tui.Position, tui.Glyph, wall](c.World())
	for y := hudRows; y < hudRows+boardH; y++ {
		wallMap.NewWith(&tui.Position{X: 0, Y: y}, &tui.Glyph{Char: '│', Style: styleWall}, &wall{})
		wallMap.NewWith(&tui.Position{X: boardW - 1, Y: y}, &tui.Glyph{Char: '│', Style: styleWall}, &wall{})
	}
	for x := 0; x < boardW; x++ {
		wallMap.NewWith(&tui.Position{X: x, Y: hudRows}, &tui.Glyph{Char: '─', Style: styleWall}, &wall{})
	}

	// Paddle.
	paddle := app.GetResource[Paddle](c)
	paddle.Y = hudRows + boardH - 2
	paddle.X = boardW/2 - paddleW/2
	pMap := generic.NewMap2[tui.Position, tui.Glyph](c.World())
	for i := 0; i < paddleW; i++ {
		paddle.Entities[i] = pMap.NewWith(
			&tui.Position{X: paddle.X + i, Y: paddle.Y},
			&tui.Glyph{Char: '═', Style: stylePaddle},
		)
	}

	// Bricks.
	bricks := app.GetResource[Bricks](c)
	bricks.Left = 0
	bMap := generic.NewMap2[tui.Position, tui.Glyph](c.World())
	for r := 0; r < bricksH; r++ {
		for cIdx := 0; cIdx < bricksW; cIdx++ {
			bricks.Alive[r][cIdx] = true
			bricks.Left++
			y := brickY0 + r
			for k := 0; k < brickPx; k++ {
				ch := '█'
				if k == brickPx-1 {
					ch = ' ' // tiny gap so individual bricks read as separate
				}
				bricks.Ents[r][cIdx][k] = bMap.NewWith(
					&tui.Position{X: brickX0 + cIdx*brickPx + k, Y: y},
					&tui.Glyph{Char: ch, Style: brickStyles[r]},
				)
			}
		}
	}

	// Ball — sits on the paddle until launch.
	ball := app.GetResource[Ball](c)
	ball.X = paddle.X + paddleW/2
	ball.Y = paddle.Y - 1
	ball.VX, ball.VY = 1, -1
	ball.Stuck = true
	ball.Entity = pMap.NewWith(
		&tui.Position{X: ball.X, Y: ball.Y},
		&tui.Glyph{Char: '●', Style: styleBall},
	)
}

func teardownGame(c *app.Ctx) {
	paddle := app.GetResource[Paddle](c)
	for _, e := range paddle.Entities {
		if c.World().Alive(e) {
			c.World().RemoveEntity(e)
		}
	}
	paddle.Entities = [paddleW]ecs.Entity{}

	bricks := app.GetResource[Bricks](c)
	for r := 0; r < bricksH; r++ {
		for cIdx := 0; cIdx < bricksW; cIdx++ {
			if !bricks.Alive[r][cIdx] {
				continue
			}
			for _, e := range bricks.Ents[r][cIdx] {
				if c.World().Alive(e) {
					c.World().RemoveEntity(e)
				}
			}
			bricks.Alive[r][cIdx] = false
		}
	}

	ball := app.GetResource[Ball](c)
	if c.World().Alive(ball.Entity) {
		c.World().RemoveEntity(ball.Entity)
	}

	var walls []ecs.Entity
	app.Query1[wall](c, func(e ecs.Entity, _ *wall) {
		walls = append(walls, e)
	})
	for _, e := range walls {
		c.World().RemoveEntity(e)
	}
}

// --- Systems -------------------------------------------------------------

func handleInput(c *app.Ctx) {
	state := app.GetState[GameMode](c)
	hold := app.GetResource[Hold](c)
	for _, ev := range app.ReadEvents[input.KeyEvent](c) {
		if ev.Key == tcell.KeyCtrlC || ev.Key == tcell.KeyEscape || ev.Rune == 'q' {
			c.App().Stop()
			return
		}
		if state.Get() == GameOver || state.Get() == Won {
			if ev.Rune == 'r' {
				state.Set(Playing)
			}
			continue
		}
		switch {
		case ev.Key == tcell.KeyLeft, ev.Rune == 'a', ev.Rune == 'A':
			hold.Left = holdTicks
		case ev.Key == tcell.KeyRight, ev.Rune == 'd', ev.Rune == 'D':
			hold.Right = holdTicks
		case ev.Rune == ' ':
			ball := app.GetResource[Ball](c)
			if ball.Stuck {
				ball.Stuck = false
				ball.VY = -1
				ball.VX = 1
				if rand.Intn(2) == 0 {
					ball.VX = -1
				}
			}
		}
	}
}

func step(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}
	movePaddle(c)
	moveBall(c)
}

func movePaddle(c *app.Ctx) {
	paddle := app.GetResource[Paddle](c)
	hold := app.GetResource[Hold](c)
	dx := 0
	if hold.Left > 0 {
		hold.Left--
		dx -= 2
	}
	if hold.Right > 0 {
		hold.Right--
		dx += 2
	}
	if dx == 0 {
		return
	}
	newX := paddle.X + dx
	if newX < 1 {
		newX = 1
	}
	if newX+paddleW > boardW-1 {
		newX = boardW - 1 - paddleW
	}
	if newX == paddle.X {
		return
	}
	paddle.X = newX
	gMap := generic.NewMap1[tui.Position](c.World())
	for i, e := range paddle.Entities {
		pos := gMap.Get(e)
		pos.X = paddle.X + i
	}

	// If the ball is stuck, it rides along.
	ball := app.GetResource[Ball](c)
	if ball.Stuck {
		ball.X = paddle.X + paddleW/2
		bp := gMap.Get(ball.Entity)
		bp.X = ball.X
	}
}

func moveBall(c *app.Ctx) {
	ball := app.GetResource[Ball](c)
	if ball.Stuck {
		return
	}
	stats := app.GetResource[Stats](c)
	paddle := app.GetResource[Paddle](c)
	bricks := app.GetResource[Bricks](c)

	// Two X-steps per Y-step to compensate for terminal cell aspect.
	for sub := 0; sub < 2; sub++ {
		dy := 0
		if sub == 0 {
			dy = ball.VY
		}
		stepBallOnce(c, ball, paddle, bricks, stats, ball.VX, dy)
		if ball.Stuck {
			break
		}
	}

	gMap := generic.NewMap1[tui.Position](c.World())
	pos := gMap.Get(ball.Entity)
	pos.X = ball.X
	pos.Y = ball.Y

	if bricks.Left == 0 {
		app.GetState[GameMode](c).Set(Won)
	}
	if stats.Lives <= 0 {
		app.GetState[GameMode](c).Set(GameOver)
	}
}

func stepBallOnce(c *app.Ctx, ball *Ball, paddle *Paddle, bricks *Bricks, stats *Stats, dx, dy int) {
	nx := ball.X + dx
	ny := ball.Y + dy

	// Side walls.
	if nx <= 0 {
		nx = 1
		ball.VX = -ball.VX
	}
	if nx >= boardW-1 {
		nx = boardW - 2
		ball.VX = -ball.VX
	}

	// Top wall.
	if ny <= hudRows {
		ny = hudRows + 1
		ball.VY = -ball.VY
	}

	// Bottom — life lost.
	if ny >= hudRows+boardH {
		stats.Lives--
		// Re-stick the ball on the paddle.
		ball.X = paddle.X + paddleW/2
		ball.Y = paddle.Y - 1
		ball.Stuck = true
		ball.VX, ball.VY = 1, -1
		return
	}

	// Brick collision.
	if ny >= brickY0 && ny < brickY0+bricksH {
		r := ny - brickY0
		col := (nx - brickX0) / brickPx
		if col >= 0 && col < bricksW && bricks.Alive[r][col] {
			// Despawn the brick segments.
			for _, e := range bricks.Ents[r][col] {
				if c.World().Alive(e) {
					c.World().RemoveEntity(e)
				}
			}
			bricks.Alive[r][col] = false
			bricks.Left--
			stats.Score += brickPoints[r]
			ball.VY = -ball.VY
			ny = ball.Y // bounce back to where we came from
		}
	}

	// Paddle collision.
	if ny == paddle.Y && nx >= paddle.X && nx < paddle.X+paddleW {
		ball.VY = -1
		// English: leftmost cells push left, rightmost push right.
		rel := nx - paddle.X
		switch {
		case rel < paddleW/3:
			ball.VX = -1
		case rel >= paddleW-paddleW/3:
			ball.VX = 1
		}
		ny = paddle.Y - 1
	}

	ball.X = nx
	ball.Y = ny
}

func renderHUD(c *app.Ctx) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	stats := app.GetResource[Stats](c)
	header := fmt.Sprintf(" SPLITI BREAKOUT   score: %d   lives: %d   q: quit ",
		stats.Score, stats.Lives)
	drawText(s, 1, 0, styleHUD, header)

	switch app.GetState[GameMode](c).Get() {
	case GameOver:
		showCenter(s, "  GAME OVER  ", "  press 'r' to restart, 'q' to quit  ", styleOver)
	case Won:
		showCenter(s, "  YOU WIN!  ", "  press 'r' to play again, 'q' to quit  ", styleWon)
	}

	// Hint when ball is stuck.
	if app.GetState[GameMode](c).Get() == Playing && app.GetResource[Ball](c).Stuck {
		drawText(s, boardW/2-12, hudRows+boardH-4, styleHUD, " press SPACE to launch ")
	}
}

func showCenter(s tcell.Screen, msg, hint string, style tcell.Style) {
	w, _ := s.Size()
	drawText(s, (w-len(msg))/2, hudRows+boardH/2, style, msg)
	drawText(s, (w-len(hint))/2, hudRows+boardH/2+1, style, hint)
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
			FixedTimestep:   55 * gotime.Millisecond,
			TargetFrameRate: 60,
		},
	})

	app.InsertResource(a, &Ball{})
	app.InsertResource(a, &Paddle{})
	app.InsertResource(a, &Bricks{})
	app.InsertResource(a, &Hold{})
	app.InsertResource(a, &Stats{})
	app.InitState(a, Playing)

	app.OnEnter(a, Playing, setupGame)
	app.OnExit(a, Playing, teardownGame)

	a.AddSystems(schedule.Update, handleInput)
	a.AddSystems(schedule.FixedUpdate, step)
	tui.AddOverlay(a, renderHUD)

	a.Run()
}
