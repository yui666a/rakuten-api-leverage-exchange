# PDCA v2 基盤強化 実装計画 — 2026-04-21

2026-04-21 の PDCA 20 分チャレンジ（`docs/pdca/2026-04-21_promotion_v3.md`）で得た知見から、戦略最適化基盤を次のレベルに引き上げるための全 16 PR を洗い出し、そのうちコスパ最大の 6 PR を「Phase B」として先行実装する。

## 背景

v3 昇格時点の状況:

- 1yr/2yr/3yr すべてでプラスリターンを達成（+9.6% / +9.9% / +25.0%）
- ただし **SL = 20% という実質無効化の設定**で頑健性を確保している（不健全）
- **`stop_loss_atr_multiplier` / `htf_filter.alignment_boost` が未配線**（profile で変えても結果不変）
- **複数期間の頑健性は手動で curl を何度も叩いて集計**（1yr/2yr/3yr を別々に実行）
- **exit 理由 / シグナル別の寄与度**が見えない → cycle05/06 で `enabled=false` にして差分推定する非効率
- **ADX / Stochastics / Ichimoku** など実装済み指標が Strategy 側に配線されていない（FE では描画済み）

これらを一気に解決する。

## 設計方針

- Clean Architecture (`indicator/` → `IndicatorSet` → `StrategyEngineOptions` → `StrategyProfile` → `ConfigurableStrategy`) の既存依存方向を厳守
- **指標追加は漏れなく配線**。cycle08/09 で発覚した「profile で変えても効かない罠」を再発させないため、配線確認テストを DoD に入れる
- TDD（`test → 実装 → 通る`）— AGENTS.md 明記の規約
- **FE に描画ロジックがある指標（Stochastics / Ichimoku）は FE 実装と golden value で一致させる**
- PR は 1 機能 1 PR。Stacked PR として順番に積む（AGENTS.md の Git Strategy）
- 各 PR の本文には必ず `1yr / 2yr / 3yr での Return 差分` を記載

## 全 16 PR 一覧

### Phase A — 観測側強化（PDCA 品質底上げ）

| # | タイトル | 工数 | 対応する v3 での痛み |
|---|---|---|---|
| PR-1 | Exit 理由別 / シグナル別サマリ | 1 日 | cycle05/06 で disable して差分推定する非効率 |
| PR-2 | 複数期間一括バックテスト API + 頑健性スコア | 2 日 | v3 で curl を 1yr/2yr/3yr × 候補数だけ手動で叩いた |
| PR-3 | Drawdown 詳細 / Time-in-market / Expectancy | 1 日 | DD の「深さ」しか見えず「回復時間」「頻度」が不明 |
| PR-4 | Slippage/Spread 敏感度テスト | 0.5 日 | 実運用リスクが見えない |
| PR-5 | Regime 分類（bull-trend/bear-trend/range/volatile） | 1 日 | 2024/2025 でパフォーマンスが逆転する理由を説明できない |

### Phase B — 新指標追加（Strategy 強化の土台）

| # | タイトル | 工数 | 備考 |
|---|---|---|---|
| PR-6 | ADX (+DI/-DI) | 1.5 日 | **最重要**: 2024 年負けの最有力対策。trend/range ゲートを ADX で切り分け |
| PR-7 | Stochastics + Stochastic RSI | 1 日 | FE の `StochasticsChart.tsx` から Go に移植 |
| PR-8 | Ichimoku | 2 日 | FE の `CandlestickChart.tsx` から Go に移植。HTF フィルタに「雲モード」追加 |
| PR-9 | OBV / CMF | 1 日 | breakout の volume_ratio と併用して偽ブレイク検出 |
| PR-10 | VWAP / Anchored VWAP | 1 日 | エントリー価格乖離ゲート |
| PR-11 | Donchian Channel | 0.5 日 | breakout の素直判定 |

### Phase C — エンジン側機能

| # | タイトル | 工数 | 備考 |
|---|---|---|---|
| PR-12 | ATR Trailing Stop | 2 日 | **最優先**: 現 prod の SL=20% を健全化。未配線の `stop_loss_atr_multiplier` を実装 |
| PR-13 | Walk-forward 最適化 | 3 日 | **過学習の根本対策**。in-sample 最適化 × out-of-sample 検証を複数窓で |
| PR-14 | Regime-conditional プロファイル | 2 日 | PR-5 の上に構築。regime ごとに別の SL/TP/シグナル重み |
| PR-15 | 部分約定 / 分割利確 (partial TP) | 2 日 | 「50% を +3% で確定、残り 50% は trailing」 |
| PR-16 | 動的ポジションサイジング (ATR risk / fractional Kelly) | 1.5 日 | `position_sizing.mode` を profile に追加 |

**合計**: 23 日。

## Phase B（今回実装する 6 PR）

コスパ最大ライン。この 6 PR を終えると:

- 複数期間一括評価ができ、PDCA の回転速度が再び劇的に上がる
- ADX で 2024 年負けの真因に切り込める
- ATR trailing で SL=20% の不健全を解消できる
- Walk-forward で過学習の有無を即座に判定できる

### 実装順序（Stacked PRs）

```
main
 └─ PR-1 (Exit/Signal 内訳)
     └─ PR-2 (複数期間一括)
         └─ PR-3 (DD 詳細等)
             └─ PR-12 (ATR trailing)
                 └─ PR-6 (ADX)
                     └─ PR-13 (Walk-forward)
```

各 PR は前の PR がマージされ次第 rebase して次を出す（AGENTS.md の Stacked PR 方針）。

### Phase B 各 PR の詳細設計書

個別 Design Doc:
- [PR-1: Exit 理由別 / シグナル別サマリ](./plans/2026-04-21-pr1-exit-signal-breakdown.md)
- [PR-2: 複数期間一括バックテスト API + 頑健性スコア](./plans/2026-04-21-pr2-multi-period-backtest.md)
- [PR-3: Drawdown 詳細 / Time-in-market / Expectancy](./plans/2026-04-21-pr3-drawdown-detail.md)
- [PR-12: ATR Trailing Stop](./plans/2026-04-21-pr12-atr-trailing-stop.md)
- [PR-6: ADX (+DI/-DI)](./plans/2026-04-21-pr6-adx-indicator.md)
- [PR-13: Walk-forward 最適化](./plans/2026-04-21-pr13-walk-forward.md)

以下は本計画書内に残してある要旨。詳細は上記個別 Doc を参照。



#### PR-1: Exit 理由別 / シグナル別サマリ

**変更**:
- `entity.BacktestSummary` に以下を追加
  ```go
  type SummaryBreakdown struct {
      Trades   int
      WinRate  float64
      PnL      float64
      PF       float64
  }
  ByExitReason    map[string]SummaryBreakdown // "reverse_signal" / "stop_loss" / "take_profit" / "end_of_test"
  BySignalSource  map[string]SummaryBreakdown // "trend_follow" / "contrarian" / "breakout"
  ```
- `BacktestTradeRecord.ReasonEntry` から signal source を抽出する正規化関数 `parseSignalSource(reasonEntry string) string`
- DB マイグレーション: `backtest_results` に `breakdown_json` TEXT カラム追加 (JSON で格納。後方互換のため NULL 許容)
- API レスポンスで新フィールド返却
- Frontend: バックテスト詳細に「exit 理由別」「シグナル別」タブ追加

**DoD**:
- `parseSignalSource` の unit test（全 3 種と未知パターン）
- Summary 生成の unit test（手動で作った trades に対して期待 breakdown が出る）
- 後方互換: `breakdown_json = NULL` の既存行は従来通り読める

#### PR-2: 複数期間一括バックテスト API + 頑健性スコア

**変更**:
- 新 entity `MultiPeriodResult`
  ```go
  type MultiPeriodResult struct {
      Periods    []BacktestResult              // 各期間の個別結果
      Aggregate  MultiPeriodAggregate          // 横断統計
  }
  type MultiPeriodAggregate struct {
      GeomMeanReturn      float64
      ReturnStdDev        float64
      WorstReturn         float64
      WorstDrawdown       float64
      AllPositive         bool
      RobustnessScore     float64  // geomMean - stdDev
  }
  ```
- `POST /backtest/run-multi` — リクエスト:
  ```json
  {
    "profileName": "production",
    "data": "data/candles_LTC_JPY_PT15M.csv",
    "dataHtf": "data/candles_LTC_JPY_PT1H.csv",
    "periods": [
      {"label":"1yr","from":"2025-04-01","to":"2026-03-31"},
      {"label":"2yr","from":"2024-04-01","to":"2026-03-31"},
      {"label":"3yr","from":"2023-04-01","to":"2026-03-31"}
    ],
    "initialBalance": 100000,
    "tradeAmount": 0.1,
    "pdcaCycleId": "...",
    "hypothesis": "..."
  }
  ```
- CLI: `go run ./cmd/backtest multi --profile X --periods config.json`
- Frontend: ランキングビュー（profile × periods の heatmap）

**DoD**:
- 並列実行（goroutine）で N 期間を同時処理
- RobustnessScore の unit test
- 既存単一 backtest API は触らない（後方互換）

#### PR-3: Drawdown 詳細 / Time-in-market / Expectancy

**変更**:
- Summary に
  ```go
  DrawdownPeriods    []DrawdownPeriod  // 5% 以上の DD をすべて列挙
  TimeInMarketRatio  float64           // ポジション保有時間 / 全期間
  ExpectancyPerTrade float64           // 平均期待値 (JPY)
  ```
- `DrawdownPeriod { From, To, Depth, RecoveryBars }`
- 計算は `reporter.BuildSummary` に統合

**DoD**:
- DD 検出アルゴリズムの unit test（toy equity curve で期待 DD が検出される）

#### PR-12: ATR Trailing Stop

**変更**:
- `entity.RiskConfig` に `TrailingATRMultiplier float64` を追加（ゼロ = 無効）
- `usecase.RiskManager` に trailing stop 状態管理:
  - ポジションごとの最高値 (LONG) / 最安値 (SHORT) を追跡
  - 各 tick で ATR × multiplier 分離れたら決済
- 既存未配線の `strategy_risk.stop_loss_atr_multiplier` を適用
  - `stop_loss_atr_multiplier > 0` かつ `stop_loss_percent` も指定されている場合、**より遠い方**を SL とする（保守的）
- profile に:
  ```json
  "strategy_risk": {
      "stop_loss_percent": 5,
      "stop_loss_atr_multiplier": 2.0,
      "trailing_atr_multiplier": 2.0
  }
  ```

**DoD**:
- **配線確認テスト**: `TestConfigurableStrategy_ATRSLTakesEffect` で ATR SL が実際に効くことを assertion（cycle08/09 の罠を再発させない）
- RiskManager の unit test（LONG 最高値追跡、SHORT 最安値追跡、両方式）
- production profile を SL=20 → SL+ATR trailing に戻せるか検証して Phase C v4 promotion の基盤を作る

#### PR-6: ADX (+DI/-DI)

**変更**:
- `backend/internal/infrastructure/indicator/adx.go` 新規
  - Wilder 方式 (ATR と同じスムージング)
  - 期間: ADX14 デフォルト
- `entity.IndicatorSet` に `ADX14 / PlusDI14 / MinusDI14 *float64`
- `IndicatorCalculator` で計算して詰める
- `StrategyEngineOptions` に
  ```go
  ADXMinForTrend   float64  // trend_follow 発動の最低 ADX (デフォルト 20)
  ADXMaxForRange   float64  // contrarian 発動の最大 ADX (デフォルト 20)
  ```
- `StrategyProfile.SignalRules.TrendFollow.ADXMin / Contrarian.ADXMax` を追加
- `configurable_strategy.go` で配線
- evaluateTrendFollow / evaluateContrarian でゲート

**DoD**:
- ADX 計算の unit test（known input → known ADX；TA-Lib や投資教科書の値に一致）
- **配線確認テスト**: profile で `adx_min: 99` にすると trend_follow が無くなる assertion
- 既存 production.json との等価性: ADX 閾値をデフォルトに設定して legacy と同じ結果になる

#### PR-13: Walk-forward 最適化

**変更**:
- 新 usecase `backtest/walkforward.go`
  ```go
  type WalkForwardInput struct {
      BaseProfile       *StrategyProfile
      ParameterGrid     []ParameterSet        // 試行パラメータ
      InSampleMonths    int                   // e.g. 6
      OutOfSampleMonths int                   // e.g. 3
      StepMonths        int                   // e.g. 3
      From              time.Time
      To                time.Time
  }
  type WalkForwardResult struct {
      Windows []WalkForwardWindow
      AggregateOOS MultiPeriodAggregate       // out-of-sample の統合スコア
  }
  ```
- 各窓で in-sample ベストを選択、out-of-sample で評価、結果を集約
- `POST /backtest/walk-forward` API
- CLI サブコマンド `backtest walk-forward --profile base --grid grid.json --in 6 --oos 3 --step 3`
- Frontend: 窓別パフォーマンス一覧

**DoD**:
- 少数窓（3 窓程度）の unit test
- ParameterGrid の組合せ爆発対策（並列実行、上限 N）
- in-sample / out-of-sample の期間が重ならないことの assertion

### Phase B の見積もり

| PR | 工数 |
|---|---|
| PR-1 | 1 日 |
| PR-2 | 2 日 |
| PR-3 | 1 日 |
| PR-12 | 2 日 |
| PR-6 | 1.5 日 |
| PR-13 | 3 日 |
| **合計** | **10.5 日** |

## Phase B 完了後の再評価

以下を実施してから Phase A/C の残りに進む:

1. **v4 promotion**: PR-12 + PR-6 を使い v3 (SL=20%) を置き換える健全な profile を探索
2. **Walk-forward による過学習確認**: v3 / v4 を walk-forward にかけて真の OOS 性能を測定
3. **残りの PR の優先順位見直し**: v4 で見えた弱点に基づき Phase A/C の順序を更新

## 残り PR (Phase B 後に別計画で実装)

以下は **本計画では実装しない** が、`docs/pdca/agent-guide.md` の Known TODOs および本ドキュメント §4 に記載して将来の実装候補として残す。

| # | タイトル | Phase | 見送り理由 / 前提 |
|---|---|---|---|
| PR-4 | Slippage/Spread 敏感度テスト | A | 現 spread=0.1% / slippage=0 固定で定性評価は可能。定量化は優先度低 |
| PR-5 | Regime 分類 | A | PR-14 と一緒に出す方が自然 |
| PR-7 | Stochastics | B | FE に実装済みで緊急性低。PR-6 (ADX) で置き換え可能な用途 |
| PR-8 | Ichimoku | B | 同上。HTF フィルタの代替手段。現 HTF フィルタも十分機能 |
| PR-9 | OBV / CMF | B | breakout が主力でない限り効果限定 |
| PR-10 | VWAP | B | 15m 足主体では日次アンカー VWAP の効果が限定的 |
| PR-11 | Donchian | B | volume_ratio で代替可能 |
| PR-14 | Regime-conditional profile | C | PR-5 と組で。profile JSON 仕様が複雑化するので慎重に |
| PR-15 | Partial TP | C | SimExecutor への侵襲が大きい。v4 での TP 最適値を見てから |
| PR-16 | 動的ポジションサイジング | C | 既存 `tradeAmount=0.1` 固定で現状問題ない |

### 残り PR の簡易仕様（将来実装者向けメモ）

以下は、実装時に再度詳細設計するための最小限のメモ。

**PR-4**: `POST /backtest/sensitivity` で `spread × slippage` のグリッド（例 3×3）を走らせ heatmap を返す。パラメータは request body で受け取る。

**PR-5**: HTF EMA の傾きと BB bandwidth を組合せ `bull_trend / bear_trend / range / volatile` の 4 分類。`IndicatorSet.Regime string` を追加し、各 Trade に `RegimeAtEntry` を付与。Summary に `ByRegime map[string]SummaryBreakdown`。

**PR-7**: FE `StochasticsChart.tsx:9-56` の `calcStochastics` を Go に移植。`backend/internal/infrastructure/indicator/stochastics.go`。golden value は FE チャート実測値を CSV エクスポートして使用。

**PR-8**: FE `CandlestickChart.tsx` の `calcIchimoku` (138-178行) を Go に移植。`backend/internal/infrastructure/indicator/ichimoku.go`。`IndicatorSet.Ichimoku` をネスト構造体（Tenkan/Kijun/SenkouA/SenkouB/Chikou）で格納。`htf_filter.mode: "ema" | "ichimoku"` を追加。

**PR-9**: `obv.go` / `cmf.go`。累積出来高 (OBV) と Chaikin Money Flow。breakout signal rule に `require_obv_uptrend` / `require_cmf_positive` 条件を追加。

**PR-10**: `vwap.go`。セッション VWAP（1 日アンカー）と累積 VWAP。profile に `signal_rules.*.require_above_vwap` / `max_deviation_from_vwap` 等。

**PR-11**: `donchian.go`。N 足高値/安値ブレイク。`signal_rules.breakout.mode: "volume" | "donchian" | "both"`。

**PR-14**: profile に `regime_overrides` セクションを追加:
```json
"regime_overrides": {
  "bull_trend": {"strategy_risk": {"take_profit_percent": 8}},
  "range": {"signal_rules": {"trend_follow": {"enabled": false}}}
}
```
実行時に PR-5 が出力する regime を見て該当オーバーライドを `StrategyEngineOptions` に適用。

**PR-15**: `strategy_risk.partial_exits: [{at_profit_percent: 3, exit_ratio: 0.5}, ...]`。`SimExecutor` に部分決済機能を追加し、残りポジションは trailing SL に引き継ぎ。

**PR-16**: profile に `position_sizing: {mode: "fixed" | "atr_risk" | "fractional_kelly", risk_per_trade: 0.02, kelly_lookback_trades: 50}`。`RiskHandler` でトレードごとに amount を動的決定。Kelly は直近 N トレードの WR と平均 win/loss から 0.25 × Kelly を採用（fractional）。

## 参照

- 本計画のトリガー: `docs/pdca/2026-04-21_promotion_v3.md`
- 既存 PDCA 基盤設計: `docs/superpowers/specs/2026-04-16-pdca-strategy-optimizer-design.md`
- エージェント向けガイド: `docs/pdca/agent-guide.md`
- バックテストエンジン設計: `docs/design/2026-04-14-backtest-engine-design.md`
