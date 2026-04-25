package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

func newRepoFixture(t *testing.T) *TradingConfigRepo {
	t.Helper()
	dir := t.TempDir()
	db, err := NewDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := RunMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewTradingConfigRepo(db)
}

func TestTradingConfigRepo_LoadEmpty(t *testing.T) {
	repo := newRepoFixture(t)
	got, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil on empty table, got %+v", got)
	}
}

func TestTradingConfigRepo_SaveLoadRoundTrip(t *testing.T) {
	repo := newRepoFixture(t)
	want := repository.TradingConfigState{SymbolID: 10, TradeAmount: 1500}
	if err := repo.Save(context.Background(), want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("expected loaded state, got nil")
	}
	if got.SymbolID != want.SymbolID || got.TradeAmount != want.TradeAmount {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, want)
	}
	if got.UpdatedAt == 0 {
		t.Fatalf("UpdatedAt should be set, got 0")
	}
}

func TestTradingConfigRepo_SaveOverwrites(t *testing.T) {
	repo := newRepoFixture(t)
	if err := repo.Save(context.Background(), repository.TradingConfigState{SymbolID: 7, TradeAmount: 500}); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := repo.Save(context.Background(), repository.TradingConfigState{SymbolID: 10, TradeAmount: 1500}); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	got, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.SymbolID != 10 || got.TradeAmount != 1500 {
		t.Fatalf("upsert did not overwrite: got %+v", got)
	}
}
