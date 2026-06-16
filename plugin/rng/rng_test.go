package rng_test

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/rng"
	"github.com/hvuhsg/spliti/schedule"
)

// draw runs an app with the given seed and returns the first n random ints.
func draw(seed int64, n int) []int {
	a := app.New().AddPlugins(rng.Plugin{Seed: seed})
	out := make([]int, 0, n)
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		r := app.GetResource[rng.Rand](c)
		for i := 0; i < n; i++ {
			out = append(out, r.Intn(1_000_000))
		}
	}).SetMaxFrames(1).Run()
	return out
}

func TestSameSeedSameSequence(t *testing.T) {
	a := draw(42, 8)
	b := draw(42, 8)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sequences diverged at %d: %d != %d", i, a[i], b[i])
		}
	}
}

func TestDifferentSeedDiffersSomewhere(t *testing.T) {
	a := draw(1, 16)
	b := draw(2, 16)
	same := true
	for i := range a {
		if a[i] != b[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("seeds 1 and 2 produced identical sequences")
	}
}

func TestResourceIsInstalled(t *testing.T) {
	a := app.New().AddPlugins(rng.Plugin{Seed: 0})
	var got *rng.Rand
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		got = app.GetResource[rng.Rand](c)
	}).SetMaxFrames(1).Run()
	if got == nil || got.Rand == nil {
		t.Fatal("rng.Rand resource not installed")
	}
}
