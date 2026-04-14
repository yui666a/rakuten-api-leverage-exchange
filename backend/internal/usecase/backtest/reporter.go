package backtest

import (
	"math"
	"sort"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type SummaryReporter struct{}

func NewSummaryReporter() *SummaryReporter {
	return &SummaryReporter{}
}

func (r *SummaryReporter) BuildSummary(
	config entity.BacktestConfig,
	finalBalance float64,
	trades []entity.BacktestTradeRecord,
) entity.BacktestSummary {
	totalTrades := len(trades)
	winTrades := 0
	lossTrades := 0
	profitSum := 0.0
	lossSum := 0.0
	carryingCost := 0.0
	spreadCost := 0.0
	holdSecondsTotal := int64(0)

	balance := config.InitialBalance
	equityCurve := []float64{balance}
	equityTimes := []int64{config.FromTimestamp}

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

		balance += tr.PnL
		equityCurve = append(equityCurve, balance)
		equityTimes = append(equityTimes, tr.ExitTime)
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

	maxDDRatio, maxDDBalance := calcMaxDrawdown(equityCurve)
	sharpe := calcSharpe(config.InitialBalance, trades)

	return entity.BacktestSummary{
		PeriodFrom:         config.FromTimestamp,
		PeriodTo:           config.ToTimestamp,
		InitialBalance:     config.InitialBalance,
		FinalBalance:       finalBalance,
		TotalReturn:        calcTotalReturn(config.InitialBalance, finalBalance),
		TotalTrades:        totalTrades,
		WinTrades:          winTrades,
		LossTrades:         lossTrades,
		WinRate:            winRate,
		ProfitFactor:       profitFactor,
		MaxDrawdown:        maxDDRatio,
		MaxDrawdownBalance: maxDDBalance,
		SharpeRatio:        sharpe,
		AvgHoldSeconds:     avgHold,
		TotalCarryingCost:  carryingCost,
		TotalSpreadCost:    spreadCost,
	}
}

func calcTotalReturn(initialBalance, finalBalance float64) float64 {
	if initialBalance == 0 {
		return 0
	}
	return (finalBalance - initialBalance) / initialBalance
}

func calcMaxDrawdown(equity []float64) (ratio float64, trough float64) {
	if len(equity) == 0 {
		return 0, 0
	}
	peak := equity[0]
	maxDD := 0.0
	troughBalance := equity[0]
	for _, v := range equity {
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

func calcSharpe(initialBalance float64, trades []entity.BacktestTradeRecord) float64 {
	if initialBalance <= 0 {
		return 0
	}
	if len(trades) == 0 {
		return 0
	}

	sorted := make([]entity.BacktestTradeRecord, len(trades))
	copy(sorted, trades)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ExitTime < sorted[j].ExitTime
	})

	dailyBalance := make(map[string]float64)
	balance := initialBalance
	for _, tr := range sorted {
		balance += tr.PnL
		key := time.UnixMilli(tr.ExitTime).UTC().Format("2006-01-02")
		dailyBalance[key] = balance
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
