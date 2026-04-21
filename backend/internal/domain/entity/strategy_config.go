package entity

import (
	"errors"
	"fmt"
)

// StrategyProfile is the declarative configuration file that drives
// backtest / live strategy parameters. The on-disk JSON shape is documented in
// docs/superpowers/specs/2026-04-16-pdca-strategy-optimizer-design.md §3.2.
//
// Note: the `Risk` struct field is intentionally tagged `strategy_risk`
// (not `risk`) to match the spec.
type StrategyProfile struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Indicators  IndicatorConfig    `json:"indicators"`
	StanceRules StanceRulesConfig  `json:"stance_rules"`
	SignalRules SignalRulesConfig  `json:"signal_rules"`
	Risk        StrategyRiskConfig `json:"strategy_risk"`
	HTFFilter   HTFFilterConfig    `json:"htf_filter"`
}

// IndicatorConfig declares the lookback periods and shape parameters used when
// computing technical indicators (SMA / RSI / MACD / BB / ATR).
type IndicatorConfig struct {
	SMAShort     int     `json:"sma_short"`
	SMALong      int     `json:"sma_long"`
	RSIPeriod    int     `json:"rsi_period"`
	MACDFast     int     `json:"macd_fast"`
	MACDSlow     int     `json:"macd_slow"`
	MACDSignal   int     `json:"macd_signal"`
	BBPeriod     int     `json:"bb_period"`
	BBMultiplier float64 `json:"bb_multiplier"`
	ATRPeriod    int     `json:"atr_period"`
}

// StanceRulesConfig declares thresholds for rule-based stance classification
// (TREND_FOLLOW / CONTRARIAN / BREAKOUT / HOLD).
type StanceRulesConfig struct {
	RSIOversold             float64 `json:"rsi_oversold"`
	RSIOverbought           float64 `json:"rsi_overbought"`
	SMAConvergenceThreshold float64 `json:"sma_convergence_threshold"`
	BBSqueezeLookback       int     `json:"bb_squeeze_lookback"`
	BreakoutVolumeRatio     float64 `json:"breakout_volume_ratio"`
}

// SignalRulesConfig aggregates per-stance entry/exit rule configs.
type SignalRulesConfig struct {
	TrendFollow TrendFollowConfig `json:"trend_follow"`
	Contrarian  ContrarianConfig  `json:"contrarian"`
	Breakout    BreakoutConfig    `json:"breakout"`
}

// TrendFollowConfig configures the trend-follow signal generator.
type TrendFollowConfig struct {
	Enabled            bool    `json:"enabled"`
	RequireMACDConfirm bool    `json:"require_macd_confirm"`
	RequireEMACross    bool    `json:"require_ema_cross"`
	RSIBuyMax          float64 `json:"rsi_buy_max"`
	RSISellMin         float64 `json:"rsi_sell_min"`
	// PR-6: trend_follow fires only when ADX >= ADXMin (0 = gate disabled).
	ADXMin float64 `json:"adx_min"`
}

// ContrarianConfig configures the contrarian signal generator.
type ContrarianConfig struct {
	Enabled            bool    `json:"enabled"`
	RSIEntry           float64 `json:"rsi_entry"`
	RSIExit            float64 `json:"rsi_exit"`
	MACDHistogramLimit float64 `json:"macd_histogram_limit"`
	// PR-6: contrarian fires only when ADX <= ADXMax (0 = gate disabled).
	ADXMax float64 `json:"adx_max"`
	// PR-7: contrarian Stochastics gates. 0 = gate disabled.
	//   - StochEntryMax: contrarian BUY requires %K <= this (oversold).
	//     Typical value: 20.
	//   - StochExitMin:  contrarian SELL requires %K >= this (overbought).
	//     Typical value: 80.
	StochEntryMax float64 `json:"stoch_entry_max"`
	StochExitMin  float64 `json:"stoch_exit_min"`
}

// BreakoutConfig configures the breakout signal generator.
type BreakoutConfig struct {
	Enabled            bool    `json:"enabled"`
	VolumeRatioMin     float64 `json:"volume_ratio_min"`
	RequireMACDConfirm bool    `json:"require_macd_confirm"`
	// PR-6: breakout fires only when ADX >= ADXMin (0 = gate disabled).
	ADXMin float64 `json:"adx_min"`
}

// HTFFilterConfig configures the higher-timeframe trend filter.
type HTFFilterConfig struct {
	Enabled           bool    `json:"enabled"`
	BlockCounterTrend bool    `json:"block_counter_trend"`
	AlignmentBoost    float64 `json:"alignment_boost"`
	// PR-8: Mode selects the HTF trend-detection method.
	//   - "" or "ema":      legacy SMA20/SMA50 comparison (default).
	//   - "ichimoku":       price vs. cloud on the higher timeframe.
	//                       above-cloud -> uptrend, below-cloud -> downtrend,
	//                       inside-cloud -> neutral (blocks both directions
	//                       when block_counter_trend is true).
	// A missing Ichimoku snapshot falls through to "unknown" and takes no
	// action (neither blocks nor boosts) so partial warmup never silently
	// opens up a counter-trend signal.
	Mode string `json:"mode,omitempty"`
}

// StrategyRiskConfig configures the per-strategy risk envelope (position
// sizing, stop-loss, take-profit, daily loss cap).
type StrategyRiskConfig struct {
	StopLossPercent       float64 `json:"stop_loss_percent"`
	TakeProfitPercent     float64 `json:"take_profit_percent"`
	StopLossATRMultiplier float64 `json:"stop_loss_atr_multiplier"`
	// TrailingATRMultiplier: >0 ならトレイリングストップを ATR ベースで計算。
	// 0 なら従来通り StopLossPercent ベース。詳細は entity.RiskConfig のコメント。
	TrailingATRMultiplier float64 `json:"trailing_atr_multiplier"`
	MaxPositionAmount     float64 `json:"max_position_amount"`
	MaxDailyLoss          float64 `json:"max_daily_loss"`
}

// Validate reports structural problems in the profile. The goal is to reject
// garbage (missing name, negative periods, inverted RSI bounds), not to
// enforce a business policy.
//
// All violations are collected via errors.Join so the caller sees every
// problem in one pass.
//
// Value receiver: Validate() does not mutate the receiver, and a value
// receiver eliminates the possibility of a nil-pointer panic if a caller
// ever holds a nil *StrategyProfile.
func (p StrategyProfile) Validate() error {
	var errs []error

	if p.Name == "" {
		errs = append(errs, errors.New("name must not be empty"))
	}

	ind := p.Indicators
	if ind.SMAShort <= 0 {
		errs = append(errs, fmt.Errorf("indicators.sma_short must be > 0 (got %d)", ind.SMAShort))
	}
	if ind.SMALong <= 0 {
		errs = append(errs, fmt.Errorf("indicators.sma_long must be > 0 (got %d)", ind.SMALong))
	}
	if ind.SMAShort > 0 && ind.SMALong > 0 && ind.SMAShort >= ind.SMALong {
		errs = append(errs, fmt.Errorf("indicators.sma_short (%d) must be < sma_long (%d)", ind.SMAShort, ind.SMALong))
	}
	if ind.RSIPeriod <= 0 {
		errs = append(errs, fmt.Errorf("indicators.rsi_period must be > 0 (got %d)", ind.RSIPeriod))
	}
	if ind.MACDFast <= 0 {
		errs = append(errs, fmt.Errorf("indicators.macd_fast must be > 0 (got %d)", ind.MACDFast))
	}
	if ind.MACDSlow <= 0 {
		errs = append(errs, fmt.Errorf("indicators.macd_slow must be > 0 (got %d)", ind.MACDSlow))
	}
	if ind.MACDFast > 0 && ind.MACDSlow > 0 && ind.MACDFast >= ind.MACDSlow {
		errs = append(errs, fmt.Errorf("indicators.macd_fast (%d) must be < macd_slow (%d)", ind.MACDFast, ind.MACDSlow))
	}
	if ind.MACDSignal <= 0 {
		errs = append(errs, fmt.Errorf("indicators.macd_signal must be > 0 (got %d)", ind.MACDSignal))
	}
	if ind.BBPeriod <= 0 {
		errs = append(errs, fmt.Errorf("indicators.bb_period must be > 0 (got %d)", ind.BBPeriod))
	}
	if ind.ATRPeriod <= 0 {
		errs = append(errs, fmt.Errorf("indicators.atr_period must be > 0 (got %d)", ind.ATRPeriod))
	}
	if ind.BBMultiplier <= 0 {
		errs = append(errs, fmt.Errorf("indicators.bb_multiplier must be > 0 (got %v)", ind.BBMultiplier))
	}

	sr := p.StanceRules
	if sr.RSIOversold <= 0 || sr.RSIOversold >= 100 {
		errs = append(errs, fmt.Errorf("stance_rules.rsi_oversold must be in (0, 100) (got %v)", sr.RSIOversold))
	}
	if sr.RSIOverbought <= 0 || sr.RSIOverbought >= 100 {
		errs = append(errs, fmt.Errorf("stance_rules.rsi_overbought must be in (0, 100) (got %v)", sr.RSIOverbought))
	}
	if sr.RSIOversold > 0 && sr.RSIOverbought > 0 && sr.RSIOversold >= sr.RSIOverbought {
		errs = append(errs, fmt.Errorf("stance_rules.rsi_oversold (%v) must be < rsi_overbought (%v)", sr.RSIOversold, sr.RSIOverbought))
	}

	if p.Risk.StopLossPercent < 0 {
		errs = append(errs, fmt.Errorf("strategy_risk.stop_loss_percent must be >= 0 (got %v)", p.Risk.StopLossPercent))
	}
	if p.Risk.TakeProfitPercent < 0 {
		errs = append(errs, fmt.Errorf("strategy_risk.take_profit_percent must be >= 0 (got %v)", p.Risk.TakeProfitPercent))
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid strategy profile: %w", errors.Join(errs...))
}
