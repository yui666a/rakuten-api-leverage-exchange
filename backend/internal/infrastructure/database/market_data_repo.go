package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type MarketDataRepo struct {
	db *sql.DB
}

func NewMarketDataRepo(db *sql.DB) *MarketDataRepo {
	return &MarketDataRepo{db: db}
}

func (r *MarketDataRepo) SaveCandle(ctx context.Context, symbolID int64, interval string, candle entity.Candle) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO candles (symbol_id, open, high, low, close, volume, time, interval)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		symbolID, candle.Open, candle.High, candle.Low, candle.Close, candle.Volume, candle.Time, interval,
	)
	if err != nil {
		return fmt.Errorf("save candle: %w", err)
	}
	return nil
}

func (r *MarketDataRepo) SaveCandles(ctx context.Context, symbolID int64, interval string, candles []entity.Candle) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO candles (symbol_id, open, high, low, close, volume, time, interval)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare stmt: %w", err)
	}
	defer stmt.Close()

	for _, c := range candles {
		if _, err := stmt.ExecContext(ctx, symbolID, c.Open, c.High, c.Low, c.Close, c.Volume, c.Time, interval); err != nil {
			return fmt.Errorf("exec stmt: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (r *MarketDataRepo) GetCandles(ctx context.Context, symbolID int64, interval string, limit int, before int64) ([]entity.Candle, error) {
	var rows *sql.Rows
	var err error
	if before > 0 {
		rows, err = r.db.QueryContext(ctx,
			`SELECT open, high, low, close, volume, time FROM candles
			 WHERE symbol_id = ? AND interval = ? AND time < ?
			 ORDER BY time DESC LIMIT ?`,
			symbolID, interval, before, limit,
		)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT open, high, low, close, volume, time FROM candles
			 WHERE symbol_id = ? AND interval = ?
			 ORDER BY time DESC LIMIT ?`,
			symbolID, interval, limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("query candles: %w", err)
	}
	defer rows.Close()

	var candles []entity.Candle
	for rows.Next() {
		var c entity.Candle
		if err := rows.Scan(&c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &c.Time); err != nil {
			return nil, fmt.Errorf("scan candle: %w", err)
		}
		candles = append(candles, c)
	}
	return candles, rows.Err()
}

func (r *MarketDataRepo) SaveTicker(ctx context.Context, ticker entity.Ticker) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tickers (symbol_id, best_ask, best_bid, open, high, low, last, volume, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ticker.SymbolID, ticker.BestAsk, ticker.BestBid, ticker.Open, ticker.High,
		ticker.Low, ticker.Last, ticker.Volume, ticker.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("save ticker: %w", err)
	}
	return nil
}

func (r *MarketDataRepo) GetLatestTicker(ctx context.Context, symbolID int64) (*entity.Ticker, error) {
	var t entity.Ticker
	err := r.db.QueryRowContext(ctx,
		`SELECT symbol_id, best_ask, best_bid, open, high, low, last, volume, timestamp
		 FROM tickers WHERE symbol_id = ? ORDER BY timestamp DESC LIMIT 1`,
		symbolID,
	).Scan(&t.SymbolID, &t.BestAsk, &t.BestBid, &t.Open, &t.High, &t.Low, &t.Last, &t.Volume, &t.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("get latest ticker: %w", err)
	}
	return &t, nil
}
