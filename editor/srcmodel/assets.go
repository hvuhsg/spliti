package srcmodel

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// AssetsFile is the parsed //spliti:assets function — the game's LoadAssets,
// which fills the mesh and material registries:
//
//	//spliti:assets
//	func LoadAssets(c *app.Ctx) {
//		meshes := app.GetResource[render3d.MeshRegistry](c)
//		materials := app.GetResource[render3d.MaterialRegistry](c)
//		must(meshes.Load("crate", render3d.Cube(1)))
//		must(meshes.LoadOBJFile("teapot", "game/assets/teapot.obj"))
//		must(materials.Load("metal", render3d.Material{...}))
//	}
//
// The editor's Assets panel lists every recognized mesh/material entry and
// appends new ones when a file is imported. Each recognized registration is one
// must(<registry>.Load|LoadOBJFile(...)) statement; statements it does not
// recognize are preserved verbatim and never touched.
type AssetsFile struct {
	Path    string
	Pkg     string
	Entries []*AssetEntry

	file        *dst.File
	fn          *dst.FuncDecl
	meshVar     string // local var holding the *render3d.MeshRegistry
	matVar      string // local var holding the *render3d.MaterialRegistry
	lastWritten []byte
}

// AssetKind classifies a registered asset.
type AssetKind string

const (
	AssetMesh     AssetKind = "mesh"
	AssetMaterial AssetKind = "material"
)

// AssetEntry is one recognized registration line.
type AssetEntry struct {
	Key        string
	Kind       AssetKind
	File       string // relative asset path for file-backed meshes; "" otherwise
	Procedural bool   // mesh built from a render3d primitive (Cube/Plane/...)
	Editable   bool   // material literal is a keyed all-literal struct
	removable  bool   // the registration is wholly contained in one statement
	stmt       dst.Stmt
	lit        *dst.CompositeLit // material composite literal, when Editable
}

// Lit returns the material's composite literal, or nil.
func (e *AssetEntry) Lit() *dst.CompositeLit { return e.lit }

// Material reads the editable subset of an editable material's literal. ok is
// false for non-materials or non-literal materials.
func (e *AssetEntry) Material() (MaterialSpec, bool) {
	if e.Kind != AssetMaterial || e.lit == nil {
		return MaterialSpec{}, false
	}
	s := MaterialSpec{A: 1}
	for _, el := range e.lit.Elts {
		kv, ok := el.(*dst.KeyValueExpr)
		if !ok {
			continue
		}
		switch exprName(kv.Key) {
		case "BaseColor":
			if cl, ok := kv.Value.(*dst.CompositeLit); ok {
				for _, ce := range cl.Elts {
					ckv, ok := ce.(*dst.KeyValueExpr)
					if !ok {
						continue
					}
					v, _ := floatLit(ckv.Value)
					switch exprName(ckv.Key) {
					case "R":
						s.R = v
					case "G":
						s.G = v
					case "B":
						s.B = v
					case "A":
						s.A = v
					}
				}
			}
		case "Metallic":
			s.Metallic, _ = floatLit(kv.Value)
		case "Roughness":
			s.Roughness, _ = floatLit(kv.Value)
		case "DoubleSided":
			s.DoubleSided = exprName(kv.Value) == "true"
		}
	}
	return s, true
}

const assetsDirective = "spliti:assets"

// render3dPkg is the package qualifier mesh/material constructors are written
// under; matches the scene grammar's xformPkg.
const render3dPkg = xformPkg

// FindAssetsFile scans dir's top-level .go files for a //spliti:assets directive.
func FindAssetsFile(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		if data, err := os.ReadFile(path); err == nil && bytes.Contains(data, []byte("//"+assetsDirective)) {
			return path, true
		}
	}
	return "", false
}

// LastWritten returns the bytes of the most recent successful Save.
func (af *AssetsFile) LastWritten() []byte { return af.lastWritten }

// ParseAssetsFile reads path and locates its //spliti:assets function.
func ParseAssetsFile(path string) (*AssetsFile, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseAssetsSource(src, path)
}

func parseAssetsSource(src []byte, path string) (*AssetsFile, error) {
	f, err := decorator.ParseFile(token.NewFileSet(), path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	af := &AssetsFile{Path: path, Pkg: f.Name.Name, file: f}
	for _, decl := range f.Decls {
		fn, ok := decl.(*dst.FuncDecl)
		if ok && hasDirective(fn.Decs.Start.All(), assetsDirective) {
			af.fn = fn
			break
		}
	}
	if af.fn == nil {
		return nil, fmt.Errorf("srcmodel: no //%s function in %s", assetsDirective, path)
	}
	af.rescan()
	return af, nil
}

// rescan rebuilds the recognized model from the function body. Mutations
// invalidate entry pointers — re-look-up by key.
func (af *AssetsFile) rescan() {
	af.Entries = nil
	af.meshVar, af.matVar = "", ""
	if af.fn.Body == nil {
		return
	}
	loaderVars := map[string]string{} // var -> OBJ file path
	for _, stmt := range af.fn.Body.List {
		// Registry handles: meshes := app.GetResource[render3d.MeshRegistry](c)
		if v, typ, ok := getResourceAssign(stmt); ok {
			switch typ {
			case render3dPkg + ".MeshRegistry":
				af.meshVar = v
			case render3dPkg + ".MaterialRegistry":
				af.matVar = v
			}
			continue
		}
		// OBJ loads into a local var: teapot, err := render3d.LoadOBJ("path")
		if v, file, ok := objLoaderAssign(stmt); ok {
			loaderVars[v] = file
			continue
		}
		// Registration: must(<registry>.Load|LoadOBJFile(...))
		if e := af.recognizeRegister(stmt, loaderVars); e != nil {
			af.Entries = append(af.Entries, e)
		}
	}
}

// Entry returns the asset registered under key, or nil.
func (af *AssetsFile) Entry(key string) *AssetEntry {
	for _, e := range af.Entries {
		if e.Key == key {
			return e
		}
	}
	return nil
}

// Has reports whether key is registered.
func (af *AssetsFile) Has(key string) bool { return af.Entry(key) != nil }

// getResourceAssign matches `<v> := app.GetResource[<pkg.Type>](c)` and returns
// the variable name and the fully qualified type argument.
func getResourceAssign(stmt dst.Stmt) (varName, typ string, ok bool) {
	as, isAssign := stmt.(*dst.AssignStmt)
	if !isAssign || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return "", "", false
	}
	id, isID := as.Lhs[0].(*dst.Ident)
	if !isID {
		return "", "", false
	}
	call, isCall := as.Rhs[0].(*dst.CallExpr)
	if !isCall {
		return "", "", false
	}
	var fun dst.Expr
	switch idx := call.Fun.(type) {
	case *dst.IndexExpr:
		if exprName(idx.X) != "app.GetResource" {
			return "", "", false
		}
		fun = idx.Index
	case *dst.IndexListExpr:
		if exprName(idx.X) != "app.GetResource" || len(idx.Indices) != 1 {
			return "", "", false
		}
		fun = idx.Indices[0]
	default:
		return "", "", false
	}
	return id.Name, exprName(fun), true
}

// objLoaderAssign matches `<v>[, err] := render3d.LoadOBJ("path")`.
func objLoaderAssign(stmt dst.Stmt) (varName, file string, ok bool) {
	as, isAssign := stmt.(*dst.AssignStmt)
	if !isAssign || len(as.Lhs) < 1 || len(as.Rhs) != 1 {
		return "", "", false
	}
	id, isID := as.Lhs[0].(*dst.Ident)
	if !isID {
		return "", "", false
	}
	call, isCall := as.Rhs[0].(*dst.CallExpr)
	if !isCall {
		return "", "", false
	}
	switch exprName(call.Fun) {
	case render3dPkg + ".LoadOBJ", render3dPkg + ".LoadOBJFS":
		if s, ok := stringLit(lastArg(call)); ok {
			return id.Name, s, true
		}
	}
	return "", "", false
}

// recognizeRegister matches a must(<registry>.Load|LoadOBJFile("key", ...)) line.
func (af *AssetsFile) recognizeRegister(stmt dst.Stmt, loaderVars map[string]string) *AssetEntry {
	es, ok := stmt.(*dst.ExprStmt)
	if !ok {
		return nil
	}
	must, ok := es.X.(*dst.CallExpr)
	if !ok || exprName(must.Fun) != "must" || len(must.Args) != 1 {
		return nil
	}
	call, ok := must.Args[0].(*dst.CallExpr)
	if !ok {
		return nil
	}
	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok {
		return nil
	}
	recv, ok := sel.X.(*dst.Ident)
	if !ok || len(call.Args) < 2 {
		return nil
	}
	key, ok := stringLit(call.Args[0])
	if !ok {
		return nil
	}
	switch {
	case recv.Name == af.meshVar && sel.Sel.Name == "LoadOBJFile":
		file, _ := stringLit(call.Args[1])
		return &AssetEntry{Key: key, Kind: AssetMesh, File: file, removable: true, stmt: stmt}
	case recv.Name == af.meshVar && sel.Sel.Name == "Load":
		file, proc := meshSource(call.Args[1], loaderVars)
		return &AssetEntry{Key: key, Kind: AssetMesh, File: file, Procedural: proc, stmt: stmt}
	case af.matVar != "" && recv.Name == af.matVar && sel.Sel.Name == "Load":
		e := &AssetEntry{Key: key, Kind: AssetMaterial, removable: true, stmt: stmt}
		if lit, ok := call.Args[1].(*dst.CompositeLit); ok && exprName(lit.Type) == render3dPkg+".Material" {
			e.lit = lit
			e.Editable = isKeyedLiteral(lit)
		}
		return e
	}
	return nil
}

// meshSource resolves a meshes.Load second argument to a file path (when it is,
// or traces back to, render3d.LoadOBJ) or flags it procedural (a render3d
// primitive constructor like Cube/Plane).
func meshSource(e dst.Expr, loaderVars map[string]string) (file string, procedural bool) {
	switch v := e.(type) {
	case *dst.Ident:
		if f, ok := loaderVars[v.Name]; ok {
			return f, false
		}
	case *dst.CallExpr:
		name := exprName(v.Fun)
		switch name {
		case render3dPkg + ".LoadOBJ", render3dPkg + ".LoadOBJFS":
			if s, ok := stringLit(lastArg(v)); ok {
				return s, false
			}
		default:
			if strings.HasPrefix(name, render3dPkg+".") {
				return "", true
			}
		}
	}
	return "", false
}

// AddMesh appends a must(meshes.LoadOBJFile(key, relFile)) line. key must not
// already exist; relFile is the asset path relative to the project root.
func (af *AssetsFile) AddMesh(key, relFile string) error {
	if af.Has(key) {
		return fmt.Errorf("srcmodel: asset %q already registered", key)
	}
	if af.meshVar == "" {
		return fmt.Errorf("srcmodel: %s has no mesh registry variable", filepath.Base(af.Path))
	}
	call := registryCall(af.meshVar, "LoadOBJFile",
		strLit(key), strLit(filepath.ToSlash(relFile)))
	af.appendStmt(mustStmt(call))
	af.rescan()
	return nil
}

// MaterialSpec is the editable subset of a render3d.Material the editor writes.
type MaterialSpec struct {
	R, G, B, A          float32
	Metallic, Roughness float32
	DoubleSided         bool
}

// AddMaterial appends a must(materials.Load(key, render3d.Material{...})) line.
func (af *AssetsFile) AddMaterial(key string, spec MaterialSpec) error {
	if af.Has(key) {
		return fmt.Errorf("srcmodel: asset %q already registered", key)
	}
	if af.matVar == "" {
		return fmt.Errorf("srcmodel: %s has no material registry variable", filepath.Base(af.Path))
	}
	call := registryCall(af.matVar, "Load", strLit(key), materialLit(spec))
	af.appendStmt(mustStmt(call))
	af.rescan()
	return nil
}

// SetMaterial rewrites an existing material's literal in place. The entry must
// be an editable material.
func (af *AssetsFile) SetMaterial(key string, spec MaterialSpec) error {
	e := af.Entry(key)
	if e == nil || e.Kind != AssetMaterial || e.lit == nil {
		return fmt.Errorf("srcmodel: %q is not an editable material", key)
	}
	lit := materialLit(spec)
	lit.Decs.NodeDecs = e.lit.Decs.NodeDecs
	// Replace the literal node inside the existing must(...) statement.
	must := e.stmt.(*dst.ExprStmt).X.(*dst.CallExpr)
	call := must.Args[0].(*dst.CallExpr)
	call.Args[1] = lit
	af.rescan()
	return nil
}

// Remove deletes a registration line. Only single-statement entries (a mesh
// loaded via LoadOBJFile, or a material) can be removed cleanly; a mesh whose
// geometry is loaded through a shared local variable is refused, since dropping
// the Load line alone would leave that variable unused (a compile error).
func (af *AssetsFile) Remove(key string) error {
	e := af.Entry(key)
	if e == nil {
		return fmt.Errorf("srcmodel: asset %q not registered", key)
	}
	if !e.removable {
		return fmt.Errorf("srcmodel: asset %q cannot be removed automatically", key)
	}
	body := af.fn.Body
	for i, s := range body.List {
		if s == e.stmt {
			body.List = append(body.List[:i], body.List[i+1:]...)
			break
		}
	}
	af.rescan()
	return nil
}

func (af *AssetsFile) appendStmt(stmt dst.Stmt) {
	stmt.Decorations().Before = dst.NewLine
	stmt.Decorations().After = dst.NewLine
	af.fn.Body.List = append(af.fn.Body.List, stmt)
}

// Save prints the file, verifies the output re-parses to a model with the same
// asset keys, and atomically replaces Path.
func (af *AssetsFile) Save() error {
	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, af.file); err != nil {
		return fmt.Errorf("srcmodel: print %s: %w", af.Path, err)
	}
	out := buf.Bytes()
	check, err := parseAssetsSource(out, af.Path)
	if err != nil {
		return fmt.Errorf("srcmodel: refusing to save %s: output does not re-parse: %w", af.Path, err)
	}
	for _, e := range af.Entries {
		if check.Entry(e.Key) == nil {
			return fmt.Errorf("srcmodel: refusing to save %s: asset %q lost in round trip", af.Path, e.Key)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(af.Path), ".spliti-save-*")
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
	if info, err := os.Stat(af.Path); err == nil {
		os.Chmod(tmpName, info.Mode())
	}
	if err := os.Rename(tmpName, af.Path); err != nil {
		return err
	}
	af.lastWritten = out
	return nil
}

// --- dst construction helpers -------------------------------------------------

func mustStmt(inner dst.Expr) dst.Stmt {
	return &dst.ExprStmt{X: &dst.CallExpr{Fun: dst.NewIdent("must"), Args: []dst.Expr{inner}}}
}

func registryCall(recv, method string, args ...dst.Expr) *dst.CallExpr {
	return &dst.CallExpr{
		Fun:  &dst.SelectorExpr{X: dst.NewIdent(recv), Sel: dst.NewIdent(method)},
		Args: args,
	}
}

func strLit(s string) dst.Expr {
	return &dst.BasicLit{Kind: token.STRING, Value: strconv.Quote(s)}
}

// materialLit builds render3d.Material{ BaseColor: render3d.Color{...}, ... }.
func materialLit(s MaterialSpec) *dst.CompositeLit {
	color := &dst.CompositeLit{
		Type: &dst.SelectorExpr{X: dst.NewIdent(render3dPkg), Sel: dst.NewIdent("Color")},
		Elts: []dst.Expr{
			keyed("R", floatExpr(s.R)), keyed("G", floatExpr(s.G)),
			keyed("B", floatExpr(s.B)), keyed("A", floatExpr(s.A)),
		},
	}
	elts := []dst.Expr{keyed("BaseColor", color)}
	if s.Metallic != 0 {
		elts = append(elts, keyed("Metallic", floatExpr(s.Metallic)))
	}
	if s.Roughness != 0 {
		elts = append(elts, keyed("Roughness", floatExpr(s.Roughness)))
	}
	if s.DoubleSided {
		elts = append(elts, keyed("DoubleSided", dst.NewIdent("true")))
	}
	return &dst.CompositeLit{
		Type: &dst.SelectorExpr{X: dst.NewIdent(render3dPkg), Sel: dst.NewIdent("Material")},
		Elts: elts,
	}
}

func keyed(key string, val dst.Expr) dst.Expr {
	return &dst.KeyValueExpr{Key: dst.NewIdent(key), Value: val}
}

// lastArg returns a call's final argument, or nil.
func lastArg(call *dst.CallExpr) dst.Expr {
	if len(call.Args) == 0 {
		return nil
	}
	return call.Args[len(call.Args)-1]
}

// isKeyedLiteral reports whether every element of a composite literal is a
// key:value pair (so the editor can rewrite it field by field).
func isKeyedLiteral(lit *dst.CompositeLit) bool {
	for _, el := range lit.Elts {
		if _, ok := el.(*dst.KeyValueExpr); !ok {
			return false
		}
	}
	return true
}
