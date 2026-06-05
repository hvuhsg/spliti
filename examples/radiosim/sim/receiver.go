package sim

import "math"

// Boltzmann is k in joules per kelvin, for the thermal noise floor kTB.
const Boltzmann = 1.380649e-23

// Modulation selects the bit-error-rate curve used to map an SNR to a BER.
type Modulation int

const (
	BPSK Modulation = iota
	QPSK
	QAM16
	QAM64
)

// NoiseFloorW returns the receiver thermal noise power in watts, N = k·T·B scaled
// by the noise figure: N = kTB·10^(NF/10). This is the one place ambient
// temperature enters the link budget. With T = 290 K, B = 1 Hz, NF = 0 it is the
// familiar −174 dBm/Hz.
func (rx Rx) NoiseFloorW() float64 {
	t := rx.TempK
	if t <= 0 {
		t = 290
	}
	nf := math.Pow(10, rx.NoiseFigDB/10)
	return Boltzmann * t * rx.BandwidthHz * nf
}

// SNRdB returns the signal-to-noise ratio in dB for a received signal power
// (watts) against the receiver noise floor.
func SNRdB(signalW float64, rx Rx) float64 {
	n := rx.NoiseFloorW()
	if n <= 0 || signalW <= 0 {
		return -math.Inf(1)
	}
	return 10 * math.Log10(signalW/n)
}

// BER returns the bit error rate for the modulation at a given SNR (interpreted as
// Eb/N0 in dB). BPSK and QPSK share Pb = Q(√(2·Eb/N0)); square-QAM uses the
// standard nearest-neighbor approximation.
func BER(snrdB float64, mod Modulation) float64 {
	ebn0 := math.Pow(10, snrdB/10)
	if ebn0 <= 0 {
		return 0.5
	}
	switch mod {
	case QAM16:
		return berSquareQAM(ebn0, 16)
	case QAM64:
		return berSquareQAM(ebn0, 64)
	default: // BPSK, QPSK
		return qfunc(math.Sqrt(2 * ebn0))
	}
}

// berSquareQAM is the nearest-neighbor BER approximation for square M-QAM.
func berSquareQAM(ebn0, mOrder float64) float64 {
	l := math.Log2(mOrder)
	return (4 / l) * (1 - 1/math.Sqrt(mOrder)) * qfunc(math.Sqrt(3*l/(mOrder-1)*ebn0))
}

// SensitivityDBm returns the minimum received power (dBm) that meets a target BER
// for the receiver's modulation and noise floor: it inverts BER→SNR (monotone)
// and scales the noise floor by the required SNR.
func (rx Rx) SensitivityDBm(targetBER float64) float64 {
	lo, hi := -10.0, 80.0 // SNR (dB) search bracket
	for i := 0; i < 100; i++ {
		mid := (lo + hi) / 2
		if BER(mid, rx.Mod) > targetBER {
			lo = mid // need more SNR
		} else {
			hi = mid
		}
	}
	snrLin := math.Pow(10, (lo+hi)/2/10)
	return DBm(snrLin * rx.NoiseFloorW())
}

// qfunc is the Gaussian Q-function Q(x) = ½·erfc(x/√2).
func qfunc(x float64) float64 { return 0.5 * math.Erfc(x/math.Sqrt2) }
