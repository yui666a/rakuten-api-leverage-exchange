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
	// If before > 0, only candles with time < before are returned (for pagination).
	GetCandles(ctx context.Context, symbolID int64, interval string, limit int, before int64) ([]entity.Candle, error)

	// SaveTicker saves ticker data.
	SaveTicker(ctx context.Context, ticker entity.Ticker) error

	// GetLatestTicker returns the most recent ticker for a symbol.
	GetLatestTicker(ctx context.Context, symbolID int64) (*entity.Ticker, error)

	// GetTickersBetween returns ticker rows in [from, to] (unix-millis),
	// ascending by timestamp, capped at limit. Used by the execution-quality
	// reporter to look up the mid price near each my-trade row. limit <= 0
	// falls back to a server-defined default.
	GetTickersBetween(ctx context.Context, symbolID int64, from, to int64, limit int) ([]entity.Ticker, error)

	// SaveTrades batch-saves market trade ticks. Duplicates (by trade ID) are ignored.
	SaveTrades(ctx context.Context, symbolID int64, trades []entity.MarketTrade) error

	// SaveOrderbook persists a single orderbook snapshot. depthLimit caps how many
	// ask/bid levels are serialized into the JSON column (0 = all available levels).
	SaveOrderbook(ctx context.Context, ob entity.Orderbook, depthLimit int) error

	// GetOrderbookHistory returns orderbook snapshots for a symbol within [from, to],
	// oldest first, up to limit rows. from/to are unix milliseconds; 0 means unbounded.
	GetOrderbookHistory(ctx context.Context, symbolID int64, from, to int64, limit int) ([]entity.Orderbook, error)

	// PurgeOldMarketData removes rows older than cutoffMillis from the high-volume
	// market data tables (tickers, trades, orderbook_snapshots). Returns total
	// rows deleted across all three.
	PurgeOldMarketData(ctx context.Context, cutoffMillis int64) (int64, error)
}
