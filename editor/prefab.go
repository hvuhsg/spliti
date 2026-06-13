package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/srcmodel"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/mlange-42/arche/ecs"
)

// createPrefab extracts a live mesh entity into a new //spliti:entity prefab
// under game/entities. The prefab reproduces the entity's mesh + material and
// scene.Set's its remaining editable components (Transform3D is the spawn
// argument, so it is not baked in). The new function is a fresh Go symbol, so it
// only becomes spawnable after a Rebuild & Restart — flagged here so the toolbar
// banner appears. Children/subtrees are not included (single entity only).
func (st *state) createPrefab(c *app.Ctx, e ecs.Entity) {
	if st.mode != modeEdit {
		st.status("create prefab is disabled during play")
		return
	}
	w := c.World()
	if !w.Alive(e) {
		return
	}
	meshTI, matTI := st.reg.Lookup("MeshRenderer"), st.reg.Lookup("MaterialRef")
	if meshTI == nil || matTI == nil || !meshTI.Has(w, e) || !matTI.Has(w, e) {
		st.status("create prefab: only mesh entities are supported")
		return
	}
	mr, _ := meshTI.Value(w, e).(render3d.MeshRenderer)
	mref, _ := matTI.Value(w, e).(render3d.MaterialRef)

	// Every other visible component becomes a scene.Set in the prefab body.
	var comps []srcmodel.PrefabComponent
	for _, ti := range st.reg.On(w, e) {
		switch ti.Name {
		case "Transform3D", "MeshRenderer", "MaterialRef":
			continue
		}
		comps = append(comps, srcmodel.PrefabComponent{
			TypeName: ti.Pkg + "." + ti.Name,
			Value:    reflect.ValueOf(ti.Value(w, e)),
		})
	}

	base := exportIdent(instanceName(c, e))
	if base == "" {
		base = exportIdent(mr.Mesh)
	}
	if base == "" {
		base = "Entity"
	}
	dir := filepath.Join(st.cfg.ProjectRoot, "game", "entities")
	funcName, file := st.freePrefabName(dir, base)

	src, skipped, err := srcmodel.RenderPrefab(funcName, mr.Mesh, mref.Material, comps)
	if err != nil {
		st.logf(logError, "create prefab: %v", err)
		return
	}
	if err := os.WriteFile(file, src, 0o644); err != nil {
		st.logf(logError, "create prefab: write %s: %v", filepath.Base(file), err)
		return
	}
	st.rebuildNeeded = true
	msg := fmt.Sprintf("created prefab entities.%s in %s - Rebuild & Restart to use it", funcName, filepath.Base(file))
	if len(skipped) > 0 {
		msg += fmt.Sprintf(" (skipped unexpressible: %s)", strings.Join(skipped, ", "))
	}
	st.logf(logInfo, "%s", msg)
}

// freePrefabName returns an unused (SpawnX function name, file path) pair under
// dir, suffixing with a counter when the base collides with a registered prefab
// or an existing file.
func (st *state) freePrefabName(dir, base string) (funcName, file string) {
	taken := func(fn, f string) bool {
		if _, ok := st.cfg.Prefabs["entities."+fn]; ok {
			return true
		}
		_, err := os.Stat(f)
		return err == nil
	}
	fn := "Spawn" + base
	f := filepath.Join(dir, strings.ToLower(base)+".go")
	for i := 2; taken(fn, f); i++ {
		fn = "Spawn" + base + strconv.Itoa(i)
		f = filepath.Join(dir, strings.ToLower(base)+strconv.Itoa(i)+".go")
	}
	return fn, f
}

// exportIdent turns an arbitrary name into an exported Go identifier: it keeps
// letters and digits, drops everything else, uppercases the first letter, and
// drops a leading digit run. Returns "" when nothing usable remains.
func exportIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	out := strings.TrimLeftFunc(b.String(), unicode.IsDigit)
	if out == "" {
		return ""
	}
	return strings.ToUpper(out[:1]) + out[1:]
}
