package indicator

import "math"

// VolumeSMA computes the simple moving average of volume over the last `period` candles.
// Returns NaN if len(volumes) < period.
func VolumeSMA(volumes []float64, period int) float64 {
	if len(volumes) < period {
		return math.NaN()
	}
	sum := 0.0
	for _, v := range volumes[len(volumes)-period:] {
		sum += v
	}
	return sum / float64(period)
}

// VolumeRatio computes currentVolume / sma.
// Returns NaN if sma is zero.
func VolumeRatio(currentVolume, sma float64) float64 {
	if sma == 0 {
		return math.NaN()
	}
	return currentVolume / sma
}
