package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scaffoldFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), `module demo

go 1.25.0

require github.com/hvuhsg/spliti v0.0.0

replace github.com/hvuhsg/spliti => ../engine
`)
	writeFile(t, filepath.Join(dir, "spliti.toml"), `name = "demo"

[window]
title = "Demo"
width = 1024
height = 600

[scene]
file = "game/scenes/main.go"
default = "Main"
`)
	writeFile(t, filepath.Join(dir, "game/scenes/main.go"), `package scenes

//spliti:scene Main
func Main(c *app.Ctx) {}

//spliti:scene Arena
func Arena(c *app.Ctx) {}
`)
	writeFile(t, filepath.Join(dir, "game/components/components.go"), `package components

type Health struct{ Max, Current int }

type Spinner struct{ Speed float32 }

type internal struct{} // unexported: skipped

type Alias = Health // not a struct decl: skipped
`)
	return dir
}

func TestLoadAndGenerate(t *testing.T) {
	dir := scaffoldFixture(t)
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Module != "demo" || p.Config.Scene.Default != "Main" {
		t.Fatalf("project = %+v", p)
	}
	if strings.Join(p.Scenes, ",") != "Main,Arena" {
		t.Fatalf("scenes = %v", p.Scenes)
	}
	if strings.Join(p.Components, ",") != "Health,Spinner" {
		t.Fatalf("components = %v", p.Components)
	}
	if p.EngineVersion != "v0.0.0" || !strings.HasSuffix(p.EngineReplace, "engine") || !filepath.IsAbs(p.EngineReplace) {
		t.Fatalf("engine wiring = %q %q", p.EngineVersion, p.EngineReplace)
	}

	if err := p.Generate(); err != nil {
		t.Fatal(err)
	}
	mustContain := func(file string, wants ...string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(p.EditorDir(), file))
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range wants {
			if !strings.Contains(string(b), w) {
				t.Errorf("%s missing %q", file, w)
			}
		}
	}
	mustContain("go.mod",
		"replace demo => ../..",
		"replace github.com/hvuhsg/spliti => "+p.EngineReplace)
	mustContain("main.go",
		`SetupScene:  scenes.Main`,
		`Scene:       "Main"`,
		`"demo/game"`,
		`Width:  1024`,
		"//go:build !js")
	mustContain("registry_gen.go",
		"registry.Register[components.Health](r, \"Health\", \"components\")",
		"registry.Register[components.Spinner](r, \"Spinner\", \"components\")")
}

func TestLoadRejectsScenelessProject(t *testing.T) {
	dir := scaffoldFixture(t)
	writeFile(t, filepath.Join(dir, "game/scenes/main.go"), "package scenes\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error for project with no scenes")
	}
}
