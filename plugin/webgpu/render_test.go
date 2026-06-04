package webgpu

import "testing"

// approx compares two float32s within a small tolerance.
func approx(a, b float32) bool {
	const eps = 1e-5
	d := a - b
	return d < eps && d > -eps
}

func TestOrthoMapsCornersToClip(t *testing.T) {
	m := ortho(80, 60)
	// Multiply the column-major matrix by a world-space point (x,y,0,1).
	apply := func(x, y float32) (cx, cy float32) {
		cx = m[0]*x + m[4]*y + m[8]*0 + m[12]
		cy = m[1]*x + m[5]*y + m[9]*0 + m[13]
		return
	}
	// Top-left world (0,0) -> clip (-1, +1) (Y is flipped, top-left origin).
	if cx, cy := apply(0, 0); !approx(cx, -1) || !approx(cy, 1) {
		t.Fatalf("(0,0) -> (%v,%v), want (-1,1)", cx, cy)
	}
	// Bottom-right world (80,60) -> clip (+1, -1).
	if cx, cy := apply(80, 60); !approx(cx, 1) || !approx(cy, -1) {
		t.Fatalf("(80,60) -> (%v,%v), want (1,-1)", cx, cy)
	}
	// Center maps to the clip origin.
	if cx, cy := apply(40, 30); !approx(cx, 0) || !approx(cy, 0) {
		t.Fatalf("(40,30) -> (%v,%v), want (0,0)", cx, cy)
	}
}

func TestOrthoDegenerateSizeDoesNotPanic(t *testing.T) {
	_ = ortho(0, 0) // guards against divide-by-zero
}

func TestPackInstancesSortsByZAndBatchesByRef(t *testing.T) {
	items := []renderItem{
		{ref: "a", z: 5, inst: instanceData{OffX: 5}},
		{ref: "b", z: 1, inst: instanceData{OffX: 1}},
		{ref: "a", z: 1, inst: instanceData{OffX: 2}}, // same z as b, spawned later
		{ref: "a", z: 3, inst: instanceData{OffX: 3}},
	}
	scratch, batches := packInstances(items, nil, nil)

	// Sorted ascending by z: z1(b), z1(a), z3(a), z5(a).
	wantOff := []float32{1, 2, 3, 5}
	if len(scratch) != len(wantOff) {
		t.Fatalf("scratch len = %d, want %d", len(scratch), len(wantOff))
	}
	for i, w := range wantOff {
		if !approx(scratch[i].OffX, w) {
			t.Fatalf("scratch[%d].OffX = %v, want %v", i, scratch[i].OffX, w)
		}
	}

	// Batches: b(1), then a(3) — the two adjacent "a" entries (z1,z3) merge with
	// the z5 "a" because they are contiguous after the sort.
	if len(batches) != 2 {
		t.Fatalf("got %d batches, want 2: %+v", len(batches), batches)
	}
	if batches[0] != (spriteBatch{ref: "b", start: 0, count: 1}) {
		t.Fatalf("batch[0] = %+v", batches[0])
	}
	if batches[1] != (spriteBatch{ref: "a", start: 1, count: 3}) {
		t.Fatalf("batch[1] = %+v", batches[1])
	}
}

func TestPackInstancesBreaksBatchOnRefChange(t *testing.T) {
	// Interleaved refs at the same z must produce separate batches in order.
	items := []renderItem{
		{ref: "a", z: 0, inst: instanceData{OffX: 1}},
		{ref: "b", z: 0, inst: instanceData{OffX: 2}},
		{ref: "a", z: 0, inst: instanceData{OffX: 3}},
	}
	_, batches := packInstances(items, nil, nil)
	if len(batches) != 3 {
		t.Fatalf("got %d batches, want 3 (a,b,a): %+v", len(batches), batches)
	}
	if batches[0].ref != "a" || batches[1].ref != "b" || batches[2].ref != "a" {
		t.Fatalf("batch order = %s,%s,%s", batches[0].ref, batches[1].ref, batches[2].ref)
	}
}

func TestColorOrDefault(t *testing.T) {
	if got := (Color{}).orDefault(); got != opaqueWhite {
		t.Fatalf("zero Color orDefault = %+v, want opaque white", got)
	}
	custom := Color{R: 0.2, G: 0.4, B: 0.6, A: 0.8}
	if got := custom.orDefault(); got != custom {
		t.Fatalf("non-zero Color changed: %+v", got)
	}
}
