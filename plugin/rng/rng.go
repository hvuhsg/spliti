// Package rng provides spliti's seeded, deterministic random-number resource.
//
// Games that must behave reproducibly under headless verification (the virtual
// clock of plugin/time + `spliti check`) draw randomness from this resource
// rather than the package-level math/rand: the same Seed then yields the same
// sequence on every run, so a recorded run can be replayed and asserted on.
//
// Fetch it like any resource and use it as a *math/rand.Rand:
//
//	r := app.GetResource[rng.Rand](c)
//	angle := r.Float64() * 2 * math.Pi
//
// The source is not safe for concurrent use, which is fine: spliti's schedule
// is single-threaded.
package rng

import (
	mathrand "math/rand"

	"github.com/hvuhsg/spliti/app"
)

// Rand is the engine's random-source resource. It embeds *math/rand.Rand, so
// every method (Float64, Intn, Perm, Shuffle, …) is available directly.
type Rand struct {
	*mathrand.Rand
}

// Plugin installs the Rand resource seeded with Seed. Seed 0 is a valid, fixed
// seed (not "randomize") — the whole point is reproducibility; pick a varying
// seed yourself if you want run-to-run variety.
type Plugin struct {
	Seed int64
}

// Build implements app.Plugin.
func (p Plugin) Build(a *app.App) {
	app.InsertResource(a, &Rand{Rand: mathrand.New(mathrand.NewSource(p.Seed))})
}
