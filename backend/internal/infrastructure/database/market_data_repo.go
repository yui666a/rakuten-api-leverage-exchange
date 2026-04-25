package database

import (
	"context"
	"database/sql"
	"encoding/json"
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

// SaveTrades batch-inserts market trade ticks. Duplicate trade_ids per symbol are
// silently ignored via INSERT OR IGNORE — the WS feed retransmits the most recent
// trades on every "trades" frame, so dedup at write-time keeps the table clean.
func (r *MarketDataRepo) SaveTrades(ctx context.Context, symbolID int64, trades []entity.MarketTrade) error {
	if len(trades) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO trades (symbol_id, trade_id, order_side, price, amount, asset_amount, traded_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare stmt: %w", err)
	}
	defer stmt.Close()

	for _, t := range trades {
		if _, err := stmt.ExecContext(ctx,
			symbolID, t.ID, t.OrderSide, t.Price, t.Amount, t.AssetAmount, t.TradedAt,
		); err != nil {
			return fmt.Errorf("exec stmt: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// orderbookDepthEntry is the on-disk shape inside depth_json. Short keys keep
// the JSON small (1 row × thousands per day per symbol); the struct is local
// to this file because no other layer reads the raw JSON directly.
type orderbookDepthEntry struct {
	P float64 `json:"p"`
	A float64 `json:"a"`
}

type orderbookDepth struct {
	Asks []orderbookDepthEntry `json:"asks"`
	Bids []orderbookDepthEntry `json:"bids"`
}

func truncateLevels(levels []entity.OrderbookEntry, depthLimit int) []orderbookDepthEntry {
	n := len(levels)
	if depthLimit > 0 && n > depthLimit {
		n = depthLimit
	}
	out := make([]orderbookDepthEntry, n)
	for i := 0; i < n; i++ {
		out[i] = orderbookDepthEntry{P: levels[i].Price, A: levels[i].Amount}
	}
	return out
}

// SaveOrderbook persists one snapshot. depthLimit caps how many ask/bid levels
// are serialized — the venue ships 100+ levels per side and we typically only
// need the top ~20 for SOR / impact estimation.
func (r *MarketDataRepo) SaveOrderbook(ctx context.Context, ob entity.Orderbook, depthLimit int) error {
	depth := orderbookDepth{
		Asks: truncateLevels(ob.Asks, depthLimit),
		Bids: truncateLevels(ob.Bids, depthLimit),
	}
	depthJSON, err := json.Marshal(depth)
	if err != nil {
		return fmt.Errorf("marshal depth: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO orderbook_snapshots (symbol_id, best_ask, best_bid, mid_price, spread, depth_json, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ob.SymbolID, ob.BestAsk, ob.BestBid, ob.MidPrice, ob.Spread, string(depthJSON), ob.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("save orderbook: %w", err)
	}
	return nil
}

func (r *MarketDataRepo) GetOrderbookHistory(ctx context.Context, symbolID int64, from, to int64, limit int) ([]entity.Orderbook, error) {
	if limit <= 0 {
		limit = 1000
	}
	args := []any{symbolID}
	q := `SELECT symbol_id, best_ask, best_bid, mid_price, spread, depth_json, timestamp
	      FROM orderbook_snapshots WHERE symbol_id = ?`
	if from > 0 {
		q += ` AND timestamp >= ?`
		args = append(args, from)
	}
	if to > 0 {
		q += ` AND timestamp <= ?`
		args = append(args, to)
	}
	q += ` ORDER BY timestamp ASC LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query orderbook history: %w", err)
	}
	defer rows.Close()

	var out []entity.Orderbook
	for rows.Next() {
		var (
			ob       entity.Orderbook
			depthRaw string
		)
		if err := rows.Scan(&ob.SymbolID, &ob.BestAsk, &ob.BestBid, &ob.MidPrice, &ob.Spread, &depthRaw, &ob.Timestamp); err != nil {
			return nil, fmt.Errorf("scan orderbook: %w", err)
		}
		var depth orderbookDepth
		if err := json.Unmarshal([]byte(depthRaw), &depth); err != nil {
			return nil, fmt.Errorf("unmarshal depth: %w", err)
		}
		ob.Asks = make([]entity.OrderbookEntry, len(depth.Asks))
		for i, a := range depth.Asks {
			ob.Asks[i] = entity.OrderbookEntry{Price: a.P, Amount: a.A}
		}
		ob.Bids = make([]entity.OrderbookEntry, len(depth.Bids))
		for i, b := range depth.Bids {
			ob.Bids[i] = entity.OrderbookEntry{Price: b.P, Amount: b.A}
		}
		out = append(out, ob)
	}
	return out, rows.Err()
}

// PurgeOldMarketData deletes high-volume rows older than cutoffMillis from
// tickers / trades / orderbook_snapshots. Returns total rows deleted.
// The three deletes run in independent statements (not a single tx) so that
// a long retention sweep never blocks the writer goroutine for the whole batch.
func (r *MarketDataRepo) PurgeOldMarketData(ctx context.Context, cutoffMillis int64) (int64, error) {
	var total int64

	res, err := r.db.ExecContext(ctx,
		`DELETE FROM tickers WHERE timestamp < ?`, cutoffMillis)
	if err != nil {
		return total, fmt.Errorf("purge tickers: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		total += n
	}

	res, err = r.db.ExecContext(ctx,
		`DELETE FROM trades WHERE traded_at < ?`, cutoffMillis)
	if err != nil {
		return total, fmt.Errorf("purge trades: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		total += n
	}

	res, err = r.db.ExecContext(ctx,
		`DELETE FROM orderbook_snapshots WHERE timestamp < ?`, cutoffMillis)
	if err != nil {
		return total, fmt.Errorf("purge orderbook_snapshots: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		total += n
	}

	return total, nil
}
