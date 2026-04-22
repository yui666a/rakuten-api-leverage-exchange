package backtest

import (
	"fmt"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// ParameterOverride declares one parameter axis for a walk-forward grid.
// Path is a dot-separated override location understood by
// ApplyOverrides (e.g. "strategy_risk.stop_loss_percent"). Values is the
// set of candidate values to try along that axis.
type ParameterOverride struct {
	Path   string    `json:"path"`
	Values []float64 `json:"values"`
}

// ParameterStringOverride declares one string-valued parameter axis.
// String axes run alongside ParameterOverride in the combined grid but
// route to ApplyStringOverrides at combo-apply time. Kept as a separate
// shape so the numeric API contract (including the bestParameters /
// parameters fields persisted in the WFO envelope and rendered by the
// frontend) stays as map[string]float64 for every existing caller.
type ParameterStringOverride struct {
	Path   string   `json:"path"`
	Values []string `json:"values"`
}

// GridCombination is one expanded grid cell. Numeric and String are
// applied in sequence (ApplyOverrides → ApplyStringOverrides) so either
// axis can participate in the same grid.
type GridCombination struct {
	Numeric map[string]float64
	String  map[string]string
}

// WalkForwardWindow is one (in-sample, out-of-sample) slice of the walk-
// forward schedule. The in-sample window is used to select best grid
// parameters; the out-of-sample window is used to score them.
type WalkForwardWindow struct {
	Index        int       `json:"index"`
	InSampleFrom time.Time `json:"inSampleFrom"`
	InSampleTo   time.Time `json:"inSampleTo"`
	OOSFrom      time.Time `json:"oosFrom"`
	OOSTo        time.Time `json:"oosTo"`
}

// MaxWalkForwardGridSize bounds the total number of parameter combinations
// produced by ExpandGrid.
const MaxWalkForwardGridSize = 100

// ComputeWindows builds a list of non-overlapping in-sample / out-of-
// sample slices across [from, to] using month-based arithmetic.
func ComputeWindows(from, to time.Time, inSampleMonths, oosMonths, stepMonths int) ([]WalkForwardWindow, error) {
	if inSampleMonths <= 0 {
		return nil, fmt.Errorf("walk-forward: inSampleMonths must be > 0")
	}
	if oosMonths <= 0 {
		return nil, fmt.Errorf("walk-forward: oosMonths must be > 0")
	}
	if stepMonths <= 0 {
		return nil, fmt.Errorf("walk-forward: stepMonths must be > 0")
	}
	if !from.Before(to) {
		return nil, fmt.Errorf("walk-forward: from must be before to")
	}

	// addMonthsClamped is used instead of time.Time.AddDate directly because
	// AddDate normalises an out-of-range day into the next month:
	// e.g. 2024-01-31 + 1m => 2024-03-02, which silently skips February
	// and progressively drifts later windows. Clamping to the target
	// month's last day keeps month-aligned schedules stable through
	// 30/31-day and leap-year boundaries.
	var out []WalkForwardWindow
	idx := 0
	isStart := from
	for {
		isEnd := addMonthsClamped(isStart, inSampleMonths)
		oosEnd := addMonthsClamped(isEnd, oosMonths)
		if oosEnd.After(to) {
			break
		}
		out = append(out, WalkForwardWindow{
			Index:        idx,
			InSampleFrom: isStart,
			InSampleTo:   isEnd,
			OOSFrom:      isEnd,
			OOSTo:        oosEnd,
		})
		idx++
		isStart = addMonthsClamped(isStart, stepMonths)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("walk-forward: [%s, %s] is too short for in=%d oos=%d months",
			from.Format("2006-01-02"), to.Format("2006-01-02"), inSampleMonths, oosMonths)
	}
	return out, nil
}

// addMonthsClamped adds n calendar months to t with two invariants the
// stdlib time.Time.AddDate violates:
//
//  1. If t is already on the last day of its month, the result is also
//     on the last day of the target month. e.g. 2024-01-31 + 1m =>
//     2024-02-29 (and subsequent monthly steps stay anchored on the last
//     day, never drifting to 02-29 -> 03-29 -> 04-29).
//  2. If t's day-of-month does not exist in the target month, clamp to
//     the target's last day. e.g. 2024-03-31 + 1m => 2024-04-30.
//
// Without these, AddDate(0,1,0) on 2024-01-31 returns 2024-03-02, which
// silently skips February and breaks any walk-forward schedule whose
// cadence depends on "month +1" meaning "same point of the next month".
func addMonthsClamped(t time.Time, months int) time.Time {
	y, m, d := t.Date()

	// Normalise target year/month with a negative-safe modulo.
	total := int(m) - 1 + months
	targetYear := y + total/12
	targetMonthIdx := total % 12
	if targetMonthIdx < 0 {
		targetYear--
		targetMonthIdx += 12
	}
	targetMonth := time.Month(targetMonthIdx + 1)

	// Last day of source month and target month: "day 0 of next month".
	srcLastDay := time.Date(y, m+1, 0, 0, 0, 0, 0, t.Location()).Day()
	targetLastDay := time.Date(targetYear, targetMonth+1, 0, 0, 0, 0, 0, t.Location()).Day()

	switch {
	case d == srcLastDay:
		// Month-end anchor: stay on month-end across calls.
		d = targetLastDay
	case d > targetLastDay:
		// Source day doesn't exist in target month — clamp down.
		d = targetLastDay
	}

	return time.Date(targetYear, targetMonth, d,
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// ExpandGrid computes the full cartesian product of the supplied numeric
// parameter axes. An empty slice returns exactly one empty combination so
// callers can always iterate it (baseline-only sweeps just work).
//
// Kept for backwards compatibility with callers that only use numeric
// axes. For mixed numeric+string grids, use ExpandCombinedGrid.
func ExpandGrid(overrides []ParameterOverride) ([]map[string]float64, error) {
	combos, err := ExpandCombinedGrid(overrides, nil)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]float64, 0, len(combos))
	for _, c := range combos {
		out = append(out, c.Numeric)
	}
	return out, nil
}

// ExpandCombinedGrid computes the cartesian product of numeric and
// string parameter axes. Combinations are produced in a deterministic
// order (numeric axes before string axes, right-most index varying
// fastest) so grid-rank comparisons between runs stay stable.
//
// Duplicate paths — whether within numeric, within string, or across
// the two — are rejected: a later axis would silently overwrite an
// earlier one when applied to the profile, producing a visible combo
// count greater than distinct combos and a scoring signal that is not
// what the caller asked for.
func ExpandCombinedGrid(numeric []ParameterOverride, strs []ParameterStringOverride) ([]GridCombination, error) {
	if len(numeric) == 0 && len(strs) == 0 {
		return []GridCombination{{
			Numeric: map[string]float64{},
			String:  map[string]string{},
		}}, nil
	}

	seenPaths := make(map[string]struct{}, len(numeric)+len(strs))
	total := 1
	for _, o := range numeric {
		if o.Path == "" {
			return nil, fmt.Errorf("walk-forward: override path must not be empty")
		}
		if _, dup := seenPaths[o.Path]; dup {
			return nil, fmt.Errorf("walk-forward: duplicate override path %q", o.Path)
		}
		seenPaths[o.Path] = struct{}{}
		if len(o.Values) == 0 {
			return nil, fmt.Errorf("walk-forward: override %q has no values", o.Path)
		}
		total *= len(o.Values)
	}
	for _, o := range strs {
		if o.Path == "" {
			return nil, fmt.Errorf("walk-forward: override path must not be empty")
		}
		if _, dup := seenPaths[o.Path]; dup {
			return nil, fmt.Errorf("walk-forward: duplicate override path %q", o.Path)
		}
		seenPaths[o.Path] = struct{}{}
		if len(o.Values) == 0 {
			return nil, fmt.Errorf("walk-forward: override %q has no values", o.Path)
		}
		for _, v := range o.Values {
			if v == "" {
				return nil, fmt.Errorf("walk-forward: override %q has empty string value", o.Path)
			}
		}
		total *= len(o.Values)
	}
	if total > MaxWalkForwardGridSize {
		return nil, fmt.Errorf("walk-forward: grid size %d exceeds MAX_GRID_SIZE=%d",
			total, MaxWalkForwardGridSize)
	}

	out := make([]GridCombination, 0, total)
	nIdx := make([]int, len(numeric))
	sIdx := make([]int, len(strs))
	for {
		combo := GridCombination{
			Numeric: make(map[string]float64, len(numeric)),
			String:  make(map[string]string, len(strs)),
		}
		for i, o := range numeric {
			combo.Numeric[o.Path] = o.Values[nIdx[i]]
		}
		for i, o := range strs {
			combo.String[o.Path] = o.Values[sIdx[i]]
		}
		out = append(out, combo)

		// Advance the combined index (string axes first from the right,
		// then numeric axes) so string mode swaps hold the numeric cell
		// steady for one full string pass before stepping numeric.
		advanced := false
		for j := len(strs) - 1; j >= 0; j-- {
			sIdx[j]++
			if sIdx[j] < len(strs[j].Values) {
				advanced = true
				break
			}
			sIdx[j] = 0
		}
		if !advanced {
			for j := len(numeric) - 1; j >= 0; j-- {
				nIdx[j]++
				if nIdx[j] < len(numeric[j].Values) {
					advanced = true
					break
				}
				nIdx[j] = 0
			}
		}
		if !advanced {
			break
		}
	}
	return out, nil
}

// ApplyOverrides returns a deep copy of base with the requested
// dot-separated fields set to their override values.
//
// Supported override paths (keep this comment in lockstep with the switch
// below — unknown paths error out, so the switch is the authoritative list):
//
//	strategy_risk.stop_loss_percent
//	strategy_risk.take_profit_percent
//	strategy_risk.stop_loss_atr_multiplier
//	strategy_risk.trailing_atr_multiplier
//	strategy_risk.max_position_amount
//	strategy_risk.max_daily_loss
//	signal_rules.trend_follow.{rsi_buy_max,rsi_sell_min,adx_min}
//	signal_rules.contrarian.{rsi_entry,rsi_exit,macd_histogram_limit,adx_max,stoch_entry_max,stoch_exit_min}
//	signal_rules.breakout.{volume_ratio_min,adx_min,donchian_period}
//	stance_rules.{rsi_oversold,rsi_overbought,sma_convergence_threshold,breakout_volume_ratio}
//	htf_filter.alignment_boost
//	regime_routing.detector_config.trend_adx_min
//	regime_routing.detector_config.volatile_atr_percent_min
//	regime_routing.detector_config.hysteresis_bars
//
// String-valued axes (e.g. htf_filter.mode) live on a separate path via
// ApplyStringOverrides so the numeric API contract stays purely float64.
func ApplyOverrides(base entity.StrategyProfile, overrides map[string]float64) (entity.StrategyProfile, error) {
	out := base
	for path, value := range overrides {
		switch path {
		case "strategy_risk.stop_loss_percent":
			out.Risk.StopLossPercent = value
		case "strategy_risk.take_profit_percent":
			out.Risk.TakeProfitPercent = value
		case "strategy_risk.stop_loss_atr_multiplier":
			out.Risk.StopLossATRMultiplier = value
		case "strategy_risk.trailing_atr_multiplier":
			out.Risk.TrailingATRMultiplier = value
		case "strategy_risk.max_position_amount":
			out.Risk.MaxPositionAmount = value
		case "strategy_risk.max_daily_loss":
			out.Risk.MaxDailyLoss = value
		case "signal_rules.trend_follow.rsi_buy_max":
			out.SignalRules.TrendFollow.RSIBuyMax = value
		case "signal_rules.trend_follow.rsi_sell_min":
			out.SignalRules.TrendFollow.RSISellMin = value
		case "signal_rules.trend_follow.adx_min":
			out.SignalRules.TrendFollow.ADXMin = value
		case "signal_rules.contrarian.rsi_entry":
			out.SignalRules.Contrarian.RSIEntry = value
		case "signal_rules.contrarian.rsi_exit":
			out.SignalRules.Contrarian.RSIExit = value
		case "signal_rules.contrarian.macd_histogram_limit":
			out.SignalRules.Contrarian.MACDHistogramLimit = value
		case "signal_rules.contrarian.adx_max":
			out.SignalRules.Contrarian.ADXMax = value
		case "signal_rules.contrarian.stoch_entry_max":
			out.SignalRules.Contrarian.StochEntryMax = value
		case "signal_rules.contrarian.stoch_exit_min":
			out.SignalRules.Contrarian.StochExitMin = value
		case "signal_rules.breakout.volume_ratio_min":
			out.SignalRules.Breakout.VolumeRatioMin = value
		case "signal_rules.breakout.adx_min":
			out.SignalRules.Breakout.ADXMin = value
		case "signal_rules.breakout.donchian_period":
			// Period is an int; a fractional grid value would silently
			// truncate, mirroring the existing "no silent rounding"
			// contract used for regime_routing.detector_config.hysteresis_bars.
			if value != float64(int(value)) {
				return entity.StrategyProfile{}, fmt.Errorf("walk-forward: signal_rules.breakout.donchian_period must be an integer (got %v)", value)
			}
			if value < 0 {
				return entity.StrategyProfile{}, fmt.Errorf("walk-forward: signal_rules.breakout.donchian_period must be >= 0 (got %v)", value)
			}
			out.SignalRules.Breakout.DonchianPeriod = int(value)
		case "stance_rules.rsi_oversold":
			out.StanceRules.RSIOversold = value
		case "stance_rules.rsi_overbought":
			out.StanceRules.RSIOverbought = value
		case "stance_rules.sma_convergence_threshold":
			out.StanceRules.SMAConvergenceThreshold = value
		case "stance_rules.breakout_volume_ratio":
			out.StanceRules.BreakoutVolumeRatio = value
		case "htf_filter.alignment_boost":
			out.HTFFilter.AlignmentBoost = value
		case "regime_routing.detector_config.trend_adx_min",
			"regime_routing.detector_config.volatile_atr_percent_min",
			"regime_routing.detector_config.hysteresis_bars":
			// Routing detector thresholds: only meaningful when the
			// profile is a router. We allocate the nested config on
			// demand so a non-router profile that happens to receive
			// this path through a typo still surfaces the override
			// path itself as supported (the strategy builder will
			// then ignore detector_config for flat profiles).
			if out.RegimeRouting == nil {
				out.RegimeRouting = &entity.RegimeRoutingConfig{}
			}
			if out.RegimeRouting.DetectorConfig == nil {
				out.RegimeRouting.DetectorConfig = &entity.RegimeDetectorConfig{}
			}
			switch path {
			case "regime_routing.detector_config.trend_adx_min":
				out.RegimeRouting.DetectorConfig.TrendADXMin = value
			case "regime_routing.detector_config.volatile_atr_percent_min":
				out.RegimeRouting.DetectorConfig.VolatileATRPercentMin = value
			case "regime_routing.detector_config.hysteresis_bars":
				// HysteresisBars is an int — a fractional grid value
				// would silently truncate, so reject up front to
				// match the existing "no silent rounding" contract
				// for grid axes.
				if value != float64(int(value)) {
					return entity.StrategyProfile{}, fmt.Errorf("walk-forward: regime_routing.detector_config.hysteresis_bars must be an integer (got %v)", value)
				}
				out.RegimeRouting.DetectorConfig.HysteresisBars = int(value)
			}
		default:
			return entity.StrategyProfile{}, fmt.Errorf("walk-forward: unsupported override path %q", path)
		}
	}
	return out, nil
}

// ApplyStringOverrides returns a deep copy of base with the requested
// dot-separated string-valued fields set to their override values.
//
// Supported override paths (keep this comment in lockstep with the switch
// below — unknown paths error out, so the switch is the authoritative list):
//
//	htf_filter.mode   values: "" | "ema" | "ichimoku"
//
// The allowed values for each path are enforced here at the override
// boundary rather than in StrategyProfile.Validate() so pre-existing
// profiles that stored a blank or legacy string keep loading, but a WFO
// grid with a typo fails fast instead of silently running "ema".
func ApplyStringOverrides(base entity.StrategyProfile, overrides map[string]string) (entity.StrategyProfile, error) {
	out := base
	for path, value := range overrides {
		switch path {
		case "htf_filter.mode":
			switch value {
			case "", "ema", "ichimoku":
				out.HTFFilter.Mode = value
			default:
				return entity.StrategyProfile{}, fmt.Errorf(
					"walk-forward: unsupported value %q for %q (want \"\", \"ema\", or \"ichimoku\")",
					value, path)
			}
		default:
			return entity.StrategyProfile{}, fmt.Errorf("walk-forward: unsupported string override path %q", path)
		}
	}
	return out, nil
}

// ApplyCombination applies both numeric and string overrides from one
// expanded grid cell. Convenience wrapper used by WalkForwardRunner so
// the runner doesn't have to thread two maps through every call site.
func ApplyCombination(base entity.StrategyProfile, combo GridCombination) (entity.StrategyProfile, error) {
	out, err := ApplyOverrides(base, combo.Numeric)
	if err != nil {
		return entity.StrategyProfile{}, err
	}
	out, err = ApplyStringOverrides(out, combo.String)
	if err != nil {
		return entity.StrategyProfile{}, err
	}
	return out, nil
}

// SelectByObjective returns the scalar score walk-forward uses to compare
// grid combinations. Unknown names fall through to "return" so a typo
// doesn't silently change the scoring axis.
func SelectByObjective(s entity.BacktestSummary, objective string) float64 {
	switch objective {
	case "sharpe":
		return s.SharpeRatio
	case "profit_factor":
		return s.ProfitFactor
	case "return", "":
		return s.TotalReturn
	default:
		return s.TotalReturn
	}
}
