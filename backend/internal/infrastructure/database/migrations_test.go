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
