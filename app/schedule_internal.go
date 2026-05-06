package app

import (
	"fmt"
	"sort"
)

// systemEntry is the internal representation of a registered system inside a
// stage. Insertion order is preserved via index for stable topo-sort tiebreaks.
type systemEntry struct {
	fn     SystemFunc
	label  string
	runIf  []func(*Ctx) bool
	before []string
	after  []string
	index  int
}

func (e systemEntry) shouldRun(ctx *Ctx) bool {
	for _, c := range e.runIf {
		if !c(ctx) {
			return false
		}
	}
	return true
}

// stageSystems holds the systems registered in a single stage along with the
// resolved execution order computed at App.Run time.
type stageSystems struct {
	entries []systemEntry
	order   []int // indexes into entries, in execution order
}

func (s *stageSystems) add(e systemEntry) {
	e.index = len(s.entries)
	s.entries = append(s.entries, e)
}

// resolve performs a stable topological sort over before/after edges.
func (s *stageSystems) resolve() error {
	n := len(s.entries)
	labels := make(map[string]int, n)
	for i, e := range s.entries {
		if e.label == "" {
			continue
		}
		if _, dup := labels[e.label]; dup {
			return fmt.Errorf("duplicate system label %q", e.label)
		}
		labels[e.label] = i
	}

	edges := make([][]int, n)
	indeg := make([]int, n)
	addEdge := func(from, to int) {
		edges[from] = append(edges[from], to)
		indeg[to]++
	}
	for i, e := range s.entries {
		for _, lbl := range e.before {
			j, ok := labels[lbl]
			if !ok {
				return fmt.Errorf("system %q: Before references unknown label %q", e.label, lbl)
			}
			addEdge(i, j)
		}
		for _, lbl := range e.after {
			j, ok := labels[lbl]
			if !ok {
				return fmt.Errorf("system %q: After references unknown label %q", e.label, lbl)
			}
			addEdge(j, i)
		}
	}

	ready := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if indeg[i] == 0 {
			ready = append(ready, i)
		}
	}
	out := make([]int, 0, n)
	for len(ready) > 0 {
		sort.Ints(ready) // stable insertion-order tiebreak
		next := ready[0]
		ready = ready[1:]
		out = append(out, next)
		for _, j := range edges[next] {
			indeg[j]--
			if indeg[j] == 0 {
				ready = append(ready, j)
			}
		}
	}
	if len(out) != n {
		return fmt.Errorf("ordering cycle detected")
	}
	s.order = out
	return nil
}
