package editor

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/dave/dst"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/registry"
	"github.com/hvuhsg/spliti/editor/srcmodel"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Every editor mutation is an undo.Command defined here. Commands write the
// live world and the scene source model together; the debounced save in
// schedule.Last persists the model to disk afterwards. On a read-only scene
// (parse error) the source half is skipped — edits stay live-only, as the
// status line warns.

// push executes a command through the undo stack and surfaces failures in the
// status line.
func (st *state) push(c *app.Ctx, cmd interface {
	Name() string
	Do(c *app.Ctx) error
	Undo(c *app.Ctx) error
}) {
	// In Play mode edits are live-only and ephemeral (Stop restores the
	// snapshot), so they bypass the undo stack — an entry would be stale the
	// moment the world reverts.
	if st.mode != modeEdit {
		if err := cmd.Do(c); err != nil {
			st.status(fmt.Sprintf("%s: %v", cmd.Name(), err))
		}
		return
	}
	if err := st.undo.Push(c, cmd); err != nil {
		st.status(fmt.Sprintf("%s: %v", cmd.Name(), err))
		return
	}
	st.touchSource()
}

// scene returns the writable scene model, or nil when the source is read-only.
// In Play mode it is always nil: play edits never reach the scene file.
func (st *state) scene() *srcmodel.Scene {
	if st.src == nil || st.srcErr != nil || st.mode != modeEdit {
		return nil
	}
	return st.src.Scene(st.cfg.Scene)
}

// qualName is the component type as written in scene source ("components.Health").
func qualName(ti *registry.TypeInfo) string { return ti.Pkg + "." + ti.Name }

// resolve finds the live entity for a command target: by instance name when
// the target is named (handles survive despawn/respawn cycles), by the stored
// handle otherwise.
func resolve(c *app.Ctx, instance string, e ecs.Entity) (ecs.Entity, bool) {
	if instance != "" {
		return entityByInstance(c, instance)
	}
	if c.World().Alive(e) {
		return e, true
	}
	return ecs.Entity{}, false
}

// --- transform --------------------------------------------------------------

// cmdTransform sets an entity's local Transform3D (gizmo drags and inspector
// scrubs aggregate into one of these per gesture).
type cmdTransform struct {
	instance      string
	entity        ecs.Entity
	before, after render3d.Transform3D
}

func (cmd *cmdTransform) Name() string { return "transform " + cmd.target() }
func (cmd *cmdTransform) target() string {
	if cmd.instance != "" {
		return cmd.instance
	}
	return "entity"
}
func (cmd *cmdTransform) Do(c *app.Ctx) error   { return cmd.apply(c, cmd.after) }
func (cmd *cmdTransform) Undo(c *app.Ctx) error { return cmd.apply(c, cmd.before) }

func (cmd *cmdTransform) apply(c *app.Ctx, t render3d.Transform3D) error {
	st := app.GetResource[state](c)
	e, ok := resolve(c, cmd.instance, cmd.entity)
	if !ok {
		return fmt.Errorf("target no longer exists")
	}
	tm := generic.NewMap[render3d.Transform3D](c.World())
	if !tm.Has(e) {
		return fmt.Errorf("target has no Transform3D")
	}
	*tm.Get(e) = t
	delete(st.dirty, cmd.instance) // the command writes the model itself
	if sc := st.scene(); sc != nil && cmd.instance != "" {
		if err := sc.SetTransform(cmd.instance, srcmodel.FromTransform3D(t)); err != nil {
			st.status(fmt.Sprintf("live-only: %v", err))
		}
	}
	return nil
}

// --- component value edit ----------------------------------------------------

// cmdSetComponent replaces a component's whole value, upserting the instance's
// scene.Set override line.
type cmdSetComponent struct {
	instance      string
	entity        ecs.Entity
	typeName      string // registry name
	before, after any
	hadSetLine    bool // captured at construction: did a Set line exist before?
}

func (cmd *cmdSetComponent) Name() string { return "edit " + cmd.typeName }

func (cmd *cmdSetComponent) Do(c *app.Ctx) error { return cmd.apply(c, cmd.after, true) }
func (cmd *cmdSetComponent) Undo(c *app.Ctx) error {
	return cmd.apply(c, cmd.before, cmd.hadSetLine)
}

func (cmd *cmdSetComponent) apply(c *app.Ctx, val any, wantLine bool) error {
	st := app.GetResource[state](c)
	ti := st.reg.Lookup(cmd.typeName)
	if ti == nil {
		return fmt.Errorf("unknown component %s", cmd.typeName)
	}
	e, ok := resolve(c, cmd.instance, cmd.entity)
	if !ok {
		return fmt.Errorf("target no longer exists")
	}
	ti.SetValue(c.World(), e, val)
	sc := st.scene()
	if sc == nil || cmd.instance == "" {
		return nil
	}
	if wantLine {
		if err := sc.SetComponentValue(cmd.instance, qualName(ti), reflect.ValueOf(val)); err != nil {
			st.status(fmt.Sprintf("live-only: %v", err))
		}
	} else {
		if _, err := sc.RemoveSet(cmd.instance, qualName(ti)); err != nil {
			st.status(fmt.Sprintf("live-only: %v", err))
		}
	}
	return nil
}

// newSetComponent captures the pre-edit source state for proper undo.
func (st *state) newSetComponent(instance string, e ecs.Entity, ti *registry.TypeInfo, before, after any) *cmdSetComponent {
	cmd := &cmdSetComponent{
		instance: instance, entity: e, typeName: ti.Name,
		before: before, after: after,
	}
	if sc := st.scene(); sc != nil && instance != "" {
		if sp := sc.Spawn(instance); sp != nil {
			cmd.hadSetLine = sp.Set(qualName(ti)) != nil
		}
	}
	return cmd
}

// --- add / remove component --------------------------------------------------

// cmdAddComponent attaches a zero-valued component and pins it in source with
// a scene.Set line (deleting a scene.Remove line that suppressed the type).
type cmdAddComponent struct {
	instance      string
	entity        ecs.Entity
	typeName      string
	hadRemoveLine bool
}

func (cmd *cmdAddComponent) Name() string { return "add " + cmd.typeName }

func (cmd *cmdAddComponent) Do(c *app.Ctx) error {
	st := app.GetResource[state](c)
	ti := st.reg.Lookup(cmd.typeName)
	e, ok := resolve(c, cmd.instance, cmd.entity)
	if ti == nil || !ok {
		return fmt.Errorf("cannot add %s", cmd.typeName)
	}
	if ti.Has(c.World(), e) {
		return fmt.Errorf("%s already present", cmd.typeName)
	}
	ti.Add(c.World(), e)
	if sc := st.scene(); sc != nil && cmd.instance != "" {
		if sp := sc.Spawn(cmd.instance); sp != nil {
			cmd.hadRemoveLine = sp.Removed(qualName(ti)) != nil
		}
		if cmd.hadRemoveLine {
			sc.DeleteRemove(cmd.instance, qualName(ti))
		}
		zero := reflect.New(ti.Type).Elem()
		if err := sc.SetComponentValue(cmd.instance, qualName(ti), zero); err != nil {
			st.status(fmt.Sprintf("live-only: %v", err))
		}
	}
	return nil
}

func (cmd *cmdAddComponent) Undo(c *app.Ctx) error {
	st := app.GetResource[state](c)
	ti := st.reg.Lookup(cmd.typeName)
	e, ok := resolve(c, cmd.instance, cmd.entity)
	if ti == nil || !ok {
		return fmt.Errorf("cannot undo add %s", cmd.typeName)
	}
	if ti.Has(c.World(), e) {
		ti.Remove(c.World(), e)
	}
	if sc := st.scene(); sc != nil && cmd.instance != "" {
		sc.RemoveSet(cmd.instance, qualName(ti))
		if cmd.hadRemoveLine {
			sc.AddRemove(cmd.instance, qualName(ti))
		}
	}
	return nil
}

// cmdRemoveComponent strips a component, deleting any scene.Set override and
// recording a scene.Remove line so a prefab-provided component stays gone on
// the next load.
type cmdRemoveComponent struct {
	instance   string
	entity     ecs.Entity
	typeName   string
	before     any
	hadSetLine bool
}

func (cmd *cmdRemoveComponent) Name() string { return "remove " + cmd.typeName }

func (cmd *cmdRemoveComponent) Do(c *app.Ctx) error {
	st := app.GetResource[state](c)
	ti := st.reg.Lookup(cmd.typeName)
	e, ok := resolve(c, cmd.instance, cmd.entity)
	if ti == nil || !ok || !ti.Has(c.World(), e) {
		return fmt.Errorf("cannot remove %s", cmd.typeName)
	}
	cmd.before = ti.Value(c.World(), e)
	ti.Remove(c.World(), e)
	if sc := st.scene(); sc != nil && cmd.instance != "" {
		had, _ := sc.RemoveSet(cmd.instance, qualName(ti))
		cmd.hadSetLine = had
		if err := sc.AddRemove(cmd.instance, qualName(ti)); err != nil {
			st.status(fmt.Sprintf("live-only: %v", err))
		}
	}
	return nil
}

func (cmd *cmdRemoveComponent) Undo(c *app.Ctx) error {
	st := app.GetResource[state](c)
	ti := st.reg.Lookup(cmd.typeName)
	e, ok := resolve(c, cmd.instance, cmd.entity)
	if ti == nil || !ok {
		return fmt.Errorf("cannot undo remove %s", cmd.typeName)
	}
	ti.SetValue(c.World(), e, cmd.before)
	if sc := st.scene(); sc != nil && cmd.instance != "" {
		sc.DeleteRemove(cmd.instance, qualName(ti))
		if cmd.hadSetLine {
			sc.SetComponentValue(cmd.instance, qualName(ti), reflect.ValueOf(cmd.before))
		}
	}
	return nil
}

// --- spawn / delete ----------------------------------------------------------

// cmdSpawn adds a new instance: a live prefab call plus a scene.Spawn line.
type cmdSpawn struct {
	instance string
	prefab   string
	t        render3d.Transform3D
}

func (cmd *cmdSpawn) Name() string { return "spawn " + cmd.instance }

func (cmd *cmdSpawn) Do(c *app.Ctx) error {
	st := app.GetResource[state](c)
	if _, exists := entityByInstance(c, cmd.instance); exists {
		return fmt.Errorf("instance %q already exists", cmd.instance)
	}
	if _, err := spawnLive(c, st, cmd.instance, cmd.prefab, cmd.t); err != nil {
		return err
	}
	if sc := st.scene(); sc != nil {
		if _, err := sc.AddSpawn(cmd.instance, cmd.prefab, srcmodel.FromTransform3D(cmd.t)); err != nil {
			st.status(fmt.Sprintf("live-only: %v", err))
		}
	}
	return nil
}

func (cmd *cmdSpawn) Undo(c *app.Ctx) error {
	st := app.GetResource[state](c)
	if e, ok := entityByInstance(c, cmd.instance); ok {
		despawnSubtree(c, e)
		st.deselectIf(e)
	}
	if sc := st.scene(); sc != nil {
		if _, err := sc.RemoveSpawn(cmd.instance); err != nil {
			return err
		}
	}
	return nil
}

// cmdDelete removes an instance and its named descendants from the live world
// and the scene source. Undo restores the source lines and rebuilds the live
// subtree from them (live-only tweaks on the subtree do not survive).
type cmdDelete struct {
	root      string
	instances []string // leaf-first
	removed   []*srcmodel.RemovedSpawn
}

func (cmd *cmdDelete) Name() string { return "delete " + cmd.root }

func (cmd *cmdDelete) Do(c *app.Ctx) error {
	st := app.GetResource[state](c)
	e, ok := entityByInstance(c, cmd.root)
	if !ok {
		return fmt.Errorf("instance %q not found", cmd.root)
	}
	cmd.instances = namedSubtreeLeafFirst(c, e)
	cmd.removed = nil
	if sc := st.scene(); sc != nil {
		for _, inst := range cmd.instances {
			rem, err := sc.RemoveSpawn(inst)
			if err != nil {
				// Roll the already-removed lines back and refuse: a partial
				// source delete would desync live and source.
				for i := len(cmd.removed) - 1; i >= 0; i-- {
					sc.RestoreSpawn(cmd.removed[i])
				}
				cmd.removed = nil
				return err
			}
			cmd.removed = append(cmd.removed, rem)
		}
	}
	despawnSubtree(c, e)
	st.deselectIf(e)
	return nil
}

func (cmd *cmdDelete) Undo(c *app.Ctx) error {
	st := app.GetResource[state](c)
	sc := st.scene()
	if sc != nil {
		for i := len(cmd.removed) - 1; i >= 0; i-- {
			if err := sc.RestoreSpawn(cmd.removed[i]); err != nil {
				return err
			}
		}
	}
	// Rebuild live entities from the restored lines, parents before children.
	for i := len(cmd.instances) - 1; i >= 0; i-- {
		if sc == nil {
			break
		}
		if sp := sc.Spawn(cmd.instances[i]); sp != nil {
			if err := syncInstanceFromModel(c, st, sp); err != nil {
				st.status(fmt.Sprintf("restore %s: %v", cmd.instances[i], err))
			}
		}
	}
	return nil
}

// --- rename / reparent / duplicate -------------------------------------------

type cmdRename struct {
	old, new string
}

func (cmd *cmdRename) Name() string { return fmt.Sprintf("rename %s -> %s", cmd.old, cmd.new) }

func (cmd *cmdRename) Do(c *app.Ctx) error   { return renameLive(c, cmd.old, cmd.new) }
func (cmd *cmdRename) Undo(c *app.Ctx) error { return renameLive(c, cmd.new, cmd.old) }

func renameLive(c *app.Ctx, from, to string) error {
	st := app.GetResource[state](c)
	if to == "" {
		return fmt.Errorf("empty name")
	}
	if _, exists := entityByInstance(c, to); exists {
		return fmt.Errorf("instance %q already exists", to)
	}
	e, ok := entityByInstance(c, from)
	if !ok {
		return fmt.Errorf("instance %q not found", from)
	}
	nm := nameMap(c)
	nm.Get(e).Value = to
	if sc := st.scene(); sc != nil {
		if err := sc.Rename(from, to); err != nil {
			st.status(fmt.Sprintf("live-only: %v", err))
		}
	}
	return nil
}

// cmdReparent moves an instance under a new parent ("" = scene root), keeping
// its world placement by rewriting the local transform.
type cmdReparent struct {
	instance             string
	oldParent, newParent string
	oldLocal, newLocal   render3d.Transform3D
}

func (cmd *cmdReparent) Name() string { return "reparent " + cmd.instance }

func (cmd *cmdReparent) Do(c *app.Ctx) error {
	return reparentLive(c, cmd.instance, cmd.newParent, cmd.newLocal)
}

func (cmd *cmdReparent) Undo(c *app.Ctx) error {
	return reparentLive(c, cmd.instance, cmd.oldParent, cmd.oldLocal)
}

func reparentLive(c *app.Ctx, instance, parent string, local render3d.Transform3D) error {
	st := app.GetResource[state](c)
	e, ok := entityByInstance(c, instance)
	if !ok {
		return fmt.Errorf("instance %q not found", instance)
	}
	w := c.World()
	if parent == "" {
		pm := generic.NewMap1[render3d.Parent](w)
		gm := generic.NewMap[render3d.Parent](w)
		if gm.Has(e) {
			pm.Remove(e)
		}
	} else {
		pe, ok := entityByInstance(c, parent)
		if !ok {
			return fmt.Errorf("parent %q not found", parent)
		}
		if pe == e || isDescendant(c, pe, e) {
			return fmt.Errorf("cannot parent %s under its own subtree", instance)
		}
		scene.Set(c, e, render3d.Parent{Entity: pe})
	}
	tm := generic.NewMap[render3d.Transform3D](w)
	if tm.Has(e) {
		*tm.Get(e) = local
	}
	if sc := st.scene(); sc != nil {
		if err := sc.SetParent(instance, parent); err != nil {
			st.status(fmt.Sprintf("live-only: %v", err))
		} else if err := sc.SetTransform(instance, srcmodel.FromTransform3D(local)); err != nil {
			st.status(fmt.Sprintf("live-only: %v", err))
		}
	}
	return nil
}

// cmdDuplicate copies an instance: same prefab and transform (nudged), plus
// clones of its scene.Set/Remove override lines and parent link.
type cmdDuplicate struct {
	src, instance string
}

func (cmd *cmdDuplicate) Name() string { return "duplicate " + cmd.src }

func (cmd *cmdDuplicate) Do(c *app.Ctx) error {
	st := app.GetResource[state](c)
	sc := st.scene()
	if sc == nil {
		return fmt.Errorf("scene is read-only")
	}
	src := sc.Spawn(cmd.src)
	if src == nil {
		return fmt.Errorf("instance %q has no scene line", cmd.src)
	}
	if src.Transform == nil || !src.Transform.Editable {
		return fmt.Errorf("instance %q has a non-literal transform", cmd.src)
	}
	t := *src.Transform
	if _, err := sc.AddSpawn(cmd.instance, src.Prefab, t); err != nil {
		return err
	}
	for _, sl := range src.Sets {
		if sl.Editable {
			sc.SetComponent(cmd.instance, dst.Clone(sl.Lit()).(*dst.CompositeLit))
		}
	}
	for _, rl := range src.Removes {
		sc.AddRemove(cmd.instance, rl.Type)
	}
	if src.ParentLine != nil {
		sc.SetParent(cmd.instance, src.ParentLine.Parent)
	}
	sp := sc.Spawn(cmd.instance)
	return syncInstanceFromModel(c, st, sp)
}

func (cmd *cmdDuplicate) Undo(c *app.Ctx) error {
	st := app.GetResource[state](c)
	if e, ok := entityByInstance(c, cmd.instance); ok {
		despawnSubtree(c, e)
		st.deselectIf(e)
	}
	if sc := st.scene(); sc != nil {
		if _, err := sc.RemoveSpawn(cmd.instance); err != nil {
			return err
		}
	}
	return nil
}

// --- live-world helpers --------------------------------------------------------

// spawnLive runs the prefab function and tags the result, mirroring what the
// scene line will do on the next load.
func spawnLive(c *app.Ctx, st *state, instance, prefab string, t render3d.Transform3D) (ecs.Entity, error) {
	fn := st.cfg.Prefabs[prefab]
	if fn == nil {
		return ecs.Entity{}, fmt.Errorf("unknown prefab %s (regenerate with spliti gen)", prefab)
	}
	e := fn(c, t)
	scene.Spawn(c, instance, e)
	return e, nil
}

// syncInstanceFromModel makes the live world match one model spawn: the entity
// is created from its prefab when missing, then transform, scene.Set overrides,
// scene.Remove strips, and the parent link are applied. Shared by undo-of-
// delete, Reload, and the file watcher.
func syncInstanceFromModel(c *app.Ctx, st *state, sp *srcmodel.Spawn) error {
	e, ok := entityByInstance(c, sp.Instance)
	if !ok {
		var err error
		var t render3d.Transform3D
		if sp.Transform != nil {
			t = sp.Transform.Value()
		} else {
			t = render3d.XForm()
		}
		e, err = spawnLive(c, st, sp.Instance, sp.Prefab, t)
		if err != nil {
			return err
		}
	} else if sp.Transform != nil && sp.Transform.Editable {
		tm := generic.NewMap[render3d.Transform3D](c.World())
		if tm.Has(e) {
			*tm.Get(e) = sp.Transform.Value()
		}
	}
	for _, sl := range sp.Sets {
		if !sl.Editable {
			continue
		}
		ti := st.typeByQual(sl.Type)
		if ti == nil {
			continue
		}
		v := reflect.New(ti.Type).Elem()
		if err := srcmodel.ApplyLit(sl.Lit(), v); err != nil {
			st.status(fmt.Sprintf("%s: %v", sp.Instance, err))
			continue
		}
		ti.SetValue(c.World(), e, v.Interface())
	}
	for _, rl := range sp.Removes {
		if ti := st.typeByQual(rl.Type); ti != nil && ti.Has(c.World(), e) {
			ti.Remove(c.World(), e)
		}
	}
	// Parent link last (the parent may have been synced just before).
	pm := generic.NewMap[render3d.Parent](c.World())
	if sp.ParentLine != nil {
		if pe, ok := entityByInstance(c, sp.ParentLine.Parent); ok {
			scene.Set(c, e, render3d.Parent{Entity: pe})
		}
	} else if pm.Has(e) {
		// Only clear parents that point at *named* instances: prefabs may
		// build internal unnamed hierarchies the scene file knows nothing of.
		if instanceName(c, pm.Get(e).Entity) != "" {
			p1 := generic.NewMap1[render3d.Parent](c.World())
			p1.Remove(e)
		}
	}
	return nil
}

// typeByQual finds a registered type by its written form ("components.Health").
func (st *state) typeByQual(qual string) *registry.TypeInfo {
	for _, ti := range st.reg.Types() {
		if qualName(ti) == qual {
			return ti
		}
	}
	return nil
}

// despawnSubtree removes an entity and every descendant immediately (the
// queued render3d.DespawnRecursive applies at end of stage, too late for
// command semantics).
func despawnSubtree(c *app.Ctx, root ecs.Entity) {
	w := c.World()
	if !w.Alive(root) {
		return
	}
	children := map[ecs.Entity][]ecs.Entity{}
	app.Query1[render3d.Parent](c, func(e ecs.Entity, p *render3d.Parent) {
		children[p.Entity] = append(children[p.Entity], e)
	})
	visited := map[ecs.Entity]bool{root: true}
	doomed := []ecs.Entity{root}
	for i := 0; i < len(doomed); i++ {
		for _, ch := range children[doomed[i]] {
			if !visited[ch] {
				visited[ch] = true
				doomed = append(doomed, ch)
			}
		}
	}
	for _, e := range doomed {
		if w.Alive(e) {
			w.RemoveEntity(e)
		}
	}
}

// namedSubtreeLeafFirst lists the instance names in root's subtree (root
// included), deepest first, so source deletion never leaves a parent line
// pointing at a removed instance.
func namedSubtreeLeafFirst(c *app.Ctx, root ecs.Entity) []string {
	type rec struct {
		name  string
		depth int
	}
	var out []rec
	app.Query1[scene.Name](c, func(e ecs.Entity, n *scene.Name) {
		d, ok := depthUnder(c, e, root)
		if ok {
			out = append(out, rec{n.Value, d})
		}
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].depth > out[j].depth })
	names := make([]string, len(out))
	for i, r := range out {
		names[i] = r.name
	}
	return names
}

// depthUnder walks e's parent chain looking for root; returns the link count.
func depthUnder(c *app.Ctx, e, root ecs.Entity) (int, bool) {
	if e == root {
		return 0, true
	}
	pm := generic.NewMap[render3d.Parent](c.World())
	depth := 0
	for cur := e; depth < 1024; depth++ {
		if !pm.Has(cur) {
			return 0, false
		}
		cur = pm.Get(cur).Entity
		if cur == root {
			return depth + 1, true
		}
		if !c.World().Alive(cur) {
			return 0, false
		}
	}
	return 0, false
}

// isDescendant reports whether e sits in root's subtree.
func isDescendant(c *app.Ctx, e, root ecs.Entity) bool {
	_, ok := depthUnder(c, e, root)
	return ok
}

// deselectIf clears the selection when it points at (a descendant of) e.
func (st *state) deselectIf(e ecs.Entity) {
	if st.hasSelected && st.selected == e {
		st.hasSelected = false
	}
}
