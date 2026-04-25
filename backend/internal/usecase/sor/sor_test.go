package sor

import (
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestSelector_DefaultsToMarket(t *testing.T) {
	s := New(Config{})
	plan := s.Plan(SelectInput{
		SymbolID: 7, Side: entity.OrderSideBuy, Amount: 0.1,
		BestBid: 999, BestAsk: 1001,
	})
	if plan.Strategy != StrategyMarket {
		t.Fatalf("expected market strategy, got %s", plan.Strategy)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Kind != StepKindSubmit {
		t.Fatalf("expected single submit step, got %+v", plan.Steps)
	}
	if plan.Steps[0].Order.OrderData.OrderType != entity.OrderTypeMarket {
		t.Fatalf("expected MARKET, got %s", plan.Steps[0].Order.OrderData.OrderType)
	}
	if plan.Steps[0].Order.OrderData.PostOnly != nil {
		t.Fatalf("MARKET must not carry postOnly: %+v", plan.Steps[0].Order.OrderData.PostOnly)
	}
}

func TestSelector_PostOnlyEscalate_BuyPlacesBelowBestBid(t *testing.T) {
	s := New(Config{
		Strategy:         StrategyPostOnlyEscalate,
		LimitOffsetTicks: 1,
		TickSize:         0.1,
	})
	plan := s.Plan(SelectInput{
		SymbolID: 7, Side: entity.OrderSideBuy, Amount: 0.1,
		BestBid: 9000, BestAsk: 9011.3,
	})
	if plan.Strategy != StrategyPostOnlyEscalate {
		t.Fatalf("strategy: %s", plan.Strategy)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(plan.Steps))
	}
	// Step 0: post-only LIMIT at BestBid - 1 tick = 8999.9
	limit := plan.Steps[0]
	if limit.Kind != StepKindSubmit || limit.Order.OrderData.OrderType != entity.OrderTypeLimit {
		t.Fatalf("step0 not LIMIT submit: %+v", limit)
	}
	if limit.Order.OrderData.PostOnly == nil || !*limit.Order.OrderData.PostOnly {
		t.Fatalf("postOnly must be true: %+v", limit.Order.OrderData.PostOnly)
	}
	if limit.Order.OrderData.Price == nil || math.Abs(*limit.Order.OrderData.Price-8999.9) > 1e-6 {
		t.Fatalf("expected price 8999.9, got %v", limit.Order.OrderData.Price)
	}

	// Step 1: wait-or-escalate with MARKET fallback
	wait := plan.Steps[1]
	if wait.Kind != StepKindWaitOrEscalate {
		t.Fatalf("step1 kind: %s", wait.Kind)
	}
	if wait.EscalateAfterMs != DefaultEscalateAfterMs {
		t.Fatalf("expected default escalate window, got %d", wait.EscalateAfterMs)
	}
	if wait.FallbackOrder.OrderData.OrderType != entity.OrderTypeMarket {
		t.Fatalf("fallback must be MARKET, got %s", wait.FallbackOrder.OrderData.OrderType)
	}
}

func TestSelector_PostOnlyEscalate_SellPlacesAboveBestAsk(t *testing.T) {
	s := New(Config{Strategy: StrategyPostOnlyEscalate, LimitOffsetTicks: 1, TickSize: 0.1})
	plan := s.Plan(SelectInput{
		SymbolID: 7, Side: entity.OrderSideSell, Amount: 0.1,
		BestBid: 8978, BestAsk: 9011.3,
	})
	limit := plan.Steps[0]
	if limit.Order.OrderData.OrderSide != entity.OrderSideSell {
		t.Fatalf("side: %s", limit.Order.OrderData.OrderSide)
	}
	if math.Abs(*limit.Order.OrderData.Price-9011.4) > 1e-6 {
		t.Fatalf("expected price 9011.4 (BestAsk + 1 tick), got %v", *limit.Order.OrderData.Price)
	}
}

func TestSelector_PostOnlyEscalate_FallsBackToMarketWithoutTouch(t *testing.T) {
	// BestBid/BestAsk = 0 → no touch → cannot make a sane LIMIT → MARKET only
	s := New(Config{Strategy: StrategyPostOnlyEscalate})
	plan := s.Plan(SelectInput{
		SymbolID: 7, Side: entity.OrderSideBuy, Amount: 0.1,
	})
	if plan.Strategy != StrategyMarket {
		t.Fatalf("expected MARKET fallback when no touch, got %s", plan.Strategy)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(plan.Steps))
	}
}

func TestSelector_CloseInheritsPositionID(t *testing.T) {
	s := New(Config{Strategy: StrategyPostOnlyEscalate, LimitOffsetTicks: 1, TickSize: 0.1})
	posID := int64(42)
	plan := s.Plan(SelectInput{
		SymbolID: 7, Side: entity.OrderSideSell, Amount: 0.1,
		BestBid: 8978, BestAsk: 9011.3,
		PositionID: &posID,
	})
	limit := plan.Steps[0].Order.OrderData
	if limit.OrderBehavior != entity.OrderBehaviorClose {
		t.Fatalf("close behavior: %s", limit.OrderBehavior)
	}
	if limit.PositionID == nil || *limit.PositionID != 42 {
		t.Fatalf("position id: %v", limit.PositionID)
	}
	// Fallback must also carry PositionID — escalating to MARKET on a CLOSE
	// without it would be rejected by the venue.
	fb := plan.Steps[1].FallbackOrder.OrderData
	if fb.PositionID == nil || *fb.PositionID != 42 {
		t.Fatalf("fallback missing position id: %v", fb.PositionID)
	}
}

func TestSelector_MinIntervalRespectsConfig(t *testing.T) {
	s := New(Config{Strategy: StrategyMarket, MinIntervalMs: 500})
	if s.MinIntervalMs() != 500 {
		t.Fatalf("expected 500, got %d", s.MinIntervalMs())
	}
	d := New(Config{}) // default
	if d.MinIntervalMs() != DefaultMinIntervalMs {
		t.Fatalf("expected default %d, got %d", DefaultMinIntervalMs, d.MinIntervalMs())
	}
}

func TestSelector_PostOnlyEscalate_TickSizeFallback(t *testing.T) {
	s := New(Config{Strategy: StrategyPostOnlyEscalate})
	plan := s.Plan(SelectInput{
		SymbolID: 7, Side: entity.OrderSideBuy, Amount: 0.1,
		BestBid: 9000, BestAsk: 9001,
	})
	// DefaultTickSize=0.1, DefaultLimitOffsetTicks=1 → 9000 - 0.1 = 8999.9
	got := *plan.Steps[0].Order.OrderData.Price
	if math.Abs(got-8999.9) > 1e-6 {
		t.Fatalf("expected fallback tick to yield 8999.9, got %f", got)
	}
}
