package decisionlog_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/database"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/decisionlog"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
)

func TestRecorder_EndToEnd_FullCycleAndHoldBar(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	if err := database.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	repo := database.NewDecisionLogRepository(db)

	rec := decisionlog.NewRecorder(repo, decisionlog.RecorderConfig{
		SymbolID:        7,
		CurrencyPair:    "LTC_JPY",
		PrimaryInterval: "PT15M",
		StanceProvider:  func() string { return "TREND_FOLLOW" },
	})

	bus := eventengine.NewEventBus()
	bus.Register(entity.EventTypeIndicator, 99, rec)
	bus.Register(entity.EventTypeSignal, 99, rec)
	bus.Register(entity.EventTypeApproved, 99, rec)
	bus.Register(entity.EventTypeRejected, 99, rec)
	bus.Register(entity.EventTypeOrder, 99, rec)

	ctx := context.Background()

	// Bar 1: full BUY cycle.
	if err := bus.Dispatch(ctx, []entity.Event{
		entity.IndicatorEvent{SymbolID: 7, Interval: "PT15M", LastPrice: 30210, Timestamp: 1_000},
		entity.SignalEvent{
			Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy, Confidence: 0.7, Reason: "ema cross"},
			Price:     30210,
			Timestamp: 1_000,
		},
		entity.ApprovedSignalEvent{
			Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy, Reason: "ema cross"},
			Price:     30210,
			Timestamp: 1_000,
			Amount:    0.5,
		},
		entity.OrderEvent{
			OrderID: 42, SymbolID: 7, Side: "BUY", Action: "open",
			Price: 30215, Amount: 0.5, Reason: "ema cross", Timestamp: 1_001,
			Trigger: entity.DecisionTriggerBarClose, OpenedPositionID: 100,
		},
	}); err != nil {
		t.Fatalf("Dispatch bar1: %v", err)
	}

	// Bar 2: HOLD only. Bar 3 indicator triggers the flush of bar 2's draft.
	if err := bus.Dispatch(ctx, []entity.Event{
		entity.IndicatorEvent{SymbolID: 7, Interval: "PT15M", LastPrice: 30220, Timestamp: 2_000},
	}); err != nil {
		t.Fatalf("Dispatch bar2: %v", err)
	}
	if err := bus.Dispatch(ctx, []entity.Event{
		entity.IndicatorEvent{SymbolID: 7, Interval: "PT15M", LastPrice: 30230, Timestamp: 3_000},
	}); err != nil {
		t.Fatalf("Dispatch bar3: %v", err)
	}

	rows, _, err := repo.List(ctx, repository.DecisionLogFilter{SymbolID: 7, Limit: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (bar1 + bar2), got %d", len(rows))
	}
	// Newest first: bar2 then bar1.
	if rows[0].BarCloseAt != 2_000 || rows[0].SignalAction != "HOLD" {
		t.Errorf("bar2 row wrong: %+v", rows[0])
	}
	if rows[1].BarCloseAt != 1_000 || rows[1].SignalAction != "BUY" || rows[1].OrderOutcome != entity.DecisionOrderFilled {
		t.Errorf("bar1 row wrong: %+v", rows[1])
	}
}
