package indicator

import "math"

func MACD(prices []float64, fastPeriod, slowPeriod, signalPeriod int) (macdLine, signalLine, histogram float64) {
	fastEMA := EMASeries(prices, fastPeriod)
	slowEMA := EMASeries(prices, slowPeriod)
	if len(fastEMA) == 0 || len(slowEMA) == 0 {
		return math.NaN(), math.NaN(), math.NaN()
	}
	offset := len(fastEMA) - len(slowEMA)
	if offset < 0 {
		return math.NaN(), math.NaN(), math.NaN()
	}
	macdSeries := make([]float64, len(slowEMA))
	for i := range slowEMA {
		macdSeries[i] = fastEMA[i+offset] - slowEMA[i]
	}
	signalSeries := EMASeries(macdSeries, signalPeriod)
	if len(signalSeries) == 0 {
		return math.NaN(), math.NaN(), math.NaN()
	}
	macdLine = macdSeries[len(macdSeries)-1]
	signalLine = signalSeries[len(signalSeries)-1]
	histogram = macdLine - signalLine
	return macdLine, signalLine, histogram
}
