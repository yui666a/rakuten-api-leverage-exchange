package risk

import (
	"errors"
	"strings"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestStopLossSpec_Mode(t *testing.T) {
	tests := []struct {
		name string
		spec StopLossSpec
		want StopLossMode
	}{
		{name: "no atr multiplier returns Percent", spec: StopLossSpec{Percent: 14}, want: StopLossModePercent},
		{name: "positive atr multiplier returns ATR", spec: StopLossSpec{Percent: 14, ATRMultiplier: 1.5}, want: StopLossModeATR},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.spec.Mode(); got != tc.want {
				t.Errorf("Mode() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRiskPolicy_Validate(t *testing.T) {
	valid := RiskPolicy{
		StopLoss:   StopLossSpec{Percent: 14},
		TakeProfit: TakeProfitSpec{Percent: 4},
		Trailing:   TrailingSpec{Mode: TrailingModeATR, ATRMultiplier: 2.5},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid policy returned error: %v", err)
	}

	tests := []struct {
		name      string
		policy    RiskPolicy
		wantSubst string
	}{
		{
			name: "zero stop_loss percent fails fast",
			policy: RiskPolicy{
				StopLoss:   StopLossSpec{Percent: 0},
				TakeProfit: TakeProfitSpec{Percent: 4},
				Trailing:   TrailingSpec{Mode: TrailingModePercent},
			},
			wantSubst: "stop_loss percent must be > 0",
		},
		{
			name: "negative atr multiplier fails fast",
			policy: RiskPolicy{
				StopLoss:   StopLossSpec{Percent: 14, ATRMultiplier: -1},
				TakeProfit: TakeProfitSpec{Percent: 4},
				Trailing:   TrailingSpec{Mode: TrailingModePercent},
			},
			wantSubst: "stop_loss atr_multiplier must be >= 0",
		},
		{
			name: "zero take_profit percent fails fast",
			policy: RiskPolicy{
				StopLoss:   StopLossSpec{Percent: 14},
				TakeProfit: TakeProfitSpec{Percent: 0},
				Trailing:   TrailingSpec{Mode: TrailingModePercent},
			},
			wantSubst: "take_profit percent must be > 0",
		},
		{
			name: "ATR trailing without multiplier fails fast",
			policy: RiskPolicy{
				StopLoss:   StopLossSpec{Percent: 14},
				TakeProfit: TakeProfitSpec{Percent: 4},
				Trailing:   TrailingSpec{Mode: TrailingModeATR, ATRMultiplier: 0},
			},
			wantSubst: "trailing mode=ATR requires atr_multiplier > 0",
		},
		{
			name: "unknown trailing mode fails fast",
			policy: RiskPolicy{
				StopLoss:   StopLossSpec{Percent: 14},
				TakeProfit: TakeProfitSpec{Percent: 4},
				Trailing:   TrailingSpec{Mode: TrailingMode(99)},
			},
			wantSubst: "unknown trailing mode",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Validate()
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubst) {
				t.Errorf("error %q does not contain %q", err, tc.wantSubst)
			}
		})
	}
}

func TestRiskPolicy_MustValidate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustValidate on invalid policy did not panic")
		}
	}()
	RiskPolicy{}.MustValidate()
}

func TestFromProfile_NilReturnsEnvFallbackAndSentinel(t *testing.T) {
	got, err := FromProfile(nil, 5, 2)
	if !errors.Is(err, ErrEmptyPolicy) {
		t.Errorf("err = %v, want ErrEmptyPolicy", err)
	}
	want := RiskPolicy{
		StopLoss:   StopLossSpec{Percent: 5},
		TakeProfit: TakeProfitSpec{Percent: 2},
		Trailing:   TrailingSpec{Mode: TrailingModePercent},
	}
	if got != want {
		t.Errorf("policy = %+v, want %+v", got, want)
	}
}

func TestFromProfile_ProductionLTC60kMappingProducesExpectedPolicy(t *testing.T) {
	// Mirrors backend/profiles/production_ltc_60k.json.
	profile := &entity.StrategyProfile{
		Risk: entity.StrategyRiskConfig{
			StopLossPercent:       14,
			TakeProfitPercent:     4,
			StopLossATRMultiplier: 0,
			TrailingATRMultiplier: 2.5,
		},
	}
	got, err := FromProfile(profile, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := RiskPolicy{
		StopLoss:   StopLossSpec{Percent: 14, ATRMultiplier: 0},
		TakeProfit: TakeProfitSpec{Percent: 4},
		Trailing:   TrailingSpec{Mode: TrailingModeATR, ATRMultiplier: 2.5},
	}
	if got != want {
		t.Errorf("policy = %+v, want %+v", got, want)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("policy failed validate: %v", err)
	}
}

func TestFromProfile_ProfileZerosFallBackToEnv(t *testing.T) {
	profile := &entity.StrategyProfile{
		Risk: entity.StrategyRiskConfig{
			StopLossPercent:   0, // forces env fallback
			TakeProfitPercent: 0,
		},
	}
	got, err := FromProfile(profile, 7, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.StopLoss.Percent != 7 {
		t.Errorf("stop_loss percent = %v, want 7 (env fallback)", got.StopLoss.Percent)
	}
	if got.TakeProfit.Percent != 3 {
		t.Errorf("take_profit percent = %v, want 3 (env fallback)", got.TakeProfit.Percent)
	}
}

func TestFromProfile_ZeroTrailingMapsToPercentMode(t *testing.T) {
	profile := &entity.StrategyProfile{
		Risk: entity.StrategyRiskConfig{
			StopLossPercent:       14,
			TakeProfitPercent:     4,
			TrailingATRMultiplier: 0, // legacy percent-only trailing
		},
	}
	got, err := FromProfile(profile, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Trailing.Mode != TrailingModePercent {
		t.Errorf("trailing mode = %v, want TrailingModePercent", got.Trailing.Mode)
	}
}
