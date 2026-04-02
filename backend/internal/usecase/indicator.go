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
	// Need 50 for SMA50, 35 for MACD signal. Fetch 100 for safety.
	candles, err := c.repo.GetCandles(ctx, symbolID, interval, 100)
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
