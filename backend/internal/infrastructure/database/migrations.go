package database

import (
	"database/sql"
	"fmt"
)

// addColumnIfNotExists は SQLite の ALTER TABLE ADD COLUMN を冪等に実行する。
// SQLite は `ADD COLUMN IF NOT EXISTS` をサポートしないため、PRAGMA table_info で
// 既存カラムを確認してから ALTER を発行する。
// columnDef は "<name> <type> [DEFAULT <value>] ..." の形式 (ADD COLUMN の右辺)。
func addColumnIfNotExists(db *sql.DB, table, columnName, columnDef string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info(%s): %w", table, err)
		}
		if name == columnName {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table_info(%s): %w", table, err)
	}

	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table, columnDef)
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("alter table %s add column %s: %w", table, columnName, err)
	}
	return nil
}

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

		`CREATE TABLE IF NOT EXISTS trade_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol_id INTEGER NOT NULL,
			order_id INTEGER NOT NULL,
			side TEXT NOT NULL,
			action TEXT NOT NULL,
			price REAL NOT NULL,
			amount REAL NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			is_stop_loss INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_history_symbol_time
			ON trade_history(symbol_id, created_at DESC)`,

		`CREATE TABLE IF NOT EXISTS risk_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			daily_loss REAL NOT NULL DEFAULT 0,
			balance REAL NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,

		`CREATE TABLE IF NOT EXISTS stance_overrides (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			stance TEXT NOT NULL,
			reasoning TEXT NOT NULL DEFAULT '',
			set_at INTEGER NOT NULL,
			ttl_sec INTEGER NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS client_orders (
			client_order_id TEXT PRIMARY KEY,
			executed INTEGER NOT NULL,
			order_id INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_client_orders_created
			ON client_orders(created_at)`,
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	// client_orders をライフサイクル監査ログに格上げするための ALTER 群。
	// 既存行は status='completed' で保護される (executed=true 前提のレコードのみ存在していたため)。
	clientOrderColumns := []struct {
		name string
		def  string
	}{
		{"status", "status TEXT NOT NULL DEFAULT 'completed'"},
		{"symbol_id", "symbol_id INTEGER NOT NULL DEFAULT 0"},
		{"intent", "intent TEXT NOT NULL DEFAULT ''"},
		{"side", "side TEXT NOT NULL DEFAULT ''"},
		{"amount", "amount REAL NOT NULL DEFAULT 0"},
		{"position_id", "position_id INTEGER NOT NULL DEFAULT 0"},
		{"raw_response", "raw_response TEXT NOT NULL DEFAULT ''"},
		{"error_message", "error_message TEXT NOT NULL DEFAULT ''"},
		{"updated_at", "updated_at INTEGER NOT NULL DEFAULT 0"},
	}
	for _, col := range clientOrderColumns {
		if err := addColumnIfNotExists(db, "client_orders", col.name, col.def); err != nil {
			return fmt.Errorf("client_orders alter: %w", err)
		}
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_client_orders_status
		ON client_orders(status, updated_at)`); err != nil {
		return fmt.Errorf("create idx_client_orders_status: %w", err)
	}

	return nil
}
