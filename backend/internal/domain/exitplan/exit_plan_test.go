package exitplan

import (
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

func TestNew_validInputs(t *testing.T) {
	policy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 1.5, ATRMultiplier: 2.0},
		TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
	}
	plan, err := New(NewInput{
		PositionID: 100,
		SymbolID:   7,
		Side:       entity.OrderSideBuy,
		EntryPrice: 10000,
		Policy:     policy,
		CreatedAt:  1700000000000,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if plan.PositionID != 100 || plan.SymbolID != 7 || plan.Side != entity.OrderSideBuy {
		t.Errorf("identity fields wrong: %+v", plan)
	}
	if plan.EntryPrice != 10000 {
		t.Errorf("EntryPrice = %v, want 10000", plan.EntryPrice)
	}
	if plan.Policy.StopLoss.Percent != 1.5 {
		t.Errorf("policy not embedded: %+v", plan.Policy)
	}
	if plan.TrailingActivated {
		t.Errorf("TrailingActivated should default false")
	}
	if plan.TrailingHWM != nil {
		t.Errorf("TrailingHWM should default nil; got %v", *plan.TrailingHWM)
	}
	if plan.ClosedAt != nil {
		t.Errorf("ClosedAt should default nil")
	}
}

func TestNew_validation(t *testing.T) {
	validPolicy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 1.5},
		TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeDisabled},
	}
	cases := []struct {
		name    string
		input   NewInput
		wantErr string
	}{
		{
			"zero PositionID",
			NewInput{PositionID: 0, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000, Policy: validPolicy, CreatedAt: 1},
			"PositionID must be > 0",
		},
		{
			"zero SymbolID",
			NewInput{PositionID: 1, SymbolID: 0, Side: entity.OrderSideBuy, EntryPrice: 10000, Policy: validPolicy, CreatedAt: 1},
			"SymbolID must be > 0",
		},
		{
			"empty Side",
			NewInput{PositionID: 1, SymbolID: 7, Side: "", EntryPrice: 10000, Policy: validPolicy, CreatedAt: 1},
			"Side must be BUY or SELL",
		},
		{
			"non-positive EntryPrice",
			NewInput{PositionID: 1, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 0, Policy: validPolicy, CreatedAt: 1},
			"EntryPrice must be > 0",
		},
		{
			"invalid policy",
			NewInput{PositionID: 1, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000, Policy: risk.RiskPolicy{}, CreatedAt: 1},
			"invalid policy",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.input)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strContains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestExitPlan_RaiseTrailingHWM_long(t *testing.T) {
	plan := mustNewForTest(t, 100, entity.OrderSideBuy, 10000)
	if changed := plan.RaiseTrailingHWM(9990, 1); changed {
		t.Errorf("loss-side tick should not raise HWM")
	}
	if plan.TrailingActivated || plan.TrailingHWM != nil {
		t.Errorf("HWM must remain unactivated; got activated=%v hwm=%+v", plan.TrailingActivated, plan.TrailingHWM)
	}
	if changed := plan.RaiseTrailingHWM(10050, 2); !changed {
		t.Errorf("first profit tick should activate HWM")
	}
	if !plan.TrailingActivated || plan.TrailingHWM == nil || *plan.TrailingHWM != 10050 {
		t.Errorf("activation wrong: activated=%v hwm=%+v", plan.TrailingActivated, plan.TrailingHWM)
	}
	if plan.UpdatedAt != 2 {
		t.Errorf("UpdatedAt not refreshed; got %v", plan.UpdatedAt)
	}
	if changed := plan.RaiseTrailingHWM(10100, 3); !changed {
		t.Errorf("higher high should update HWM")
	}
	if *plan.TrailingHWM != 10100 {
		t.Errorf("HWM = %v, want 10100", *plan.TrailingHWM)
	}
	if changed := plan.RaiseTrailingHWM(10080, 4); changed {
		t.Errorf("lower tick should not change HWM")
	}
	if *plan.TrailingHWM != 10100 {
		t.Errorf("HWM regressed: %v", *plan.TrailingHWM)
	}
}

func TestExitPlan_RaiseTrailingHWM_short(t *testing.T) {
	plan := mustNewForTest(t, 100, entity.OrderSideSell, 10000)
	if changed := plan.RaiseTrailingHWM(10020, 1); changed {
		t.Errorf("short loss-side tick should not raise HWM")
	}
	if changed := plan.RaiseTrailingHWM(9950, 2); !changed {
		t.Errorf("short first profit should activate HWM")
	}
	if !plan.TrailingActivated || plan.TrailingHWM == nil || *plan.TrailingHWM != 9950 {
		t.Errorf("short activation wrong: %+v", plan)
	}
	if changed := plan.RaiseTrailingHWM(9900, 3); !changed {
		t.Errorf("lower low should update short HWM")
	}
	if *plan.TrailingHWM != 9900 {
		t.Errorf("short HWM = %v, want 9900", *plan.TrailingHWM)
	}
}

func TestExitPlan_Close_invariant(t *testing.T) {
	plan := mustNewForTest(t, 100, entity.OrderSideBuy, 10000)
	if err := plan.Close(1700000000999); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if plan.ClosedAt == nil || *plan.ClosedAt != 1700000000999 {
		t.Errorf("ClosedAt = %+v, want 1700000000999", plan.ClosedAt)
	}
	if err := plan.Close(1700000001000); err == nil {
		t.Errorf("second Close should error")
	}
	if plan.RaiseTrailingHWM(10050, 1700000001001) {
		t.Errorf("RaiseTrailingHWM on closed plan should be no-op")
	}
}

// --- helpers ---

func mustNewForTest(t *testing.T, posID int64, side entity.OrderSide, entry float64) *ExitPlan {
	t.Helper()
	policy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 1.5, ATRMultiplier: 2.0},
		TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
	}
	plan, err := New(NewInput{
		PositionID: posID,
		SymbolID:   7,
		Side:       side,
		EntryPrice: entry,
		Policy:     policy,
		CreatedAt:  1700000000000,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return plan
}

func strContains(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
