package backtest

import (
	"math"
	"sort"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// DefaultDrawdownThreshold is the minimum depth (fraction of peak equity)
// at which a drawdown is recorded. 2% is aggressive enough to surface most
// meaningful pullbacks on a 6-month run while still suppressing single-bar
// noise.
const DefaultDrawdownThreshold = 0.02

// DetectDrawdowns walks an equity curve and returns every drawdown whose
// depth reaches at least `threshold` (fraction of peak, e.g. 0.02 = 2%),
// plus an optional unrecovered drawdown at the end of the run. Episodes
// are returned in chronological order.
//
// Convention:
//   - FromTimestamp = prior peak, ToTimestamp = trough, RecoveredAt = new peak
//   - Unrecovered -> RecoveredAt = 0, RecoveryBars = -1, not in `recovered`
//   - Duration/Recovery bar counts are index deltas in the supplied slice
//     (suitable for "bars at the primary interval").
func DetectDrawdowns(points []EquityPoint, threshold float64) (recovered []entity.DrawdownPeriod, unrecovered *entity.DrawdownPeriod) {
	if len(points) == 0 {
		return nil, nil
	}

	// Ensure chronological order without mutating the caller's slice.
	sorted := make([]EquityPoint, len(points))
	copy(sorted, points)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })

	peak := sorted[0].Equity
	peakIdx := 0
	inDD := false
	var current entity.DrawdownPeriod
	var botIdx int

	for i, p := range sorted {
		// Equity >= peak ends the current drawdown (equality covers the
		// common case of "recovered to exactly the prior peak"); a strict
		// > would leave such episodes unrecovered.
		if p.Equity >= peak {
			if inDD {
				current.RecoveredAt = p.Timestamp
				current.RecoveryBars = i - botIdx
				recovered = append(recovered, current)
				inDD = false
				current = entity.DrawdownPeriod{}
			}
			peak = p.Equity
			peakIdx = i
			continue
		}

		// Equal or below peak — measure depth.
		if peak <= 0 {
			continue
		}
		depth := (peak - p.Equity) / peak

		if !inDD && depth >= threshold {
			inDD = true
			current = entity.DrawdownPeriod{
				FromTimestamp: sorted[peakIdx].Timestamp,
				ToTimestamp:   p.Timestamp,
				Depth:         depth,
				DepthBalance:  p.Equity,
				DurationBars:  i - peakIdx,
			}
			botIdx = i
		}

		if inDD && depth > current.Depth {
			current.Depth = depth
			current.DepthBalance = p.Equity
			current.ToTimestamp = p.Timestamp
			current.DurationBars = i - peakIdx
			botIdx = i
		}
	}

	if inDD {
		current.RecoveryBars = -1
		u := current
		unrecovered = &u
	}
	return recovered, unrecovered
}

// ComputeTimeInMarket returns (ratio, longestFlatStreakBars) for the supplied
// trade history against a bar timeline.
//
//   - barTimestamps must be sorted ascending and represent every primary-
//     interval candle in the run (len(barTimestamps) == totalBars).
//   - A bar counts as "in market" if any trade's [EntryTime, ExitTime]
//     interval (inclusive, millisecond timestamps) covers its timestamp.
//   - Overlapping trades do NOT double-count the same bar.
func ComputeTimeInMarket(trades []entity.BacktestTradeRecord, barTimestamps []int64, totalBars int) (ratio float64, longestFlat int) {
	if totalBars <= 0 || len(barTimestamps) == 0 {
		return 0, 0
	}

	// Build a set of covered bar indices. O(N*M) worst case but trades and
	// bars are small enough in practice (<5k trades, <10k bars).
	inMarket := make([]bool, len(barTimestamps))
	for _, tr := range trades {
		// Find the range of bar indices covered by [tr.EntryTime, tr.ExitTime].
		start := sort.Search(len(barTimestamps), func(i int) bool { return barTimestamps[i] >= tr.EntryTime })
		// upper bound: first index with timestamp > tr.ExitTime
		end := sort.Search(len(barTimestamps), func(i int) bool { return barTimestamps[i] > tr.ExitTime })
		for i := start; i < end && i < len(inMarket); i++ {
			inMarket[i] = true
		}
	}

	inMarketCount := 0
	currentFlat := 0
	for _, x := range inMarket {
		if x {
			if currentFlat > longestFlat {
				longestFlat = currentFlat
			}
			currentFlat = 0
			inMarketCount++
		} else {
			currentFlat++
		}
	}
	if currentFlat > longestFlat {
		longestFlat = currentFlat
	}
	ratio = float64(inMarketCount) / float64(totalBars)
	return ratio, longestFlat
}

// ComputeExpectancy returns the per-trade expected PnL together with the
// average win and average loss used to compute it. Convention:
//
//   - wins  = trades with PnL >= 0 (matches reporter.go)
//   - AvgLoss is reported as an absolute (positive) number so UI callers do
//     not need to negate.
//   - Empty input returns zeros, matching reporter.go's zero-trade handling.
func ComputeExpectancy(trades []entity.BacktestTradeRecord) (expectancy, avgWin, avgLoss float64) {
	if len(trades) == 0 {
		return 0, 0, 0
	}
	wins := 0
	losses := 0
	sumWin := 0.0
	sumLoss := 0.0
	for _, t := range trades {
		if t.PnL >= 0 {
			wins++
			sumWin += t.PnL
		} else {
			losses++
			sumLoss += math.Abs(t.PnL)
		}
	}
	if wins > 0 {
		avgWin = sumWin / float64(wins)
	}
	if losses > 0 {
		avgLoss = sumLoss / float64(losses)
	}
	wr := float64(wins) / float64(len(trades))
	expectancy = wr*avgWin - (1-wr)*avgLoss
	return expectancy, avgWin, avgLoss
}
