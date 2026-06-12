package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

// scaffold writes a new game project under dir (which must not exist yet).
// engine, when non-empty, becomes a local `replace` directive in go.mod.
func scaffold(dir, engine string) error {
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("new: %s already exists", dir)
	}
	name := filepath.Base(dir)
	data := struct {
		Name   string
		Module string
		Engine string
	}{Name: name, Module: name, Engine: engine}

	for path, tmpl := range projectFiles {
		t, err := template.New(path).Parse(tmpl)
		if err != nil {
			return fmt.Errorf("new: template %s: %w", path, err)
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, data); err != nil {
			return fmt.Errorf("new: template %s: %w", path, err)
		}
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, buf.Bytes(), 0o644); err != nil {
			return err
		}
	}

	// Resolve dependencies so the project builds out of the box.
	if err := runIn(dir, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("new: go mod tidy failed (project files were created): %w", err)
	}
	return nil
}

func runIn(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// projectFiles maps relative path → template. The layout follows the editor's
// conventions: prefabs in game/entities (one per file), systems in
// game/systems, scenes in game/scenes, plain component structs in
// game/components.
var projectFiles = map[string]string{
	"go.mod": `module {{.Module}}

go 1.25.0

require github.com/hvuhsg/spliti v0.0.0
{{- if .Engine}}

replace github.com/hvuhsg/spliti => {{.Engine}}
{{- end}}
`,

	".gitignore": `/.spliti/
{{.Name}}
{{.Name}}.exe
game.wasm
`,

	"spliti.toml": `name = "{{.Name}}"

[window]
title = "{{.Name}}"
width = 1280
height = 720

[scene]
file = "game/scenes/main.go"
default = "Main"
`,

	"main.go": `// Command {{.Name}} is the game entry point. It builds natively and for
// js/wasm; the spliti editor runs the same packages through the generated
// .spliti/editor target instead.
package main

import (
	"runtime"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs/actions"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"

	"{{.Module}}/game"
	"{{.Module}}/game/scenes"
)

func init() { runtime.LockOSThread() }

func main() {
	a := app.New()
	a.AddPlugins(
		splititime.Plugin{},
		render3d.Plugin{
			Width: 1280, Height: 720,
			Title:   "{{.Name}}",
			Ambient: m.Vec3{X: 0.05, Y: 0.05, Z: 0.06},
			Samples: 4,
			VSync:   true,
		},
		actions.Plugin{Map: game.BuildActions()},
	)
	game.RegisterSystems(a)
	a.AddSystems(schedule.Startup, app.Chain(
		app.System(game.LoadAssets),
		app.System(scenes.Main),
	))
	a.Run()
}
`,

	"game/game.go": `// Package game wires gameplay to the engine: RegisterSystems adds the game's
// systems (the editor gates these behind Play mode) and LoadAssets fills the
// mesh/material registries (the editor runs this as-is).
package game

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/schedule"

	"{{.Module}}/game/systems"
)

//spliti:systems
func RegisterSystems(a *app.App) {
	a.AddSystems(schedule.Update, app.System(systems.Spin).Label("spin"))
}

//spliti:assets
func LoadAssets(c *app.Ctx) {
	meshes := app.GetResource[render3d.MeshRegistry](c)
	materials := app.GetResource[render3d.MaterialRegistry](c)
	must(meshes.Load("ground", render3d.Plane(20, 20, 1, 1)))
	must(meshes.Load("crate", render3d.Cube(1)))
	must(meshes.Load("sphere", render3d.UVSphere(0.6, 48, 32)))
	must(materials.Load("ground", render3d.Material{
		BaseColor: render3d.Color{R: 0.5, G: 0.5, B: 0.55, A: 1},
		Roughness: 0.9,
	}))
	must(materials.Load("metal", render3d.Material{
		BaseColor: render3d.Color{R: 1, G: 0.78, B: 0.34, A: 1},
		Metallic:  1, Roughness: 0.25,
	}))
	must(materials.Load("plastic", render3d.Material{
		BaseColor: render3d.Color{R: 0.85, G: 0.15, B: 0.18, A: 1},
		Roughness: 0.4,
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
`,

	"game/input.go": `package game

import (
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/inputs/actions"
)

// BuildActions is the game's input table: logical actions bound to physical
// keys, mouse buttons, and gamepad input. The editor's Input panel edits this
// function in place; game systems read it via actions.Get(c).
//
//spliti:input
func BuildActions() *actions.Map {
	m := actions.NewMap()
	m.Bind("jump", actions.Key(inputs.KeySpace), actions.Pad(inputs.GamepadA))
	m.BindAxis("move-x",
		actions.ButtonAxis(actions.Key(inputs.KeyA), actions.Key(inputs.KeyD)),
		actions.PadAxis(inputs.AxisLeftX))
	m.BindAxis("move-y",
		actions.ButtonAxis(actions.Key(inputs.KeyS), actions.Key(inputs.KeyW)),
		actions.PadAxis(inputs.AxisLeftY).Inverted())
	return m
}
`,

	"game/layers.go": `package game

// Collision layers: each name is one bit in collision Layer/Mask fields.
// The editor's Layers panel edits this block in place — rename or append
// freely, but do not remove or reorder entries (the bit positions are baked
// into compiled code and saved scenes).
//
//spliti:layers
const (
	LayerDefault uint32 = 1 << iota
	LayerPlayer
	LayerEnemy
)
`,

	"game/components/components.go": `// Package components holds the game's component structs. Every exported
// struct here is registered with the editor's inspector by spliti gen.
package components

// Spinner rotates its entity around the world Y axis (see systems.Spin).
type Spinner struct {
	Speed float32 // radians per second
}
`,

	"game/systems/spin.go": `package systems

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/mlange-42/arche/ecs"

	"{{.Module}}/game/components"
)

// Spin rotates every Spinner entity around the world Y axis.
func Spin(c *app.Ctx) {
	dt := float32(app.GetResource[splititime.Time](c).Delta().Seconds())
	app.Query2[components.Spinner, render3d.Transform3D](c,
		func(_ ecs.Entity, s *components.Spinner, t *render3d.Transform3D) {
			t.Rotation = m.FromAxisAngle(m.Vec3{Y: 1}, s.Speed*dt).Mul(t.Rotation)
		})
}
`,

	"game/entities/ground.go": `package entities

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/mlange-42/arche/ecs"
)

//spliti:entity
func SpawnGround(c *app.Ctx, t render3d.Transform3D) ecs.Entity {
	return render3d.NewMesh(c, t, "ground", "ground")
}
`,

	"game/entities/crate.go": `package entities

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/mlange-42/arche/ecs"
)

//spliti:entity
func SpawnCrate(c *app.Ctx, t render3d.Transform3D) ecs.Entity {
	return render3d.NewMesh(c, t, "crate", "metal")
}
`,

	"game/entities/sphere.go": `package entities

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"
	"github.com/mlange-42/arche/ecs"

	"{{.Module}}/game/components"
)

//spliti:entity
func SpawnSphere(c *app.Ctx, t render3d.Transform3D) ecs.Entity {
	e := render3d.NewMesh(c, t, "sphere", "plastic")
	scene.Set(c, e, components.Spinner{Speed: 1})
	return e
}
`,

	"game/entities/sun.go": `package entities

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// SpawnSun makes a directional (sun) light. The light casts along the
// transform's forward (-Z) axis, so the scene's XForm().EulerDeg(...) aims it —
// rotate it with the editor gizmo to change the time of day.
//
//spliti:entity
func SpawnSun(c *app.Ctx, t render3d.Transform3D) ecs.Entity {
	mp := generic.NewMap3[render3d.Transform3D, render3d.GlobalTransform, render3d.DirectionalLight](c.World())
	return mp.NewWith(&t, &render3d.GlobalTransform{Matrix: m.Identity4()},
		&render3d.DirectionalLight{
			Color:     m.Vec3{X: 1, Y: 0.98, Z: 0.92},
			Intensity: 3,
		})
}
`,

	"game/scenes/main.go": `// Package scenes holds the game's scene setup functions. The spliti editor
// reads and edits these files — keep spawn arguments literal.
package scenes

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"

	"{{.Module}}/game/entities"
)

//spliti:scene Main
func Main(c *app.Ctx) {
	_ = scene.Spawn(c, "ground", entities.SpawnGround(c, render3d.XForm()))
	_ = scene.Spawn(c, "sun", entities.SpawnSun(c, render3d.XForm().EulerDeg(-60, 30, 0)))
	_ = scene.Spawn(c, "crate1", entities.SpawnCrate(c, render3d.XForm().At(-1.4, 0.5, 0)))
	_ = scene.Spawn(c, "sphere1", entities.SpawnSphere(c, render3d.XForm().At(1.4, 0.6, 0)))
}
`,
}
