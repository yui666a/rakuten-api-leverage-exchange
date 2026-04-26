package decisionlog

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

type stubRepo struct {
	inserted  []entity.DecisionRecord
	insertErr error
}

func (s *stubRepo) Insert(_ context.Context, rec entity.DecisionRecord) error {
	if s.insertErr != nil {
		return s.insertErr
	}
	s.inserted = append(s.inserted, rec)
	return nil
}

func (s *stubRepo) InsertAndID(_ context.Context, rec entity.DecisionRecord) (int64, error) {
	if s.insertErr != nil {
		return 0, s.insertErr
	}
	s.inserted = append(s.inserted, rec)
	// id starts at 1 and matches the insertion order so tests can correlate
	// later Update calls back to a specific row.
	return int64(len(s.inserted)), nil
}

func (s *stubRepo) Update(_ context.Context, rec entity.DecisionRecord) error {
	if s.insertErr != nil {
		// Reuse the same fault-injection knob — tests only need one switch.
		return s.insertErr
	}
	for i := range s.inserted {
		if int64(i+1) == rec.ID {
			s.inserted[i] = rec
			return nil
		}
	}
	return fmt.Errorf("stub Update: id %d not found", rec.ID)
}

func (s *stubRepo) List(_ context.Context, _ repository.DecisionLogFilter) ([]entity.DecisionRecord, int64, error) {
	return nil, 0, nil
}

func newRecorderForTest(repo repository.DecisionLogRepository) *Recorder {
	return NewRecorder(repo, RecorderConfig{
		SymbolID:        7,
		CurrencyPair:    "LTC_JPY",
		PrimaryInterval: "PT15M",
		StanceProvider:  func() string { return "TREND_FOLLOW" },
	})
}

func indicatorEvent(symbolID int64, ts int64) entity.IndicatorEvent {
	return entity.IndicatorEvent{
		SymbolID:  symbolID,
		Interval:  "PT15M",
		LastPrice: 30210,
		Timestamp: ts,
	}
}

func TestRecorder_HoldOnlyBarInsertsImmediatelyOnIndicator(t *testing.T) {
	// New flush model: every IndicatorEvent INSERTs a row right away with
	// HOLD/SKIPPED/NOOP defaults, so HOLD-only bars are visible without
	// waiting for the next bar.
	repo := &stubRepo{}
	rec := newRecorderForTest(repo)
	ctx := context.Background()

	if _, err := rec.Handle(ctx, indicatorEvent(7, 1_000)); err != nil {
		t.Fatalf("Handle bar1: %v", err)
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("after bar1 indicator, expected 1 insert, got %d", len(repo.inserted))
	}
	// A second bar adds another row; the first row stays as HOLD/SKIPPED/NOOP.
	if _, err := rec.Handle(ctx, indicatorEvent(7, 2_000)); err != nil {
		t.Fatalf("Handle bar2: %v", err)
	}
	if len(repo.inserted) != 2 {
		t.Fatalf("after bar2 indicator, expected 2 inserts, got %d", len(repo.inserted))
	}
	got := repo.inserted[0]
	if got.SignalAction != "HOLD" {
		t.Errorf("SignalAction = %q, want HOLD", got.SignalAction)
	}
	if got.RiskOutcome != entity.DecisionRiskSkipped {
		t.Errorf("RiskOutcome = %q, want SKIPPED", got.RiskOutcome)
	}
	if got.OrderOutcome != entity.DecisionOrderNoop {
		t.Errorf("OrderOutcome = %q, want NOOP", got.OrderOutcome)
	}
	if got.TriggerKind != entity.DecisionTriggerBarClose {
		t.Errorf("TriggerKind = %q, want BAR_CLOSE", got.TriggerKind)
	}
	if got.BarCloseAt != 1_000 {
		t.Errorf("BarCloseAt = %d, want 1_000", got.BarCloseAt)
	}
	if got.IndicatorsJSON == "" {
		t.Errorf("IndicatorsJSON must not be empty")
	}
}

func TestRecorder_FullBuyFlushesOnOrder(t *testing.T) {
	repo := &stubRepo{}
	rec := newRecorderForTest(repo)
	ctx := context.Background()

	if _, err := rec.Handle(ctx, indicatorEvent(7, 1_000)); err != nil {
		t.Fatalf("Handle indicator: %v", err)
	}
	if _, err := rec.Handle(ctx, entity.SignalEvent{
		Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy, Confidence: 0.7, Reason: "ema cross", Timestamp: 1_000},
		Price:     30210,
		Timestamp: 1_000,
	}); err != nil {
		t.Fatalf("Handle signal: %v", err)
	}
	if _, err := rec.Handle(ctx, entity.ApprovedSignalEvent{
		Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy, Reason: "ema cross"},
		Price:     30210,
		Timestamp: 1_000,
		Amount:    0.5,
	}); err != nil {
		t.Fatalf("Handle approved: %v", err)
	}
	if _, err := rec.Handle(ctx, entity.OrderEvent{
		OrderID: 42, SymbolID: 7, Side: "BUY", Action: "open",
		Price: 30215, Amount: 0.5, Reason: "ema cross", Timestamp: 1_001,
		Trigger: entity.DecisionTriggerBarClose, OpenedPositionID: 100,
	}); err != nil {
		t.Fatalf("Handle order: %v", err)
	}

	// Immediate-flush model: 1 INSERT (Indicator) + 3 UPDATEs (Signal,
	// Approved, Order) all on the same row id.
	if len(repo.inserted) != 1 {
		t.Fatalf("expected 1 insert (single row UPDATEd in place), got %d", len(repo.inserted))
	}
	got := repo.inserted[0]
	if got.SignalAction != "BUY" || got.RiskOutcome != entity.DecisionRiskApproved ||
		got.BookGateOutcome != entity.DecisionBookAllowed || got.OrderOutcome != entity.DecisionOrderFilled {
		t.Errorf("final record fields wrong: %+v", got)
	}
	if got.OpenedPositionID != 100 || got.OrderID != 42 || got.ExecutedAmount != 0.5 {
		t.Errorf("execution fields wrong: %+v", got)
	}
}

func TestRecorder_RiskRejectionUpdatesInPlace(t *testing.T) {
	repo := &stubRepo{}
	rec := newRecorderForTest(repo)
	ctx := context.Background()

	_, _ = rec.Handle(ctx, indicatorEvent(7, 1_000))
	_, _ = rec.Handle(ctx, entity.SignalEvent{
		Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy, Reason: "ema cross"},
		Price:     30210,
		Timestamp: 1_000,
	})
	_, _ = rec.Handle(ctx, entity.RejectedSignalEvent{
		Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionBuy, Reason: "ema cross"},
		Stage:     entity.RejectedStageRisk,
		Reason:    "daily loss limit hit",
		Price:     30210,
		Timestamp: 1_000,
	})

	if len(repo.inserted) != 1 {
		t.Fatalf("expected 1 row (Indicator inserted, then UPDATEd by Signal+Rejected), got %d", len(repo.inserted))
	}
	got := repo.inserted[0]
	if got.RiskOutcome != entity.DecisionRiskRejected || got.RiskReason != "daily loss limit hit" {
		t.Errorf("risk fields wrong: %+v", got)
	}
	if got.SignalAction != "BUY" {
		t.Errorf("SignalAction must be preserved as BUY, got %q", got.SignalAction)
	}
	if got.OrderOutcome != entity.DecisionOrderNoop {
		t.Errorf("OrderOutcome must remain NOOP, got %q", got.OrderOutcome)
	}
}

func TestRecorder_BookGateVetoMarksApprovedThenVetoed(t *testing.T) {
	repo := &stubRepo{}
	rec := newRecorderForTest(repo)
	ctx := context.Background()

	_, _ = rec.Handle(ctx, indicatorEvent(7, 1_000))
	_, _ = rec.Handle(ctx, entity.SignalEvent{
		Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionSell, Reason: "rsi extreme"},
		Price:     30210,
		Timestamp: 1_000,
	})
	_, _ = rec.Handle(ctx, entity.RejectedSignalEvent{
		Signal:    entity.Signal{SymbolID: 7, Action: entity.SignalActionSell, Reason: "rsi extreme"},
		Stage:     entity.RejectedStageBookGate,
		Reason:    "thin book on bid",
		Price:     30210,
		Timestamp: 1_000,
	})

	if len(repo.inserted) != 1 {
		t.Fatalf("expected 1 insert, got %d", len(repo.inserted))
	}
	got := repo.inserted[0]
	if got.RiskOutcome != entity.DecisionRiskApproved {
		t.Errorf("RiskOutcome must be APPROVED (book gate is post-risk), got %q", got.RiskOutcome)
	}
	if got.BookGateOutcome != entity.DecisionBookVetoed || got.BookGateReason != "thin book on bid" {
		t.Errorf("book gate fields wrong: %+v", got)
	}
}

func TestRecorder_TickSLTPClosePersistedAsSeparateRow(t *testing.T) {
	repo := &stubRepo{}
	rec := newRecorderForTest(repo)
	ctx := context.Background()

	_, _ = rec.Handle(ctx, indicatorEvent(7, 1_000))
	_, _ = rec.Handle(ctx, entity.OrderEvent{
		OrderID: 99, SymbolID: 7, Side: "SELL", Action: "close",
		Price: 30180, Amount: 0.5, Reason: "stop_loss", Timestamp: 1_500,
		Trigger: entity.DecisionTriggerTickSLTP, ClosedPositionID: 100,
	})

	// Immediate-flush model: bar1 INSERT (BAR_CLOSE) + tick INSERT = 2 rows.
	if len(repo.inserted) != 2 {
		t.Fatalf("expected 2 inserts (BAR_CLOSE + tick row), got %d", len(repo.inserted))
	}
	got := repo.inserted[1]
	if got.TriggerKind != entity.DecisionTriggerTickSLTP {
		t.Errorf("TriggerKind = %q, want TICK_SLTP", got.TriggerKind)
	}
	if got.ClosedPositionID != 100 {
		t.Errorf("ClosedPositionID = %d, want 100", got.ClosedPositionID)
	}
	if got.SequenceInBar != 1 {
		t.Errorf("SequenceInBar = %d, want 1 (bar1 BAR_CLOSE = 0, then this = 1)", got.SequenceInBar)
	}
	if got.SignalReason != "stop_loss" {
		t.Errorf("SignalReason = %q, want %q", got.SignalReason, "stop_loss")
	}
}

func TestRecorder_InsertErrorDoesNotPropagate(t *testing.T) {
	repo := &stubRepo{insertErr: errors.New("db down")}
	rec := newRecorderForTest(repo)
	ctx := context.Background()

	if _, err := rec.Handle(ctx, indicatorEvent(7, 1_000)); err != nil {
		t.Fatalf("Handle indicator returned error: %v", err)
	}
	if _, err := rec.Handle(ctx, indicatorEvent(7, 2_000)); err != nil {
		t.Fatalf("Handle indicator must swallow Insert errors, got: %v", err)
	}
}
