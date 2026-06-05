package sim

import (
	"math"
	"math/cmplx"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// Edge is a diffracting wedge: a line segment A→B shared by two non-coplanar
// faces, with the wedge factor n (the exterior — air — wedge angle is n·π).
// Vertical building corners and roof eaves become edges; a flat junction (n≈1)
// does not diffract and is not emitted. WedgeN follows from the angle between the
// two faces' outward normals: n = 1 + acos(n₁·n₂)/π (1.5 for a 90° corner, 2 for
// a thin screen).
type Edge struct {
	A, B         m.Vec3
	FaceL, FaceR int
	WedgeN       float64
}

// Dir returns the unit edge direction A→B.
func (e Edge) Dir() m.Vec3 { return e.B.Sub(e.A).Normalize() }

// FindEdges extracts diffracting wedges from the face set: rectangle edges shared
// by two faces whose normals are not parallel. It is built once per scene
// alongside the BVH. Endpoints are quantized so coincident corners from adjacent
// box faces match despite floating-point noise.
func FindEdges(faces []Face) []Edge {
	type rec struct {
		fi   int
		a, b m.Vec3
	}
	shared := map[[2]ipoint][]rec{}
	for fi, f := range faces {
		c := f.Corners()
		for i := 0; i < 4; i++ {
			a, b := c[i], c[(i+1)%4]
			key := edgeKey(a, b)
			shared[key] = append(shared[key], rec{fi, a, b})
		}
	}

	var edges []Edge
	for _, rs := range shared {
		if len(rs) < 2 {
			continue
		}
		l, r := rs[0], rs[1]
		n := 1 + math.Acos(clampf64(float64(faces[l.fi].Normal.Dot(faces[r.fi].Normal)), -1, 1))/math.Pi
		if n < 1.05 { // near-flat: not a diffracting wedge
			continue
		}
		edges = append(edges, Edge{A: l.a, B: l.b, FaceL: l.fi, FaceR: r.fi, WedgeN: n})
	}
	return edges
}

// ipoint is a quantized point used to match coincident edge endpoints.
type ipoint struct{ x, y, z int64 }

func quantize(p m.Vec3) ipoint {
	const s = 1e4 // 0.1 mm grid
	return ipoint{int64(math.Round(float64(p.X) * s)), int64(math.Round(float64(p.Y) * s)), int64(math.Round(float64(p.Z) * s))}
}

// edgeKey is an order-independent key for the segment a→b.
func edgeKey(a, b m.Vec3) [2]ipoint {
	pa, pb := quantize(a), quantize(b)
	if less(pb, pa) {
		pa, pb = pb, pa
	}
	return [2]ipoint{pa, pb}
}

func less(a, b ipoint) bool {
	if a.x != b.x {
		return a.x < b.x
	}
	if a.y != b.y {
		return a.y < b.y
	}
	return a.z < b.z
}

// diffractionPoint returns the point Q on edge e where the path S→Q→F is
// stationary (the Keller diffraction point: incident and diffracted rays make
// equal angles with the edge), and whether it lies strictly in the edge interior.
// |S−Q|+|Q−F| is convex in the edge parameter, so a ternary search converges.
func diffractionPoint(e Edge, s, f m.Vec3) (m.Vec3, bool) {
	d := e.B.Sub(e.A)
	at := func(t float32) m.Vec3 { return e.A.Add(d.Scale(t)) }
	cost := func(t float32) float64 {
		q := at(t)
		return float64(s.Sub(q).Length() + q.Sub(f).Length())
	}
	lo, hi := float32(0), float32(1)
	for i := 0; i < 80; i++ {
		m1 := lo + (hi-lo)/3
		m2 := hi - (hi-lo)/3
		if cost(m1) < cost(m2) {
			hi = m2
		} else {
			lo = m1
		}
	}
	t := (lo + hi) / 2
	if t <= 1e-3 || t >= 1-1e-3 {
		return m.Vec3{}, false // stationary point off the finite edge
	}
	return at(t), true
}

// KnifeEdge returns the Fresnel–Kirchhoff knife-edge diffraction coefficient for
// the dimensionless diffraction parameter v: F(v) = (1+j)/2 · ∫ᵥ^∞ e^{−jπτ²/2}dτ.
// |F| → 1 in full illumination (v → −∞), 1/2 at grazing (v = 0, the classic
// −6 dB), and → 0 in deep shadow (v → +∞). It is the closed-form oracle the UTD
// coefficient is checked against and the diffraction model the engine uses.
func KnifeEdge(v float64) complex128 {
	c, s := fresnelCS(v)
	// (1+j)/2 · [(1/2 − C) − j(1/2 − S)]
	tail := complex(0.5-c, -(0.5 - s))
	return complex(0.5, 0.5) * tail
}

// fresnelCS returns the Fresnel cosine and sine integrals C(u)=∫₀ᵘcos(πt²/2)dt and
// S(u)=∫₀ᵘsin(πt²/2)dt using the Abramowitz & Stegun 7.3.32 rational fit (odd in u).
func fresnelCS(u float64) (c, s float64) {
	sgn := 1.0
	if u < 0 {
		sgn, u = -1, -u
	}
	f := (1 + 0.926*u) / (2 + 1.792*u + 3.104*u*u)
	g := 1 / (2 + 4.142*u + 3.492*u*u + 6.670*u*u*u)
	a := math.Pi / 2 * u * u
	c = 0.5 + f*math.Sin(a) - g*math.Cos(a)
	s = 0.5 - f*math.Cos(a) - g*math.Sin(a)
	return sgn * c, sgn * s
}

// fresnelTransition is the UTD transition function F(x) = 2j√x·e^{jx}·∫_{√x}^∞
// e^{−jτ²}dτ for x ≥ 0. F(x) → 1 as x → ∞ (away from a shadow/reflection boundary)
// and → 0 as x → 0 (on the boundary), smoothing the GTD singularity.
func fresnelTransition(x float64) complex128 {
	if x <= 0 {
		return 0
	}
	u := math.Sqrt(2 * x / math.Pi)
	c, s := fresnelCS(u)
	// ∫_{√x}^∞ e^{−jτ²}dτ = √(π/2)·[(1/2−C) − j(1/2−S)]
	integral := complex(math.Sqrt(math.Pi/2), 0) * complex(0.5-c, -(0.5-s))
	return complex(0, 2*math.Sqrt(x)) * cmplx.Exp(complex(0, x)) * integral
}

// DiffractionUTD returns the UTD wedge diffraction coefficient for a ray from
// source to field point that bends at point q on edge e, at wavelength lambda. It
// models an absorbing wedge: only the incident shadow-boundary terms are kept
// (the reflection-boundary terms, which need the face reflection coefficients,
// are omitted), so in deep shadow it reduces to the knife-edge coefficient. The
// engine currently propagates diffraction with the simpler, equally-valid
// KnifeEdge model; this coefficient is the higher-fidelity upgrade, validated
// against KnifeEdge in the tests.
//
// The diffracted field is E(field) = E_inc(q) · D · √(s'/(s(s'+s))) · e^{-jks},
// where s' = |source−q|, s = |field−q|, and D is this coefficient.
func DiffractionUTD(e Edge, q, source, field m.Vec3, lambda float64) complex128 {
	k := 2 * math.Pi / lambda
	ehat := e.Dir()

	rp := source.Sub(q) // q → source
	rf := field.Sub(q)  // q → field
	sP := float64(rp.Length())
	s := float64(rf.Length())
	if sP < 1e-9 || s < 1e-9 {
		return 0
	}
	beta0 := angleWithEdge(rf, ehat)
	sinb := math.Sin(beta0)
	// Spherical-wave distance parameter.
	L := sP * s * sinb * sinb / (sP + s)
	// φ − φ': the signed angle from the incident side to the diffraction side,
	// measured in the plane perpendicular to the edge. Only this difference enters
	// the absorbing-wedge coefficient, so the choice of reference axis is moot.
	beta := signedPerpAngle(rp, rf, ehat)
	return utdCoefficient(beta, beta0, L, e.WedgeN, k)
}

// utdCoefficient evaluates the absorbing-wedge UTD coefficient from the angle
// difference β = φ − φ' (in the plane perpendicular to the edge), the Keller-cone
// half-angle β₀, the spherical distance parameter L, the wedge factor n, and the
// wavenumber k.
func utdCoefficient(beta, beta0, L, n, k float64) complex128 {
	sinb := math.Sin(beta0)
	if sinb < 1e-6 {
		sinb = 1e-6
	}
	pre := -cmplx.Exp(complex(0, -math.Pi/4)) / complex(2*n*math.Sqrt(2*math.Pi*k)*sinb, 0)
	t1 := complex(cot((math.Pi+beta)/(2*n)), 0) * fresnelTransition(k*L*aPlus(beta, n))
	t2 := complex(cot((math.Pi-beta)/(2*n)), 0) * fresnelTransition(k*L*aMinus(beta, n))
	return pre * (t1 + t2)
}

// signedPerpAngle returns the signed angle from a to b measured in the plane
// perpendicular to ehat (right-handed about ehat), in (−π, π].
func signedPerpAngle(a, b, ehat m.Vec3) float64 {
	ap := a.Sub(ehat.Scale(a.Dot(ehat)))
	bp := b.Sub(ehat.Scale(b.Dot(ehat)))
	if ap.Length() < 1e-9 || bp.Length() < 1e-9 {
		return 0
	}
	ap = ap.Normalize()
	bp = bp.Normalize()
	cross := float64(ehat.Dot(ap.Cross(bp)))
	dot := float64(ap.Dot(bp))
	return math.Atan2(cross, dot)
}

// aPlus/aMinus are the UTD angular functions a±(β) = 2cos²((2πnN± − β)/2), with
// N± the integers that bring the argument nearest a multiple of 2π.
func aPlus(beta, n float64) float64 {
	N := math.Round((beta + math.Pi) / (2 * math.Pi * n))
	c := math.Cos((2*math.Pi*n*N - beta) / 2)
	return 2 * c * c
}

func aMinus(beta, n float64) float64 {
	N := math.Round((beta - math.Pi) / (2 * math.Pi * n))
	c := math.Cos((2*math.Pi*n*N - beta) / 2)
	return 2 * c * c
}

func cot(x float64) float64 { return math.Cos(x) / math.Sin(x) }

// angleWithEdge returns the angle (radians) between a direction and the edge.
func angleWithEdge(d, ehat m.Vec3) float64 {
	c := math.Abs(float64(d.Normalize().Dot(ehat)))
	if c > 1 {
		c = 1
	}
	return math.Acos(c)
}
