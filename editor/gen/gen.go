// Package gen scans a spliti game project and (re)generates its editor
// target: .spliti/editor/main.go wires the game's packages into editor.Plugin,
// and .spliti/editor/registry_gen.go registers every exported component struct
// so the inspector can edit them. `spliti gen` (and `spliti edit`, which runs
// gen first) call into here.
//
// Scanning is syntactic (go/parser), matching the editor's conventions:
// components are the exported structs of game/components, scenes are the
// //spliti:scene functions of the configured scene file.
package gen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"
	"github.com/hvuhsg/spliti/editor/srcmodel"
)

// Config mirrors spliti.toml.
type Config struct {
	Name   string `toml:"name"`
	Window struct {
		Title  string `toml:"title"`
		Width  int    `toml:"width"`
		Height int    `toml:"height"`
	} `toml:"window"`
	Scene struct {
		File    string `toml:"file"`
		Default string `toml:"default"`
	} `toml:"scene"`
}

// Project is everything gen learned about a game project.
type Project struct {
	Root       string
	Module     string
	Config     Config
	Scenes     []string // //spliti:scene names in the configured scene file
	Components []string // exported struct names in game/components
	Prefabs    []string // //spliti:entity function names in game/entities
	InputFunc  string   // //spliti:input function name in game/, "" when none

	// Engine module wiring, read from the game's go.mod, for the generated
	// target's own go.mod (see Generate).
	EngineVersion string // require version of github.com/hvuhsg/spliti
	EngineReplace string // absolute replace path, "" when none
}

// Load reads go.mod, spliti.toml, and scans the project under root.
func Load(root string) (*Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	p := &Project{Root: abs}

	mod, err := os.ReadFile(filepath.Join(abs, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("gen: not a Go module: %w", err)
	}
	p.Module = modulePath(mod)
	if p.Module == "" {
		return nil, fmt.Errorf("gen: no module line in go.mod")
	}
	p.EngineVersion, p.EngineReplace = engineWiring(mod, abs)

	if _, err := toml.DecodeFile(filepath.Join(abs, "spliti.toml"), &p.Config); err != nil {
		return nil, fmt.Errorf("gen: read spliti.toml: %w", err)
	}
	if p.Config.Scene.File == "" {
		p.Config.Scene.File = "game/scenes/main.go"
	}

	sf, err := srcmodel.ParseSceneFile(filepath.Join(abs, p.Config.Scene.File))
	if err != nil {
		return nil, fmt.Errorf("gen: parse scene file: %w", err)
	}
	for _, s := range sf.Scenes {
		p.Scenes = append(p.Scenes, s.Name)
	}
	if len(p.Scenes) == 0 {
		return nil, fmt.Errorf("gen: no //spliti:scene functions in %s", p.Config.Scene.File)
	}
	if p.Config.Scene.Default == "" {
		p.Config.Scene.Default = p.Scenes[0]
	}

	p.Components, err = exportedStructs(filepath.Join(abs, "game", "components"))
	if err != nil {
		return nil, err
	}
	p.Prefabs, err = prefabFuncs(filepath.Join(abs, "game", "entities"))
	if err != nil {
		return nil, err
	}
	p.InputFunc = directiveFunc(filepath.Join(abs, "game"), "spliti:input")
	return p, nil
}

// directiveFunc returns the name of the first exported function in dir's
// package carrying the given //directive, or "".
func directiveFunc(dir, directive string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() || fn.Recv != nil || fn.Doc == nil {
				continue
			}
			for _, cm := range fn.Doc.List {
				if strings.TrimSpace(strings.TrimPrefix(cm.Text, "//")) == directive {
					return fn.Name.Name
				}
			}
		}
	}
	return ""
}

// Generate writes .spliti/editor/{go.mod,main.go,registry_gen.go}.
//
// The target is its own nested Go module, not a package of the game module:
// the go tool ignores dot-directories entirely, and the nesting keeps
// editor-only dependencies (dst, cimgui) out of the shipped game's go.mod.
// Its go.mod points at the game via a relative replace and at the engine via
// the same require/replace the game uses. EditorDir's go.sum is maintained by
// `go mod tidy` run there (spliti gen does this).
func (p *Project) Generate() error {
	dir := p.EditorDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := p.render(gomodTmpl, filepath.Join(dir, "go.mod")); err != nil {
		return err
	}
	if err := p.render(mainTmpl, filepath.Join(dir, "main.go")); err != nil {
		return err
	}
	return p.render(registryTmpl, filepath.Join(dir, "registry_gen.go"))
}

// EditorDir is where the generated editor target lives.
func (p *Project) EditorDir() string { return filepath.Join(p.Root, ".spliti", "editor") }

func (p *Project) render(t *template.Template, path string) error {
	var buf bytes.Buffer
	if err := t.Execute(&buf, p); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// DefaultSceneFunc returns the func name of the default scene (scene names and
// func names coincide unless the directive renames; M1 templates keep them
// equal, so the name is used directly).
func (p *Project) DefaultSceneFunc() string { return p.Config.Scene.Default }

// engineWiring extracts the game's require version and replace path (if any)
// for the engine module, so the generated go.mod can mirror them. Relative
// replace paths are resolved against the game root.
func engineWiring(gomod []byte, root string) (version, replace string) {
	version = "v0.0.0"
	const engine = "github.com/hvuhsg/spliti"
	for _, line := range strings.Split(string(gomod), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "replace "); ok {
			parts := strings.SplitN(rest, "=>", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == engine {
				replace = strings.TrimSpace(parts[1])
				if !filepath.IsAbs(replace) {
					replace = filepath.Join(root, replace)
				}
			}
			continue
		}
		line = strings.TrimPrefix(line, "require ")
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == engine {
			version = fields[1]
		}
	}
	return version, replace
}

func modulePath(gomod []byte) string {
	for _, line := range strings.Split(string(gomod), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// exportedStructs lists the exported struct type names declared in dir's
// package. A missing directory is fine (no game components yet).
func exportedStructs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("gen: parse %s: %w", e.Name(), err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !ts.Name.IsExported() {
					continue
				}
				if _, ok := ts.Type.(*ast.StructType); ok {
					out = append(out, ts.Name.Name)
				}
			}
		}
	}
	return out, nil
}

// prefabFuncs lists the exported //spliti:entity functions of dir's package:
// func SpawnX(c *app.Ctx, t render3d.Transform3D) ecs.Entity. The signature is
// checked by arity only (param/result counts); the generated code's compile
// enforces the rest. A missing directory is fine.
func prefabFuncs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("gen: parse %s: %w", e.Name(), err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() || fn.Recv != nil || fn.Doc == nil {
				continue
			}
			directive := false
			for _, cm := range fn.Doc.List {
				if strings.TrimSpace(strings.TrimPrefix(cm.Text, "//")) == "spliti:entity" {
					directive = true
				}
			}
			if !directive {
				continue
			}
			if fn.Type.Params == nil || len(fn.Type.Params.List) != 2 ||
				fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
				continue
			}
			out = append(out, fn.Name.Name)
		}
	}
	return out, nil
}

var gomodTmpl = template.Must(template.New("gomod").Parse(`module spliti-editor-target

go 1.25.0

require (
	github.com/hvuhsg/spliti {{.EngineVersion}}
	{{.Module}} v0.0.0
)

replace {{.Module}} => ../..
{{- if .EngineReplace}}

replace github.com/hvuhsg/spliti => {{.EngineReplace}}
{{- end}}
`))

var mainTmpl = template.Must(template.New("main").Parse(`// Code generated by spliti gen; DO NOT EDIT.
//go:build !js

package main

import (
	"runtime"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor"
{{- if .InputFunc}}
	"github.com/hvuhsg/spliti/plugin/inputs/actions"
{{- end}}
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/plugin/ui"

	"{{.Module}}/game"
	"{{.Module}}/game/scenes"
)

func init() { runtime.LockOSThread() }

func main() {
	a := app.New()
	a.AddPlugins(
		splititime.Plugin{},
		render3d.Plugin{
			Width:  {{if .Config.Window.Width}}{{.Config.Window.Width}}{{else}}1280{{end}},
			Height: {{if .Config.Window.Height}}{{.Config.Window.Height}}{{else}}720{{end}},
			Title:  "{{if .Config.Window.Title}}{{.Config.Window.Title}}{{else}}{{.Config.Name}}{{end}} — spliti editor",
			Ambient: m.Vec3{X: 0.05, Y: 0.05, Z: 0.06},
			Samples: 4,
			VSync:   true,
		},
		ui.Plugin{},
{{- if .InputFunc}}
		actions.Plugin{Map: game.{{.InputFunc}}()},
{{- end}}
		editor.Plugin{
			ProjectRoot: {{printf "%q" .Root}},
			SceneFile:   "{{.Config.Scene.File}}",
			Scene:       "{{.Config.Scene.Default}}",
			SetupScene:  scenes.{{.DefaultSceneFunc}},
			LoadAssets:  game.LoadAssets,
			GameSystems: game.RegisterSystems,
			Registry:    buildRegistry(),
			Prefabs:     buildPrefabs(),
			GamePkg:     "{{.Module}}/game",
		},
	)
	a.Run()
}
`))

var registryTmpl = template.Must(template.New("registry").Parse(`// Code generated by spliti gen; DO NOT EDIT.
//go:build !js

package main

import (
	"github.com/hvuhsg/spliti/editor"
	"github.com/hvuhsg/spliti/editor/registry"
{{- if or .Components .Prefabs}}
{{end}}
{{- if .Components}}
	"{{.Module}}/game/components"
{{- end}}
{{- if .Prefabs}}
	"{{.Module}}/game/entities"
{{- end}}
)

func buildRegistry() *registry.Registry {
	r := registry.New()
	registry.Builtin(r)
{{- range .Components}}
	registry.Register[components.{{.}}](r, "{{.}}", "components")
{{- end}}
	return r
}

func buildPrefabs() map[string]editor.PrefabFunc {
	return map[string]editor.PrefabFunc{
{{- range .Prefabs}}
		"entities.{{.}}": entities.{{.}},
{{- end}}
	}
}
`))
