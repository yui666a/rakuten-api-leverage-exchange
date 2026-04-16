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
		"profile_name":      false,
		"pdca_cycle_id":     false,
		"hypothesis":        false,
		"parent_result_id":  false,
		"biweekly_win_rate": false,
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
}
