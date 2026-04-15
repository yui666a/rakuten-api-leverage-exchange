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

func (m *mockStanceResolver) Resolve(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64) StanceResult {
	return m.result
}

func (m *mockStanceResolver) ResolveAt(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64, now time.Time) StanceResult {
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

func TestStrategyEngine_Confidence_TrendFollowStrong(t *testing.T) {
	// Strong uptrend: EMA and SMA divergent, RSI 55, histogram +5 → high confidence
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceTrendFollow, Reasoning: "uptrend", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(5100000.0), // 2% above SMA50
		SMA50:     ptr(5000000.0),
		EMA12:     ptr(5120000.0), // ~2.4% above EMA26
		EMA26:     ptr(5000000.0),
		RSI14:     ptr(55.0),
		Histogram: ptr(5.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY, got %s (reason: %s)", signal.Action, signal.Reason)
	}
	// EMA divergence: min(2.4, 2.0)/2.0 = 1.0 * 0.3 = 0.3
	// SMA divergence: min(2.0, 2.0)/2.0 = 1.0 * 0.15 = 0.15
	// RSI room: (70-55)/40 = 0.375 * 0.25 = 0.09375
	// MACD confirm: min(5/10, 1.0) = 0.5 * 0.3 = 0.15
	// Total: ~0.69
	if signal.Confidence < 0.6 || signal.Confidence > 0.8 {
		t.Fatalf("expected confidence ~0.69, got %.4f", signal.Confidence)
	}
}

func TestStrategyEngine_Confidence_TrendFollowWeak(t *testing.T) {
	// Weak uptrend: EMA/SMA barely crossing, RSI 68, no histogram
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceTrendFollow, Reasoning: "uptrend", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(5005000.0), // 0.1% above SMA50
		SMA50:     ptr(5000000.0),
		EMA12:     ptr(5003000.0), // 0.06% above EMA26
		EMA26:     ptr(5000000.0),
		RSI14:     ptr(68.0),
		Histogram: nil,
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5005000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY, got %s (reason: %s)", signal.Action, signal.Reason)
	}
	// EMA divergence: min(0.06, 2.0)/2.0 = 0.03 * 0.3 = 0.009
	// SMA divergence: min(0.1, 2.0)/2.0 = 0.05 * 0.15 = 0.0075
	// RSI room: (70-68)/40 = 0.05 * 0.25 = 0.0125
	// MACD confirm: nil → 0.5 * 0.3 = 0.15
	// Total: ~0.179
	if signal.Confidence > 0.25 {
		t.Fatalf("expected low confidence (<0.25), got %.4f", signal.Confidence)
	}
}

func TestStrategyEngine_EMA_CrossWithSMAMisalignment(t *testing.T) {
	// EMA12 > EMA26 (bullish) but SMA20 < SMA50 (SMA not aligned) → HOLD
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceTrendFollow, Reasoning: "uptrend", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(4990000.0), // SMA still bearish
		SMA50:    ptr(5000000.0),
		EMA12:    ptr(5010000.0), // EMA already bullish
		EMA26:    ptr(5000000.0),
		RSI14:    ptr(55.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5010000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when EMA crossed but SMA not aligned, got %s (reason: %s)", signal.Action, signal.Reason)
	}
}

func TestStrategyEngine_EMA_FallbackToSMAWhenNil(t *testing.T) {
	// EMA nil → falls back to SMA-only evaluation
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceTrendFollow, Reasoning: "uptrend", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(5100000.0),
		SMA50:     ptr(5000000.0),
		EMA12:     nil, // no EMA data
		EMA26:     nil,
		RSI14:     ptr(55.0),
		Histogram: ptr(3.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY with SMA fallback, got %s", signal.Action)
	}
}

func TestStrategyEngine_Confidence_ContrarianStrong(t *testing.T) {
	// Deep oversold: RSI 15, histogram mildly negative (-3)
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceContrarian, Reasoning: "oversold", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(4900000.0),
		SMA50:     ptr(5000000.0),
		RSI14:     ptr(15.0),
		Histogram: ptr(-3.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 4900000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY, got %s", signal.Action)
	}
	// RSI extreme: (30-15)/30 = 0.5 * 0.6 = 0.3
	// MACD not against: 1.0 - min(3/20, 1.0) = 0.85 * 0.4 = 0.34
	// Total: 0.64
	if signal.Confidence < 0.55 || signal.Confidence > 0.75 {
		t.Fatalf("expected confidence ~0.64, got %.4f", signal.Confidence)
	}
}

func TestStrategyEngine_MTF_BuyBlockedByHigherDowntrend(t *testing.T) {
	// PT15M says BUY, but PT1H SMA20 < SMA50 (higher timeframe downtrend) → HOLD
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceTrendFollow, Reasoning: "uptrend", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(5100000.0),
		SMA50:     ptr(5000000.0),
		RSI14:     ptr(55.0),
		Histogram: ptr(3.0),
	}
	higherTF := &entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(4900000.0), // downtrend on higher TF
		SMA50:    ptr(5000000.0),
	}
	signal, err := engine.EvaluateWithHigherTF(context.Background(), indicators, higherTF, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when higher TF is downtrend but signal is BUY, got %s (reason: %s)", signal.Action, signal.Reason)
	}
}

func TestStrategyEngine_MTF_SellBlockedByHigherUptrend(t *testing.T) {
	// PT15M says SELL, but PT1H SMA20 > SMA50 (higher timeframe uptrend) → HOLD
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceTrendFollow, Reasoning: "downtrend", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(4900000.0),
		SMA50:     ptr(5000000.0),
		RSI14:     ptr(45.0),
		Histogram: ptr(-3.0),
	}
	higherTF := &entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000.0), // uptrend on higher TF
		SMA50:    ptr(5000000.0),
	}
	signal, err := engine.EvaluateWithHigherTF(context.Background(), indicators, higherTF, 4900000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD when higher TF is uptrend but signal is SELL, got %s (reason: %s)", signal.Action, signal.Reason)
	}
}

func TestStrategyEngine_MTF_BuyAlignedWithHigherUptrend(t *testing.T) {
	// PT15M says BUY, PT1H also uptrend → BUY with boosted confidence
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceTrendFollow, Reasoning: "uptrend", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(5100000.0),
		SMA50:     ptr(5000000.0),
		RSI14:     ptr(55.0),
		Histogram: ptr(5.0),
	}
	higherTF := &entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5200000.0), // uptrend on higher TF too
		SMA50:    ptr(5000000.0),
	}

	signalWithMTF, err := engine.EvaluateWithHigherTF(context.Background(), indicators, higherTF, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signalWithMTF.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY, got %s", signalWithMTF.Action)
	}

	// Compare: without higher TF
	signalBase, _ := engine.Evaluate(context.Background(), indicators, 5100000)
	if signalWithMTF.Confidence <= signalBase.Confidence {
		t.Fatalf("expected MTF-aligned confidence (%.4f) > base confidence (%.4f)", signalWithMTF.Confidence, signalBase.Confidence)
	}
}

func TestStrategyEngine_MTF_NilHigherTFFallsBack(t *testing.T) {
	// nil higherTF → behaves same as Evaluate()
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceTrendFollow, Reasoning: "uptrend", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(5100000.0),
		SMA50:     ptr(5000000.0),
		RSI14:     ptr(55.0),
		Histogram: ptr(3.0),
	}
	signal, err := engine.EvaluateWithHigherTF(context.Background(), indicators, nil, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY with nil higherTF, got %s", signal.Action)
	}
}

func TestStrategyEngine_MTF_ContrarianNotFiltered(t *testing.T) {
	// Contrarian signals are NOT filtered by higher TF (they're intentionally counter-trend)
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceContrarian, Reasoning: "oversold", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(4900000.0),
		SMA50:     ptr(5000000.0),
		RSI14:     ptr(25.0),
		Histogram: ptr(-3.0),
	}
	higherTF := &entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(4800000.0), // downtrend on higher TF
		SMA50:    ptr(5000000.0),
	}
	signal, err := engine.EvaluateWithHigherTF(context.Background(), indicators, higherTF, 4900000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected contrarian BUY even with higher TF downtrend, got %s", signal.Action)
	}
}

func TestStrategyEngine_VolatilityFilter_SqueezeBlocksTrendFollow(t *testing.T) {
	// BB bandwidth < 0.02 during trend follow → HOLD
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceTrendFollow, Reasoning: "uptrend", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:    7,
		SMA20:       ptr(5100000.0),
		SMA50:       ptr(5000000.0),
		RSI14:       ptr(55.0),
		Histogram:   ptr(3.0),
		BBBandwidth: ptr(0.015), // squeeze
	}
	signal, err := engine.EvaluateWithHigherTF(context.Background(), indicators, nil, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD during BB squeeze, got %s", signal.Action)
	}
	if signal.Reason != "volatility filter: Bollinger squeeze, trend signal unreliable" {
		t.Fatalf("unexpected reason: %s", signal.Reason)
	}
}

func TestStrategyEngine_VolatilityFilter_NormalBandwidthAllowsTrade(t *testing.T) {
	// BB bandwidth >= 0.02 → normal trading
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceTrendFollow, Reasoning: "uptrend", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:    7,
		SMA20:       ptr(5100000.0),
		SMA50:       ptr(5000000.0),
		RSI14:       ptr(55.0),
		Histogram:   ptr(3.0),
		BBBandwidth: ptr(0.05), // normal volatility
	}
	signal, err := engine.EvaluateWithHigherTF(context.Background(), indicators, nil, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY with normal bandwidth, got %s", signal.Action)
	}
}

func TestStrategyEngine_BB_ContrarianBuyAtLowerBandBoost(t *testing.T) {
	// Contrarian BUY with price at lower BB → confidence boosted
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceContrarian, Reasoning: "oversold", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(4900000.0),
		SMA50:     ptr(5000000.0),
		RSI14:     ptr(25.0),
		Histogram: ptr(-3.0),
		BBUpper:   ptr(5200000.0),
		BBLower:   ptr(4850000.0),
	}
	// Price at lower band
	signal, err := engine.EvaluateWithHigherTF(context.Background(), indicators, nil, 4850000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY, got %s", signal.Action)
	}

	// Compare with same signal without BB
	indicatorsNoBB := indicators
	indicatorsNoBB.BBLower = nil
	indicatorsNoBB.BBUpper = nil
	signalNoBB, _ := engine.EvaluateWithHigherTF(context.Background(), indicatorsNoBB, nil, 4850000)
	if signal.Confidence <= signalNoBB.Confidence {
		t.Fatalf("expected BB-boosted confidence (%.4f) > base (%.4f)", signal.Confidence, signalNoBB.Confidence)
	}
}

func TestStrategyEngine_Confidence_HoldIsZero(t *testing.T) {
	// HOLD signals should have 0.0 confidence
	resolver := &mockStanceResolver{
		result: StanceResult{Stance: entity.MarketStanceHold, Reasoning: "uncertain", Source: "rule-based", UpdatedAt: time.Now().Unix()},
	}
	engine := NewStrategyEngine(resolver)

	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    ptr(5100000.0),
		SMA50:    ptr(5000000.0),
		RSI14:    ptr(55.0),
	}
	signal, err := engine.Evaluate(context.Background(), indicators, 5100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD, got %s", signal.Action)
	}
	if signal.Confidence != 0.0 {
		t.Fatalf("expected confidence 0.0 for HOLD, got %.4f", signal.Confidence)
	}
}

func TestStrategyEngine_EvaluateAt_UsesInjectedTimestamp(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)
	at := time.Date(2026, 4, 14, 12, 30, 0, 0, time.UTC)

	signal, err := engine.EvaluateAt(context.Background(), entity.IndicatorSet{SymbolID: 7}, 5000000, at)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Timestamp != at.Unix() {
		t.Fatalf("expected timestamp %d, got %d", at.Unix(), signal.Timestamp)
	}
}

func TestStrategyEngine_EvaluateWithHigherTFAt_UsesInjectedTimestampForMTFHold(t *testing.T) {
	resolver := &mockStanceResolver{
		result: StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			Source:    "rule-based",
			UpdatedAt: time.Now().Unix(),
		},
	}
	engine := NewStrategyEngine(resolver)
	at := time.Date(2026, 4, 14, 13, 45, 0, 0, time.UTC)

	indicators := entity.IndicatorSet{
		SymbolID:  7,
		SMA20:     ptr(5100000),
		SMA50:     ptr(5000000),
		EMA12:     ptr(101),
		EMA26:     ptr(100),
		RSI14:     ptr(55),
		Histogram: ptr(2),
	}
	higherTF := &entity.IndicatorSet{
		SMA20: ptr(5000000),
		SMA50: ptr(5100000), // downtrend blocks buy
	}

	signal, err := engine.EvaluateWithHigherTFAt(context.Background(), indicators, higherTF, 5100000, at)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signal.Action != entity.SignalActionHold {
		t.Fatalf("expected HOLD by MTF filter, got %s", signal.Action)
	}
	if signal.Timestamp != at.Unix() {
		t.Fatalf("expected timestamp %d, got %d", at.Unix(), signal.Timestamp)
	}
}
