// Package game wires gameplay to the engine: RegisterSystems installs the
// game's systems and the shared game-state resource, and LoadAssets fills the
// mesh/material registries — here, the hex highlight marker plus every Kenney
// hexagon-kit tile the wave-function collapse can place.
package game

import (
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
	app.InsertResource(a, systems.NewPlayer())
	app.InsertResource(a, systems.NewCombat())
	app.InsertResource(a, systems.NewHUD())

	// Endless-wave run state. StartRun fires for the initial state at startup and
	// again on every restart; it must not regenerate the world.
	app.InitState(a, systems.Playing)
	app.OnEnter(a, systems.Playing, systems.StartRun)

	// Fixed-timestep simulation: player physics and enemy AI.
	a.AddSystems(schedule.FixedUpdate,
		app.System(systems.PlayerMove).Label("player-move"),
		app.System(systems.EnemyAI).Label("enemy-ai"),
	)

	// Per-frame: input/look, camera, then weapon and the run bookkeeping.
	a.AddSystems(schedule.Update,
		app.System(systems.PlayerInput).Label("player-input"),
		app.System(systems.PlayerCamera).Label("player-camera").After("player-input"),
		app.System(systems.FireWeapon).Label("fire").After("player-camera"),
		app.System(systems.SpawnWaves).Label("waves").After("fire"),
		app.System(systems.CheckEndConditions).Label("end").After("waves"),
		app.System(systems.HandleRestart).Label("restart").After("end"),
		app.System(systems.ExpireTTL).Label("ttl").After("restart"),
		// HUD: builds/queues its panels from Update (DrawPanel is drawn by the
		// engine's overlay pass during render). Loading panels here, outside the
		// render encoder, avoids corrupting the in-flight GPU pass.
		app.System(systems.DrawHUD).Label("hud").After("ttl"),
	)
}

//spliti:assets
func LoadAssets(c *app.Ctx) {
	meshes := app.GetResource[render3d.MeshRegistry](c)
	materials := app.GetResource[render3d.MaterialRegistry](c)

	// The translucent containment dome around the island (see systems.SpawnBarrier).
	must(meshes.Load("barrier", render3d.UVSphere(systems.IslandRadius, 48, 24)))
	must(materials.Load("barrier", render3d.Material{
		BaseColor:   render3d.Color{R: 0.45, G: 0.8, B: 1, A: 0.16},
		Emissive:    m.Vec3{X: 0.06, Y: 0.12, Z: 0.18},
		Roughness:   1,
		Alpha:       render3d.AlphaBlend,
		DoubleSided: true,
	}))

	// Primitive monster proxy (used until a real model is loaded) and the bullet
	// tracer streak.
	must(meshes.Load("enemy", render3d.UVSphere(systems.EnemyBodyRadius, 16, 12)))
	must(materials.Load("enemy", render3d.Material{
		BaseColor: render3d.Color{R: 0.8, G: 0.12, B: 0.12, A: 1},
		Emissive:  m.Vec3{X: 0.4, Y: 0.04, Z: 0.04},
		Roughness: 0.5,
	}))
	must(meshes.Load("tracer", render3d.Cube(1)))
	must(materials.Load("tracer", render3d.Material{
		BaseColor: render3d.Color{R: 1, G: 0.9, B: 0.5, A: 1},
		Emissive:  m.Vec3{X: 1, Y: 0.8, Z: 0.3},
		Roughness: 1,
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

	// Animated CC0 creature (Khronos "Fox", CC0). Optional: if the file is
	// missing the enemies fall back to the primitive proxy, so the game still
	// runs without it.
	cb := app.GetResource[systems.Combat](c)
	if fox, err := render3d.LoadGLTF(meshes, materials, "fox", "game/assets/monsters/fox.glb"); err == nil {
		cb.Monsters["fox"] = fox
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
