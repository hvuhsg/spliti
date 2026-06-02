package editor

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/components"
	"github.com/hvuhsg/spliti/editor/state"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// PlaySnapshot is the editor's saved-state for Play→Stop. When the user
// presses Play, we encode every EditorMeta entity's components through
// the components registry and stash them here. On Stop, we despawn the
// (possibly moved/destroyed) play-time entities and replay this
// snapshot to restore the scene to its pre-play state.
//
// Encoding via the registry is the same path used by scene save, so the
// snapshot reuses tested code and round-trips perfectly. Performance is
// O(N entities × M components); v1 scenes are tiny enough that this is
// invisible.
type PlaySnapshot struct {
	Entries []SnapshotEntry
}

// SnapshotEntry mirrors a single SceneEntity in memory. SceneName lets
// the hierarchy panel show the same labels post-restore.
type SnapshotEntry struct {
	SceneName  string
	Components map[string]map[string]any
}

// CaptureSnapshot encodes every EditorMeta entity into a PlaySnapshot.
// Same locked-world caveat as SaveScene: collect during Query, encode
// after.
func CaptureSnapshot(c *app.Ctx) *PlaySnapshot {
	w := c.World()
	type pending struct {
		entity    ecs.Entity
		sceneName string
	}
	var ps []pending
	app.Query1[state.EditorMeta](c, func(e ecs.Entity, m *state.EditorMeta) {
		ps = append(ps, pending{entity: e, sceneName: m.SceneName})
	})
	snap := &PlaySnapshot{Entries: make([]SnapshotEntry, 0, len(ps))}
	for _, p := range ps {
		entry := SnapshotEntry{
			SceneName:  p.sceneName,
			Components: map[string]map[string]any{},
		}
		for _, d := range components.Registry() {
			val, ok := d.Encode(w, p.entity)
			if !ok {
				continue
			}
			entry.Components[d.Name] = val
		}
		snap.Entries = append(snap.Entries, entry)
	}
	return snap
}

// RestoreSnapshot despawns every EditorMeta entity in the world, then
// re-spawns from snap.Entries with the same component values. After
// this returns and the Commands queue flushes, the world is in the
// state it was when the snapshot was captured.
//
// Caller is responsible for ensuring no system mid-iteration depends on
// the to-be-despawned entities. Calling from an OnExit hook for the
// Playing state — between frames — is the intended use.
func RestoreSnapshot(c *app.Ctx, snap *PlaySnapshot) {
	if snap == nil {
		return
	}
	cmds := c.Commands()
	app.Query1[state.EditorMeta](c, func(e ecs.Entity, _ *state.EditorMeta) {
		cmds.Despawn(e)
	})
	// Apply queued despawns immediately so the new spawns can't collide
	// with stale entity IDs (arche reuses IDs).
	cmds.Add(func(w *ecs.World) {
		// no-op marker; the scheduler runs apply() at end of stage which
		// drains all queued ops. We rely on that.
	})

	// Spawn replacements. We build them through a custom Commands.Add
	// closure so they happen AFTER the despawns above, in the same
	// drain pass.
	for _, entry := range snap.Entries {
		entry := entry // capture
		cmds.Add(func(w *ecs.World) {
			e := w.NewEntity()
			// Attach EditorMeta first.
			meta := components.ByName("editorMeta")
			_ = meta // EditorMeta isn't in the components registry by name;
			// it's the editor's structural marker. We attach it
			// directly.
			attachEditorMeta(w, e, entry.SceneName)
			// Apply each saved component via the registry.
			ctx := freshCtxForRestore(c, w)
			for cname, cval := range entry.Components {
				d := components.ByName(cname)
				if d == nil {
					continue
				}
				_ = d.Decode(ctx, e, cval)
			}
		})
	}
}

// attachEditorMeta is a small helper that adds + populates EditorMeta
// without going through the (intentionally absent) registry entry for
// it. Centralised so we don't repeat the boilerplate.
func attachEditorMeta(w *ecs.World, e ecs.Entity, sceneName string) {
	m := generic.NewMap1[state.EditorMeta](w)
	m.Add(e)
	*m.Get(e) = state.EditorMeta{SceneName: sceneName}
}

// freshCtxForRestore returns a *app.Ctx wrapping the same App but a
// freshly-bound world reference. Needed because the registry's Decode
// functions take a Ctx (not a raw World).
//
// We'd rather not synthesize a Ctx, but the registry contract chose Ctx
// for symmetry with the rest of the editor. The original c is what we
// have; we hand it back unchanged because c.World() and w are the same
// world when this runs (we're inside its Commands queue's apply).
func freshCtxForRestore(c *app.Ctx, _ *ecs.World) *app.Ctx { return c }
