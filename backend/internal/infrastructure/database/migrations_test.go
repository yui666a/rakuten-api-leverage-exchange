package database

import (
	"path/filepath"
	"testing"
)

func TestRunMigrations(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	var count int
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='candles'").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Fatal("candles table should exist")
	}

	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='tickers'").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Fatal("tickers table should exist")
	}

	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='trades'").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Fatal("trades table should exist")
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("first migration failed: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
}

func TestRunMigrations_PDCABacktestResultsColumnsAndIndexes(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("first migration failed: %v", err)
	}
	// 2 度実行しても壊れないことを確認 (冪等性)。
	if err := RunMigrations(db); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}

	// 新カラムの存在を PRAGMA table_info で確認。
	wantColumns := map[string]bool{
		"profile_name":             false,
		"pdca_cycle_id":            false,
		"hypothesis":               false,
		"parent_result_id":         false,
		"biweekly_win_rate":        false,
		"breakdown_json":           false, // PR-1
		"drawdown_periods_json":    false, // PR-3
		"drawdown_threshold":       false, // PR-3
		"time_in_market_ratio":     false, // PR-3
		"longest_flat_streak_bars": false, // PR-3
		"expectancy_per_trade":     false, // PR-3
		"avg_win_jpy":              false, // PR-3
		"avg_loss_jpy":             false, // PR-3
	}
	rows, err := db.Query("PRAGMA table_info(backtest_results)")
	if err != nil {
		t.Fatalf("pragma table_info: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    interface{}
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if _, ok := wantColumns[name]; ok {
			wantColumns[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info: %v", err)
	}
	for col, seen := range wantColumns {
		if !seen {
			t.Errorf("expected column %q in backtest_results", col)
		}
	}

	// インデックスの存在確認。
	wantIndexes := map[string]bool{
		"idx_backtest_results_parent":     false,
		"idx_backtest_results_profile":    false,
		"idx_backtest_results_pdca_cycle": false,
	}
	idxRows, err := db.Query(
		"SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='backtest_results'",
	)
	if err != nil {
		t.Fatalf("query indexes: %v", err)
	}
	defer idxRows.Close()
	for idxRows.Next() {
		var name string
		if err := idxRows.Scan(&name); err != nil {
			t.Fatalf("scan index name: %v", err)
		}
		if _, ok := wantIndexes[name]; ok {
			wantIndexes[name] = true
		}
	}
	if err := idxRows.Err(); err != nil {
		t.Fatalf("iterate indexes: %v", err)
	}
	for idx, seen := range wantIndexes {
		if !seen {
			t.Errorf("expected index %q to exist", idx)
		}
	}

	// PR-2: multi_period_results テーブルとそのカラム・インデックスが揃って
	// いることを確認。PR-1 の breakdown_json 列と同様、将来の migration
	// リファクタで退行しないためのガードレール。
	wantMultiCols := map[string]bool{
		"id":                false,
		"created_at":        false,
		"profile_name":      false,
		"pdca_cycle_id":     false,
		"hypothesis":        false,
		"parent_result_id":  false,
		"aggregate_json":    false,
		"period_result_ids": false,
	}
	mpRows, err := db.Query("PRAGMA table_info(multi_period_results)")
	if err != nil {
		t.Fatalf("pragma table_info(multi_period_results): %v", err)
	}
	defer mpRows.Close()
	for mpRows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    interface{}
			pk      int
		)
		if err := mpRows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan multi_period_results table_info: %v", err)
		}
		if _, ok := wantMultiCols[name]; ok {
			wantMultiCols[name] = true
		}
	}
	if err := mpRows.Err(); err != nil {
		t.Fatalf("iterate multi_period_results table_info: %v", err)
	}
	for col, seen := range wantMultiCols {
		if !seen {
			t.Errorf("expected column %q in multi_period_results", col)
		}
	}

	wantMultiIndexes := map[string]bool{
		"idx_multi_period_created": false,
		"idx_multi_period_profile": false,
		"idx_multi_period_pdca":    false,
	}
	mpIdxRows, err := db.Query("SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='multi_period_results'")
	if err != nil {
		t.Fatalf("query multi_period indexes: %v", err)
	}
	defer mpIdxRows.Close()
	for mpIdxRows.Next() {
		var name string
		if err := mpIdxRows.Scan(&name); err != nil {
			t.Fatalf("scan multi_period index name: %v", err)
		}
		if _, ok := wantMultiIndexes[name]; ok {
			wantMultiIndexes[name] = true
		}
	}
	for idx, seen := range wantMultiIndexes {
		if !seen {
			t.Errorf("expected index %q on multi_period_results", idx)
		}
	}

	// PR-13 follow-up (#120): walk_forward_results table + indexes.
	wantWFCols := map[string]bool{
		"id":                 false,
		"created_at":         false,
		"base_profile":       false,
		"pdca_cycle_id":      false,
		"hypothesis":         false,
		"objective":          false,
		"parent_result_id":   false,
		"request_json":       false,
		"result_json":        false,
		"aggregate_oos_json": false,
	}
	wfRows, err := db.Query("PRAGMA table_info(walk_forward_results)")
	if err != nil {
		t.Fatalf("pragma table_info(walk_forward_results): %v", err)
	}
	defer wfRows.Close()
	for wfRows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    interface{}
			pk      int
		)
		if err := wfRows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan walk_forward_results table_info: %v", err)
		}
		if _, ok := wantWFCols[name]; ok {
			wantWFCols[name] = true
		}
	}
	if err := wfRows.Err(); err != nil {
		t.Fatalf("iterate walk_forward_results table_info: %v", err)
	}
	for col, seen := range wantWFCols {
		if !seen {
			t.Errorf("expected column %q in walk_forward_results", col)
		}
	}

	wantWFIndexes := map[string]bool{
		"idx_wf_created": false,
		"idx_wf_profile": false,
		"idx_wf_pdca":    false,
	}
	wfIdxRows, err := db.Query("SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='walk_forward_results'")
	if err != nil {
		t.Fatalf("query walk_forward indexes: %v", err)
	}
	defer wfIdxRows.Close()
	for wfIdxRows.Next() {
		var name string
		if err := wfIdxRows.Scan(&name); err != nil {
			t.Fatalf("scan walk_forward index name: %v", err)
		}
		if _, ok := wantWFIndexes[name]; ok {
			wantWFIndexes[name] = true
		}
	}
	for idx, seen := range wantWFIndexes {
		if !seen {
			t.Errorf("expected index %q on walk_forward_results", idx)
		}
	}
}

func TestRunMigrations_DecisionLogTablesAndIndexes(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	// Idempotency.
	if err := RunMigrations(db); err != nil {
		t.Fatalf("re-run migration failed: %v", err)
	}

	for _, table := range []string{"decision_log", "backtest_decision_log"} {
		var count int
		err := db.QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&count)
		if err != nil {
			t.Fatalf("query table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("table %s should exist", table)
		}
	}

	for _, idx := range []string{
		"idx_decision_log_symbol_time",
		"idx_decision_log_created",
		"idx_backtest_decision_log_run",
		"idx_backtest_decision_log_created",
	} {
		var count int
		err := db.QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?`, idx,
		).Scan(&count)
		if err != nil {
			t.Fatalf("query index %s: %v", idx, err)
		}
		if count != 1 {
			t.Errorf("index %s should exist", idx)
		}
	}
}

// TestRunMigrations_DecisionLogV2Columns: Phase 1 (Signal/Decision/ExecutionPolicy
// 三層分離) で追加した 6 カラムが decision_log と backtest_decision_log の両方に
// 存在することを確認。冪等性、既存データ保護も検証。
func TestRunMigrations_DecisionLogV2Columns(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("first migration failed: %v", err)
	}

	// 既存データを 1 行入れて、2 度目の migration でも残ることを確認 (ALTER TABLE 安全性)。
	_, err = db.Exec(`INSERT INTO decision_log (
		bar_close_at, sequence_in_bar, trigger_kind,
		symbol_id, currency_pair, primary_interval,
		stance, last_price, signal_action,
		risk_outcome, order_outcome, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1700000000000, 0, "BAR_CLOSE",
		1, "LTC_JPY", "PT15M",
		"TREND_FOLLOW", 8900.0, "BUY",
		"APPROVED", "FILLED", 1700000000000)
	if err != nil {
		t.Fatalf("insert pre-migration row failed: %v", err)
	}

	// 2 度実行しても壊れないことを確認 (冪等性)。
	if err := RunMigrations(db); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}

	// データが残っていることを確認。
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM decision_log`).Scan(&count); err != nil {
		t.Fatalf("count after second migration: %v", err)
	}
	if count != 1 {
		t.Errorf("decision_log row count = %d, want 1 (data lost during migration)", count)
	}

	// 既存行の新カラムが DEFAULT 値になっていることを確認。
	var (
		signalDirection   string
		signalStrength    float64
		decisionIntent    string
		decisionSide      string
		decisionReason    string
		exitPolicyOutcome string
	)
	err = db.QueryRow(`SELECT signal_direction, signal_strength, decision_intent,
		decision_side, decision_reason, exit_policy_outcome FROM decision_log`).
		Scan(&signalDirection, &signalStrength, &decisionIntent,
			&decisionSide, &decisionReason, &exitPolicyOutcome)
	if err != nil {
		t.Fatalf("select new columns: %v", err)
	}
	if signalDirection != "" || decisionIntent != "" || decisionSide != "" ||
		decisionReason != "" || exitPolicyOutcome != "" {
		t.Errorf("new TEXT columns should default to empty, got dir=%q intent=%q side=%q reason=%q exit=%q",
			signalDirection, decisionIntent, decisionSide, decisionReason, exitPolicyOutcome)
	}
	if signalStrength != 0 {
		t.Errorf("signal_strength should default to 0, got %v", signalStrength)
	}

	// 新カラムが両テーブルに存在することを PRAGMA で確認。
	wantNewCols := []string{
		"signal_direction",
		"signal_strength",
		"decision_intent",
		"decision_side",
		"decision_reason",
		"exit_policy_outcome",
	}
	for _, table := range []string{"decision_log", "backtest_decision_log"} {
		seen := make(map[string]bool, len(wantNewCols))
		for _, c := range wantNewCols {
			seen[c] = false
		}
		rows, err := db.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			t.Fatalf("pragma table_info(%s): %v", table, err)
		}
		for rows.Next() {
			var (
				cid     int
				name    string
				ctype   string
				notnull int
				dflt    interface{}
				pk      int
			)
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				rows.Close()
				t.Fatalf("scan table_info(%s): %v", table, err)
			}
			if _, ok := seen[name]; ok {
				seen[name] = true
			}
		}
		rows.Close()
		for c, ok := range seen {
			if !ok {
				t.Errorf("expected column %q in %s", c, table)
			}
		}
	}
}
