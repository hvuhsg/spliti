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

// InputFile is the parsed //spliti:input function: the game's action-binding
// table, recognized statement by statement:
//
//	//spliti:input
//	func BuildActions() *actions.Map {
//		m := actions.NewMap()
//		m.Bind("jump", actions.Key(inputs.KeySpace), actions.Pad(inputs.GamepadA))
//		m.BindAxis("move-x",
//			actions.ButtonAxis(actions.Key(inputs.KeyA), actions.Key(inputs.KeyD)),
//			actions.PadAxis(inputs.AxisLeftX))
//		return m
//	}
//
// Unrecognized statements are preserved verbatim; a Bind line with computed
// arguments parses as non-editable and the editor leaves it alone.
type InputFile struct {
	Path    string
	Actions []*ActionBinding // m.Bind lines, in source order
	Axes    []*AxisBinding   // m.BindAxis lines, in source order

	file        *dst.File
	fn          *dst.FuncDecl
	recv        string // the map variable ("m")
	newMapStmt  dst.Stmt
	lastWritten []byte
}

// SourceRef is one button-like binding source as written in code.
type SourceRef struct {
	Kind string // "key", "mouse", "pad"
	Name string // inputs identifier: "KeySpace", "MouseButtonLeft", "GamepadA"
}

// AxisSourceRef is one axis binding source as written in code.
type AxisSourceRef struct {
	Kind     string    // "buttons", "pad"
	Neg, Pos SourceRef // buttons form
	Axis     string    // pad form: inputs identifier ("AxisLeftX")
	Inverted bool
}

// ActionBinding is one recognized m.Bind line.
type ActionBinding struct {
	Name     string
	Sources  []SourceRef
	Editable bool
	stmt     dst.Stmt
}

// AxisBinding is one recognized m.BindAxis line.
type AxisBinding struct {
	Name     string
	Sources  []AxisSourceRef
	Editable bool
	stmt     dst.Stmt
}

const (
	inputDirective = "spliti:input"
	actionsPkg     = "actions"
	inputsPkg      = "inputs"

	actionsImportPath = "github.com/hvuhsg/spliti/plugin/inputs/actions"
	inputsImportPath  = "github.com/hvuhsg/spliti/plugin/inputs"
)

// FindInputFile scans dir's top-level .go files for a //spliti:input
// directive and returns the file path holding it.
func FindInputFile(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if data, err := os.ReadFile(path); err == nil && bytes.Contains(data, []byte("//"+inputDirective)) {
			return path, true
		}
	}
	return "", false
}

// ParseInputFile reads path and locates its //spliti:input function.
func ParseInputFile(path string) (*InputFile, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseInputSource(src, path)
}

func parseInputSource(src []byte, path string) (*InputFile, error) {
	f, err := decorator.ParseFile(token.NewFileSet(), path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	inf := &InputFile{Path: path, file: f}
	for _, decl := range f.Decls {
		fn, ok := decl.(*dst.FuncDecl)
		if ok && hasDirective(fn.Decs.Start.All(), inputDirective) {
			inf.fn = fn
			break
		}
	}
	if inf.fn == nil {
		return nil, fmt.Errorf("srcmodel: no //%s function in %s", inputDirective, path)
	}
	inf.rescan()
	if inf.recv == "" {
		return nil, fmt.Errorf("srcmodel: %s: no `<v> := actions.NewMap()` statement", path)
	}
	return inf, nil
}

// rescan rebuilds the recognized model from the function body. Mutations
// invalidate binding pointers — re-look-up by action name.
func (inf *InputFile) rescan() {
	inf.Actions, inf.Axes = nil, nil
	inf.recv, inf.newMapStmt = "", nil
	if inf.fn.Body == nil {
		return
	}
	for _, stmt := range inf.fn.Body.List {
		if inf.recv == "" {
			if as, ok := stmt.(*dst.AssignStmt); ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 {
				if id, ok := as.Lhs[0].(*dst.Ident); ok {
					if call, ok := as.Rhs[0].(*dst.CallExpr); ok && exprName(call.Fun) == actionsPkg+".NewMap" {
						inf.recv, inf.newMapStmt = id.Name, stmt
					}
				}
			}
			continue // bindings before NewMap can't reference the map
		}
		call := exprStmtCall(stmt)
		if call == nil || len(call.Args) < 1 {
			continue
		}
		sel, ok := call.Fun.(*dst.SelectorExpr)
		if !ok {
			continue
		}
		if recv, ok := sel.X.(*dst.Ident); !ok || recv.Name != inf.recv {
			continue
		}
		name, ok := stringLit(call.Args[0])
		if !ok {
			continue
		}
		switch sel.Sel.Name {
		case "Bind":
			ab := &ActionBinding{Name: name, Editable: true, stmt: stmt}
			for _, arg := range call.Args[1:] {
				src, ok := parseSourceExpr(arg)
				if !ok {
					ab.Editable = false
					continue
				}
				ab.Sources = append(ab.Sources, src)
			}
			inf.Actions = append(inf.Actions, ab)
		case "BindAxis":
			ax := &AxisBinding{Name: name, Editable: true, stmt: stmt}
			for _, arg := range call.Args[1:] {
				src, ok := parseAxisSourceExpr(arg)
				if !ok {
					ax.Editable = false
					continue
				}
				ax.Sources = append(ax.Sources, src)
			}
			inf.Axes = append(inf.Axes, ax)
		}
	}
}

// Action returns the binding for name, or nil.
func (inf *InputFile) Action(name string) *ActionBinding {
	for _, ab := range inf.Actions {
		if ab.Name == name {
			return ab
		}
	}
	return nil
}

// Axis returns the axis binding for name, or nil.
func (inf *InputFile) Axis(name string) *AxisBinding {
	for _, ax := range inf.Axes {
		if ax.Name == name {
			return ax
		}
	}
	return nil
}

func parseSourceExpr(e dst.Expr) (SourceRef, bool) {
	call, ok := e.(*dst.CallExpr)
	if !ok || len(call.Args) != 1 {
		return SourceRef{}, false
	}
	arg, ok := strings.CutPrefix(exprName(call.Args[0]), inputsPkg+".")
	if !ok || arg == "" {
		return SourceRef{}, false
	}
	switch exprName(call.Fun) {
	case actionsPkg + ".Key":
		return SourceRef{Kind: "key", Name: arg}, true
	case actionsPkg + ".Mouse":
		return SourceRef{Kind: "mouse", Name: arg}, true
	case actionsPkg + ".Pad":
		return SourceRef{Kind: "pad", Name: arg}, true
	}
	return SourceRef{}, false
}

func parseAxisSourceExpr(e dst.Expr) (AxisSourceRef, bool) {
	inverted := false
	if call, ok := e.(*dst.CallExpr); ok && len(call.Args) == 0 {
		if sel, ok := call.Fun.(*dst.SelectorExpr); ok && sel.Sel.Name == "Inverted" {
			inverted = true
			e = sel.X
		}
	}
	call, ok := e.(*dst.CallExpr)
	if !ok {
		return AxisSourceRef{}, false
	}
	switch exprName(call.Fun) {
	case actionsPkg + ".ButtonAxis":
		if len(call.Args) != 2 {
			return AxisSourceRef{}, false
		}
		neg, ok1 := parseSourceExpr(call.Args[0])
		pos, ok2 := parseSourceExpr(call.Args[1])
		if !ok1 || !ok2 {
			return AxisSourceRef{}, false
		}
		return AxisSourceRef{Kind: "buttons", Neg: neg, Pos: pos, Inverted: inverted}, true
	case actionsPkg + ".PadAxis":
		if len(call.Args) != 1 {
			return AxisSourceRef{}, false
		}
		axis, ok := strings.CutPrefix(exprName(call.Args[0]), inputsPkg+".")
		if !ok || axis == "" {
			return AxisSourceRef{}, false
		}
		return AxisSourceRef{Kind: "pad", Axis: axis, Inverted: inverted}, true
	}
	return AxisSourceRef{}, false
}

func sourceExpr(s SourceRef) (dst.Expr, error) {
	var fn string
	switch s.Kind {
	case "key":
		fn = "Key"
	case "mouse":
		fn = "Mouse"
	case "pad":
		fn = "Pad"
	default:
		return nil, fmt.Errorf("srcmodel: unknown source kind %q", s.Kind)
	}
	if !validIdent(s.Name) {
		return nil, fmt.Errorf("srcmodel: invalid input identifier %q", s.Name)
	}
	return &dst.CallExpr{
		Fun:  &dst.SelectorExpr{X: dst.NewIdent(actionsPkg), Sel: dst.NewIdent(fn)},
		Args: []dst.Expr{&dst.SelectorExpr{X: dst.NewIdent(inputsPkg), Sel: dst.NewIdent(s.Name)}},
	}, nil
}

func axisSourceExpr(s AxisSourceRef) (dst.Expr, error) {
	var e dst.Expr
	switch s.Kind {
	case "buttons":
		neg, err := sourceExpr(s.Neg)
		if err != nil {
			return nil, err
		}
		pos, err := sourceExpr(s.Pos)
		if err != nil {
			return nil, err
		}
		e = &dst.CallExpr{
			Fun:  &dst.SelectorExpr{X: dst.NewIdent(actionsPkg), Sel: dst.NewIdent("ButtonAxis")},
			Args: []dst.Expr{neg, pos},
		}
	case "pad":
		if !validIdent(s.Axis) {
			return nil, fmt.Errorf("srcmodel: invalid axis identifier %q", s.Axis)
		}
		e = &dst.CallExpr{
			Fun:  &dst.SelectorExpr{X: dst.NewIdent(actionsPkg), Sel: dst.NewIdent("PadAxis")},
			Args: []dst.Expr{&dst.SelectorExpr{X: dst.NewIdent(inputsPkg), Sel: dst.NewIdent(s.Axis)}},
		}
	default:
		return nil, fmt.Errorf("srcmodel: unknown axis source kind %q", s.Kind)
	}
	if s.Inverted {
		e = &dst.CallExpr{Fun: &dst.SelectorExpr{X: e, Sel: dst.NewIdent("Inverted")}}
	}
	return e, nil
}

// SetBinding upserts the m.Bind line for an action. An empty source list is
// allowed (the action exists but is unbound).
func (inf *InputFile) SetBinding(name string, sources []SourceRef) error {
	args := []dst.Expr{&dst.BasicLit{Kind: token.STRING, Value: strconv.Quote(name)}}
	for _, s := range sources {
		e, err := sourceExpr(s)
		if err != nil {
			return err
		}
		args = append(args, e)
	}
	return inf.upsert("Bind", name, args, inf.Action(name).stmtOrNil())
}

// SetAxisBinding upserts the m.BindAxis line for an axis action.
func (inf *InputFile) SetAxisBinding(name string, sources []AxisSourceRef) error {
	args := []dst.Expr{&dst.BasicLit{Kind: token.STRING, Value: strconv.Quote(name)}}
	for _, s := range sources {
		e, err := axisSourceExpr(s)
		if err != nil {
			return err
		}
		args = append(args, e)
	}
	return inf.upsert("BindAxis", name, args, inf.Axis(name).stmtOrNil())
}

func (ab *ActionBinding) stmtOrNil() dst.Stmt {
	if ab == nil {
		return nil
	}
	return ab.stmt
}

func (ax *AxisBinding) stmtOrNil() dst.Stmt {
	if ax == nil {
		return nil
	}
	return ax.stmt
}

func (inf *InputFile) upsert(method, name string, args []dst.Expr, existing dst.Stmt) error {
	ensureImport(inf.file, actionsImportPath)
	if len(args) > 1 {
		ensureImport(inf.file, inputsImportPath)
	}
	if existing != nil {
		call := exprStmtCall(existing)
		call.Args = args
		inf.rescan()
		return nil
	}
	stmt := &dst.ExprStmt{X: &dst.CallExpr{
		Fun:  &dst.SelectorExpr{X: dst.NewIdent(inf.recv), Sel: dst.NewIdent(method)},
		Args: args,
	}}
	stmt.Decs.Before = dst.NewLine
	stmt.Decs.After = dst.NewLine
	// Insert after the last recognized binding (or the NewMap line).
	at := inf.stmtIndex(inf.newMapStmt)
	for _, ab := range inf.Actions {
		if i := inf.stmtIndex(ab.stmt); i > at {
			at = i
		}
	}
	for _, ax := range inf.Axes {
		if i := inf.stmtIndex(ax.stmt); i > at {
			at = i
		}
	}
	body := inf.fn.Body.List
	pos := at + 1
	if pos > len(body) {
		pos = len(body)
	}
	inf.fn.Body.List = append(body[:pos], append([]dst.Stmt{stmt}, body[pos:]...)...)
	inf.rescan()
	return nil
}

// RemoveBinding deletes the action's Bind line. Reports whether one existed.
func (inf *InputFile) RemoveBinding(name string) bool {
	ab := inf.Action(name)
	if ab == nil {
		return false
	}
	inf.deleteStmt(ab.stmt)
	inf.rescan()
	return true
}

// RemoveAxisBinding deletes the action's BindAxis line.
func (inf *InputFile) RemoveAxisBinding(name string) bool {
	ax := inf.Axis(name)
	if ax == nil {
		return false
	}
	inf.deleteStmt(ax.stmt)
	inf.rescan()
	return true
}

func (inf *InputFile) stmtIndex(stmt dst.Stmt) int {
	for i, st := range inf.fn.Body.List {
		if st == stmt {
			return i
		}
	}
	return -1
}

func (inf *InputFile) deleteStmt(del dst.Stmt) {
	stmts := inf.fn.Body.List[:0]
	for _, stmt := range inf.fn.Body.List {
		if stmt != del {
			stmts = append(stmts, stmt)
		}
	}
	inf.fn.Body.List = stmts
}

// LastWritten returns the bytes of the most recent successful Save.
func (inf *InputFile) LastWritten() []byte { return inf.lastWritten }

// Save prints the file, verifies the output re-parses with the same binding
// count, and atomically replaces Path.
func (inf *InputFile) Save() error {
	pruneUnusedImports(inf.file)
	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, inf.file); err != nil {
		return fmt.Errorf("srcmodel: print %s: %w", inf.Path, err)
	}
	out := buf.Bytes()
	check, err := parseInputSource(out, inf.Path)
	if err != nil {
		return fmt.Errorf("srcmodel: refusing to save %s: output does not re-parse: %w", inf.Path, err)
	}
	if len(check.Actions) != len(inf.Actions) || len(check.Axes) != len(inf.Axes) {
		return fmt.Errorf("srcmodel: refusing to save %s: bindings lost in round trip", inf.Path)
	}
	tmp, err := os.CreateTemp(filepath.Dir(inf.Path), ".spliti-save-*")
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
	if info, err := os.Stat(inf.Path); err == nil {
		os.Chmod(tmpName, info.Mode())
	}
	if err := os.Rename(tmpName, inf.Path); err != nil {
		return err
	}
	inf.lastWritten = out
	return nil
}
