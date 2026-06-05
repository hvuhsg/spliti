package sim

import (
	"math"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// In free space the SBR direct path recovers the Friis received power: the
// closest-aligned ray is given the exact Friis amplitude for its length.
func TestSBRFreeSpaceFriis(t *testing.T) {
	tx := Tx{Pos: m.Vec3{}, PowerW: 1, FreqHz: 1e9}
	rx := Rx{Pos: m.Vec3{X: 50}}
	sc := NewScene(nil)
	cfg := Config{MaxOrder: 0, NumRays: 40000}

	pw, _ := Received(SBREngine{}.Paths(tx, rx, sc, cfg))

	lambda := SpeedOfLight / tx.FreqHz
	friis := math.Pow(lambda/(4*math.Pi*50), 2) // Pt=1, isotropic
	if math.Abs(DBm(pw)-DBm(friis)) > 1.0 {
		t.Errorf("SBR free-space = %.2f dBm, Friis = %.2f dBm", DBm(pw), DBm(friis))
	}
}

// SBR agrees with the exact image engine on a free-space link (within a dB).
func TestSBRvsImageFreeSpace(t *testing.T) {
	tx := Tx{Pos: m.Vec3{}, PowerW: 2, FreqHz: 2.4e9}
	rx := Rx{Pos: m.Vec3{X: 30, Y: 5}}
	sc := NewScene(nil)
	cfg := Config{MaxOrder: 0, NumRays: 40000}

	pSBR, _ := Received(SBREngine{}.Paths(tx, rx, sc, cfg))
	pImg, _ := Received(ImageEngine{}.Paths(tx, rx, sc, cfg))
	if math.Abs(DBm(pSBR)-DBm(pImg)) > 1.0 {
		t.Errorf("SBR %.2f dBm vs image %.2f dBm", DBm(pSBR), DBm(pImg))
	}
}

// SBR results are independent of the worker count (deterministic Fibonacci
// directions, merge-by-closest-approach).
func TestSBRDeterministic(t *testing.T) {
	tx := Tx{Pos: m.Vec3{}, PowerW: 1, FreqHz: 1e9}
	rx := Rx{Pos: m.Vec3{X: 40, Y: 3}}
	sc := NewScene(nil)

	p1, _ := Received(SBREngine{}.Paths(tx, rx, sc, Config{MaxOrder: 0, NumRays: 20000, Workers: 1}))
	p8, _ := Received(SBREngine{}.Paths(tx, rx, sc, Config{MaxOrder: 0, NumRays: 20000, Workers: 8}))
	if math.Abs(p1-p8)/p1 > 1e-9 {
		t.Errorf("SBR not deterministic across workers: %v vs %v", p1, p8)
	}
}

// SBR captures the ground reflection and lands within a few dB of the exact
// image-method two-ray power.
func TestSBRTwoRay(t *testing.T) {
	tx := Tx{Pos: m.Vec3{X: 0, Y: 8}, PowerW: 1, FreqHz: 9e8}
	rx := Rx{Pos: m.Vec3{X: 60, Y: 4}}
	ground := []Face{NewFace(m.Vec3{X: -20, Z: -20}, m.Vec3{X: 200}, m.Vec3{Z: 40})}
	sc := NewScene(ground)
	sc.FaceMat[0] = MatMetal
	cfg := Config{MaxOrder: 1, NumRays: 200000}

	sbr := SBREngine{}.Paths(tx, rx, sc, cfg)
	if len(sbr) < 2 {
		t.Fatalf("SBR should find direct + ground reflection, got %d paths", len(sbr))
	}
	pSBR, _ := Received(sbr)
	pImg, _ := Received(ImageEngine{}.Paths(tx, rx, sc, cfg))
	if math.Abs(DBm(pSBR)-DBm(pImg)) > 3.0 {
		t.Errorf("SBR two-ray %.2f dBm vs image %.2f dBm (>3 dB)", DBm(pSBR), DBm(pImg))
	}
}

// The COST-231 Walfisch-Ikegami model gives a plausible urban median loss that
// grows with distance and frequency, with positive diffraction terms.
func TestCost231WI(t *testing.T) {
	c := DefaultWICity()
	lb := Cost231WI(c, 1, 900)
	if lb < 120 || lb > 160 {
		t.Errorf("WI loss at 1 km, 900 MHz = %.1f dB, want ~130-150", lb)
	}
	if Cost231WI(c, 2, 900) <= lb {
		t.Error("WI loss should grow with distance")
	}
	if Cost231WI(c, 1, 1800) <= lb {
		t.Error("WI loss should grow with frequency")
	}
}
