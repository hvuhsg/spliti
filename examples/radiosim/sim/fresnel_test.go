package sim

import (
	"math"
	"math/cmplx"
	"testing"
)

// cAbs is a terse complex magnitude for tests.
func cAbs(z complex128) float64 { return cmplx.Abs(z) }

// lossless is a fictitious lossless dielectric (εr = 4, n = 2) used for the
// closed-form Fresnel checks where conductivity would muddy exact values.
var lossless = Material{Name: "lossless", A: 4, B: 0, C: 0, D: 0}

// At normal incidence Γ_TE = (1−n)/(1+n) and Γ_TM = (n−1)/(n+1); for n = 2 that
// is ∓1/3.
func TestFresnelNormalIncidence(t *testing.T) {
	gTE, _ := Fresnel(lossless, 1e9, 0, TE)
	if math.Abs(real(gTE)-(-1.0/3)) > 1e-9 || math.Abs(imag(gTE)) > 1e-9 {
		t.Errorf("Γ_TE(0) = %v, want -1/3", gTE)
	}
	gTM, _ := Fresnel(lossless, 1e9, 0, TM)
	if math.Abs(real(gTM)-(1.0/3)) > 1e-9 || math.Abs(imag(gTM)) > 1e-9 {
		t.Errorf("Γ_TM(0) = %v, want +1/3", gTM)
	}
}

// At the Brewster angle θB = atan(n) the parallel (TM) reflection vanishes.
func TestFresnelBrewster(t *testing.T) {
	thetaB := math.Atan(2.0) // atan(√εr), εr = 4
	gTM, _ := Fresnel(lossless, 1e9, thetaB, TM)
	if cAbs(gTM) > 1e-9 {
		t.Errorf("|Γ_TM(Brewster)| = %v, want ~0", cAbs(gTM))
	}
	// TE does not vanish at the Brewster angle.
	gTE, _ := Fresnel(lossless, 1e9, thetaB, TE)
	if cAbs(gTE) < 0.1 {
		t.Errorf("|Γ_TE(Brewster)| = %v, should be substantial", cAbs(gTE))
	}
}

// At grazing incidence both polarizations approach total reflection (|Γ| → 1).
func TestFresnelGrazing(t *testing.T) {
	theta := 89.9 * math.Pi / 180
	mat := DefaultMaterialDB().Get(MatConcrete)
	for _, pol := range []Pol{TE, TM} {
		g, _ := Fresnel(mat, 2.4e9, theta, pol)
		if cAbs(g) < 0.99 {
			t.Errorf("|Γ| at grazing (pol %d) = %v, want ~1", pol, cAbs(g))
		}
	}
}

// For a lossless interface, reflected + transmitted power equals the incident
// power: R + T = 1, with T_TE = Re(n₂cosθt)/cosθi · |τ_TE|².
func TestFresnelEnergyConservation(t *testing.T) {
	for _, deg := range []float64{0, 30, 60} {
		theta := deg * math.Pi / 180
		g, tau := Fresnel(lossless, 1e9, theta, TE)
		er := lossless.ComplexPermittivity(1e9)
		sinI := math.Sin(theta)
		root := cmplx.Sqrt(er - complex(sinI*sinI, 0))
		R := cAbs(g) * cAbs(g)
		T := real(root) / math.Cos(theta) * cAbs(tau) * cAbs(tau)
		if math.Abs(R+T-1) > 1e-9 {
			t.Errorf("θ=%g°: R+T = %v, want 1 (R=%v T=%v)", deg, R+T, R, T)
		}
	}
}

// A perfect conductor reflects totally and transmits nothing.
func TestFresnelPECLimit(t *testing.T) {
	metal := DefaultMaterialDB().Get(MatMetal)
	gTE, tTE := Fresnel(metal, 1e9, math.Pi/4, TE)
	if real(gTE) != -1 || imag(gTE) != 0 || tTE != 0 {
		t.Errorf("PEC TE: Γ=%v τ=%v, want -1, 0", gTE, tTE)
	}
	gTM, tTM := Fresnel(metal, 1e9, math.Pi/4, TM)
	if real(gTM) != 1 || imag(gTM) != 0 || tTM != 0 {
		t.Errorf("PEC TM: Γ=%v τ=%v, want +1, 0", gTM, tTM)
	}
}
