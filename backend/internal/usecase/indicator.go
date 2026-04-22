package usecase

import (
	"context"
	"math"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/indicator"
)

// IndicatorCalculator computes technical indicators from candlestick data.
type IndicatorCalculator struct {
	repo repository.MarketDataRepository
}

func NewIndicatorCalculator(repo repository.MarketDataRepository) *IndicatorCalculator {
	return &IndicatorCalculator{repo: repo}
}

// Calculate computes all technical indicators for the given symbol and interval.
// Retrieves candles from the repository and calculates SMA, EMA, RSI, MACD.
// Indicators that cannot be calculated due to insufficient data are nil.
func (c *IndicatorCalculator) Calculate(ctx context.Context, symbolID int64, interval string) (*entity.IndicatorSet, error) {
	// EMA/RSI/MACDはパス依存型指標のため、十分なウォームアップ期間が必要。
	// EMA26は約3倍(78本)、MACD Signal(9)の追加で約90本のウォームアップ。
	// 500本取得すれば実用上十分な精度に収束する。
	candles, err := c.repo.GetCandles(ctx, symbolID, interval, 500, 0)
	if err != nil {
		return nil, err
	}

	// GetCandles returns newest-first, reverse to oldest-first for calculations
	n := len(candles)
	prices := make([]float64, n)
	highs := make([]float64, n)
	lows := make([]float64, n)
	for i, cd := range candles {
		prices[n-1-i] = cd.Close
		highs[n-1-i] = cd.High
		lows[n-1-i] = cd.Low
	}

	var timestamp int64
	if len(candles) > 0 {
		timestamp = candles[0].Time // newest candle's timestamp
	}

	result := &entity.IndicatorSet{
		SymbolID:  symbolID,
		SMA20:     toPtr(indicator.SMA(prices, 20)),
		SMA50:     toPtr(indicator.SMA(prices, 50)),
		EMA12:     toPtr(indicator.EMA(prices, 12)),
		EMA26:     toPtr(indicator.EMA(prices, 26)),
		RSI14:     toPtr(indicator.RSI(prices, 14)),
		Timestamp: timestamp,
	}

	macdLine, signalLine, histogram := indicator.MACD(prices, 12, 26, 9)
	result.MACDLine = toPtr(macdLine)
	result.SignalLine = toPtr(signalLine)
	result.Histogram = toPtr(histogram)

	bbUpper, bbMiddle, bbLower, bbBandwidth := indicator.BollingerBands(prices, 20, 2.0)
	result.BBUpper = toPtr(bbUpper)
	result.BBMiddle = toPtr(bbMiddle)
	result.BBLower = toPtr(bbLower)
	result.BBBandwidth = toPtr(bbBandwidth)

	result.ATR14 = toPtr(indicator.ATR(highs, lows, prices, 14))

	// PR-6: ADX family. ADX/PlusDI/MinusDI return NaN until 2*period+1
	// bars are available; toPtr collapses that to nil for the caller.
	adxVal, plusDI, minusDI := indicator.ADX(highs, lows, prices, 14)
	result.ADX14 = toPtr(adxVal)
	result.PlusDI14 = toPtr(plusDI)
	result.MinusDI14 = toPtr(minusDI)

	// PR-7: Stochastics (14, 3, 3) + Stochastic RSI (14, 14). Both return
	// NaN -> nil pointer when the warmup window is not filled yet.
	stochK, stochD := indicator.Stochastics(highs, lows, prices, 14, 3, 3)
	result.StochK14_3 = toPtr(stochK)
	result.StochD14_3 = toPtr(stochD)
	result.StochRSI14 = toPtr(indicator.StochasticRSI(prices, 14, 14))

	// PR-8: Ichimoku. Each of the five lines may be NaN independently during
	// warmup; buildIchimokuSnapshot returns nil when every line is unknown.
	if snap := buildIchimokuSnapshot(indicator.Ichimoku(highs, lows, prices, 9, 26, 52)); snap != nil {
		result.Ichimoku = snap
	}

	// PR-11: Donchian Channel (20-bar default). Mirror the other range-of-N
	// indicators — NaN until 20 bars of history are available; toPtr
	// collapses that into nil pointers for downstream gates.
	donU, donL, donM := indicator.Donchian(highs, lows, 20)
	result.Donchian20Upper = toPtr(donU)
	result.Donchian20Lower = toPtr(donL)
	result.Donchian20Middle = toPtr(donM)

	// Volume indicators
	volumes := make([]float64, n)
	for i, cd := range candles {
		volumes[n-1-i] = cd.Volume
	}
	volSMA := indicator.VolumeSMA(volumes, 20)
	result.VolumeSMA20 = toPtr(volSMA)
	if !math.IsNaN(volSMA) && volSMA > 0 && n > 0 {
		vr := indicator.VolumeRatio(volumes[n-1], volSMA)
		result.VolumeRatio = toPtr(vr)
	}

	// PR-9: OBV + CMF (volume-based). OBVSlope20 carries the gate signal
	// (cumulative buying volume over 20 bars); raw OBV is exposed for
	// diagnostics / frontend charting. CMF20 is bounded in [-1, 1].
	result.OBV = toPtr(indicator.OBV(prices, volumes))
	result.OBVSlope20 = toPtr(indicator.OBVSlope(prices, volumes, 20))
	result.CMF20 = toPtr(indicator.CMF(highs, lows, prices, volumes, 20))

	// RecentSqueeze: check if any of the last 5 candles had BBBandwidth < 0.02
	if n >= 20 {
		recentSqueeze := false
		lookback := 5
		if lookback > n-19 {
			lookback = n - 19
		}
		for i := 0; i < lookback; i++ {
			offset := n - 1 - i
			windowPrices := prices[:offset+1]
			_, _, _, bw := indicator.BollingerBands(windowPrices, 20, 2.0)
			if !math.IsNaN(bw) && bw < 0.02 {
				recentSqueeze = true
				break
			}
		}
		result.RecentSqueeze = &recentSqueeze
	}

	return result, nil
}

// toPtr converts a float64 to *float64. Returns nil if the value is NaN.
func toPtr(v float64) *float64 {
	if math.IsNaN(v) {
		return nil
	}
	return &v
}

// buildIchimokuSnapshot maps an indicator.IchimokuResult onto the entity
// pointer snapshot. Returns nil when every line is NaN (pure warmup state)
// so consumers can cheaply branch on `if snap := ...; snap != nil`.
func buildIchimokuSnapshot(r indicator.IchimokuResult) *entity.IchimokuSnapshot {
	snap := &entity.IchimokuSnapshot{
		Tenkan:  toPtr(r.Tenkan),
		Kijun:   toPtr(r.Kijun),
		SenkouA: toPtr(r.SenkouA),
		SenkouB: toPtr(r.SenkouB),
		Chikou:  toPtr(r.Chikou),
	}
	if snap.Tenkan == nil && snap.Kijun == nil && snap.SenkouA == nil && snap.SenkouB == nil && snap.Chikou == nil {
		return nil
	}
	return snap
}
