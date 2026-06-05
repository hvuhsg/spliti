package sim

import "math"

// Weather describes the atmosphere along the propagation paths: a rain rate
// (mm/h), an air temperature (K), and a relative humidity (%). It drives the
// band-gated specific attenuation — negligible at sub-6 GHz in clear air,
// significant at mmWave where the 60 GHz oxygen line and rain dominate.
type Weather struct {
	RainRateMMH float64
	TempK       float64
	HumidityPct float64
}

// SpecificAttenDBperKM returns the total atmospheric specific attenuation in
// dB/km at frequency fHz: gaseous absorption (an oxygen complex peaking near
// 60 GHz plus a humidity-dependent water-vapor term) plus rain (ITU-R P.838).
// The frequency dependence band-gates the effect automatically — it is a small
// fraction of a dB/km below 6 GHz and rises to ~15 dB/km at the 60 GHz oxygen
// peak.
func (w Weather) SpecificAttenDBperKM(fHz float64) float64 {
	fG := fHz / 1e9
	return oxygenAtten(fG) + waterVaporAtten(fG, w.HumidityPct) + rainAtten(fG, w.RainRateMMH)
}

// oxygenAtten approximates dry-air (oxygen) specific attenuation: a small
// low-frequency baseline plus a Gaussian centered on the 60 GHz oxygen
// absorption complex. The Gaussian tail vanishes quickly, so sub-6 GHz air is
// essentially transparent while the 60 GHz peak reaches ~15 dB/km.
func oxygenAtten(fG float64) float64 {
	const base = 0.0067 // ~sea-level dry-air floor, dB/km
	d := (fG - 60) / 5
	return base + 15*math.Exp(-0.5*d*d)
}

// waterVaporAtten approximates water-vapor specific attenuation: the 22.235 GHz
// line plus a continuum rising with frequency, scaled by humidity (≈7.5 g/m³ at
// 100% as a reference density).
func waterVaporAtten(fG, humidityPct float64) float64 {
	if humidityPct <= 0 {
		return 0
	}
	rho := humidityPct / 100 * 7.5
	d := (fG - 22.235) / 3
	line := 0.05 * rho / (1 + d*d)
	continuum := 1e-6 * rho * fG * fG
	return line + continuum
}

// rainAtten returns the ITU-R P.838 rain specific attenuation γ = k·R^α (dB/km)
// for rain rate R (mm/h), with the horizontal-polarization k and α interpolated
// across frequency.
func rainAtten(fG, rainMMH float64) float64 {
	if rainMMH <= 0 {
		return 0
	}
	k, a := p838(fG)
	return k * math.Pow(rainMMH, a)
}

// p838 returns the ITU-R P.838 horizontal-polarization coefficients k and α at
// frequency fG (GHz), log-log interpolating k and linearly interpolating α over a
// table of anchor frequencies.
func p838(fG float64) (k, a float64) {
	freqs := []float64{1, 2, 4, 6, 8, 10, 12, 15, 20, 25, 30, 35, 40}
	ks := []float64{0.0000387, 0.000154, 0.000650, 0.00175, 0.00454, 0.0101, 0.0188, 0.0367, 0.0751, 0.124, 0.187, 0.263, 0.350}
	as := []float64{0.912, 0.963, 1.121, 1.308, 1.327, 1.276, 1.217, 1.154, 1.099, 1.061, 1.021, 0.979, 0.939}

	if fG <= freqs[0] {
		return ks[0], as[0]
	}
	n := len(freqs)
	if fG >= freqs[n-1] {
		return ks[n-1], as[n-1]
	}
	i := 0
	for i < n-1 && freqs[i+1] < fG {
		i++
	}
	t := (math.Log(fG) - math.Log(freqs[i])) / (math.Log(freqs[i+1]) - math.Log(freqs[i]))
	logK := math.Log(ks[i]) + t*(math.Log(ks[i+1])-math.Log(ks[i]))
	a = as[i] + t*(as[i+1]-as[i])
	return math.Exp(logK), a
}

// atmosphericFieldFactor is the field-amplitude multiplier for a path of length
// lengthM (meters) at frequency fHz under the given weather. A power loss of A dB
// corresponds to a field factor 10^(−A/20).
func atmosphericFieldFactor(w Weather, fHz float64, lengthM float32) float64 {
	gammaPerKm := w.SpecificAttenDBperKM(fHz)
	if gammaPerKm <= 0 {
		return 1
	}
	lossDB := gammaPerKm * float64(lengthM) / 1000
	return math.Pow(10, -lossDB/20)
}
