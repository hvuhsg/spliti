package main

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/mlange-42/arche/ecs"
)

// GameMap is a grid of wall ids. 0 is empty/walkable; 1..N are solid wall
// texture ids (see wallColor in render.go).
type GameMap struct {
	W, H int
	Grid []uint8
}

// At returns the wall id at (x,y); out-of-bounds reads as solid stone so rays
// and movement treat the world edge as a wall.
func (m GameMap) At(x, y int) uint8 {
	if x < 0 || y < 0 || x >= m.W || y >= m.H {
		return 1
	}
	return m.Grid[y*m.W+x]
}

// solid reports whether cell (x,y) blocks movement and rays.
func (m GameMap) solid(x, y int) bool { return m.At(x, y) != 0 }

// buildMap constructs the level procedurally so dimensions are always
// consistent: a stone border, a divided floor plan with a doorway, a hollow
// red-brick room in the upper-right, and a scattering of blue-metal pillars.
func buildMap() GameMap {
	const W, H = 24, 24
	g := make([]uint8, W*H)
	m := GameMap{W: W, H: H, Grid: g}
	set := func(x, y int, v uint8) {
		if x >= 0 && y >= 0 && x < W && y < H {
			g[y*W+x] = v
		}
	}

	// Outer border (stone = 1).
	for x := 0; x < W; x++ {
		set(x, 0, 1)
		set(x, H-1, 1)
	}
	for y := 0; y < H; y++ {
		set(0, y, 1)
		set(W-1, y, 1)
	}

	// Horizontal divider across the middle with a central gap.
	for x := 1; x < 11; x++ {
		set(x, 8, 1)
	}
	for x := 14; x < W-1; x++ {
		set(x, 8, 1)
	}

	// Vertical divider in the lower half with a doorway.
	for y := 9; y < H-1; y++ {
		set(8, y, 1)
	}
	set(8, 13, 0)
	set(8, 14, 0)

	// Hollow red-brick room (id 2) top-right with a single doorway.
	for x := 16; x <= 20; x++ {
		set(x, 3, 2)
		set(x, 7, 2)
	}
	for y := 3; y <= 7; y++ {
		set(16, y, 2)
		set(20, y, 2)
	}
	set(18, 7, 0) // doorway into the red room

	// Blue-metal pillars (id 3) scattered through the lower rooms.
	for _, p := range [][2]int{
		{4, 16}, {4, 18}, {6, 16}, {6, 18},
		{12, 20}, {14, 16}, {16, 18}, {19, 20},
	} {
		set(p[0], p[1], 3)
	}

	return m
}

// enemySpawn / pickupSpawn describe where mobs start. All coordinates are open
// cells in buildMap()'s layout.
type enemySpawn struct {
	x, y float64
	kind uint8
	hp   int
}

type pickupSpawn struct {
	x, y   float64
	kind   uint8
	amount int
}

var enemySpawns = []enemySpawn{
	{x: 12.5, y: 4.5, kind: enemyImp, hp: 100},
	{x: 18.5, y: 5.5, kind: enemyImp, hp: 100},
	{x: 4.5, y: 12.5, kind: enemyImp, hp: 100},
	{x: 12.5, y: 18.5, kind: enemyImp, hp: 100},
	{x: 20.5, y: 20.5, kind: enemyImp, hp: 100},
}

var pickupSpawns = []pickupSpawn{
	{x: 10.5, y: 10.5, kind: pickHealth, amount: 40},
	{x: 21.5, y: 3.5, kind: pickAmmo, amount: 25},
	{x: 2.5, y: 21.5, kind: pickAmmo, amount: 25},
	{x: 22.5, y: 16.5, kind: pickHealth, amount: 25},
}

// setupLevel (re)initialises the world: rebuild the map, reset the player, clear
// any mobs left over from a previous run, and spawn the level's enemies and
// pickups. Registered via OnEnter(Playing), so it covers both the initial start
// and restarts.
func setupLevel(c *app.Ctx) {
	g := app.GetResource[Game](c)
	g.Map = buildMap()
	g.Message = ""
	g.MsgTicks = 0

	p := app.GetResource[Player](c)
	*p = Player{
		X: 3.5, Y: 3.5,
		DirX: 1, DirY: 0,
		PlaneX: 0, PlaneY: 0.66, // ~66 deg FOV
		HP:   playerMaxHP,
		Ammo: startAmmo,
	}
	*app.GetResource[Hold](c) = Hold{}

	// Despawn any mobs from a previous run.
	app.Query1[Enemy](c, func(e ecs.Entity, _ *Enemy) { c.Commands().Despawn(e) })
	app.Query1[Pickup](c, func(e ecs.Entity, _ *Pickup) { c.Commands().Despawn(e) })

	for _, s := range enemySpawns {
		s := s
		app.Spawn2[Vec2, Enemy](c.Commands(), func(v *Vec2, en *Enemy) {
			v.X, v.Y = s.x, s.y
			en.Kind, en.HP = s.kind, s.hp
		})
	}
	for _, s := range pickupSpawns {
		s := s
		app.Spawn2[Vec2, Pickup](c.Commands(), func(v *Vec2, pk *Pickup) {
			v.X, v.Y = s.x, s.y
			pk.Kind, pk.Amount = s.kind, s.amount
		})
	}
}
