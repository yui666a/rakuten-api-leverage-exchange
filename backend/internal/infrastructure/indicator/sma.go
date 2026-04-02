package indicator

import "math"

func SMA(prices []float64, period int) float64 {
	if len(prices) < period {
		return math.NaN()
	}
	sum := 0.0
	for _, p := range prices[len(prices)-period:] {
		sum += p
	}
	return sum / float64(period)
}

func SMASeries(prices []float64, period int) []float64 {
	if len(prices) < period {
		return nil
	}
	result := make([]float64, 0, len(prices)-period+1)
	for i := period - 1; i < len(prices); i++ {
		sum := 0.0
		for j := i - period + 1; j <= i; j++ {
			sum += prices[j]
		}
		result = append(result, sum/float64(period))
	}
	return result
}
