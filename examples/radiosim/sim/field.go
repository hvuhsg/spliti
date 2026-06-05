package sim

import (
	"math"
	"math/cmplx"
	"sort"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// Tx is a transmitter: a position, a radiated power in watts, and a carrier
// frequency in hertz (which sets the wavelength and per-meter phase rotation).
type Tx struct {
	Pos    m.Vec3
	PowerW float64
	FreqHz float64
}

// Wavelength returns λ = c/f in meters.
func (t Tx) Wavelength() float64 {
	if t.FreqHz <= 0 {
		return 0
	}
	return SpeedOfLight / t.FreqHz
}

// Rx is a receiver: a position, an antenna, and the front-end parameters that set
// its noise floor and link quality — bandwidth, noise figure, system temperature,
// and the modulation used to map SNR to a bit error rate (see receiver.go).
type Rx struct {
	Pos         m.Vec3
	Ant         Antenna
	BandwidthHz float64
	NoiseFigDB  float64
	TempK       float64
	Mod         Modulation
}

// Path is one propagation path from the transmitter to a field point: its total
// length, the delay (length/c), the complex amplitude (magnitude = received field
// strength contribution, phase = carrier phase at arrival), and the polyline of
// world points it travels (Tx, interaction points…, field point) for drawing.
type Path struct {
	Length float32
	Delay  float32
	Amp    complex128
	Points []m.Vec3
	Order  int
}

// Tap is one resolvable multipath component of the channel impulse response: a
// delay (seconds) and a complex amplitude. Summing taps with their phases gives
// the narrowband channel gain.
type Tap struct {
	Delay float32
	Amp   complex128
}

// Received sums the complex path amplitudes into the narrowband channel gain
// H = Σ aᵢ and reports the received power |H|² in watts, plus the per-path taps
// sorted by delay (the channel impulse response). With unit-gain isotropic
// antennas this |H|² is the Friis received power including multipath
// interference.
func Received(paths []Path) (powerW float64, taps []Tap) {
	var h complex128
	taps = make([]Tap, 0, len(paths))
	for _, p := range paths {
		h += p.Amp
		taps = append(taps, Tap{Delay: p.Delay, Amp: p.Amp})
	}
	sort.Slice(taps, func(i, j int) bool { return taps[i].Delay < taps[j].Delay })
	mag := cmplx.Abs(h)
	return mag * mag, taps
}

// DBm converts a power in watts to dBm. Zero or negative power maps to a floor of
// -200 dBm so it is finite for display.
func DBm(powerW float64) float64 {
	if powerW <= 0 {
		return -200
	}
	return 10 * math.Log10(powerW/1e-3)
}

// DelaySpread returns the RMS delay spread of the taps (seconds), a single-number
// summary of how dispersive the channel is (drives inter-symbol interference).
func DelaySpread(taps []Tap) float64 {
	var p, mean, m2 float64
	for _, t := range taps {
		w := real(t.Amp)*real(t.Amp) + imag(t.Amp)*imag(t.Amp)
		p += w
		mean += w * float64(t.Delay)
	}
	if p <= 0 {
		return 0
	}
	mean /= p
	for _, t := range taps {
		w := real(t.Amp)*real(t.Amp) + imag(t.Amp)*imag(t.Amp)
		d := float64(t.Delay) - mean
		m2 += w * d * d
	}
	return math.Sqrt(m2 / p)
}
