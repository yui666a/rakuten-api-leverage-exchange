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

		`CREATE TABLE IF NOT EXISTS backtest_results (
			id TEXT PRIMARY KEY,
			created_at INTEGER NOT NULL,
			symbol TEXT NOT NULL,
			symbol_id INTEGER NOT NULL,
			primary_interval TEXT NOT NULL,
			higher_tf_interval TEXT NOT NULL DEFAULT '',
			from_ts INTEGER NOT NULL,
			to_ts INTEGER NOT NULL,
			initial_balance REAL NOT NULL,
			final_balance REAL NOT NULL,
			total_return REAL NOT NULL,
			total_trades INTEGER NOT NULL,
			win_trades INTEGER NOT NULL,
			loss_trades INTEGER NOT NULL,
			win_rate REAL NOT NULL,
			profit_factor REAL NOT NULL,
			max_drawdown REAL NOT NULL,
			max_drawdown_balance REAL NOT NULL,
			sharpe_ratio REAL NOT NULL,
			avg_hold_seconds INTEGER NOT NULL,
			total_carrying_cost REAL NOT NULL,
			total_spread_cost REAL NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_results_created
			ON backtest_results(created_at DESC, id DESC)`,

		`CREATE TABLE IF NOT EXISTS backtest_trades (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			result_id TEXT NOT NULL,
			trade_id INTEGER NOT NULL,
			symbol_id INTEGER NOT NULL,
			entry_time INTEGER NOT NULL,
			exit_time INTEGER NOT NULL,
			side TEXT NOT NULL,
			entry_price REAL NOT NULL,
			exit_price REAL NOT NULL,
			amount REAL NOT NULL,
			pnl REAL NOT NULL,
			pnl_percent REAL NOT NULL,
			carrying_cost REAL NOT NULL,
			spread_cost REAL NOT NULL,
			reason_entry TEXT NOT NULL DEFAULT '',
			reason_exit TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(result_id) REFERENCES backtest_results(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_trades_result
			ON backtest_trades(result_id, trade_id)`,
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

	// PDCA メタデータを backtest_results に追加する。
	// `parent_result_id` はルートノード=NULL のため、`ALTER TABLE ADD COLUMN ... REFERENCES` を
	// 既存行に対して冪等に実行できる (nullable かつ default NULL)。
	// `ON DELETE SET NULL` で親削除時に子を再ルート化し、履歴を保持する。
	backtestPDCAColumns := []struct {
		name string
		def  string
	}{
		{"profile_name", "profile_name TEXT NOT NULL DEFAULT ''"},
		{"pdca_cycle_id", "pdca_cycle_id TEXT NOT NULL DEFAULT ''"},
		{"hypothesis", "hypothesis TEXT NOT NULL DEFAULT ''"},
		{"parent_result_id", "parent_result_id TEXT DEFAULT NULL REFERENCES backtest_results(id) ON DELETE SET NULL"},
		{"biweekly_win_rate", "biweekly_win_rate REAL NOT NULL DEFAULT 0"},
	}
	for _, col := range backtestPDCAColumns {
		if err := addColumnIfNotExists(db, "backtest_results", col.name, col.def); err != nil {
			return fmt.Errorf("backtest_results alter: %w", err)
		}
	}

	// PR-1: バックテスト結果の breakdown (exit 理由別 / シグナル別集計) を
	// 1 本の JSON カラムに格納する。クエリで breakdown の内部構造に対して
	// 直接フィルタする要件が無く、常に summary と一緒に取る読み取り専用
	// データなので、正規化テーブルではなく JSON TEXT 1 本で十分。
	// NULL = レガシー行 (PR-1 マージ前に作成された行)。
	if err := addColumnIfNotExists(db, "backtest_results", "breakdown_json",
		"breakdown_json TEXT DEFAULT NULL"); err != nil {
		return fmt.Errorf("backtest_results alter breakdown_json: %w", err)
	}

	// PDCA 関連カラムの検索を高速化する部分インデックス。NULL/空文字列を除外して
	// インデックスサイズを抑える (大半の既存行は空文字列/NULL)。
	pdcaIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_backtest_results_parent
			ON backtest_results(parent_result_id)
			WHERE parent_result_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_results_profile
			ON backtest_results(profile_name)
			WHERE profile_name != ''`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_results_pdca_cycle
			ON backtest_results(pdca_cycle_id)
			WHERE pdca_cycle_id != ''`,
	}
	for _, stmt := range pdcaIndexes {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create pdca index: %w", err)
		}
	}

	// PR-2: 複数期間一括バックテスト (multi-period run) のまとめを保存する
	// 専用テーブル。個別期間の BacktestResult は backtest_results テーブルに
	// 独立に保存され、ここにはその ID 配列 (period_result_ids) と集約スコア
	// (aggregate_json) だけを持つ envelope を格納する。
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS multi_period_results (
		id TEXT PRIMARY KEY,
		created_at INTEGER NOT NULL,
		profile_name TEXT NOT NULL DEFAULT '',
		pdca_cycle_id TEXT NOT NULL DEFAULT '',
		hypothesis TEXT NOT NULL DEFAULT '',
		parent_result_id TEXT DEFAULT NULL,
		aggregate_json TEXT NOT NULL,
		period_result_ids TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create multi_period_results: %w", err)
	}
	multiPeriodIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_multi_period_created
			ON multi_period_results(created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_multi_period_profile
			ON multi_period_results(profile_name)
			WHERE profile_name <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_multi_period_pdca
			ON multi_period_results(pdca_cycle_id)
			WHERE pdca_cycle_id <> ''`,
	}
	for _, stmt := range multiPeriodIndexes {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create multi_period index: %w", err)
		}
	}

	return nil
}
