package sim

// Pol is a wave polarization relative to the plane of incidence: TE (transverse
// electric, "perpendicular", s-polarization) or TM (transverse magnetic,
// "parallel", p-polarization). The Fresnel coefficients differ between the two
// (see fresnel.go); the image method tracks a single polarization per evaluation.
type Pol int

const (
	TE Pol = iota // perpendicular / s-polarization
	TM            // parallel / p-polarization
)

// MaterialID indexes the MaterialDB; assigned per Face via Scene.FaceMat. The
// zero value selects the database's first entry (concrete), so faces with no
// explicit assignment get a sensible default.
type MaterialID int

// Named materials, used as indices into DefaultMaterialDB.
const (
	MatConcrete MaterialID = iota
	MatBrick
	MatGlass
	MatWood
	MatDryGround
	MatWetGround
	MatMetal
)

// Material is an ITU-R P.2040 building material. Its relative permittivity and
// conductivity follow the recommendation's power-law fits in frequency (f in
// GHz): ε' = A·f^B and σ = C·f^D (S/m). ThicknessM is the slab thickness used by
// the transmission model. PEC marks a perfect electric conductor (metal): it
// reflects with |Γ| = 1 and transmits nothing, so the frequency fits are ignored.
//
// In this phase the coefficients are carried but the reflection coefficient is
// still a scalar placeholder (see interaction.go); the Fresnel layer that
// consumes ε(f) lands in the next phase.
type Material struct {
	Name       string
	A, B       float64 // relative permittivity  ε' = A·f_GHz^B
	C, D       float64 // conductivity (S/m)      σ  = C·f_GHz^D
	ThicknessM float64 // wall thickness for the transmission model
	PEC        bool    // perfect electric conductor (metal)
}

// MaterialDB is the named material database indexed by MaterialID.
type MaterialDB struct{ mats []Material }

// Get returns the material for id, clamping out-of-range ids to the first entry.
func (db *MaterialDB) Get(id MaterialID) Material {
	if int(id) < 0 || int(id) >= len(db.mats) {
		return db.mats[0]
	}
	return db.mats[id]
}

// Len returns the number of materials in the database.
func (db *MaterialDB) Len() int { return len(db.mats) }

// DefaultMaterialDB returns the standard set of materials with ITU-R P.2040-1
// (Table 3) frequency fits, plus dry/wet ground and a perfect-conductor metal.
// The order matches the Mat* constants.
func DefaultMaterialDB() *MaterialDB {
	return &MaterialDB{mats: []Material{
		MatConcrete:  {Name: "concrete", A: 5.31, B: 0, C: 0.0326, D: 0.8095, ThicknessM: 0.30},
		MatBrick:     {Name: "brick", A: 3.75, B: 0, C: 0.038, D: 0, ThicknessM: 0.20},
		MatGlass:     {Name: "glass", A: 6.27, B: 0, C: 0.0043, D: 1.1925, ThicknessM: 0.006},
		MatWood:      {Name: "wood", A: 1.99, B: 0, C: 0.0047, D: 1.0718, ThicknessM: 0.03},
		MatDryGround: {Name: "dry ground", A: 3.0, B: 0, C: 0.00015, D: 2.52, ThicknessM: 100},
		MatWetGround: {Name: "wet ground", A: 30.0, B: 0, C: 0.15, D: 1.5, ThicknessM: 100},
		MatMetal:     {Name: "metal", A: 1, B: 0, C: 1e7, D: 0, ThicknessM: 0.01, PEC: true},
	}}
}
