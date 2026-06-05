package sim

import (
	"math"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// sceneInteraction is the per-surface physics bound to a scene and config. It is
// the single place reflection (and, in later phases, transmission and
// diffraction) is computed, so both the image-method and ray-launching engines
// share identical physics. Reflection uses the angle-, frequency-, and
// polarization-dependent Fresnel coefficients derived from each face material's
// complex permittivity.
type sceneInteraction struct {
	sc  *Scene
	cfg Config
}

// newInteraction builds the interaction the engines use for a given scene/config.
func newInteraction(sc *Scene, cfg Config) Interaction {
	return &sceneInteraction{sc: sc, cfg: cfg}
}

// OnReflect returns the complex reflection gain and the specular outgoing
// direction for a ray hitting face fi. The gain is the Fresnel reflection
// coefficient for the face material at the local incidence angle.
func (it *sceneInteraction) OnReflect(fi int, q, in m.Vec3, fHz float64, pol Pol) (complex128, m.Vec3) {
	n := it.sc.Faces[fi].Normal
	out := specularReflect(in, n)
	mat := it.sc.MaterialOf(fi)
	thetaI := incidenceAngle(in, n)
	gamma, _ := Fresnel(mat, fHz, thetaI, pol)
	return gamma, out
}

// OnTransmit returns the complex transmission gain and the outgoing direction for
// a ray passing through wall face fi. The gain is the Fresnel slab transmission
// for the face material at the local incidence angle. The outgoing ray is kept
// collinear with the incoming ray: the lateral beam offset across a thin wall is
// sub-wavelength at these geometries, a deliberate real-time simplification.
func (it *sceneInteraction) OnTransmit(fi int, q, in m.Vec3, fHz float64, pol Pol) (complex128, m.Vec3) {
	n := it.sc.Faces[fi].Normal
	mat := it.sc.MaterialOf(fi)
	thetaI := incidenceAngle(in, n)
	gain, _ := SlabTransmission(mat, fHz, thetaI, pol)
	return gain, in.Normalize()
}

// incidenceAngle returns the angle (radians) between an incoming ray direction
// and a surface normal, measured from the normal — the angle of incidence. The
// incoming direction points toward the surface; the sign of the normal does not
// matter because the cosine is taken in magnitude.
func incidenceAngle(in, n m.Vec3) float64 {
	cosI := math.Abs(float64(in.Normalize().Dot(n)))
	if cosI > 1 {
		cosI = 1
	}
	return math.Acos(cosI)
}

// specularReflect reflects an incoming unit direction about a surface normal:
// d − 2(d·n)n. The result points away from the surface for an incoming ray.
func specularReflect(d, n m.Vec3) m.Vec3 {
	return d.Sub(n.Scale(2 * d.Dot(n)))
}
