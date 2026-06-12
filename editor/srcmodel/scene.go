// Package srcmodel parses scene source files into an editable model and writes
// edits back, preserving everything it does not understand. It is the half of
// the editor that makes Go source the scene format: the model is a thin,
// order-preserving view over the dst AST (github.com/dave/dst keeps comments
// attached through mutation), and every write goes through a reparse
// verification so the editor can never save a file it cannot read back.
//
// Recognition is structural and conservative. A statement the parser does not
// recognize — any helper call with non-literal arguments, control flow, plain
// Go — is kept verbatim as an opaque statement and never touched. Recognized
// grammar in M1: the spawn form
//
//	crate := scene.Spawn(c, "crate1", entities.SpawnCrate(c,
//	    render3d.XForm().At(-1.4, 0.6, 0).EulerDeg(0, 30, 0)))
//
// (also with `_ =` or no assignment), whose transform chain can be rewritten
// in place and whose siblings can be extended with new spawn lines.
package srcmodel

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// SceneFile is one parsed Go source file holding scene setup functions.
type SceneFile struct {
	Path   string
	Pkg    string
	Scenes []*Scene

	file *dst.File
}

// Scene is one //spliti:scene function inside a SceneFile.
type Scene struct {
	Name     string // directive argument, or the function name
	FuncName string
	Spawns   []*Spawn

	fn   *dst.FuncDecl
	ctx  string // name of the *app.Ctx parameter, used when emitting new lines
	file *SceneFile
}

// Spawn is one recognized scene.Spawn statement.
type Spawn struct {
	Instance  string // the string-literal instance name (stable identity)
	Var       string // assigned variable name; "" for `_ =` or bare-call forms
	Prefab    string // prefab function as written, e.g. "entities.SpawnCrate"
	Transform *Transform

	stmt       dst.Stmt
	prefabCall *dst.CallExpr
}

// Transform is the parsed render3d.XForm() builder chain on a spawn line. Nil
// pointer fields mean the call is absent from the chain (defaults apply).
// Editable reports whether the chain consisted purely of literal arguments —
// only then may SetTransform rewrite it.
type Transform struct {
	At       *[3]float32
	EulerDeg *[3]float32
	Scaled   *[3]float32
	Rot      *[4]float32
	Editable bool
}

// scenePkg/xformPkg are the package qualifiers the grammar is recognized
// under. Scene files import them by their canonical names.
const (
	scenePkg = "scene"
	xformPkg = "render3d"
)

// ParseSceneFile reads and parses path.
func ParseSceneFile(path string) (*SceneFile, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSceneSource(src, path)
}

// ParseSceneSource parses src (path is recorded for Save and error messages).
func ParseSceneSource(src []byte, path string) (*SceneFile, error) {
	f, err := decorator.ParseFile(token.NewFileSet(), path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	sf := &SceneFile{Path: path, Pkg: f.Name.Name, file: f}
	for _, decl := range f.Decls {
		fn, ok := decl.(*dst.FuncDecl)
		if !ok {
			continue
		}
		name, ok := sceneDirective(fn)
		if !ok {
			continue
		}
		sc := &Scene{Name: name, FuncName: fn.Name.Name, fn: fn, ctx: ctxParamName(fn), file: sf}
		sc.scanSpawns()
		sf.Scenes = append(sf.Scenes, sc)
	}
	return sf, nil
}

// Scene returns the scene with the given name, or nil.
func (f *SceneFile) Scene(name string) *Scene {
	for _, s := range f.Scenes {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// Bytes prints the file back to source.
func (f *SceneFile) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, f.file); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Save prints the file, verifies the output re-parses to a model that still
// contains every scene, and atomically replaces Path. The editor must never
// write a scene file it cannot read back; any failure leaves the file on disk
// untouched.
func (f *SceneFile) Save() error {
	out, err := f.Bytes()
	if err != nil {
		return fmt.Errorf("srcmodel: print %s: %w", f.Path, err)
	}
	check, err := ParseSceneSource(out, f.Path)
	if err != nil {
		return fmt.Errorf("srcmodel: refusing to save %s: output does not re-parse: %w", f.Path, err)
	}
	for _, s := range f.Scenes {
		if check.Scene(s.Name) == nil {
			return fmt.Errorf("srcmodel: refusing to save %s: scene %q lost in round trip", f.Path, s.Name)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), ".spliti-save-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if info, err := os.Stat(f.Path); err == nil {
		os.Chmod(tmpName, info.Mode())
	}
	return os.Rename(tmpName, f.Path)
}

// Spawn returns the spawn statement for the given instance name, or nil.
func (s *Scene) Spawn(instance string) *Spawn {
	for _, sp := range s.Spawns {
		if sp.Instance == instance {
			return sp
		}
	}
	return nil
}

// sceneDirective reports whether fn carries a //spliti:scene directive and
// returns the scene name (directive argument, defaulting to the func name).
func sceneDirective(fn *dst.FuncDecl) (string, bool) {
	for _, c := range fn.Decs.Start.All() {
		text := strings.TrimSpace(strings.TrimPrefix(c, "//"))
		if text == "spliti:scene" {
			return fn.Name.Name, true
		}
		if rest, ok := strings.CutPrefix(text, "spliti:scene "); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// ctxParamName returns the name of fn's first parameter (the *app.Ctx),
// defaulting to "c".
func ctxParamName(fn *dst.FuncDecl) string {
	if fn.Type.Params != nil && len(fn.Type.Params.List) > 0 {
		p := fn.Type.Params.List[0]
		if len(p.Names) > 0 {
			return p.Names[0].Name
		}
	}
	return "c"
}

// scanSpawns walks the scene function body and records every recognized
// scene.Spawn statement, in order. Unrecognized statements are left alone.
func (s *Scene) scanSpawns() {
	if s.fn.Body == nil {
		return
	}
	for _, stmt := range s.fn.Body.List {
		if sp := recognizeSpawn(stmt); sp != nil {
			s.Spawns = append(s.Spawns, sp)
		}
	}
}

// recognizeSpawn matches the three accepted spawn statement shapes:
//
//	v := scene.Spawn(c, "name", prefab(c, <xform>, ...))
//	_ = scene.Spawn(c, "name", prefab(c, <xform>, ...))
//	scene.Spawn(c, "name", prefab(c, <xform>, ...))
func recognizeSpawn(stmt dst.Stmt) *Spawn {
	var call *dst.CallExpr
	varName := ""
	switch v := stmt.(type) {
	case *dst.AssignStmt:
		if len(v.Lhs) != 1 || len(v.Rhs) != 1 {
			return nil
		}
		id, ok := v.Lhs[0].(*dst.Ident)
		if !ok {
			return nil
		}
		if id.Name != "_" {
			varName = id.Name
		}
		call, ok = v.Rhs[0].(*dst.CallExpr)
		if !ok {
			return nil
		}
	case *dst.ExprStmt:
		var ok bool
		call, ok = v.X.(*dst.CallExpr)
		if !ok {
			return nil
		}
	default:
		return nil
	}

	if !isPkgCall(call, scenePkg, "Spawn") || len(call.Args) != 3 {
		return nil
	}
	name, ok := stringLit(call.Args[1])
	if !ok {
		return nil
	}
	prefabCall, ok := call.Args[2].(*dst.CallExpr)
	if !ok {
		return nil
	}
	prefab := exprName(prefabCall.Fun)
	if prefab == "" || len(prefabCall.Args) < 1 {
		return nil
	}
	sp := &Spawn{
		Instance:   name,
		Var:        varName,
		Prefab:     prefab,
		stmt:       stmt,
		prefabCall: prefabCall,
	}
	if len(prefabCall.Args) >= 2 {
		sp.Transform = parseTransformChain(prefabCall.Args[1])
	}
	return sp
}

// isPkgCall reports whether call is pkg.fn(...).
func isPkgCall(call *dst.CallExpr, pkg, fn string) bool {
	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*dst.Ident)
	return ok && id.Name == pkg && sel.Sel.Name == fn
}

// exprName renders an identifier or pkg.Name selector to its source text;
// anything else yields "".
func exprName(e dst.Expr) string {
	switch v := e.(type) {
	case *dst.Ident:
		return v.Name
	case *dst.SelectorExpr:
		if id, ok := v.X.(*dst.Ident); ok {
			return id.Name + "." + v.Sel.Name
		}
	}
	return ""
}

func stringLit(e dst.Expr) (string, bool) {
	lit, ok := e.(*dst.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}
