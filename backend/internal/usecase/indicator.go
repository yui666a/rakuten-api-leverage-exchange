package usecase

import (
	"context"

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
func (c *IndicatorCalculator) Calculate(ctx context.Context, symbolID int64, interval string) (*entity.IndicatorSet, error) {
	// EMA/RSI/MACDはパス依存型指標のため、十分なウォームアップ期間が必要。
	// EMA26は約3倍(78本)、MACD Signal(9)の追加で約90本のウォームアップ。
	// 500本取得すれば実用上十分な精度に収束する。
	candles, err := c.repo.GetCandles(ctx, symbolID, interval, 500)
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
		SMA20:     indicator.SMA(prices, 20),
		SMA50:     indicator.SMA(prices, 50),
		EMA12:     indicator.EMA(prices, 12),
		EMA26:     indicator.EMA(prices, 26),
		RSI14:     indicator.RSI(prices, 14),
		Timestamp: timestamp,
	}

	macdLine, signalLine, histogram := indicator.MACD(prices, 12, 26, 9)
	result.MACDLine = macdLine
	result.SignalLine = signalLine
	result.Histogram = histogram

	return result, nil
}
