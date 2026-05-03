package backtest

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
	strategyuc "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/strategy"
)

func TestIndicatorHandler_NoFutureHigherTFLeak(t *testing.T) {
	handler := NewIndicatorHandler("PT15M", "PT1H", 500)

	symbolID := int64(7)
	_, _ = handler.Handle(context.Background(), entity.CandleEvent{
		SymbolID: symbolID,
		Interval: "PT1H",
		Candle: entity.Candle{
			Open: 100, High: 101, Low: 99, Close: 100, Time: 1000,
		},
		Timestamp: 1000,
	})
	_, _ = handler.Handle(context.Background(), entity.CandleEvent{
		SymbolID: symbolID,
		Interval: "PT1H",
		Candle: entity.Candle{
			Open: 200, High: 201, Low: 199, Close: 200, Time: 2000,
		},
		Timestamp: 2000,
	})

	events, err := handler.Handle(context.Background(), entity.CandleEvent{
		SymbolID: symbolID,
		Interval: "PT15M",
		Candle: entity.Candle{
			Open: 150, High: 151, Low: 149, Close: 150, Time: 1500,
		},
		Timestamp: 1500,
	})
	if err != nil {
		t.Fatalf("handle error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	indicatorEvent, ok := events[0].(entity.IndicatorEvent)
	if !ok {
		t.Fatalf("expected IndicatorEvent, got %T", events[0])
	}
	if indicatorEvent.HigherTF == nil {
		t.Fatal("expected higherTF indicator set")
	}
	if indicatorEvent.HigherTF.Timestamp != 1000 {
		t.Fatalf("expected higherTF timestamp 1000, got %d", indicatorEvent.HigherTF.Timestamp)
	}
	if indicatorEvent.Primary.Timestamp != 1500 {
		t.Fatalf("expected primary timestamp 1500, got %d", indicatorEvent.Primary.Timestamp)
	}
}

func TestStrategyHandler_UsesIndicatorTimestamp(t *testing.T) {
	resolver := usecase.NewRuleBasedStanceResolver(nil)
	engine := usecase.NewStrategyEngine(resolver)
	handler := NewStrategyHandler(strategyuc.NewDefaultStrategy(engine))

	ts := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC).UnixMilli()
	events, err := handler.Handle(context.Background(), entity.IndicatorEvent{
		SymbolID:  7,
		Interval:  "PT15M",
		Timestamp: ts,
		LastPrice: 100,
		Primary: entity.IndicatorSet{
			SymbolID:  7,
			SMAShort:     floatPtr(120),
			SMALong:     floatPtr(100),
			EMAFast:     floatPtr(120),
			EMASlow:     floatPtr(100),
			RSI:     floatPtr(55),
			Histogram: floatPtr(1),
			Timestamp: ts,
		},
	})
	if err != nil {
		t.Fatalf("handle error: %v", err)
	}
	// PR2: actionable signal emits SignalEvent (legacy route) + MarketSignalEvent
	// (new route, shadow). HOLD bars emit only the MarketSignalEvent.
	if len(events) != 2 {
		t.Fatalf("expected 2 events (SignalEvent + MarketSignalEvent), got %d", len(events))
	}

	signalEvent, ok := events[0].(entity.SignalEvent)
	if !ok {
		t.Fatalf("expected SignalEvent at index 0, got %T", events[0])
	}
	if signalEvent.Signal.Timestamp != ts/1000 {
		t.Fatalf("expected signal timestamp %d, got %d", ts/1000, signalEvent.Signal.Timestamp)
	}
	if _, ok := events[1].(entity.MarketSignalEvent); !ok {
		t.Fatalf("expected MarketSignalEvent at index 1, got %T", events[1])
	}
}

// TestStrategyHandler_PR2_ShadowMarketSignal exercises the PR2 contract that
// StrategyHandler emits MarketSignalEvent on every non-nil signal — including
// HOLD bars where Direction=NEUTRAL and SignalEvent is intentionally dropped
// to preserve the legacy RiskHandler input.
func TestStrategyHandler_PR2_ShadowMarketSignal(t *testing.T) {
	cases := []struct {
		name        string
		action      entity.SignalAction
		wantSignal  bool
		wantDirEnum entity.SignalDirection
	}{
		{"buy", entity.SignalActionBuy, true, entity.DirectionBullish},
		{"sell", entity.SignalActionSell, true, entity.DirectionBearish},
		{"hold", entity.SignalActionHold, false, entity.DirectionNeutral},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			handler := &StrategyHandler{Strategy: stubStrategy{action: c.action, confidence: 0.42, reason: "stub"}}
			events, err := handler.Handle(context.Background(), entity.IndicatorEvent{
				SymbolID: 7, Interval: "PT15M", Timestamp: 1700000000000,
				LastPrice: 100,
				Primary:   entity.IndicatorSet{SymbolID: 7},
			})
			if err != nil {
				t.Fatalf("handle: %v", err)
			}
			wantLen := 1
			if c.wantSignal {
				wantLen = 2
			}
			if len(events) != wantLen {
				t.Fatalf("len(events) = %d, want %d", len(events), wantLen)
			}

			var ms entity.MarketSignalEvent
			for _, ev := range events {
				if x, ok := ev.(entity.MarketSignalEvent); ok {
					ms = x
				}
			}
			if ms.EventType() != entity.EventTypeMarketSignal {
				t.Fatalf("expected MarketSignalEvent, got %T", events[len(events)-1])
			}
			if ms.Signal.Direction != c.wantDirEnum {
				t.Errorf("Direction = %q, want %q", ms.Signal.Direction, c.wantDirEnum)
			}
			if ms.Signal.Strength != 0.42 {
				t.Errorf("Strength = %v, want 0.42", ms.Signal.Strength)
			}
			if ms.Signal.Source != "legacy_strategy_engine" {
				t.Errorf("Source = %q, want legacy_strategy_engine", ms.Signal.Source)
			}
		})
	}
}

func TestStrategyHandler_NilSignal_NoEvents(t *testing.T) {
	handler := &StrategyHandler{Strategy: stubStrategy{returnNil: true}}
	events, err := handler.Handle(context.Background(), entity.IndicatorEvent{
		SymbolID: 7, Timestamp: 1, Primary: entity.IndicatorSet{SymbolID: 7},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if events != nil {
		t.Errorf("expected nil events for nil signal, got %v", events)
	}
}

func TestTickGeneratorHandler_SequenceAndTimestamps(t *testing.T) {
	handler := &TickGeneratorHandler{PrimaryInterval: "PT15M"}
	c := entity.CandleEvent{
		SymbolID:  7,
		Interval:  "PT15M",
		Timestamp: 15 * 60 * 1000,
		Candle: entity.Candle{
			Open: 100, High: 110, Low: 90, Close: 105, Time: 15 * 60 * 1000,
		},
	}
	events, err := handler.Handle(context.Background(), c)
	if err != nil {
		t.Fatalf("handle error: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 ticks, got %d", len(events))
	}

	t1 := events[0].(entity.TickEvent)
	t2 := events[1].(entity.TickEvent)
	t3 := events[2].(entity.TickEvent)
	t4 := events[3].(entity.TickEvent)

	if t1.TickType != "open" || t2.TickType != "high" || t3.TickType != "low" || t4.TickType != "close" {
		t.Fatalf("unexpected tick order: %s %s %s %s", t1.TickType, t2.TickType, t3.TickType, t4.TickType)
	}
	if t1.Timestamp != 15*60*1000/4 {
		t.Fatalf("unexpected t1 timestamp: %d", t1.Timestamp)
	}
	if t4.Timestamp != c.Candle.Time {
		t.Fatalf("unexpected t4 timestamp: %d", t4.Timestamp)
	}
}

func floatPtr(v float64) *float64 { return &v }

// stubStrategy returns a fixed Signal regardless of inputs. Used by PR2 tests
// that exercise StrategyHandler's MarketSignal emission contract without
// depending on the real StrategyEngine's signal-generation logic.
type stubStrategy struct {
	action     entity.SignalAction
	confidence float64
	reason     string
	returnNil  bool
}

func (s stubStrategy) Name() string { return "stub" }

func (s stubStrategy) Evaluate(ctx context.Context, indicators *entity.IndicatorSet, higherTF *entity.IndicatorSet, lastPrice float64, now time.Time) (*entity.Signal, error) {
	if s.returnNil {
		return nil, nil
	}
	return &entity.Signal{
		SymbolID:   indicators.SymbolID,
		Action:     s.action,
		Confidence: s.confidence,
		Reason:     s.reason,
		Timestamp:  now.Unix(),
	}, nil
}

type fakeSignalExecutor struct {
	lastSymbolID  int64
	lastSide      entity.OrderSide
	lastPrice     float64
	lastAmount    float64
	lastReason    string
	lastTimestamp int64
}

func (f *fakeSignalExecutor) Open(symbolID int64, side entity.OrderSide, signalPrice, amount float64, reason string, timestamp int64) (entity.OrderEvent, error) {
	f.lastSymbolID = symbolID
	f.lastSide = side
	f.lastPrice = signalPrice
	f.lastAmount = amount
	f.lastReason = reason
	f.lastTimestamp = timestamp
	return entity.OrderEvent{
		OrderID:   1,
		SymbolID:  symbolID,
		Side:      string(side),
		Action:    "open",
		Price:     signalPrice,
		Amount:    amount,
		Reason:    reason,
		Timestamp: timestamp,
	}, nil
}

func TestExecutionHandler_ConvertsSignalToOrderEvent(t *testing.T) {
	exec := &fakeSignalExecutor{}
	handler := &ExecutionHandler{
		Executor:    exec,
		TradeAmount: 0.01,
	}

	events, err := handler.Handle(context.Background(), entity.ApprovedSignalEvent{
		Signal: entity.Signal{
			SymbolID: 7,
			Action:   entity.SignalActionSell,
			Reason:   "trend",
		},
		Price:     100,
		Timestamp: 12345,
	})
	if err != nil {
		t.Fatalf("handle error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	orderEvent, ok := events[0].(entity.OrderEvent)
	if !ok {
		t.Fatalf("expected OrderEvent, got %T", events[0])
	}
	if orderEvent.Action != "open" {
		t.Fatalf("expected open action, got %s", orderEvent.Action)
	}
	if exec.lastSide != entity.OrderSideSell {
		t.Fatalf("expected sell side, got %s", exec.lastSide)
	}
	if exec.lastAmount != 0.01 {
		t.Fatalf("expected amount 0.01, got %f", exec.lastAmount)
	}
}

func TestRiskHandler_EmitsApprovedSignalEvent(t *testing.T) {
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount:    1000000,
		MaxDailyLoss:         1000000,
		StopLossPercent:      5,
		TakeProfitPercent:    10,
		InitialCapital:       1000000,
		MaxConsecutiveLosses: 3,
		CooldownMinutes:      30,
	})
	handler := &RiskHandler{
		RiskManager: riskMgr,
		TradeAmount: 0.01,
	}

	events, err := handler.Handle(context.Background(), entity.ActionDecisionEvent{
		Decision: entity.ActionDecision{
			SymbolID: 7,
			Intent:   entity.IntentNewEntry,
			Side:     entity.OrderSideBuy,
			Reason:   "trend",
		},
		Price:     10000,
		Timestamp: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("handle error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 approved event, got %d", len(events))
	}
	if _, ok := events[0].(entity.ApprovedSignalEvent); !ok {
		t.Fatalf("expected ApprovedSignalEvent, got %T", events[0])
	}
}

func TestRiskHandler_EmitsRejectedOnRiskManagerVeto(t *testing.T) {
	// Daily loss already exceeds the cap so any new BUY proposal is rejected
	// by RiskManager.CheckOrderAt with a non-empty Reason.
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount:    1000000,
		MaxDailyLoss:         100,
		StopLossPercent:      5,
		TakeProfitPercent:    10,
		InitialCapital:       1000000,
		MaxConsecutiveLosses: 3,
		CooldownMinutes:      30,
	})
	riskMgr.RecordLoss(500) // push past MaxDailyLoss=100
	handler := &RiskHandler{
		RiskManager: riskMgr,
		TradeAmount: 0.01,
	}

	events, err := handler.Handle(context.Background(), entity.ActionDecisionEvent{
		Decision: entity.ActionDecision{
			SymbolID: 7,
			Intent:   entity.IntentNewEntry,
			Side:     entity.OrderSideBuy,
			Reason:   "ema cross",
		},
		Price:     10000,
		Timestamp: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("handle error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(events))
	}
	rej, ok := events[0].(entity.RejectedSignalEvent)
	if !ok {
		t.Fatalf("expected RejectedSignalEvent, got %T", events[0])
	}
	if rej.Stage != entity.RejectedStageRisk {
		t.Errorf("Stage = %q, want %q", rej.Stage, entity.RejectedStageRisk)
	}
	if rej.Reason == "" {
		t.Errorf("Reason must be populated from RiskManager check")
	}
	if rej.Signal.Action != entity.SignalActionBuy {
		t.Errorf("Signal must be carried through, got action %q", rej.Signal.Action)
	}
}

// TestRiskHandler_PR3_NonNewEntryIntentsAreSilent: HOLD / EXIT_CANDIDATE /
// COOLDOWN_BLOCKED must produce zero events when ExitOnSignal is OFF (the
// default). EXIT_CANDIDATE only becomes loud when both ExitOnSignal=true
// AND an executor is wired — that path has its own dedicated tests.
func TestRiskHandler_PR3_NonNewEntryIntentsAreSilent(t *testing.T) {
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 1_000_000,
		MaxDailyLoss:      1_000_000,
		InitialCapital:    1_000_000,
	})
	handler := &RiskHandler{RiskManager: riskMgr, TradeAmount: 0.01}

	for _, intent := range []entity.DecisionIntent{
		entity.IntentHold,
		entity.IntentExitCandidate,
		entity.IntentCooldownBlocked,
	} {
		events, err := handler.Handle(context.Background(), entity.ActionDecisionEvent{
			Decision: entity.ActionDecision{
				SymbolID: 7, Intent: intent, Side: entity.OrderSideBuy,
			},
			Price:     10000,
			Timestamp: time.Now().UnixMilli(),
		})
		if err != nil {
			t.Fatalf("Handle(%q): %v", intent, err)
		}
		if len(events) != 0 {
			t.Errorf("Intent %q should be silent, got %d events", intent, len(events))
		}
	}
}

// TestRiskHandler_PR3_OrderEventArmsCooldown verifies the close-detection
// path: an OrderEvent with ClosedPositionID > 0 must arm the entry cooldown
// via NoteClose, while opens (ClosedPositionID == 0) must not.
func TestRiskHandler_PR3_OrderEventArmsCooldown(t *testing.T) {
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		InitialCapital:   1_000_000,
		EntryCooldownSec: 30,
	})
	handler := &RiskHandler{RiskManager: riskMgr, TradeAmount: 0.01}

	closeTs := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC).UnixMilli()
	if _, err := handler.Handle(context.Background(), entity.OrderEvent{
		OrderID: 100, ClosedPositionID: 7, Timestamp: closeTs,
	}); err != nil {
		t.Fatalf("Handle close OrderEvent: %v", err)
	}
	if !riskMgr.IsEntryCooldown(time.UnixMilli(closeTs).Add(10 * time.Second)) {
		t.Error("close fill should have armed entry cooldown")
	}

	// Reset and confirm an open OrderEvent does NOT arm cooldown.
	riskMgr2 := usecase.NewRiskManager(entity.RiskConfig{
		InitialCapital: 1_000_000, EntryCooldownSec: 30,
	})
	handler2 := &RiskHandler{RiskManager: riskMgr2, TradeAmount: 0.01}
	if _, err := handler2.Handle(context.Background(), entity.OrderEvent{
		OrderID: 100, OpenedPositionID: 7, ClosedPositionID: 0, Timestamp: closeTs,
	}); err != nil {
		t.Fatalf("Handle open OrderEvent: %v", err)
	}
	if riskMgr2.IsEntryCooldown(time.UnixMilli(closeTs).Add(time.Second)) {
		t.Error("open fill must not arm entry cooldown")
	}

	// Failed close (OrderID == 0) must not arm cooldown either.
	riskMgr3 := usecase.NewRiskManager(entity.RiskConfig{
		InitialCapital: 1_000_000, EntryCooldownSec: 30,
	})
	handler3 := &RiskHandler{RiskManager: riskMgr3, TradeAmount: 0.01}
	if _, err := handler3.Handle(context.Background(), entity.OrderEvent{
		OrderID: 0, ClosedPositionID: 7, Timestamp: closeTs,
	}); err != nil {
		t.Fatalf("Handle failed close OrderEvent: %v", err)
	}
	if riskMgr3.IsEntryCooldown(time.UnixMilli(closeTs).Add(time.Second)) {
		t.Error("failed close must not arm entry cooldown")
	}
}

// fakeDecisionExitExecutor is a minimal DecisionExitExecutor for the
// EXIT_CANDIDATE branch tests. It records every Close call so we can assert
// the exact (positionID, price, timestamp, reason) tuples the handler emitted.
type fakeDecisionExitExecutor struct {
	positions []eventengine.Position
	closedIDs []int64
	closeErr  error
	closeCall []struct {
		positionID int64
		price      float64
		reason     string
		ts         int64
	}
}

func (f *fakeDecisionExitExecutor) Positions() []eventengine.Position {
	out := make([]eventengine.Position, len(f.positions))
	copy(out, f.positions)
	return out
}

func (f *fakeDecisionExitExecutor) Close(positionID int64, signalPrice float64, reason string, timestamp int64) (entity.OrderEvent, *entity.BacktestTradeRecord, error) {
	if f.closeErr != nil {
		return entity.OrderEvent{}, nil, f.closeErr
	}
	f.closedIDs = append(f.closedIDs, positionID)
	f.closeCall = append(f.closeCall, struct {
		positionID int64
		price      float64
		reason     string
		ts         int64
	}{positionID: positionID, price: signalPrice, reason: reason, ts: timestamp})
	// Return a minimal OrderEvent — the handler overwrites Trigger and
	// ClosedPositionID, so we only need the executor to surface a non-zero
	// OrderID so downstream cooldown plumbing (in production) would arm.
	return entity.OrderEvent{
		OrderID:   1000 + positionID,
		SymbolID:  0,
		Price:     signalPrice,
		Reason:    reason,
		Timestamp: timestamp,
	}, nil, nil
}

// TestRiskHandler_ExitOnSignal_DisabledStaysSilent is the safety net: with
// ExitOnSignal=false (default) the handler must keep the Phase 1 behaviour
// where IntentExitCandidate produces zero events. Wiring an executor must
// not flip the branch on by accident.
func TestRiskHandler_ExitOnSignal_DisabledStaysSilent(t *testing.T) {
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 1_000_000,
		InitialCapital:    1_000_000,
	})
	exec := &fakeDecisionExitExecutor{
		positions: []eventengine.Position{
			{PositionID: 1, SymbolID: 7, Side: entity.OrderSideBuy, Amount: 0.5, EntryPrice: 9000},
		},
	}
	handler := &RiskHandler{
		RiskManager:  riskMgr,
		TradeAmount:  0.01,
		Executor:     exec,
		ExitOnSignal: false,
	}

	events, err := handler.Handle(context.Background(), entity.ActionDecisionEvent{
		Decision: entity.ActionDecision{
			SymbolID: 7,
			Intent:   entity.IntentExitCandidate,
			Side:     entity.OrderSideSell,
			Reason:   "long held; bearish signal -> exit candidate",
		},
		Price:     10000,
		Timestamp: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("ExitOnSignal=false must stay silent, got %d events", len(events))
	}
	if len(exec.closedIDs) != 0 {
		t.Errorf("executor.Close must not be called, got %v", exec.closedIDs)
	}
}

// TestRiskHandler_ExitOnSignal_ClosesLongOnExitCandidate covers the headline
// case: long held + bearish MarketSignal → DecisionHandler emits
// IntentExitCandidate with Side=Sell → RiskHandler must close every Buy
// position on the symbol and tag the OrderEvent with DecisionTriggerDecisionExit.
func TestRiskHandler_ExitOnSignal_ClosesLongOnExitCandidate(t *testing.T) {
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 1_000_000,
		InitialCapital:    1_000_000,
	})
	exec := &fakeDecisionExitExecutor{
		positions: []eventengine.Position{
			{PositionID: 1, SymbolID: 7, Side: entity.OrderSideBuy, Amount: 0.5, EntryPrice: 9000},
			// Different symbol — must NOT be closed.
			{PositionID: 2, SymbolID: 99, Side: entity.OrderSideBuy, Amount: 0.5, EntryPrice: 9000},
			// Same symbol but already a Sell — must NOT be closed (the
			// Decision is to flatten longs, not shorts).
			{PositionID: 3, SymbolID: 7, Side: entity.OrderSideSell, Amount: 0.5, EntryPrice: 9000},
		},
	}
	handler := &RiskHandler{
		RiskManager:  riskMgr,
		TradeAmount:  0.01,
		Executor:     exec,
		ExitOnSignal: true,
	}

	ts := time.Now().UnixMilli()
	events, err := handler.Handle(context.Background(), entity.ActionDecisionEvent{
		Decision: entity.ActionDecision{
			SymbolID: 7,
			Intent:   entity.IntentExitCandidate,
			Side:     entity.OrderSideSell,
			Reason:   "long held; bearish signal -> exit candidate",
		},
		Price:     10000,
		Timestamp: ts,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 OrderEvent, got %d", len(events))
	}
	orderEvent, ok := events[0].(entity.OrderEvent)
	if !ok {
		t.Fatalf("expected OrderEvent, got %T", events[0])
	}
	if orderEvent.Trigger != entity.DecisionTriggerDecisionExit {
		t.Errorf("Trigger = %q, want %q", orderEvent.Trigger, entity.DecisionTriggerDecisionExit)
	}
	if orderEvent.ClosedPositionID != 1 {
		t.Errorf("ClosedPositionID = %d, want 1", orderEvent.ClosedPositionID)
	}
	if len(exec.closedIDs) != 1 || exec.closedIDs[0] != 1 {
		t.Errorf("closedIDs = %v, want [1]", exec.closedIDs)
	}
	if exec.closeCall[0].price != 10000 || exec.closeCall[0].ts != ts {
		t.Errorf("Close called with price=%v ts=%v; want 10000/%d", exec.closeCall[0].price, exec.closeCall[0].ts, ts)
	}
}

// TestRiskHandler_ExitOnSignal_ClosesShortOnExitCandidate is the symmetric
// case: short held + bullish MarketSignal → IntentExitCandidate Side=Buy →
// every Sell position on the symbol is closed.
func TestRiskHandler_ExitOnSignal_ClosesShortOnExitCandidate(t *testing.T) {
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 1_000_000,
		InitialCapital:    1_000_000,
	})
	exec := &fakeDecisionExitExecutor{
		positions: []eventengine.Position{
			{PositionID: 11, SymbolID: 7, Side: entity.OrderSideSell, Amount: 0.3, EntryPrice: 11000},
			{PositionID: 12, SymbolID: 7, Side: entity.OrderSideSell, Amount: 0.2, EntryPrice: 10800},
		},
	}
	handler := &RiskHandler{
		RiskManager:  riskMgr,
		TradeAmount:  0.01,
		Executor:     exec,
		ExitOnSignal: true,
	}

	events, err := handler.Handle(context.Background(), entity.ActionDecisionEvent{
		Decision: entity.ActionDecision{
			SymbolID: 7,
			Intent:   entity.IntentExitCandidate,
			Side:     entity.OrderSideBuy,
			Reason:   "short held; bullish signal -> exit candidate",
		},
		Price:     10500,
		Timestamp: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 OrderEvents (one per short position), got %d", len(events))
	}
	got := map[int64]bool{}
	for _, e := range events {
		oe, ok := e.(entity.OrderEvent)
		if !ok {
			t.Fatalf("expected OrderEvent, got %T", e)
		}
		got[oe.ClosedPositionID] = true
		if oe.Trigger != entity.DecisionTriggerDecisionExit {
			t.Errorf("Trigger = %q, want %q", oe.Trigger, entity.DecisionTriggerDecisionExit)
		}
	}
	if !got[11] || !got[12] {
		t.Errorf("expected both 11 and 12 closed, got %v", got)
	}
}

// TestRiskHandler_ExitOnSignal_NoPositionsNoEmits guards the "flat book"
// edge case: even with ExitOnSignal on, an EXIT_CANDIDATE with no matching
// position must be a no-op (the recorder already captured the decision).
func TestRiskHandler_ExitOnSignal_NoPositionsNoEmits(t *testing.T) {
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 1_000_000,
		InitialCapital:    1_000_000,
	})
	exec := &fakeDecisionExitExecutor{} // empty positions
	handler := &RiskHandler{
		RiskManager:  riskMgr,
		TradeAmount:  0.01,
		Executor:     exec,
		ExitOnSignal: true,
	}

	events, err := handler.Handle(context.Background(), entity.ActionDecisionEvent{
		Decision: entity.ActionDecision{
			SymbolID: 7,
			Intent:   entity.IntentExitCandidate,
			Side:     entity.OrderSideSell,
		},
		Price:     10000,
		Timestamp: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events when no positions match, got %d", len(events))
	}
	if len(exec.closedIDs) != 0 {
		t.Errorf("Close must not be called, got %v", exec.closedIDs)
	}
}

// TestRiskHandler_ExitOnSignal_NilExecutorIsSilent: ExitOnSignal=true but
// Executor=nil must stay silent (defensive — tests / partial wiring should
// never panic).
func TestRiskHandler_ExitOnSignal_NilExecutorIsSilent(t *testing.T) {
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 1_000_000,
		InitialCapital:    1_000_000,
	})
	handler := &RiskHandler{
		RiskManager:  riskMgr,
		TradeAmount:  0.01,
		Executor:     nil,
		ExitOnSignal: true,
	}
	events, err := handler.Handle(context.Background(), entity.ActionDecisionEvent{
		Decision: entity.ActionDecision{
			SymbolID: 7,
			Intent:   entity.IntentExitCandidate,
			Side:     entity.OrderSideSell,
		},
		Price:     10000,
		Timestamp: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("nil executor must not emit events, got %d", len(events))
	}
}

type fakeTickRiskExecutor struct {
	positions []eventengine.Position
	closedIDs []int64
}

func (f *fakeTickRiskExecutor) Positions() []eventengine.Position {
	out := make([]eventengine.Position, len(f.positions))
	copy(out, f.positions)
	return out
}

func (f *fakeTickRiskExecutor) SelectSLTPExit(side entity.OrderSide, stopLossPrice, takeProfitPrice, barLow, barHigh float64) (float64, string, bool) {
	switch side {
	case entity.OrderSideBuy:
		slHit := barLow <= stopLossPrice
		tpHit := barHigh >= takeProfitPrice
		if slHit && tpHit {
			return stopLossPrice, "stop_loss", true
		}
		if slHit {
			return stopLossPrice, "stop_loss", true
		}
		if tpHit {
			return takeProfitPrice, "take_profit", true
		}
	case entity.OrderSideSell:
		slHit := barHigh >= stopLossPrice
		tpHit := barLow <= takeProfitPrice
		if slHit && tpHit {
			return stopLossPrice, "stop_loss", true
		}
		if slHit {
			return stopLossPrice, "stop_loss", true
		}
		if tpHit {
			return takeProfitPrice, "take_profit", true
		}
	}
	return 0, "", false
}

func (f *fakeTickRiskExecutor) Close(positionID int64, signalPrice float64, reason string, timestamp int64) (entity.OrderEvent, *entity.BacktestTradeRecord, error) {
	f.closedIDs = append(f.closedIDs, positionID)
	filtered := make([]eventengine.Position, 0, len(f.positions))
	for _, p := range f.positions {
		if p.PositionID != positionID {
			filtered = append(filtered, p)
		}
	}
	f.positions = filtered
	return entity.OrderEvent{
		OrderID:   1,
		SymbolID:  7,
		Side:      "BUY",
		Action:    "close",
		Price:     signalPrice,
		Amount:    0.01,
		Reason:    reason,
		Timestamp: timestamp,
	}, nil, nil
}

func TestTickRiskHandler_WorstCaseStopLossOnBothHit(t *testing.T) {
	exec := &fakeTickRiskExecutor{
		positions: []eventengine.Position{
			{
				PositionID: 1,
				SymbolID:   7,
				Side:       entity.OrderSideBuy,
				EntryPrice: 100,
				Amount:     0.01,
			},
		},
	}
	handler := NewTickRiskHandler("PT15M", exec, 5, 5)

	events, err := handler.Handle(context.Background(), entity.TickEvent{
		SymbolID:   7,
		Interval:   "PT15M",
		Price:      100,
		Timestamp:  1000,
		TickType:   "high",
		ParentTime: 2000,
		BarLow:     94,  // stop-loss hit
		BarHigh:    106, // take-profit hit
	})
	if err != nil {
		t.Fatalf("handle error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 order event, got %d", len(events))
	}
	order := events[0].(entity.OrderEvent)
	if order.Reason != "stop_loss" {
		t.Fatalf("expected stop_loss by worst-case, got %s", order.Reason)
	}
}

// indicatorBookFake feeds IndicatorHandler with a fixed sequence of
// orderbooks keyed by timestamp. It returns the most recent ob whose
// timestamp <= ts, mirroring OrderbookReplay's contract.
type indicatorBookFake struct {
	books []entity.Orderbook // ascending by timestamp
}

func (f *indicatorBookFake) LatestBefore(_ context.Context, _ int64, ts int64) (entity.Orderbook, bool, error) {
	var pick entity.Orderbook
	found := false
	for _, ob := range f.books {
		if ob.Timestamp > ts {
			break
		}
		pick = ob
		found = true
	}
	return pick, found, nil
}

func TestIndicatorHandler_AttachesMicropriceAndOFIWhenBookSourceWired(t *testing.T) {
	books := []entity.Orderbook{
		{
			Timestamp: 1_000, BestBid: 100, BestAsk: 102,
			Bids: []entity.OrderbookEntry{{Price: 100, Amount: 5}},
			Asks: []entity.OrderbookEntry{{Price: 102, Amount: 5}},
		},
		{
			Timestamp: 2_000, BestBid: 100, BestAsk: 102,
			Bids: []entity.OrderbookEntry{{Price: 100, Amount: 8}},
			Asks: []entity.OrderbookEntry{{Price: 102, Amount: 2}},
		},
	}
	src := &indicatorBookFake{books: books}

	h := NewIndicatorHandler("PT15M", "", 500)
	h.SetBookSource(src, 10_000, 60_000, 5)

	// Two primary candles to drive the indicator path. Build the minimum
	// IndicatorSet that the handler needs (it does not require warmed-up
	// SMA/RSI to attach Microprice/OFI).
	candle := entity.Candle{Open: 100, High: 102, Low: 99, Close: 101, Time: 1_000, Volume: 1}
	_, err := h.Handle(context.Background(), entity.CandleEvent{
		SymbolID:  7,
		Interval:  "PT15M",
		Candle:    candle,
		Timestamp: 1_000,
	})
	if err != nil {
		t.Fatalf("first handle: %v", err)
	}

	candle2 := entity.Candle{Open: 101, High: 103, Low: 100, Close: 102, Time: 2_000, Volume: 1}
	out, err := h.Handle(context.Background(), entity.CandleEvent{
		SymbolID:  7,
		Interval:  "PT15M",
		Candle:    candle2,
		Timestamp: 2_000,
	})
	if err != nil {
		t.Fatalf("second handle: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 indicator event, got %d", len(out))
	}
	indEv := out[0].(entity.IndicatorEvent)

	// Microprice for the second snapshot: bid-heavy (8 vs 2). Expected
	// (100*2 + 102*8) / 10 = 101.6
	if indEv.Primary.Microprice == nil {
		t.Fatal("expected Microprice to be populated")
	}
	if got := *indEv.Primary.Microprice; got < 101.59 || got > 101.61 {
		t.Fatalf("microprice = %f, want ~101.6", got)
	}

	// OFI short window: bid +3, ask -3 over 1s window with denom 10 →
	// (3 - (-3)) / 10 = 0.6
	if indEv.Primary.OFIShort == nil {
		t.Fatal("expected OFIShort to be populated")
	}
	if got := *indEv.Primary.OFIShort; got < 0.59 || got > 0.61 {
		t.Fatalf("OFIShort = %f, want ~0.6", got)
	}
}

func TestIndicatorHandler_NilBookSourceLeavesFieldsUnset(t *testing.T) {
	h := NewIndicatorHandler("PT15M", "", 500)
	candle := entity.Candle{Open: 100, High: 102, Low: 99, Close: 101, Time: 1_000, Volume: 1}
	out, err := h.Handle(context.Background(), entity.CandleEvent{
		SymbolID: 7, Interval: "PT15M", Candle: candle, Timestamp: 1_000,
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	indEv := out[0].(entity.IndicatorEvent)
	if indEv.Primary.Microprice != nil || indEv.Primary.OFIShort != nil || indEv.Primary.OFILong != nil {
		t.Fatalf("orderbook-derived fields must stay nil: mp=%v short=%v long=%v",
			indEv.Primary.Microprice, indEv.Primary.OFIShort, indEv.Primary.OFILong)
	}
}

// TestIndicatorHandler_SeedPrimary_FillsIndicatorsOnFirstLiveCandle pins down
// the live-restart fix: after SeedPrimary primes enough historical PT15M
// bars (defaults need 52 for Ichimoku Senkou B, the slowest period), the
// very first CandleEvent emitted by the live source must already produce an
// IndicatorSet with SMA/RSI/MACD populated, instead of the all-nil
// indicators that previously stranded every bar at HOLD for hours.
func TestIndicatorHandler_SeedPrimary_FillsIndicatorsOnFirstLiveCandle(t *testing.T) {
	h := NewIndicatorHandler("PT15M", "", 500)

	const symbolID int64 = 10
	const seedCount = 60 // > Ichimoku SenkouB (52) so every default-period indicator can resolve
	const intervalMs int64 = 15 * 60 * 1000
	startMs := int64(1_700_000_000_000)

	seed := make([]entity.Candle, seedCount)
	for i := 0; i < seedCount; i++ {
		// Slight upward drift so SMAShort != SMALong and the resolver can
		// classify TREND_FOLLOW; values themselves don't matter, only that
		// the indicator math has enough samples to produce non-nil output.
		price := 8000.0 + float64(i)*1.5
		seed[i] = entity.Candle{
			Open:   price,
			High:   price + 5,
			Low:    price - 5,
			Close:  price + 1,
			Volume: 1,
			Time:   startMs + int64(i)*intervalMs,
		}
	}
	h.SeedPrimary(symbolID, seed)

	live := entity.Candle{
		Open: 8050, High: 8060, Low: 8045, Close: 8055,
		Volume: 1,
		Time:   startMs + int64(seedCount)*intervalMs,
	}
	out, err := h.Handle(context.Background(), entity.CandleEvent{
		SymbolID:  symbolID,
		Interval:  "PT15M",
		Candle:    live,
		Timestamp: live.Time,
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 indicator event, got %d", len(out))
	}
	indEv := out[0].(entity.IndicatorEvent)
	if indEv.Primary.SMAShort == nil {
		t.Error("SMAShort must be non-nil after seeding ~30 bars (was nil)")
	}
	if indEv.Primary.SMALong == nil {
		t.Error("SMALong must be non-nil after seeding ~30 bars (was nil)")
	}
	if indEv.Primary.RSI == nil {
		t.Error("RSI must be non-nil after seeding ~30 bars (was nil)")
	}
	if indEv.Primary.MACDLine == nil {
		t.Error("MACDLine must be non-nil after seeding ~30 bars (was nil)")
	}
	if indEv.Primary.BBUpper == nil {
		t.Error("BBUpper must be non-nil after seeding ~30 bars (was nil)")
	}
	if indEv.Primary.ATR == nil {
		t.Error("ATR must be non-nil after seeding ~30 bars (was nil)")
	}
}

// TestIndicatorHandler_SeedPrimary_NopOnEmpty guards against a no-op call
// path corrupting the buffer state.
func TestIndicatorHandler_SeedPrimary_NopOnEmpty(t *testing.T) {
	h := NewIndicatorHandler("PT15M", "", 500)
	h.SeedPrimary(7, nil)
	h.SeedPrimary(7, []entity.Candle{})

	out, err := h.Handle(context.Background(), entity.CandleEvent{
		SymbolID: 7, Interval: "PT15M",
		Candle:    entity.Candle{Open: 100, High: 101, Low: 99, Close: 100, Time: 1_000, Volume: 1},
		Timestamp: 1_000,
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 indicator event, got %d", len(out))
	}
	indEv := out[0].(entity.IndicatorEvent)
	if indEv.Primary.SMAShort != nil {
		t.Errorf("SMAShort should still be nil after no-op seed + 1 candle, got %v", *indEv.Primary.SMAShort)
	}
}
