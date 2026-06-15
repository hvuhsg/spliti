// Package game wires gameplay to the engine: RegisterSystems installs the
// game's systems and the shared game-state resource, and LoadAssets fills the
// mesh/material registries — here, the hex highlight marker plus every Kenney
// hexagon-kit tile the wave-function collapse can place.
package game

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/schedule"

	"hexisland/game/systems"
	"hexisland/game/wfc"
)

//spliti:systems
func RegisterSystems(a *app.App) {
	app.InsertResource(a, systems.NewGame())
	a.AddSystems(schedule.Update,
		app.System(systems.CameraControl).Label("camera"),
		app.System(systems.Interact).Label("interact"),
		app.System(systems.UpdateMarkers).Label("markers").After("interact"),
	)
}

//spliti:assets
func LoadAssets(c *app.Ctx) {
	meshes := app.GetResource[render3d.MeshRegistry](c)
	materials := app.GetResource[render3d.MaterialRegistry](c)

	// Hover marker: a flat, glowing, translucent hexagon that sits just above a
	// cell. InstanceColor tints it per cell each frame (see systems/markers.go).
	must(meshes.Load("marker", hexTopMesh(0.52)))
	must(materials.Load("marker", render3d.Material{
		BaseColor:   render3d.Color{R: 1, G: 1, B: 1, A: 1},
		Emissive:    m.Vec3{X: 0.25, Y: 0.35, Z: 0.45},
		Roughness:   1,
		Alpha:       render3d.AlphaBlend,
		DoubleSided: true,
	}))

	// Every tile the rules can place. Each Kenney GLB resolves its shared
	// Textures/colormap.png relative to the model, so the atlas colours come
	// through. The loaded Model is stashed on the Game resource so Interact can
	// spawn it when a cell collapses to that tile.
	g := app.GetResource[systems.Game](c)
	for _, tile := range wfc.Tiles {
		path := "game/assets/hexagon-kit/" + tile.Model + ".glb"
		model, err := render3d.LoadGLTF(meshes, materials, tile.Model, path)
		must(err)
		g.Models[tile.Model] = model
	}
}

// hexTopMesh builds a flat hexagon (a triangle fan, normal +Y) in the XZ plane,
// matching the kit's flat-left/right-edge orientation so the marker lines up
// with the tiles. radius is the circumradius (centre to corner).
func hexTopMesh(radius float32) *render3d.Mesh {
	mesh := &render3d.Mesh{}
	mesh.Vertices = append(mesh.Vertices, render3d.Vertex{
		PX: 0, PY: 0, PZ: 0, NY: 1, U: 0.5, V: 0.5, TX: 1, TW: 1,
	})
	for i := 0; i < 6; i++ {
		ang := math.Pi / 180 * float64(30+60*i) // corners at 30°,90°,…,330°
		cx := float32(math.Cos(ang))
		cz := float32(math.Sin(ang))
		mesh.Vertices = append(mesh.Vertices, render3d.Vertex{
			PX: radius * cx, PY: 0, PZ: radius * cz, NY: 1,
			U: 0.5 + 0.5*cx, V: 0.5 + 0.5*cz, TX: 1, TW: 1,
		})
	}
	for i := 0; i < 6; i++ {
		a := uint32(1 + i)
		b := uint32(1 + (i+1)%6)
		mesh.Indices = append(mesh.Indices, 0, a, b)
	}
	return mesh
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
