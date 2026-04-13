package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type mockStanceResolver struct {
	result StanceResult
}

func (m *mockStanceResolver) Resolve(ctx context.Context, indicators entity.IndicatorSet) StanceResult {
	return m.result
}

func TestStrategyEngine_TrendFollow_BuySignal(t *testing.T) {
	// TREND_FOLLOW: SMA20 > SMA50 かつ RSI < 70 → BUY
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(55),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY, got %s", signal.Action)
	}
}

func TestStrategyEngine_TrendFollow_SellSignal(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "downtrend",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(4900000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(45),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 4900000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionSell {
		t.Fatalf("expected SELL, got %s", signal.Action)
	}
}

func TestStrategyEngine_TrendFollow_HoldOnOverbought(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(75),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD, got %s", signal.Action)
	}
}

func TestStrategyEngine_Contrarian_BuyOnOversold(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceContrarian,
			Reasoning: "oversold bounce expected",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(4900000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(25),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 4900000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY on oversold contrarian, got %s", signal.Action)
	}
}

func TestStrategyEngine_Contrarian_SellOnOverbought(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceContrarian,
			Reasoning: "overbought reversal",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(75),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionSell {
		t.Fatalf("expected SELL on overbought contrarian, got %s", signal.Action)
	}
}

func TestStrategyEngine_Contrarian_HoldInNeutral(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceContrarian,
			Reasoning: "range bound",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5000000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(50),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5000000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD, got %s", signal.Action)
	}
}

func TestStrategyEngine_HoldStance_AlwaysHold(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceHold,
			Reasoning: "uncertain market",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000),
		SMA50:    ptr(5000000),
		RSI14:    ptr(55),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD for HOLD stance, got %s", signal.Action)
	}
}

func TestStrategyEngine_InsufficientIndicators_Hold(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5000000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD for insufficient indicators, got %s", signal.Action)
	}
}

func TestStrategyEngine_TrendFollow_HoldWhenMACDAgainst(t *testing.T) {
	// SMA20 > SMA50 (uptrend) but histogram negative → HOLD
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(5100000),
		SMA50:     ptr(5000000),
		RSI14:     ptr(55.0),
		Histogram: ptr(-5.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when MACD histogram is against buy, got %s", signal.Action)
	}
	expected := "trend follow: MACD histogram negative, skipping buy"
	if signal.Reason != expected {
		t.Fatalf("expected reason %q, got %q", expected, signal.Reason)
	}
}

func TestStrategyEngine_TrendFollow_SellBlockedByPositiveHistogram(t *testing.T) {
	// SMA20 < SMA50 (downtrend) but histogram positive → HOLD
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "downtrend",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(4900000),
		SMA50:     ptr(5000000),
		RSI14:     ptr(45.0),
		Histogram: ptr(5.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 4900000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when MACD histogram is against sell, got %s", signal.Action)
	}
	expected := "trend follow: MACD histogram positive, skipping sell"
	if signal.Reason != expected {
		t.Fatalf("expected reason %q, got %q", expected, signal.Reason)
	}
}

func TestStrategyEngine_TrendFollow_BuyWithMACDConfirmation(t *testing.T) {
	// uptrend + positive histogram → BUY
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(5100000),
		SMA50:     ptr(5000000),
		RSI14:     ptr(55.0),
		Histogram: ptr(3.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY with MACD confirmation, got %s", signal.Action)
	}
	expected := "trend follow: SMA20 > SMA50, RSI not overbought, MACD confirmed"
	if signal.Reason != expected {
		t.Fatalf("expected reason %q, got %q", expected, signal.Reason)
	}
}

func TestStrategyEngine_Contrarian_HoldWhenMACDAgainst(t *testing.T) {
	// RSI < 30 but histogram < -10 → HOLD
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceContrarian,
			Reasoning: "oversold bounce expected",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(4900000),
		SMA50:     ptr(5000000),
		RSI14:     ptr(25.0),
		Histogram: ptr(-15.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 4900000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when MACD momentum strongly negative, got %s", signal.Action)
	}
	expected := "contrarian: RSI oversold but MACD momentum still strongly negative"
	if signal.Reason != expected {
		t.Fatalf("expected reason %q, got %q", expected, signal.Reason)
	}
}

func TestStrategyEngine_TrendFollow_NilHistogramStillTrades(t *testing.T) {
	// histogram nil → BUY (backward compat)
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(5100000),
		SMA50:     ptr(5000000),
		RSI14:     ptr(55.0),
		Histogram: nil,
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY with nil histogram (backward compat), got %s", signal.Action)
	}
}
