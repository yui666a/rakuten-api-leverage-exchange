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
			trade_id INTEGER NOT NULL DEFAULT 0,
			order_side TEXT NOT NULL,
			price REAL NOT NULL,
			amount REAL NOT NULL,
			asset_amount REAL NOT NULL,
			traded_at INTEGER NOT NULL,
			UNIQUE(symbol_id, trade_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trades_symbol_time
			ON trades(symbol_id, traded_at DESC)`,

		// orderbook_snapshots: L2 板スナップショットの永続化先。
		// depth_json には ask/bid を {p,a} ペア配列の JSON で保存し、SQLite の
		// json_extract で必要に応じて読み出せる。テーブルを正規化すると
		// 1 スナップショットあたり 20+ 行になり、リプレイ時のスキャンが重くなるので
		// 1 行 1 スナップショットを採用する。
		`CREATE TABLE IF NOT EXISTS orderbook_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol_id INTEGER NOT NULL,
			best_ask REAL NOT NULL,
			best_bid REAL NOT NULL,
			mid_price REAL NOT NULL,
			spread REAL NOT NULL,
			depth_json TEXT NOT NULL,
			timestamp INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_orderbook_snapshots_symbol_time
			ON orderbook_snapshots(symbol_id, timestamp DESC)`,

		// execution_quality_snapshots: PR-Q3. /execution-quality endpoint の
		// レイテンシと楽天 my-trades 呼び出し回数を抑えるため、worker が
		// 60 秒間隔で計算結果をここに INSERT し、endpoint は最新行を返す。
		// by_order_behavior_json は map[string]ExecutionQualityBehaviorBucket
		// の JSON。avg_slippage_bps は NULL (=mid 不明) 区別のため REAL NULL。
		`CREATE TABLE IF NOT EXISTS execution_quality_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol_id INTEGER NOT NULL,
			captured_at INTEGER NOT NULL,
			window_sec INTEGER NOT NULL,
			from_ts INTEGER NOT NULL,
			to_ts INTEGER NOT NULL,
			trades_count INTEGER NOT NULL,
			maker_count INTEGER NOT NULL,
			taker_count INTEGER NOT NULL,
			unknown_count INTEGER NOT NULL,
			maker_ratio REAL NOT NULL,
			total_fee_jpy REAL NOT NULL,
			avg_slippage_bps REAL,
			by_order_behavior_json TEXT NOT NULL DEFAULT '{}',
			halted INTEGER NOT NULL DEFAULT 0,
			halt_reason TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_execution_quality_snapshots_symbol_time
			ON execution_quality_snapshots(symbol_id, captured_at DESC)`,

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

		// trading_config persists the user's last-selected symbol and trade
		// amount across restarts. Without this, every container rebuild
		// silently resets the pipeline to the config-default symbol while
		// the frontend keeps showing whatever the user picked before, and
		// the WS subscription never re-attaches to the right asset.
		`CREATE TABLE IF NOT EXISTS trading_config (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			symbol_id INTEGER NOT NULL,
			trade_amount REAL NOT NULL,
			updated_at INTEGER NOT NULL DEFAULT 0
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

	// trades は元々 (symbol_id, traded_at) 主体で設計されていたが、
	// WS から流れてくる MarketTrade.id を保存して重複 INSERT を防ぐため
	// trade_id カラムと一意インデックスを後付けで足す。
	// 既存行 (永続化フックが繋がる前のレコードはそもそも空) は trade_id=0
	// のまま残るため、UNIQUE INDEX は trade_id != 0 の部分インデックスにする。
	if err := addColumnIfNotExists(db, "trades", "trade_id",
		"trade_id INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("trades alter trade_id: %w", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_trades_symbol_trade_id
		ON trades(symbol_id, trade_id) WHERE trade_id <> 0`); err != nil {
		return fmt.Errorf("create idx_trades_symbol_trade_id: %w", err)
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

	// PR-3: drawdown 履歴 + time-in-market + expectancy。既存パターンに合わせ、
	// スカラー値は個別カラム、DrawdownPeriods 配列は 1 本の JSON に集約。
	// 未回復 DD も同じ JSON に入れる (null フィールドで区別可能)。
	// すべて DEFAULT 0 / NULL でレガシー行と互換。
	pr3Columns := []struct {
		name string
		def  string
	}{
		{"drawdown_periods_json", "drawdown_periods_json TEXT DEFAULT NULL"},
		{"drawdown_threshold", "drawdown_threshold REAL NOT NULL DEFAULT 0"},
		{"time_in_market_ratio", "time_in_market_ratio REAL NOT NULL DEFAULT 0"},
		{"longest_flat_streak_bars", "longest_flat_streak_bars INTEGER NOT NULL DEFAULT 0"},
		{"expectancy_per_trade", "expectancy_per_trade REAL NOT NULL DEFAULT 0"},
		{"avg_win_jpy", "avg_win_jpy REAL NOT NULL DEFAULT 0"},
		{"avg_loss_jpy", "avg_loss_jpy REAL NOT NULL DEFAULT 0"},
	}
	for _, col := range pr3Columns {
		if err := addColumnIfNotExists(db, "backtest_results", col.name, col.def); err != nil {
			return fmt.Errorf("backtest_results alter %s: %w", col.name, err)
		}
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

	// PR-13 follow-up (#120): walk-forward envelope. Mirrors the
	// multi_period_results layout — per-window BacktestResult rows are saved
	// independently into backtest_results; this table only stores the
	// envelope (request, aggregate, per-window metadata + ID references).
	//   - request_json:       the original POST body so a window can be
	//                         re-run deterministically from the saved row.
	//   - result_json:        full WalkForwardResult minus the embedded
	//                         BacktestResult bodies (those live in
	//                         backtest_results and are rehydrated on read).
	//   - aggregate_oos_json: denormalised copy of result_json.aggregateOOS
	//                         so list views can rank by RobustnessScore
	//                         without parsing the full result_json.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS walk_forward_results (
		id                 TEXT PRIMARY KEY,
		created_at         INTEGER NOT NULL,
		base_profile       TEXT NOT NULL,
		pdca_cycle_id      TEXT NOT NULL DEFAULT '',
		hypothesis         TEXT NOT NULL DEFAULT '',
		objective          TEXT NOT NULL DEFAULT 'return',
		parent_result_id   TEXT DEFAULT NULL,
		request_json       TEXT NOT NULL,
		result_json        TEXT NOT NULL,
		aggregate_oos_json TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create walk_forward_results: %w", err)
	}
	walkForwardIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_wf_created
			ON walk_forward_results(created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_wf_profile
			ON walk_forward_results(base_profile)
			WHERE base_profile <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_wf_pdca
			ON walk_forward_results(pdca_cycle_id)
			WHERE pdca_cycle_id <> ''`,
	}
	for _, stmt := range walkForwardIndexes {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create walk_forward index: %w", err)
		}
	}

	// Decision log tables. One row per pipeline decision (BAR_CLOSE) plus
	// extra rows for tick-driven SL/TP/trailing closes. The indicators
	// snapshot is stored as TEXT so the recorder can write the marshalled
	// IndicatorSet verbatim without coupling the schema to the indicator
	// struct shape.
	decisionLogTables := []string{
		`CREATE TABLE IF NOT EXISTS decision_log (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			bar_close_at       INTEGER NOT NULL,
			sequence_in_bar    INTEGER NOT NULL DEFAULT 0,
			trigger_kind       TEXT    NOT NULL,
			symbol_id          INTEGER NOT NULL,
			currency_pair      TEXT    NOT NULL,
			primary_interval   TEXT    NOT NULL,
			stance             TEXT    NOT NULL,
			last_price         REAL    NOT NULL,
			signal_action      TEXT    NOT NULL,
			signal_confidence  REAL    NOT NULL DEFAULT 0,
			signal_reason      TEXT    NOT NULL DEFAULT '',
			risk_outcome       TEXT    NOT NULL,
			risk_reason        TEXT    NOT NULL DEFAULT '',
			book_gate_outcome  TEXT    NOT NULL DEFAULT 'SKIPPED',
			book_gate_reason   TEXT    NOT NULL DEFAULT '',
			order_outcome      TEXT    NOT NULL,
			order_id           INTEGER NOT NULL DEFAULT 0,
			executed_amount    REAL    NOT NULL DEFAULT 0,
			executed_price     REAL    NOT NULL DEFAULT 0,
			order_error        TEXT    NOT NULL DEFAULT '',
			closed_position_id INTEGER NOT NULL DEFAULT 0,
			opened_position_id INTEGER NOT NULL DEFAULT 0,
			indicators_json           TEXT NOT NULL DEFAULT '{}',
			higher_tf_indicators_json TEXT NOT NULL DEFAULT '{}',
			created_at         INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_decision_log_symbol_time
			ON decision_log(symbol_id, bar_close_at DESC, sequence_in_bar)`,
		`CREATE INDEX IF NOT EXISTS idx_decision_log_created
			ON decision_log(created_at)`,

		`CREATE TABLE IF NOT EXISTS backtest_decision_log (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			backtest_run_id    TEXT    NOT NULL,
			bar_close_at       INTEGER NOT NULL,
			sequence_in_bar    INTEGER NOT NULL DEFAULT 0,
			trigger_kind       TEXT    NOT NULL,
			symbol_id          INTEGER NOT NULL,
			currency_pair      TEXT    NOT NULL,
			primary_interval   TEXT    NOT NULL,
			stance             TEXT    NOT NULL,
			last_price         REAL    NOT NULL,
			signal_action      TEXT    NOT NULL,
			signal_confidence  REAL    NOT NULL DEFAULT 0,
			signal_reason      TEXT    NOT NULL DEFAULT '',
			risk_outcome       TEXT    NOT NULL,
			risk_reason        TEXT    NOT NULL DEFAULT '',
			book_gate_outcome  TEXT    NOT NULL DEFAULT 'SKIPPED',
			book_gate_reason   TEXT    NOT NULL DEFAULT '',
			order_outcome      TEXT    NOT NULL,
			order_id           INTEGER NOT NULL DEFAULT 0,
			executed_amount    REAL    NOT NULL DEFAULT 0,
			executed_price     REAL    NOT NULL DEFAULT 0,
			order_error        TEXT    NOT NULL DEFAULT '',
			closed_position_id INTEGER NOT NULL DEFAULT 0,
			opened_position_id INTEGER NOT NULL DEFAULT 0,
			indicators_json           TEXT NOT NULL DEFAULT '{}',
			higher_tf_indicators_json TEXT NOT NULL DEFAULT '{}',
			created_at         INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_decision_log_run
			ON backtest_decision_log(backtest_run_id, bar_close_at, sequence_in_bar)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_decision_log_created
			ON backtest_decision_log(created_at)`,
	}
	for _, stmt := range decisionLogTables {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create decision_log: %w", err)
		}
	}

	if err := addDecisionLogV2Columns(db); err != nil {
		return err
	}

	if err := createExitPlansTable(db); err != nil {
		return err
	}

	return nil
}

// createExitPlansTable は ExitPlan を永続化する exit_plans テーブルを作る。
// position_id は UNIQUE で建玉と 1:1 を保証する。trailing_hwm と closed_at は
// NULL 許容（HWM 未活性 / open）。
func createExitPlansTable(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS exit_plans (
			id                      INTEGER PRIMARY KEY AUTOINCREMENT,
			position_id             INTEGER NOT NULL UNIQUE,
			symbol_id               INTEGER NOT NULL,
			side                    TEXT NOT NULL,
			entry_price             REAL NOT NULL,
			sl_percent              REAL NOT NULL,
			sl_atr_multiplier       REAL NOT NULL DEFAULT 0,
			tp_percent              REAL NOT NULL,
			trailing_mode           INTEGER NOT NULL DEFAULT 0,
			trailing_atr_multiplier REAL NOT NULL DEFAULT 0,
			trailing_activated      INTEGER NOT NULL DEFAULT 0,
			trailing_hwm            REAL,
			created_at              INTEGER NOT NULL,
			updated_at              INTEGER NOT NULL,
			closed_at               INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_exit_plans_symbol_open
			ON exit_plans(symbol_id, closed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_exit_plans_position
			ON exit_plans(position_id)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create exit_plans: %w", err)
		}
	}
	return nil
}

// addDecisionLogV2Columns は Phase 1 (Signal/Decision/ExecutionPolicy 三層分離)
// で追加された 6 カラムを decision_log / backtest_decision_log に足す。
// addColumnIfNotExists を介して冪等。既存行は DEFAULT (空文字 / 0) で残る。
func addDecisionLogV2Columns(db *sql.DB) error {
	cols := []struct {
		name string
		def  string
	}{
		{"signal_direction", "signal_direction TEXT NOT NULL DEFAULT ''"},
		{"signal_strength", "signal_strength REAL NOT NULL DEFAULT 0"},
		{"decision_intent", "decision_intent TEXT NOT NULL DEFAULT ''"},
		{"decision_side", "decision_side TEXT NOT NULL DEFAULT ''"},
		{"decision_reason", "decision_reason TEXT NOT NULL DEFAULT ''"},
		{"exit_policy_outcome", "exit_policy_outcome TEXT NOT NULL DEFAULT ''"},
	}
	for _, table := range []string{"decision_log", "backtest_decision_log"} {
		for _, c := range cols {
			if err := addColumnIfNotExists(db, table, c.name, c.def); err != nil {
				return fmt.Errorf("add %s.%s: %w", table, c.name, err)
			}
		}
	}
	return nil
}
