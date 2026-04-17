package backtest

import (
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// biweeklyWindowMillis は 14 日間ウィンドウを表すミリ秒数。
const biweeklyWindowMillis int64 = 14 * 24 * 60 * 60 * 1000

// biweeklyStepMillis はウィンドウを 1 日ずつスライドするための刻み幅 (ミリ秒)。
const biweeklyStepMillis int64 = 24 * 60 * 60 * 1000

// biweeklyMinTrades は 1 ウィンドウを「有効」と判定する最小トレード数。
// これ未満の場合、ウィンドウ勝率は 0 として扱われる (ペナルティ)。
const biweeklyMinTrades = 3

// biweeklyCoverageFloor は全ウィンドウに対する有効ウィンドウの最低割合。
// これを下回る場合、BiweeklyWinRate 全体を 0 として信頼不可と判定する。
const biweeklyCoverageFloor = 0.5

// ComputeBiweeklyWinRate は 14 日スライディングウィンドウ (1 日刻み) で
// 勝率を算出し、全ウィンドウの平均を返す (0-100 スケール)。
//
// `trades` はバックテストが吐き出したクローズ済みトレード列を想定しており、
// `BacktestTradeRecord.ExitTime` をクローズタイムスタンプとして使用する。
// トレードのタイムスタンプと periodFrom/periodTo は同一単位 (本プロジェクトでは
// ミリ秒) で指定する必要がある。
//
// アルゴリズム (spec §7.2 に準拠):
//
//   - ウィンドウ境界は半開区間 [windowStart, windowEnd)。
//   - 各ウィンドウ内のトレード数が 3 件未満 → そのウィンドウの勝率を 0 とする
//     (スキップせずペナルティとして平均の分母には残す)。
//   - 各ウィンドウ内のトレード数が 3 件以上 → win / total * 100 を勝率とする。
//     勝ちの定義は `SummaryReporter.BuildSummary` と揃えており PnL >= 0 を勝ちとする。
//   - カバレッジ率 = (>=3 件ウィンドウ数) / (全ウィンドウ数)。
//     カバレッジ率 < 50% の場合は全体として信頼不可と判定し 0 を返す。
//   - 条件を満たせば全ウィンドウ勝率 (ペナルティ 0 を含む) の単純平均を返す。
//
// エッジケース:
//   - `trades` が空 → 0。
//   - 期間 (periodTo - periodFrom) が 14 日未満 → ウィンドウが 1 つも作れないため 0。
//   - periodTo <= periodFrom → 0。
func ComputeBiweeklyWinRate(trades []entity.BacktestTradeRecord, periodFrom, periodTo int64) float64 {
	if len(trades) == 0 {
		return 0
	}
	if periodTo <= periodFrom {
		return 0
	}
	if periodTo-periodFrom < biweeklyWindowMillis {
		return 0
	}

	var (
		totalWindows   int
		coveredWindows int
		rateSum        float64
	)
	for windowStart := periodFrom; windowStart+biweeklyWindowMillis <= periodTo; windowStart += biweeklyStepMillis {
		windowEnd := windowStart + biweeklyWindowMillis
		totalWindows++

		var total, wins int
		for _, tr := range trades {
			if tr.ExitTime >= windowStart && tr.ExitTime < windowEnd {
				total++
				if tr.PnL >= 0 {
					wins++
				}
			}
		}

		if total < biweeklyMinTrades {
			// ペナルティ: 勝率 0 としてそのまま和に加える (加算なし)。
			continue
		}
		coveredWindows++
		rateSum += float64(wins) * 100.0 / float64(total)
	}

	if totalWindows == 0 {
		return 0
	}
	coverage := float64(coveredWindows) / float64(totalWindows)
	if coverage < biweeklyCoverageFloor {
		return 0
	}
	return rateSum / float64(totalWindows)
}
