// Pac-Man — maze, dots, four ghosts, power pellets, tunnels.
//
// Each playfield cell renders as cellPx screen columns so the maze reads
// proportional rather than skinny.
//
// Controls:
//   arrows or wasd:  steer
//   r:               restart on game over / win
//   q or ctrl-c:     quit
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
	cellsW  = 19
	cellsH  = 15
	cellPx  = 2 // each maze cell is cellPx screen columns wide
	hudRows = 1
	boardX0 = 1
	boardY0 = hudRows + 1

	startLives      = 3
	dotPoints       = 10
	pelletPoints    = 50
	ghostPoints     = 200
	// Speeds, expressed as fixed-tick counts. With FixedTimestep at 80ms,
	// Pac-Man moves every 160ms (~6.3 cells/sec) and chase ghosts every
	// 240ms (~4.2 cells/sec) — so Pac-Man outruns the swarm by ~1.5×.
	pacmanPeriod      = 2
	ghostChasePeriod  = 3
	ghostScaredPeriod = 5
	frightenedTicks   = 80 // ~6.4 seconds
	deathPauseTicks   = 18 // ~1.4 seconds
	eatenTicks        = 40 // ~3.2 seconds
)

// cellScreenX returns the leftmost screen column for cell column cx.
func cellScreenX(cx int) int { return boardX0 + cx*cellPx }

// --- Maze ----------------------------------------------------------------

// '#' wall, '.' dot, 'o' power pellet, ' ' open path (used for the central
// corridor), 'T' tunnel (wraps to the other side of the row), 'P' Pac-Man
// spawn (parsed as open path).
var mazeRaw = []string{
	"###################", //  0
	"#........#........#", //  1
	"#.##.###.#.###.##.#", //  2
	"#o...............o#", //  3
	"#.##.#.#####.#.##.#", //  4
	"#....#...#...#....#", //  5
	"####.###.#.###.####", //  6
	"T   .         .   T", //  7  tunnel row
	"####.###.#.###.####", //  8
	"#....#...#...#....#", //  9
	"#.##.#.#####.#.##.#", // 10
	"#o.......P.......o#", // 11
	"#.##.###.#.###.##.#", // 12
	"#........#........#", // 13
	"###################", // 14
}

// Ghost spawn cells — each has a vertical exit through the corridor row.
var ghostSpawnCells = [4][2]int{{4, 7}, {8, 7}, {10, 7}, {14, 7}}

type Tile int8

const (
	TileWall Tile = iota
	TileEmpty
	TileDot
	TilePellet
	TileTunnel
)

type Dir int

const (
	DirRight Dir = iota
	DirLeft
	DirUp
	DirDown
	DirNone
)

func (d Dir) delta() (int, int) {
	switch d {
	case DirRight:
		return 1, 0
	case DirLeft:
		return -1, 0
	case DirUp:
		return 0, -1
	case DirDown:
		return 0, 1
	}
	return 0, 0
}

func (d Dir) opposite() Dir {
	switch d {
	case DirRight:
		return DirLeft
	case DirLeft:
		return DirRight
	case DirUp:
		return DirDown
	case DirDown:
		return DirUp
	}
	return DirNone
}

// --- Resources -----------------------------------------------------------

type Maze struct {
	Tiles    [cellsH][cellsW]Tile
	DotEnt   [cellsH][cellsW]ecs.Entity
	DotsLeft int
}

type Pacman struct {
	CX, CY  int
	SpawnX  int
	SpawnY  int
	Dir     Dir
	Want    Dir
	Entity  ecs.Entity
	SubTick int
}

type GhostState int

const (
	GhostChase GhostState = iota
	GhostFrightened
	GhostEaten
)

type Ghost struct {
	CX, CY      int
	SpawnX      int
	SpawnY      int
	Dir         Dir
	Personality int // 0 Blinky, 1 Pinky, 2 Inky, 3 Clyde
	State       GhostState
	Entity      ecs.Entity
	SubTick     int
	EatenTimer  int
	Color       tcell.Color
}

type Ghosts struct{ All [4]Ghost }

type Stats struct {
	Score      int
	Lives      int
	Frightened int
	DeathPause int
}

type GameMode int

const (
	Playing GameMode = iota
	GameOver
	Won
)

// Markers for cleanup-by-query.
type wallTile struct{}

// --- Styles --------------------------------------------------------------

var (
	styleWall    = tcell.StyleDefault.Foreground(tcell.ColorBlue).Bold(true)
	styleDot     = tcell.StyleDefault.Foreground(tcell.ColorLightGray)
	stylePellet  = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	stylePac     = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	styleScared  = tcell.StyleDefault.Foreground(tcell.ColorBlue).Bold(true)
	styleHUD     = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	styleOver    = tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	styleWon     = tcell.StyleDefault.Foreground(tcell.ColorLightGreen).Bold(true)
)

var ghostColors = [4]tcell.Color{
	tcell.ColorRed,    // Blinky
	tcell.ColorPink,   // Pinky
	tcell.ColorAqua,   // Inky
	tcell.ColorOrange, // Clyde
}

// --- Setup / teardown ----------------------------------------------------

func setupGame(c *app.Ctx) {
	*app.GetResource[Stats](c) = Stats{Lives: startLives}
	maze := app.GetResource[Maze](c)
	*maze = Maze{}
	pStart := parseMaze(maze)

	wallMap := generic.NewMap3[tui.Position, tui.Glyph, wallTile](c.World())
	dotMap := generic.NewMap2[tui.Position, tui.Glyph](c.World())
	for y := 0; y < cellsH; y++ {
		for x := 0; x < cellsW; x++ {
			sx := cellScreenX(x)
			sy := boardY0 + y
			switch maze.Tiles[y][x] {
			case TileWall:
				for k := 0; k < cellPx; k++ {
					wallMap.NewWith(
						&tui.Position{X: sx + k, Y: sy},
						&tui.Glyph{Char: '█', Style: styleWall},
						&wallTile{},
					)
				}
			case TileDot:
				maze.DotEnt[y][x] = dotMap.NewWith(
					&tui.Position{X: sx, Y: sy},
					&tui.Glyph{Char: '·', Style: styleDot},
				)
			case TilePellet:
				maze.DotEnt[y][x] = dotMap.NewWith(
					&tui.Position{X: sx, Y: sy},
					&tui.Glyph{Char: '●', Style: stylePellet},
				)
			}
		}
	}

	// Pac-Man.
	pacman := app.GetResource[Pacman](c)
	pacman.SpawnX, pacman.SpawnY = pStart[0], pStart[1]
	pacman.CX, pacman.CY = pacman.SpawnX, pacman.SpawnY
	pacman.Dir = DirLeft
	pacman.Want = DirLeft
	layeredMap := generic.NewMap3[tui.Position, tui.Glyph, tui.Layer](c.World())
	pacman.Entity = layeredMap.NewWith(
		&tui.Position{X: cellScreenX(pacman.CX), Y: boardY0 + pacman.CY},
		&tui.Glyph{Char: '<', Style: stylePac},
		&tui.Layer{Z: 2},
	)

	// Ghosts.
	ghosts := app.GetResource[Ghosts](c)
	for i := 0; i < 4; i++ {
		g := &ghosts.All[i]
		g.SpawnX, g.SpawnY = ghostSpawnCells[i][0], ghostSpawnCells[i][1]
		g.CX, g.CY = g.SpawnX, g.SpawnY
		g.Dir = DirLeft
		g.Personality = i
		g.State = GhostChase
		g.Color = ghostColors[i]
		g.SubTick = 0
		g.EatenTimer = 0
		g.Entity = layeredMap.NewWith(
			&tui.Position{X: cellScreenX(g.CX), Y: boardY0 + g.CY},
			&tui.Glyph{Char: 'M', Style: tcell.StyleDefault.Foreground(g.Color).Bold(true)},
			&tui.Layer{Z: 1},
		)
	}
}

func parseMaze(m *Maze) [2]int {
	var pStart [2]int
	for y, row := range mazeRaw {
		for x, ch := range row {
			switch ch {
			case '#':
				m.Tiles[y][x] = TileWall
			case '.':
				m.Tiles[y][x] = TileDot
				m.DotsLeft++
			case 'o':
				m.Tiles[y][x] = TilePellet
				m.DotsLeft++
			case 'T':
				m.Tiles[y][x] = TileTunnel
			case 'P':
				m.Tiles[y][x] = TileEmpty
				pStart = [2]int{x, y}
			default:
				m.Tiles[y][x] = TileEmpty
			}
		}
	}
	return pStart
}

func teardownGame(c *app.Ctx) {
	var walls []ecs.Entity
	app.Query1[wallTile](c, func(e ecs.Entity, _ *wallTile) {
		walls = append(walls, e)
	})
	for _, e := range walls {
		c.World().RemoveEntity(e)
	}

	maze := app.GetResource[Maze](c)
	for y := 0; y < cellsH; y++ {
		for x := 0; x < cellsW; x++ {
			e := maze.DotEnt[y][x]
			if c.World().Alive(e) {
				c.World().RemoveEntity(e)
			}
		}
	}

	pacman := app.GetResource[Pacman](c)
	if c.World().Alive(pacman.Entity) {
		c.World().RemoveEntity(pacman.Entity)
	}

	ghosts := app.GetResource[Ghosts](c)
	for i := range ghosts.All {
		if c.World().Alive(ghosts.All[i].Entity) {
			c.World().RemoveEntity(ghosts.All[i].Entity)
		}
	}
}

// --- Movement primitives -------------------------------------------------

// nextCell returns the cell reached by stepping `d` from (cx, cy), respecting
// tunnel wraparound. The bool is false when the step is blocked by a wall or
// the world edge in a non-tunnel row.
func nextCell(m *Maze, cx, cy int, d Dir) (int, int, bool) {
	dx, dy := d.delta()
	nx, ny := cx+dx, cy+dy
	if ny < 0 || ny >= cellsH {
		return cx, cy, false
	}
	if nx < 0 || nx >= cellsW {
		if m.Tiles[cy][cx] != TileTunnel {
			return cx, cy, false
		}
		if nx < 0 {
			nx = cellsW - 1
		} else {
			nx = 0
		}
	}
	if m.Tiles[ny][nx] == TileWall {
		return cx, cy, false
	}
	return nx, ny, true
}

// --- Systems -------------------------------------------------------------

func handleInput(c *app.Ctx) {
	state := app.GetState[GameMode](c)
	pacman := app.GetResource[Pacman](c)
	for _, ev := range app.ReadEvents[input.KeyEvent](c) {
		if ev.Key == tcell.KeyCtrlC || ev.Key == tcell.KeyEscape || ev.Rune == 'q' {
			c.App().Stop()
			return
		}
		if state.Get() != Playing {
			if ev.Rune == 'r' {
				state.Set(Playing)
			}
			continue
		}
		switch {
		case ev.Key == tcell.KeyUp, ev.Rune == 'w', ev.Rune == 'W':
			pacman.Want = DirUp
		case ev.Key == tcell.KeyDown, ev.Rune == 's', ev.Rune == 'S':
			pacman.Want = DirDown
		case ev.Key == tcell.KeyLeft, ev.Rune == 'a', ev.Rune == 'A':
			pacman.Want = DirLeft
		case ev.Key == tcell.KeyRight, ev.Rune == 'd', ev.Rune == 'D':
			pacman.Want = DirRight
		}
	}
}

func step(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}
	stats := app.GetResource[Stats](c)
	if stats.DeathPause > 0 {
		stats.DeathPause--
		if stats.DeathPause == 0 {
			softReset(c)
		}
		return
	}
	if stats.Frightened > 0 {
		stats.Frightened--
		if stats.Frightened == 0 {
			ghosts := app.GetResource[Ghosts](c)
			for i := range ghosts.All {
				if ghosts.All[i].State == GhostFrightened {
					ghosts.All[i].State = GhostChase
				}
			}
		}
	}
	pacman := app.GetResource[Pacman](c)
	pacman.SubTick++
	if pacman.SubTick >= pacmanPeriod {
		pacman.SubTick = 0
		movePacman(c)
		if checkCollision(c) {
			return
		}
	}
	moveGhosts(c)
	checkCollision(c)

	maze := app.GetResource[Maze](c)
	if maze.DotsLeft == 0 && app.GetState[GameMode](c).Get() == Playing {
		app.GetState[GameMode](c).Set(Won)
	}
}

func movePacman(c *app.Ctx) {
	pacman := app.GetResource[Pacman](c)
	maze := app.GetResource[Maze](c)

	// Latch the desired turn if it's now possible.
	if pacman.Want != DirNone && pacman.Want != pacman.Dir {
		if _, _, ok := nextCell(maze, pacman.CX, pacman.CY, pacman.Want); ok {
			pacman.Dir = pacman.Want
		}
	}

	nx, ny, ok := nextCell(maze, pacman.CX, pacman.CY, pacman.Dir)
	if !ok {
		return
	}
	pacman.CX, pacman.CY = nx, ny

	switch maze.Tiles[ny][nx] {
	case TileDot:
		if c.World().Alive(maze.DotEnt[ny][nx]) {
			c.World().RemoveEntity(maze.DotEnt[ny][nx])
		}
		maze.Tiles[ny][nx] = TileEmpty
		maze.DotsLeft--
		app.GetResource[Stats](c).Score += dotPoints
	case TilePellet:
		if c.World().Alive(maze.DotEnt[ny][nx]) {
			c.World().RemoveEntity(maze.DotEnt[ny][nx])
		}
		maze.Tiles[ny][nx] = TileEmpty
		maze.DotsLeft--
		stats := app.GetResource[Stats](c)
		stats.Score += pelletPoints
		stats.Frightened = frightenedTicks
		ghosts := app.GetResource[Ghosts](c)
		for i := range ghosts.All {
			if ghosts.All[i].State == GhostChase {
				ghosts.All[i].State = GhostFrightened
				ghosts.All[i].Dir = ghosts.All[i].Dir.opposite()
			}
		}
	}

	gMap := generic.NewMap1[tui.Position](c.World())
	pos := gMap.Get(pacman.Entity)
	pos.X = cellScreenX(pacman.CX)
	pos.Y = boardY0 + pacman.CY

	glyMap := generic.NewMap1[tui.Glyph](c.World())
	gly := glyMap.Get(pacman.Entity)
	switch pacman.Dir {
	case DirRight:
		gly.Char = '>'
	case DirLeft:
		gly.Char = '<'
	case DirUp:
		gly.Char = '^'
	case DirDown:
		gly.Char = 'v'
	}
}

func moveGhosts(c *app.Ctx) {
	ghosts := app.GetResource[Ghosts](c)
	pacman := app.GetResource[Pacman](c)
	maze := app.GetResource[Maze](c)
	stats := app.GetResource[Stats](c)
	gMap := generic.NewMap1[tui.Position](c.World())
	glyMap := generic.NewMap1[tui.Glyph](c.World())

	for i := range ghosts.All {
		g := &ghosts.All[i]

		if g.State == GhostEaten {
			g.EatenTimer--
			if g.EatenTimer <= 0 {
				g.CX, g.CY = g.SpawnX, g.SpawnY
				g.Dir = DirLeft
				if stats.Frightened > 0 {
					g.State = GhostFrightened
				} else {
					g.State = GhostChase
				}
				pos := gMap.Get(g.Entity)
				pos.X = cellScreenX(g.CX)
				pos.Y = boardY0 + g.CY
				gly := glyMap.Get(g.Entity)
				if g.State == GhostFrightened {
					gly.Char = 'm'
					gly.Style = styleScared
				} else {
					gly.Char = 'M'
					gly.Style = tcell.StyleDefault.Foreground(g.Color).Bold(true)
				}
			}
			continue
		}

		// Chase ghosts step every ghostChasePeriod fixed ticks; frightened
		// ghosts every ghostScaredPeriod (slower than chase, slower than
		// Pac-Man — pellets matter).
		g.SubTick++
		period := ghostChasePeriod
		if g.State == GhostFrightened {
			period = ghostScaredPeriod
		}
		if g.SubTick < period {
			continue
		}
		g.SubTick = 0

		switch g.State {
		case GhostFrightened:
			valid := availableDirs(maze, g.CX, g.CY, g.Dir.opposite())
			if len(valid) > 0 {
				g.Dir = valid[rand.Intn(len(valid))]
			}
			if nx, ny, ok := nextCell(maze, g.CX, g.CY, g.Dir); ok {
				g.CX, g.CY = nx, ny
			}
		case GhostChase:
			target := chaseTarget(g, &pacman.CX, &pacman.CY, pacman.Dir)
			bestDir := g.Dir
			bestDist := -1
			for _, d := range []Dir{DirUp, DirLeft, DirDown, DirRight} {
				if d == g.Dir.opposite() {
					continue
				}
				nx, ny, ok := nextCell(maze, g.CX, g.CY, d)
				if !ok {
					continue
				}
				dist := abs(nx-target[0]) + abs(ny-target[1])
				if bestDist < 0 || dist < bestDist {
					bestDist = dist
					bestDir = d
				}
			}
			if bestDist < 0 {
				// Dead end — reverse if at all possible.
				bestDir = g.Dir.opposite()
			}
			g.Dir = bestDir
			if nx, ny, ok := nextCell(maze, g.CX, g.CY, g.Dir); ok {
				g.CX, g.CY = nx, ny
			}
		}

		pos := gMap.Get(g.Entity)
		pos.X = cellScreenX(g.CX)
		pos.Y = boardY0 + g.CY
		gly := glyMap.Get(g.Entity)
		if g.State == GhostFrightened {
			gly.Char = 'm'
			gly.Style = styleScared
		} else {
			gly.Char = 'M'
			gly.Style = tcell.StyleDefault.Foreground(g.Color).Bold(true)
		}
	}
}

func chaseTarget(g *Ghost, pacX, pacY *int, pacDir Dir) [2]int {
	dx, dy := pacDir.delta()
	switch g.Personality {
	case 0: // Blinky — straight at Pac-Man.
		return [2]int{*pacX, *pacY}
	case 1: // Pinky — four tiles ahead of Pac-Man.
		return [2]int{*pacX + 4*dx, *pacY + 4*dy}
	case 2: // Inky — four tiles behind Pac-Man (cheap stand-in for the
		//   real "vector flip through Blinky" rule).
		return [2]int{*pacX - 4*dx, *pacY - 4*dy}
	case 3: // Clyde — chase when far, scatter when close.
		if abs(g.CX-*pacX)+abs(g.CY-*pacY) > 8 {
			return [2]int{*pacX, *pacY}
		}
		return [2]int{0, cellsH - 1}
	}
	return [2]int{*pacX, *pacY}
}

func availableDirs(m *Maze, cx, cy int, exclude Dir) []Dir {
	var dirs []Dir
	for _, d := range []Dir{DirUp, DirLeft, DirDown, DirRight} {
		if d == exclude {
			continue
		}
		if _, _, ok := nextCell(m, cx, cy, d); ok {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func checkCollision(c *app.Ctx) bool {
	pacman := app.GetResource[Pacman](c)
	ghosts := app.GetResource[Ghosts](c)
	stats := app.GetResource[Stats](c)
	gMap := generic.NewMap1[tui.Position](c.World())
	glyMap := generic.NewMap1[tui.Glyph](c.World())

	for i := range ghosts.All {
		g := &ghosts.All[i]
		if g.State == GhostEaten {
			continue
		}
		if g.CX != pacman.CX || g.CY != pacman.CY {
			continue
		}
		if g.State == GhostFrightened {
			g.State = GhostEaten
			g.EatenTimer = eatenTicks
			stats.Score += ghostPoints
			gly := glyMap.Get(g.Entity)
			gly.Char = ' '
			gly.Style = tcell.StyleDefault
			pos := gMap.Get(g.Entity)
			pos.X = -10
			pos.Y = -10
			continue
		}
		// Pac-Man dies.
		stats.Lives--
		if stats.Lives <= 0 {
			app.GetState[GameMode](c).Set(GameOver)
		} else {
			stats.DeathPause = deathPauseTicks
		}
		return true
	}
	return false
}

func softReset(c *app.Ctx) {
	pacman := app.GetResource[Pacman](c)
	ghosts := app.GetResource[Ghosts](c)
	stats := app.GetResource[Stats](c)
	stats.Frightened = 0

	pacman.CX, pacman.CY = pacman.SpawnX, pacman.SpawnY
	pacman.Dir = DirLeft
	pacman.Want = DirLeft
	pacman.SubTick = 0
	gMap := generic.NewMap1[tui.Position](c.World())
	glyMap := generic.NewMap1[tui.Glyph](c.World())
	pos := gMap.Get(pacman.Entity)
	pos.X = cellScreenX(pacman.CX)
	pos.Y = boardY0 + pacman.CY
	gly := glyMap.Get(pacman.Entity)
	gly.Char = '<'

	for i := range ghosts.All {
		gh := &ghosts.All[i]
		gh.CX, gh.CY = gh.SpawnX, gh.SpawnY
		gh.Dir = DirLeft
		gh.State = GhostChase
		gh.SubTick = 0
		gh.EatenTimer = 0
		pos := gMap.Get(gh.Entity)
		pos.X = cellScreenX(gh.CX)
		pos.Y = boardY0 + gh.CY
		gly := glyMap.Get(gh.Entity)
		gly.Char = 'M'
		gly.Style = tcell.StyleDefault.Foreground(gh.Color).Bold(true)
	}
}

func renderHUD(c *app.Ctx) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	stats := app.GetResource[Stats](c)
	maze := app.GetResource[Maze](c)
	header := fmt.Sprintf(" SPLITI PAC-MAN   score: %d   lives: %d   dots: %d   q: quit ",
		stats.Score, stats.Lives, maze.DotsLeft)
	drawText(s, 1, 0, styleHUD, header)

	if stats.Frightened > 0 {
		drawText(s, 1, hudRows+cellsH+1, styleScared, " ! frightened ! ")
	}

	switch app.GetState[GameMode](c).Get() {
	case GameOver:
		showCenter(s, "  GAME OVER  ", "  press 'r' to restart, 'q' to quit  ", styleOver)
	case Won:
		showCenter(s, "  YOU WIN!  ", "  press 'r' to play again  ", styleWon)
	}
}

func showCenter(s tcell.Screen, msg, hint string, style tcell.Style) {
	w, _ := s.Size()
	drawText(s, (w-len(msg))/2, hudRows+cellsH/2, style, msg)
	drawText(s, (w-len(hint))/2, hudRows+cellsH/2+1, style, hint)
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
			FixedTimestep:   80 * gotime.Millisecond,
			TargetFrameRate: 60,
		},
	})

	app.InsertResource(a, &Maze{})
	app.InsertResource(a, &Pacman{})
	app.InsertResource(a, &Ghosts{})
	app.InsertResource(a, &Stats{})
	app.InitState(a, Playing)

	app.OnEnter(a, Playing, setupGame)
	app.OnExit(a, Playing, teardownGame)

	a.AddSystems(schedule.Update, handleInput)
	a.AddSystems(schedule.FixedUpdate, step)
	tui.AddOverlay(a, renderHUD)

	a.Run()
}
