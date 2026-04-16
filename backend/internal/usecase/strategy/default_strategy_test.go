package strategy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// stubStanceResolver returns a fixed StanceResult for every call. It mirrors
// the pattern used by the existing usecase.mockStanceResolver but lives in the
// strategy package so we can instantiate a real *usecase.StrategyEngine
// without touching unexported internals.
type stubStanceResolver struct {
	result usecase.StanceResult
}

func (s *stubStanceResolver) Resolve(
	ctx context.Context,
	indicators entity.IndicatorSet,
	lastPrice float64,
) usecase.StanceResult {
	return s.result
}

func (s *stubStanceResolver) ResolveAt(
	ctx context.Context,
	indicators entity.IndicatorSet,
	lastPrice float64,
	now time.Time,
) usecase.StanceResult {
	return s.result
}

func floatPtr(v float64) *float64 { return &v }

func TestDefaultStrategy_Name(t *testing.T) {
	engine := usecase.NewStrategyEngine(&stubStanceResolver{})
	s := NewDefaultStrategy(engine)
	if got := s.Name(); got != DefaultStrategyName {
		t.Errorf("Name() = %q, want %q", got, DefaultStrategyName)
	}
}

// TestDefaultStrategy_Evaluate_DelegatesToEngine verifies that the wrapper
// produces the same Signal as the underlying StrategyEngine for identical
// inputs. This guarantees behavioural equivalence between calling
// StrategyEngine.EvaluateWithHigherTFAt directly and via the port.
func TestDefaultStrategy_Evaluate_DelegatesToEngine(t *testing.T) {
	resolver := &stubStanceResolver{
		result: usecase.StanceResult{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			Source:    "rule-based",
		},
	}
	engine := usecase.NewStrategyEngine(resolver)
	wrapped := NewDefaultStrategy(engine)

	// Minimal indicator set that drives StrategyEngine into a BUY decision
	// under TREND_FOLLOW: SMA20 > SMA50, RSI < 70, EMA aligned with SMA.
	indicators := entity.IndicatorSet{
		SymbolID: 7,
		SMA20:    floatPtr(5_100_000),
		SMA50:    floatPtr(5_000_000),
		EMA12:    floatPtr(5_100_000),
		EMA26:    floatPtr(5_000_000),
		RSI14:    floatPtr(55),
	}
	lastPrice := 5_100_000.0
	now := time.Unix(1_700_000_000, 0)

	direct, err := engine.EvaluateWithHigherTFAt(
		context.Background(),
		indicators,
		nil,
		lastPrice,
		now,
	)
	if err != nil {
		t.Fatalf("engine direct call failed: %v", err)
	}

	viaPort, err := wrapped.Evaluate(
		context.Background(),
		&indicators,
		nil,
		lastPrice,
		now,
	)
	if err != nil {
		t.Fatalf("wrapped Evaluate failed: %v", err)
	}

	if viaPort == nil || direct == nil {
		t.Fatalf("expected non-nil signals, got direct=%v viaPort=%v", direct, viaPort)
	}
	if viaPort.Action != direct.Action {
		t.Errorf("Action mismatch: direct=%s wrapped=%s", direct.Action, viaPort.Action)
	}
	if viaPort.SymbolID != direct.SymbolID {
		t.Errorf("SymbolID mismatch: direct=%d wrapped=%d", direct.SymbolID, viaPort.SymbolID)
	}
	if viaPort.Reason != direct.Reason {
		t.Errorf("Reason mismatch: direct=%q wrapped=%q", direct.Reason, viaPort.Reason)
	}
	if viaPort.Confidence != direct.Confidence {
		t.Errorf("Confidence mismatch: direct=%v wrapped=%v", direct.Confidence, viaPort.Confidence)
	}
	if viaPort.Timestamp != direct.Timestamp {
		t.Errorf("Timestamp mismatch: direct=%d wrapped=%d", direct.Timestamp, viaPort.Timestamp)
	}
	// Sanity: our fabricated TREND_FOLLOW inputs should produce a BUY so we
	// know the test actually exercised the engine and didn't just match a
	// trivial HOLD.
	if viaPort.Action != entity.SignalActionBuy {
		t.Fatalf("expected BUY signal, got %s (reason=%q)", viaPort.Action, viaPort.Reason)
	}
}

func TestDefaultStrategy_Evaluate_NilIndicators(t *testing.T) {
	engine := usecase.NewStrategyEngine(&stubStanceResolver{})
	s := NewDefaultStrategy(engine)

	signal, err := s.Evaluate(
		context.Background(),
		nil, // nil indicators
		nil,
		0,
		time.Now(),
	)
	if err == nil {
		t.Fatal("expected error for nil indicators, got nil")
	}
	if !errors.Is(err, ErrIndicatorsRequired) {
		t.Errorf("expected ErrIndicatorsRequired, got %v", err)
	}
	if signal != nil {
		t.Errorf("expected nil signal on error, got %+v", signal)
	}
}

func TestDefaultStrategy_Evaluate_NilEngine(t *testing.T) {
	s := NewDefaultStrategy(nil)
	indicators := entity.IndicatorSet{SymbolID: 1}
	signal, err := s.Evaluate(context.Background(), &indicators, nil, 0, time.Now())
	if err == nil {
		t.Fatal("expected error when wrapping a nil engine, got nil")
	}
	if signal != nil {
		t.Errorf("expected nil signal on error, got %+v", signal)
	}
}
