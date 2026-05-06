// Space Invaders — five rows of marching aliens, four shields, one cannon.
//
// Controls:
//   left / right (or a / d):  move the cannon
//   space:                     fire (one bullet on screen at a time)
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
	boardW    = 60
	boardH    = 22
	hudRows   = 1
	swarmRows = 5
	swarmCols = 11
	colSpace  = 4 // horizontal cells between alien columns
	rowSpace  = 1
	startLives = 3
	holdTicks = 4
	shieldCount = 4
)

// --- Components / markers ------------------------------------------------

type Bullet struct {
	VY int // -1 player bullet (going up), +1 alien bomb (going down)
}

type Shield struct{}

type Cannon struct{}

// --- Resources -----------------------------------------------------------

type Player struct {
	X, Y   int
	Entity ecs.Entity
}

type Swarm struct {
	Alive    [swarmRows][swarmCols]bool
	Ents     [swarmRows][swarmCols]ecs.Entity
	AnchorX  int
	AnchorY  int
	Dir      int
	Count    int
	StepTick int // counts up; when >= stepEvery, march
	BombTick int
}

type Hold struct{ Left, Right int }

type Stats struct {
	Score, Lives, Wave int
}

type GameMode int

const (
	Playing GameMode = iota
	GameOver
	Won
)

// --- Styles --------------------------------------------------------------

var alienGlyph = [swarmRows]rune{'Ψ', 'Ж', 'Ω', '◊', '#'}
var alienPoints = [swarmRows]int{40, 30, 20, 15, 10}
var alienStyle = [swarmRows]tcell.Style{
	tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true),
	tcell.StyleDefault.Foreground(tcell.ColorOrange).Bold(true),
	tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true),
	tcell.StyleDefault.Foreground(tcell.ColorAqua).Bold(true),
	tcell.StyleDefault.Foreground(tcell.ColorPurple).Bold(true),
}

var (
	stylePlayer  = tcell.StyleDefault.Foreground(tcell.ColorLightGreen).Bold(true)
	styleBullet  = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	styleBomb    = tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	styleShield  = tcell.StyleDefault.Foreground(tcell.ColorGreen).Bold(true)
	styleHUD     = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	styleOver    = tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	styleWon     = tcell.StyleDefault.Foreground(tcell.ColorLightGreen).Bold(true)
)

// --- Setup / teardown ----------------------------------------------------

func setupGame(c *app.Ctx) {
	stats := app.GetResource[Stats](c)
	*stats = Stats{Lives: startLives, Wave: 1}
	*app.GetResource[Hold](c) = Hold{}
	spawnWave(c, 1)
}

func teardownGame(c *app.Ctx) {
	swarm := app.GetResource[Swarm](c)
	for r := 0; r < swarmRows; r++ {
		for cIdx := 0; cIdx < swarmCols; cIdx++ {
			if swarm.Alive[r][cIdx] && c.World().Alive(swarm.Ents[r][cIdx]) {
				c.World().RemoveEntity(swarm.Ents[r][cIdx])
			}
			swarm.Alive[r][cIdx] = false
		}
	}
	swarm.Count = 0

	player := app.GetResource[Player](c)
	if c.World().Alive(player.Entity) {
		c.World().RemoveEntity(player.Entity)
	}

	purge := func(qFn func(c *app.Ctx, fn func(e ecs.Entity))) {
		var ents []ecs.Entity
		qFn(c, func(e ecs.Entity) { ents = append(ents, e) })
		for _, e := range ents {
			c.World().RemoveEntity(e)
		}
	}
	purge(func(c *app.Ctx, fn func(e ecs.Entity)) {
		app.Query1[Bullet](c, func(e ecs.Entity, _ *Bullet) { fn(e) })
	})
	purge(func(c *app.Ctx, fn func(e ecs.Entity)) {
		app.Query1[Shield](c, func(e ecs.Entity, _ *Shield) { fn(e) })
	})
}

func spawnWave(c *app.Ctx, wave int) {
	swarm := app.GetResource[Swarm](c)
	swarm.AnchorX = 4
	swarm.AnchorY = hudRows + 1 + min(wave-1, 4) // each wave starts a row lower
	swarm.Dir = 1
	swarm.StepTick = 0
	swarm.BombTick = 0
	swarm.Count = swarmRows * swarmCols

	mapper := generic.NewMap2[tui.Position, tui.Glyph](c.World())
	for r := 0; r < swarmRows; r++ {
		for cIdx := 0; cIdx < swarmCols; cIdx++ {
			swarm.Alive[r][cIdx] = true
			x := swarm.AnchorX + cIdx*colSpace
			y := swarm.AnchorY + r*(rowSpace+1)
			swarm.Ents[r][cIdx] = mapper.NewWith(
				&tui.Position{X: x, Y: y},
				&tui.Glyph{Char: alienGlyph[r], Style: alienStyle[r]},
			)
		}
	}

	// Player cannon.
	player := app.GetResource[Player](c)
	player.X = boardW/2
	player.Y = hudRows + boardH - 2
	pMap := generic.NewMap3[tui.Position, tui.Glyph, Cannon](c.World())
	player.Entity = pMap.NewWith(
		&tui.Position{X: player.X, Y: player.Y},
		&tui.Glyph{Char: '▲', Style: stylePlayer},
		&Cannon{},
	)

	// Shields — small clusters distributed across the lower middle.
	sMap := generic.NewMap3[tui.Position, tui.Glyph, Shield](c.World())
	shieldY := hudRows + boardH - 5
	gap := boardW / (shieldCount + 1)
	for i := 1; i <= shieldCount; i++ {
		cx := i * gap
		// 5-wide × 2-tall block with a notch cut from the bottom-middle.
		for dy := 0; dy < 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				if dy == 1 && (dx == 0) {
					continue
				}
				sMap.NewWith(
					&tui.Position{X: cx + dx, Y: shieldY + dy},
					&tui.Glyph{Char: '▓', Style: styleShield},
					&Shield{},
				)
			}
		}
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
			fireBullet(c)
		}
	}
}

func fireBullet(c *app.Ctx) {
	// One player bullet at a time.
	var existing int
	app.Query1[Bullet](c, func(_ ecs.Entity, b *Bullet) {
		if b.VY < 0 {
			existing++
		}
	})
	if existing > 0 {
		return
	}
	player := app.GetResource[Player](c)
	mapper := generic.NewMap3[tui.Position, tui.Glyph, Bullet](c.World())
	mapper.NewWith(
		&tui.Position{X: player.X, Y: player.Y - 1},
		&tui.Glyph{Char: '|', Style: styleBullet},
		&Bullet{VY: -1},
	)
}

func step(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}
	movePlayer(c)
	moveBullets(c)
	marchSwarm(c)
	maybeAlienBomb(c)
}

func movePlayer(c *app.Ctx) {
	hold := app.GetResource[Hold](c)
	player := app.GetResource[Player](c)
	dx := 0
	if hold.Left > 0 {
		hold.Left--
		dx -= 1
	}
	if hold.Right > 0 {
		hold.Right--
		dx += 1
	}
	if dx == 0 {
		return
	}
	newX := player.X + dx
	if newX < 1 {
		newX = 1
	}
	if newX >= boardW-1 {
		newX = boardW - 2
	}
	if newX == player.X {
		return
	}
	player.X = newX
	gMap := generic.NewMap1[tui.Position](c.World())
	pos := gMap.Get(player.Entity)
	pos.X = player.X
}

func moveBullets(c *app.Ctx) {
	swarm := app.GetResource[Swarm](c)
	stats := app.GetResource[Stats](c)
	player := app.GetResource[Player](c)

	// Snapshot every bullet's state up front so we don't run other queries
	// (alien / shield lookups) inside an active iteration.
	type bulletState struct {
		e  ecs.Entity
		x  int
		y  int
		vy int
	}
	var bullets []bulletState
	app.Query2[Bullet, tui.Position](c, func(e ecs.Entity, b *Bullet, p *tui.Position) {
		bullets = append(bullets, bulletState{e: e, x: p.X, y: p.Y, vy: b.VY})
	})

	// Collect shield positions once for O(1) lookup during processing.
	shields := map[[2]int]ecs.Entity{}
	app.Query2[Shield, tui.Position](c, func(e ecs.Entity, _ *Shield, p *tui.Position) {
		shields[[2]int{p.X, p.Y}] = e
	})

	type kill struct{ bullet ecs.Entity }
	var toKill []kill
	var killAlien []struct{ r, c int }
	killShield := map[ecs.Entity]struct{}{}
	hitPlayer := false

	gMap := generic.NewMap1[tui.Position](c.World())
	for _, bs := range bullets {
		newY := bs.y + bs.vy
		if newY < hudRows || newY >= hudRows+boardH {
			toKill = append(toKill, kill{bs.e})
			continue
		}
		// Alien hit — only player bullets matter.
		if bs.vy < 0 {
			if r, cIdx, ok := alienAt(swarm, bs.x, newY); ok {
				killAlien = append(killAlien, struct{ r, c int }{r, cIdx})
				toKill = append(toKill, kill{bs.e})
				continue
			}
		}
		// Shield hit, either direction.
		if shieldEnt, ok := shields[[2]int{bs.x, newY}]; ok {
			killShield[shieldEnt] = struct{}{}
			delete(shields, [2]int{bs.x, newY})
			toKill = append(toKill, kill{bs.e})
			continue
		}
		// Player hit by bomb.
		if bs.vy > 0 && newY == player.Y && bs.x == player.X {
			hitPlayer = true
			toKill = append(toKill, kill{bs.e})
			continue
		}
		// Survives the step — push the new Y to the entity.
		pos := gMap.Get(bs.e)
		pos.Y = newY
	}

	for _, kk := range toKill {
		if c.World().Alive(kk.bullet) {
			c.World().RemoveEntity(kk.bullet)
		}
	}
	for _, ka := range killAlien {
		if swarm.Alive[ka.r][ka.c] {
			if c.World().Alive(swarm.Ents[ka.r][ka.c]) {
				c.World().RemoveEntity(swarm.Ents[ka.r][ka.c])
			}
			swarm.Alive[ka.r][ka.c] = false
			swarm.Count--
			stats.Score += alienPoints[ka.r]
		}
	}
	for e := range killShield {
		if c.World().Alive(e) {
			c.World().RemoveEntity(e)
		}
	}
	if hitPlayer {
		stats.Lives--
		if stats.Lives <= 0 {
			app.GetState[GameMode](c).Set(GameOver)
		}
	}
	if swarm.Count == 0 && app.GetState[GameMode](c).Get() == Playing {
		stats.Wave++
		stats.Score += 200 // wave bonus
		// Reset swarm-related entities, keep player and remove old shields.
		var sh []ecs.Entity
		app.Query1[Shield](c, func(e ecs.Entity, _ *Shield) { sh = append(sh, e) })
		for _, e := range sh {
			c.World().RemoveEntity(e)
		}
		if c.World().Alive(player.Entity) {
			c.World().RemoveEntity(player.Entity)
		}
		spawnWave(c, stats.Wave)
	}
}

func alienAt(s *Swarm, x, y int) (int, int, bool) {
	for r := 0; r < swarmRows; r++ {
		for cIdx := 0; cIdx < swarmCols; cIdx++ {
			if !s.Alive[r][cIdx] {
				continue
			}
			ax := s.AnchorX + cIdx*colSpace
			ay := s.AnchorY + r*(rowSpace+1)
			if ax == x && ay == y {
				return r, cIdx, true
			}
		}
	}
	return 0, 0, false
}

func marchSwarm(c *app.Ctx) {
	swarm := app.GetResource[Swarm](c)
	if swarm.Count == 0 {
		return
	}
	swarm.StepTick++
	// As fewer aliens remain, swarm marches faster. Period in fixed-ticks.
	period := 6 + swarm.Count/8
	if swarm.StepTick < period {
		return
	}
	swarm.StepTick = 0

	// Find leading-edge column for the current direction.
	var minCol, maxCol = swarmCols, -1
	for r := 0; r < swarmRows; r++ {
		for cIdx := 0; cIdx < swarmCols; cIdx++ {
			if swarm.Alive[r][cIdx] {
				if cIdx < minCol {
					minCol = cIdx
				}
				if cIdx > maxCol {
					maxCol = cIdx
				}
			}
		}
	}
	if maxCol < 0 {
		return
	}

	leadX := func() int {
		if swarm.Dir > 0 {
			return swarm.AnchorX + maxCol*colSpace
		}
		return swarm.AnchorX + minCol*colSpace
	}

	// Would the next move push the leading column off the board?
	if leadX()+swarm.Dir < 1 || leadX()+swarm.Dir > boardW-2 {
		swarm.AnchorY++
		swarm.Dir = -swarm.Dir
	} else {
		swarm.AnchorX += swarm.Dir
	}

	// Push positions to entities, and check landing.
	gMap := generic.NewMap1[tui.Position](c.World())
	stats := app.GetResource[Stats](c)
	player := app.GetResource[Player](c)
	for r := 0; r < swarmRows; r++ {
		for cIdx := 0; cIdx < swarmCols; cIdx++ {
			if !swarm.Alive[r][cIdx] {
				continue
			}
			x := swarm.AnchorX + cIdx*colSpace
			y := swarm.AnchorY + r*(rowSpace+1)
			pos := gMap.Get(swarm.Ents[r][cIdx])
			pos.X = x
			pos.Y = y
			if y >= player.Y {
				stats.Lives = 0
				app.GetState[GameMode](c).Set(GameOver)
			}
		}
	}
}

func maybeAlienBomb(c *app.Ctx) {
	swarm := app.GetResource[Swarm](c)
	if swarm.Count == 0 {
		return
	}
	swarm.BombTick++
	// Drop a bomb every ~25 fixed ticks, more often as game progresses.
	stats := app.GetResource[Stats](c)
	period := 30 - stats.Wave*3
	if period < 8 {
		period = 8
	}
	if swarm.BombTick < period {
		return
	}
	swarm.BombTick = 0

	// Pick a random alive column, drop from the lowest alive alien in that col.
	var aliveCols []int
	for cIdx := 0; cIdx < swarmCols; cIdx++ {
		for r := 0; r < swarmRows; r++ {
			if swarm.Alive[r][cIdx] {
				aliveCols = append(aliveCols, cIdx)
				break
			}
		}
	}
	if len(aliveCols) == 0 {
		return
	}
	cIdx := aliveCols[rand.Intn(len(aliveCols))]
	var lowR int
	for r := swarmRows - 1; r >= 0; r-- {
		if swarm.Alive[r][cIdx] {
			lowR = r
			break
		}
	}
	x := swarm.AnchorX + cIdx*colSpace
	y := swarm.AnchorY + lowR*(rowSpace+1) + 1
	mapper := generic.NewMap3[tui.Position, tui.Glyph, Bullet](c.World())
	mapper.NewWith(
		&tui.Position{X: x, Y: y},
		&tui.Glyph{Char: '!', Style: styleBomb},
		&Bullet{VY: 1},
	)
}

func renderHUD(c *app.Ctx) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	stats := app.GetResource[Stats](c)
	header := fmt.Sprintf(" SPLITI INVADERS   score: %d   lives: %d   wave: %d   q: quit ",
		stats.Score, stats.Lives, stats.Wave)
	drawText(s, 1, 0, styleHUD, header)

	switch app.GetState[GameMode](c).Get() {
	case GameOver:
		showCenter(s, "  EARTH HAS FALLEN  ", "  press 'r' to retry, 'q' to quit  ", styleOver)
	case Won:
		showCenter(s, "  YOU WIN  ", "  press 'r' to play again  ", styleWon)
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
			FixedTimestep:   60 * gotime.Millisecond,
			TargetFrameRate: 60,
		},
	})

	app.InsertResource(a, &Player{})
	app.InsertResource(a, &Swarm{})
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
