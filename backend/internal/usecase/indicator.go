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
	prices := make([]float64, len(candles))
	for i, cd := range candles {
		prices[len(candles)-1-i] = cd.Close
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

	return result, nil
}

// toPtr converts a float64 to *float64. Returns nil if the value is NaN.
func toPtr(v float64) *float64 {
	if math.IsNaN(v) {
		return nil
	}
	return &v
}
