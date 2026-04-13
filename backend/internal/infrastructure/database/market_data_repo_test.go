package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func setupTestDB(t *testing.T) *MarketDataRepo {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewMarketDataRepo(db)
}

func TestSaveAndGetCandle(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	candle := entity.Candle{
		Open: 5000000, High: 5010000, Low: 4990000,
		Close: 5005000, Volume: 10.5, Time: 1700000000000,
	}

	err := repo.SaveCandle(ctx, 7, "PT1M", candle)
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}

	candles, err := repo.GetCandles(ctx, 7, "PT1M", 10, 0)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if len(candles) != 1 {
		t.Fatalf("expected 1 candle, got %d", len(candles))
	}
	if candles[0].Close != 5005000 {
		t.Fatalf("expected close 5005000, got %f", candles[0].Close)
	}
}

func TestSaveCandle_DuplicateIgnored(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	candle := entity.Candle{
		Open: 5000000, High: 5010000, Low: 4990000,
		Close: 5005000, Volume: 10.5, Time: 1700000000000,
	}

	_ = repo.SaveCandle(ctx, 7, "PT1M", candle)
	err := repo.SaveCandle(ctx, 7, "PT1M", candle)
	if err != nil {
		t.Fatalf("duplicate save should not error: %v", err)
	}

	candles, err := repo.GetCandles(ctx, 7, "PT1M", 10, 0)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(candles) != 1 {
		t.Fatalf("expected 1 candle (duplicate ignored), got %d", len(candles))
	}
}

func TestSaveCandles_Batch(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	candles := []entity.Candle{
		{Open: 5000000, High: 5010000, Low: 4990000, Close: 5005000, Volume: 10.5, Time: 1700000000000},
		{Open: 5005000, High: 5020000, Low: 5000000, Close: 5015000, Volume: 8.3, Time: 1700000060000},
		{Open: 5015000, High: 5025000, Low: 5010000, Close: 5020000, Volume: 12.1, Time: 1700000120000},
	}

	err := repo.SaveCandles(ctx, 7, "PT1M", candles)
	if err != nil {
		t.Fatalf("batch save failed: %v", err)
	}

	result, err := repo.GetCandles(ctx, 7, "PT1M", 10, 0)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 candles, got %d", len(result))
	}

	// Newest first
	if result[0].Time != 1700000120000 {
		t.Fatalf("expected newest first, got time %d", result[0].Time)
	}
}

func TestGetCandles_Limit(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	candles := make([]entity.Candle, 5)
	for i := range candles {
		candles[i] = entity.Candle{
			Open: 5000000, High: 5010000, Low: 4990000,
			Close: 5005000, Volume: 10.5,
			Time: int64(1700000000000 + i*60000),
		}
	}
	_ = repo.SaveCandles(ctx, 7, "PT1M", candles)

	result, err := repo.GetCandles(ctx, 7, "PT1M", 3, 0)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 candles (limited), got %d", len(result))
	}
}

func TestGetCandles_BeforeCursor(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	candles := []entity.Candle{
		{Open: 1, High: 2, Low: 0, Close: 1, Volume: 1, Time: 1000},
		{Open: 2, High: 3, Low: 1, Close: 2, Volume: 2, Time: 2000},
		{Open: 3, High: 4, Low: 2, Close: 3, Volume: 3, Time: 3000},
		{Open: 4, High: 5, Low: 3, Close: 4, Volume: 4, Time: 4000},
		{Open: 5, High: 6, Low: 4, Close: 5, Volume: 5, Time: 5000},
	}
	_ = repo.SaveCandles(ctx, 7, "PT1M", candles)

	// before=3000 should return candles with time < 3000, i.e. time=2000 and time=1000
	result, err := repo.GetCandles(ctx, 7, "PT1M", 10, 3000)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 candles before time=3000, got %d", len(result))
	}
	// Newest first
	if result[0].Time != 2000 {
		t.Fatalf("expected newest first (time=2000), got time=%d", result[0].Time)
	}
	if result[1].Time != 1000 {
		t.Fatalf("expected second (time=1000), got time=%d", result[1].Time)
	}

	// before=0 should return all (no filter)
	all, err := repo.GetCandles(ctx, 7, "PT1M", 10, 0)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("expected 5 candles with before=0, got %d", len(all))
	}

	// before with limit
	limited, err := repo.GetCandles(ctx, 7, "PT1M", 1, 5000)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("expected 1 candle with limit=1, got %d", len(limited))
	}
	if limited[0].Time != 4000 {
		t.Fatalf("expected time=4000 (newest before 5000 with limit 1), got %d", limited[0].Time)
	}
}

func TestGetCandles_FilterBySymbolAndInterval(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_ = repo.SaveCandle(ctx, 7, "PT1M", entity.Candle{Open: 1, High: 2, Low: 0, Close: 1, Volume: 1, Time: 1000})
	_ = repo.SaveCandle(ctx, 7, "PT5M", entity.Candle{Open: 1, High: 2, Low: 0, Close: 1, Volume: 1, Time: 1000})
	_ = repo.SaveCandle(ctx, 8, "PT1M", entity.Candle{Open: 1, High: 2, Low: 0, Close: 1, Volume: 1, Time: 1000})

	result, err := repo.GetCandles(ctx, 7, "PT1M", 10, 0)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 candle filtered by symbol+interval, got %d", len(result))
	}
}

func TestSaveAndGetTicker(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	ticker := entity.Ticker{
		SymbolID: 7, BestAsk: 5000100, BestBid: 5000000,
		Open: 4900000, High: 5100000, Low: 4800000,
		Last: 5000050, Volume: 123.45, Timestamp: 1700000000000,
	}

	err := repo.SaveTicker(ctx, ticker)
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}

	latest, err := repo.GetLatestTicker(ctx, 7)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if latest.Last != 5000050 {
		t.Fatalf("expected last 5000050, got %f", latest.Last)
	}
}

func TestGetLatestTicker_ReturnsNewest(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_ = repo.SaveTicker(ctx, entity.Ticker{SymbolID: 7, Last: 100, Timestamp: 1000})
	_ = repo.SaveTicker(ctx, entity.Ticker{SymbolID: 7, Last: 200, Timestamp: 2000})
	_ = repo.SaveTicker(ctx, entity.Ticker{SymbolID: 7, Last: 150, Timestamp: 1500})

	latest, err := repo.GetLatestTicker(ctx, 7)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if latest.Last != 200 {
		t.Fatalf("expected latest last=200, got %f", latest.Last)
	}
}

func TestGetLatestTicker_NotFound(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.GetLatestTicker(ctx, 999)
	if err == nil {
		t.Fatal("expected error for non-existent symbol")
	}
}
