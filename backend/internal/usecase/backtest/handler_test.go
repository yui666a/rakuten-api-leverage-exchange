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
