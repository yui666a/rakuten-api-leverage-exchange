# PR-3: Drawdown 詳細 / Time-in-market / Expectancy

**親計画**: [`docs/design/2026-04-21-pdca-v2-infrastructure-plan.md`](../2026-04-21-pdca-v2-infrastructure-plan.md)
**Phase**: A（観測側強化）
**Stacked PR 順序**: #3 / 6（PR-2 の上）
**見積もり**: 1 日

## 動機

- 現状 `MaxDrawdown` しかなく、「**その DD がいつ発生し、何本で回復したか**」が不明。v3 の 2yr DD=7.9% は回復込みか未回復か見えない
- `avgHoldSeconds` はあるが、**全期間に対する保有時間率**が無いため資本効率を評価できない
- `TotalPnL` / `WinRate` はあるが、**1 トレードあたり期待値 (JPY)**が無いため tradeAmount スケール時の挙動が見えない

いずれも Summary に追加するだけで観測品質が大きく改善する。

## 対象外

- Drawdown を規準にしたロス制限機能（実装機能は PR-16 のポジションサイジングで）
- Time-in-market のリアルタイム表示（バックテスト結果内のみ）

## 仕様

### データモデル

```go
// entity/backtest.go
type DrawdownPeriod struct {
    FromTimestamp int64   `json:"fromTimestamp"` // DD 開始（ピーク到達時刻）
    ToTimestamp   int64   `json:"toTimestamp"`   // DD 底
    RecoveredAt   int64   `json:"recoveredAt"`   // 回復時刻（ピーク再到達）、未回復なら 0
    Depth         float64 `json:"depth"`         // 0-1（例 0.083 = 8.3%）
    DepthBalance  float64 `json:"depthBalance"`  // 底時点の残高
    DurationBars  int     `json:"durationBars"`  // From→To の足数
    RecoveryBars  int     `json:"recoveryBars"`  // To→Recovered の足数、未回復なら -1
}

type BacktestSummary struct {
    // ... 既存フィールド含め ...

    // Drawdown: 閾値以上の DD をすべて列挙
    DrawdownPeriods        []DrawdownPeriod `json:"drawdownPeriods"`
    DrawdownThreshold      float64          `json:"drawdownThreshold"` // 検出閾値（0.02 = 2%）
    UnrecoveredDrawdown    *DrawdownPeriod  `json:"unrecoveredDrawdown,omitempty"` // 期間末まで未回復の DD

    // Time-in-market
    TimeInMarketRatio      float64 `json:"timeInMarketRatio"`      // ポジション保有時間 / 全期間
    LongestFlatStreakBars  int     `json:"longestFlatStreakBars"`  // 連続ノーポジ期間の最長

    // Expectancy
    ExpectancyPerTrade     float64 `json:"expectancyPerTrade"`     // JPY/trade。E = WR * AvgWin - (1-WR) * AvgLoss
    AvgWinJPY              float64 `json:"avgWinJpy"`
    AvgLossJPY             float64 `json:"avgLossJpy"`             // 絶対値（正の数）
    AvgHoldBars            float64 `json:"avgHoldBars"`
}
```

### Drawdown 検出アルゴリズム

入力: `equityPoints []EquityPoint{Timestamp, Equity}`（既に `runner.go` で生成済み）

```
peak = equityPoints[0].Equity
peakIdx = 0
inDD = false
currentDD = DrawdownPeriod{}

for i, p := range equityPoints:
    if p.Equity > peak:
        if inDD:
            currentDD.RecoveredAt = p.Timestamp
            currentDD.RecoveryBars = i - botIdx
            periods = append(periods, currentDD)
            inDD = false
        peak = p.Equity
        peakIdx = i
    else:
        depth = (peak - p.Equity) / peak
        if !inDD && depth >= threshold:
            inDD = true
            currentDD = DrawdownPeriod{
                FromTimestamp: equityPoints[peakIdx].Timestamp,
                Depth: depth, DepthBalance: p.Equity,
            }
            botIdx = i
        if inDD && depth > currentDD.Depth:
            currentDD.Depth = depth
            currentDD.DepthBalance = p.Equity
            currentDD.ToTimestamp = p.Timestamp
            botIdx = i

# 期間末まで未回復の DD
if inDD:
    unrecovered = currentDD
```

閾値はデフォルト 2%（`DrawdownThreshold = 0.02`）。設定可能にするかは MVP では不要（定数）。

### Time-in-market

```
totalBars   = len(primaryCandles)
inMarketBars = 0  // ポジション open の足数
maxFlatStreak = 0, currentFlatStreak = 0

for each bar:
    if any position is open at bar close:
        inMarketBars++
        if currentFlatStreak > maxFlatStreak: maxFlatStreak = currentFlatStreak
        currentFlatStreak = 0
    else:
        currentFlatStreak++

TimeInMarketRatio = inMarketBars / totalBars
LongestFlatStreakBars = max(maxFlatStreak, currentFlatStreak)
```

実装は `SimExecutor` か `reporter` で各 bar のポジション有無を追跡。既存 `equityPoints` の処理に統合する。

### Expectancy

```
WR = winTrades / totalTrades
AvgWin  = sum(pnl > 0) / winTrades       -- JPY
AvgLoss = sum(|pnl| where pnl < 0) / lossTrades

Expectancy = WR * AvgWin - (1 - WR) * AvgLoss  -- JPY/trade
```

エッジケース:
- totalTrades = 0: すべて 0
- winTrades = 0 or lossTrades = 0: それぞれ 0、Expectancy は `-AvgLoss` または `+AvgWin`

### DB / API / Frontend

- 現行スキーマでは `BacktestSummary` の各フィールドは `backtest_results` テーブルに **個別カラム** として展開されている（`total_return` / `max_drawdown` / `sharpe_ratio` / `biweekly_win_rate` ... の列、`resultColumns` 定数参照）。一方で PR-1 で追加した `breakdown_json` カラムは複数のサブ集計をまとめて JSON で格納している
- 本 PR の新規フィールドは **マイグレーションを伴う**選択肢が 2 つある:
  - **案 A**: 個別カラム追加（`time_in_market_ratio` / `expectancy_per_trade` / `longest_flat_streak_bars` など）+ `drawdownPeriods` だけ JSON 列（`drawdown_periods_json TEXT DEFAULT NULL`）。既存パターンと揃えやすい
  - **案 B**: 新規 `summary_extra_json TEXT DEFAULT '{}'` 1 本を追加して PR-3 / 将来拡張をここに集約。スキーマ変更頻度を抑えられる
  - 現時点では **案 A を採用予定**（type-safety と既存 PRAGMA テストパターンに揃うため）。実装時に再評価
- API レスポンスで新フィールドが返る
- Frontend: バックテスト詳細に
  - 「Drawdown 履歴」テーブル（列: 開始日、底日、深さ、継続期間、回復期間）
  - 「Time-in-market」カード
  - 「Expectancy」カード（AvgWin / AvgLoss / E 並べる）

## テスト計画

### Unit

1. `DetectDrawdowns` — toy equity curve（ピーク 100 → 90 → 95 → 80 → 105）で期待 DD が 2 件検出される
2. 未回復ケース — 期間末まで未回復の DD が `UnrecoveredDrawdown` に入る
3. `ComputeTimeInMarket` — 合成シナリオで比率・最長フラットが一致
4. `ComputeExpectancy` — WR 60% / AvgWin 100 / AvgLoss 50 で E = 40 が返る
5. エッジケース — トレード 0 件、全勝、全敗

### Integration

1. `runner_test.go`: 既存シナリオで `summary.DrawdownPeriods` に 1 件以上、`TimeInMarketRatio` が 0-1 の範囲、`ExpectancyPerTrade` が finite

## DoD（as-built）

- [x] Unit 12 本 passing: DetectDrawdowns 4 ケース (2 episodes, unrecovered, below threshold, empty) + ComputeTimeInMarket 4 ケース (full, no trades, always-in-market, empty bars) + ComputeExpectancy 4 ケース (mixed WR, no trades, all wins, all losses)
- [x] Integration 2 本: reporter.BuildSummary に新フィールドが詰まる + repository round-trip (drawdown periods + unrecovered + time-in-market + expectancy)
- [x] 既存テスト全 17 パッケージ緑
- [x] migrations_test に PR-3 カラム 7 本の assertion を追加
- [x] docker e2e: 6mo production で `/backtest/run` を叩き、unrecoveredDrawdown/timeInMarketRatio/expectancyPerTrade が返ることを確認

### フォローアップ（別 PR）

- Frontend に DD 履歴テーブル / TimeInMarket カード / Expectancy カードを追加
- PR 本文: 直近 6 ヶ月 production の数値を貼付（下記 #XYZ PR 本文に実測値）

## ロールバック

summary に field を追加するだけなので、PR 全体を revert すれば影響なし。既存行は新 field = 0 / empty で読める。

## 備考

- PR-3 終了時点で **Phase A 完了**。続く PR-12 の ATR trailing で v4 promotion の基盤を作る
- Drawdown 閾値を将来 profile 駆動にする場合は `profile.reporting.drawdown_threshold` を導入予定（今回スコープ外）
