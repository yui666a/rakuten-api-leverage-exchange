package indicator

import "math"

// BollingerBands computes upper, middle (SMA), lower bands and bandwidth.
// period is typically 20, multiplier is typically 2.0.
// Returns NaN values if insufficient data.
func BollingerBands(prices []float64, period int, multiplier float64) (upper, middle, lower, bandwidth float64) {
	if len(prices) < period {
		return math.NaN(), math.NaN(), math.NaN(), math.NaN()
	}

	middle = SMA(prices, period)

	// Standard deviation of the last `period` prices
	window := prices[len(prices)-period:]
	sumSqDiff := 0.0
	for _, p := range window {
		diff := p - middle
		sumSqDiff += diff * diff
	}
	stdDev := math.Sqrt(sumSqDiff / float64(period))

	upper = middle + multiplier*stdDev
	lower = middle - multiplier*stdDev

	if middle > 0 {
		bandwidth = (upper - lower) / middle
	}
	return upper, middle, lower, bandwidth
}
