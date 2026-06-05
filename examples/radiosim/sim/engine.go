package sim

import "github.com/hvuhsg/spliti/plugin/render3d/m"

// Scene is the static world the engines query: the planar faces, an optional BVH
// over them, the per-face material assignment, the material database, and (in
// later phases) diffracting edges and weather. Keeping the scene separate from
// the transmitter and config lets a single scene be re-used across many Tx/Rx
// evaluations.
type Scene struct {
	Faces   []Face
	BVH     *BVH
	FaceMat []MaterialID // parallel to Faces; missing entries default to MaterialID(0)
	Mats    *MaterialDB  // material database; nil falls back to DefaultMaterialDB
	Edges   []Edge       // diffracting wedges extracted from the faces
	Weather Weather      // atmospheric conditions applied as per-length path loss
}

// NewScene builds a Scene from faces, constructs the BVH and diffracting-edge
// list, and assigns every face the default material (concrete). Callers override
// individual entries of FaceMat to paint walls, ground, glass, etc.
func NewScene(faces []Face) *Scene {
	fm := make([]MaterialID, len(faces))
	return &Scene{
		Faces:   faces,
		BVH:     BuildBVH(faces),
		FaceMat: fm,
		Mats:    DefaultMaterialDB(),
		Edges:   FindEdges(faces),
	}
}

// MaterialOf returns the material assigned to face fi, falling back to the
// database's default entry when the face has no explicit assignment.
func (sc *Scene) MaterialOf(fi int) Material {
	db := sc.Mats
	if db == nil {
		db = DefaultMaterialDB()
	}
	id := MaterialID(0)
	if fi >= 0 && fi < len(sc.FaceMat) {
		id = sc.FaceMat[fi]
	}
	return db.Get(id)
}

// Interaction is the per-surface physics shared by every engine: given a ray
// striking face fi at point q with incoming direction in, at frequency fHz and
// polarization pol, it returns the complex gain and outgoing direction for a
// reflection or a transmission. The image method uses only the gain (its geometry
// is already known); the ray-launching engine also uses the outgoing direction to
// continue the ray. Diffraction joins this interface in a later phase.
type Interaction interface {
	OnReflect(fi int, q, in m.Vec3, fHz float64, pol Pol) (gain complex128, out m.Vec3)
	OnTransmit(fi int, q, in m.Vec3, fHz float64, pol Pol) (gain complex128, out m.Vec3)
}

// Config tunes a propagation engine. MaxOrder bounds the interaction depth (0 =
// direct only, 1 = single bounce, 2 = double bounce); higher orders grow
// combinatorially in the image method, so 1–2 is its real-time range. Reflection
// is the (real) reflection coefficient Γ applied per bounce — a placeholder for
// the per-material, angle- and frequency-dependent Fresnel coefficients added in
// a later phase. A physical wall is partially absorbing and phase-inverting, so a
// value like -0.7 is typical; -1 models a perfect conductor.
type Config struct {
	MaxOrder   int
	Reflection float64
	// Transmission enables propagation through walls: the direct path is
	// attenuated by the Fresnel slab transmission of each wall it crosses rather
	// than being blocked outright.
	Transmission bool
	// Diffraction enables single-edge knife-edge diffraction around building
	// corners and roof eaves, so shadowed receivers receive a non-zero field.
	Diffraction bool

	// NumRays is the SBR ray budget per transmitter (the accuracy/speed knob);
	// 0 selects a default. Workers is the goroutine count for SBR (0 = CPUs−2).
	// RxRadiusM overrides the adaptive SBR reception-sphere radius when positive.
	NumRays   int
	Workers   int
	RxRadiusM float64
}

// DefaultConfig is a reasonable single-bounce setup.
func DefaultConfig() Config { return Config{MaxOrder: 1, Reflection: -0.7} }

// Engine finds the propagation paths from a transmitter to a receiver (and over a
// coverage grid) within a scene. It is the single seam the application talks to:
// the exact image-method engine and the real-time shooting-bouncing-rays engine
// both implement it, so swapping engines — or moving to a GPU engine later — does
// not touch the visualization, controls, or HUD.
type Engine interface {
	// Paths returns every propagation path from tx to rx through the scene.
	Paths(tx Tx, rx Rx, sc *Scene, cfg Config) []Path
	// Coverage evaluates received power (watts) at every grid cell from tx.
	Coverage(tx Tx, grid Grid, sc *Scene, cfg Config) []float64
}
