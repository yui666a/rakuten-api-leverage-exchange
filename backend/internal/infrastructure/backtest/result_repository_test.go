package backtest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
)

func TestResultRepository_SaveListFindDelete(t *testing.T) {
	tmp := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	defer db.Close()
	if err := database.RunMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewResultRepository(db)
	now := time.Now().Unix()

	result := entity.BacktestResult{
		ID:        "bt-test-1",
		CreatedAt: now,
		Config: entity.BacktestConfig{
			Symbol:           "BTC_JPY",
			SymbolID:         7,
			PrimaryInterval:  "PT15M",
			HigherTFInterval: "PT1H",
			FromTimestamp:    1000,
			ToTimestamp:      2000,
		},
		Summary: entity.BacktestSummary{
			InitialBalance:     100000,
			FinalBalance:       110000,
			TotalReturn:        0.1,
			TotalTrades:        1,
			WinTrades:          1,
			LossTrades:         0,
			WinRate:            100,
			ProfitFactor:       2,
			MaxDrawdown:        0.05,
			MaxDrawdownBalance: 95000,
			SharpeRatio:        1.2,
			AvgHoldSeconds:     3600,
			TotalCarryingCost:  100,
			TotalSpreadCost:    50,
		},
		Trades: []entity.BacktestTradeRecord{
			{
				TradeID:      1,
				SymbolID:     7,
				EntryTime:    1100,
				ExitTime:     1500,
				Side:         "BUY",
				EntryPrice:   100,
				ExitPrice:    110,
				Amount:       0.01,
				PnL:          10,
				PnLPercent:   10,
				CarryingCost: 1,
				SpreadCost:   0.5,
				ReasonEntry:  "entry",
				ReasonExit:   "exit",
			},
		},
	}
	if err := repo.Save(context.Background(), result); err != nil {
		t.Fatalf("save: %v", err)
	}

	list, err := repo.List(context.Background(), repository.BacktestResultFilter{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 result, got %d", len(list))
	}
	if list[0].ID != result.ID {
		t.Fatalf("unexpected list id: %s", list[0].ID)
	}

	found, err := repo.FindByID(context.Background(), result.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found == nil {
		t.Fatal("expected found result")
	}
	if len(found.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(found.Trades))
	}

	deleted, err := repo.DeleteOlderThan(context.Background(), now+1)
	if err != nil {
		t.Fatalf("delete older than: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted row, got %d", deleted)
	}
}
