package usecase

import (
	"context"
	"math"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/indicator"
)

// IndicatorCalculator computes technical indicators from candlestick data.
type IndicatorCalculator struct {
	repo repository.MarketDataRepository

	// bbSqueezeLookback: cycle44. Legacy default is 5; callers that load
	// a profile should call SetBBSqueezeLookback so stance_rules.
	// bb_squeeze_lookback takes effect. 0 disables RecentSqueeze entirely.
	bbSqueezeLookback int

	// periods drives all indicator lookbacks computed in Calculate. Filled
	// from a StrategyProfile via SetIndicatorPeriods at composition time;
	// zero-valued fields fall back to legacy defaults via
	// IndicatorConfig.WithDefaults so existing call sites behave the same
	// without a profile.
	periods entity.IndicatorConfig
}

func NewIndicatorCalculator(repo repository.MarketDataRepository) *IndicatorCalculator {
	return &IndicatorCalculator{
		repo:              repo,
		bbSqueezeLookback: 5,
		periods:           entity.IndicatorConfig{}.WithDefaults(),
	}
}

// SetBBSqueezeLookback lets the composition root override the window
// used for RecentSqueeze. Mirrors IndicatorHandler.SetBBSqueezeLookback
// so the live and backtest pipelines honour the same profile knob.
func (c *IndicatorCalculator) SetBBSqueezeLookback(n int) {
	if n < 0 {
		n = 0
	}
	c.bbSqueezeLookback = n
}

// SetIndicatorPeriods overrides the lookback periods used for SMA / EMA /
// RSI / MACD / BB / ATR / VolumeSMA. Zero-valued fields are filled in from
// the legacy hardcoded defaults via IndicatorConfig.WithDefaults so a
// partial profile still produces a working set.
//
// PR-C will extend this to ADX / Stochastics / Donchian / CMF / OBVSlope /
// Ichimoku; until then those continue to use their hardcoded periods.
func (c *IndicatorCalculator) SetIndicatorPeriods(p entity.IndicatorConfig) {
	c.periods = p.WithDefaults()
}

// Calculate computes all technical indicators for the given symbol and interval.
// Retrieves candles from the repository and calculates SMA, EMA, RSI, MACD.
// Indicators that cannot be calculated due to insufficient data are nil.
func (c *IndicatorCalculator) Calculate(ctx context.Context, symbolID int64, interval string) (*entity.IndicatorSet, error) {
	// EMA/RSI/MACDはパス依存型指標のため、十分なウォームアップ期間が必要。
	// EMASlowは約3倍(78本)、MACD Signal(9)の追加で約90本のウォームアップ。
	// 500本取得すれば実用上十分な精度に収束する。
	candles, err := c.repo.GetCandles(ctx, symbolID, interval, 500, 0)
	if err != nil {
		return nil, err
	}

	// GetCandles returns newest-first, reverse to oldest-first for calculations
	n := len(candles)
	prices := make([]float64, n)
	highs := make([]float64, n)
	lows := make([]float64, n)
	for i, cd := range candles {
		prices[n-1-i] = cd.Close
		highs[n-1-i] = cd.High
		lows[n-1-i] = cd.Low
	}

	var timestamp int64
	if len(candles) > 0 {
		timestamp = candles[0].Time // newest candle's timestamp
	}

	periods := c.periods.WithDefaults()

	result := &entity.IndicatorSet{
		SymbolID:  symbolID,
		SMAShort:  toPtr(indicator.SMA(prices, periods.SMAShort)),
		SMALong:   toPtr(indicator.SMA(prices, periods.SMALong)),
		EMAFast:   toPtr(indicator.EMA(prices, periods.EMAFast)),
		EMASlow:   toPtr(indicator.EMA(prices, periods.EMASlow)),
		RSI:       toPtr(indicator.RSI(prices, periods.RSIPeriod)),
		Timestamp: timestamp,
	}

	macdLine, signalLine, histogram := indicator.MACD(prices, periods.MACDFast, periods.MACDSlow, periods.MACDSignal)
	result.MACDLine = toPtr(macdLine)
	result.SignalLine = toPtr(signalLine)
	result.Histogram = toPtr(histogram)

	bbUpper, bbMiddle, bbLower, bbBandwidth := indicator.BollingerBands(prices, periods.BBPeriod, periods.BBMultiplier)
	result.BBUpper = toPtr(bbUpper)
	result.BBMiddle = toPtr(bbMiddle)
	result.BBLower = toPtr(bbLower)
	result.BBBandwidth = toPtr(bbBandwidth)

	result.ATR = toPtr(indicator.ATR(highs, lows, prices, periods.ATRPeriod))

	// PR-6: ADX family. ADX/PlusDI/MinusDI return NaN until 2*period+1
	// bars are available; toPtr collapses that to nil for the caller.
	// PR-C will profile-drive this period.
	adxVal, plusDI, minusDI := indicator.ADX(highs, lows, prices, 14)
	result.ADX = toPtr(adxVal)
	result.PlusDI = toPtr(plusDI)
	result.MinusDI = toPtr(minusDI)

	// PR-7: Stochastics (14, 3, 3) + Stochastic RSI (14, 14). Both return
	// NaN -> nil pointer when the warmup window is not filled yet.
	// PR-C will profile-drive these periods.
	stochK, stochD := indicator.Stochastics(highs, lows, prices, 14, 3, 3)
	result.StochK = toPtr(stochK)
	result.StochD = toPtr(stochD)
	result.StochRSI = toPtr(indicator.StochasticRSI(prices, 14, 14))

	// PR-8: Ichimoku. Each of the five lines may be NaN independently during
	// warmup; buildIchimokuSnapshot returns nil when every line is unknown.
	// PR-C will profile-drive these periods.
	if snap := buildIchimokuSnapshot(indicator.Ichimoku(highs, lows, prices, 9, 26, 52)); snap != nil {
		result.Ichimoku = snap
	}

	// PR-11: Donchian Channel (20-bar default). Mirror the other range-of-N
	// indicators — NaN until 20 bars of history are available; toPtr
	// collapses that into nil pointers for downstream gates.
	// PR-C will profile-drive this period.
	donU, donL, donM := indicator.Donchian(highs, lows, 20)
	result.DonchianUpper = toPtr(donU)
	result.DonchianLower = toPtr(donL)
	result.DonchianMiddle = toPtr(donM)

	// Volume indicators
	volumes := make([]float64, n)
	for i, cd := range candles {
		volumes[n-1-i] = cd.Volume
	}
	volSMA := indicator.VolumeSMA(volumes, periods.VolumeSMAPeriod)
	result.VolumeSMA = toPtr(volSMA)
	if !math.IsNaN(volSMA) && volSMA > 0 && n > 0 {
		vr := indicator.VolumeRatio(volumes[n-1], volSMA)
		result.VolumeRatio = toPtr(vr)
	}

	// PR-9: OBV + CMF (volume-based). OBVSlope carries the gate signal
	// (cumulative buying volume over 20 bars); raw OBV is exposed for
	// diagnostics / frontend charting. CMF is bounded in [-1, 1].
	// PR-C will profile-drive these periods.
	result.OBV = toPtr(indicator.OBV(prices, volumes))
	result.OBVSlope = toPtr(indicator.OBVSlope(prices, volumes, 20))
	result.CMF = toPtr(indicator.CMF(highs, lows, prices, volumes, 20))

	// RecentSqueeze: check if any of the last `c.bbSqueezeLookback`
	// candles had BBBandwidth < 0.02. cycle44: profile's stance_rules
	// now drives this via SetBBSqueezeLookback; 0 disables the gate
	// (RecentSqueeze stays nil). bb period must match the BB calculation
	// above so the squeeze window is consistent.
	if n >= periods.BBPeriod && c.bbSqueezeLookback > 0 {
		recentSqueeze := false
		lookback := c.bbSqueezeLookback
		if lookback > n-(periods.BBPeriod-1) {
			lookback = n - (periods.BBPeriod - 1)
		}
		for i := 0; i < lookback; i++ {
			offset := n - 1 - i
			windowPrices := prices[:offset+1]
			_, _, _, bw := indicator.BollingerBands(windowPrices, periods.BBPeriod, periods.BBMultiplier)
			if !math.IsNaN(bw) && bw < 0.02 {
				recentSqueeze = true
				break
			}
		}
		result.RecentSqueeze = &recentSqueeze
	}

	return result, nil
}

// toPtr converts a float64 to *float64. Returns nil if the value is NaN.
func toPtr(v float64) *float64 {
	if math.IsNaN(v) {
		return nil
	}
	return &v
}

// buildIchimokuSnapshot maps an indicator.IchimokuResult onto the entity
// pointer snapshot. Returns nil when every line is NaN (pure warmup state)
// so consumers can cheaply branch on `if snap := ...; snap != nil`.
func buildIchimokuSnapshot(r indicator.IchimokuResult) *entity.IchimokuSnapshot {
	snap := &entity.IchimokuSnapshot{
		Tenkan:  toPtr(r.Tenkan),
		Kijun:   toPtr(r.Kijun),
		SenkouA: toPtr(r.SenkouA),
		SenkouB: toPtr(r.SenkouB),
		Chikou:  toPtr(r.Chikou),
	}
	if snap.Tenkan == nil && snap.Kijun == nil && snap.SenkouA == nil && snap.SenkouB == nil && snap.Chikou == nil {
		return nil
	}
	return snap
}
