# PR3 Plan: RiskHandler の Decision 化 + EntryCooldown + 旧ルート削除

- 作成日: 2026-05-02
- 親設計書: `docs/design/2026-04-29-signal-decision-policy-separation-design.md`
- 前段 PR: PR1 (#232) 型 / migration、PR2 (#233) shadow 配線
- スコープ: Phase 1 / Stacked PR シリーズ (PR3 ÷ 5)
- **動作変更あり**: ここで意味論が切り替わる。両建て総額判定バグ解消、cooldown 発動

---

## 0. このドキュメントの位置付け

PR1〜PR2 は完全に動作不変だった。**PR3 は Phase 1 のクライマックス**で、実発注経路を旧ルート (EventTypeSignal → RiskHandler) から新ルート (EventTypeDecision → RiskHandler) に切り替える。同時に：

- **両建て総額判定バグ解消**: ロング保有中の BEARISH シグナルが REJECTED されなくなる（DecisionHandler が EXIT_CANDIDATE と判定し、Risk が close 経路で素通り）
- **EntryCooldown 発動**: close 約定後 N 秒は新規エントリーを抑制
- **PositionView 本実装**: FlatPositionView から PositionManager / SimExecutor 経由の adapter に差し替え
- **旧ルート削除**: `EventTypeSignal → RiskHandler` の登録を消す。recorder は受け続けるが、Risk チェックは新ルート一本

PR3 マージ後、bot の挙動は事実上「新システム」に移行する。LTC は **flat に戻してから** マージする運用ルール（設計書 §8.5）。

---

## 1. 設計書からの調整点

### 1.1 既存 cooldown と衝突しない設計

RiskManager には既に `cooldownUntil` / `MaxConsecutiveLosses` / `CooldownMinutes` がある（連敗時 cooldown）。設計書 §5.4 通り、**別フィールド `entryCooldownUntil` を追加**。両者は独立して機能：

| field | 何のため | いつ伸びる | 何を止めるか |
|---|---|---|---|
| `cooldownUntil` (既存) | 連敗時の冷却 | `RecordTrade` で N 連敗到達時 | `CheckOrder` の最初に reject |
| `entryCooldownUntil` (新) | close 直後の再エントリー抑制 | `NoteClose` で close 約定検知時 | DecisionHandler が `IsEntryCooldown` で `COOLDOWN_BLOCKED` 判定 |

DecisionHandler から Risk のロックを取らずに状態を読めるよう `IsEntryCooldown(now)` だけ readlock 経由で公開する。

### 1.2 PositionView の本実装

`OrderExecutor.Positions() []eventengine.Position` が両系統で生えているので、これを叩く adapter を新設：

```go
// usecase/decision/executor_position_view.go
type ExecutorPositionView struct {
    Executor eventengine.OrderExecutor
}

func (v ExecutorPositionView) CurrentSide(ctx context.Context, symbolID int64) entity.OrderSide {
    var longAmount, shortAmount float64
    for _, p := range v.Executor.Positions() {
        if p.SymbolID != symbolID {
            continue
        }
        switch p.Side {
        case entity.OrderSideBuy:
            longAmount += p.Amount
        case entity.OrderSideSell:
            shortAmount += p.Amount
        }
    }
    switch {
    case longAmount > shortAmount:
        return entity.OrderSideBuy
    case shortAmount > longAmount:
        return entity.OrderSideSell
    default:
        return ""
    }
}
```

両建て総額判定バグの根は「長 ¥10,613 + 短 ¥2,665 = ¥13,278 で枠超え」だった。PR3 後は **net side** を返すので、ロング保有中の BEARISH は EXIT_CANDIDATE/SELL となり、RiskHandler 側の close 経路で IsClose=true の素通り判定が効く。

### 1.3 RiskHandler は `ActionDecisionEvent` を受け取る

PR2 までは RiskHandler.Handle が `entity.SignalEvent` 型 assert だった。PR3 では：

```go
func (h *RiskHandler) Handle(ctx context.Context, event entity.Event) ([]entity.Event, error) {
    decisionEvent, ok := event.(entity.ActionDecisionEvent)
    if !ok {
        return nil, nil
    }
    decision := decisionEvent.Decision
    if !decision.IsActionable() {
        return nil, nil // HOLD / COOLDOWN_BLOCKED は何も発行しない
    }
    // ... 以下、Sizer / RiskManager / BookGate / ApprovedSignalEvent
}
```

`ApprovedSignalEvent` は当面 `entity.Signal` を内部に含む構造のままにする（ExecutionHandler が `Signal.Action` / `Signal.Confidence` / `Signal.Urgency` を読む）。Decision → Signal のシグナル相当値を組み立てて詰める：

```go
synthSignal := entity.Signal{
    SymbolID:   decision.SymbolID,
    Action:     toSignalAction(decision.Side), // BUY/SELL
    Confidence: decision.Strength,
    Reason:     decision.Reason,
    Urgency:    "", // PR3 では Urgency は空、Phase 6+ で Decision に乗せる検討
    Timestamp:  decision.Timestamp,
}
```

ExecutionHandler の改修は不要 — ApprovedSignalEvent の入力契約は変えない。

### 1.4 EXIT_CANDIDATE の扱い

設計書 §6 PR3 では「EXIT_CANDIDATE は IsClose=true で Risk を素通り」を想定。実装では：

- **EXIT_CANDIDATE** の場合は OrderProposal の `IsClose=true` をセット、`PositionID` も対象 position から探して入れる
- これにより既存 `RiskManager.CheckOrderAt` の close 経路（risk.go:144-146）に乗る
- ただし: 「実 exit は TP/SL に任せる」という設計書 §4.1 の方針とどう整合させるか

判断: **PR3 では EXIT_CANDIDATE もそのまま発注する**（TP/SL とのレース条件は OrderExecutor 側で natural に解決される、後勝ち）。設計書 §4.1 のコメントは Phase 6+ の慎重ロジック（exit policy 拡張）を見据えた文言で、PR3 では「EXIT_CANDIDATE = 利確/損切りシグナル発火 → Risk 素通り → 即 close 約定」のシンプルな形で十分。

ただしリスクがあるので**プロファイル設定で抑制可能にする**：`risk.exit_on_signal: true/false`。デフォルトは `false`（既存挙動と同じ。EXIT_CANDIDATE は HOLD 同様に何もしない）。`production_ltc_60k.json` で `true` にする決断は本 PR では行わず、PR3 マージ後に PDCA で検証してから手動で flip。

→ 実装簡素化のため **PR3 では `exit_on_signal=false` のみサポート**。EXIT_CANDIDATE は Risk 段階で skip する（HOLD と同じ挙動）。設定キーを足すのは Phase 6+ の課題。

### 1.5 NoteClose の呼び出し点

設計書 §5.4 では「OrderExecutor.ClosePosition 約定検知 → RiskManager.NoteClose」とある。実装上、close 検知のクリーンな点は：

- **EventBus 上の `OrderEvent.ClosedPositionID > 0`** を観測する handler
- それを priority 50 あたりに新設し、約定検知 → `RiskManager.NoteClose(now)` を呼ぶ

新規 handler を作るより、**既存 RiskHandler に `OrderEvent` 購読を追加**する方が依存が浅い。RiskHandler.Handle の switch を拡張：

```go
func (h *RiskHandler) Handle(ctx context.Context, event entity.Event) ([]entity.Event, error) {
    switch ev := event.(type) {
    case entity.ActionDecisionEvent:
        return h.handleDecision(ctx, ev)
    case entity.OrderEvent:
        h.handleOrderEvent(ctx, ev) // close 検知して NoteClose
        return nil, nil
    }
    return nil, nil
}

func (h *RiskHandler) handleOrderEvent(ctx context.Context, ev entity.OrderEvent) {
    if ev.ClosedPositionID > 0 && ev.OrderID > 0 {
        h.RiskManager.NoteClose(time.UnixMilli(ev.Timestamp))
    }
}
```

EventBus に `EventTypeOrder → 50, riskHandler` の登録を追加。priority 50 は executionHandler (40) と recorder (99) の間で問題なし。

### 1.6 RiskConfig.EntryCooldownSec

`entity.RiskConfig` に新フィールド追加：

```go
EntryCooldownSec int `json:"entryCooldownSec,omitempty"` // 0=無効
```

Profile JSON で未設定なら 0（cooldown 無効）。`production_ltc_60k.json` には PR3 では追加しない（PR4 のプロファイル更新と同梱するため）。

---

## 2. ファイル変更マップ

| ファイル | 変更 | 行数目安 |
|---|---|---|
| `backend/internal/domain/entity/risk.go` | EntryCooldownSec フィールド追加 | +3 |
| `backend/internal/usecase/risk.go` | entryCooldownUntil + IsEntryCooldown + NoteClose | +35 |
| `backend/internal/usecase/risk_test.go` | EntryCooldown 動作テスト | +80 |
| `backend/internal/usecase/decision/executor_position_view.go` | **新規** ExecutorPositionView adapter | +40 |
| `backend/internal/usecase/decision/executor_position_view_test.go` | **新規** adapter テスト | +80 |
| `backend/internal/usecase/decision/handler.go` | Cooldown 経路追加 (CooldownChecker 注入) | +25 |
| `backend/internal/usecase/decision/handler_test.go` | COOLDOWN_BLOCKED テスト追加 | +50 |
| `backend/internal/usecase/backtest/handler.go` | RiskHandler を ActionDecisionEvent 入力へ + OrderEvent 購読 | ~70 行差分 |
| `backend/internal/usecase/backtest/handler_test.go` | RiskHandler 新インタフェース対応テスト | ~80 行差分 |
| `backend/internal/usecase/backtest/runner.go` | EventBus 配線変更 (旧 EventTypeSignal→Risk 削除、新 EventTypeDecision→Risk 追加、EventTypeOrder→Risk 追加、PositionView 本実装注入) | ~15 行差分 |
| `backend/cmd/event_pipeline.go` | 同上 (live 側) | ~15 行差分 |

合計：新規 2、編集 8、約 +400 / -100 行。

---

## 3. 実装タスク

### Task 1: RiskConfig + RiskManager 拡張

**目的**: EntryCooldown の状態を持たせる。

**変更**:
- `entity/risk.go` に `EntryCooldownSec int` 追加
- `usecase/risk.go` の RiskManager に：
  - `entryCooldownUntil time.Time` フィールド
  - `IsEntryCooldown(now time.Time) bool` メソッド (RLock)
  - `NoteClose(now time.Time)` メソッド (Lock + cooldown 延長)

**テスト** (`risk_test.go`):
- `IsEntryCooldown` が `entryCooldownUntil` 経過後に false に戻る
- `NoteClose` で cooldown 期間が `EntryCooldownSec` 秒延びる
- `EntryCooldownSec=0` のとき `NoteClose` を呼んでも cooldown は伸びない（no-op）
- 既存の `cooldownUntil` (連敗時) と独立して動く: 片方 active でも片方 expired のまま

**完了判定**: `go test ./internal/usecase/... -run "EntryCooldown"` 緑。既存 `risk_test.go` の連敗 cooldown テストが緑のまま。

---

### Task 2: ExecutorPositionView adapter

**目的**: PositionView の本実装。FlatPositionView を置換できる準備。

**変更**: `backend/internal/usecase/decision/executor_position_view.go` 新規

```go
type ExecutorPositionView struct {
    Executor eventengine.OrderExecutor
}

func (v ExecutorPositionView) CurrentSide(ctx context.Context, symbolID int64) entity.OrderSide {
    if v.Executor == nil {
        return ""
    }
    var longAmount, shortAmount float64
    for _, p := range v.Executor.Positions() {
        if p.SymbolID != symbolID {
            continue
        }
        switch p.Side {
        case entity.OrderSideBuy:
            longAmount += p.Amount
        case entity.OrderSideSell:
            shortAmount += p.Amount
        }
    }
    if longAmount > shortAmount {
        return entity.OrderSideBuy
    }
    if shortAmount > longAmount {
        return entity.OrderSideSell
    }
    return ""
}
```

**テスト**: stub OrderExecutor を渡して以下を検証：
- ポジション 0 件 → ""
- ロング 1 件のみ → "BUY"
- ショート 1 件のみ → "SELL"
- ロング 2 件 + ショート 1 件 (ロング net) → "BUY"
- 同量ロング/ショート → "" (両建て中立は flat 扱い、保守的)
- 別 SymbolID は除外
- nil Executor → ""

**完了判定**: `go test ./internal/usecase/decision/... -run ExecutorPosition` 緑。

---

### Task 3: DecisionHandler に Cooldown 経路を追加

**目的**: RiskManager の `IsEntryCooldown` を読んで `COOLDOWN_BLOCKED` を出す。

**変更**: `usecase/decision/handler.go`

```go
type CooldownChecker interface {
    IsEntryCooldown(now time.Time) bool
}

type Config struct {
    Positions PositionView
    Cooldown  CooldownChecker // optional. nil = cooldown disabled
}

func (h *Handler) decide(ms entity.MarketSignal, hold entity.OrderSide, now time.Time) entity.ActionDecision {
    base := entity.ActionDecision{
        SymbolID: ms.SymbolID, Source: ms.Source, Strength: ms.Strength, Timestamp: ms.Timestamp,
    }

    // Cooldown 中はすべての新規エントリー / EXIT_CANDIDATE を抑制する。
    // 既存 long/short の TP/SL/Trailing は別経路（TickRiskHandler）なので影響しない。
    if h.cooldown != nil && h.cooldown.IsEntryCooldown(now) {
        base.Intent = entity.IntentCooldownBlocked
        base.Reason = "entry cooldown active after recent close"
        return base
    }

    // ... 既存マトリクス
}
```

`Handle` が `now` を渡すために `time.UnixMilli(ev.Timestamp)` を使う（backtest 決定論性のため）。

**テスト**:
- cooldown active 時に Direction にかかわらず COOLDOWN_BLOCKED
- cooldown inactive 時に既存マトリクス通り
- nil CooldownChecker のとき既存挙動 (既存テスト全部緑)

**完了判定**: `go test ./internal/usecase/decision/... -run Handler` 全緑。

---

### Task 4: RiskHandler を Decision 入力へ改修 + OrderEvent 購読

**目的**: 実発注経路の中核。Phase 1 の意味論変更が起こる。

**変更**: `backend/internal/usecase/backtest/handler.go` の `RiskHandler.Handle`

`SignalEvent` 型 assert を `ActionDecisionEvent` に置き換え、Decision → Signal の synth を内部で行う。`Handle` の switch は OrderEvent 用に分岐：

```go
func (h *RiskHandler) Handle(ctx context.Context, event entity.Event) ([]entity.Event, error) {
    switch ev := event.(type) {
    case entity.ActionDecisionEvent:
        return h.handleDecision(ctx, ev)
    case entity.OrderEvent:
        h.handleOrderEvent(ev)
        return nil, nil
    }
    return nil, nil
}
```

`handleDecision` は：
- `IsActionable() == false` → 何もしない（HOLD / COOLDOWN_BLOCKED）
- EXIT_CANDIDATE → `IntentExitCandidate` だけ別経路で skip（PR3 範囲外。reason 付き reject ではなく完全に何もしない）
- NEW_ENTRY → 既存 sizing / risk check / book gate / ApprovedSignalEvent

```go
if decision.Intent != entity.IntentNewEntry {
    return nil, nil
}
synthSignal := entity.Signal{
    SymbolID: decision.SymbolID,
    Action:   sideToAction(decision.Side),
    Confidence: decision.Strength,
    Reason:   decision.Reason,
    Timestamp: decision.Timestamp,
}
// ... sizer / RiskManager.CheckOrder / BookGate / ApprovedSignalEvent
```

`handleOrderEvent` は close 検知して NoteClose：

```go
func (h *RiskHandler) handleOrderEvent(ev entity.OrderEvent) {
    if h.RiskManager == nil {
        return
    }
    if ev.ClosedPositionID > 0 && ev.OrderID > 0 {
        h.RiskManager.NoteClose(time.UnixMilli(ev.Timestamp))
    }
}
```

**重要な互換性**:
- `ApprovedSignalEvent` の構造は変えない（ExecutionHandler がそのまま動く）
- `RejectedSignalEvent` の構造も変えない（recorder がそのまま動く）
- ただし `RejectedSignalEvent.Signal` を埋めるため synthSignal を渡す

**テスト** (`handler_test.go` の RiskHandler セクション):
- ActionDecisionEvent (NEW_ENTRY/BUY) → ApprovedSignalEvent
- ActionDecisionEvent (NEW_ENTRY/SELL) → ApprovedSignalEvent
- ActionDecisionEvent (HOLD) → 何も発行しない
- ActionDecisionEvent (COOLDOWN_BLOCKED) → 何も発行しない
- ActionDecisionEvent (EXIT_CANDIDATE) → 何も発行しない (PR3 スコープ外)
- OrderEvent (ClosedPositionID > 0) → RiskManager.NoteClose 呼び出し検証
- OrderEvent (ClosedPositionID = 0, OpenedPositionID > 0) → NoteClose 呼ばれない
- 既存の Sizer skip / RiskManager veto / BookGate veto 経路がすべて新型で動く

既存テストを `entity.SignalEvent` から `entity.ActionDecisionEvent` への変換に書き換える必要あり。fakeRiskManager の interface 変更があれば追従。

**完了判定**: `go test ./internal/usecase/backtest/... -run RiskHandler` 全緑。

---

### Task 5: backtest runner の EventBus 配線変更

**変更**: `backend/internal/usecase/backtest/runner.go`

```go
// 旧:
//   bus.Register(entity.EventTypeSignal, 30, riskHandler)
// 新:
bus.Register(entity.EventTypeDecision, 30, riskHandler)
bus.Register(entity.EventTypeOrder, 50, riskHandler) // close 検知 → NoteClose

// PositionView を FlatPositionView から本実装へ
decisionHandler := decision.NewHandler(decision.Config{
    Positions: decision.ExecutorPositionView{Executor: simAdapter},
    Cooldown:  rm,
})
```

`simAdapter` は既存の `eventengine.OrderExecutor` 実装が確認済み (handler.go:621 の TickRiskExecutor が同型)。

旧 `EventTypeSignal` ルートは完全削除。recorder の `EventTypeSignal` 購読は残す（StrategyHandler が引き続き出すため、recorder で旧カラムも埋まる — 既存挙動維持）。

**テスト**: `runner_decision_log_test.go` を更新：
- recorder が ActionDecisionEvent を受け取るのは PR2 で確認済み
- 新規追加: backtest 結果 metrics（Total Trades / Return）が PR2 適用前と一致するかを 1 期間で計測（数値乖離なきこと）

**完了判定**: 既存 backtest テスト緑、新規 metrics 一致テスト緑。

---

### Task 6: live event_pipeline の EventBus 配線変更

**変更**: `backend/cmd/event_pipeline.go`

```go
// 旧:
//   bus.Register(entity.EventTypeSignal, 30, riskHandler)
// 新:
bus.Register(entity.EventTypeDecision, 30, riskHandler)
bus.Register(entity.EventTypeOrder, 50, riskHandler)

// PositionView を本実装へ (executor は live の RealExecutor)
decisionHandler := decision.NewHandler(decision.Config{
    Positions: decision.ExecutorPositionView{Executor: executor},
    Cooldown:  p.riskMgr,
})
```

**動作確認**:
- `docker compose up --build -d` → 起動
- `/api/v1/status` で `pipelineRunning=true`
- `decision_log` の最新行で `signal_action` と `decision_intent` が両方埋まることを確認
- 1 時間放置 → cooldown が伸びない（ポジション無いので close 検知なし）

---

### Task 7: 全パッケージ緑 + 動作確認

```bash
go test ./... -race -count=1
go vet ./...
```

**動作確認 (重要)**:

1. **両建て総額判定バグの再現テスト**:
   - 新規ユニットテストで「ロング ¥10,613 + ショート ¥2,665」の状況を構成
   - PR2 適用前と PR3 適用後で挙動が変わることを assert：
     - PR2 まで: BEARISH signal → REJECTED (`position limit exceeded`)
     - PR3 後: BEARISH signal → DecisionHandler が EXIT_CANDIDATE を出す → RiskHandler が skip → recorder に EXIT_CANDIDATE が記録される

2. **動作不変の検証**:
   - 同じプロファイル / 同じ期間の backtest を PR2 / PR3 両方で走らせる
   - **EXIT_CANDIDATE が一度も出ない条件下では metrics 完全一致**を期待
   - 出る条件下では「rejected が減って ApprovedSignalEvent が増える」差分を期待

3. **live cooldown 動作**:
   - `EntryCooldownSec=60` を一時的に設定（プロファイルではなく env 上書き）
   - 手動で position close を 1 回起こす
   - 直後 60 秒間、同じシグナル来ても COOLDOWN_BLOCKED で reject される
   - 60 秒経過後は normal に戻る

---

## 4. テスト戦略

### 4.1 単体テスト

| 対象 | テスト内容 |
|---|---|
| RiskManager.NoteClose / IsEntryCooldown | active/inactive 遷移、`EntryCooldownSec=0` no-op |
| RiskManager 既存 cooldown と独立性 | 連敗 cooldown と新 cooldown が混ざらない |
| ExecutorPositionView | 7 ケース（flat / long-only / short-only / net long / net short / 別 symbol / nil executor） |
| DecisionHandler.decide | cooldown active → COOLDOWN_BLOCKED 全方向 |
| RiskHandler (新型) | ActionDecisionEvent → Approved / Rejected / 何もしない（5 ケース） |
| RiskHandler.handleOrderEvent | close 検知 → NoteClose 呼び出し |

### 4.2 統合テスト

- backtest 1 期間で旧 metrics と一致（EXIT_CANDIDATE 出ない条件）
- backtest 1 期間で EXIT_CANDIDATE が出る条件 → rejected が減ること
- recorder の新カラム埋め込みが PR2 と同等

### 4.3 動作不変が成り立たない条件の事前周知

PR3 マージ後、以下のシナリオで挙動が変わる：

- **ロング保有中の BEARISH**: PR2 まで REJECTED → PR3 後 EXIT_CANDIDATE (Risk skip)
- **ショート保有中の BULLISH**: PR2 まで REJECTED → PR3 後 EXIT_CANDIDATE (Risk skip)
- **close 直後の新規エントリー**: PR2 まで通る → PR3 後 (EntryCooldownSec>0 のとき) COOLDOWN_BLOCKED

これらは設計書 §1.4 のユーザ要件そのもの。回帰ではなく仕様変更。

---

## 5. リスクと緩和

| リスク | 影響 | 緩和 |
|---|---|---|
| 旧 `SignalEvent → riskHandler` ルート削除で何かが壊れる | 発注全停止 | 単体テスト / 統合テスト / Docker 起動確認の 3 段階で検出 |
| ExecutorPositionView の net side 計算が間違っていて bug の修正が逆方向に効く | 両建て総額バグが残る or 別の bug | 7 ケースの単体テスト + 「両建て総額バグ再現テスト」で挙動検証 |
| EntryCooldown の状態管理レース | live で誤判定 | RWMutex で readlock / writelock を区別、go test -race で検出 |
| ApprovedSignalEvent.Signal の Confidence が `decision.Strength` 由来になり、旧 Confidence と微妙に違う | 既存 sizer の confidence scaling が変動 | toMarketSignal で Strength = signal.Confidence をそのまま渡しているので bit-identical |
| OrderEvent 経由で NoteClose を呼ぶ priority 50 配線が executionHandler の前に走ってしまうと未約定で誤検知 | 誤 cooldown | EventBus は priority 昇順なので 40 (execution) → 50 (risk OrderEvent purchase) で順序保証 |
| LTC ポジション保有中に PR3 マージ → 直後 BEARISH で予期せぬ exit 約定 | 実損 | flat 確認後にマージ。本 plan §0 の運用ルール |

---

## 6. PR 作成手順

1. ブランチ: `feat/risk-decision-cutover`
2. コミット粒度（5〜6 コミット）:
   - **Commit 1**: RiskConfig + RiskManager の EntryCooldown フィールドとメソッド (動作影響なし、設定 0 のまま)
   - **Commit 2**: ExecutorPositionView adapter
   - **Commit 3**: DecisionHandler に CooldownChecker 配線 (Cooldown=nil のとき動作不変)
   - **Commit 4**: RiskHandler を ActionDecisionEvent 入力へ改修 + OrderEvent 購読
   - **Commit 5**: backtest runner の EventBus 配線変更（旧ルート削除、新ルート + cooldown / position view 注入）
   - **Commit 6**: live event_pipeline の EventBus 配線変更
3. PR 本文：
   - 「PR3 of 5 (Phase 1 Signal/Decision/ExecutionPolicy)」
   - **動作変更あり** を明記、両建て総額バグ解消が中心
   - 動作確認結果（before/after の DB SELECT 例）
   - LTC flat 確認状況
4. CI 緑で squash merge
5. マージ後、Docker 再起動 → 30 分監視

---

## 7. 完了の定義（DoD）

- [ ] 7 タスクすべて完了
- [ ] `go test ./... -race -count=1` 緑
- [ ] `go vet ./...` 警告なし
- [ ] 両建て総額バグ再現テストが PR3 後で異なる挙動を示す
- [ ] EntryCooldown の active/inactive 遷移テストが緑
- [ ] backtest 1 期間で metrics が EXIT_CANDIDATE が出ない条件で完全一致
- [ ] live: 1 時間動かして decision_log に新カラムが書かれ、`pipelineRunning=true` 維持
- [ ] LTC ポジション flat 確認 (`/positions` が `[]`)
- [ ] PR 本文に動作変更宣言 + before/after の DB 例

---

## 8. 後続 PR への引き継ぎ

PR3 マージ後、PR4 (BookGate 有効化 + プロファイル更新) と PR5 (UI 表示 + cleanup) の plan を書く。PR3 で確定する事項：

- ExecutorPositionView の挙動（同量両建て時の "" 返し方が良いか、PR4 の BookGate と組み合わせて再評価）
- EntryCooldownSec のデフォルト値（PR4 のプロファイル更新で 60 秒で良いか PDCA で sweep する）
- EXIT_CANDIDATE の本格利用は Phase 6+ で `exit_on_signal` 設定を導入してから

PR3 マージ時点で Phase 1 の本質的な変更は完了。PR4 / PR5 は仕上げ。
