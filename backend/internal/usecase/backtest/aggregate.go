package backtest

import (
	"math"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// ComputeAggregate condenses a slice of per-period backtest results into a
// single MultiPeriodAggregate. See the PR-2 design doc for the rationale
// behind each field and the ruin-handling convention (geomMean -> NaN when
// any period returns <= -1).
func ComputeAggregate(items []entity.LabeledBacktestResult) entity.MultiPeriodAggregate {
	n := len(items)
	if n == 0 {
		// "No evidence" is explicitly not "all positive" — callers that
		// treat this aggregate as a score must not confuse the two.
		return entity.MultiPeriodAggregate{AllPositive: false}
	}

	// First pass: worst/best return, worst drawdown, ruin detection.
	ruined := false
	worstRet := math.Inf(1)
	bestRet := math.Inf(-1)
	worstDD := math.Inf(-1)
	allPositive := true
	sumRet := 0.0
	for _, it := range items {
		r := it.Result.Summary.TotalReturn
		if r <= -1.0 {
			ruined = true
		}
		if r < worstRet {
			worstRet = r
		}
		if r > bestRet {
			bestRet = r
		}
		if r <= 0 {
			allPositive = false
		}
		sumRet += r

		dd := it.Result.Summary.MaxDrawdown
		if dd > worstDD {
			worstDD = dd
		}
	}

	// Population standard deviation of TotalReturn across periods.
	// Single-period case -> stdDev = 0 which matches intuition.
	mean := sumRet / float64(n)
	varSum := 0.0
	for _, it := range items {
		d := it.Result.Summary.TotalReturn - mean
		varSum += d * d
	}
	stdDev := math.Sqrt(varSum / float64(n))

	// Geometric mean of (1 + r_i). NaN on ruin so downstream scoring
	// cannot accidentally treat bankruptcy as "bad but comparable".
	geomMean := 0.0
	robustness := 0.0
	if ruined {
		geomMean = math.NaN()
		robustness = math.NaN()
		// Explicit: any ruin period invalidates the "all positive" story.
		allPositive = false
	} else {
		product := 1.0
		for _, it := range items {
			product *= 1.0 + it.Result.Summary.TotalReturn
		}
		// product > 0 is guaranteed here because every (1+r_i) > 0 (ruin
		// path is excluded above).
		geomMean = math.Pow(product, 1.0/float64(n)) - 1
		robustness = geomMean - stdDev
	}

	return entity.MultiPeriodAggregate{
		GeomMeanReturn:  geomMean,
		ReturnStdDev:    stdDev,
		WorstReturn:     worstRet,
		BestReturn:      bestRet,
		WorstDrawdown:   worstDD,
		AllPositive:     allPositive,
		RobustnessScore: robustness,
	}
}
