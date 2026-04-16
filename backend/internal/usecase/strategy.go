package usecase

import (
	"context"
	"math"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// StrategyEngineOptions parameterizes the thresholds and feature toggles used
// by StrategyEngine. Zero values are replaced with production defaults by
// applyDefaults, so callers can construct a partially-populated options struct
// and still get sensible behaviour — but production code paths that create
// StrategyEngine via NewStrategyEngine inherit every default implicitly.
//
// Boolean feature toggles (EnableTrendFollow / EnableContrarian /
// EnableBreakout / HTFEnabled / HTFBlockCounterTrend / RequireMACDConfirm /
// RequireEMACross / BreakoutRequireMACDConfirm) default to true because the
// current hard-coded StrategyEngine behaviour applies each of those gates.
// They are expressed as plain bools rather than *bool because the defaulting
// code explicitly flips them to true when they arrive as false AND the struct
// is otherwise empty — see applyDefaults.
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

	// Stance-level feature toggles
	EnableTrendFollow bool // if false, TREND_FOLLOW stance yields HOLD; default true
	EnableContrarian  bool // if false, CONTRARIAN stance yields HOLD; default true
	EnableBreakout    bool // if false, BREAKOUT stance yields HOLD; default true

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

	if !e.options.HTFEnabled || higherTF == nil || higherTF.SMA20 == nil || higherTF.SMA50 == nil {
		return signal, nil
	}

	// Contrarian and Breakout signals are intentionally allowed against higher TF
	if result.Stance == entity.MarketStanceContrarian || result.Stance == entity.MarketStanceBreakout {
		return signal, nil
	}

	higherUptrend := *higherTF.SMA20 > *higherTF.SMA50

	if e.options.HTFBlockCounterTrend {
		if signal.Action == entity.SignalActionBuy && !higherUptrend {
			return &entity.Signal{
				SymbolID:  indicators.SymbolID,
				Action:    entity.SignalActionHold,
				Reason:    "MTF filter: higher timeframe downtrend blocks buy",
				Timestamp: signal.Timestamp,
			}, nil
		}
		if signal.Action == entity.SignalActionSell && higherUptrend {
			return &entity.Signal{
				SymbolID:  indicators.SymbolID,
				Action:    entity.SignalActionHold,
				Reason:    "MTF filter: higher timeframe uptrend blocks sell",
				Timestamp: signal.Timestamp,
			}, nil
		}
	}

	// Signal aligns with higher TF: boost confidence by HTFAlignmentBoost
	// (capped at 1.0). Boost only applies when the signal direction matches
	// the higher-timeframe trend direction.
	aligned := (signal.Action == entity.SignalActionBuy && higherUptrend) ||
		(signal.Action == entity.SignalActionSell && !higherUptrend)
	if aligned {
		signal.Confidence = math.Min(signal.Confidence+e.options.HTFAlignmentBoost, 1.0)
	}
	return signal, nil
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
		return e.evaluateContrarian(indicators.SymbolID, rsi, indicators.Histogram, nowUnix), nil
	case entity.MarketStanceBreakout:
		if !e.options.EnableBreakout {
			return &entity.Signal{
				SymbolID:  indicators.SymbolID,
				Action:    entity.SignalActionHold,
				Reason:    "breakout: disabled by profile",
				Timestamp: nowUnix,
			}, nil
		}
		return e.evaluateBreakout(indicators.SymbolID, lastPrice, indicators.BBUpper, indicators.BBLower, indicators.BBMiddle, indicators.VolumeRatio, indicators.Histogram, nowUnix), nil
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
			Confidence: trendFollowConfidence(sma20, sma50, rsi, ema12, ema26, histogram, true),
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
			Confidence: trendFollowConfidence(sma20, sma50, rsi, ema12, ema26, histogram, false),
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
func trendFollowConfidence(sma20, sma50, rsi float64, ema12, ema26, histogram *float64, isBuy bool) float64 {
	// EMA divergence: how strongly the EMA cross is established
	emaDivergence := 0.5
	if ema12 != nil && ema26 != nil && *ema26 != 0 {
		emaDivergence = math.Min(math.Abs(*ema12-*ema26)/math.Abs(*ema26)*100, 2.0) / 2.0
	}

	// SMA trend strength (capped at 2%)
	smaDivergence := math.Min(math.Abs(sma20-sma50)/sma50*100, 2.0) / 2.0

	// RSI headroom: distance from the overbought/oversold boundary
	var rsiRoom float64
	if isBuy {
		rsiRoom = (70 - rsi) / 40
	} else {
		rsiRoom = (rsi - 30) / 40
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
			Confidence: contrarianConfidence(rsi, histogram, true),
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
			Confidence: contrarianConfidence(rsi, histogram, false),
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
func contrarianConfidence(rsi float64, histogram *float64, isBuy bool) float64 {
	// RSI extremity: how deep into oversold/overbought territory
	var rsiExtreme float64
	if isBuy {
		rsiExtreme = (30 - rsi) / 30 // 0→1.0, 30→0.0
	} else {
		rsiExtreme = (rsi - 70) / 30 // 100→1.0, 70→0.0
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
