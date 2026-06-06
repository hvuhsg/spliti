package collision

import (
	"reflect"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

func TestFloorDiv(t *testing.T) {
	cases := []struct{ a, b, want int }{
		{0, 16, 0},
		{15, 16, 0},
		{16, 16, 1},
		{-1, 16, -1},
		{-16, 16, -1},
		{-17, 16, -2},
		{-32, 16, -2},
	}
	for _, tc := range cases {
		if got := floorDiv(tc.a, tc.b); got != tc.want {
			t.Errorf("floorDiv(%d,%d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestGrid2_QueryFindsSharedCells(t *testing.T) {
	g := NewGrid2(10)
	g.Insert(0, 0, 0, 5, 5)     // cell (0,0)
	g.Insert(1, 12, 12, 18, 18) // cell (1,1)
	g.Insert(2, 8, 8, 22, 22)   // spans (0,0),(1,0),(0,1),(1,1)

	// A query over cell (0,0) should see bodies 0 and 2, not 1.
	got := g.Query(1, 1, 4, 4)
	if !reflect.DeepEqual(got, []int{0, 2}) {
		t.Fatalf("Query (0,0) = %v, want [0 2]", got)
	}
	// A query over cell (1,1) should see bodies 1 and 2, not 0.
	got = g.Query(13, 13, 17, 17)
	if !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("Query (1,1) = %v, want [1 2]", got)
	}
}

func TestGrid2_NegativeCoordinates(t *testing.T) {
	g := NewGrid2(8)
	g.Insert(0, -8, -8, -4, -4) // cell (-1,-1)
	g.Insert(1, -3, -3, 3, 3)   // spans (-1,-1),(0,-1),(-1,0),(0,0)
	g.Insert(2, 10, 10, 14, 14) // cell (1,1)

	got := g.Query(-7, -7, -5, -5) // inside cell (-1,-1)
	if !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("Query negative cell = %v, want [0 1]", got)
	}
	got = g.Query(11, 11, 13, 13)
	if !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("Query (1,1) = %v, want [2]", got)
	}
}

func TestGrid3_Query(t *testing.T) {
	g := NewGrid3(4)
	g.Insert(0, m.Vec3{X: 0, Y: 0, Z: 0}, m.Vec3{X: 2, Y: 2, Z: 2})       // cell (0,0,0)
	g.Insert(1, m.Vec3{X: -2, Y: -2, Z: -2}, m.Vec3{X: -1, Y: -1, Z: -1}) // cell (-1,-1,-1)
	g.Insert(2, m.Vec3{X: 1, Y: 1, Z: 1}, m.Vec3{X: 9, Y: 9, Z: 9})       // spans (0,0,0)..(2,2,2)

	got := g.Query(m.Vec3{X: 0.5, Y: 0.5, Z: 0.5}, m.Vec3{X: 1.5, Y: 1.5, Z: 1.5})
	if !reflect.DeepEqual(got, []int{0, 2}) {
		t.Fatalf("Query origin cell = %v, want [0 2]", got)
	}
	got = g.Query(m.Vec3{X: -1.8, Y: -1.8, Z: -1.8}, m.Vec3{X: -1.2, Y: -1.2, Z: -1.2})
	if !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("Query negative cell = %v, want [1]", got)
	}
}
