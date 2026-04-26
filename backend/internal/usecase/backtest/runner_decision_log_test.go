package backtest

import (
	"context"
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// recordingHandler captures every event the runner forwards to a registered
// decision recorder. We deliberately do NOT use the real decisionlog.Recorder
// here — that would create an import cycle. The point is to assert the runner
// attaches whatever EventHandler it was handed at the configured priorities.
type recordingHandler struct {
	seen []string
}

func (h *recordingHandler) Handle(_ context.Context, ev entity.Event) ([]entity.Event, error) {
	h.seen = append(h.seen, ev.EventType())
	return nil, nil
}

func TestRunner_WithDecisionRecorder_ForwardsBusEvents(t *testing.T) {
	primary := make([]entity.Candle, 0, 80)
	baseTime := int64(1_770_000_000_000)
	price := 100.0
	for i := 0; i < 80; i++ {
		price += math.Sin(float64(i)/7.0) * 0.8
		ts := baseTime + int64(i)*15*60*1000
		primary = append(primary, entity.Candle{
			Open:  price - 0.5,
			High:  price + 1.0,
			Low:   price - 1.0,
			Close: price,
			Time:  ts,
		})
	}

	rec := &recordingHandler{}
	runner := NewBacktestRunner(WithDecisionRecorder(rec))
	_, err := runner.Run(context.Background(), RunInput{
		Config: entity.BacktestConfig{
			Symbol:          "BTC_JPY",
			SymbolID:        7,
			PrimaryInterval: "PT15M",
			FromTimestamp:   primary[0].Time,
			ToTimestamp:     primary[len(primary)-1].Time,
			InitialBalance:  100000,
			SpreadPercent:   0.1,
		},
		RiskConfig: entity.RiskConfig{
			MaxPositionAmount: 1_000_000_000,
			MaxDailyLoss:      1_000_000_000,
			StopLossPercent:   5,
			TakeProfitPercent: 10,
			InitialCapital:    100000,
		},
		TradeAmount:    0.01,
		PrimaryCandles: primary,
	})
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if len(rec.seen) == 0 {
		t.Fatalf("recorder must receive at least one event; got 0")
	}
	sawIndicator := false
	for _, et := range rec.seen {
		if et == entity.EventTypeIndicator {
			sawIndicator = true
			break
		}
	}
	if !sawIndicator {
		t.Errorf("recorder must see at least one IndicatorEvent; got %v", rec.seen)
	}
}

func TestRunner_PreAllocatedResultID_IsHonoured(t *testing.T) {
	primary := []entity.Candle{
		{Open: 100, High: 101, Low: 99, Close: 100, Time: 1_770_000_000_000},
	}
	runner := NewBacktestRunner()
	res, err := runner.Run(context.Background(), RunInput{
		Config: entity.BacktestConfig{
			Symbol:          "BTC_JPY",
			SymbolID:        7,
			PrimaryInterval: "PT15M",
			FromTimestamp:   primary[0].Time,
			ToTimestamp:     primary[0].Time,
			InitialBalance:  100000,
		},
		RiskConfig:     entity.RiskConfig{InitialCapital: 100000, StopLossPercent: 5},
		TradeAmount:    0.01,
		PrimaryCandles: primary,
		ResultID:       "preallocated-123",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ID != "preallocated-123" {
		t.Errorf("result.ID = %q, want preallocated value", res.ID)
	}
}

func TestRunner_WithDecisionRecorder_NilDoesNotPanic(t *testing.T) {
	primary := []entity.Candle{
		{Open: 100, High: 101, Low: 99, Close: 100, Time: 1_770_000_000_000},
	}
	runner := NewBacktestRunner(WithDecisionRecorder(nil))
	_, err := runner.Run(context.Background(), RunInput{
		Config: entity.BacktestConfig{
			Symbol:          "BTC_JPY",
			SymbolID:        7,
			PrimaryInterval: "PT15M",
			FromTimestamp:   primary[0].Time,
			ToTimestamp:     primary[0].Time,
			InitialBalance:  100000,
		},
		RiskConfig:     entity.RiskConfig{InitialCapital: 100000, StopLossPercent: 5},
		TradeAmount:    0.01,
		PrimaryCandles: primary,
	})
	if err != nil {
		t.Fatalf("nil recorder should be a no-op, got error: %v", err)
	}
}
