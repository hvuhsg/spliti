package sim

import (
	"math"
	"math/cmplx"
)

// Eps0 is the vacuum permittivity ε₀ in farads per meter.
const Eps0 = 8.8541878128e-12

// ComplexPermittivity returns the material's relative permittivity as a complex
// number at frequency fHz, following the ITU-R P.2040 convention with an e^{jωt}
// time dependence: ε = ε' − j·σ/(ωε₀), where ε' = A·f_GHz^B and σ = C·f_GHz^D
// (S/m). The negative imaginary part represents loss, consistent with the
// e^{-jkL} propagation factor used to build path phases. A perfect conductor
// returns a very large imaginary magnitude.
func (mat Material) ComplexPermittivity(fHz float64) complex128 {
	if mat.PEC {
		return complex(1, -1e12)
	}
	if fHz <= 0 {
		return complex(mat.A, 0)
	}
	fGHz := fHz / 1e9
	epsR := mat.A * math.Pow(fGHz, mat.B)
	sigma := mat.C * math.Pow(fGHz, mat.D)
	omega := 2 * math.Pi * fHz
	return complex(epsR, -sigma/(omega*Eps0))
}

// Fresnel returns the complex reflection coefficient Γ and transmission
// coefficient τ (amplitude) at the interface between vacuum and the material, for
// a wave arriving from vacuum at incidence angle thetaI (radians from the surface
// normal), polarization pol, and frequency fHz. These replace the scalar
// reflection placeholder: Γ now depends on angle, frequency, polarization, and
// the material's complex permittivity.
//
// The forms are the standard non-magnetic Fresnel equations written in terms of
// the complex relative permittivity εr and the incidence angle, using
// √(εr − sin²θi) = n₂cosθt:
//
//	TE (perpendicular): Γ = (cosθi − √(εr−sin²θi)) / (cosθi + √(εr−sin²θi))
//	TM (parallel):      Γ = (εr·cosθi − √(εr−sin²θi)) / (εr·cosθi + √(εr−sin²θi))
//
// A perfect conductor reflects totally: Γ = −1 (TE) or +1 (TM), τ = 0.
func Fresnel(mat Material, fHz, thetaI float64, pol Pol) (gamma, tau complex128) {
	if mat.PEC {
		if pol == TE {
			return complex(-1, 0), 0
		}
		return complex(1, 0), 0
	}

	er := mat.ComplexPermittivity(fHz)
	sinI := math.Sin(thetaI)
	cosI := complex(math.Cos(thetaI), 0)
	root := cmplx.Sqrt(er - complex(sinI*sinI, 0))

	switch pol {
	case TM:
		gamma = (er*cosI - root) / (er*cosI + root)
		tau = 2 * cmplx.Sqrt(er) * cosI / (er*cosI + root)
	default: // TE
		gamma = (cosI - root) / (cosI + root)
		tau = 2 * cosI / (cosI + root)
	}
	return gamma, tau
}

// SlabTransmission returns the complex transmission gain through a wall of the
// material — both vacuum/medium interfaces plus propagation across the slab,
// including internal multiple reflections (the Airy/Fabry-Pérot form) — and the
// refraction angle thetaT (radians) inside the slab. The wall thickness is
// mat.ThicknessM. A wave arrives from vacuum at incidence angle thetaI with
// polarization pol at frequency fHz.
//
// With Γ the single-interface Fresnel reflection (vacuum→medium) and the slab
// phase thickness φ = k₀·d·√(εr−sin²θi):
//
//	T = (1−Γ²)·e^{−jφ} / (1 − Γ²·e^{−2jφ})
//
// The imaginary part of √(εr−sin²θi) makes |e^{−jφ}| < 1 for lossy materials, so
// thick or conductive walls attenuate strongly. A perfect conductor transmits
// nothing.
func SlabTransmission(mat Material, fHz, thetaI float64, pol Pol) (gain complex128, thetaT float64) {
	if mat.PEC || fHz <= 0 || mat.ThicknessM <= 0 {
		return 0, 0
	}
	er := mat.ComplexPermittivity(fHz)
	sinI := math.Sin(thetaI)
	root := cmplx.Sqrt(er - complex(sinI*sinI, 0))

	gamma, _ := Fresnel(mat, fHz, thetaI, pol)
	g2 := gamma * gamma

	k0 := 2 * math.Pi * fHz / SpeedOfLight
	phi := complex(k0*mat.ThicknessM, 0) * root
	e := cmplx.Exp(complex(0, -1) * phi)
	gain = (1 - g2) * e / (1 - g2*e*e)

	// Refraction angle from the real part of the index (Snell).
	nReal := real(cmplx.Sqrt(er))
	if nReal < 1e-9 {
		nReal = 1e-9
	}
	sinT := sinI / nReal
	if sinT > 1 {
		sinT = 1
	}
	thetaT = math.Asin(sinT)
	return gain, thetaT
}
