package decisionlog

import (
	"context"
	"errors"
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

func TestRecorder_HoldOnlyBarFlushesOnNextIndicator(t *testing.T) {
	repo := &stubRepo{}
	rec := newRecorderForTest(repo)
	ctx := context.Background()

	if _, err := rec.Handle(ctx, indicatorEvent(7, 1_000)); err != nil {
		t.Fatalf("Handle bar1: %v", err)
	}
	if len(repo.inserted) != 0 {
		t.Fatalf("after bar1 alone, expected 0 inserts, got %d", len(repo.inserted))
	}

	if _, err := rec.Handle(ctx, indicatorEvent(7, 2_000)); err != nil {
		t.Fatalf("Handle bar2: %v", err)
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("expected 1 insert (bar1 flushed), got %d", len(repo.inserted))
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

	if len(repo.inserted) != 1 {
		t.Fatalf("expected 1 insert (flushed on OrderEvent), got %d", len(repo.inserted))
	}
	got := repo.inserted[0]
	if got.SignalAction != "BUY" || got.RiskOutcome != entity.DecisionRiskApproved ||
		got.BookGateOutcome != entity.DecisionBookAllowed || got.OrderOutcome != entity.DecisionOrderFilled {
		t.Errorf("flushed record fields wrong: %+v", got)
	}
	if got.OpenedPositionID != 100 || got.OrderID != 42 || got.ExecutedAmount != 0.5 {
		t.Errorf("execution fields wrong: %+v", got)
	}
}

func TestRecorder_RiskRejectionFlushesImmediately(t *testing.T) {
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
		t.Fatalf("expected 1 insert (flushed on Rejected), got %d", len(repo.inserted))
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

	if len(repo.inserted) != 1 {
		t.Fatalf("expected 1 insert (tick row, bar1 still pending), got %d", len(repo.inserted))
	}
	got := repo.inserted[0]
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
