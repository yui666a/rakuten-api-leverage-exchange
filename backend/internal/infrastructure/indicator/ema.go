package indicator

import "math"

func EMA(prices []float64, period int) float64 {
	series := EMASeries(prices, period)
	if len(series) == 0 {
		return math.NaN()
	}
	return series[len(series)-1]
}

func EMASeries(prices []float64, period int) []float64 {
	if len(prices) < period {
		return nil
	}
	multiplier := 2.0 / float64(period+1)
	result := make([]float64, 0, len(prices)-period+1)
	sma := SMA(prices[:period], period)
	result = append(result, sma)
	for i := period; i < len(prices); i++ {
		prev := result[len(result)-1]
		ema := (prices[i]-prev)*multiplier + prev
		result = append(result, ema)
	}
	return result
}
