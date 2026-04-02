package database

import (
	"database/sql"
	"fmt"
)

// RunMigrations creates the database schema.
// Uses IF NOT EXISTS so it can be run idempotently.
func RunMigrations(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS candles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol_id INTEGER NOT NULL,
			open REAL NOT NULL,
			high REAL NOT NULL,
			low REAL NOT NULL,
			close REAL NOT NULL,
			volume REAL NOT NULL,
			time INTEGER NOT NULL,
			interval TEXT NOT NULL,
			UNIQUE(symbol_id, time, interval)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_candles_symbol_time
			ON candles(symbol_id, time DESC)`,
		`CREATE TABLE IF NOT EXISTS tickers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol_id INTEGER NOT NULL,
			best_ask REAL NOT NULL,
			best_bid REAL NOT NULL,
			open REAL NOT NULL,
			high REAL NOT NULL,
			low REAL NOT NULL,
			last REAL NOT NULL,
			volume REAL NOT NULL,
			timestamp INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tickers_symbol_time
			ON tickers(symbol_id, timestamp DESC)`,
		`CREATE TABLE IF NOT EXISTS trades (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol_id INTEGER NOT NULL,
			order_side TEXT NOT NULL,
			price REAL NOT NULL,
			amount REAL NOT NULL,
			asset_amount REAL NOT NULL,
			traded_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trades_symbol_time
			ON trades(symbol_id, traded_at DESC)`,
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}
