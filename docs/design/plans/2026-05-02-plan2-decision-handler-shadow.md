# PR2 Plan: Decision レイヤ新設 + StrategyHandler の MarketSignal 出力 (shadow)

- 作成日: 2026-05-02
- 親設計書: `docs/design/2026-04-29-signal-decision-policy-separation-design.md`
- 前段 PR: PR1 (#232) — entity 型と decision_log 列を追加済み
- スコープ: Phase 1 / Stacked PR シリーズ (PR2 ÷ 5)
- 動作影響: **無し（shadow のみ）**。実発注は引き続き旧ルート (Signal → Risk) で動く

---

## 0. このドキュメントの位置付け

PR1 で型と DB 列を入れたので、PR2 では **新ルート（Indicator → MarketSignal → Decision → Recorder）** を旧ルートと並走させる。実発注経路は触らないので bot の挙動は変わらない。

PR2 マージ時点の **観測可能な変化**:

- `decision_log` の新カラム (`signal_direction`, `signal_strength`, `decision_intent`, `decision_side`, `decision_reason`) に値が書き込まれ始める
- `signal_action` / `signal_confidence` / `signal_reason` 等の旧カラムは引き続き埋まる（Recorder は両ルート受信）
- `/decisions` API レスポンスに新カラムが含まれる（フロントは Phase 5 まで無視）
- 実際に発注されるシグナルは旧ルートのまま — 数値的にも挙動的にも 100% 同じ

実切替（旧ルート削除 + 両建て総額判定バグ解消）は PR3 で行う。

---

## 1. 設計書からの調整点

PR1 の plan と同じく、設計書 §6 PR2 と現状コードを突き合わせて以下を確定する：

### 1.1 EventBus priority 番号

設計書 §4.2 では Decision を 25 に書いているが、**priority 25 は既に `indicatorEventTap` が live 側で使用中**（`backend/cmd/event_pipeline.go:763`）。設計書の脚注通り **Decision を 27** に置く。

```
EventTypeIndicator    → 5  : tickGenerator (backtest 側)
EventTypeIndicator    → 10 : indicatorHandler
EventTypeIndicator    → 12 : tickRiskHandler (TP/SL)
EventTypeIndicator    → 20 : strategyHandler  ← MarketSignal も発行する (PR2)
EventTypeIndicator    → 25 : indicatorEventTap (live のみ、変更なし)
EventTypeMarketSignal → 27 : decisionHandler   ← 新設 (PR2)
EventTypeMarketSignal → 99 : recorder          ← 新規購読 (PR2)
EventTypeDecision     → 99 : recorder          ← 新規購読 (PR2)
EventTypeSignal       → 30 : riskHandler       ← 旧ルート、PR2 では touch しない
EventTypeSignal       → 99 : recorder          ← 旧ルート、変更なし
EventTypeApproved     → 40 : executionHandler  ← 変更なし
```

### 1.2 StrategyHandler は HOLD 時に何も発行しない

現行 `StrategyHandler.Handle` (handler.go:351-) は HOLD signal を **drop して event 発行しない**。recorder は IndicatorEvent 受信時に「HOLD 既定」で 1 行 INSERT しているので、シグナルが鳴らないバーは旧カラム上で正しく HOLD と記録される（recorder.go:130）。

PR2 では新ルートでも同じ非対称性を保つ：

- **MarketSignal**: HOLD 相当（Direction=NEUTRAL）でも publish する → recorder が `signal_direction=NEUTRAL` を埋められる
- **ActionDecision**: Decision レイヤは IntentHold / NEUTRAL でも publish する → recorder が `decision_intent=HOLD` を埋められる

これにより、PR3 で新ルートに切り替えた瞬間、HOLD バーの decision_log もシームレスに埋まる。

### 1.3 既存 Signal → MarketSignal 翻訳の責務

設計書 §4.1 では「StrategyHandler が Direction/Strength を直接吐くように改修」とあるが、StrategyEngine 内部は BUY/SELL/HOLD ベースで動いていて、これを全部書き直すのは PR2 のスコープを超える。

→ **StrategyHandler 内で `Signal → MarketSignal` の薄い翻訳レイヤを噛ませる**。StrategyEngine の中身は触らない。

```go
func toMarketSignal(s entity.Signal, indicators entity.IndicatorSet) entity.MarketSignal {
    var dir entity.SignalDirection
    switch s.Action {
    case entity.SignalActionBuy:  dir = entity.DirectionBullish
    case entity.SignalActionSell: dir = entity.DirectionBearish
    default:                       dir = entity.DirectionNeutral
    }
    return entity.MarketSignal{
        SymbolID:   s.SymbolID,
        Direction:  dir,
        Strength:   s.Confidence,    // 0..1 そのまま
        Source:     "legacy_strategy_engine",
        Reason:     s.Reason,
        Indicators: indicators,
        Timestamp:  s.Timestamp,
    }
}
```

将来 (Phase 6+) に StrategyEngine 自体が Direction/Strength を直接吐くようリファクタする選択肢を残す。

### 1.4 DecisionHandler のロジック

設計書 §4.1 の表をコードに翻訳：

| 保有状況 | Direction | cooldown | Intent | Side |
|---|---|---|---|---|
| なし | BULLISH | off | NEW_ENTRY | BUY |
| なし | BEARISH | off | NEW_ENTRY | SELL |
| なし | NEUTRAL | off | HOLD | "" |
| ロング中 | BULLISH | off | HOLD | "" |
| ロング中 | BEARISH | off | EXIT_CANDIDATE | SELL |
| ショート中 | BEARISH | off | HOLD | "" |
| ショート中 | BULLISH | off | EXIT_CANDIDATE | BUY |
| 任意 | 任意 | on | COOLDOWN_BLOCKED | "" |

設計書 §4.1 では `EXIT_CANDIDATE` は「Phase 1 では HOLD として扱う。実 exit は TP/SL に任せる」とある。**PR2 中は EXIT_CANDIDATE を decision_log に正しく記録するが、実発注は EXIT_CANDIDATE では行わない**（Risk への配線も PR3 で）。

cooldown は **PR2 ではまだ無効**（RiskManager 拡張は PR3）。DecisionHandler は cooldown のクエリはせず、IsEntryCooldown は false 固定で進める。

### 1.5 ポジション保有状況の取得

DecisionHandler は「現在のポジションがあるか/どっち向きか」を知る必要がある。既存の取得経路：

- **live**: `PositionManager` 経由 (`backend/cmd/event_pipeline.go` で参照されている)
- **backtest**: `SimExecutor` 経由 (実 trades の代わりに in-memory で保持)

両方を抽象化する小さなインターフェースを `usecase/decision/` パッケージで定義し、live / backtest それぞれで adapter を組み立てる：

```go
// usecase/decision/handler.go
type PositionView interface {
    // CurrentSide returns OrderSideBuy / OrderSideSell / "" (no position).
    // SymbolID で問い合わせ。複数ポジ保有時は net side を返す（合計 long / short 比較）。
    CurrentSide(ctx context.Context, symbolID int64) entity.OrderSide
}
```

PositionView の live 実装は `event_pipeline.go` で既存 `PositionManager` から組み立て、backtest 実装は `SimExecutor` から組み立てる。**PR2 では「保有なし固定」のスタブ実装で良い**：

- 動作影響無し（新ルートの結果は recorder にしか流れない）
- PR3 で実装を入れる時に PositionView の interface 設計だけは PR2 で確定させておく

### 1.6 recorder の新ルート対応

Recorder.Handle に追加分岐：

```go
case entity.MarketSignalEvent:
    r.onMarketSignal(ctx, ev)
case entity.ActionDecisionEvent:
    r.onActionDecision(ctx, ev)
```

それぞれ `pendingRec.SignalDirection / SignalStrength` と `DecisionIntent / DecisionSide / DecisionReason` を埋めて UPDATE。**旧 onSignal も触らない**（旧ルートからの SignalEvent も来続けるので）。

---

## 2. ファイル変更マップ

| ファイル | 変更 | 行数目安 |
|---|---|---|
| `backend/internal/usecase/decision/handler.go` | **新規** DecisionHandler 本体 | ~120 |
| `backend/internal/usecase/decision/handler_test.go` | **新規** マトリクステスト | ~200 |
| `backend/internal/usecase/decision/position_view.go` | **新規** PositionView interface + stub | ~40 |
| `backend/internal/usecase/decision/position_view_test.go` | **新規** stub テスト | ~30 |
| `backend/internal/usecase/backtest/handler.go` | StrategyHandler に MarketSignal 並列発行 | +30 |
| `backend/internal/usecase/backtest/handler_test.go` | StrategyHandler の MarketSignal 発行テスト | +60 |
| `backend/internal/usecase/decisionlog/recorder.go` | onMarketSignal / onActionDecision 追加 | +50 |
| `backend/internal/usecase/decisionlog/recorder_test.go` | 新ルート用テスト | +120 |
| `backend/internal/usecase/backtest/runner.go` | EventBus 配線追加 | +12 |
| `backend/internal/usecase/backtest/runner_test.go` | 新カラム埋め込み統合テスト | +80 |
| `backend/cmd/event_pipeline.go` | EventBus 配線追加 | +12 |

合計：新規 4、編集 7、約 +750 行 / -0 行。

非対象：

- `RiskHandler`：PR3 で `ActionDecisionEvent` 受け取りに改修
- `RiskManager` の cooldown：PR3
- `BookGate` 有効化：PR4
- frontend：PR5

---

## 3. 実装タスク

### Task 1: PositionView interface とスタブ実装

**目的**: DecisionHandler が依存する型の最小定義。PR3 で本実装に差し替えるための場所決め。

**変更**: `backend/internal/usecase/decision/position_view.go`（新規）

```go
package decision

import (
    "context"
    "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type PositionView interface {
    CurrentSide(ctx context.Context, symbolID int64) entity.OrderSide
}

// FlatPositionView always reports no position. PR2 uses this as the live
// and backtest wiring while DecisionHandler is shadow-only and its output
// drives only the recorder. PR3 swaps in a real implementation that reads
// from PositionManager / SimExecutor.
type FlatPositionView struct{}

func (FlatPositionView) CurrentSide(ctx context.Context, symbolID int64) entity.OrderSide {
    return ""
}
```

**テスト**: `position_view_test.go` で FlatPositionView が常に "" を返すことだけ検証。

**完了判定**: `go build ./internal/usecase/decision/...` 通過。

---

### Task 2: DecisionHandler 本体

**目的**: MarketSignalEvent → ActionDecisionEvent 変換。PR2 の中核。

**変更**: `backend/internal/usecase/decision/handler.go`（新規）

主要な分岐：

```go
func (h *Handler) decide(ms entity.MarketSignal, hold entity.OrderSide) entity.ActionDecision {
    base := entity.ActionDecision{
        SymbolID:  ms.SymbolID,
        Source:    ms.Source,
        Strength:  ms.Strength,
        Timestamp: ms.Timestamp,
    }

    // Cooldown は PR3 で配線。PR2 中は常に false。
    // if h.cooldown.IsEntryCooldown(...) { return COOLDOWN_BLOCKED }

    switch hold {
    case "": // 保有なし
        switch ms.Direction {
        case entity.DirectionBullish:
            base.Intent = entity.IntentNewEntry
            base.Side = entity.OrderSideBuy
            base.Reason = "no position; bullish signal → new long"
        case entity.DirectionBearish:
            base.Intent = entity.IntentNewEntry
            base.Side = entity.OrderSideSell
            base.Reason = "no position; bearish signal → new short"
        default:
            base.Intent = entity.IntentHold
            base.Reason = "no position; neutral signal"
        }
    case entity.OrderSideBuy: // ロング保有中
        switch ms.Direction {
        case entity.DirectionBearish:
            base.Intent = entity.IntentExitCandidate
            base.Side = entity.OrderSideSell
            base.Reason = "long held; bearish signal → exit candidate"
        default: // BULLISH も NEUTRAL も保有を維持
            base.Intent = entity.IntentHold
            base.Reason = "long held; not bearish"
        }
    case entity.OrderSideSell: // ショート保有中
        switch ms.Direction {
        case entity.DirectionBullish:
            base.Intent = entity.IntentExitCandidate
            base.Side = entity.OrderSideBuy
            base.Reason = "short held; bullish signal → exit candidate"
        default:
            base.Intent = entity.IntentHold
            base.Reason = "short held; not bullish"
        }
    }
    return base
}

func (h *Handler) Handle(ctx context.Context, event entity.Event) ([]entity.Event, error) {
    ev, ok := event.(entity.MarketSignalEvent)
    if !ok {
        return nil, nil
    }
    side := h.positions.CurrentSide(ctx, ev.Signal.SymbolID)
    decision := h.decide(ev.Signal, side)
    return []entity.Event{
        entity.ActionDecisionEvent{
            Decision:   decision,
            Price:      ev.Price,
            CurrentATR: ev.CurrentATR,
            Timestamp:  ev.Timestamp,
        },
    }, nil
}
```

**テスト** (`handler_test.go`): 表 §1.4 の 8 ケース + COOLDOWN_BLOCKED 1 ケース（cooldown は PR2 中スキップする旨コメント）。テーブル駆動で。

**完了判定**: `go test ./internal/usecase/decision/...` 緑、特に IsActionable が NEW_ENTRY/EXIT_CANDIDATE で true になることを cross-check。

---

### Task 3: StrategyHandler に MarketSignal 並列発行を追加

**目的**: 旧ルートを温存しつつ新ルートも publish する shadow 動作。

**変更**: `backend/internal/usecase/backtest/handler.go` の `StrategyHandler.Handle`

現行 (handler.go:371-386) を以下のように拡張：

```go
if signal == nil {
    return nil, nil
}

var atr float64
if indicators.ATR != nil {
    atr = *indicators.ATR
}

events := []entity.Event{}

// 旧ルート: BUY/SELL のみ SignalEvent を発行 (HOLD は drop) — 既存挙動維持
if signal.Action != entity.SignalActionHold {
    events = append(events, entity.SignalEvent{
        Signal:     *signal,
        Price:      indicatorEvent.LastPrice,
        Timestamp:  indicatorEvent.Timestamp,
        CurrentATR: atr,
    })
}

// 新ルート: HOLD でも MarketSignalEvent を発行 (NEUTRAL Direction として)
events = append(events, entity.MarketSignalEvent{
    Signal:     toMarketSignal(*signal, indicators),
    Price:      indicatorEvent.LastPrice,
    CurrentATR: atr,
    Timestamp:  indicatorEvent.Timestamp,
})

return events, nil
```

`toMarketSignal` ヘルパーは同じファイルの末尾に追加（§1.3 のスニペット通り）。

**重要**: 旧 SignalEvent の発行条件 (`Action != HOLD`) は **絶対に変えない**。これを変えると現状 backtest で記録される `signal_action='HOLD'` 行が `signal_confidence`/`signal_reason` 込みで上書きされ、recorder の旧カラム挙動が回帰する。

**テスト** (`handler_test.go`):
- BUY signal → SignalEvent + MarketSignalEvent(BULLISH) 両方発行
- SELL signal → SignalEvent + MarketSignalEvent(BEARISH) 両方発行
- HOLD signal → SignalEvent **発行なし** + MarketSignalEvent(NEUTRAL) のみ発行
- nil signal → 何も発行せず

**完了判定**: `go test ./internal/usecase/backtest/... -run StrategyHandler` 緑、既存テスト全部緑（後方互換）。

---

### Task 4: Recorder に新ルート購読を追加

**目的**: decision_log の新カラムを埋め始める。

**変更**: `backend/internal/usecase/decisionlog/recorder.go`

`Handle` の switch に分岐追加：

```go
case entity.MarketSignalEvent:
    r.onMarketSignal(ctx, ev)
case entity.ActionDecisionEvent:
    r.onActionDecision(ctx, ev)
```

新規メソッド：

```go
func (r *Recorder) onMarketSignal(ctx context.Context, ev entity.MarketSignalEvent) {
    if !r.hasPending {
        return
    }
    r.pendingRec.SignalDirection = string(ev.Signal.Direction)
    r.pendingRec.SignalStrength = ev.Signal.Strength
    // signal_reason は旧 onSignal が埋めるので新ルート由来の reason は
    // decision_reason に積む。decision_log の旧カラムには旧ルート由来の値が
    // 残り続ける (互換維持)。
    r.persistPending(ctx, "market_signal")
}

func (r *Recorder) onActionDecision(ctx context.Context, ev entity.ActionDecisionEvent) {
    if !r.hasPending {
        return
    }
    r.pendingRec.DecisionIntent = string(ev.Decision.Intent)
    r.pendingRec.DecisionSide = string(ev.Decision.Side)
    r.pendingRec.DecisionReason = ev.Decision.Reason
    r.persistPending(ctx, "action_decision")
}
```

**テスト** (`recorder_test.go`):
- BUY 旧ルート + BULLISH 新ルート併走 → 全カラム両方埋まる
- HOLD 旧ルート（drop）+ NEUTRAL 新ルート → 新カラムだけ埋まる
- ApprovedSignalEvent / OrderEvent との順序関係（pending 同一行への UPDATE が壊れない）

**完了判定**: `go test ./internal/usecase/decisionlog/... -run "MarketSignal|ActionDecision"` 緑。

---

### Task 5: backtest runner の EventBus 配線

**目的**: backtest 側に新ルートを通す。

**変更**: `backend/internal/usecase/backtest/runner.go` の `Run` 内 (現行 275-293 行)

```go
bus := eventengine.NewEventBus()
bus.Register(entity.EventTypeCandle, 5, tickGenerator)
bus.Register(entity.EventTypeCandle, 10, indicatorHandler)
bus.Register(entity.EventTypeIndicator, 12, tickRiskHandler)
bus.Register(entity.EventTypeTick, 15, tickRiskHandler)
bus.Register(entity.EventTypeIndicator, 20, strategyHandler)
bus.Register(entity.EventTypeSignal, 30, riskHandler)
bus.Register(entity.EventTypeApproved, 40, executionHandler)

// PR2: shadow Decision route. PositionView は flat スタブ (PR3 で実装差替)。
decisionHandler := decision.NewHandler(decision.Config{
    Positions: decision.FlatPositionView{},
})
bus.Register(entity.EventTypeMarketSignal, 27, decisionHandler)

if r.decisionRecorder != nil {
    bus.Register(entity.EventTypeIndicator, 99, r.decisionRecorder)
    bus.Register(entity.EventTypeSignal, 99, r.decisionRecorder)
    bus.Register(entity.EventTypeMarketSignal, 99, r.decisionRecorder)  // 新規
    bus.Register(entity.EventTypeDecision, 99, r.decisionRecorder)       // 新規
    bus.Register(entity.EventTypeApproved, 99, r.decisionRecorder)
    bus.Register(entity.EventTypeRejected, 99, r.decisionRecorder)
    bus.Register(entity.EventTypeOrder, 99, r.decisionRecorder)
}
```

**テスト** (`runner_test.go`): 1 日分の合成 candle で backtest を実行し、`backtest_decision_log` を SELECT して：

- すべての行で `signal_direction` が空でない（BULLISH/BEARISH/NEUTRAL のいずれか）
- すべての行で `decision_intent` が空でない（NEW_ENTRY/HOLD のいずれか — PR2 では FlatPositionView なので EXIT_CANDIDATE は出ない）
- BUY シグナルの行で `decision_side='BUY'`、SELL の行で `decision_side='SELL'`、HOLD の行で `decision_side=''`
- 旧カラム（`signal_action` 等）の値は新ルート導入前と完全一致

**完了判定**: `go test ./internal/usecase/backtest/... -run Runner` 緑、新カラム検証テストが緑。

---

### Task 6: live event_pipeline の EventBus 配線

**目的**: live 側にも新ルートを通す。

**変更**: `backend/cmd/event_pipeline.go` の `setupBus` 相当部分（現行 664-779 行）

backtest と同じ追加：

```go
decisionHandler := decision.NewHandler(decision.Config{
    Positions: decision.FlatPositionView{}, // PR3 で本実装に差替
})
bus.Register(entity.EventTypeMarketSignal, 27, decisionHandler)

// recorder の購読追加
if recorder != nil {
    // ... 既存
    bus.Register(entity.EventTypeMarketSignal, 99, recorder)  // 新規
    bus.Register(entity.EventTypeDecision, 99, recorder)       // 新規
}
```

**テスト**: live 側はフルテスト難しいので、`event_pipeline_test.go` (あれば) でハンドラ登録の存在確認。なければ skip し、Docker 起動 + 1 サイクル動作確認で代替。

**完了判定**: `docker compose up --build -d` で起動 → 30 分監視 → `decision_log` の最新行で新カラムが埋まっていることを確認。

---

### Task 7: 全パッケージ緑 + 動作確認

**コマンド**:

```bash
go test ./... -race -count=1
go vet ./...
```

**動作確認**:

1. **動作不変** (旧ルート挙動)
   - PR2 適用前後で同じ profile / 同じ期間の backtest を 2 回実行
   - 全 metrics (Return, MaxDD, Sharpe, TradeCount) が完全一致

2. **新ルート shadow 動作**
   - backtest 1 本実行 → `backtest_decision_log` の新 6 カラムをサンプル SELECT
   - BUY/SELL/HOLD 各バーで期待値が入っているか目視確認

3. **live shadow 動作**
   - `docker compose up --build -d` → 1 時間 (4 バー分) 待つ
   - `sqlite3 trading.db "SELECT bar_close_at, signal_action, signal_direction, decision_intent, decision_side FROM decision_log ORDER BY id DESC LIMIT 10;"` で新旧カラムが揃っていることを確認

---

## 4. テスト戦略

### 4.1 単体テスト

| 対象 | テスト内容 |
|---|---|
| `FlatPositionView` | 常に "" を返す |
| `Handler.decide` | §1.4 の 8 ケース + cooldown 経路（PR2 中スキップ） |
| `StrategyHandler` (改修後) | BUY/SELL/HOLD/nil の 4 ケースで MarketSignal 発行 |
| `Recorder.onMarketSignal` | 単独受信時の field 更新 |
| `Recorder.onActionDecision` | 単独受信時の field 更新 |

### 4.2 統合テスト

- **新カラム埋め込み**: backtest を 1 日分流して `backtest_decision_log` の新カラムが期待値で埋まる
- **動作不変**: 同じ profile / 同じ期間で PR1 時点の DB スナップショットと PR2 時点を比較し、旧カラム値と metrics が一致
- **HOLD バーの新カラム**: シグナルが HOLD の bar で `signal_direction='NEUTRAL'`, `decision_intent='HOLD'` が入る

### 4.3 既存テスト影響

- `StrategyHandler_test.go` の `len(events) == 1` を期待していたテストは新ルートで 2 events になる（HOLD は 1 event）→ assertion を「BUY/SELL は 2 events、HOLD は 1 event」に修正
- `recorder_test.go` で各イベント単独テストは無影響、組合せテストでは新カラム期待値追加

### 4.4 動作不変の検証スクリプト

```bash
# PR2 適用前（main で）
docker compose down
git checkout main
docker compose up --build -d backend
# backtest を 1 本走らせて DB をダンプ
sqlite3 ... "SELECT signal_action, signal_confidence FROM backtest_decision_log ORDER BY id" > /tmp/before.txt

# PR2 適用後
git checkout feat/decision-handler-shadow
docker compose up --build -d backend
sqlite3 ... "SELECT signal_action, signal_confidence FROM backtest_decision_log ORDER BY id" > /tmp/after.txt

diff /tmp/before.txt /tmp/after.txt   # 空であることを確認
```

ただし backtest ID が違うので別 query にする必要があり、実行は `runner_test.go` 内で同じ candle stream を流して比較する方式にしたほうが堅い。

---

## 5. リスクと緩和

| リスク | 影響 | 緩和 |
|---|---|---|
| HOLD バーで新ルート event が増えて recorder の UPDATE が増える | DB 負荷 | 実測で 15min/bar = 96 events/day、UPDATE 1 回追加でも問題ないレベル |
| MarketSignal の Reason と SignalEvent の Reason が分離 | 旧クエリで decision_reason カラム参照していると壊れる | PR2 では旧 signal_reason は維持、新カラム追加のみ |
| backtest と live で priority 番号がズレる | EventBus dispatch 順が違って数値乖離 | live は `indicatorEventTap` priority 25 が居るが backtest にはいない。Decision priority 27 はどちらでも使えるので問題なし |
| FlatPositionView だと EXIT_CANDIDATE が一度も出ない | テストカバレッジ不足 | DecisionHandler 単体テストで全ケース検証、PR3 で実 PositionView 入れた時の統合テストで補完 |
| 既存 StrategyHandler の HOLD drop を変えてしまう | 旧 signal_action='HOLD' 行に余計な reason が書かれて回帰 | §1.2 §3 task 3 で明示的に「旧 SignalEvent 発行条件は変えない」とコメントを残す |

---

## 6. PR 作成手順

1. ブランチ: `feat/decision-handler-shadow`
2. コミット粒度（5 コミット）:
   - **Commit 1**: PositionView interface + FlatPositionView スタブ
   - **Commit 2**: DecisionHandler + マトリクステスト
   - **Commit 3**: StrategyHandler に MarketSignal 並列発行 + テスト
   - **Commit 4**: Recorder に onMarketSignal / onActionDecision 追加 + テスト
   - **Commit 5**: backtest runner + live event_pipeline の EventBus 配線
3. PR 本文：
   - 「PR2 of 5 (Phase 1 Signal/Decision/ExecutionPolicy)」を冒頭に明記
   - **shadow 動作・実発注経路は PR3** を太字で明記
   - 動作確認結果（DB SELECT 例）を添付
4. CI 緑で squash merge

---

## 7. 完了の定義（DoD）

- [ ] 6 タスクすべて完了
- [ ] `go test ./... -race -count=1` 緑
- [ ] `go vet ./...` 警告なし
- [ ] DecisionHandler の 8 ケースマトリクステストが全部緑
- [ ] backtest を 1 本走らせて新カラムが埋まり、旧カラム値が PR2 適用前と一致
- [ ] live で 1 時間動かして decision_log に新カラムが書かれることを確認
- [ ] `/api/v1/status` `pipelineRunning=true`、auto-trading resumed
- [ ] PR 本文に shadow 動作宣言

---

## 8. 後続 PR への引き継ぎ

PR2 マージ後、PR3（RiskHandler の Decision 化 + cooldown）の plan を書く。その時点で確定する事項：

- PositionView の本実装（PositionManager 経由 / SimExecutor 経由のアダプタ）
- RiskManager.NoteClose を呼ぶタイミング（OrderExecutor.ClosePosition 約定検知ポイント）
- 旧 EventTypeSignal → riskHandler ルートの削除タイミング（PR3 の最終コミット）
- recorder の onSignal / onMarketSignal の重複 reason 処理（旧ルート削除後の整理）

PR3 で意味論が切り替わる（両建て総額判定バグ解消、cooldown 発動）。LTC を flat に戻してから PR3 をマージする運用ルールを思い出す。
