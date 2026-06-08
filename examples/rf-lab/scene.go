package main

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// setup loads meshes/materials and spawns the scene: the ground plane, the
// transmitter and receiver marker spheres (each on a thin decorative mast), and
// the sun. It also frames the fly camera looking across the plane.
func setup(c *app.Ctx) {
	lab := app.GetResource[Lab](c)
	meshes := app.GetResource[render3d.MeshRegistry](c)
	materials := app.GetResource[render3d.MaterialRegistry](c)

	must(meshes.Load("ground", render3d.Plane(160, 160, 1, 1)))
	must(meshes.Load("head", render3d.UVSphere(1.4, 32, 20)))
	must(meshes.Load("mast", render3d.Cube(1)))
	must(meshes.Load("block", render3d.Cube(1)))
	must(meshes.Load("cell", render3d.Quad(cellSize*0.98, cellSize*0.98)))

	must(materials.Load("ground", render3d.Material{
		BaseColor: render3d.Color{R: 0.10, G: 0.12, B: 0.14, A: 1}, Roughness: 0.95,
	}))
	// Field cells: emissive so the per-instance color reads vividly regardless of
	// scene lighting (the InstanceColor carries the live field value).
	must(materials.Load("field", render3d.Material{
		BaseColor: render3d.Color{R: 0.02, G: 0.02, B: 0.02, A: 1},
		Emissive:  m.Vec3{X: 1, Y: 1, Z: 1},
	}))
	must(materials.Load("mast", render3d.Material{
		BaseColor: render3d.Color{R: 0.4, G: 0.42, B: 0.46, A: 1}, Roughness: 0.6,
	}))
	// Transmitter: warm. Receiver: cool. Each has a brighter "selected" variant so
	// the highlight reads regardless of scene lighting.
	must(materials.Load("tx", render3d.Material{
		BaseColor: render3d.Color{R: 1, G: 0.82, B: 0.3, A: 1},
		Emissive:  m.Vec3{X: 0.6, Y: 0.4, Z: 0.08},
	}))
	must(materials.Load("tx_sel", render3d.Material{
		BaseColor: render3d.Color{R: 1, G: 0.9, B: 0.5, A: 1},
		Emissive:  m.Vec3{X: 1.4, Y: 1.0, Z: 0.25},
	}))
	must(materials.Load("rx", render3d.Material{
		BaseColor: render3d.Color{R: 0.3, G: 0.85, B: 1, A: 1},
		Emissive:  m.Vec3{X: 0.08, Y: 0.4, Z: 0.55},
	}))
	must(materials.Load("rx_sel", render3d.Material{
		BaseColor: render3d.Color{R: 0.55, G: 0.95, B: 1, A: 1},
		Emissive:  m.Vec3{X: 0.25, Y: 1.0, Z: 1.3},
	}))
	// LoS-blocking obstacle: a matte slab, with a lit "selected" variant.
	must(materials.Load("block", render3d.Material{
		BaseColor: render3d.Color{R: 0.22, G: 0.23, B: 0.26, A: 1}, Roughness: 0.85,
	}))
	must(materials.Load("block_sel", render3d.Material{
		BaseColor: render3d.Color{R: 0.45, G: 0.47, B: 0.52, A: 1}, Roughness: 0.7,
		Emissive: m.Vec3{X: 0.15, Y: 0.16, Z: 0.2},
	}))

	cmd := c.Commands()

	render3d.SpawnMesh(cmd, render3d.NewTransform3D(m.Vec3{}), "ground", "ground")
	spawnField(cmd)

	spawnTx(cmd, m.Vec3{X: -30, Y: markerHeight, Z: 0}, lab, false)
	spawnRx(cmd, m.Vec3{X: 30, Y: markerHeight, Z: 0}, lab, false)

	render3d.SpawnDirectionalLight(cmd, render3d.DirectionalLight{
		Direction: m.Vec3{X: -0.3, Y: -1, Z: -0.25},
		Color:     m.Vec3{X: 1, Y: 0.97, Z: 0.9},
		Intensity: 2.6,
	})

	cam := app.GetResource[render3d.Camera3D](c)
	ctl := app.GetResource[CamCtl](c)
	ctl.init(cam, m.Vec3{X: 0, Y: 34, Z: 60})
}

// spawnField spawns the grid of flat ground quads that visualize the wave, laid
// out over the whole plane and rotated so the +Z quad faces +Y.
func spawnField(cmd *app.Commands) {
	flat := m.FromAxisAngle(m.Vec3{X: 1}, -math.Pi/2)
	for iz := 0; iz < fieldN; iz++ {
		for ix := 0; ix < fieldN; ix++ {
			cx := -fieldExtent + (float32(ix)+0.5)*cellSize
			cz := -fieldExtent + (float32(iz)+0.5)*cellSize
			t := render3d.NewTransform3D(m.Vec3{X: cx, Y: 0.05, Z: cz})
			t.Rotation = flat
			spawnFieldCell(cmd, fieldCell{X: cx, Z: cz}, t)
		}
	}
}

func spawnFieldCell(cmd *app.Commands, fc fieldCell, t render3d.Transform3D) {
	cmd.Add(func(w *ecs.World) {
		mp := generic.NewMap6[render3d.Transform3D, render3d.GlobalTransform, render3d.MeshRenderer, render3d.MaterialRef, render3d.InstanceColor, fieldCell](w)
		col := render3d.InstanceColor{R: 0.04, G: 0.04, B: 0.05, A: 1}
		mp.NewWith(&t, &render3d.GlobalTransform{Matrix: m.Identity4()},
			&render3d.MeshRenderer{Mesh: "cell"}, &render3d.MaterialRef{Material: "field"},
			&col, &fc)
	})
}

// spawnTx spawns a transmitter: a tagged, pickable head sphere carrying a
// TxDevice (its own settings + signal chain), plus a decorative mast. When sel is
// true the new transmitter becomes the current selection (used for runtime adds).
func spawnTx(cmd *app.Commands, pos m.Vec3, lab *Lab, sel bool) {
	head := render3d.NewTransform3D(pos)
	cmd.Add(func(w *ecs.World) {
		mp := generic.NewMap6[render3d.Transform3D, render3d.GlobalTransform, render3d.MeshRenderer, render3d.MaterialRef, TxDevice, txTag](w)
		e := mp.NewWith(&head, &render3d.GlobalTransform{Matrix: m.Identity4()},
			&render3d.MeshRenderer{Mesh: "head"}, &render3d.MaterialRef{Material: "tx"},
			newTxDevice(), &txTag{})
		spawnMast(w, e)
		if sel {
			lab.Sel, lab.Ent = SelTx, e
		}
	})
}

// spawnRx spawns a receiver: a tagged head sphere carrying an RxDevice (its
// front-end + decode chain), plus a decorative mast. When sel is true the new
// receiver becomes the current selection (used for runtime adds).
func spawnRx(cmd *app.Commands, pos m.Vec3, lab *Lab, sel bool) {
	head := render3d.NewTransform3D(pos)
	cmd.Add(func(w *ecs.World) {
		mp := generic.NewMap6[render3d.Transform3D, render3d.GlobalTransform, render3d.MeshRenderer, render3d.MaterialRef, RxDevice, rxTag](w)
		e := mp.NewWith(&head, &render3d.GlobalTransform{Matrix: m.Identity4()},
			&render3d.MeshRenderer{Mesh: "head"}, &render3d.MaterialRef{Material: "rx"},
			newRxDevice(), &rxTag{})
		spawnMast(w, e)
		if sel {
			lab.Sel, lab.Ent = SelRx, e
		}
	})
}

// spawnMast adds a thin tall box from the ground to the head, parented to it so
// it follows when dragged (its local transform is relative to the head).
func spawnMast(w *ecs.World, parent ecs.Entity) {
	mast := render3d.NewTransform3D(m.Vec3{Y: -markerHeight / 2})
	mast.Scale = m.Vec3{X: 0.25, Y: markerHeight, Z: 0.25}
	mp := generic.NewMap5[render3d.Transform3D, render3d.GlobalTransform, render3d.MeshRenderer, render3d.MaterialRef, render3d.Parent](w)
	mp.NewWith(&mast, &render3d.GlobalTransform{Matrix: m.Identity4()},
		&render3d.MeshRenderer{Mesh: "mast"}, &render3d.MaterialRef{Material: "mast"},
		&render3d.Parent{Entity: parent})
}
