package entity

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// validProfile returns a profile that passes Validate(). Individual test
// cases mutate it to exercise failure branches.
func validProfile() StrategyProfile {
	return StrategyProfile{
		Name:        "ltc_aggressive_v3",
		Description: "LTC向け攻めの短期戦略",
		Indicators: IndicatorConfig{
			SMAShort:     10,
			SMALong:      30,
			RSIPeriod:    14,
			MACDFast:     12,
			MACDSlow:     26,
			MACDSignal:   9,
			BBPeriod:     20,
			BBMultiplier: 2.0,
			ATRPeriod:    14,
		},
		StanceRules: StanceRulesConfig{
			RSIOversold:             20,
			RSIOverbought:           80,
			SMAConvergenceThreshold: 0.001,
			BBSqueezeLookback:       5,
			BreakoutVolumeRatio:     1.5,
		},
		SignalRules: SignalRulesConfig{
			TrendFollow: TrendFollowConfig{
				Enabled:            true,
				RequireMACDConfirm: true,
				RequireEMACross:    true,
				RSIBuyMax:          70,
				RSISellMin:         30,
			},
			Contrarian: ContrarianConfig{
				Enabled:            true,
				RSIEntry:           30,
				RSIExit:            70,
				MACDHistogramLimit: 10,
			},
			Breakout: BreakoutConfig{
				Enabled:            true,
				VolumeRatioMin:     1.5,
				RequireMACDConfirm: true,
			},
		},
		Risk: StrategyRiskConfig{
			StopLossPercent:       5,
			TakeProfitPercent:     10,
			StopLossATRMultiplier: 0,
			MaxPositionAmount:     100000,
			MaxDailyLoss:          50000,
		},
		HTFFilter: HTFFilterConfig{
			Enabled:           true,
			BlockCounterTrend: true,
			AlignmentBoost:    0.1,
		},
	}
}

func TestStrategyProfile_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*StrategyProfile)
		wantErr string // substring expected in the joined error; empty means expect nil
	}{
		{
			name:    "happy path",
			mutate:  func(p *StrategyProfile) {},
			wantErr: "",
		},
		{
			name:    "empty name",
			mutate:  func(p *StrategyProfile) { p.Name = "" },
			wantErr: "name must not be empty",
		},
		{
			name:    "sma_short zero",
			mutate:  func(p *StrategyProfile) { p.Indicators.SMAShort = 0 },
			wantErr: "sma_short",
		},
		{
			name:    "sma_long zero",
			mutate:  func(p *StrategyProfile) { p.Indicators.SMALong = 0 },
			wantErr: "sma_long",
		},
		{
			name: "sma_short >= sma_long",
			mutate: func(p *StrategyProfile) {
				p.Indicators.SMAShort = 30
				p.Indicators.SMALong = 30
			},
			wantErr: "must be < sma_long",
		},
		{
			name:    "rsi_period zero",
			mutate:  func(p *StrategyProfile) { p.Indicators.RSIPeriod = 0 },
			wantErr: "rsi_period",
		},
		{
			name:    "macd_fast zero",
			mutate:  func(p *StrategyProfile) { p.Indicators.MACDFast = 0 },
			wantErr: "macd_fast",
		},
		{
			name:    "macd_slow zero",
			mutate:  func(p *StrategyProfile) { p.Indicators.MACDSlow = 0 },
			wantErr: "macd_slow",
		},
		{
			name: "macd_fast >= macd_slow",
			mutate: func(p *StrategyProfile) {
				p.Indicators.MACDFast = 26
				p.Indicators.MACDSlow = 26
			},
			wantErr: "must be < macd_slow",
		},
		{
			name:    "macd_signal zero",
			mutate:  func(p *StrategyProfile) { p.Indicators.MACDSignal = 0 },
			wantErr: "macd_signal",
		},
		{
			name:    "bb_period zero",
			mutate:  func(p *StrategyProfile) { p.Indicators.BBPeriod = 0 },
			wantErr: "bb_period",
		},
		{
			name:    "atr_period negative",
			mutate:  func(p *StrategyProfile) { p.Indicators.ATRPeriod = -1 },
			wantErr: "atr_period",
		},
		{
			name:    "bb_multiplier zero",
			mutate:  func(p *StrategyProfile) { p.Indicators.BBMultiplier = 0 },
			wantErr: "bb_multiplier",
		},
		{
			name:    "rsi_oversold out of range low",
			mutate:  func(p *StrategyProfile) { p.StanceRules.RSIOversold = 0 },
			wantErr: "rsi_oversold",
		},
		{
			name:    "rsi_oversold out of range high",
			mutate:  func(p *StrategyProfile) { p.StanceRules.RSIOversold = 100 },
			wantErr: "rsi_oversold",
		},
		{
			name:    "rsi_overbought out of range",
			mutate:  func(p *StrategyProfile) { p.StanceRules.RSIOverbought = 150 },
			wantErr: "rsi_overbought",
		},
		{
			name: "rsi_oversold >= rsi_overbought",
			mutate: func(p *StrategyProfile) {
				p.StanceRules.RSIOversold = 80
				p.StanceRules.RSIOverbought = 20
			},
			wantErr: "must be < rsi_overbought",
		},
		{
			name:    "stop_loss_percent negative",
			mutate:  func(p *StrategyProfile) { p.Risk.StopLossPercent = -1 },
			wantErr: "stop_loss_percent",
		},
		{
			name:    "take_profit_percent negative",
			mutate:  func(p *StrategyProfile) { p.Risk.TakeProfitPercent = -1 },
			wantErr: "take_profit_percent",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validProfile()
			tc.mutate(&p)
			err := p.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

// TestStrategyProfile_JSONRoundTrip confirms JSON tag correctness by
// marshaling the spec §3.2 example, comparing against the spec-literal JSON,
// then unmarshaling and checking the struct round-trips. The fixture is
// inlined so we don't depend on any on-disk file.
func TestStrategyProfile_JSONRoundTrip(t *testing.T) {
	// This is the spec §3.2 example verbatim.
	const specJSON = `{
  "name": "ltc_aggressive_v3",
  "description": "LTC向け攻めの短期戦略",
  "indicators": {
    "sma_short": 10,
    "sma_long": 30,
    "rsi_period": 14,
    "macd_fast": 12,
    "macd_slow": 26,
    "macd_signal": 9,
    "bb_period": 20,
    "bb_multiplier": 2.0,
    "atr_period": 14
  },
  "stance_rules": {
    "rsi_oversold": 20,
    "rsi_overbought": 80,
    "sma_convergence_threshold": 0.001,
    "bb_squeeze_lookback": 5,
    "breakout_volume_ratio": 1.5
  },
  "signal_rules": {
    "trend_follow": {
      "enabled": true,
      "require_macd_confirm": true,
      "require_ema_cross": true,
      "rsi_buy_max": 70,
      "rsi_sell_min": 30,
      "adx_min": 0
    },
    "contrarian": {
      "enabled": true,
      "rsi_entry": 30,
      "rsi_exit": 70,
      "macd_histogram_limit": 10,
      "adx_max": 0,
      "stoch_entry_max": 0,
      "stoch_exit_min": 0
    },
    "breakout": {
      "enabled": true,
      "volume_ratio_min": 1.5,
      "require_macd_confirm": true,
      "adx_min": 0
    }
  },
  "strategy_risk": {
    "stop_loss_percent": 5,
    "take_profit_percent": 10,
    "stop_loss_atr_multiplier": 0,
    "trailing_atr_multiplier": 0,
    "max_position_amount": 100000,
    "max_daily_loss": 50000
  },
  "htf_filter": {
    "enabled": true,
    "block_counter_trend": true,
    "alignment_boost": 0.1
  }
}`

	var decoded StrategyProfile
	if err := json.Unmarshal([]byte(specJSON), &decoded); err != nil {
		t.Fatalf("unmarshal spec json: %v", err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("decoded spec profile failed Validate: %v", err)
	}

	want := validProfile()
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded profile does not match expected struct\n got: %#v\nwant: %#v", decoded, want)
	}

	// Marshal the struct, then decode both sides into map[string]any and
	// compare — avoids whitespace / key-order sensitivity while still
	// proving all JSON tags are correct.
	re, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var gotMap, wantMap map[string]any
	if err := json.Unmarshal(re, &gotMap); err != nil {
		t.Fatalf("unmarshal re-encoded json: %v", err)
	}
	if err := json.Unmarshal([]byte(specJSON), &wantMap); err != nil {
		t.Fatalf("unmarshal spec json as map: %v", err)
	}
	if !reflect.DeepEqual(gotMap, wantMap) {
		t.Fatalf("JSON round-trip differs.\n got: %v\nwant: %v", gotMap, wantMap)
	}
}

// TestStrategyProfile_Validate_RegimeDetectorConfig is the PR-5 part F
// schema guard. detector_config is optional inside regime_routing;
// when present, every field must be >= 0 (zero is "use default" per
// regime.NewDetector). Negative values are silently coerced by the
// detector to defaults today, which would mask grid typos — Validate
// catches them at the loader boundary instead.
func TestStrategyProfile_Validate_RegimeDetectorConfig(t *testing.T) {
	routerWith := func(dc *RegimeDetectorConfig) StrategyProfile {
		return StrategyProfile{
			Name: "router",
			RegimeRouting: &RegimeRoutingConfig{
				Default:        "child",
				DetectorConfig: dc,
			},
		}
	}

	t.Run("nil detector_config is allowed", func(t *testing.T) {
		if err := routerWith(nil).Validate(); err != nil {
			t.Fatalf("nil detector_config rejected: %v", err)
		}
	})

	t.Run("zero values are allowed (= use defaults)", func(t *testing.T) {
		p := routerWith(&RegimeDetectorConfig{})
		if err := p.Validate(); err != nil {
			t.Fatalf("zero detector_config rejected: %v", err)
		}
	})

	t.Run("positive values are allowed", func(t *testing.T) {
		p := routerWith(&RegimeDetectorConfig{
			TrendADXMin: 25, VolatileATRPercentMin: 3.5, HysteresisBars: 5,
		})
		if err := p.Validate(); err != nil {
			t.Fatalf("positive detector_config rejected: %v", err)
		}
	})

	t.Run("negative TrendADXMin rejected", func(t *testing.T) {
		p := routerWith(&RegimeDetectorConfig{TrendADXMin: -1})
		if err := p.Validate(); err == nil {
			t.Fatal("negative TrendADXMin must be rejected")
		}
	})

	t.Run("negative VolatileATRPercentMin rejected", func(t *testing.T) {
		p := routerWith(&RegimeDetectorConfig{VolatileATRPercentMin: -0.5})
		if err := p.Validate(); err == nil {
			t.Fatal("negative VolatileATRPercentMin must be rejected")
		}
	})

	t.Run("negative HysteresisBars rejected", func(t *testing.T) {
		p := routerWith(&RegimeDetectorConfig{HysteresisBars: -1})
		if err := p.Validate(); err == nil {
			t.Fatal("negative HysteresisBars must be rejected")
		}
	})
}
