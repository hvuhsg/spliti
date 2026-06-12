package srcmodel

import (
	"fmt"
	"go/token"
	"sort"
	"strconv"

	"github.com/dave/dst"
)

// SetLine is one recognized `scene.Set(c, ref, pkg.Type{...})` statement — the
// scene-file form of a per-instance component override.
type SetLine struct {
	Type     string // component type as written, e.g. "components.Health"
	Editable bool   // composite literal is keyed and all-literal

	ref  string
	stmt dst.Stmt
	lit  *dst.CompositeLit
}

// Lit returns the override's composite literal for value decoding (ApplyLit).
func (sl *SetLine) Lit() *dst.CompositeLit { return sl.lit }

// RemoveLine is one recognized `scene.Remove[pkg.Type](c, ref)` statement —
// the scene-file form of stripping a component the prefab added.
type RemoveLine struct {
	Type string

	ref  string
	stmt dst.Stmt
}

// ParentLine is one recognized `scene.Parent(c, child, parent)` statement.
type ParentLine struct {
	Parent string // parent instance name (resolved from the var binding)

	childRef, parentRef string
	stmt                dst.Stmt
}

// recognizeSet matches `scene.Set(c, ref, pkg.Type{...})`. vars is the set of
// spawn variable names declared so far — identifiers naming them inside the
// literal are entity references, which stay editable.
func recognizeSet(stmt dst.Stmt, vars map[string]bool) *SetLine {
	call := exprStmtCall(stmt)
	if call == nil || !isPkgCall(call, scenePkg, "Set") || len(call.Args) != 3 {
		return nil
	}
	ref, ok := identName(call.Args[1])
	if !ok {
		return nil
	}
	lit, ok := call.Args[2].(*dst.CompositeLit)
	if !ok {
		return nil
	}
	typeName := exprName(lit.Type)
	if typeName == "" {
		return nil
	}
	return &SetLine{
		Type:     typeName,
		Editable: compositeEditable(lit, vars),
		ref:      ref,
		stmt:     stmt,
		lit:      lit,
	}
}

// recognizeRemove matches `scene.Remove[pkg.Type](c, ref)`.
func recognizeRemove(stmt dst.Stmt) *RemoveLine {
	call := exprStmtCall(stmt)
	if call == nil || len(call.Args) != 2 {
		return nil
	}
	idx, ok := call.Fun.(*dst.IndexExpr)
	if !ok {
		return nil
	}
	sel, ok := idx.X.(*dst.SelectorExpr)
	if !ok {
		return nil
	}
	if id, ok := sel.X.(*dst.Ident); !ok || id.Name != scenePkg || sel.Sel.Name != "Remove" {
		return nil
	}
	typeName := exprName(idx.Index)
	if typeName == "" {
		return nil
	}
	ref, ok := identName(call.Args[1])
	if !ok {
		return nil
	}
	return &RemoveLine{Type: typeName, ref: ref, stmt: stmt}
}

// recognizeParent matches `scene.Parent(c, child, parent)`.
func recognizeParent(stmt dst.Stmt) *ParentLine {
	call := exprStmtCall(stmt)
	if call == nil || !isPkgCall(call, scenePkg, "Parent") || len(call.Args) != 3 {
		return nil
	}
	child, ok1 := identName(call.Args[1])
	parent, ok2 := identName(call.Args[2])
	if !ok1 || !ok2 {
		return nil
	}
	return &ParentLine{childRef: child, parentRef: parent, stmt: stmt}
}

func exprStmtCall(stmt dst.Stmt) *dst.CallExpr {
	es, ok := stmt.(*dst.ExprStmt)
	if !ok {
		return nil
	}
	call, ok := es.X.(*dst.CallExpr)
	if !ok {
		return nil
	}
	return call
}

func identName(e dst.Expr) (string, bool) {
	id, ok := e.(*dst.Ident)
	if !ok || id.Name == "_" {
		return "", false
	}
	return id.Name, true
}

// --- structural mutations -------------------------------------------------
//
// All mutations operate on the dst AST and finish with rescan(), so existing
// *Spawn/*SetLine pointers are invalidated — re-look-up by instance name.

// EnsureVar makes sure the instance's spawn statement binds a variable
// (promoting `_ =` and bare-call forms) and returns the variable name, so
// attachment lines can reference the spawn.
func (s *Scene) EnsureVar(instance string) (string, error) {
	sp := s.Spawn(instance)
	if sp == nil {
		return "", fmt.Errorf("srcmodel: scene %q has no instance %q", s.Name, instance)
	}
	if sp.Var != "" {
		return sp.Var, nil
	}
	name := s.freeVarName(instance)
	switch st := sp.stmt.(type) {
	case *dst.AssignStmt: // `_ = scene.Spawn(...)`
		st.Lhs[0] = dst.NewIdent(name)
		st.Tok = token.DEFINE
	case *dst.ExprStmt: // bare `scene.Spawn(...)`
		repl := &dst.AssignStmt{
			Lhs: []dst.Expr{dst.NewIdent(name)},
			Tok: token.DEFINE,
			Rhs: []dst.Expr{st.X},
		}
		repl.Decs.NodeDecs = st.Decs.NodeDecs
		idx := s.stmtIndex(sp.stmt)
		if idx < 0 {
			return "", fmt.Errorf("srcmodel: instance %q: statement not found in body", instance)
		}
		s.fn.Body.List[idx] = repl
	default:
		return "", fmt.Errorf("srcmodel: instance %q: unexpected spawn statement form", instance)
	}
	s.rescan()
	return name, nil
}

// freeVarName derives a valid, unused variable name from an instance name.
func (s *Scene) freeVarName(instance string) string {
	base := sanitizeIdent(instance)
	used := s.usedIdents()
	name := base
	for used[name] {
		name += "_"
	}
	return name
}

// usedIdents collects every identifier mentioned in the scene function —
// conservative enough to avoid shadowing or collisions.
func (s *Scene) usedIdents() map[string]bool {
	used := map[string]bool{s.ctx: true, scenePkg: true, xformPkg: true}
	dst.Inspect(s.fn, func(n dst.Node) bool {
		if id, ok := n.(*dst.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	return used
}

func sanitizeIdent(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		alpha := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		digit := r >= '0' && r <= '9'
		switch {
		case alpha || (digit && len(out) > 0):
			out = append(out, r)
		case digit: // leading digit
			out = append(out, 'e', r)
		}
	}
	if len(out) == 0 {
		return "inst"
	}
	switch string(out) {
	// identifiers that would shadow or clash with grammar vocabulary
	case "scene", "render3d", "entities", "components", "c", "_":
		return string(out) + "Ent"
	}
	return string(out)
}

// SetComponent upserts the instance's `scene.Set(c, var, <lit>)` line for the
// literal's component type, replacing the existing line's literal or inserting
// a new statement right after the instance's last recognized line.
func (s *Scene) SetComponent(instance string, lit *dst.CompositeLit) error {
	return s.setComponentRefs(instance, lit, nil)
}

// setComponentRefs is SetComponent constrained by entity references: the line
// must sit after the spawn statement of every instance in refInstances, or the
// emitted variable references would not compile (declare before use). An
// existing line in too early a position is moved.
func (s *Scene) setComponentRefs(instance string, lit *dst.CompositeLit, refInstances []string) error {
	typeName := exprName(lit.Type)
	if typeName == "" {
		return fmt.Errorf("srcmodel: composite literal has no recognizable type")
	}
	sp := s.Spawn(instance)
	if sp == nil {
		return fmt.Errorf("srcmodel: scene %q has no instance %q", s.Name, instance)
	}
	// Recomputed after every mutation: rescans shift statement indices.
	minIdx := func() int {
		min := -1
		for _, ref := range refInstances {
			if rp := s.Spawn(ref); rp != nil {
				if i := s.stmtIndex(rp.stmt); i > min {
					min = i
				}
			}
		}
		return min
	}
	if sl := sp.Set(typeName); sl != nil {
		if !sl.Editable {
			return fmt.Errorf("srcmodel: instance %q: scene.Set(%s) is not a literal override", instance, typeName)
		}
		if s.stmtIndex(sl.stmt) >= minIdx() {
			lit.Decs.NodeDecs = *sl.lit.Decorations()
			call := exprStmtCall(sl.stmt)
			call.Args[2] = lit
			s.rescan()
			return nil
		}
		// The new literal references a spawn declared after the existing line:
		// drop it and reinsert below.
		s.deleteStmts(sl.stmt)
		s.rescan()
	}
	varName, err := s.EnsureVar(instance)
	if err != nil {
		return err
	}
	stmt := &dst.ExprStmt{X: &dst.CallExpr{
		Fun:  &dst.SelectorExpr{X: dst.NewIdent(scenePkg), Sel: dst.NewIdent("Set")},
		Args: []dst.Expr{dst.NewIdent(s.ctx), dst.NewIdent(varName), lit},
	}}
	stmt.Decs.Before = dst.NewLine
	stmt.Decs.After = dst.NewLine
	at := s.lastStmtOf(instance)
	if m := minIdx(); m > at {
		at = m
	}
	s.insertAfter(at, stmt)
	s.rescan()
	return nil
}

// RemoveSet deletes the instance's scene.Set line for the component type.
// Reports whether a line existed.
func (s *Scene) RemoveSet(instance, typeName string) (bool, error) {
	sp := s.Spawn(instance)
	if sp == nil {
		return false, fmt.Errorf("srcmodel: scene %q has no instance %q", s.Name, instance)
	}
	sl := sp.Set(typeName)
	if sl == nil {
		return false, nil
	}
	s.deleteStmts(sl.stmt)
	s.rescan()
	return true, nil
}

// AddRemove appends a `scene.Remove[pkg.Type](c, var)` line for the instance
// (idempotent: an existing line for the type is kept).
func (s *Scene) AddRemove(instance, typeName string) error {
	sp := s.Spawn(instance)
	if sp == nil {
		return fmt.Errorf("srcmodel: scene %q has no instance %q", s.Name, instance)
	}
	if sp.Removed(typeName) != nil {
		return nil
	}
	typeExpr := nameExpr(typeName)
	if typeExpr == nil {
		return fmt.Errorf("srcmodel: invalid type name %q", typeName)
	}
	varName, err := s.EnsureVar(instance)
	if err != nil {
		return err
	}
	stmt := &dst.ExprStmt{X: &dst.CallExpr{
		Fun: &dst.IndexExpr{
			X:     &dst.SelectorExpr{X: dst.NewIdent(scenePkg), Sel: dst.NewIdent("Remove")},
			Index: typeExpr,
		},
		Args: []dst.Expr{dst.NewIdent(s.ctx), dst.NewIdent(varName)},
	}}
	stmt.Decs.Before = dst.NewLine
	stmt.Decs.After = dst.NewLine
	s.insertAfter(s.lastStmtOf(instance), stmt)
	s.rescan()
	return nil
}

// DeleteRemove drops the instance's scene.Remove line for the type, if any.
// Reports whether a line existed.
func (s *Scene) DeleteRemove(instance, typeName string) (bool, error) {
	sp := s.Spawn(instance)
	if sp == nil {
		return false, fmt.Errorf("srcmodel: scene %q has no instance %q", s.Name, instance)
	}
	rl := sp.Removed(typeName)
	if rl == nil {
		return false, nil
	}
	s.deleteStmts(rl.stmt)
	s.rescan()
	return true, nil
}

// SetParent upserts the instance's scene.Parent line. An empty parent removes
// the line (unparenting).
func (s *Scene) SetParent(instance, parent string) error {
	sp := s.Spawn(instance)
	if sp == nil {
		return fmt.Errorf("srcmodel: scene %q has no instance %q", s.Name, instance)
	}
	if parent == "" {
		if sp.ParentLine != nil {
			s.deleteStmts(sp.ParentLine.stmt)
			s.rescan()
		}
		return nil
	}
	if s.Spawn(parent) == nil {
		return fmt.Errorf("srcmodel: scene %q has no instance %q (parent)", s.Name, parent)
	}
	childVar, err := s.EnsureVar(instance)
	if err != nil {
		return err
	}
	parentVar, err := s.EnsureVar(parent)
	if err != nil {
		return err
	}
	sp = s.Spawn(instance) // EnsureVar rescans
	if pl := sp.ParentLine; pl != nil {
		call := exprStmtCall(pl.stmt)
		call.Args[1] = dst.NewIdent(childVar)
		call.Args[2] = dst.NewIdent(parentVar)
		s.rescan()
		return nil
	}
	stmt := &dst.ExprStmt{X: &dst.CallExpr{
		Fun:  &dst.SelectorExpr{X: dst.NewIdent(scenePkg), Sel: dst.NewIdent("Parent")},
		Args: []dst.Expr{dst.NewIdent(s.ctx), dst.NewIdent(childVar), dst.NewIdent(parentVar)},
	}}
	stmt.Decs.Before = dst.NewLine
	stmt.Decs.After = dst.NewLine
	// The parent line must come after BOTH spawns; instance order in the body
	// already guarantees both vars are declared by the later of the two.
	at := s.lastStmtOf(instance)
	if p := s.lastStmtOf(parent); p > at {
		at = p
	}
	s.insertAfter(at, stmt)
	s.rescan()
	return nil
}

// Rename changes the instance's string-literal identity. The bound variable
// name is plumbing and is left untouched.
func (s *Scene) Rename(old, new string) error {
	if new == "" {
		return fmt.Errorf("srcmodel: empty instance name")
	}
	if s.Spawn(new) != nil {
		return fmt.Errorf("srcmodel: scene %q already has instance %q", s.Name, new)
	}
	sp := s.Spawn(old)
	if sp == nil {
		return fmt.Errorf("srcmodel: scene %q has no instance %q", s.Name, old)
	}
	call := spawnCallOf(sp.stmt)
	if call == nil {
		return fmt.Errorf("srcmodel: instance %q: unexpected spawn statement form", old)
	}
	nameLit := call.Args[1].(*dst.BasicLit)
	nameLit.Value = strconv.Quote(new)
	s.rescan()
	return nil
}

// spawnCallOf digs the scene.Spawn call out of a recognized spawn statement.
func spawnCallOf(stmt dst.Stmt) *dst.CallExpr {
	switch st := stmt.(type) {
	case *dst.AssignStmt:
		if call, ok := st.Rhs[0].(*dst.CallExpr); ok {
			return call
		}
	case *dst.ExprStmt:
		if call, ok := st.X.(*dst.CallExpr); ok {
			return call
		}
	}
	return nil
}

// RemovedSpawn captures the statements deleted by RemoveSpawn so the deletion
// can be undone with RestoreSpawn. It stays valid as long as the SceneFile is
// not reloaded from disk.
type RemovedSpawn struct {
	Instance string
	stmts    []dst.Stmt
	indices  []int // original body indices, ascending
}

// RemoveSpawn deletes the instance's spawn statement plus every recognized
// line that references it: its Set/Remove/Parent attachments and parent lines
// of other instances that name it as the parent. It refuses when the spawn's
// variable is still referenced by statements it does not delete (hand-written
// code), so a deletion can never silently break compilation.
func (s *Scene) RemoveSpawn(instance string) (*RemovedSpawn, error) {
	sp := s.Spawn(instance)
	if sp == nil {
		return nil, fmt.Errorf("srcmodel: scene %q has no instance %q", s.Name, instance)
	}
	doomed := map[dst.Stmt]bool{sp.stmt: true}
	for _, sl := range sp.Sets {
		doomed[sl.stmt] = true
	}
	for _, rl := range sp.Removes {
		doomed[rl.stmt] = true
	}
	if sp.ParentLine != nil {
		doomed[sp.ParentLine.stmt] = true
	}
	for _, other := range s.Spawns {
		if other.ParentLine != nil && other.ParentLine.Parent == instance {
			doomed[other.ParentLine.stmt] = true
		}
	}
	// Guard: the variable must not be referenced by any surviving statement.
	if sp.Var != "" {
		for _, stmt := range s.fn.Body.List {
			if doomed[stmt] {
				continue
			}
			if referencesIdent(stmt, sp.Var) {
				return nil, fmt.Errorf("srcmodel: instance %q: variable %s is still referenced elsewhere in the scene; clear that reference first", instance, sp.Var)
			}
		}
	}
	rem := &RemovedSpawn{Instance: instance}
	for i, stmt := range s.fn.Body.List {
		if doomed[stmt] {
			rem.stmts = append(rem.stmts, stmt)
			rem.indices = append(rem.indices, i)
		}
	}
	stmts := make([]dst.Stmt, 0, len(s.fn.Body.List)-len(rem.stmts))
	for _, stmt := range s.fn.Body.List {
		if !doomed[stmt] {
			stmts = append(stmts, stmt)
		}
	}
	s.fn.Body.List = stmts
	s.rescan()
	return rem, nil
}

// RestoreSpawn reinserts statements captured by RemoveSpawn at (or as close as
// possible to) their original positions.
func (s *Scene) RestoreSpawn(rem *RemovedSpawn) error {
	if rem == nil || len(rem.stmts) == 0 {
		return fmt.Errorf("srcmodel: nothing to restore")
	}
	if s.Spawn(rem.Instance) != nil {
		return fmt.Errorf("srcmodel: scene %q already has instance %q", s.Name, rem.Instance)
	}
	type ins struct {
		stmt dst.Stmt
		at   int
	}
	inserts := make([]ins, len(rem.stmts))
	for i := range rem.stmts {
		inserts[i] = ins{rem.stmts[i], rem.indices[i]}
	}
	sort.SliceStable(inserts, func(i, j int) bool { return inserts[i].at < inserts[j].at })
	for _, in := range inserts {
		at := in.at
		if at > len(s.fn.Body.List) {
			at = len(s.fn.Body.List)
		}
		s.fn.Body.List = append(s.fn.Body.List[:at],
			append([]dst.Stmt{in.stmt}, s.fn.Body.List[at:]...)...)
	}
	s.rescan()
	return nil
}

// referencesIdent reports whether the statement mentions the identifier.
func referencesIdent(stmt dst.Stmt, name string) bool {
	found := false
	dst.Inspect(stmt, func(n dst.Node) bool {
		if id, ok := n.(*dst.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// stmtIndex finds the statement's position in the function body, or -1.
func (s *Scene) stmtIndex(stmt dst.Stmt) int {
	for i, st := range s.fn.Body.List {
		if st == stmt {
			return i
		}
	}
	return -1
}

// lastStmtOf returns the body index of the instance's last recognized
// statement (spawn line or attachment), so inserts keep an instance's lines
// grouped together.
func (s *Scene) lastStmtOf(instance string) int {
	sp := s.Spawn(instance)
	if sp == nil {
		return len(s.fn.Body.List) - 1
	}
	last := s.stmtIndex(sp.stmt)
	consider := func(stmt dst.Stmt) {
		if i := s.stmtIndex(stmt); i > last {
			last = i
		}
	}
	for _, sl := range sp.Sets {
		consider(sl.stmt)
	}
	for _, rl := range sp.Removes {
		consider(rl.stmt)
	}
	if sp.ParentLine != nil {
		consider(sp.ParentLine.stmt)
	}
	return last
}

// insertAfter places stmt directly after body index at.
func (s *Scene) insertAfter(at int, stmt dst.Stmt) {
	pos := at + 1
	if pos > len(s.fn.Body.List) {
		pos = len(s.fn.Body.List)
	}
	if pos < 0 {
		pos = 0
	}
	s.fn.Body.List = append(s.fn.Body.List[:pos],
		append([]dst.Stmt{stmt}, s.fn.Body.List[pos:]...)...)
}

// deleteStmts removes the given statements from the function body.
func (s *Scene) deleteStmts(del ...dst.Stmt) {
	doomed := map[dst.Stmt]bool{}
	for _, d := range del {
		doomed[d] = true
	}
	stmts := s.fn.Body.List[:0]
	for _, stmt := range s.fn.Body.List {
		if !doomed[stmt] {
			stmts = append(stmts, stmt)
		}
	}
	s.fn.Body.List = stmts
}
