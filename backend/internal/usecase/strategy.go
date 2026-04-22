package usecase

import (
	"context"
	"math"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// StrategyEngineOptions parameterizes the thresholds and feature toggles used
// by StrategyEngine.
//
// Defaulting rules (see applyDefaults):
//   - Numeric threshold fields (RSIBuyMax, RSISellMin, ContrarianRSIEntry,
//     ContrarianRSIExit, MACDHistogramLimit, BreakoutVolumeRatio,
//     HTFAlignmentBoost) are backfilled with the legacy production constants
//     when they arrive as zero.
//   - Boolean toggles (EnableTrendFollow / EnableContrarian / EnableBreakout /
//     HTFEnabled / HTFBlockCounterTrend / RequireMACDConfirm / RequireEMACross
//     / BreakoutRequireMACDConfirm) are NOT defaulted — they are taken
//     as-supplied so callers can opt out of each gate explicitly.
//
// Constructing StrategyEngineOptions safely:
//   - Call defaultStrategyEngineOptions() to get the legacy-preserving
//     starting point (all bools true, all numerics at production values) and
//     override only the fields you want to change.
//   - Or populate every field you care about explicitly, including every
//     boolean toggle that your profile intends to enable.
//
// Passing a bare StrategyEngineOptions{} to NewStrategyEngineWithOptions
// yields all-false booleans, which effectively disables signal generation
// (every stance branch hits its EnableX==false guard and returns HOLD). This
// is intentional: profile-driven configurations that want, for example,
// EnableTrendFollow=false simply omit it, and nothing flips it back on behind
// the caller's back.
type StrategyEngineOptions struct {
	// Trend-follow thresholds
	RSIBuyMax          float64 // RSI ceiling for TREND_FOLLOW buy; default 70
	RSISellMin         float64 // RSI floor for TREND_FOLLOW sell; default 30
	RequireMACDConfirm bool    // if true, histogram sign gates trend-follow signals; default true
	RequireEMACross    bool    // if true, use EMA-based cross with SMA alignment; default true

	// Contrarian thresholds
	ContrarianRSIEntry float64 // RSI below which contrarian BUYs; default 30
	ContrarianRSIExit  float64 // RSI above which contrarian SELLs; default 70
	MACDHistogramLimit float64 // |histogram| above which contrarian is suppressed; default 10

	// Breakout thresholds
	BreakoutVolumeRatio        float64 // volume ratio required for breakout; default 1.5
	BreakoutRequireMACDConfirm bool    // if true, histogram sign gates breakout signals; default true

	// HTF filter
	HTFEnabled           bool    // if false, skip the HTF filter entirely; default true
	HTFBlockCounterTrend bool    // if true, block signals against higher-TF trend; default true
	HTFAlignmentBoost    float64 // confidence boost when HTF aligns; default 0.1
	// PR-8: selects the HTF trend-detection method ("ema" = legacy SMA20/50,
	// "ichimoku" = price vs. cloud). Empty string defaults to "ema".
	HTFMode string

	// Stance-level feature toggles
	EnableTrendFollow bool // if false, TREND_FOLLOW stance yields HOLD; default true
	EnableContrarian  bool // if false, CONTRARIAN stance yields HOLD; default true
	EnableBreakout    bool // if false, BREAKOUT stance yields HOLD; default true

	// PR-6: ADX-based gates. All zero-by-default so the legacy behaviour
	// (no ADX filtering) is preserved. >0 values activate the gate.
	//   - TrendFollowADXMin: trend-follow signals require ADX >= this.
	//   - ContrarianADXMax:  contrarian signals require ADX <= this.
	//   - BreakoutADXMin:    breakout signals require ADX >= this.
	// Missing ADX (indicator.ADX returned NaN -> nil pointer) treats the
	// gate as a fail, matching the spirit of "if we cannot measure trend
	// strength, don't fire a trend-conditioned signal".
	TrendFollowADXMin float64
	ContrarianADXMax  float64
	BreakoutADXMin    float64

	// PR-7: Stochastics gates on contrarian signals. 0 = gate disabled.
	//   - ContrarianStochEntryMax: contrarian BUY requires %K <= this
	//     (oversold). Typical: 20.
	//   - ContrarianStochExitMin: contrarian SELL requires %K >= this
	//     (overbought). Typical: 80.
	// Missing %K is treated as a failed gate, same philosophy as ADX.
	ContrarianStochEntryMax float64
	ContrarianStochExitMin  float64

	// PR-11: Donchian confirmation on breakout signals. When
	// BreakoutDonchianPeriod > 0, the breakout stance additionally requires
	// lastPrice > Donchian20Upper (BUY) or lastPrice < Donchian20Lower
	// (SELL). Typical value: 20 (matches the IndicatorSet's default
	// Donchian20 fields). 0 keeps the legacy BB-only breakout.
	//
	// Note: the live pipeline currently computes Donchian with period=20
	// only; setting BreakoutDonchianPeriod to any other positive value
	// still activates the gate but will compare against the same
	// Donchian20Upper/Lower pair. Exposing per-bar arbitrary periods is
	// out of scope for PR-11 — the gate is the cheap win; period tuning
	// can be a follow-up once WFO tells us Donchian20 helps at all.
	BreakoutDonchianPeriod int

	// defaulted tracks whether applyDefaults has already been called so we
	// don't flip booleans to true twice (e.g. on a caller that explicitly
	// wants them false).
	defaulted bool
}

// defaultStrategyEngineOptions returns the legacy hard-coded configuration
// preserved exactly as it was before profile-driven overrides were threaded
// through the engine.
func defaultStrategyEngineOptions() StrategyEngineOptions {
	return StrategyEngineOptions{
		RSIBuyMax:                  70,
		RSISellMin:                 30,
		RequireMACDConfirm:         true,
		RequireEMACross:            true,
		ContrarianRSIEntry:         30,
		ContrarianRSIExit:          70,
		MACDHistogramLimit:         10,
		BreakoutVolumeRatio:        1.5,
		BreakoutRequireMACDConfirm: true,
		HTFEnabled:                 true,
		HTFBlockCounterTrend:       true,
		HTFAlignmentBoost:          0.1,
		EnableTrendFollow:          true,
		EnableContrarian:           true,
		EnableBreakout:             true,
		defaulted:                  true,
	}
}

// applyDefaults fills zero-valued numeric fields with legacy constants. It
// does NOT touch boolean toggles: callers that explicitly want a gate
// disabled must pass the bool they want, and callers that want defaults
// should use defaultStrategyEngineOptions() as the starting point.
func (o StrategyEngineOptions) applyDefaults() StrategyEngineOptions {
	if o.defaulted {
		return o
	}
	d := defaultStrategyEngineOptions()
	if o.RSIBuyMax == 0 {
		o.RSIBuyMax = d.RSIBuyMax
	}
	if o.RSISellMin == 0 {
		o.RSISellMin = d.RSISellMin
	}
	if o.ContrarianRSIEntry == 0 {
		o.ContrarianRSIEntry = d.ContrarianRSIEntry
	}
	if o.ContrarianRSIExit == 0 {
		o.ContrarianRSIExit = d.ContrarianRSIExit
	}
	if o.MACDHistogramLimit == 0 {
		o.MACDHistogramLimit = d.MACDHistogramLimit
	}
	if o.BreakoutVolumeRatio == 0 {
		o.BreakoutVolumeRatio = d.BreakoutVolumeRatio
	}
	if o.HTFAlignmentBoost == 0 {
		o.HTFAlignmentBoost = d.HTFAlignmentBoost
	}
	o.defaulted = true
	return o
}

// StrategyEngine はテクニカル指標とスタンスリゾルバの戦略方針を統合して売買シグナルを生成する。
type StrategyEngine struct {
	stanceResolver StanceResolver
	options        StrategyEngineOptions
}

func NewStrategyEngine(stanceResolver StanceResolver) *StrategyEngine {
	return NewStrategyEngineWithOptions(stanceResolver, defaultStrategyEngineOptions())
}

// NewStrategyEngineWithOptions builds a StrategyEngine driven by explicit
// options. Zero-valued numeric fields are backfilled with legacy defaults;
// boolean toggles are taken as-is (see applyDefaults for the split).
func NewStrategyEngineWithOptions(stanceResolver StanceResolver, options StrategyEngineOptions) *StrategyEngine {
	return &StrategyEngine{
		stanceResolver: stanceResolver,
		options:        options.applyDefaults(),
	}
}

// EvaluateWithHigherTF はマルチタイムフレーム分析付きでシグナルを生成する。
// higherTFがnon-nilの場合、Trend Followシグナルが上位トレンドに逆行していればHOLDにフィルタする。
// 上位トレンドと一致している場合はconfidenceを10%ブーストする。
// Contrarianシグナルは意図的に逆張りなのでフィルタしない。
// ボラティリティフィルター: BBバンド幅が非常に狭い(squeeze)場合、Trend Followシグナルを抑制する。
func (e *StrategyEngine) EvaluateWithHigherTF(ctx context.Context, indicators entity.IndicatorSet, higherTF *entity.IndicatorSet, lastPrice float64) (*entity.Signal, error) {
	return e.EvaluateWithHigherTFAt(ctx, indicators, higherTF, lastPrice, time.Now())
}

// EvaluateWithHigherTFAt is a deterministic variant for backtests.
func (e *StrategyEngine) EvaluateWithHigherTFAt(
	ctx context.Context,
	indicators entity.IndicatorSet,
	higherTF *entity.IndicatorSet,
	lastPrice float64,
	now time.Time,
) (*entity.Signal, error) {
	signal, err := e.EvaluateAt(ctx, indicators, lastPrice, now)
	if err != nil || signal.Action == entity.SignalActionHold {
		return signal, err
	}

	result := e.resolveAt(ctx, indicators, lastPrice, now)

	// Low volume filter: reject all signals when volume is extremely low
	if indicators.VolumeRatio != nil && *indicators.VolumeRatio < 0.3 {
		return &entity.Signal{
			SymbolID:  indicators.SymbolID,
			Action:    entity.SignalActionHold,
			Reason:    "volume filter: volume ratio too low, signal unreliable",
			Timestamp: signal.Timestamp,
		}, nil
	}

	// BB position can boost/penalize confidence for contrarian
	if result.Stance == entity.MarketStanceContrarian && indicators.BBLower != nil && indicators.BBUpper != nil {
		if signal.Action == entity.SignalActionBuy && lastPrice <= *indicators.BBLower {
			signal.Confidence = math.Min(signal.Confidence+0.1, 1.0)
		} else if signal.Action == entity.SignalActionSell && lastPrice >= *indicators.BBUpper {
			signal.Confidence = math.Min(signal.Confidence+0.1, 1.0)
		}
	}

	if !e.options.HTFEnabled || higherTF == nil {
		return signal, nil
	}

	// Contrarian and Breakout signals are intentionally allowed against higher TF
	if result.Stance == entity.MarketStanceContrarian || result.Stance == entity.MarketStanceBreakout {
		return signal, nil
	}

	// PR-8: trend direction on the higher timeframe. Three-valued so the
	// ichimoku "inside the cloud" state can block both directions. When
	// the measurement is unavailable (early warmup), we fall through and
	// take no action — same as the legacy "missing SMA => skip" path.
	dir := htfTrendDirection(e.options.HTFMode, higherTF, lastPrice)
	if dir == htfTrendUnknown {
		return signal, nil
	}

	if e.options.HTFBlockCounterTrend {
		if signal.Action == entity.SignalActionBuy && dir != htfTrendUp {
			reason := "MTF filter: higher timeframe downtrend blocks buy"
			if dir == htfTrendNeutral {
				reason = "MTF filter: higher timeframe inside cloud blocks buy"
			}
			return &entity.Signal{
				SymbolID:  indicators.SymbolID,
				Action:    entity.SignalActionHold,
				Reason:    reason,
				Timestamp: signal.Timestamp,
			}, nil
		}
		if signal.Action == entity.SignalActionSell && dir != htfTrendDown {
			reason := "MTF filter: higher timeframe uptrend blocks sell"
			if dir == htfTrendNeutral {
				reason = "MTF filter: higher timeframe inside cloud blocks sell"
			}
			return &entity.Signal{
				SymbolID:  indicators.SymbolID,
				Action:    entity.SignalActionHold,
				Reason:    reason,
				Timestamp: signal.Timestamp,
			}, nil
		}
	}

	// Signal aligns with higher TF: boost confidence by HTFAlignmentBoost
	// (capped at 1.0). Boost only applies when the signal direction matches
	// the higher-timeframe trend direction. Neutral HTF produces no boost.
	aligned := (signal.Action == entity.SignalActionBuy && dir == htfTrendUp) ||
		(signal.Action == entity.SignalActionSell && dir == htfTrendDown)
	if aligned {
		signal.Confidence = math.Min(signal.Confidence+e.options.HTFAlignmentBoost, 1.0)
	}
	return signal, nil
}

type htfTrend int

const (
	htfTrendUnknown htfTrend = iota
	htfTrendUp
	htfTrendDown
	htfTrendNeutral
)

// htfTrendDirection returns the higher-timeframe trend classification using
// the configured HTF mode. "ema" (default) mirrors the legacy
// SMA20>SMA50/SMA20<SMA50 behaviour; "ichimoku" uses the cloud position.
// htfTrendUnknown is returned when the required inputs are unavailable so
// the caller can short-circuit without taking action.
func htfTrendDirection(mode string, higherTF *entity.IndicatorSet, lastPrice float64) htfTrend {
	switch mode {
	case "ichimoku":
		if higherTF == nil || higherTF.Ichimoku == nil {
			return htfTrendUnknown
		}
		ic := higherTF.Ichimoku
		if ic.SenkouA == nil || ic.SenkouB == nil {
			return htfTrendUnknown
		}
		upper := *ic.SenkouA
		lower := *ic.SenkouB
		if lower > upper {
			upper, lower = lower, upper
		}
		switch {
		case lastPrice > upper:
			return htfTrendUp
		case lastPrice < lower:
			return htfTrendDown
		default:
			return htfTrendNeutral
		}
	default:
		// "" and "ema" both use the legacy SMA cross.
		if higherTF == nil || higherTF.SMA20 == nil || higherTF.SMA50 == nil {
			return htfTrendUnknown
		}
		if *higherTF.SMA20 > *higherTF.SMA50 {
			return htfTrendUp
		}
		return htfTrendDown
	}
}

// Evaluate はテクニカル指標と現在価格から売買シグナルを生成する。
// 指標データが不足している場合はHOLDを返す。
func (e *StrategyEngine) Evaluate(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64) (*entity.Signal, error) {
	return e.EvaluateAt(ctx, indicators, lastPrice, time.Now())
}

// EvaluateAt is a deterministic variant for backtests.
func (e *StrategyEngine) EvaluateAt(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64, now time.Time) (*entity.Signal, error) {
	if now.IsZero() {
		now = time.Now()
	}
	nowUnix := now.Unix()

	// 指標チェックを先に行い、不要な処理を防ぐ
	if indicators.SMA20 == nil || indicators.SMA50 == nil || indicators.RSI14 == nil {
		return &entity.Signal{
			SymbolID:  indicators.SymbolID,
			Action:    entity.SignalActionHold,
			Reason:    "insufficient indicator data",
			Timestamp: nowUnix,
		}, nil
	}

	result := e.resolveAt(ctx, indicators, lastPrice, now)

	sma20 := *indicators.SMA20
	sma50 := *indicators.SMA50
	rsi := *indicators.RSI14

	// PR-6: ADX gates run BEFORE the per-stance evaluators so the block
	// reason surfaces cleanly ("trend follow: ADX below threshold")
	// instead of being buried inside evaluate*. A missing ADX value
	// counts as a failed gate when the gate is active.
	adxBlock := func(reason string) *entity.Signal {
		return &entity.Signal{
			SymbolID:  indicators.SymbolID,
			Action:    entity.SignalActionHold,
			Reason:    reason,
			Timestamp: nowUnix,
		}
	}

	switch result.Stance {
	case entity.MarketStanceTrendFollow:
		if !e.options.EnableTrendFollow {
			return &entity.Signal{
				SymbolID:  indicators.SymbolID,
				Action:    entity.SignalActionHold,
				Reason:    "trend follow: disabled by profile",
				Timestamp: nowUnix,
			}, nil
		}
		if min := e.options.TrendFollowADXMin; min > 0 {
			if indicators.ADX14 == nil || *indicators.ADX14 < min {
				return adxBlock("trend follow: ADX below threshold"), nil
			}
		}
		return e.evaluateTrendFollow(indicators.SymbolID, sma20, sma50, rsi, indicators.EMA12, indicators.EMA26, indicators.Histogram, nowUnix), nil
	case entity.MarketStanceContrarian:
		if !e.options.EnableContrarian {
			return &entity.Signal{
				SymbolID:  indicators.SymbolID,
				Action:    entity.SignalActionHold,
				Reason:    "contrarian: disabled by profile",
				Timestamp: nowUnix,
			}, nil
		}
		if max := e.options.ContrarianADXMax; max > 0 {
			// When ADX is unknown we assume a strong trend (worst case
			// for contrarian) and block.
			if indicators.ADX14 == nil || *indicators.ADX14 > max {
				return adxBlock("contrarian: ADX above threshold"), nil
			}
		}
		sig := e.evaluateContrarian(indicators.SymbolID, rsi, indicators.Histogram, nowUnix)
		// PR-7: Stochastics gates apply only when a direction was emitted.
		// BUY requires %K <= StochEntryMax (truly oversold); SELL requires
		// %K >= StochExitMin (truly overbought). Missing %K fails the gate.
		if sig.Action == entity.SignalActionBuy && e.options.ContrarianStochEntryMax > 0 {
			if indicators.StochK14_3 == nil || *indicators.StochK14_3 > e.options.ContrarianStochEntryMax {
				return adxBlock("contrarian: Stoch %K not oversold enough"), nil
			}
		}
		if sig.Action == entity.SignalActionSell && e.options.ContrarianStochExitMin > 0 {
			if indicators.StochK14_3 == nil || *indicators.StochK14_3 < e.options.ContrarianStochExitMin {
				return adxBlock("contrarian: Stoch %K not overbought enough"), nil
			}
		}
		return sig, nil
	case entity.MarketStanceBreakout:
		if !e.options.EnableBreakout {
			return &entity.Signal{
				SymbolID:  indicators.SymbolID,
				Action:    entity.SignalActionHold,
				Reason:    "breakout: disabled by profile",
				Timestamp: nowUnix,
			}, nil
		}
		if min := e.options.BreakoutADXMin; min > 0 {
			if indicators.ADX14 == nil || *indicators.ADX14 < min {
				return adxBlock("breakout: ADX below threshold"), nil
			}
		}
		sig := e.evaluateBreakout(indicators.SymbolID, lastPrice, indicators.BBUpper, indicators.BBLower, indicators.BBMiddle, indicators.VolumeRatio, indicators.Histogram, nowUnix)
		// PR-11: Donchian confirmation. Applies only when a direction was
		// actually emitted — HOLD passes through unchanged. BUY requires
		// lastPrice > Donchian20Upper; SELL requires lastPrice <
		// Donchian20Lower. Missing Donchian (warmup) fails the gate,
		// matching the ADX / Stochastics convention.
		if e.options.BreakoutDonchianPeriod > 0 && sig != nil {
			switch sig.Action {
			case entity.SignalActionBuy:
				if indicators.Donchian20Upper == nil || lastPrice <= *indicators.Donchian20Upper {
					return adxBlock("breakout: price below Donchian upper"), nil
				}
			case entity.SignalActionSell:
				if indicators.Donchian20Lower == nil || lastPrice >= *indicators.Donchian20Lower {
					return adxBlock("breakout: price above Donchian lower"), nil
				}
			}
		}
		return sig, nil
	default:
		return &entity.Signal{
			SymbolID:  indicators.SymbolID,
			Action:    entity.SignalActionHold,
			Reason:    "stance is HOLD",
			Timestamp: nowUnix,
		}, nil
	}
}

func (e *StrategyEngine) resolveAt(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64, now time.Time) StanceResult {
	return e.stanceResolver.ResolveAt(ctx, indicators, lastPrice, now)
}

func (e *StrategyEngine) evaluateTrendFollow(symbolID int64, sma20, sma50, rsi float64, ema12, ema26, histogram *float64, nowUnix int64) *entity.Signal {

	// Primary signal: EMA12/26 crossover (faster than SMA cross), controlled
	// by the RequireEMACross option. When disabled, we skip EMA entirely and
	// only look at SMA20/50. When enabled but EMA is unavailable, we fall
	// back to SMA — matches the original behaviour.
	var fastAboveSlow bool
	var fastBelowSlow bool
	useEMA := e.options.RequireEMACross && ema12 != nil && ema26 != nil

	if useEMA {
		fastAboveSlow = *ema12 > *ema26
		fastBelowSlow = *ema12 < *ema26
	} else {
		fastAboveSlow = sma20 > sma50
		fastBelowSlow = sma20 < sma50
	}

	// SMA filter: require SMA trend alignment when EMA is the primary signal
	if useEMA {
		smaAligned := (fastAboveSlow && sma20 >= sma50) || (fastBelowSlow && sma20 <= sma50)
		if !smaAligned {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "trend follow: EMA cross but SMA not aligned",
				Timestamp: nowUnix,
			}
		}
	}

	if fastAboveSlow && rsi < e.options.RSIBuyMax {
		// MACD histogram confirmation: skip buy if momentum is negative
		if e.options.RequireMACDConfirm && histogram != nil && *histogram < 0 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "trend follow: MACD histogram negative, skipping buy",
				Timestamp: nowUnix,
			}
		}
		reason := "trend follow: EMA12 > EMA26, SMA aligned, RSI not overbought"
		if !useEMA {
			reason = "trend follow: SMA20 > SMA50, RSI not overbought"
		}
		if histogram != nil {
			reason += ", MACD confirmed"
		}
		return &entity.Signal{
			SymbolID:   symbolID,
			Action:     entity.SignalActionBuy,
			Confidence: e.trendFollowConfidence(sma20, sma50, rsi, ema12, ema26, histogram, true),
			Reason:     reason,
			Timestamp:  nowUnix,
		}
	}
	if fastBelowSlow && rsi > e.options.RSISellMin {
		// MACD histogram confirmation: skip sell if momentum is positive
		if e.options.RequireMACDConfirm && histogram != nil && *histogram > 0 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "trend follow: MACD histogram positive, skipping sell",
				Timestamp: nowUnix,
			}
		}
		reason := "trend follow: EMA12 < EMA26, SMA aligned, RSI not oversold"
		if !useEMA {
			reason = "trend follow: SMA20 < SMA50, RSI not oversold"
		}
		if histogram != nil {
			reason += ", MACD confirmed"
		}
		return &entity.Signal{
			SymbolID:   symbolID,
			Action:     entity.SignalActionSell,
			Confidence: e.trendFollowConfidence(sma20, sma50, rsi, ema12, ema26, histogram, false),
			Reason:     reason,
			Timestamp:  nowUnix,
		}
	}
	return &entity.Signal{
		SymbolID:  symbolID,
		Action:    entity.SignalActionHold,
		Reason:    "trend follow: no clear signal",
		Timestamp: nowUnix,
	}
}

// trendFollowConfidence computes a 0.0–1.0 confidence score for trend-follow signals.
// Factors: EMA/SMA divergence (30%), SMA trend strength (15%), RSI headroom (25%), MACD (30%).
//
// RSI headroom is measured against the configured RSIBuyMax / RSISellMin
// thresholds so that profile overrides (e.g. RSIBuyMax=60) feed through to
// confidence as well as gating. The width (RSIBuyMax - RSISellMin) is used as
// the denominator; under default thresholds (70, 30) this preserves the
// legacy 40-wide band exactly.
func (e *StrategyEngine) trendFollowConfidence(sma20, sma50, rsi float64, ema12, ema26, histogram *float64, isBuy bool) float64 {
	// EMA divergence: how strongly the EMA cross is established
	emaDivergence := 0.5
	if ema12 != nil && ema26 != nil && *ema26 != 0 {
		emaDivergence = math.Min(math.Abs(*ema12-*ema26)/math.Abs(*ema26)*100, 2.0) / 2.0
	}

	// SMA trend strength (capped at 2%)
	smaDivergence := math.Min(math.Abs(sma20-sma50)/sma50*100, 2.0) / 2.0

	// RSI headroom: distance from the overbought/oversold boundary, scaled by
	// the configured RSI band width. Guard against a zero/negative band (which
	// would be a misconfiguration) by falling back to 0.0 headroom.
	rsiBand := e.options.RSIBuyMax - e.options.RSISellMin
	var rsiRoom float64
	if rsiBand > 0 {
		if isBuy {
			rsiRoom = (e.options.RSIBuyMax - rsi) / rsiBand
		} else {
			rsiRoom = (rsi - e.options.RSISellMin) / rsiBand
		}
	}
	rsiRoom = math.Max(0, math.Min(1, rsiRoom))

	// MACD histogram confirmation
	macdConfirm := 0.5
	if histogram != nil {
		macdConfirm = math.Min(math.Abs(*histogram)/10, 1.0)
	}

	return emaDivergence*0.3 + smaDivergence*0.15 + rsiRoom*0.25 + macdConfirm*0.3
}

func (e *StrategyEngine) evaluateContrarian(symbolID int64, rsi float64, histogram *float64, nowUnix int64) *entity.Signal {

	if rsi < e.options.ContrarianRSIEntry {
		// Skip contrarian buy if MACD momentum is still strongly negative
		if histogram != nil && *histogram < -e.options.MACDHistogramLimit {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "contrarian: RSI oversold but MACD momentum still strongly negative",
				Timestamp: nowUnix,
			}
		}
		reason := "contrarian: RSI oversold, expecting bounce"
		if histogram != nil {
			reason += ", MACD not strongly against"
		}
		return &entity.Signal{
			SymbolID:   symbolID,
			Action:     entity.SignalActionBuy,
			Confidence: e.contrarianConfidence(rsi, histogram, true),
			Reason:     reason,
			Timestamp:  nowUnix,
		}
	}
	if rsi > e.options.ContrarianRSIExit {
		// Skip contrarian sell if MACD momentum is still strongly positive
		if histogram != nil && *histogram > e.options.MACDHistogramLimit {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "contrarian: RSI overbought but MACD momentum still strongly positive",
				Timestamp: nowUnix,
			}
		}
		reason := "contrarian: RSI overbought, expecting pullback"
		if histogram != nil {
			reason += ", MACD not strongly against"
		}
		return &entity.Signal{
			SymbolID:   symbolID,
			Action:     entity.SignalActionSell,
			Confidence: e.contrarianConfidence(rsi, histogram, false),
			Reason:     reason,
			Timestamp:  nowUnix,
		}
	}
	return &entity.Signal{
		SymbolID:  symbolID,
		Action:    entity.SignalActionHold,
		Reason:    "contrarian: RSI in neutral zone",
		Timestamp: nowUnix,
	}
}

// contrarianConfidence computes a 0.0–1.0 confidence score for contrarian signals.
// Factors: RSI extremity (60%), MACD not-against (40%).
//
// RSI extremity uses the configured ContrarianRSIEntry / ContrarianRSIExit
// thresholds so profile overrides affect confidence. The denominators are
// sized to preserve legacy behaviour under default thresholds (entry=30,
// exit=70): buy-side = ContrarianRSIEntry (i.e. 30), sell-side =
// 100 − ContrarianRSIExit (i.e. 30).
func (e *StrategyEngine) contrarianConfidence(rsi float64, histogram *float64, isBuy bool) float64 {
	// RSI extremity: how deep into oversold/overbought territory. Guard
	// against degenerate thresholds (entry<=0 or exit>=100) that would make
	// the denominator non-positive.
	var rsiExtreme float64
	if isBuy {
		denom := e.options.ContrarianRSIEntry
		if denom > 0 {
			rsiExtreme = (e.options.ContrarianRSIEntry - rsi) / denom
		}
	} else {
		denom := 100.0 - e.options.ContrarianRSIExit
		if denom > 0 {
			rsiExtreme = (rsi - e.options.ContrarianRSIExit) / denom
		}
	}
	rsiExtreme = math.Max(0, math.Min(1, rsiExtreme))

	// MACD not-against: lower opposing momentum = higher confidence
	macdNotAgainst := 0.5 // neutral when histogram unavailable
	if histogram != nil {
		macdNotAgainst = 1.0 - math.Min(math.Abs(*histogram)/20, 1.0)
	}

	return rsiExtreme*0.6 + macdNotAgainst*0.4
}

func (e *StrategyEngine) evaluateBreakout(symbolID int64, lastPrice float64, bbUpper, bbLower, bbMiddle, volumeRatio, histogram *float64, nowUnix int64) *entity.Signal {
	if bbUpper == nil || bbLower == nil || bbMiddle == nil || volumeRatio == nil {
		return &entity.Signal{
			SymbolID:  symbolID,
			Action:    entity.SignalActionHold,
			Reason:    "breakout: insufficient BB/volume data",
			Timestamp: nowUnix,
		}
	}

	if lastPrice > *bbUpper && *volumeRatio >= e.options.BreakoutVolumeRatio {
		// MACD histogram confirmation
		if e.options.BreakoutRequireMACDConfirm && histogram != nil && *histogram < 0 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "breakout: MACD histogram negative, skipping buy",
				Timestamp: nowUnix,
			}
		}
		return &entity.Signal{
			SymbolID:   symbolID,
			Action:     entity.SignalActionBuy,
			Confidence: breakoutConfidence(lastPrice, *bbUpper, *bbMiddle, *volumeRatio, histogram, true),
			Reason:     "breakout: price above BB upper with volume confirmation",
			Timestamp:  nowUnix,
		}
	}

	if lastPrice < *bbLower && *volumeRatio >= e.options.BreakoutVolumeRatio {
		if e.options.BreakoutRequireMACDConfirm && histogram != nil && *histogram > 0 {
			return &entity.Signal{
				SymbolID:  symbolID,
				Action:    entity.SignalActionHold,
				Reason:    "breakout: MACD histogram positive, skipping sell",
				Timestamp: nowUnix,
			}
		}
		return &entity.Signal{
			SymbolID:   symbolID,
			Action:     entity.SignalActionSell,
			Confidence: breakoutConfidence(lastPrice, *bbLower, *bbMiddle, *volumeRatio, histogram, false),
			Reason:     "breakout: price below BB lower with volume confirmation",
			Timestamp:  nowUnix,
		}
	}

	return &entity.Signal{
		SymbolID:  symbolID,
		Action:    entity.SignalActionHold,
		Reason:    "breakout: no clear breakout signal",
		Timestamp: nowUnix,
	}
}

// breakoutConfidence computes a 0.0–1.0 confidence score for breakout signals.
// Factors: volume strength (40%), breakout depth (30%), MACD confirmation (30%).
func breakoutConfidence(lastPrice, bandEdge, bbMiddle, volumeRatio float64, histogram *float64, isBuy bool) float64 {
	// Volume strength: (VolumeRatio - 1.0) / 2.0, capped at 1.0
	volStrength := math.Min((volumeRatio-1.0)/2.0, 1.0)
	if volStrength < 0 {
		volStrength = 0
	}

	// Breakout depth: distance from band edge normalized by BBMiddle
	var depth float64
	if bbMiddle > 0 {
		if isBuy {
			depth = (lastPrice - bandEdge) / bbMiddle
		} else {
			depth = (bandEdge - lastPrice) / bbMiddle
		}
	}
	depth = math.Max(0, math.Min(depth*50, 1.0)) // 2% deviation = 1.0

	// MACD confirmation
	macdConfirm := 0.5
	if histogram != nil {
		macdConfirm = math.Min(math.Abs(*histogram)/10, 1.0)
	}

	return volStrength*0.4 + depth*0.3 + macdConfirm*0.3
}
