package main

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radio3d/prop"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// building is one box on the street, in world-space min/max corners.
type building struct{ Min, Max m.Vec3 }

// Street/scene layout constants (meters).
const (
	streetHalfX = 45 // street spans X in [-45, 45]
	streetHalfZ = 22 // ground spans Z in [-22, 22]; buildings flank the central lane
	rxHeight    = 1.6
	shellCount  = 6 // pool of expanding wavefront shells

	heatNX = 72 // coverage grid resolution
	heatNZ = 36
)

// newScene builds the procedural street geometry, the transmitter, the BVH, and
// the coverage grid — all CPU data, no GPU. Buildings are laid out in two rows
// flanking a central lane, with deterministic pseudo-random heights/gaps so the
// scene is reproducible.
func newScene() *Scene {
	var buildings []building
	var faces []prop.Face

	// Ground: one large reflecting face in the y=0 plane (normal +Y).
	faces = append(faces, prop.NewFace(
		m.Vec3{X: -streetHalfX - 5, Z: -streetHalfZ - 5},
		m.Vec3{X: 2 * (streetHalfX + 5)},
		m.Vec3{Z: 2 * (streetHalfZ + 5)},
	))

	// Two rows of buildings at Z = ±laneZ, marching along X.
	const laneZ = 9.0
	const depth = 9.0
	var seed uint32 = 0x9e3779b9
	rnd := func() float32 { // small deterministic LCG in [0,1)
		seed = seed*1664525 + 1013904223
		return float32(seed>>8) / float32(1<<24)
	}
	for _, side := range []float32{-1, 1} {
		x := float32(-streetHalfX + 4)
		for x < streetHalfX-4 {
			w := 5 + rnd()*6
			h := 8 + rnd()*20
			gap := 2 + rnd()*4
			zCenter := side * (laneZ + depth/2)
			min := m.Vec3{X: x, Y: 0, Z: zCenter - depth/2}
			max := m.Vec3{X: x + w, Y: h, Z: zCenter + depth/2}
			buildings = append(buildings, building{Min: min, Max: max})
			faces = append(faces, prop.Box(min, max)...)
			x += w + gap
		}
	}

	tx := prop.Tx{
		Pos:    m.Vec3{X: -streetHalfX + 6, Y: 8, Z: 0},
		PowerW: 1,
		// A low VHF-ish carrier keeps the interference fringes a few meters wide so
		// the coverage heatmap shows smooth constructive/destructive bands rather
		// than aliased noise at this grid resolution.
		FreqHz: 100e6,
	}

	grid := prop.Grid{
		MinX: -streetHalfX, MaxX: streetHalfX,
		MinZ: -streetHalfZ, MaxZ: streetHalfZ,
		NX: heatNX, NZ: heatNZ, Y: rxHeight,
	}

	return &Scene{
		Faces:     faces,
		BVH:       prop.BuildBVH(faces),
		Buildings: buildings,
		Tx:        tx,
		Cfg:       prop.DefaultConfig(),
		Grid:      grid,
		RxHeight:  rxHeight,
		recompute: true,
	}
}

// setup loads meshes/materials and spawns every entity: ground, buildings, the
// transmitter, the draggable receiver, the wavefront shell pool, the coverage
// heatmap quads, and the sun.
func setup(c *app.Ctx) {
	scene := app.GetResource[Scene](c)
	meshes := app.GetResource[render3d.MeshRegistry](c)
	materials := app.GetResource[render3d.MaterialRegistry](c)

	must(meshes.Load("cube", render3d.Cube(1)))
	must(meshes.Load("sphere", render3d.UVSphere(1, 32, 20)))
	must(meshes.Load("shell", render3d.UVSphere(1, 40, 24)))
	must(meshes.Load("ground", render3d.Plane(2*(streetHalfX+5), 2*(streetHalfZ+5), 1, 1)))
	cellW := (scene.Grid.MaxX - scene.Grid.MinX) / float32(scene.Grid.NX)
	cellD := (scene.Grid.MaxZ - scene.Grid.MinZ) / float32(scene.Grid.NZ)
	must(meshes.Load("cell", render3d.Quad(cellW*0.95, cellD*0.95)))

	must(materials.Load("ground", render3d.Material{
		BaseColor: render3d.Color{R: 0.12, G: 0.13, B: 0.15, A: 1}, Roughness: 0.95,
	}))
	must(materials.Load("building", render3d.Material{
		BaseColor: render3d.Color{R: 0.55, G: 0.57, B: 0.62, A: 1}, Roughness: 0.7,
	}))
	must(materials.Load("tx", render3d.Material{
		BaseColor: render3d.Color{R: 1, G: 0.85, B: 0.3, A: 1},
		Emissive:  m.Vec3{X: 1.0, Y: 0.7, Z: 0.15},
	}))
	must(materials.Load("rx", render3d.Material{
		BaseColor: render3d.Color{R: 0.3, G: 0.9, B: 1, A: 1},
		Emissive:  m.Vec3{X: 0.1, Y: 0.5, Z: 0.7},
	}))
	// Heatmap cells are emissive-tinted so the per-instance color reads vividly
	// regardless of scene lighting.
	must(materials.Load("cell", render3d.Material{
		BaseColor: render3d.Color{R: 0.02, G: 0.02, B: 0.02, A: 1},
		Emissive:  m.Vec3{X: 0.9, Y: 0.9, Z: 0.9},
	}))
	// Wavefront shells: emissive white, tinted + made translucent per instance.
	must(materials.Load("shell", render3d.Material{
		BaseColor: render3d.Color{R: 0.1, G: 0.3, B: 0.6, A: 1},
		Emissive:  m.Vec3{X: 0.3, Y: 0.6, Z: 1.0},
	}))

	cmd := c.Commands()

	// Ground.
	render3d.SpawnMesh(cmd, render3d.NewTransform3D(m.Vec3{}), "ground", "ground")

	// Buildings (unit cube scaled to each box).
	for _, b := range scene.Buildings {
		center := b.Min.Add(b.Max).Scale(0.5)
		size := b.Max.Sub(b.Min)
		t := render3d.NewTransform3D(center)
		t.Scale = size
		render3d.SpawnMesh(cmd, t, "cube", "building")
	}

	// Transmitter marker (small emissive sphere) following Tx.Pos.
	spawnTagged(cmd, txTag{}, transformAt(scene.Tx.Pos, 1.1), "sphere", "tx")

	// Draggable receiver, started near the middle of the lane.
	spawnTagged(cmd, receiverTag{}, transformAt(m.Vec3{X: 0, Y: rxHeight, Z: 0}, 0.8), "sphere", "rx")

	// Wavefront shell pool (transparent, recolored/scaled each frame).
	for i := 0; i < shellCount; i++ {
		spawnShell(cmd, float32(i)/float32(shellCount))
	}

	// Coverage heatmap quads, laid flat (rotate the +Z quad to face +Y).
	flat := m.FromAxisAngle(m.Vec3{X: 1}, -math.Pi/2)
	for iz := 0; iz < scene.Grid.NZ; iz++ {
		for ix := 0; ix < scene.Grid.NX; ix++ {
			p := scene.Grid.Cell(ix, iz)
			p.Y = 0.05
			t := render3d.NewTransform3D(p)
			t.Rotation = flat
			spawnHeatCell(cmd, heatCell{Ix: ix, Iz: iz}, t)
		}
	}

	// Sun.
	render3d.SpawnDirectionalLight(cmd, render3d.XForm().Facing(m.Vec3{X: -0.3, Y: -1, Z: -0.2}), render3d.DirectionalLight{
		Color:     m.Vec3{X: 1, Y: 0.97, Z: 0.9},
		Intensity: 2.4,
	})

	// Frame the camera looking down the street.
	cam := app.GetResource[render3d.Camera3D](c)
	ctl := app.GetResource[CamCtl](c)
	ctl.init(cam, m.Vec3{X: -streetHalfX + 2, Y: 16, Z: 28})
}

// transformAt returns a uniform-scaled transform at p.
func transformAt(p m.Vec3, scale float32) render3d.Transform3D {
	t := render3d.NewTransform3D(p)
	t.Scale = m.Vec3{X: scale, Y: scale, Z: scale}
	return t
}

// --- spawn helpers (arche generic maps for the component bundles) ---

func spawnTagged[T any](cmd *app.Commands, tag T, t render3d.Transform3D, mesh, material string) {
	cmd.Add(func(w *ecs.World) {
		mp := generic.NewMap5[render3d.Transform3D, render3d.GlobalTransform, render3d.MeshRenderer, render3d.MaterialRef, T](w)
		mp.NewWith(&t, &render3d.GlobalTransform{Matrix: m.Identity4()},
			&render3d.MeshRenderer{Mesh: mesh}, &render3d.MaterialRef{Material: material}, &tag)
	})
}

func spawnShell(cmd *app.Commands, offset float32) {
	cmd.Add(func(w *ecs.World) {
		// 7 components incl. Transparent (tag) so the shell goes through the blended
		// back-to-front pass.
		mp := generic.NewMap7[render3d.Transform3D, render3d.GlobalTransform, render3d.MeshRenderer, render3d.MaterialRef, render3d.InstanceColor, render3d.Transparent, shell](w)
		t := render3d.NewTransform3D(m.Vec3{})
		col := render3d.InstanceColor{R: 0.3, G: 0.6, B: 1, A: 0}
		sh := shell{Offset: offset}
		mp.NewWith(&t, &render3d.GlobalTransform{Matrix: m.Identity4()},
			&render3d.MeshRenderer{Mesh: "shell"}, &render3d.MaterialRef{Material: "shell"},
			&col, &render3d.Transparent{}, &sh)
	})
}

func spawnHeatCell(cmd *app.Commands, hc heatCell, t render3d.Transform3D) {
	cmd.Add(func(w *ecs.World) {
		mp := generic.NewMap6[render3d.Transform3D, render3d.GlobalTransform, render3d.MeshRenderer, render3d.MaterialRef, render3d.InstanceColor, heatCell](w)
		col := render3d.InstanceColor{R: 0, G: 0, B: 0.2, A: 1}
		mp.NewWith(&t, &render3d.GlobalTransform{Matrix: m.Identity4()},
			&render3d.MeshRenderer{Mesh: "cell"}, &render3d.MaterialRef{Material: "cell"}, &col, &hc)
	})
}
