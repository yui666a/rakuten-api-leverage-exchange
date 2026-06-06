# 調査報告書: ティック単位 SL/TP の実動作確認と ATR 供給バグ

- **調査日**: 2026-06-06
- **対象**: ライブ自動売買パイプライン（`EventDrivenPipeline`）の損切り・利確（SL/TP）発火経路
- **発端の問い**: 「損切り・利確は本当にティック単位で動いているか？ 15分ごとの投資判断によって売買が成立しているように見える。バグがないか確認してほしい」
- **結論**: ティック単位の **percent ベース SL/TP は正常に動作**している。ただし **ATR ベースの SL・トレーリングはライブで機能していない**バグを1件発見（バックテストとの挙動乖離）。

---

## 1. 結論サマリ

| 項目 | 状態 | 備考 |
|---|---|---|
| ティック単位の TickEvent 発火 | ✅ 正常 | 全ティッカーが毎回 `TickEvent` を生成 |
| percent ベース SL/TP（本番 SL=10% / TP=2.2%） | ✅ 正常 | 15分足確定を待たずティック単位で発火・実発注 |
| ATR ベース SL（`stop_loss_atr_multiplier`） | ❌ 不動作 | ライブで `currentATR=0` 固定のため ATR 分岐に入らない |
| ATR トレーリング（本番 `trailing_atr_multiplier=2.0`） | ❌ 不動作 | percent フォールバック（=SL%=10%）に落ちる |
| バックテストとの整合性 | ⚠️ 乖離 | バックテストは ATR が効くがライブは効かない |

「15分ごとの判断でしか売買していないように見える」というユーザーの体感に対しては、**percent SL/TP はティック単位で発火している**のが事実。ただしその発火距離が ATR 設定を無視した percent 値になっている点が問題。

---

## 2. ティック単位 SL/TP 機構の確認（正常箇所）

### 2.1 全ティックで TickEvent が発火する

`internal/infrastructure/live/live_source.go:93-120` `HandleTick`:

- 全ティッカーを `CandleBuilder.AddTickAndSnapshot` に通し、**毎回 `TickEvent` を生成**する。
- プライマリ足（PT15M）が確定したティックのときだけ、追加で `CandleEvent` も生成する。

```go
// Always emit a TickEvent for SL/TP checking.
tickEvent := entity.TickEvent{ ... BarLow: barLow, BarHigh: barHigh }
events = append(events, tickEvent)
if candleEvent != nil { ... }
```

### 2.2 TickRiskHandler が毎ティック SL/TP を評価し実発注する

`cmd/event_pipeline.go:707` で `TickRiskHandler` を `EventTypeTick`（priority 15）に登録。

`internal/usecase/backtest/handler.go:965-1095` `Handle`:

- `h.Executor.Positions()` で保有ポジションを取得し、各ポジションに対して SL/TP を評価。
- 発火条件は `h.policy.stopLossPercent > 0 && h.policy.takeProfitPercent > 0`（本番は SL=10%, TP=2.2% で成立）。
- ヒット時は `h.Executor.Close(...)` を呼び出し、`real_executor.go:335-396` で **venue へ成行クローズ注文を実発注**する。

→ したがって **percent ベースの SL/TP は 15分足の確定を待たず、ティック単位で発火・約定する**。この経路にバグはない。

---

## 3. 発見したバグ: ライブで ATR が TickRiskHandler に供給されない

### 3.1 ATR は IndicatorEvent 経由でしか更新されない

`internal/usecase/backtest/handler.go:965-974` `Handle`:

```go
func (h *TickRiskHandler) Handle(_ context.Context, event entity.Event) ([]entity.Event, error) {
    // ATR は IndicatorEvent から抽出。
    if indicatorEvent, ok := event.(entity.IndicatorEvent); ok {
        if indicatorEvent.Primary.ATR != nil {
            h.UpdateATR(*indicatorEvent.Primary.ATR)
        }
        return nil, nil
    }
    // ... 以降が TickEvent 処理
}
```

`UpdateATR`（`handler.go:915-923`）が `currentATR` を更新する唯一の経路。これは **ハンドラーが `EventTypeIndicator` に登録されていることが前提**。

### 3.2 バックテスト runner は2箇所に登録している（正）

`internal/usecase/backtest/runner.go:288-292`:

```go
// Run tickRiskHandler on IndicatorEvent (priority 12, strictly before
// the strategy at priority 20) so the new ATR value is already in place
// by the time the risk loop sees the next TickEvent.
bus.Register(entity.EventTypeIndicator, 12, tickRiskHandler) // ← ATR 更新経路
bus.Register(entity.EventTypeTick, 15, tickRiskHandler)
```

### 3.3 ライブパイプラインは1箇所しか登録していない（誤・バグ）

`cmd/event_pipeline.go:707`:

```go
bus.Register(entity.EventTypeTick, 15, tickRiskHandler)
// EventTypeIndicator への登録が存在しない
```

`event_pipeline.go` 内の `EventTypeIndicator` 登録は以下のみで、いずれも `tickRiskHandler` ではない:

| 行 | priority | ハンドラー | 用途 |
|---|---|---|---|
| 732 | 20 | `strategyHandler` | シグナル生成 |
| 813 | 26 | `atrSrc` | ExitPlan shadow 用（別物） |
| 840 | 25 | `indicatorEventTap` | stance recorder 用キャッシュ |
| 851 | 99 | `recorder` | decision_log 記録 |

→ `tickRiskHandler` は `EventTypeIndicator` を受け取らないため、**ライブでは `UpdateATR` が一度も呼ばれず `currentATR` が 0 のまま固定**される。`Handle` 内の `IndicatorEvent` 分岐はライブでは到達不能なデッドコード。

### 3.4 影響

`currentATR == 0` により、以下の ATR ロジックが常に無効化される。

**SL 距離** `stopLossDistance`（`handler.go:929-939`）:
```go
atrDist := 0.0
if h.policy.stopLossATRMultiplier > 0 && h.currentATR > 0 { // currentATR=0 で常に false
    atrDist = h.currentATR * h.policy.stopLossATRMultiplier
}
// → 常に percentDist が採用される
```

**トレーリング距離** `trailingDistance`（`handler.go:946-963`、ATR モード）:
```go
case trailingModeATR:
    percentDist := entryPrice * h.policy.stopLossPercent / 100.0
    atrDist := 0.0
    if h.policy.trailingATRMultiplier > 0 && h.currentATR > 0 { // 常に false
        atrDist = h.currentATR * h.policy.trailingATRMultiplier
    }
    // → 常に percentDist(=SL%=10%) にフォールバック
```

結果:
- ライブの **SL は常に percent 値**（ATR 設定を無視）。
- ライブの **トレーリング幅は percent フォールバック**になり、本番設定 `trailing_atr_multiplier=2.0` が実質無効。
- バックテストでは ATR が効くため、**チューニング結果とライブ実挙動が乖離**する。

### 3.5 既存ドキュメントの誤診

`backend/profiles/production.json` の `description` 内の記述:

> trailing_atr_multiplier 2.5→2.0 (cosmetic — has known bug in handler.go:946 trailingDistance where **percentDist always wins at LTC scale**)

これは **根本原因の誤診**。「LTC スケールだから percent が勝つ」のではなく、**ライブで ATR が供給されず `currentATR=0` のため `atrDist` が常に 0 になる**のが真因。バックテスト単体では ATR は正しく効く（runner で正しく登録されているため）。

---

## 4. 推奨修正

runner と同じく、ライブパイプラインでも `tickRiskHandler` を `EventTypeIndicator` に登録する（strategy の priority 20 より前に置く）。

`cmd/event_pipeline.go`（`bus.Register(entity.EventTypeTick, 15, tickRiskHandler)` の前後）に1行追加:

```go
// ATR を IndicatorEvent から TickRiskHandler に供給する（runner と同じ経路）。
// strategy(20) より前の priority に置き、次の TickEvent までに ATR を反映させる。
bus.Register(entity.EventTypeIndicator, 12, tickRiskHandler)
bus.Register(entity.EventTypeTick, 15, tickRiskHandler)
```

- ライブの `IndicatorHandler` は `atr_period=14` で ATR を算出済みのため、登録するだけで供給される。
- あわせて `production.json` の誤診記述（「percentDist always wins at LTC scale」）を真因に沿って訂正することを推奨。

### 補足: 回帰テストの提案

このバグは「ハンドラー登録漏れ」であり既存テストで検知できていない。ライブパイプライン構築後に
`EventTypeIndicator` に `tickRiskHandler` が登録されていること、もしくは IndicatorEvent 投入後に
`currentATR` が更新されることを検証するテストの追加が望ましい。

---

## 5. 参照ファイル

| ファイル | 該当箇所 | 内容 |
|---|---|---|
| `cmd/event_pipeline.go` | 707 | ライブの tickRiskHandler 登録（Indicator 登録欠落） |
| `internal/usecase/backtest/runner.go` | 288-292 | バックテストの正しい二重登録 |
| `internal/usecase/backtest/handler.go` | 915-923 | `UpdateATR` |
| `internal/usecase/backtest/handler.go` | 929-963 | `stopLossDistance` / `trailingDistance` |
| `internal/usecase/backtest/handler.go` | 965-1095 | `TickRiskHandler.Handle`（SL/TP 評価） |
| `internal/infrastructure/live/live_source.go` | 93-120 | `HandleTick`（毎ティック TickEvent 発火） |
| `internal/infrastructure/live/real_executor.go` | 335-396 | `Close`（venue へ実発注） |
| `backend/profiles/production.json` | description | trailing_atr の誤診記述 |
