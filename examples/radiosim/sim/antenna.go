package sim

import (
	"math"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// AntKind selects an antenna radiation pattern.
type AntKind int

const (
	Isotropic      AntKind = iota // unit gain in every direction (0 dBi)
	HalfWaveDipole                // λ/2 dipole: doughnut pattern about its axis (2.15 dBi peak)
	Directional                   // a pencil beam about the boresight, set by HPBWdeg
)

// Antenna is a radiation pattern plus a polarization vector. Boresight is the
// dipole axis or the beam direction; HPBWdeg is the half-power beamwidth for a
// Directional antenna; Pol is the (unit) polarization direction used for the
// polarization-mismatch loss between two antennas (a zero Pol means "unspecified",
// i.e. perfectly matched).
type Antenna struct {
	Kind      AntKind
	Boresight m.Vec3
	HPBWdeg   float64
	Pol       m.Vec3
}

// Gain returns the linear power gain in direction dir (a direction from the
// antenna toward a path), relative to an isotropic radiator.
func (a Antenna) Gain(dir m.Vec3) float64 {
	d := dir.Normalize()
	switch a.Kind {
	case HalfWaveDipole:
		axis := a.Boresight
		if axis == (m.Vec3{}) {
			axis = m.Vec3{Z: 1}
		}
		cosT := clampf64(float64(d.Dot(axis.Normalize())), -1, 1)
		sinT := math.Sqrt(1 - cosT*cosT)
		if sinT < 1e-6 {
			return 1e-6 // deep null along the dipole axis
		}
		f := math.Cos(math.Pi/2*cosT) / sinT
		return 1.64 * f * f
	case Directional:
		bs := a.Boresight
		if bs == (m.Vec3{}) {
			bs = m.Vec3{X: 1}
		}
		hpbw := a.HPBWdeg * math.Pi / 180
		if hpbw <= 0 {
			hpbw = math.Pi / 6 // 30° default
		}
		cosA := clampf64(float64(d.Dot(bs.Normalize())), -1, 1)
		alpha := math.Acos(cosA)
		// Directivity approximated from the beamwidth; Gaussian lobe with the
		// half-power point at alpha = hpbw/2 (exp(-2.776·0.25) = 0.5).
		gmax := 16 / (hpbw * hpbw)
		r := alpha / hpbw
		return gmax * math.Exp(-2.776*r*r)
	default:
		return 1
	}
}

// PolMismatch returns the polarization-coupling loss |ê_tx·ê_rx|² between two
// antennas: 1 for aligned polarizations, 0 for orthogonal. A zero polarization on
// either side means unspecified and couples perfectly (1).
func PolMismatch(tx, rx Antenna) float64 {
	if tx.Pol == (m.Vec3{}) || rx.Pol == (m.Vec3{}) {
		return 1
	}
	d := float64(tx.Pol.Normalize().Dot(rx.Pol.Normalize()))
	return d * d
}

// ReceivedWith sums the path amplitudes like Received but weights each path by the
// transmit and receive antenna field gains in the path's launch/arrival
// directions and by the polarization mismatch, yielding the received power and
// taps for directive, polarized links. With isotropic, unspecified-polarization
// antennas it reduces exactly to Received.
func ReceivedWith(paths []Path, txAnt, rxAnt Antenna) (powerW float64, taps []Tap) {
	pf := PolMismatch(txAnt, rxAnt)
	weighted := make([]Path, len(paths))
	for i, p := range paths {
		g := math.Sqrt(txAnt.Gain(launchDir(p)) * rxAnt.Gain(arrivalDir(p).Neg()) * pf)
		wp := p
		wp.Amp = p.Amp * complex(g, 0)
		weighted[i] = wp
	}
	return Received(weighted)
}

// launchDir is the unit direction a path leaves the transmitter.
func launchDir(p Path) m.Vec3 {
	if len(p.Points) < 2 {
		return m.Vec3{X: 1}
	}
	return p.Points[1].Sub(p.Points[0]).Normalize()
}

// arrivalDir is the unit direction of travel as a path reaches the receiver.
func arrivalDir(p Path) m.Vec3 {
	n := len(p.Points)
	if n < 2 {
		return m.Vec3{X: 1}
	}
	return p.Points[n-1].Sub(p.Points[n-2]).Normalize()
}
