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
	// RegimeRouting is optional. When set (non-nil), the profile is
	// treated as a router that delegates to child profiles by Regime.
	// See RegimeRoutingConfig and usecase/strategy.ProfileRouter.
	//
	// Pointer (not value) so the JSON round-trip preserves "this
	// profile has no routing block" — a value-typed RegimeRoutingConfig
	// always serialises an empty map, which would corrupt the existing
	// TestStrategyProfile_JSONRoundTrip contract.
	RegimeRouting *RegimeRoutingConfig `json:"regime_routing,omitempty"`
}

// IndicatorConfig declares the lookback periods and shape parameters used when
// computing technical indicators. Every indicator the strategy layer consumes
// is profile-driven so PT5M / PT1M strategies can scale lookbacks to their
// timeframe; zero on any field falls back to the legacy LTC PT15M default
// via WithDefaults.
type IndicatorConfig struct {
	SMAShort     int     `json:"sma_short"`
	SMALong      int     `json:"sma_long"`
	EMAFast      int     `json:"ema_fast"`
	EMASlow      int     `json:"ema_slow"`
	RSIPeriod    int     `json:"rsi_period"`
	MACDFast     int     `json:"macd_fast"`
	MACDSlow     int     `json:"macd_slow"`
	MACDSignal   int     `json:"macd_signal"`
	BBPeriod     int     `json:"bb_period"`
	BBMultiplier float64 `json:"bb_multiplier"`
	ATRPeriod    int     `json:"atr_period"`
	// VolumeSMAPeriod drives both VolumeSMA and the VolumeRatio denominator.
	// 0 falls back to BBPeriod (legacy behaviour computed VolumeSMA20 alongside
	// BB20; keeping the same default avoids surprising existing profiles).
	VolumeSMAPeriod int `json:"volume_sma_period"`

	// PR-C: ADX (Average Directional Index) family lookback. Used by both
	// the trend-follow ADX_min gate and the contrarian ADX_max gate; needs
	// 2*period+1 bars before producing a non-NaN value (Wilder smoothing).
	// 0 falls back to 14.
	ADXPeriod int `json:"adx_period"`

	// PR-C: Stochastics (slow %K) parameters. StochKPeriod is the raw
	// look-back over which the high/low range is taken, StochSmoothK
	// smooths the raw %K into the slow %K, StochSmoothD smooths the slow
	// %K into %D. Legacy values 14 / 3 / 3. 0 each falls back.
	StochKPeriod  int `json:"stoch_k_period"`
	StochSmoothK  int `json:"stoch_smooth_k"`
	StochSmoothD  int `json:"stoch_smooth_d"`

	// PR-C: Stochastic RSI parameters. StochRSIRSIPeriod is the inner RSI
	// window, StochRSIStochPeriod is the outer stochastic window applied
	// to the RSI series. Legacy 14 / 14. 0 each falls back.
	StochRSIRSIPeriod   int `json:"stoch_rsi_rsi_period"`
	StochRSIStochPeriod int `json:"stoch_rsi_stoch_period"`

	// PR-C: Donchian Channel high/low look-back. Default 20 (~5h on 15m
	// bars). 0 falls back. Used by the breakout gate's Donchian filter.
	DonchianPeriod int `json:"donchian_period"`

	// PR-C: OBV slope look-back (in bars). Default 20. Drives OBVSlope
	// which trend-follow uses as a volume-confirmation gate. 0 falls back.
	OBVSlopePeriod int `json:"obv_slope_period"`

	// PR-C: CMF (Chaikin Money Flow) look-back. Default 20. Drives the
	// breakout gate's CMF filter (cmf_buy_min / cmf_sell_max). 0 falls back.
	CMFPeriod int `json:"cmf_period"`

	// PR-C: Ichimoku Kinkō Hyō periods.
	//   - IchimokuTenkan: conversion line (default 9)
	//   - IchimokuKijun:  base line; also drives the +N forward shift of
	//                     SenkouA/B and the −N backward shift of Chikou
	//                     (default 26)
	//   - IchimokuSenkouB: leading span B look-back (default 52)
	// Used by HTFFilter when mode="ichimoku". 0 each falls back.
	IchimokuTenkan  int `json:"ichimoku_tenkan"`
	IchimokuKijun   int `json:"ichimoku_kijun"`
	IchimokuSenkouB int `json:"ichimoku_senkou_b"`
}

// WithDefaults returns the IndicatorConfig with zero-valued fields filled
// in from the legacy hardcoded defaults (SMA 20/50, EMA 12/26, RSI 14, MACD
// 12/26/9, BB 20×2.0, ATR 14, VolumeSMA = bb_period). This is what the
// IndicatorCalculator / IndicatorHandler use when a profile does not specify
// every field — they get period-aware behaviour where the profile is
// explicit and the legacy LTC PT15M baseline elsewhere.
//
// Method on a copy so callers cannot accidentally mutate the receiver.
func (c IndicatorConfig) WithDefaults() IndicatorConfig {
	if c.SMAShort <= 0 {
		c.SMAShort = 20
	}
	if c.SMALong <= 0 {
		c.SMALong = 50
	}
	if c.EMAFast <= 0 {
		c.EMAFast = 12
	}
	if c.EMASlow <= 0 {
		c.EMASlow = 26
	}
	if c.RSIPeriod <= 0 {
		c.RSIPeriod = 14
	}
	if c.MACDFast <= 0 {
		c.MACDFast = 12
	}
	if c.MACDSlow <= 0 {
		c.MACDSlow = 26
	}
	if c.MACDSignal <= 0 {
		c.MACDSignal = 9
	}
	if c.BBPeriod <= 0 {
		c.BBPeriod = 20
	}
	if c.BBMultiplier <= 0 {
		c.BBMultiplier = 2.0
	}
	if c.ATRPeriod <= 0 {
		c.ATRPeriod = 14
	}
	if c.VolumeSMAPeriod <= 0 {
		c.VolumeSMAPeriod = c.BBPeriod
	}
	if c.ADXPeriod <= 0 {
		c.ADXPeriod = 14
	}
	if c.StochKPeriod <= 0 {
		c.StochKPeriod = 14
	}
	if c.StochSmoothK <= 0 {
		c.StochSmoothK = 3
	}
	if c.StochSmoothD <= 0 {
		c.StochSmoothD = 3
	}
	if c.StochRSIRSIPeriod <= 0 {
		c.StochRSIRSIPeriod = 14
	}
	if c.StochRSIStochPeriod <= 0 {
		c.StochRSIStochPeriod = 14
	}
	if c.DonchianPeriod <= 0 {
		c.DonchianPeriod = 20
	}
	if c.OBVSlopePeriod <= 0 {
		c.OBVSlopePeriod = 20
	}
	if c.CMFPeriod <= 0 {
		c.CMFPeriod = 20
	}
	if c.IchimokuTenkan <= 0 {
		c.IchimokuTenkan = 9
	}
	if c.IchimokuKijun <= 0 {
		c.IchimokuKijun = 26
	}
	if c.IchimokuSenkouB <= 0 {
		c.IchimokuSenkouB = 52
	}
	return c
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
	// PR-9: OBV slope confirmation. When RequireOBVAlignment is true, a
	// trend-follow BUY requires OBVSlope > 0 (net buying volume over the
	// last 20 bars) and SELL requires OBVSlope < 0. Missing OBVSlope
	// fails the gate, matching the ADX/Stoch convention. Defaults to false
	// so existing profiles are bit-identical.
	RequireOBVAlignment bool `json:"require_obv_alignment,omitempty"`
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
	// PR-11: Donchian Channel confirmation. DonchianPeriod > 0 activates
	// the gate; a typical value is 20 (~5 hours on 15m bars). When active:
	//   - BUY  requires lastPrice > Donchian(period).Upper
	//   - SELL requires lastPrice < Donchian(period).Lower
	// The gate is orthogonal to the existing BB-width/volume gates — BB
	// detects mean-reversion squeeze-and-release while Donchian detects
	// range-of-N breakout; both must agree before a signal fires. Missing
	// Donchian (warmup) treats the gate as a fail, matching ADX/Stoch.
	DonchianPeriod int `json:"donchian_period,omitempty"`
	// PR-9: CMF confirmation. CMFBuyMin > 0 activates the BUY gate
	// (breakout BUY requires CMF >= CMFBuyMin); CMFSellMax < 0
	// activates the SELL gate (SELL requires CMF <= CMFSellMax). Both
	// default to 0 so existing profiles are bit-identical. CMF is
	// bounded in [-1, 1]; typical active values ~ ±0.1.
	CMFBuyMin  float64 `json:"cmf_buy_min,omitempty"`
	CMFSellMax float64 `json:"cmf_sell_max,omitempty"`
}

// HTFFilterConfig configures the higher-timeframe trend filter.
type HTFFilterConfig struct {
	Enabled           bool    `json:"enabled"`
	BlockCounterTrend bool    `json:"block_counter_trend"`
	AlignmentBoost    float64 `json:"alignment_boost"`
	// PR-8: Mode selects the HTF trend-detection method.
	//   - "" or "ema":      legacy SMAShort/SMALong comparison (default).
	//   - "ichimoku":       price vs. cloud on the higher timeframe.
	//                       above-cloud -> uptrend, below-cloud -> downtrend,
	//                       inside-cloud -> neutral (blocks both directions
	//                       when block_counter_trend is true).
	// A missing Ichimoku snapshot falls through to "unknown" and takes no
	// action (neither blocks nor boosts) so partial warmup never silently
	// opens up a counter-trend signal.
	Mode string `json:"mode,omitempty"`
}

// RegimeRoutingConfig declares regime-conditional profile delegation.
// When set, the loaded profile becomes a *router*: the strategy looks
// up the current Regime (see usecase/regime.Detector) and delegates to
// the child profile named by Default (when no regime-specific override
// applies) or by Overrides[regime].
//
// Child profiles are loaded by the same Loader from the same base
// directory. Children must NOT themselves carry regime_routing — depth
// is capped at 1 to keep the routing graph readable and to avoid
// silent infinite-loop debugging.
//
// Example: cycle28-37 produced two finalists, one strong on
// trending bull regimes (sl14_tf60_35) and one robust to bear /
// volatile regimes (sl6_tr30_tp6_tf60_35). A regime router pairing the
// two looks like:
//
//	"regime_routing": {
//	  "default": "experiment_2026-04-22_sl14_tf60_35",
//	  "overrides": {
//	    "bear-trend": "experiment_2026-04-22_sl6_tr30_tp6_tf60_35",
//	    "volatile":   "experiment_2026-04-22_sl6_tr30_tp6_tf60_35"
//	  }
//	}
type RegimeRoutingConfig struct {
	// Default is the child profile name used when the detector emits
	// RegimeUnknown (warmup) or a regime not listed in Overrides.
	// Required when regime_routing is set; the profile is otherwise a
	// no-op routing wrapper that just runs Default 100% of the time.
	Default string `json:"default"`

	// Overrides maps Regime label → child profile name. Keys must be
	// known Regime values (bull-trend / bear-trend / range / volatile);
	// "" / "unknown" is rejected because it would shadow Default.
	Overrides map[string]string `json:"overrides,omitempty"`

	// DetectorConfig optionally tunes the regime detector thresholds
	// for this router profile. nil (or an empty struct, since pointer
	// elision is awkward inside a value field) falls back to
	// regime.DefaultConfig — the cycle39 result motivated exposing
	// these so the WFO can sweep them and find thresholds that
	// actually emit more than one regime on the asset under test.
	//
	// All three fields are optional; zero / negative values are
	// replaced by regime.DefaultConfig values inside regime.NewDetector.
	DetectorConfig *RegimeDetectorConfig `json:"detector_config,omitempty"`
}

// RegimeDetectorConfig mirrors regime.Config in the JSON schema. Kept in
// the entity package so the strategy / handler layers can populate it
// without importing the regime usecase package directly (the builder
// at strategy.BuildStrategyFromProfile is the single place that
// translates this into a regime.Config value).
type RegimeDetectorConfig struct {
	// TrendADXMin: ADX value at or above which a directional regime
	// (bull-trend / bear-trend) becomes eligible. 0 / unset → 20
	// (Wilder's "trend present" threshold).
	TrendADXMin float64 `json:"trend_adx_min,omitempty"`

	// VolatileATRPercentMin: ATR/price threshold (in percent units,
	// e.g. 2.5 = 2.5%) at or above which a non-trending bar is
	// classified as volatile rather than range. 0 / unset → 2.5.
	VolatileATRPercentMin float64 `json:"volatile_atr_percent_min,omitempty"`

	// HysteresisBars: minimum consecutive bars a new candidate regime
	// must persist before the detector switches to it. 0 / unset → 3.
	HysteresisBars int `json:"hysteresis_bars,omitempty"`
}

// IsRouting reports whether this profile is a router. Centralised so
// callers do not duplicate the "Default != """ check.
func (r RegimeRoutingConfig) IsRouting() bool { return r.Default != "" }

// HasRouting is the nil-safe form for *RegimeRoutingConfig fields on
// StrategyProfile — handles "no routing block at all" without forcing
// callers to write `p.RegimeRouting != nil && p.RegimeRouting.IsRouting()`.
func (p StrategyProfile) HasRouting() bool {
	return p.RegimeRouting != nil && p.RegimeRouting.IsRouting()
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

	// PositionSizing configures dynamic lot sizing. nil (or Mode == "fixed" /
	// unset) keeps the legacy behaviour: the runtime supplied trade amount is
	// used as-is. When set, the sizer computes per-trade lot size from
	// equity, signal confidence, ATR, and drawdown. See usecase/positionsize.
	PositionSizing *PositionSizingConfig `json:"position_sizing,omitempty"`
}

// PositionSizingConfig declares how per-trade lot size is derived at runtime.
// All fields are optional; zero values fall back to conservative defaults
// inside the sizer so existing profiles remain bit-identical.
type PositionSizingConfig struct {
	// Mode selects the sizing algorithm.
	//   - ""      : treated as "fixed" (legacy behaviour, bit-identical).
	//   - "fixed" : the runtime trade amount is used as-is (legacy).
	//   - "risk_pct" : per-trade risk budget is a fraction of equity; lot
	//     size = risk_jpy / (entry_price × stop_loss_percent).
	Mode string `json:"mode,omitempty"`

	// RiskPerTradePct is the fraction of equity risked on a single trade,
	// expressed in percent (1.0 = 1%). Only consulted when Mode == "risk_pct".
	// Must be > 0 and ≤ 10.0 when Mode is "risk_pct"; 0 falls back to 1.0.
	RiskPerTradePct float64 `json:"risk_per_trade_pct,omitempty"`

	// MaxPositionPctOfEquity caps the notional value of a single position as
	// a fraction of equity in percent (20 = 20%). 0 disables the cap.
	MaxPositionPctOfEquity float64 `json:"max_position_pct_of_equity,omitempty"`

	// ATRAdjust enables volatility normalisation. When true, the lot size is
	// scaled by TargetATRPct / (current_atr/entry_price*100), clamped to
	// [ATRScaleMin, ATRScaleMax]. Ignored when current ATR is missing.
	ATRAdjust      bool    `json:"atr_adjust,omitempty"`
	TargetATRPct   float64 `json:"target_atr_pct,omitempty"`
	ATRScaleMin    float64 `json:"atr_scale_min,omitempty"`
	ATRScaleMax    float64 `json:"atr_scale_max,omitempty"`

	// DrawdownScaleDown triggers lot-size de-escalation when equity is in
	// drawdown. Thresholds are in percent of peak equity. 0 disables the
	// corresponding tier.
	DDScaleDown DrawdownScaleConfig `json:"drawdown_scale_down,omitzero"`

	// MinLot / LotStep control venue-side rounding. 0 disables and falls
	// back to the executor's defaults.
	MinLot  float64 `json:"min_lot,omitempty"`
	LotStep float64 `json:"lot_step,omitempty"`
}

// DrawdownScaleConfig is a two-tier DD-based lot scaler.
//   - TierAPct (e.g. 10.0) = first threshold; when DD ≥ TierAPct, lot *= TierAScale.
//   - TierBPct (e.g. 15.0) = second threshold; when DD ≥ TierBPct, lot *= TierBScale.
// 0 on either tier disables that tier.
type DrawdownScaleConfig struct {
	TierAPct   float64 `json:"tier_a_pct,omitempty"`
	TierAScale float64 `json:"tier_a_scale,omitempty"`
	TierBPct   float64 `json:"tier_b_pct,omitempty"`
	TierBScale float64 `json:"tier_b_scale,omitempty"`
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

	// Routing profiles delegate every per-bar decision to children, so
	// indicator periods / signal rules / risk envelope on the router
	// itself are unused. Skip the field-level checks below and only
	// validate the routing block.
	if p.RegimeRouting != nil && p.RegimeRouting.Default != "" {
		for key := range p.RegimeRouting.Overrides {
			if !Regime(key).IsValidLabel() {
				errs = append(errs, fmt.Errorf("regime_routing.overrides has unknown regime key %q (want one of bull-trend, bear-trend, range, volatile)", key))
			}
		}
		// detector_config: zero / unset means "use regime.DefaultConfig",
		// so we only reject negative values that would otherwise be
		// silently coerced. Tightness checks (e.g. ADX 0..100) belong
		// in the detector — Validate stays at "structural sanity".
		if dc := p.RegimeRouting.DetectorConfig; dc != nil {
			if dc.TrendADXMin < 0 {
				errs = append(errs, fmt.Errorf("regime_routing.detector_config.trend_adx_min must be >= 0 (got %v)", dc.TrendADXMin))
			}
			if dc.VolatileATRPercentMin < 0 {
				errs = append(errs, fmt.Errorf("regime_routing.detector_config.volatile_atr_percent_min must be >= 0 (got %v)", dc.VolatileATRPercentMin))
			}
			if dc.HysteresisBars < 0 {
				errs = append(errs, fmt.Errorf("regime_routing.detector_config.hysteresis_bars must be >= 0 (got %d)", dc.HysteresisBars))
			}
		}
		if len(errs) == 0 {
			return nil
		}
		return fmt.Errorf("invalid strategy profile: %w", errors.Join(errs...))
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
	// EMAFast/Slow are optional: 0 means "fall back to defaults" via WithDefaults.
	// Reject only obviously broken ordering (fast >= slow) and negative values.
	if ind.EMAFast < 0 {
		errs = append(errs, fmt.Errorf("indicators.ema_fast must be >= 0 (got %d)", ind.EMAFast))
	}
	if ind.EMASlow < 0 {
		errs = append(errs, fmt.Errorf("indicators.ema_slow must be >= 0 (got %d)", ind.EMASlow))
	}
	if ind.EMAFast > 0 && ind.EMASlow > 0 && ind.EMAFast >= ind.EMASlow {
		errs = append(errs, fmt.Errorf("indicators.ema_fast (%d) must be < ema_slow (%d)", ind.EMAFast, ind.EMASlow))
	}
	if ind.VolumeSMAPeriod < 0 {
		errs = append(errs, fmt.Errorf("indicators.volume_sma_period must be >= 0 (got %d)", ind.VolumeSMAPeriod))
	}
	// PR-C: optional periods. 0 means "fall back to default" via WithDefaults;
	// only reject negative values that would otherwise be silently coerced.
	if ind.ADXPeriod < 0 {
		errs = append(errs, fmt.Errorf("indicators.adx_period must be >= 0 (got %d)", ind.ADXPeriod))
	}
	if ind.StochKPeriod < 0 {
		errs = append(errs, fmt.Errorf("indicators.stoch_k_period must be >= 0 (got %d)", ind.StochKPeriod))
	}
	if ind.StochSmoothK < 0 {
		errs = append(errs, fmt.Errorf("indicators.stoch_smooth_k must be >= 0 (got %d)", ind.StochSmoothK))
	}
	if ind.StochSmoothD < 0 {
		errs = append(errs, fmt.Errorf("indicators.stoch_smooth_d must be >= 0 (got %d)", ind.StochSmoothD))
	}
	if ind.StochRSIRSIPeriod < 0 {
		errs = append(errs, fmt.Errorf("indicators.stoch_rsi_rsi_period must be >= 0 (got %d)", ind.StochRSIRSIPeriod))
	}
	if ind.StochRSIStochPeriod < 0 {
		errs = append(errs, fmt.Errorf("indicators.stoch_rsi_stoch_period must be >= 0 (got %d)", ind.StochRSIStochPeriod))
	}
	if ind.DonchianPeriod < 0 {
		errs = append(errs, fmt.Errorf("indicators.donchian_period must be >= 0 (got %d)", ind.DonchianPeriod))
	}
	if ind.OBVSlopePeriod < 0 {
		errs = append(errs, fmt.Errorf("indicators.obv_slope_period must be >= 0 (got %d)", ind.OBVSlopePeriod))
	}
	if ind.CMFPeriod < 0 {
		errs = append(errs, fmt.Errorf("indicators.cmf_period must be >= 0 (got %d)", ind.CMFPeriod))
	}
	if ind.IchimokuTenkan < 0 {
		errs = append(errs, fmt.Errorf("indicators.ichimoku_tenkan must be >= 0 (got %d)", ind.IchimokuTenkan))
	}
	if ind.IchimokuKijun < 0 {
		errs = append(errs, fmt.Errorf("indicators.ichimoku_kijun must be >= 0 (got %d)", ind.IchimokuKijun))
	}
	if ind.IchimokuSenkouB < 0 {
		errs = append(errs, fmt.Errorf("indicators.ichimoku_senkou_b must be >= 0 (got %d)", ind.IchimokuSenkouB))
	}
	if ind.IchimokuTenkan > 0 && ind.IchimokuKijun > 0 && ind.IchimokuTenkan >= ind.IchimokuKijun {
		errs = append(errs, fmt.Errorf("indicators.ichimoku_tenkan (%d) must be < ichimoku_kijun (%d)", ind.IchimokuTenkan, ind.IchimokuKijun))
	}
	if ind.IchimokuKijun > 0 && ind.IchimokuSenkouB > 0 && ind.IchimokuKijun >= ind.IchimokuSenkouB {
		errs = append(errs, fmt.Errorf("indicators.ichimoku_kijun (%d) must be < ichimoku_senkou_b (%d)", ind.IchimokuKijun, ind.IchimokuSenkouB))
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

	// PR-11: negative Donchian period would compute NaN indefinitely; 0
	// disables the gate and is the safe default.
	if p.SignalRules.Breakout.DonchianPeriod < 0 {
		errs = append(errs, fmt.Errorf("signal_rules.breakout.donchian_period must be >= 0 (got %d)", p.SignalRules.Breakout.DonchianPeriod))
	}

	// PR-9: CMF gate bounds. CMF is in [-1, 1], so any value outside that
	// range either never fires (CMFBuyMin > 1) or always fires (CMFBuyMin
	// < -1), both silent no-ops. Reject to fail loudly.
	if p.SignalRules.Breakout.CMFBuyMin < 0 || p.SignalRules.Breakout.CMFBuyMin > 1 {
		errs = append(errs, fmt.Errorf("signal_rules.breakout.cmf_buy_min must be in [0, 1] (got %v)", p.SignalRules.Breakout.CMFBuyMin))
	}
	if p.SignalRules.Breakout.CMFSellMax < -1 || p.SignalRules.Breakout.CMFSellMax > 0 {
		errs = append(errs, fmt.Errorf("signal_rules.breakout.cmf_sell_max must be in [-1, 0] (got %v)", p.SignalRules.Breakout.CMFSellMax))
	}

	// regime_routing.overrides without a default is also flagged — a
	// non-empty Overrides without Default is almost certainly a typo
	// (the writer meant to set both).
	if p.RegimeRouting != nil && len(p.RegimeRouting.Overrides) > 0 && p.RegimeRouting.Default == "" {
		errs = append(errs, errors.New("regime_routing.default must be set when regime_routing.overrides is non-empty"))
	}

	if ps := p.Risk.PositionSizing; ps != nil {
		switch ps.Mode {
		case "", "fixed", "risk_pct":
		default:
			errs = append(errs, fmt.Errorf("strategy_risk.position_sizing.mode must be one of fixed|risk_pct (got %q)", ps.Mode))
		}
		if ps.RiskPerTradePct < 0 || ps.RiskPerTradePct > 10 {
			errs = append(errs, fmt.Errorf("strategy_risk.position_sizing.risk_per_trade_pct must be in [0, 10] (got %v)", ps.RiskPerTradePct))
		}
		if ps.MaxPositionPctOfEquity < 0 || ps.MaxPositionPctOfEquity > 100 {
			errs = append(errs, fmt.Errorf("strategy_risk.position_sizing.max_position_pct_of_equity must be in [0, 100] (got %v)", ps.MaxPositionPctOfEquity))
		}
		if ps.TargetATRPct < 0 {
			errs = append(errs, fmt.Errorf("strategy_risk.position_sizing.target_atr_pct must be >= 0 (got %v)", ps.TargetATRPct))
		}
		if ps.ATRScaleMin < 0 || ps.ATRScaleMax < 0 || (ps.ATRScaleMin > 0 && ps.ATRScaleMax > 0 && ps.ATRScaleMin > ps.ATRScaleMax) {
			errs = append(errs, fmt.Errorf("strategy_risk.position_sizing.atr_scale_min/max invalid (min=%v max=%v)", ps.ATRScaleMin, ps.ATRScaleMax))
		}
		if ps.MinLot < 0 || ps.LotStep < 0 {
			errs = append(errs, fmt.Errorf("strategy_risk.position_sizing.min_lot/lot_step must be >= 0 (min=%v step=%v)", ps.MinLot, ps.LotStep))
		}
		d := ps.DDScaleDown
		if d.TierAPct < 0 || d.TierBPct < 0 || d.TierAScale < 0 || d.TierBScale < 0 {
			errs = append(errs, fmt.Errorf("strategy_risk.position_sizing.drawdown_scale_down values must be >= 0"))
		}
		if d.TierAPct > 0 && d.TierBPct > 0 && d.TierAPct >= d.TierBPct {
			errs = append(errs, fmt.Errorf("strategy_risk.position_sizing.drawdown_scale_down.tier_a_pct (%v) must be < tier_b_pct (%v)", d.TierAPct, d.TierBPct))
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid strategy profile: %w", errors.Join(errs...))
}
