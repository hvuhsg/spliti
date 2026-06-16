// Package inspect dumps the live ECS world to JSON so an agent (or a test, or a
// debugger) can read game state as data and assert on it — "player.x > 0",
// "score == 3", "len(enemies) == 5".
//
// It is the "observe" half of the agent loop (write → run → observe → correct):
// pair it with the deterministic virtual clock (plugin/time Manual mode) and a
// seeded plugin/rng to get reproducible runs whose state you can diff.
//
// The dump is fully generic — no per-game registration or code generation. It
// walks arche's public reflection surface (World.Ids → ecs.ComponentInfo →
// World.Get) so every component on every entity is captured by type, and
// entity-reference fields serialize stably as arche's [id, gen] pairs (the
// engine's Parent links and game ecs.Entity fields alike). Resources are dumped
// best-effort: anything that marshals to meaningful JSON is included; engine
// internals with no exported state (which marshal to "{}") and values JSON
// cannot represent (funcs, channels) are skipped, so the output stays focused on
// game state.
package inspect

import (
	"encoding/json"
	"reflect"
	"sort"

	"github.com/hvuhsg/spliti/app"
	"github.com/mlange-42/arche/ecs"
)

// Dump is a snapshot of the world as plain data.
type Dump struct {
	Entities  []EntityDump               `json:"entities"`
	Resources map[string]json.RawMessage `json:"resources,omitempty"`
}

// EntityDump is one entity and its components, keyed by short type name
// (e.g. "components.Spinner", "render3d.Transform3D").
type EntityDump struct {
	ID         uint32                     `json:"id"`
	Gen        uint32                     `json:"gen"`
	Components map[string]json.RawMessage `json:"components"`
}

// World captures the current world as a structured Dump.
func World(c *app.Ctx) Dump {
	w := c.World()

	// Collect entities first: the world is locked while a query is open, and
	// the per-component reads below must run with no query in flight.
	var all []ecs.Entity
	q := w.Query(ecs.All())
	for q.Next() {
		all = append(all, q.Entity())
	}

	d := Dump{Entities: make([]EntityDump, 0, len(all))}
	for _, e := range all {
		ed := EntityDump{ID: e.ID(), Gen: e.Generation(), Components: map[string]json.RawMessage{}}
		for _, id := range w.Ids(e) {
			info, ok := ecs.ComponentInfo(w, id)
			if !ok {
				continue
			}
			// reflect.NewAt over arche's component storage gives a value over
			// the live data; .Interface() copies it out for marshaling.
			rv := reflect.NewAt(info.Type, w.Get(e, id)).Elem()
			ed.Components[shortType(info.Type)] = marshalSafe(rv.Interface())
		}
		d.Entities = append(d.Entities, ed)
	}

	d.Resources = dumpResources(c.App())
	if len(d.Resources) == 0 {
		d.Resources = nil
	}
	return d
}

// WorldJSON returns the world dump as indented JSON. It never fails on an
// unmarshalable component or resource — those are skipped or replaced with an
// error placeholder so a harness can always read the rest of the state.
func WorldJSON(c *app.Ctx) []byte {
	b, err := json.MarshalIndent(World(c), "", "  ")
	if err != nil {
		// World() only produces json.RawMessage leaves that already marshaled,
		// so the top-level marshal cannot realistically fail; guard anyway.
		b, _ = json.Marshal(map[string]string{"_error": err.Error()})
	}
	return b
}

// dumpResources marshals each installed resource best-effort, keeping only those
// with meaningful JSON (skips "{}"/"null" — engine internals with no exported
// state — and anything JSON cannot represent).
func dumpResources(a *app.App) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for tp, v := range a.Resources() {
		raw, ok := tryMarshal(v)
		if !ok {
			continue
		}
		s := string(raw)
		if s == "{}" || s == "null" {
			continue
		}
		out[shortType(tp)] = raw
	}
	return out
}

// marshalSafe marshals v, returning a {"_error": ...} placeholder rather than
// failing the whole dump when a component cannot be represented as JSON.
func marshalSafe(v any) json.RawMessage {
	if raw, ok := tryMarshal(v); ok {
		return raw
	}
	raw, _ := json.Marshal(map[string]string{"_error": "not JSON-serializable"})
	return raw
}

// tryMarshal marshals v, recovering from any panic and reporting success.
func tryMarshal(v any) (raw json.RawMessage, ok bool) {
	defer func() {
		if recover() != nil {
			raw, ok = nil, false
		}
	}()
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return b, true
}

// shortType renders a reflect.Type as "pkg.Name" using the last path element of
// its package — matching how scene files qualify component types. Pointer types
// resolve to their element; unnamed types fall back to String().
func shortType(tp reflect.Type) string {
	for tp.Kind() == reflect.Pointer {
		tp = tp.Elem()
	}
	name := tp.Name()
	if name == "" {
		return tp.String()
	}
	pkg := tp.PkgPath()
	if pkg == "" {
		return name
	}
	if i := lastSlash(pkg); i >= 0 {
		pkg = pkg[i+1:]
	}
	return pkg + "." + name
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

// SortedEntityIDs returns the dump's entity IDs in ascending order — a small
// convenience for stable test assertions.
func (d Dump) SortedEntityIDs() []uint32 {
	ids := make([]uint32, len(d.Entities))
	for i, e := range d.Entities {
		ids[i] = e.ID
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
