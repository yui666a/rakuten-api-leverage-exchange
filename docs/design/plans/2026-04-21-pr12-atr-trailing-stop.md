# PR-12: ATR Trailing Stop（未配線フィールドの解消含む）

**親計画**: [`docs/design/2026-04-21-pdca-v2-infrastructure-plan.md`](../2026-04-21-pdca-v2-infrastructure-plan.md)
**Phase**: C（エンジン側機能）
**Stacked PR 順序**: #4 / 6（PR-3 の上）
**見積もり**: 2 日

## 動機

v3 production は `stop_loss_percent: 20` という**実質 SL 無効化**の設定で 1yr/2yr/3yr すべてプラスを達成しているが、これは不健全。実運用で価格が急落したときに際限なく損失が膨らむ。PDCA 探索中に `stop_loss_atr_multiplier` も試したが**未配線のため効かなかった** (cycle08)。

**ATR trailing stop を実装し、未配線の `stop_loss_atr_multiplier` を正しく配線する**ことで、v3 の頑健性を保ちつつ SL=20% という不健全を解消する。

## 対象外

- 部分約定 / partial TP（PR-15 で別途）
- 動的ポジションサイジング（PR-16 で別途）
- SL 発動時の分割撤退

## 仕様

### 概念整理

2 種類の ATR SL モードを用意する:

**モード A: ATR Initial SL（静的）**
- エントリー時に `SL = entry - ATR × atr_sl_multiplier`（LONG）
- 以降は更新されず、従来の `stop_loss_percent` と同じ使い方
- **profile.strategy_risk.stop_loss_atr_multiplier** が担当（未配線を解消）

**モード B: ATR Trailing Stop（動的）**
- エントリー後、ポジション保有中に毎 bar で最高値（LONG）/ 最安値（SHORT）を追跡
- `SL = peak - ATR × trailing_atr_multiplier`（LONG）、`SL = trough + ATR × trailing_atr_multiplier`（SHORT）
- 一度上げた SL は下げない（LONG / trailing の原則）
- **profile.strategy_risk.trailing_atr_multiplier** が担当（新規フィールド）

両モードは独立に有効化可能。両方有効なら「より遠い方（保守的）」を採用。

### データモデル

```go
// entity/backtest.go および entity/risk.go
type StrategyRiskConfig struct {
    StopLossPercent         float64 `json:"stop_loss_percent"`
    TakeProfitPercent       float64 `json:"take_profit_percent"`
    StopLossATRMultiplier   float64 `json:"stop_loss_atr_multiplier"`    // 既存だが未配線 → 有効化
    TrailingATRMultiplier   float64 `json:"trailing_atr_multiplier"`     // 新規
    MaxPositionAmount       float64 `json:"max_position_amount"`
    MaxDailyLoss            float64 `json:"max_daily_loss"`
}

// usecase/risk.go の RiskConfig に同フィールドを追加
// (DDD 上は entity の値を usecase が受け取る構造。既存の RiskConfig も拡張)
```

### RiskManager の変更

```go
type RiskManager struct {
    config entity.RiskConfig
    // 追加: ポジション別トラッキング
    trackers map[int64]*positionTracker  // key = PositionID
}

type positionTracker struct {
    PositionID   int64
    Side         entity.OrderSide
    EntryPrice   float64
    PeakPrice    float64  // LONG: high side
    TroughPrice  float64  // SHORT: low side
    CurrentSL    float64  // 現在の trailing SL 価格（なければ 0）
    InitialSL    float64  // entry 時に決めた静的 SL（モードA）
}
```

各 tick で以下を実行:
```go
func (rm *RiskManager) UpdateTrailing(posID int64, bar entity.Candle, atr float64) (sl float64, ok bool) {
    t := rm.trackers[posID]
    switch t.Side {
    case OrderSideBuy: // LONG
        if bar.High > t.PeakPrice { t.PeakPrice = bar.High }
        if rm.config.TrailingATRMultiplier > 0 && atr > 0 {
            candidate := t.PeakPrice - atr * rm.config.TrailingATRMultiplier
            if candidate > t.CurrentSL { t.CurrentSL = candidate } // never widen
        }
    case OrderSideSell: // SHORT
        if bar.Low < t.TroughPrice { t.TroughPrice = bar.Low }
        if rm.config.TrailingATRMultiplier > 0 && atr > 0 {
            candidate := t.TroughPrice + atr * rm.config.TrailingATRMultiplier
            if t.CurrentSL == 0 || candidate < t.CurrentSL { t.CurrentSL = candidate }
        }
    }
    // 併用時の SL 確定: initial SL と trailing SL のうち「遠い方（保守的）」
    return rm.resolveSL(t), true
}
```

### ATR 取得経路

現 `IndicatorSet.ATR14` は既に計算済み（`entity/indicator.go`）。`NewTickRiskHandler` に IndicatorSet を流す経路を既存 `IndicatorHandler → RiskHandler` と同じ形で追加。

変更範囲:
- `backend/internal/usecase/backtest/handler.go` の `NewTickRiskHandler` に `indicators *entity.IndicatorSet` を渡せるよう signature 変更（既存 caller 1 箇所を修正）
- live pipeline 側（`cmd/pipeline.go`）も同様に配線

### SL 発動ロジック

既存 `TickRiskHandler.SelectSLTPExit` は「SL 価格と TP 価格を受け取り、bar が hit したか判定」する純関数。インターフェースは変えず、`stopLossPrice` を呼び出し側（RiskManager 経由）で動的にセットするだけで済む。

### profile 変更

```json
"strategy_risk": {
    "stop_loss_percent": 5,
    "take_profit_percent": 4,
    "stop_loss_atr_multiplier": 2.0,      // 既存だが配線されるように
    "trailing_atr_multiplier": 2.0,        // 新規
    "max_position_amount": 100000,
    "max_daily_loss": 50000
}
```

優先順位（SL 価格決定）:
1. `trailing_atr_multiplier > 0` → trailing SL を計算
2. `stop_loss_atr_multiplier > 0` → initial ATR SL を計算
3. `stop_loss_percent > 0` → パーセンテージ SL
4. 複数有効ならポジション方向で「より遠い（保守的）」を採用

## テスト計画

### Unit

1. `RiskManager.UpdateTrailing` LONG: エントリー後 peak が上がるたびに SL が追従すること、価格が下がっても SL は下がらない
2. 同 SHORT: 同様（trough 側）
3. Initial ATR SL: エントリー時に `SL = entry - ATR × mult` が計算される
4. 併用優先順位: `percent=5%` と `atr=2.0 × ATR100=200` の場合、遠い方（この例ではパーセント）が採用される

### 配線確認テスト（必須）

5. `TestConfigurableStrategy_ATRTrailingTakesEffect`
    - profile A: `trailing_atr_multiplier: 0`（無効）
    - profile B: `trailing_atr_multiplier: 2.0`
    - 同じ CSV で実行して結果が **必ず** 異なることを assert
    - cycle08 の罠（profile 上は違うのに結果同一）を絶対に再発させない
6. `TestConfigurableStrategy_StopLossATRMultiplierTakesEffect`
    - 同様の assertion。これで未配線フィールドが解消されたことを保証

### Integration

7. `runner_test.go` に 1 ケース: 既存 production.json を `trailing_atr_multiplier: 2.0` に変えて再実行、SL exit が発生することを確認

### 既存テストの確認

- `TestConfigurableStrategy_EquivalentToDefault` は `trailing_atr_multiplier = 0, stop_loss_atr_multiplier = 0` で走っていれば引き続き通る（デフォルト不変）

## DoD

- [ ] Unit 4 本 + 配線確認 2 本 + integration 1 本 = **7 本** passing
- [ ] 既存 `TestConfigurableStrategy_EquivalentToDefault` が通る
- [ ] `docs/pdca/agent-guide.md` §8 の「未配線フィールド」表から `stop_loss_atr_multiplier` を削除
- [ ] PR 本文: v3 production (SL=20%) と、SL=5% + trailing=2.0 × ATR の profile で 1yr/2yr/3yr 比較（PR-2 の multi API 使用）
- [ ] v4 候補 profile（健全化版）を 1 つ作り、PDCA サイクル記録 `docs/pdca/YYYY-MM-DD_cycleNN.md` を追加

## v4 promotion （PR-12 マージ後の自然な流れ）

- **目標**: SL=20% を SL=5% + trailing 2×ATR に置き換え、1yr/2yr/3yr いずれも v3 と同等以上
- 置換候補 profile: `backend/profiles/experiment_2026-04-2x_healthy_trailing.json`
- `docs/pdca/2026-04-2x_promotion_v4.md` 作成

## ロールバック

- 全フィールドに明示的デフォルト 0（= 無効）。既存 production.json を触らない限り挙動不変
- `trailing_atr_multiplier` 0 なら従来と同じ

## 備考

- tracker map の state は `BacktestRunner.Run` 単位で新規作成される（per-run 分離）
- live pipeline では `MarketDataService` → `RiskManager` に ATR が流れる経路を追加（既に IndicatorSet が流れているので接続だけ）
- この PR で **RiskManager が indicator に依存する方向**が生まれる。Clean Architecture 上は usecase が infra 層の結果（IndicatorSet）を受け取る形なので問題なし
