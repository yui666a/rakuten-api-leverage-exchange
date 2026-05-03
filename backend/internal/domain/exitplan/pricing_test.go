package exitplan

import (
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

func TestExitPlan_CurrentSLPrice_long(t *testing.T) {
	cases := []struct {
		name   string
		policy risk.RiskPolicy
		atr    float64
		entry  float64
		wantSL float64
	}{
		{
			"percent only — long",
			risk.RiskPolicy{
				StopLoss:   risk.StopLossSpec{Percent: 1.5},
				TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
				Trailing:   risk.TrailingSpec{Mode: risk.TrailingModePercent},
			},
			0,
			10000,
			9850, // 10000 - 150
		},
		{
			"ATR mode, ATR wins — long",
			risk.RiskPolicy{
				StopLoss:   risk.StopLossSpec{Percent: 1.0, ATRMultiplier: 2.0},
				TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
				Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
			},
			100, // ATR distance = 200, percent distance = 100, ATR wins
			10000,
			9800,
		},
		{
			"ATR mode but ATR=0 fallback to percent — long",
			risk.RiskPolicy{
				StopLoss:   risk.StopLossSpec{Percent: 1.5, ATRMultiplier: 2.0},
				TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
				Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
			},
			0,
			10000,
			9850,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := New(NewInput{
				PositionID: 1, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: tc.entry,
				Policy: tc.policy, CreatedAt: 1,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			got := plan.CurrentSLPrice(tc.atr)
			if got != tc.wantSL {
				t.Errorf("CurrentSLPrice = %v, want %v", got, tc.wantSL)
			}
		})
	}
}

func TestExitPlan_CurrentSLPrice_short(t *testing.T) {
	plan := mustNewForTest(t, 1, entity.OrderSideSell, 10000)
	if got := plan.CurrentSLPrice(0); got != 10150 {
		t.Errorf("short SL with ATR=0: %v, want 10150", got)
	}
	if got := plan.CurrentSLPrice(100); got != 10200 {
		t.Errorf("short SL with ATR=100: %v, want 10200", got)
	}
}

func TestExitPlan_CurrentTPPrice(t *testing.T) {
	long := mustNewForTest(t, 1, entity.OrderSideBuy, 10000)
	if got := long.CurrentTPPrice(); got != 10300 {
		t.Errorf("long TP: %v, want 10300", got)
	}
	short := mustNewForTest(t, 2, entity.OrderSideSell, 10000)
	if got := short.CurrentTPPrice(); got != 9700 {
		t.Errorf("short TP: %v, want 9700", got)
	}
}

func TestExitPlan_CurrentTPPrice_disabled(t *testing.T) {
	policy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 1.5},
		TakeProfit: risk.TakeProfitSpec{Percent: 0},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeDisabled},
	}
	_, err := New(NewInput{
		PositionID: 1, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000,
		Policy: policy, CreatedAt: 1,
	})
	if err == nil {
		t.Errorf("expected validation error for TP=0")
	}
}

func TestExitPlan_CurrentTrailingTriggerPrice(t *testing.T) {
	plan := mustNewForTest(t, 1, entity.OrderSideBuy, 10000)
	if got := plan.CurrentTrailingTriggerPrice(0); got != nil {
		t.Errorf("unactivated should return nil, got %v", *got)
	}
	plan.RaiseTrailingHWM(10250, 100)
	got := plan.CurrentTrailingTriggerPrice(0)
	if got == nil {
		t.Fatal("activated should return non-nil")
	}
	if *got != 10100 {
		t.Errorf("trigger = %v, want 10100", *got)
	}
	got = plan.CurrentTrailingTriggerPrice(100)
	if *got != 10000 {
		t.Errorf("trigger with ATR: %v, want 10000", *got)
	}
}

func TestExitPlan_CurrentTrailingTriggerPrice_short(t *testing.T) {
	plan := mustNewForTest(t, 1, entity.OrderSideSell, 10000)
	plan.RaiseTrailingHWM(9800, 100) // ショート Activation
	got := plan.CurrentTrailingTriggerPrice(0)
	if got == nil {
		t.Fatal("activated short should return non-nil")
	}
	// percent distance = 150 → trigger = HWM (9800) + 150 = 9950
	if *got != 9950 {
		t.Errorf("short trigger = %v, want 9950", *got)
	}
}

func TestExitPlan_CurrentTrailingTriggerPrice_disabled(t *testing.T) {
	policy := risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 1.5},
		TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeDisabled},
	}
	plan, err := New(NewInput{
		PositionID: 1, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000,
		Policy: policy, CreatedAt: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plan.RaiseTrailingHWM(10250, 100)
	if got := plan.CurrentTrailingTriggerPrice(0); got != nil {
		t.Errorf("disabled trailing should return nil")
	}
}
