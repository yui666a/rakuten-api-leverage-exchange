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

// ExpandGrid computes the full cartesian product of the supplied
// parameter axes. An empty slice returns exactly one empty combination
// so callers can always iterate it (baseline-only sweeps just work).
func ExpandGrid(overrides []ParameterOverride) ([]map[string]float64, error) {
	if len(overrides) == 0 {
		return []map[string]float64{{}}, nil
	}

	// Reject duplicate paths up-front: a later axis would silently overwrite
	// the earlier one in the combo map, producing visible combo count >
	// distinct combos and a scoring signal that is not what the caller
	// asked for.
	seenPaths := make(map[string]struct{}, len(overrides))
	total := 1
	for _, o := range overrides {
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
	if total > MaxWalkForwardGridSize {
		return nil, fmt.Errorf("walk-forward: grid size %d exceeds MAX_GRID_SIZE=%d",
			total, MaxWalkForwardGridSize)
	}

	out := make([]map[string]float64, 0, total)
	indices := make([]int, len(overrides))
	for {
		combo := make(map[string]float64, len(overrides))
		for i, o := range overrides {
			combo[o.Path] = o.Values[indices[i]]
		}
		out = append(out, combo)

		j := len(overrides) - 1
		for j >= 0 {
			indices[j]++
			if indices[j] < len(overrides[j].Values) {
				break
			}
			indices[j] = 0
			j--
		}
		if j < 0 {
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
//   strategy_risk.stop_loss_percent
//   strategy_risk.take_profit_percent
//   strategy_risk.stop_loss_atr_multiplier
//   strategy_risk.trailing_atr_multiplier
//   strategy_risk.max_position_amount
//   strategy_risk.max_daily_loss
//   signal_rules.trend_follow.{rsi_buy_max,rsi_sell_min,adx_min}
//   signal_rules.contrarian.{rsi_entry,rsi_exit,macd_histogram_limit,adx_max,stoch_entry_max,stoch_exit_min}
//   signal_rules.breakout.{volume_ratio_min,adx_min}
//   stance_rules.{rsi_oversold,rsi_overbought,sma_convergence_threshold,breakout_volume_ratio}
//   htf_filter.alignment_boost
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
		default:
			return entity.StrategyProfile{}, fmt.Errorf("walk-forward: unsupported override path %q", path)
		}
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
