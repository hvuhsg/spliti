package sim

import (
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// m3 is a terse Vec3 constructor for tests.
func m3(x, y, z float32) m.Vec3 { return m.Vec3{X: x, Y: y, Z: z} }

// The default database exposes the named materials in the documented order, with
// metal flagged as a perfect conductor.
func TestDefaultMaterialDB(t *testing.T) {
	db := DefaultMaterialDB()
	cases := []struct {
		id   MaterialID
		name string
	}{
		{MatConcrete, "concrete"},
		{MatBrick, "brick"},
		{MatGlass, "glass"},
		{MatWood, "wood"},
		{MatDryGround, "dry ground"},
		{MatWetGround, "wet ground"},
		{MatMetal, "metal"},
	}
	for _, tc := range cases {
		if got := db.Get(tc.id).Name; got != tc.name {
			t.Errorf("material %d name = %q, want %q", tc.id, got, tc.name)
		}
	}
	if !db.Get(MatMetal).PEC {
		t.Error("metal should be a perfect conductor")
	}
	if db.Get(MatConcrete).PEC {
		t.Error("concrete should not be a perfect conductor")
	}
	// Out-of-range ids clamp to the first entry rather than panicking.
	if db.Get(-1).Name != "concrete" || db.Get(999).Name != "concrete" {
		t.Error("out-of-range id should clamp to the default material")
	}
}

// The reflection coefficient is driven by the face material via Fresnel: a metal
// face reflects totally (Γ = -1 for TE), while a concrete face reflects partially
// (|Γ| < 1) and exactly matches the Fresnel coefficient for its permittivity.
func TestReflectionIsMaterialDriven(t *testing.T) {
	face := NewFace(m3(-1, 0, -1), m3(2, 0, 0), m3(0, 0, 2)) // y=0, normal +Y
	sc := NewScene([]Face{face})

	in := m3(0, -1, 0) // straight down: normal incidence
	cfg := Config{}
	it := newInteraction(sc, cfg)

	// Default material (concrete) matches its Fresnel coefficient at normal
	// incidence and reflects only partially.
	concrete, _ := it.OnReflect(0, m3(0, 0, 0), in, 1e9, TE)
	wantConcrete, _ := Fresnel(sc.MaterialOf(0), 1e9, 0, TE)
	if concrete != wantConcrete {
		t.Errorf("concrete gain = %v, want Fresnel %v", concrete, wantConcrete)
	}
	if mag := cAbs(concrete); mag >= 1 {
		t.Errorf("concrete |Γ| = %v, want < 1 (partial reflection)", mag)
	}

	// Metal reflects totally.
	sc.FaceMat[0] = MatMetal
	it = newInteraction(sc, cfg)
	metal, _ := it.OnReflect(0, m3(0, 0, 0), in, 1e9, TE)
	if real(metal) != -1 || imag(metal) != 0 {
		t.Errorf("metal gain = %v, want -1+0i", metal)
	}
	if metal == concrete {
		t.Error("metal and concrete should reflect differently")
	}
}
