package repository

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// MarketDataRepository defines the persistence interface for market data.
type MarketDataRepository interface {
	// SaveCandle saves a single candlestick. Duplicates are ignored.
	SaveCandle(ctx context.Context, symbolID int64, interval string, candle entity.Candle) error

	// SaveCandles batch-saves multiple candlesticks.
	SaveCandles(ctx context.Context, symbolID int64, interval string, candles []entity.Candle) error

	// GetCandles returns candlesticks for a symbol/interval, newest first, up to limit.
	GetCandles(ctx context.Context, symbolID int64, interval string, limit int) ([]entity.Candle, error)

	// SaveTicker saves ticker data.
	SaveTicker(ctx context.Context, ticker entity.Ticker) error

	// GetLatestTicker returns the most recent ticker for a symbol.
	GetLatestTicker(ctx context.Context, symbolID int64) (*entity.Ticker, error)
}
