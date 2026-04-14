package backtest

import (
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestSimExecutor_SelectSLTPExit_BuyWorstCase(t *testing.T) {
	sim := NewSimExecutor(SimConfig{})
	price, reason, hit := sim.SelectSLTPExit(
		entity.OrderSideBuy,
		95,
		105,
		94,  // sl hit
		106, // tp hit
	)
	if !hit {
		t.Fatal("expected hit")
	}
	if reason != "stop_loss" {
		t.Fatalf("expected stop_loss, got %s", reason)
	}
	if price != 95 {
		t.Fatalf("expected stop-loss price 95, got %f", price)
	}
}

func TestSimExecutor_SelectSLTPExit_SellWorstCase(t *testing.T) {
	sim := NewSimExecutor(SimConfig{})
	price, reason, hit := sim.SelectSLTPExit(
		entity.OrderSideSell,
		105,
		95,
		94,  // tp hit
		106, // sl hit
	)
	if !hit {
		t.Fatal("expected hit")
	}
	if reason != "stop_loss" {
		t.Fatalf("expected stop_loss, got %s", reason)
	}
	if price != 105 {
		t.Fatalf("expected stop-loss price 105, got %f", price)
	}
}

func TestSimExecutor_OpenClose_AppliesCarryingAndSpread(t *testing.T) {
	sim := NewSimExecutor(SimConfig{
		InitialBalance:    100000,
		SpreadPercent:     0.1,
		DailyCarryingCost: 0.04,
		SlippagePercent:   0,
	})

	entryTS := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC).UnixMilli()
	orderOpen, err := sim.Open(7, entity.OrderSideBuy, 100, 1, "entry", entryTS)
	if err != nil {
		t.Fatalf("open error: %v", err)
	}
	if orderOpen.Action != "open" {
		t.Fatalf("expected open action, got %s", orderOpen.Action)
	}

	pos := sim.Positions()[0]
	exitTS := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	_, trade, err := sim.Close(pos.PositionID, 110, "exit", exitTS)
	if err != nil {
		t.Fatalf("close error: %v", err)
	}
	if trade == nil {
		t.Fatal("trade record should not be nil")
	}
	if trade.CarryingCost <= 0 {
		t.Fatalf("expected positive carrying cost, got %f", trade.CarryingCost)
	}
	if trade.SpreadCost <= 0 {
		t.Fatalf("expected positive spread cost, got %f", trade.SpreadCost)
	}
	if sim.Balance() <= 100000 {
		t.Fatalf("expected profitable close balance > initial, got %f", sim.Balance())
	}
}

func TestSimExecutor_EquityIncludesUnrealizedPnL(t *testing.T) {
	sim := NewSimExecutor(SimConfig{
		InitialBalance: 100000,
	})
	entryTS := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC).UnixMilli()
	if _, err := sim.Open(7, entity.OrderSideBuy, 100, 1, "entry", entryTS); err != nil {
		t.Fatalf("open error: %v", err)
	}
	eq := sim.Equity(map[int64]float64{7: 120})
	if eq <= 100000 {
		t.Fatalf("expected equity above initial with unrealized gain, got %f", eq)
	}
}
