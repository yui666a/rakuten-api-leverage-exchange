# Decision Log: 売買判断履歴の永続化と可視化

**Date:** 2026-04-26
**Status:** Approved (design)
**Owner:** yui666a

## 目的

15 分足クローズごとに走るパイプラインの売買判断 (BUY / SELL / HOLD) を、各段階の理由と指標スナップショット込みで永続化し、UI から閲覧できるようにする。

現状、`StrategyHandler` / `RiskHandler` / `BookGate` / `ExecutionHandler` はそれぞれ `Reason` 文字列を生成しているが、HOLD・却下・BookGate veto は `return nil` で破棄されており、約定 (`/history`) は楽天 API 由来の生データしか確認できない。「なぜこの 15 分は何もしなかったか」「なぜこのシグナルが却下されたか」が一切残らない。

## スコープ

- **対象**: ライブ EventDrivenPipeline と Backtest Runner の両方
- **粒度**: 1 バー = 0..N レコード (反対売買・SL/TP 由来の決済も別行で記録)
- **保持**: ライブは無期限、バックテストは 3 日 retention で自動削除
- **UI**: 既存 `/history` 画面に「選択通貨の判断ログ」タブを追加。バックテスト UI は今 PR のスコープ外 (API のみ)
- **用途**: あくまでログ。判断ロジックには一切影響を与えない観測層

## 非スコープ

- 既存の売買ロジック (`StrategyEngine` / `RiskManager` / `BookGate`) の挙動変更
- 集計・ダッシュボード (HOLD 率推移、却下理由の分布等) — 別 PR
- バックテスト結果一覧画面からの「判断ログを見る」リンク — 別 PR
- 認証 (ローカル運用前提なので既存 API と同様に未認証)

## アーキテクチャ概要

```
Live:
  MarketData ─tick→ LiveSource ─candle→ EventBus
                                            │
   ┌──────────┬──────────┬─────────────────┼────────────────────┐
   │priority10│priority20│priority30       │priority40          │priority99
IndicatorH  StrategyH  RiskH/BookGate    ExecutionH         DecisionRecorder
                                                                 │
                                                                 ▼
                                                        decision_log (SQLite)

Backtest:
  CSV ─candle→ EventBus → 同じハンドラ群 + DecisionRecorder
                                                  │
                                                  ▼
                                  backtest_decision_log (SQLite)
                                  (backtest_run_id で run と紐付け)
```

DecisionRecorder は EventBus に **priority 99 の subscriber** として全イベント種に登録され、既存ハンドラのコードに一切手を入れない観測層として動作する。例外として、Risk / BookGate 却下を recorder が検知できるよう、却下時に `RejectedSignalEvent` を新規発火する 1 箇所だけ既存ハンドラに変更を入れる。

## データモデル

### `decision_log` (ライブ)

```sql
CREATE TABLE decision_log (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  bar_close_at    INTEGER NOT NULL,        -- 15分足クローズ時刻 (unix ms)
  sequence_in_bar INTEGER NOT NULL DEFAULT 0,  -- 同一バー内の発火順
  trigger_kind    TEXT    NOT NULL,        -- BAR_CLOSE / TICK_SLTP / TICK_TRAILING

  symbol_id        INTEGER NOT NULL,
  currency_pair    TEXT    NOT NULL,
  primary_interval TEXT    NOT NULL,

  stance          TEXT    NOT NULL,        -- TREND_FOLLOW / CONTRARIAN / BREAKOUT / HOLD
  last_price      REAL    NOT NULL,

  -- Strategy 段
  signal_action     TEXT NOT NULL,         -- BUY / SELL / HOLD
  signal_confidence REAL NOT NULL DEFAULT 0,
  signal_reason     TEXT NOT NULL DEFAULT '',

  -- Risk 段
  risk_outcome    TEXT NOT NULL,           -- APPROVED / REJECTED / SKIPPED
  risk_reason     TEXT NOT NULL DEFAULT '',

  -- BookGate 段
  book_gate_outcome TEXT NOT NULL DEFAULT 'SKIPPED', -- ALLOWED / VETOED / SKIPPED
  book_gate_reason  TEXT NOT NULL DEFAULT '',

  -- Execution 段
  order_outcome    TEXT    NOT NULL,       -- FILLED / FAILED / NOOP
  order_id         INTEGER NOT NULL DEFAULT 0,
  executed_amount  REAL    NOT NULL DEFAULT 0,
  executed_price   REAL    NOT NULL DEFAULT 0,
  order_error      TEXT    NOT NULL DEFAULT '',

  -- ポジション関係
  closed_position_id INTEGER NOT NULL DEFAULT 0,
  opened_position_id INTEGER NOT NULL DEFAULT 0,

  -- 指標スナップショット (フル IndicatorSet を JSON で保持)
  indicators_json           TEXT NOT NULL DEFAULT '{}',
  higher_tf_indicators_json TEXT NOT NULL DEFAULT '{}',

  created_at      INTEGER NOT NULL
);

CREATE INDEX idx_decision_log_symbol_time
  ON decision_log(symbol_id, bar_close_at DESC, sequence_in_bar);
CREATE INDEX idx_decision_log_created
  ON decision_log(created_at);
```

### `backtest_decision_log` (バックテスト)

```sql
CREATE TABLE backtest_decision_log (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  backtest_run_id  TEXT    NOT NULL,       -- backtest_results.id への論理外部キー
  -- 以下は decision_log と同じカラム構成 (上記参照)
  bar_close_at     INTEGER NOT NULL,
  sequence_in_bar  INTEGER NOT NULL DEFAULT 0,
  trigger_kind     TEXT    NOT NULL,
  symbol_id        INTEGER NOT NULL,
  currency_pair    TEXT    NOT NULL,
  primary_interval TEXT    NOT NULL,
  stance           TEXT    NOT NULL,
  last_price       REAL    NOT NULL,
  signal_action     TEXT NOT NULL,
  signal_confidence REAL NOT NULL DEFAULT 0,
  signal_reason     TEXT NOT NULL DEFAULT '',
  risk_outcome      TEXT NOT NULL,
  risk_reason       TEXT NOT NULL DEFAULT '',
  book_gate_outcome TEXT NOT NULL DEFAULT 'SKIPPED',
  book_gate_reason  TEXT NOT NULL DEFAULT '',
  order_outcome     TEXT    NOT NULL,
  order_id          INTEGER NOT NULL DEFAULT 0,
  executed_amount   REAL    NOT NULL DEFAULT 0,
  executed_price    REAL    NOT NULL DEFAULT 0,
  order_error       TEXT    NOT NULL DEFAULT '',
  closed_position_id INTEGER NOT NULL DEFAULT 0,
  opened_position_id INTEGER NOT NULL DEFAULT 0,
  indicators_json           TEXT NOT NULL DEFAULT '{}',
  higher_tf_indicators_json TEXT NOT NULL DEFAULT '{}',
  created_at       INTEGER NOT NULL
);

CREATE INDEX idx_backtest_decision_log_run
  ON backtest_decision_log(backtest_run_id, bar_close_at, sequence_in_bar);
CREATE INDEX idx_backtest_decision_log_created
  ON backtest_decision_log(created_at);
```

### Retention

- `decision_log`: 自動削除なし (手動 SQL のみ)
- `backtest_decision_log`: 起動時 + 1 時間毎に `DELETE FROM backtest_decision_log WHERE created_at < (now - 3 days)` を実行する goroutine を `cmd/main.go` から起動

## 新規ドメインイベント

```go
// entity/decision_event.go
type RejectedSignalEvent struct {
    Signal    Signal
    Stage     string // "risk" | "book_gate"
    Reason    string
    Price     float64
    Timestamp int64
}

func (RejectedSignalEvent) EventType() EventType { return EventTypeRejected }
```

`RiskHandler.Handle` と `BookGate` 経路で「これまで `return nil, nil` していた却下分岐」だけを `return []entity.Event{RejectedSignalEvent{...}}, nil` に置き換える。承認パスは無変更。

## 新規コンポーネント

### Backend

```
backend/internal/domain/entity/
  decision.go                    -- DecisionRecord struct (DB 行に対応)
  decision_event.go              -- RejectedSignalEvent

backend/internal/domain/repository/
  decision_log.go                -- DecisionLogRepository interface
                                    Insert(ctx, DecisionRecord) error
                                    List(ctx, Filter) ([]DecisionRecord, nextCursor, error)
                                    DeleteByBacktestRun(ctx, runID) (int64, error)
                                    DeleteOlderThan(ctx, cutoff) (int64, error)

backend/internal/infrastructure/database/
  decision_log_repo.go           -- live 用 SQLite 実装
  backtest_decision_log_repo.go  -- backtest 用 SQLite 実装
  migrations.go                  -- 上記 2 テーブルを追加 (既存に追記)

backend/internal/usecase/decisionlog/
  recorder.go                    -- DecisionRecorder (EventBus subscriber)
  recorder_test.go
  retention.go                   -- 3日 retention goroutine
  retention_test.go

backend/internal/interfaces/api/handler/
  decision.go                    -- DecisionHandler (live)
  backtest.go                    -- 既存に GetBacktestDecisions / DeleteBacktestDecisions 追加

backend/internal/interfaces/api/router.go
  -- v1.GET("/decisions", decisionHandler.List)
  -- v1.GET("/backtest/results/:id/decisions", backtestHandler.ListDecisions)
  -- v1.DELETE("/backtest/results/:id/decisions", backtestHandler.DeleteDecisions)
```

### Frontend

```
frontend/src/hooks/
  useDecisionLog.ts              -- useQuery + 15s polling + cursor paging

frontend/src/components/
  DecisionLogTable.tsx           -- テーブル + 色分け + 行展開
  DecisionDetailPanel.tsx        -- indicators 詳細展開

frontend/src/lib/api.ts          -- DecisionLogItem 型 + fetchDecisions

frontend/src/routes/history.tsx  -- タブ追加 (既存変更)
```

## DecisionRecorder ステートマシン

```
state: pendingByBar map[bar_close_at] *DraftRecord

[priority 99 で全 EventType に登録]

IndicatorEvent 受領:
  1. 直前バーの draft が pendingByBar に残っていれば
     → 「Strategy が HOLD で何も後続が来なかった」とみなし flush()
  2. 新しい draft を作成
     draft.trigger = BAR_CLOSE
     draft.bar_close_at = event.Timestamp
     draft.sequence_in_bar = 0
     draft.indicators_json = json.Marshal(event.Primary)
     draft.higher_tf_indicators_json = json.Marshal(event.HigherTF)
     draft.stance = currentStance
     draft.last_price = event.LastPrice
     draft.signal_action = "HOLD"  (デフォルト、後で上書き)
     draft.risk_outcome = "SKIPPED"
     draft.order_outcome = "NOOP"
  3. pendingByBar[ts] = draft

SignalEvent 受領:
  draft = pendingByBar[event.Timestamp]
  draft.signal_action = event.Signal.Action
  draft.signal_confidence = event.Signal.Confidence
  draft.signal_reason = event.Signal.Reason

ApprovedSignalEvent 受領:
  draft.risk_outcome = "APPROVED"
  draft.book_gate_outcome = "ALLOWED"  -- ApprovedSignalEvent が来た時点で両方通過済

RejectedSignalEvent 受領:
  if event.Stage == "risk":
    draft.risk_outcome = "REJECTED"
    draft.risk_reason  = event.Reason
  else if event.Stage == "book_gate":
    draft.risk_outcome = "APPROVED"
    draft.book_gate_outcome = "VETOED"
    draft.book_gate_reason  = event.Reason
  flush(draft)

OrderEvent 受領:
  if event.Trigger == "BAR_CLOSE":
    draft = pendingByBar[currentBarTs]
    draft.order_outcome = "FILLED" or "FAILED"
    draft.order_id = event.OrderID
    draft.executed_amount = event.Amount
    draft.executed_price = event.Price
    draft.order_error = event.Error
    draft.opened_position_id = event.PositionID (新規)
    draft.closed_position_id = event.ClosedPositionID (反対売買による決済)
    flush(draft)
  else if event.Trigger == "TICK_SLTP" or "TICK_TRAILING":
    -- バーをまたいで起きる SL/TP/Trailing は別レコードで即時 INSERT
    record = newDraft(trigger=event.Trigger, sequence_in_bar=次の連番)
    record.indicators_json = lastKnownIndicators  -- 最後に確定した指標を流用
    record.order_outcome = "FILLED" / "FAILED"
    record.closed_position_id = event.PositionID
    record.signal_reason = event.Reason  -- "stop_loss" / "take_profit" / "trailing_stop"
    insert(record)

flush(draft):
  repo.Insert(draft)
  delete(pendingByBar, draft.bar_close_at)
```

`OrderEvent` が `bar_close_at` に紐付くか tick 由来かを判別するため、`entity.OrderEvent` に `Trigger` / `ClosedPositionID` フィールドを追加する。これは既存コードの一部変更を伴うが、ExecutionHandler / TickRiskHandler の呼び出し点 4 箇所程度。

## API

### `GET /api/v1/decisions`

クエリパラメータ:
- `symbolId` (省略時: 全シンボル)
- `from` (unix ms, 省略時: 24h 前)
- `to` (unix ms, 省略時: now)
- `limit` (default 200, max 1000)
- `cursor` (id, `WHERE id < cursor` で次ページ送り)

レスポンス:
```json
{
  "decisions": [
    {
      "id": 12345,
      "barCloseAt": 1745654700000,
      "sequenceInBar": 0,
      "triggerKind": "BAR_CLOSE",
      "symbolId": 7,
      "currencyPair": "LTC_JPY",
      "primaryInterval": "PT15M",
      "stance": "TREND_FOLLOW",
      "lastPrice": 30210,
      "signal":   { "action": "BUY", "confidence": 0.72, "reason": "..." },
      "risk":     { "outcome": "APPROVED", "reason": "" },
      "bookGate": { "outcome": "ALLOWED", "reason": "" },
      "order":    { "outcome": "FILLED", "orderId": 887766, "amount": 0.5, "price": 30215, "error": "" },
      "closedPositionId": 0,
      "openedPositionId": 1234,
      "indicators": { /* full IndicatorSet JSON */ },
      "higherTfIndicators": { /* ... */ },
      "createdAt": 1745654702000
    }
  ],
  "nextCursor": 12200,
  "hasMore": true
}
```

### `GET /api/v1/backtest/results/{runId}/decisions`

クエリ: `limit` (default 500, max 5000), `cursor`
レスポンス: 上と同形式 (run スコープなので `symbolId` / `from` / `to` 不要)

### `DELETE /api/v1/backtest/results/{runId}/decisions`

このランの判断ログを 3 日待たずに即時削除。レスポンス: `{ "deleted": 35042 }`

実装ノート:
- ページング: cursor (id) ベース
- `indicators_json` は DB から `TEXT` 取得、ハンドラで `json.RawMessage` として透過送出 (再シリアライズ無し)

## Frontend

### `/history` のタブ構成変更

現在: `[全通貨] [選択通貨]`
変更後: `[全通貨の約定] [選択通貨の約定] [選択通貨の判断ログ]`

判断ログタブは選択通貨 1 つに絞る (全通貨横断は 1 ページに収まらないため)。

### 「判断ログ」タブ

タイムラインテーブル (新しい順):

| 列 | 内容 |
|---|---|
| 時刻 | `barCloseAt` を JST で表示 + `triggerKind` バッジ |
| スタンス | `stance` |
| シグナル | `signal.action` (BUY/SELL/HOLD) |
| 信頼度 | `signal.confidence` (HOLD は `—`) |
| リスク | `risk.outcome` (APPROVED/REJECTED/SKIPPED) |
| BookGate | `bookGate.outcome` (ALLOWED/VETOED/SKIPPED) |
| 結果 | `order.outcome` (FILLED/FAILED/NOOP) |
| 数量/価格 | `order.amount @ order.price` (NOOP は `—`) |
| 理由 | `signal.reason` または `risk.reason` または `bookGate.reason` から最も具体的なものを表示 |

### 行の色分け (背景)
- 緑: `order.outcome == "FILLED"`
- 黄: `signal.action == "HOLD"` かつ `triggerKind == "BAR_CLOSE"`
- 赤: `risk.outcome == "REJECTED"` または `bookGate.outcome == "VETOED"`
- グレー: `triggerKind` が `TICK_SLTP` / `TICK_TRAILING`

### 行展開

クリックで詳細パネルを開き `indicators` を整形表示:
- RSI / MACD hist / ATR / ADX / +DI / -DI / Stoch %K %D
- BB upper/mid/lower + 現在価格との距離
- EMA fast/slow, SMA short/long
- 上位足の指標 (`higherTfIndicators`) を折りたたみで表示

### Pagination

- 初回 200 件取得
- スクロール下端に「もっと見る」ボタン → cursor で追加取得
- 上端は 15 秒間隔の polling で新規行を `prepend`

## エラーハンドリング

- DecisionRecorder の Insert 失敗 → `slog.Warn` で警告ログのみ。**売買ロジックは絶対に止めない** (観測層なのでデータ欠損は許容、ただし監視はしたいので警告ログは必須)
- バックテスト中の Insert 失敗も同様に warn-only
- Retention goroutine の DELETE 失敗 → warn-only、次回再試行
- API 側で indicators_json のパースに失敗 → 該当行を skip して残りを返す

## テスト戦略

### Backend
- `decisionlog/recorder_test.go`:
  - HOLD のみで終わるバー → 1 行 INSERT, signal_action=HOLD, order_outcome=NOOP
  - BUY → APPROVED → FILLED のフルパス → 1 行 INSERT, FILLED
  - BUY → REJECTED (risk) → 1 行 INSERT, risk_outcome=REJECTED, signal_reason 保持
  - BUY → APPROVED → BookGate VETO → 1 行 INSERT, book_gate_outcome=VETOED
  - 同一バーで反対売買 (close + open) → 2 行 INSERT, sequence_in_bar=0,1
  - SL/TP 由来のクローズ → trigger_kind=TICK_SLTP で別行
  - Insert 失敗時にパイプラインが止まらないことを確認
- `decisionlog/retention_test.go`: 3 日経過行が削除されること、3 日未満が残ること
- `database/decision_log_repo_test.go`: CRUD と cursor paging
- 既存の `handler.go` / EventEngine のテストを更新 (`RejectedSignalEvent` 発火)

### Frontend
- `useDecisionLog` のクエリキー安定性 + cursor paging
- `DecisionLogTable` の色分けロジック (snapshot test)
- 行展開の indicators レンダリング

### 統合
- `cmd/sync_state_test.go` 系の流れで EventDrivenPipeline 起動 → ダミーティック投入 → `decision_log` に行が入ることを確認

## マイグレーション戦略

- `migrations.go` の既存 migration リスト末尾に新規 2 テーブル + 3 インデックスを追加
- 既存 DB (named volume) には docker-compose 再起動時に自動で追加 migration が走る
- ロールバックは migrations を逆順実行する仕組みが現状ないので、明示的に `DROP TABLE` する手順を docs に残す (必要時のみ手動)

## 段階的リリース順 (実装順)

1. domain + entity + RejectedSignalEvent 追加
2. migrations + repository + recorder (テスト含む)
3. RiskHandler / BookGate に RejectedSignalEvent 発火を追加 (1 行ずつ)
4. EventDrivenPipeline / Backtest Runner に recorder を DI
5. Retention goroutine + cmd/main.go から起動
6. API ハンドラ + ルート登録
7. Frontend: 型定義 + hook
8. Frontend: テーブル + 詳細パネル + history.tsx タブ追加

各ステップは独立した小 PR にする (Stacked PR を許容)。

## オープンな前提・既知の限界

- **EventBus subscriber 順 / 派生イベントの再投入**: recorder は priority 99 で全 EventType に登録するため、(1) 同一 EventType 内では他ハンドラ (priority 10/20/30/40) より後に呼ばれること、(2) ハンドラが return したイベント (`StrategyHandler` → `SignalEvent`、`RiskHandler` → `ApprovedSignalEvent`/`RejectedSignalEvent`、`ExecutionHandler` → `OrderEvent`) が同一 EventBus に再投入され recorder にも届くこと、の 2 点が EventEngine の不変条件である必要がある。実装着手前に `eventengine.EventBus` / `EventEngine.Run` を読んで両方が成立することを確認する。成立しない場合は recorder を Engine 側のミドルウェアに変更するか、各 Handler から recorder への直接コールに切り替える
- **同一バー内の決済 + 新規開始**: 単一の `OrderEvent` で表現されるか、2 つの `OrderEvent` に分かれるかは実装によって異なる。recorder 側はどちらでも動くよう、`closed_position_id` と `opened_position_id` を 1 行に同居させる構造にしている
- **複数銘柄同時運用**: 現状は 1 銘柄運用なので `pendingByBar` は単一 map で十分。将来の複数銘柄対応時は `map[symbolID]map[barTs]*Draft` に拡張する
- **indicators_json サイズ**: 1 行あたり ~1.5 KB を想定。1 銘柄 1 年で約 53 MB。複数銘柄に拡張する際に retention 検討要
