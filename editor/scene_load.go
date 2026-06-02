package editor

import (
	"fmt"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/components"
	"github.com/hvuhsg/spliti/editor/project"
	"github.com/hvuhsg/spliti/editor/state"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// LoadScene parses path and spawns one entity per [[entity]] block. Each
// entity gets an EditorMeta tag (so save/load round-trips and the
// hierarchy panel lists them) plus whatever components the scene file
// declared, decoded through the components registry.
//
// Components named in the file but missing from the registry are
// silently ignored. The loader returns the first hard decode error
// (badly-typed value in a known component) so the editor's status bar
// can surface it.
//
// The registry is initialised on first call so direct callers (tests,
// programmatic use) don't need to remember to RegisterBuiltins().
// editor.Plugin.Build does the same idempotent check, so wiring through
// the plugin is also fine.
func LoadScene(c *app.Ctx, path string) error {
	if len(components.Registry()) == 0 {
		components.RegisterBuiltins()
	}
	sf, err := project.LoadSceneFile(path)
	if err != nil {
		return err
	}
	w := c.World()
	for _, e := range sf.Entities {
		entity := w.NewEntity()

		// Always attach EditorMeta first so the hierarchy can list this
		// entity even if every other component decoder rejects.
		metaMap := generic.NewMap1[state.EditorMeta](w)
		metaMap.Add(entity)
		*metaMap.Get(entity) = state.EditorMeta{SceneName: e.Name}

		for cname, cval := range e.Components {
			desc := components.ByName(cname)
			if desc == nil {
				continue
			}
			tbl, _ := cval.(map[string]any)
			if tbl == nil {
				continue
			}
			if err := desc.Decode(c, entity, tbl); err != nil {
				return fmt.Errorf("entity %q component %s: %w", e.Name, cname, err)
			}
		}
	}
	return nil
}

// SaveScene writes the world's EditorMeta-tagged entities to path as a
// scene file. Each entity's components are emitted via the registry's
// Encode functions in registration order so on-disk diffs are stable
// across runs.
//
// We collect entity IDs and their EditorMeta values inside the Query
// (during which arche's World is locked), then perform the per-component
// Encode loop OUTSIDE the query — the encoders may call
// ecs.ComponentID[T] for component types not yet seen, which requires
// an unlocked world.
func SaveScene(c *app.Ctx, path, name string) error {
	w := c.World()
	sf := &project.SceneFile{Schema: 1, Name: name}

	type pending struct {
		entity    ecs.Entity
		sceneName string
	}
	var pendings []pending
	app.Query1[state.EditorMeta](c, func(e ecs.Entity, meta *state.EditorMeta) {
		pendings = append(pendings, pending{entity: e, sceneName: meta.SceneName})
	})
	for _, p := range pendings {
		ent := project.SceneEntity{
			Name:       p.sceneName,
			Components: map[string]any{},
		}
		for _, desc := range components.Registry() {
			val, ok := desc.Encode(w, p.entity)
			if !ok {
				continue
			}
			ent.Components[desc.Name] = val
		}
		sf.Entities = append(sf.Entities, ent)
	}
	return project.SaveSceneFile(path, sf)
}
