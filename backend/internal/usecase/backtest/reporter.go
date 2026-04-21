package backtest

import (
	"math"
	"sort"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type SummaryReporter struct{}

type EquityPoint struct {
	Timestamp int64
	Equity    float64
}

func NewSummaryReporter() *SummaryReporter {
	return &SummaryReporter{}
}

func (r *SummaryReporter) BuildSummary(
	config entity.BacktestConfig,
	finalBalance float64,
	trades []entity.BacktestTradeRecord,
	equityPoints []EquityPoint,
) entity.BacktestSummary {
	totalTrades := len(trades)
	winTrades := 0
	lossTrades := 0
	profitSum := 0.0
	lossSum := 0.0
	carryingCost := 0.0
	spreadCost := 0.0
	holdSecondsTotal := int64(0)

	for _, tr := range trades {
		if tr.PnL >= 0 {
			winTrades++
			profitSum += tr.PnL
		} else {
			lossTrades++
			lossSum += math.Abs(tr.PnL)
		}
		carryingCost += tr.CarryingCost
		spreadCost += tr.SpreadCost
		if tr.ExitTime > tr.EntryTime {
			holdSecondsTotal += (tr.ExitTime - tr.EntryTime) / 1000
		}
	}

	winRate := 0.0
	if totalTrades > 0 {
		winRate = float64(winTrades) / float64(totalTrades) * 100
	}
	profitFactor := 0.0
	if lossSum > 0 {
		profitFactor = profitSum / lossSum
	}
	avgHold := int64(0)
	if totalTrades > 0 {
		avgHold = holdSecondsTotal / int64(totalTrades)
	}

	maxDDRatio, maxDDBalance := calcMaxDrawdown(equityPoints)
	sharpe := calcSharpe(equityPoints)
	biweekly := ComputeBiweeklyWinRate(trades, config.FromTimestamp, config.ToTimestamp)

	byExit := BuildBreakdown(trades, func(t entity.BacktestTradeRecord) string { return t.ReasonExit })
	bySource := BuildBreakdown(trades, func(t entity.BacktestTradeRecord) string {
		return parseSignalSource(t.ReasonEntry)
	})

	recoveredDDs, unrecoveredDD := DetectDrawdowns(equityPoints, DefaultDrawdownThreshold)
	expectancy, avgWin, avgLoss := ComputeExpectancy(trades)

	// Derive per-bar timestamps from the equity curve itself. runner.go pushes
	// exactly one EquityPoint per primary-interval candle plus a seed point,
	// so skipping the seed yields the bar timeline we need. This avoids
	// threading the full candle slice into the reporter.
	var barTimestamps []int64
	if len(equityPoints) > 1 {
		barTimestamps = make([]int64, 0, len(equityPoints)-1)
		for _, p := range equityPoints[1:] {
			barTimestamps = append(barTimestamps, p.Timestamp)
		}
	}
	timeInMarket, longestFlat := ComputeTimeInMarket(trades, barTimestamps, len(barTimestamps))

	return entity.BacktestSummary{
		PeriodFrom:          config.FromTimestamp,
		PeriodTo:            config.ToTimestamp,
		InitialBalance:      config.InitialBalance,
		FinalBalance:        finalBalance,
		TotalReturn:         calcTotalReturn(config.InitialBalance, finalBalance),
		TotalTrades:         totalTrades,
		WinTrades:           winTrades,
		LossTrades:          lossTrades,
		WinRate:             winRate,
		ProfitFactor:        profitFactor,
		MaxDrawdown:         maxDDRatio,
		MaxDrawdownBalance:  maxDDBalance,
		SharpeRatio:         sharpe,
		AvgHoldSeconds:      avgHold,
		TotalCarryingCost:   carryingCost,
		TotalSpreadCost:     spreadCost,
		BiweeklyWinRate:     biweekly,
		ByExitReason:        byExit,
		BySignalSource:      bySource,
		DrawdownPeriods:     recoveredDDs,
		DrawdownThreshold:   DefaultDrawdownThreshold,
		UnrecoveredDrawdown: unrecoveredDD,
		ExpectancyPerTrade:    expectancy,
		AvgWinJPY:             avgWin,
		AvgLossJPY:            avgLoss,
		TimeInMarketRatio:     timeInMarket,
		LongestFlatStreakBars: longestFlat,
	}
}

func calcTotalReturn(initialBalance, finalBalance float64) float64 {
	if initialBalance == 0 {
		return 0
	}
	return (finalBalance - initialBalance) / initialBalance
}

func calcMaxDrawdown(points []EquityPoint) (ratio float64, trough float64) {
	if len(points) == 0 {
		return 0, 0
	}
	sorted := make([]EquityPoint, len(points))
	copy(sorted, points)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp < sorted[j].Timestamp
	})

	peak := sorted[0].Equity
	maxDD := 0.0
	troughBalance := sorted[0].Equity
	for _, p := range sorted {
		v := p.Equity
		if v > peak {
			peak = v
		}
		if peak <= 0 {
			continue
		}
		dd := (peak - v) / peak
		if dd > maxDD {
			maxDD = dd
			troughBalance = v
		}
	}
	return maxDD, troughBalance
}

func calcSharpe(points []EquityPoint) float64 {
	if len(points) < 2 {
		return 0
	}

	sorted := make([]EquityPoint, len(points))
	copy(sorted, points)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp < sorted[j].Timestamp
	})

	loc, _ := time.LoadLocation("Asia/Tokyo")
	dailyBalance := make(map[string]float64)
	for _, p := range sorted {
		key := time.UnixMilli(p.Timestamp).In(loc).Format("2006-01-02")
		// keep the last snapshot of the day as daily close equity
		dailyBalance[key] = p.Equity
	}
	if len(dailyBalance) < 2 {
		return 0
	}

	keys := make([]string, 0, len(dailyBalance))
	for k := range dailyBalance {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	returns := make([]float64, 0, len(keys)-1)
	prev := dailyBalance[keys[0]]
	for _, k := range keys[1:] {
		curr := dailyBalance[k]
		if prev != 0 {
			returns = append(returns, (curr-prev)/prev)
		}
		prev = curr
	}
	if len(returns) == 0 {
		return 0
	}

	mean := 0.0
	for _, v := range returns {
		mean += v
	}
	mean /= float64(len(returns))

	variance := 0.0
	for _, v := range returns {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(len(returns))
	stddev := math.Sqrt(variance)
	if stddev == 0 {
		return 0
	}
	return mean / stddev * math.Sqrt(365)
}
