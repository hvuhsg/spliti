// Tetris — the seven tetrominoes, line clears, levels.
//
// Controls:
//   left/right or a/d:  shift
//   down or s:          soft drop
//   up or w:            rotate clockwise
//   space:              hard drop
//   p:                  pause
//   r:                  restart on game over
//   q or ctrl-c:        quit
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
	cellsW  = 10
	cellsH  = 20
	cellPx  = 2 // each playfield cell renders as cellPx screen columns
	hudRows = 1
	boardX0 = 4
	boardY0 = hudRows + 1
)

// cellScreenX returns the leftmost screen column for cell column cx.
func cellScreenX(cx int) int { return boardX0 + cx*cellPx }

// --- Pieces --------------------------------------------------------------

// Each piece is 4 rotations × 4 blocks × (col, row) inside a 4×4 bounding box.
type rot = [4][2]int
type piece [4]rot

var pieces = [7]piece{
	// I
	{
		{{0, 1}, {1, 1}, {2, 1}, {3, 1}},
		{{2, 0}, {2, 1}, {2, 2}, {2, 3}},
		{{0, 2}, {1, 2}, {2, 2}, {3, 2}},
		{{1, 0}, {1, 1}, {1, 2}, {1, 3}},
	},
	// O
	{
		{{1, 0}, {2, 0}, {1, 1}, {2, 1}},
		{{1, 0}, {2, 0}, {1, 1}, {2, 1}},
		{{1, 0}, {2, 0}, {1, 1}, {2, 1}},
		{{1, 0}, {2, 0}, {1, 1}, {2, 1}},
	},
	// T
	{
		{{1, 0}, {0, 1}, {1, 1}, {2, 1}},
		{{1, 0}, {1, 1}, {2, 1}, {1, 2}},
		{{0, 1}, {1, 1}, {2, 1}, {1, 2}},
		{{1, 0}, {0, 1}, {1, 1}, {1, 2}},
	},
	// S
	{
		{{1, 0}, {2, 0}, {0, 1}, {1, 1}},
		{{1, 0}, {1, 1}, {2, 1}, {2, 2}},
		{{1, 1}, {2, 1}, {0, 2}, {1, 2}},
		{{0, 0}, {0, 1}, {1, 1}, {1, 2}},
	},
	// Z
	{
		{{0, 0}, {1, 0}, {1, 1}, {2, 1}},
		{{2, 0}, {1, 1}, {2, 1}, {1, 2}},
		{{0, 1}, {1, 1}, {1, 2}, {2, 2}},
		{{1, 0}, {0, 1}, {1, 1}, {0, 2}},
	},
	// L
	{
		{{2, 0}, {0, 1}, {1, 1}, {2, 1}},
		{{1, 0}, {1, 1}, {1, 2}, {2, 2}},
		{{0, 1}, {1, 1}, {2, 1}, {0, 2}},
		{{0, 0}, {1, 0}, {1, 1}, {1, 2}},
	},
	// J
	{
		{{0, 0}, {0, 1}, {1, 1}, {2, 1}},
		{{1, 0}, {2, 0}, {1, 1}, {1, 2}},
		{{0, 1}, {1, 1}, {2, 1}, {2, 2}},
		{{1, 0}, {1, 1}, {0, 2}, {1, 2}},
	},
}

var pieceColors = [7]tcell.Color{
	tcell.ColorAqua,    // I
	tcell.ColorYellow,  // O
	tcell.ColorPurple,  // T
	tcell.ColorGreen,   // S
	tcell.ColorRed,     // Z
	tcell.ColorOrange,  // L
	tcell.ColorBlue,    // J
}

// --- Resources -----------------------------------------------------------

type Field struct {
	Filled [cellsH][cellsW]bool
	Color  [cellsH][cellsW]tcell.Color
	Ents   [cellsH][cellsW][cellPx]ecs.Entity
}

type Falling struct {
	Kind   int
	Rot    int
	X, Y   int // top-left of 4×4 box in cell coords
	Blocks [4][cellPx]ecs.Entity
}

type NextPiece struct{ Kind int }

type Score struct {
	Value int
	Lines int
	Level int
}

type Drop struct {
	Tick     int // counter towards next forced drop
	EveryFix int // forced-drop period in fixed ticks
}

type GameMode int

const (
	Playing GameMode = iota
	GameOver
)

// --- Markers -------------------------------------------------------------

type nextCell struct{}
type frame struct{}

// --- Setup / teardown ----------------------------------------------------

func setupGame(c *app.Ctx) {
	score := app.GetResource[Score](c)
	*score = Score{Level: 1}
	app.GetResource[Drop](c).Tick = 0
	app.GetResource[Drop](c).EveryFix = dropPeriodForLevel(1)

	field := app.GetResource[Field](c)
	for y := 0; y < cellsH; y++ {
		for x := 0; x < cellsW; x++ {
			field.Filled[y][x] = false
		}
	}

	// Border frame — sized to the wide rendering.
	frameMap := generic.NewMap3[tui.Position, tui.Glyph, frame](c.World())
	border := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	rightX := boardX0 + cellsW*cellPx
	for y := 0; y < cellsH; y++ {
		frameMap.NewWith(&tui.Position{X: boardX0 - 1, Y: boardY0 + y},
			&tui.Glyph{Char: '│', Style: border}, &frame{})
		frameMap.NewWith(&tui.Position{X: rightX, Y: boardY0 + y},
			&tui.Glyph{Char: '│', Style: border}, &frame{})
	}
	for x := -1; x <= cellsW*cellPx; x++ {
		frameMap.NewWith(&tui.Position{X: boardX0 + x, Y: boardY0 + cellsH},
			&tui.Glyph{Char: '─', Style: border}, &frame{})
	}

	app.GetResource[NextPiece](c).Kind = rand.Intn(7)
	spawnFalling(c)
}

func teardownGame(c *app.Ctx) {
	field := app.GetResource[Field](c)
	for y := 0; y < cellsH; y++ {
		for x := 0; x < cellsW; x++ {
			if field.Filled[y][x] {
				for _, e := range field.Ents[y][x] {
					if c.World().Alive(e) {
						c.World().RemoveEntity(e)
					}
				}
			}
			field.Filled[y][x] = false
		}
	}
	falling := app.GetResource[Falling](c)
	for _, blk := range falling.Blocks {
		for _, e := range blk {
			if c.World().Alive(e) {
				c.World().RemoveEntity(e)
			}
		}
	}
	falling.Blocks = [4][cellPx]ecs.Entity{}

	purge := func(qry func(c *app.Ctx, fn func(e ecs.Entity))) {
		var toRemove []ecs.Entity
		qry(c, func(e ecs.Entity) { toRemove = append(toRemove, e) })
		for _, e := range toRemove {
			c.World().RemoveEntity(e)
		}
	}
	purge(func(c *app.Ctx, fn func(e ecs.Entity)) {
		app.Query1[frame](c, func(e ecs.Entity, _ *frame) { fn(e) })
	})
	purge(func(c *app.Ctx, fn func(e ecs.Entity)) {
		app.Query1[nextCell](c, func(e ecs.Entity, _ *nextCell) { fn(e) })
	})
}

func spawnFalling(c *app.Ctx) {
	falling := app.GetResource[Falling](c)
	next := app.GetResource[NextPiece](c)
	falling.Kind = next.Kind
	falling.Rot = 0
	falling.X = 3
	falling.Y = 0

	if collides(c, falling.Kind, falling.Rot, falling.X, falling.Y) {
		app.GetState[GameMode](c).Set(GameOver)
		return
	}

	mapper := generic.NewMap2[tui.Position, tui.Glyph](c.World())
	style := tcell.StyleDefault.Foreground(pieceColors[falling.Kind]).Bold(true)
	for i, b := range pieces[falling.Kind][falling.Rot] {
		cx, cy := falling.X+b[0], falling.Y+b[1]
		sx := cellScreenX(cx)
		for k := 0; k < cellPx; k++ {
			falling.Blocks[i][k] = mapper.NewWith(
				&tui.Position{X: sx + k, Y: boardY0 + cy},
				&tui.Glyph{Char: '█', Style: style},
			)
		}
	}

	next.Kind = rand.Intn(7)
	redrawNextPreview(c)
}

func redrawNextPreview(c *app.Ctx) {
	// Wipe existing preview cells.
	var toRemove []ecs.Entity
	app.Query1[nextCell](c, func(e ecs.Entity, _ *nextCell) {
		toRemove = append(toRemove, e)
	})
	for _, e := range toRemove {
		c.World().RemoveEntity(e)
	}

	next := app.GetResource[NextPiece](c)
	mapper := generic.NewMap3[tui.Position, tui.Glyph, nextCell](c.World())
	style := tcell.StyleDefault.Foreground(pieceColors[next.Kind]).Bold(true)
	previewX0 := boardX0 + cellsW*cellPx + 4
	previewY0 := boardY0 + 2
	for _, b := range pieces[next.Kind][0] {
		baseX := previewX0 + b[0]*cellPx
		for k := 0; k < cellPx; k++ {
			mapper.NewWith(
				&tui.Position{X: baseX + k, Y: previewY0 + b[1]},
				&tui.Glyph{Char: '█', Style: style},
				&nextCell{},
			)
		}
	}
}

// --- Collision and motion ------------------------------------------------

func collides(c *app.Ctx, kind, rot, x, y int) bool {
	field := app.GetResource[Field](c)
	for _, b := range pieces[kind][rot] {
		cx, cy := x+b[0], y+b[1]
		if cx < 0 || cx >= cellsW || cy >= cellsH {
			return true
		}
		if cy < 0 {
			continue
		}
		if field.Filled[cy][cx] {
			return true
		}
	}
	return false
}

func updateFallingPositions(c *app.Ctx) {
	falling := app.GetResource[Falling](c)
	gMap := generic.NewMap1[tui.Position](c.World())
	for i, b := range pieces[falling.Kind][falling.Rot] {
		sx := cellScreenX(falling.X + b[0])
		sy := boardY0 + falling.Y + b[1]
		for k := 0; k < cellPx; k++ {
			pos := gMap.Get(falling.Blocks[i][k])
			pos.X = sx + k
			pos.Y = sy
		}
	}
}

func tryMove(c *app.Ctx, dx, dy int) bool {
	falling := app.GetResource[Falling](c)
	if collides(c, falling.Kind, falling.Rot, falling.X+dx, falling.Y+dy) {
		return false
	}
	falling.X += dx
	falling.Y += dy
	updateFallingPositions(c)
	return true
}

func tryRotate(c *app.Ctx) bool {
	falling := app.GetResource[Falling](c)
	newRot := (falling.Rot + 1) % 4
	// Try simple offset wall-kicks: 0, ±1, ±2 horizontally.
	for _, dx := range []int{0, -1, 1, -2, 2} {
		if !collides(c, falling.Kind, newRot, falling.X+dx, falling.Y) {
			falling.X += dx
			falling.Rot = newRot
			updateFallingPositions(c)
			return true
		}
	}
	return false
}

func lockPiece(c *app.Ctx) {
	falling := app.GetResource[Falling](c)
	field := app.GetResource[Field](c)
	for i, b := range pieces[falling.Kind][falling.Rot] {
		cx, cy := falling.X+b[0], falling.Y+b[1]
		if cy < 0 || cy >= cellsH || cx < 0 || cx >= cellsW {
			// Out of bounds — already game-over case; just drop the entities.
			for _, e := range falling.Blocks[i] {
				if c.World().Alive(e) {
					c.World().RemoveEntity(e)
				}
			}
			continue
		}
		field.Filled[cy][cx] = true
		field.Color[cy][cx] = pieceColors[falling.Kind]
		field.Ents[cy][cx] = falling.Blocks[i]
	}
	falling.Blocks = [4][cellPx]ecs.Entity{}

	clearLines(c)

	if app.GetState[GameMode](c).Get() != GameOver {
		spawnFalling(c)
	}
}

func clearLines(c *app.Ctx) {
	field := app.GetResource[Field](c)
	score := app.GetResource[Score](c)

	cleared := 0
	// Walk bottom-up, compacting.
	write := cellsH - 1
	for read := cellsH - 1; read >= 0; read-- {
		full := true
		for x := 0; x < cellsW; x++ {
			if !field.Filled[read][x] {
				full = false
				break
			}
		}
		if full {
			cleared++
			for x := 0; x < cellsW; x++ {
				for _, e := range field.Ents[read][x] {
					if c.World().Alive(e) {
						c.World().RemoveEntity(e)
					}
				}
				field.Filled[read][x] = false
			}
			continue
		}
		if write != read {
			for x := 0; x < cellsW; x++ {
				field.Filled[write][x] = field.Filled[read][x]
				field.Color[write][x] = field.Color[read][x]
				field.Ents[write][x] = field.Ents[read][x]
				field.Filled[read][x] = false
			}
		}
		write--
	}
	// Zero out the rows above the write head (they got shifted down).
	for y := write; y >= 0; y-- {
		for x := 0; x < cellsW; x++ {
			field.Filled[y][x] = false
		}
	}

	if cleared > 0 {
		// Re-anchor the entity positions for everything that moved.
		gMap := generic.NewMap1[tui.Position](c.World())
		for y := 0; y < cellsH; y++ {
			for x := 0; x < cellsW; x++ {
				if !field.Filled[y][x] {
					continue
				}
				sx := cellScreenX(x)
				for k := 0; k < cellPx; k++ {
					pos := gMap.Get(field.Ents[y][x][k])
					pos.X = sx + k
					pos.Y = boardY0 + y
				}
			}
		}

		score.Lines += cleared
		score.Value += [...]int{0, 100, 300, 500, 800}[cleared] * score.Level
		newLevel := 1 + score.Lines/10
		if newLevel != score.Level {
			score.Level = newLevel
			app.GetResource[Drop](c).EveryFix = dropPeriodForLevel(newLevel)
		}
	}
}

func dropPeriodForLevel(level int) int {
	// Fixed timestep is 50ms below — so 12 means ~600ms per drop, 4 means
	// ~200ms. Caps at 2 to keep things humanly playable.
	switch {
	case level >= 10:
		return 2
	case level >= 7:
		return 3
	case level >= 5:
		return 4
	case level >= 3:
		return 6
	case level >= 2:
		return 9
	}
	return 12
}

// --- Systems -------------------------------------------------------------

func handleInput(c *app.Ctx) {
	state := app.GetState[GameMode](c)
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
		case ev.Key == tcell.KeyLeft, ev.Rune == 'a', ev.Rune == 'A':
			tryMove(c, -1, 0)
		case ev.Key == tcell.KeyRight, ev.Rune == 'd', ev.Rune == 'D':
			tryMove(c, 1, 0)
		case ev.Key == tcell.KeyDown, ev.Rune == 's', ev.Rune == 'S':
			if tryMove(c, 0, 1) {
				app.GetResource[Drop](c).Tick = 0
				app.GetResource[Score](c).Value++ // soft-drop bonus
			}
		case ev.Key == tcell.KeyUp, ev.Rune == 'w', ev.Rune == 'W':
			tryRotate(c)
		case ev.Rune == ' ':
			drop := 0
			for tryMove(c, 0, 1) {
				drop++
			}
			app.GetResource[Score](c).Value += drop * 2
			lockPiece(c)
		}
	}
}

func dropTick(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}
	drop := app.GetResource[Drop](c)
	drop.Tick++
	if drop.Tick < drop.EveryFix {
		return
	}
	drop.Tick = 0
	if !tryMove(c, 0, 1) {
		lockPiece(c)
	}
}

func renderHUD(c *app.Ctx) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	score := app.GetResource[Score](c)
	header := fmt.Sprintf(" SPLITI TETRIS   score: %d   lines: %d   level: %d   q: quit ",
		score.Value, score.Lines, score.Level)
	drawText(s, 1, 0, tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true), header)

	hudStyle := tcell.StyleDefault.Foreground(tcell.ColorLightGray)
	drawText(s, boardX0+cellsW*cellPx+4, boardY0, hudStyle, "NEXT")
	drawText(s, boardX0+cellsW*cellPx+4, boardY0+8, hudStyle, "← → move")
	drawText(s, boardX0+cellsW*cellPx+4, boardY0+9, hudStyle, "↑ rotate")
	drawText(s, boardX0+cellsW*cellPx+4, boardY0+10, hudStyle, "↓ soft drop")
	drawText(s, boardX0+cellsW*cellPx+4, boardY0+11, hudStyle, "spc hard drop")

	if app.GetState[GameMode](c).Get() == GameOver {
		over := tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
		msg := "  GAME OVER  "
		hint := "  press 'r' to restart, 'q' to quit  "
		w, _ := s.Size()
		drawText(s, (w-len(msg))/2, hudRows+cellsH/2, over, msg)
		drawText(s, (w-len(hint))/2, hudRows+cellsH/2+1, over, hint)
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
			FixedTimestep:   50 * gotime.Millisecond,
			TargetFrameRate: 60,
		},
	})

	app.InsertResource(a, &Field{})
	app.InsertResource(a, &Falling{})
	app.InsertResource(a, &NextPiece{})
	app.InsertResource(a, &Score{})
	app.InsertResource(a, &Drop{EveryFix: 12})
	app.InitState(a, Playing)

	app.OnEnter(a, Playing, setupGame)
	app.OnExit(a, Playing, teardownGame)

	a.AddSystems(schedule.Update, handleInput)
	a.AddSystems(schedule.FixedUpdate, dropTick)
	tui.AddOverlay(a, renderHUD)

	a.Run()
}
