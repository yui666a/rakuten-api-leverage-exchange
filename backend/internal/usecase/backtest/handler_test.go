package backtest

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
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
	handler := &StrategyHandler{Engine: engine}

	ts := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC).UnixMilli()
	events, err := handler.Handle(context.Background(), entity.IndicatorEvent{
		SymbolID:  7,
		Interval:  "PT15M",
		Timestamp: ts,
		LastPrice: 100,
		Primary: entity.IndicatorSet{
			SymbolID:  7,
			SMA20:     floatPtr(120),
			SMA50:     floatPtr(100),
			EMA12:     floatPtr(120),
			EMA26:     floatPtr(100),
			RSI14:     floatPtr(55),
			Histogram: floatPtr(1),
			Timestamp: ts,
		},
	})
	if err != nil {
		t.Fatalf("handle error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 signal event, got %d", len(events))
	}

	signalEvent, ok := events[0].(entity.SignalEvent)
	if !ok {
		t.Fatalf("expected SignalEvent, got %T", events[0])
	}
	if signalEvent.Signal.Timestamp != ts/1000 {
		t.Fatalf("expected signal timestamp %d, got %d", ts/1000, signalEvent.Signal.Timestamp)
	}
}

func floatPtr(v float64) *float64 { return &v }

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

	events, err := handler.Handle(context.Background(), entity.SignalEvent{
		Signal: entity.Signal{
			SymbolID: 7,
			Action:   entity.SignalActionBuy,
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
