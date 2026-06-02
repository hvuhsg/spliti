// Claudette Survivors — an auto-shooter for the terminal.
//
// You move, your weapon aims itself at the nearest enemy. Survive as long as
// you can. Every few kills you level up and choose an upgrade.
//
// Controls:
//   arrows / wasd : move
//   1 / 2 / 3     : pick an upgrade at the level-up screen
//   r             : restart on game over
//   q / esc / ^C  : quit
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

// --- Board / tuning ------------------------------------------------------

const (
	boardW  = 70
	boardH  = 22
	hudRows = 1

	startHP     = 3
	iframeTicks = 10

	startFireCooldown = 18
	minFireCooldown   = 4
	startMoveCooldown = 2
	minMoveCooldown   = 1
	startPickup       = 3
	maxPickup         = 14
	maxProjectiles    = 4
	maxPierce        = 3
	maxHPCap          = 9

	startXPNext = 5
	xpGrowth    = 3 // added to XPNext per level

	startSpawnEvery = 60
	minSpawnEvery   = 8
	spawnRamp       = 100 // every N ticks, spawnEvery drops by 1

	holdTicks = 4

	// Boss cadence: one boss can be alive at a time, summoned at ~60s
	// intervals of game time. Game time is roughly 16 ticks/sec.
	bossEveryTicks = 60 * 16
	bossHP         = 25
	bossSpeed      = 22
	bossPoints     = 10

	shooterFireEvery = 60
	heartDropPercent = 3   // out of 100
	bombDropPercent  = 1   // out of 100
	bombRadius       = 6

	maxCritStacks      = 3
	critPercentPerStack = 25
	maxLifestealStacks = 3
)

// --- Components ----------------------------------------------------------

type Enemy struct {
	Kind     int
	HP       int
	MaxHP    int
	Speed    int // ticks per cell
	Tick     int
	Points   int
	FireTick int // shooters only: counts up to shooterFireEvery
}

type Bullet struct {
	DX, DY   int
	Pierce   int
	Friendly bool // true: player → enemies; false: enemy → player
}

type Gem struct {
	XP int
}

type Heart struct{}

type Bomb struct{}

// --- Resources -----------------------------------------------------------

type Player struct {
	X, Y         int
	Entity       ecs.Entity
	HP, MaxHP    int
	IFrames      int
	FireCooldown int
	FireTick     int
	MoveCooldown int
	MoveTick     int
	Projectiles     int
	Pierce          int
	PickupRadius    int
	CritStacks      int
	LifestealStacks int
	XP, Level       int
	XPNext          int
	Alive           bool
}

type Hold struct{ Up, Down, Left, Right int }

type World struct {
	Tick        int
	SpawnTick   int
	SpawnEvery  int
	Kills       int
	NextBossAt  int  // game tick when the next boss should spawn
	BossAlive   bool // gates spawning to one boss at a time
	BossEntity  ecs.Entity
}

type Choices struct {
	Options [3]UpgradeKind
}

type GameMode int

const (
	Playing GameMode = iota
	Upgrading
	GameOver
)

// --- Enemy kinds ---------------------------------------------------------

type enemyKind struct {
	glyph  rune
	style  tcell.Style
	hp     int
	speed  int
	points int
}

const (
	kindGrunt = iota
	kindRunner
	kindBrute
	kindShooter
	kindBoss
)

var enemyKinds = [...]enemyKind{
	kindGrunt:   {glyph: 'o', style: tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true), hp: 1, speed: 14, points: 1},
	kindRunner:  {glyph: 'x', style: tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true), hp: 1, speed: 7, points: 2},
	kindBrute:   {glyph: 'M', style: tcell.StyleDefault.Foreground(tcell.ColorPurple).Bold(true), hp: 3, speed: 20, points: 5},
	kindShooter: {glyph: 'T', style: tcell.StyleDefault.Foreground(tcell.ColorOrange).Bold(true), hp: 2, speed: 22, points: 4},
	kindBoss:    {glyph: 'Ω', style: tcell.StyleDefault.Foreground(tcell.ColorFuchsia).Bold(true), hp: bossHP, speed: bossSpeed, points: bossPoints},
}

// --- Upgrades ------------------------------------------------------------

type UpgradeKind int

const (
	UpFireRate UpgradeKind = iota
	UpProjectile
	UpPierce
	UpPickup
	UpMoveSpeed
	UpMaxHP
	UpCrit
	UpLifesteal
)

type upgradeDef struct {
	name string
	desc string
	// available reports whether the upgrade is still meaningful given the
	// player's current stats.
	available func(p *Player) bool
	apply     func(p *Player)
}

var upgradeDefs = map[UpgradeKind]upgradeDef{
	UpFireRate: {
		name: "Trigger discipline",
		desc: "shoot faster",
		available: func(p *Player) bool { return p.FireCooldown > minFireCooldown },
		apply: func(p *Player) {
			p.FireCooldown -= 2
			if p.FireCooldown < minFireCooldown {
				p.FireCooldown = minFireCooldown
			}
		},
	},
	UpProjectile: {
		name: "Multishot",
		desc: "+1 projectile per volley",
		available: func(p *Player) bool { return p.Projectiles < maxProjectiles },
		apply:     func(p *Player) { p.Projectiles++ },
	},
	UpPierce: {
		name: "Pierce",
		desc: "bullets punch through +1 enemy",
		available: func(p *Player) bool { return p.Pierce < maxPierce },
		apply:     func(p *Player) { p.Pierce++ },
	},
	UpPickup: {
		name: "Magnet",
		desc: "larger XP pickup radius",
		available: func(p *Player) bool { return p.PickupRadius < maxPickup },
		apply: func(p *Player) {
			p.PickupRadius += 2
			if p.PickupRadius > maxPickup {
				p.PickupRadius = maxPickup
			}
		},
	},
	UpMoveSpeed: {
		name: "Light feet",
		desc: "move faster",
		available: func(p *Player) bool { return p.MoveCooldown > minMoveCooldown },
		apply: func(p *Player) {
			p.MoveCooldown--
			if p.MoveCooldown < minMoveCooldown {
				p.MoveCooldown = minMoveCooldown
			}
		},
	},
	UpMaxHP: {
		name: "Vitality",
		desc: "+1 max HP, heal to full",
		available: func(p *Player) bool { return p.MaxHP < maxHPCap },
		apply: func(p *Player) {
			p.MaxHP++
			p.HP = p.MaxHP
		},
	},
	UpCrit: {
		name: "Critical",
		desc: "+25% chance to deal double damage",
		available: func(p *Player) bool { return p.CritStacks < maxCritStacks },
		apply:     func(p *Player) { p.CritStacks++ },
	},
	UpLifesteal: {
		name: "Lifesteal",
		desc: "gems may also heal you",
		available: func(p *Player) bool { return p.LifestealStacks < maxLifestealStacks },
		apply:     func(p *Player) { p.LifestealStacks++ },
	},
}

// --- Styles --------------------------------------------------------------

var (
	stylePlayer    = tcell.StyleDefault.Foreground(tcell.ColorLightGreen).Bold(true)
	styleHurt      = tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorRed).Bold(true)
	styleBullet    = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	styleEnemyShot = tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	styleGem       = tcell.StyleDefault.Foreground(tcell.ColorAqua).Bold(true)
	styleHeart     = tcell.StyleDefault.Foreground(tcell.ColorLightCoral).Bold(true)
	styleBomb      = tcell.StyleDefault.Foreground(tcell.ColorOrange).Bold(true)
	styleHUD       = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	styleOver      = tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	styleLevel     = tcell.StyleDefault.Foreground(tcell.ColorLightGreen).Bold(true)
	styleBoss      = tcell.StyleDefault.Foreground(tcell.ColorFuchsia).Bold(true)
	styleDim       = tcell.StyleDefault.Foreground(tcell.ColorGray)
)

// --- Helpers -------------------------------------------------------------

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func cheb(dx, dy int) int {
	if abs(dx) > abs(dy) {
		return abs(dx)
	}
	return abs(dy)
}

// --- Setup / restart -----------------------------------------------------

func setupGame(c *app.Ctx) {
	p := app.GetResource[Player](c)
	*p = Player{
		HP:           startHP,
		MaxHP:        startHP,
		FireCooldown: startFireCooldown,
		MoveCooldown: startMoveCooldown,
		Projectiles:  1,
		Pierce:       0,
		PickupRadius: startPickup,
		XPNext:       startXPNext,
		Level:        1,
		Alive:        true,
	}
	*app.GetResource[Hold](c) = Hold{}
	*app.GetResource[World](c) = World{
		SpawnEvery: startSpawnEvery,
		NextBossAt: bossEveryTicks,
	}
	*app.GetResource[Choices](c) = Choices{}

	p.X = boardW / 2
	p.Y = hudRows + boardH/2
	pMap := generic.NewMap2[tui.Position, tui.Glyph](c.World())
	p.Entity = pMap.NewWith(
		&tui.Position{X: p.X, Y: p.Y},
		&tui.Glyph{Char: '@', Style: stylePlayer},
	)
}

func teardownGame(c *app.Ctx) {
	purge := func(qFn func(c *app.Ctx, fn func(e ecs.Entity))) {
		var ents []ecs.Entity
		qFn(c, func(e ecs.Entity) { ents = append(ents, e) })
		for _, e := range ents {
			if c.World().Alive(e) {
				c.World().RemoveEntity(e)
			}
		}
	}
	purge(func(c *app.Ctx, fn func(e ecs.Entity)) {
		app.Query1[Enemy](c, func(e ecs.Entity, _ *Enemy) { fn(e) })
	})
	purge(func(c *app.Ctx, fn func(e ecs.Entity)) {
		app.Query1[Bullet](c, func(e ecs.Entity, _ *Bullet) { fn(e) })
	})
	purge(func(c *app.Ctx, fn func(e ecs.Entity)) {
		app.Query1[Gem](c, func(e ecs.Entity, _ *Gem) { fn(e) })
	})
	purge(func(c *app.Ctx, fn func(e ecs.Entity)) {
		app.Query1[Heart](c, func(e ecs.Entity, _ *Heart) { fn(e) })
	})
	purge(func(c *app.Ctx, fn func(e ecs.Entity)) {
		app.Query1[Bomb](c, func(e ecs.Entity, _ *Bomb) { fn(e) })
	})
	p := app.GetResource[Player](c)
	if p.Alive && c.World().Alive(p.Entity) {
		c.World().RemoveEntity(p.Entity)
	}
	p.Alive = false
}

// --- Input ---------------------------------------------------------------

func handleInput(c *app.Ctx) {
	state := app.GetState[GameMode](c)
	hold := app.GetResource[Hold](c)
	for _, ev := range app.ReadEvents[input.KeyEvent](c) {
		if ev.Key == tcell.KeyCtrlC || ev.Key == tcell.KeyEscape || ev.Rune == 'q' || ev.Rune == 'Q' {
			c.App().Stop()
			return
		}
		switch state.Get() {
		case GameOver:
			if ev.Rune == 'r' || ev.Rune == 'R' {
				teardownGame(c)
				setupGame(c)
				state.Set(Playing)
			}
		case Upgrading:
			idx := -1
			switch ev.Rune {
			case '1':
				idx = 0
			case '2':
				idx = 1
			case '3':
				idx = 2
			}
			if idx >= 0 {
				applyChoice(c, idx)
				state.Set(Playing)
			}
		case Playing:
			switch {
			case ev.Key == tcell.KeyUp, ev.Rune == 'w', ev.Rune == 'W':
				hold.Up = holdTicks
			case ev.Key == tcell.KeyDown, ev.Rune == 's', ev.Rune == 'S':
				hold.Down = holdTicks
			case ev.Key == tcell.KeyLeft, ev.Rune == 'a', ev.Rune == 'A':
				hold.Left = holdTicks
			case ev.Key == tcell.KeyRight, ev.Rune == 'd', ev.Rune == 'D':
				hold.Right = holdTicks
			}
		}
	}
}

func applyChoice(c *app.Ctx, idx int) {
	p := app.GetResource[Player](c)
	ch := app.GetResource[Choices](c)
	def := upgradeDefs[ch.Options[idx]]
	def.apply(p)
}

// --- Simulation step -----------------------------------------------------

func step(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}
	w := app.GetResource[World](c)
	w.Tick++
	if w.Tick%spawnRamp == 0 && w.SpawnEvery > minSpawnEvery {
		w.SpawnEvery--
	}

	movePlayer(c)
	moveBullets(c)
	moveEnemies(c)
	moveGems(c)
	collectHearts(c)
	collectBombs(c)
	autoFire(c)
	shooterFire(c)
	spawnEnemies(c)
	maybeSpawnBoss(c)
	checkLevelUp(c)
}

func movePlayer(c *app.Ctx) {
	p := app.GetResource[Player](c)
	if !p.Alive {
		return
	}
	if p.IFrames > 0 {
		p.IFrames--
	}
	p.MoveTick++
	if p.MoveTick < p.MoveCooldown {
		// Bleed holds slowly even when not moving, so a tapped key still
		// produces one move soon after.
		return
	}
	hold := app.GetResource[Hold](c)
	dx, dy := 0, 0
	if hold.Left > 0 {
		hold.Left--
		dx = -1
	}
	if hold.Right > 0 {
		hold.Right--
		dx = 1
	}
	if hold.Up > 0 {
		hold.Up--
		dy = -1
	}
	if hold.Down > 0 {
		hold.Down--
		dy = 1
	}
	if dx == 0 && dy == 0 {
		return
	}
	p.MoveTick = 0
	nx, ny := p.X+dx, p.Y+dy
	if nx < 1 {
		nx = 1
	}
	if nx > boardW-2 {
		nx = boardW - 2
	}
	if ny < hudRows+1 {
		ny = hudRows + 1
	}
	if ny > hudRows+boardH-2 {
		ny = hudRows + boardH - 2
	}
	if nx == p.X && ny == p.Y {
		return
	}
	p.X, p.Y = nx, ny
	posMap := generic.NewMap1[tui.Position](c.World())
	pos := posMap.Get(p.Entity)
	pos.X, pos.Y = nx, ny

	// Recolor when in iframes so the player flashes.
	glMap := generic.NewMap1[tui.Glyph](c.World())
	gl := glMap.Get(p.Entity)
	if p.IFrames > 0 {
		gl.Style = styleHurt
	} else {
		gl.Style = stylePlayer
	}
}

func autoFire(c *app.Ctx) {
	p := app.GetResource[Player](c)
	if !p.Alive {
		return
	}
	p.FireTick++
	if p.FireTick < p.FireCooldown {
		return
	}

	type target struct {
		dx, dy, dist int
	}
	var targets []target
	app.Query2[Enemy, tui.Position](c, func(_ ecs.Entity, _ *Enemy, pos *tui.Position) {
		dx, dy := pos.X-p.X, pos.Y-p.Y
		targets = append(targets, target{dx, dy, cheb(dx, dy)})
	})
	if len(targets) == 0 {
		return
	}
	// Sort by distance (small N — insertion sort is fine).
	for i := 1; i < len(targets); i++ {
		for j := i; j > 0 && targets[j].dist < targets[j-1].dist; j-- {
			targets[j], targets[j-1] = targets[j-1], targets[j]
		}
	}
	p.FireTick = 0

	mapper := generic.NewMap3[tui.Position, tui.Glyph, Bullet](c.World())
	fired := 0
	seenDir := map[[2]int]bool{}
	for _, t := range targets {
		if fired >= p.Projectiles {
			break
		}
		dx, dy := sign(t.dx), sign(t.dy)
		if dx == 0 && dy == 0 {
			continue
		}
		if seenDir[[2]int{dx, dy}] {
			continue
		}
		seenDir[[2]int{dx, dy}] = true
		mapper.NewWith(
			&tui.Position{X: p.X + dx, Y: p.Y + dy},
			&tui.Glyph{Char: bulletGlyph(dx, dy), Style: styleBullet},
			&Bullet{DX: dx, DY: dy, Pierce: p.Pierce, Friendly: true},
		)
		fired++
	}
}

// shooterFire makes shooter enemies periodically launch hostile bullets
// toward the player.
func shooterFire(c *app.Ctx) {
	p := app.GetResource[Player](c)
	if !p.Alive {
		return
	}
	type shot struct {
		x, y, dx, dy int
	}
	var shots []shot
	app.Query2[Enemy, tui.Position](c, func(_ ecs.Entity, en *Enemy, pos *tui.Position) {
		if en.Kind != kindShooter {
			return
		}
		en.FireTick++
		if en.FireTick < shooterFireEvery {
			return
		}
		en.FireTick = 0
		dx, dy := sign(p.X-pos.X), sign(p.Y-pos.Y)
		if dx == 0 && dy == 0 {
			return
		}
		shots = append(shots, shot{x: pos.X + dx, y: pos.Y + dy, dx: dx, dy: dy})
	})
	if len(shots) == 0 {
		return
	}
	mapper := generic.NewMap3[tui.Position, tui.Glyph, Bullet](c.World())
	for _, s := range shots {
		mapper.NewWith(
			&tui.Position{X: s.x, Y: s.y},
			&tui.Glyph{Char: bulletGlyph(s.dx, s.dy), Style: styleEnemyShot},
			&Bullet{DX: s.dx, DY: s.dy, Pierce: 0, Friendly: false},
		)
	}
}

func bulletGlyph(dx, dy int) rune {
	switch {
	case dx == 0:
		return '|'
	case dy == 0:
		return '-'
	case dx*dy > 0:
		return '\\'
	default:
		return '/'
	}
}

func moveBullets(c *app.Ctx) {
	type bState struct {
		e        ecs.Entity
		x, y     int
		dx, dy   int
		pierce   int
		friendly bool
	}
	var bullets []bState
	app.Query2[Bullet, tui.Position](c, func(e ecs.Entity, b *Bullet, pos *tui.Position) {
		bullets = append(bullets, bState{e: e, x: pos.X, y: pos.Y, dx: b.DX, dy: b.DY, pierce: b.Pierce, friendly: b.Friendly})
	})

	// Snapshot enemies by position so we can detect hits without nested queries.
	type enemyRef struct {
		e   ecs.Entity
		pos *tui.Position
		en  *Enemy
	}
	enemies := map[[2]int]enemyRef{}
	app.Query2[Enemy, tui.Position](c, func(e ecs.Entity, en *Enemy, pos *tui.Position) {
		enemies[[2]int{pos.X, pos.Y}] = enemyRef{e: e, pos: pos, en: en}
	})

	posMap := generic.NewMap1[tui.Position](c.World())
	bulletComp := generic.NewMap1[Bullet](c.World())

	var bulletsToKill []ecs.Entity
	var enemiesToKill []ecs.Entity
	var gemsToSpawn []struct {
		x, y, xp int
	}
	var heartsToSpawn [][2]int
	var bombsToSpawn [][2]int
	playerHit := false
	w := app.GetResource[World](c)
	p := app.GetResource[Player](c)

	for _, bs := range bullets {
		nx, ny := bs.x+bs.dx, bs.y+bs.dy
		if nx < 1 || nx > boardW-2 || ny < hudRows+1 || ny > hudRows+boardH-2 {
			bulletsToKill = append(bulletsToKill, bs.e)
			continue
		}
		// Hostile bullet headed at the player.
		if !bs.friendly && p.Alive && nx == p.X && ny == p.Y {
			playerHit = true
			bulletsToKill = append(bulletsToKill, bs.e)
			continue
		}
		// Only friendly bullets damage enemies.
		if bs.friendly {
			if ref, ok := enemies[[2]int{nx, ny}]; ok {
				dmg := 1
				if p.CritStacks > 0 && rand.Intn(100) < p.CritStacks*critPercentPerStack {
					dmg = 2
				}
				ref.en.HP -= dmg
				if ref.en.HP <= 0 {
					enemiesToKill = append(enemiesToKill, ref.e)
					gemsToSpawn = append(gemsToSpawn, struct{ x, y, xp int }{nx, ny, ref.en.Points})
					if ref.en.Kind == kindBoss {
						w.BossAlive = false
						heartsToSpawn = append(heartsToSpawn, [2]int{nx, ny})
						if rand.Intn(2) == 0 {
							bombsToSpawn = append(bombsToSpawn, [2]int{nx, ny})
						}
					} else {
						if rand.Intn(100) < heartDropPercent {
							heartsToSpawn = append(heartsToSpawn, [2]int{nx, ny})
						}
						if rand.Intn(100) < bombDropPercent {
							bombsToSpawn = append(bombsToSpawn, [2]int{nx, ny})
						}
					}
					delete(enemies, [2]int{nx, ny})
					w.Kills++
				}
				if bs.pierce <= 0 {
					bulletsToKill = append(bulletsToKill, bs.e)
					continue
				}
				bs.pierce--
				bc := bulletComp.Get(bs.e)
				bc.Pierce = bs.pierce
				pos := posMap.Get(bs.e)
				pos.X, pos.Y = nx, ny
				continue
			}
		}
		pos := posMap.Get(bs.e)
		pos.X, pos.Y = nx, ny
	}

	for _, e := range bulletsToKill {
		if c.World().Alive(e) {
			c.World().RemoveEntity(e)
		}
	}
	for _, e := range enemiesToKill {
		if c.World().Alive(e) {
			c.World().RemoveEntity(e)
		}
	}
	gemMap := generic.NewMap3[tui.Position, tui.Glyph, Gem](c.World())
	for _, g := range gemsToSpawn {
		gemMap.NewWith(
			&tui.Position{X: g.x, Y: g.y},
			&tui.Glyph{Char: '*', Style: styleGem},
			&Gem{XP: g.xp},
		)
	}
	heartMap := generic.NewMap3[tui.Position, tui.Glyph, Heart](c.World())
	for _, h := range heartsToSpawn {
		heartMap.NewWith(
			&tui.Position{X: h[0], Y: h[1]},
			&tui.Glyph{Char: '♥', Style: styleHeart},
			&Heart{},
		)
	}
	bombMap := generic.NewMap3[tui.Position, tui.Glyph, Bomb](c.World())
	for _, b := range bombsToSpawn {
		bombMap.NewWith(
			&tui.Position{X: b[0], Y: b[1]},
			&tui.Glyph{Char: '✦', Style: styleBomb},
			&Bomb{},
		)
	}

	if playerHit && p.Alive && p.IFrames == 0 {
		p.HP--
		p.IFrames = iframeTicks
		if p.HP <= 0 {
			p.Alive = false
			if c.World().Alive(p.Entity) {
				c.World().RemoveEntity(p.Entity)
			}
			app.GetState[GameMode](c).Set(GameOver)
		}
	}
}

func moveEnemies(c *app.Ctx) {
	p := app.GetResource[Player](c)
	if !p.Alive {
		return
	}
	type hit struct{ enemy ecs.Entity }
	var hits []hit

	app.Query2[Enemy, tui.Position](c, func(e ecs.Entity, en *Enemy, pos *tui.Position) {
		en.Tick++
		if en.Tick < en.Speed {
			return
		}
		en.Tick = 0
		dx, dy := sign(p.X-pos.X), sign(p.Y-pos.Y)
		if dx == 0 && dy == 0 {
			hits = append(hits, hit{enemy: e})
			return
		}
		nx, ny := pos.X+dx, pos.Y+dy
		if nx == p.X && ny == p.Y {
			hits = append(hits, hit{enemy: e})
			return
		}
		// Stay inside the board.
		if nx < 1 || nx > boardW-2 || ny < hudRows+1 || ny > hudRows+boardH-2 {
			return
		}
		pos.X, pos.Y = nx, ny
	})

	for _, h := range hits {
		if !c.World().Alive(h.enemy) {
			continue
		}
		if p.IFrames == 0 {
			p.HP--
			p.IFrames = iframeTicks
		}
		c.World().RemoveEntity(h.enemy)
	}
	if p.HP <= 0 && p.Alive {
		p.Alive = false
		if c.World().Alive(p.Entity) {
			c.World().RemoveEntity(p.Entity)
		}
		app.GetState[GameMode](c).Set(GameOver)
	}
}

func moveGems(c *app.Ctx) {
	p := app.GetResource[Player](c)
	if !p.Alive {
		return
	}
	type collected struct {
		e  ecs.Entity
		xp int
	}
	var got []collected
	app.Query2[Gem, tui.Position](c, func(e ecs.Entity, g *Gem, pos *tui.Position) {
		dx, dy := p.X-pos.X, p.Y-pos.Y
		d := cheb(dx, dy)
		if d == 0 {
			got = append(got, collected{e: e, xp: g.XP})
			return
		}
		if d <= p.PickupRadius {
			pos.X += sign(dx)
			pos.Y += sign(dy)
			if pos.X == p.X && pos.Y == p.Y {
				got = append(got, collected{e: e, xp: g.XP})
			}
		}
	})
	for _, g := range got {
		if c.World().Alive(g.e) {
			c.World().RemoveEntity(g.e)
		}
		p.XP += g.xp
		if p.LifestealStacks > 0 && p.HP < p.MaxHP && rand.Intn(16) < p.LifestealStacks {
			p.HP++
		}
	}
}

// collectHearts heals 1 HP when the player steps onto a heart pickup.
// Hearts do not drift — you have to walk to them.
func collectHearts(c *app.Ctx) {
	p := app.GetResource[Player](c)
	if !p.Alive {
		return
	}
	var taken []ecs.Entity
	app.Query2[Heart, tui.Position](c, func(e ecs.Entity, _ *Heart, pos *tui.Position) {
		if pos.X == p.X && pos.Y == p.Y {
			taken = append(taken, e)
		}
	})
	for _, e := range taken {
		if c.World().Alive(e) {
			c.World().RemoveEntity(e)
		}
		if p.HP < p.MaxHP {
			p.HP++
		}
	}
}

// collectBombs detonates a bomb on pickup, killing every enemy within
// bombRadius of the bomb. No XP from those kills.
func collectBombs(c *app.Ctx) {
	p := app.GetResource[Player](c)
	if !p.Alive {
		return
	}
	type spot struct{ x, y int }
	var taken []ecs.Entity
	var spots []spot
	app.Query2[Bomb, tui.Position](c, func(e ecs.Entity, _ *Bomb, pos *tui.Position) {
		if pos.X == p.X && pos.Y == p.Y {
			taken = append(taken, e)
			spots = append(spots, spot{pos.X, pos.Y})
		}
	})
	if len(taken) == 0 {
		return
	}
	for _, e := range taken {
		if c.World().Alive(e) {
			c.World().RemoveEntity(e)
		}
	}
	w := app.GetResource[World](c)
	var doomed []ecs.Entity
	app.Query2[Enemy, tui.Position](c, func(e ecs.Entity, en *Enemy, pos *tui.Position) {
		for _, s := range spots {
			if cheb(pos.X-s.x, pos.Y-s.y) <= bombRadius {
				doomed = append(doomed, e)
				w.Kills++
				if en.Kind == kindBoss {
					w.BossAlive = false
				}
				return
			}
		}
	})
	for _, e := range doomed {
		if c.World().Alive(e) {
			c.World().RemoveEntity(e)
		}
	}
}

func spawnEnemies(c *app.Ctx) {
	w := app.GetResource[World](c)
	w.SpawnTick++
	if w.SpawnTick < w.SpawnEvery {
		return
	}
	w.SpawnTick = 0

	// Pick a kind based on elapsed game time. Later enemies are gated by
	// elapsed ticks so a fresh game doesn't open with brutes everywhere.
	kind := kindGrunt
	switch {
	case w.Tick > 90*16 && rand.Intn(6) == 0:
		kind = kindBrute
	case w.Tick > 60*16 && rand.Intn(5) == 0:
		kind = kindShooter
	case w.Tick > 30*16 && rand.Intn(3) == 0:
		kind = kindRunner
	}

	x, y := edgeSpawn()
	spawnEnemyAt(c, kind, x, y)
}

func spawnEnemyAt(c *app.Ctx, kind, x, y int) ecs.Entity {
	def := enemyKinds[kind]
	m := generic.NewMap3[tui.Position, tui.Glyph, Enemy](c.World())
	return m.NewWith(
		&tui.Position{X: x, Y: y},
		&tui.Glyph{Char: def.glyph, Style: def.style},
		&Enemy{Kind: kind, HP: def.hp, MaxHP: def.hp, Speed: def.speed, Points: def.points},
	)
}

// maybeSpawnBoss summons a single boss at the scheduled time, then schedules
// the next one. Only one boss can be alive at a time.
func maybeSpawnBoss(c *app.Ctx) {
	w := app.GetResource[World](c)
	if w.BossAlive || w.Tick < w.NextBossAt {
		return
	}
	x, y := edgeSpawn()
	w.BossEntity = spawnEnemyAt(c, kindBoss, x, y)
	w.BossAlive = true
	w.NextBossAt = w.Tick + bossEveryTicks
}

func edgeSpawn() (int, int) {
	side := rand.Intn(4)
	switch side {
	case 0: // top
		return 1 + rand.Intn(boardW-2), hudRows + 1
	case 1: // bottom
		return 1 + rand.Intn(boardW-2), hudRows + boardH - 2
	case 2: // left
		return 1, hudRows + 1 + rand.Intn(boardH-2)
	default: // right
		return boardW - 2, hudRows + 1 + rand.Intn(boardH-2)
	}
}

func checkLevelUp(c *app.Ctx) {
	p := app.GetResource[Player](c)
	if p.XP < p.XPNext {
		return
	}
	p.XP -= p.XPNext
	p.Level++
	p.XPNext += xpGrowth

	// Roll three distinct available upgrades.
	pool := []UpgradeKind{}
	for k, def := range upgradeDefs {
		if def.available(p) {
			pool = append(pool, k)
		}
	}
	if len(pool) == 0 {
		return // nothing to offer
	}
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	ch := app.GetResource[Choices](c)
	for i := 0; i < 3; i++ {
		ch.Options[i] = pool[i%len(pool)]
	}
	app.GetState[GameMode](c).Set(Upgrading)
}

// --- HUD / overlay -------------------------------------------------------

func renderHUD(c *app.Ctx) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	p := app.GetResource[Player](c)
	w := app.GetResource[World](c)
	mode := app.GetState[GameMode](c).Get()

	secs := w.Tick / 16 // ~60ms ticks => ~16/sec
	hp := makeBar(p.HP, p.MaxHP, '♥', '·')
	xp := makeBar(p.XP, p.XPNext, '=', '·')
	header := fmt.Sprintf(" SURVIVORS  time: %02d:%02d  hp: %s  lvl: %d  xp: %s  kills: %d  q: quit ",
		secs/60, secs%60, hp, p.Level, xp, w.Kills)
	drawText(s, 0, 0, styleHUD, header)

	drawBorder(s)

	if w.BossAlive && c.World().Alive(w.BossEntity) {
		drawBossBar(c, s, w)
	}

	switch mode {
	case Upgrading:
		drawUpgradePanel(c, s)
	case GameOver:
		drawCenter(s,
			"  YOU DIED  ",
			fmt.Sprintf("  time: %d:%02d   kills: %d   level: %d  ", secs/60, secs%60, w.Kills, p.Level),
			"  press 'r' to retry, 'q' to quit  ",
			styleOver)
	}
}

func drawBorder(s tcell.Screen) {
	top := hudRows
	bottom := hudRows + boardH - 1
	left := 0
	right := boardW - 1
	style := styleDim
	s.SetContent(left, top, '┌', nil, style)
	s.SetContent(right, top, '┐', nil, style)
	s.SetContent(left, bottom, '└', nil, style)
	s.SetContent(right, bottom, '┘', nil, style)
	for x := left + 1; x < right; x++ {
		s.SetContent(x, top, '─', nil, style)
		s.SetContent(x, bottom, '─', nil, style)
	}
	for y := top + 1; y < bottom; y++ {
		s.SetContent(left, y, '│', nil, style)
		s.SetContent(right, y, '│', nil, style)
	}
}

func drawBossBar(c *app.Ctx, s tcell.Screen, w *World) {
	enMap := generic.NewMap1[Enemy](c.World())
	en := enMap.Get(w.BossEntity)
	if en == nil || en.MaxHP <= 0 {
		return
	}
	const barWidth = 30
	filled := en.HP * barWidth / en.MaxHP
	if filled < 0 {
		filled = 0
	}
	if filled > barWidth {
		filled = barWidth
	}
	bar := make([]rune, barWidth)
	for i := 0; i < barWidth; i++ {
		if i < filled {
			bar[i] = '█'
		} else {
			bar[i] = '░'
		}
	}
	width, _ := s.Size()
	label := fmt.Sprintf(" BOSS %s %d/%d ", string(bar), en.HP, en.MaxHP)
	drawText(s, (width-len(label))/2, hudRows, styleBoss, label)
}

func makeBar(cur, max int, on, off rune) string {
	if max <= 0 {
		return ""
	}
	out := make([]rune, max)
	for i := 0; i < max; i++ {
		if i < cur {
			out[i] = on
		} else {
			out[i] = off
		}
	}
	return string(out)
}

func drawUpgradePanel(c *app.Ctx, s tcell.Screen) {
	ch := app.GetResource[Choices](c)
	p := app.GetResource[Player](c)
	w, _ := s.Size()
	title := fmt.Sprintf("  LEVEL %d — pick an upgrade  ", p.Level)
	x0 := (w - 50) / 2
	y0 := hudRows + 4
	drawText(s, x0, y0, styleLevel, title)
	for i := 0; i < 3; i++ {
		def := upgradeDefs[ch.Options[i]]
		line := fmt.Sprintf("  %d) %s — %s", i+1, def.name, def.desc)
		drawText(s, x0, y0+2+i*2, styleHUD, line)
	}
	drawText(s, x0, y0+9, styleDim, "  press 1, 2, or 3  ")
}

func drawCenter(s tcell.Screen, lines ...interface{}) {
	// Last arg is the style.
	style := lines[len(lines)-1].(tcell.Style)
	msgs := make([]string, 0, len(lines)-1)
	for _, l := range lines[:len(lines)-1] {
		msgs = append(msgs, l.(string))
	}
	w, _ := s.Size()
	y := hudRows + boardH/2 - len(msgs)/2
	for i, m := range msgs {
		drawText(s, (w-len(m))/2, y+i, style, m)
	}
}

func drawText(s tcell.Screen, x, y int, style tcell.Style, text string) {
	for i, r := range text {
		s.SetContent(x+i, y, r, nil, style)
	}
}

// --- Wiring --------------------------------------------------------------

func main() {
	rand.Seed(gotime.Now().UnixNano())

	a := app.New()
	a.AddPlugins(defaultplugins.Plugins{
		Time: splititime.Plugin{
			FixedTimestep:   60 * gotime.Millisecond,
			TargetFrameRate: 60,
		},
	})

	app.InsertResource(a, &Player{})
	app.InsertResource(a, &Hold{})
	app.InsertResource(a, &World{})
	app.InsertResource(a, &Choices{})
	app.InitState(a, Playing)

	a.AddSystems(schedule.Startup, setupGame)
	a.AddSystems(schedule.Update, handleInput)
	a.AddSystems(schedule.FixedUpdate, step)
	tui.AddOverlay(a, renderHUD)

	a.Run()
}
