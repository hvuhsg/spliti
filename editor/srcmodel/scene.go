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

	file        *dst.File
	lastWritten []byte
}

// LastWritten returns the bytes of the most recent successful Save — file
// watchers compare against it to ignore the editor's own writes.
func (f *SceneFile) LastWritten() []byte { return f.lastWritten }

// Scene is one //spliti:scene function inside a SceneFile.
type Scene struct {
	Name     string // directive argument, or the function name
	FuncName string
	Spawns   []*Spawn

	fn   *dst.FuncDecl
	ctx  string // name of the *app.Ctx parameter, used when emitting new lines
	file *SceneFile
	// layers, when set (SetLayers), lets scene.Set literals use the game's
	// named layer constants — both when encoding and when judging editability.
	layers *Layers
}

// Spawn is one recognized scene.Spawn statement, together with the recognized
// attachment lines (scene.Set / scene.Remove / scene.Parent) that reference its
// variable.
type Spawn struct {
	Instance  string // the string-literal instance name (stable identity)
	Var       string // assigned variable name; "" for `_ =` or bare-call forms
	Prefab    string // prefab function as written, e.g. "entities.SpawnCrate"
	Transform *Transform

	Sets       []*SetLine    // scene.Set override lines, in source order
	Removes    []*RemoveLine // scene.Remove[T] lines, in source order
	ParentLine *ParentLine   // scene.Parent line naming this spawn as child, if any

	stmt       dst.Stmt
	prefabCall *dst.CallExpr
}

// Set returns the spawn's scene.Set line for the given component type, or nil.
func (sp *Spawn) Set(typeName string) *SetLine {
	for _, sl := range sp.Sets {
		if sl.Type == typeName {
			return sl
		}
	}
	return nil
}

// Removed returns the spawn's scene.Remove line for the given type, or nil.
func (sp *Spawn) Removed(typeName string) *RemoveLine {
	for _, rl := range sp.Removes {
		if rl.Type == typeName {
			return rl
		}
	}
	return nil
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
		sc.rescan()
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
// untouched. Imports left orphaned by removed scene lines are pruned first —
// an unused import would make the saved file uncompilable.
func (f *SceneFile) Save() error {
	pruneUnusedImports(f.file)
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
	if err := os.Rename(tmpName, f.Path); err != nil {
		return err
	}
	f.lastWritten = out
	return nil
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

// rescan walks the scene function body and rebuilds the recognized model:
// spawn statements in order, then the attachment lines (scene.Set/Remove/
// Parent) that reference spawn variables. Go's declare-before-use guarantees a
// single pass suffices. Unrecognized statements are left alone. Every
// structural mutation re-runs this, so model pointers are invalidated by
// mutation — always re-look-up by instance name.
func (s *Scene) rescan() {
	s.Spawns = nil
	if s.fn.Body == nil {
		return
	}
	byVar := map[string]*Spawn{}
	vars := map[string]bool{}
	for _, stmt := range s.fn.Body.List {
		if sp := recognizeSpawn(stmt); sp != nil {
			s.Spawns = append(s.Spawns, sp)
			if sp.Var != "" {
				byVar[sp.Var] = sp
				vars[sp.Var] = true
			}
			continue
		}
		if sl := recognizeSet(stmt, vars, s.layers); sl != nil {
			if sp := byVar[sl.ref]; sp != nil {
				sp.Sets = append(sp.Sets, sl)
			}
			continue
		}
		if rl := recognizeRemove(stmt); rl != nil {
			if sp := byVar[rl.ref]; sp != nil {
				sp.Removes = append(sp.Removes, rl)
			}
			continue
		}
		if pl := recognizeParent(stmt); pl != nil {
			child, parent := byVar[pl.childRef], byVar[pl.parentRef]
			if child != nil && parent != nil && child.ParentLine == nil {
				pl.Parent = parent.Instance
				child.ParentLine = pl
			}
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
