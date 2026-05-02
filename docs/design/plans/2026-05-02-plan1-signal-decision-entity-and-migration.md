# PR1 Plan: Signal/Decision Entity 追加 + decision_log ALTER TABLE migration

- 作成日: 2026-05-02
- 親設計書: `docs/design/2026-04-29-signal-decision-policy-separation-design.md`
- スコープ: Phase 1 / Stacked PR シリーズ (PR1 ÷ 5)
- 動作影響: **無し**（型と空カラムを追加するだけ）

---

## 0. このドキュメントの位置付け

設計書 §6 PR1 の実装計画。設計書では「entity / event 型と decision_log への ALTER」と一括りに書かれているが、実装の段取りと検証手順をここで具体化する。

このプランは PR2 以降の前提。PR2 で DecisionHandler を新設するときに参照する型と、recorder が新カラムに値を入れ始めるための土台を整える。

PR1 マージ時点の **観測可能な変化**:

- `backend/internal/domain/entity/` に新ファイル 2 つ
- `backtest_event.go` に EventType 定数 2 つ追加
- `decision_log` / `backtest_decision_log` テーブルにカラム 6 列追加（既存行は空文字 / 0）
- `DecisionRecord` 構造体に対応フィールド 6 つ追加
- `decision_log_repo` / `backtest_decision_log_repo` の `INSERT` / `UPDATE` / `SELECT` が新カラムを扱う（PR1 中は常に空）
- backtest / live の挙動は **完全に同じ**（新型を生成・publish するコードはまだ無い）

---

## 1. 設計書からの調整点

設計書 §5 と現状コードを突き合わせて以下を確定する：

### 1.1 既存 `decision.go` との衝突回避

設計書 §5.1 では「`backend/internal/domain/entity/decision.go` に追記」とあるが、既存ファイルは **`DecisionRecord`（永続化用 entity）** が住んでいる。`DecisionIntent` / `ActionDecision` を同じファイルに混ぜると意味論が紛れる。

→ **新規ファイル `action_decision.go` に分離する**。`MarketSignal` は `market_signal.go`。`backtest_event.go` の EventType 定数追加だけは既存ファイルに直書き。

### 1.2 DecisionRecord 拡張

decision_log の新カラムを書くために `DecisionRecord` に 6 フィールド追加：

```go
SignalDirection    string  // "BULLISH" | "BEARISH" | "NEUTRAL" | ""
SignalStrength     float64
DecisionIntent     string  // "NEW_ENTRY" | "EXIT_CANDIDATE" | "HOLD" | "COOLDOWN_BLOCKED" | ""
DecisionSide       string  // "BUY" | "SELL" | ""
DecisionReason     string
ExitPolicyOutcome  string  // PR4 で BookGate 経由の出口判断記録に使う予定
```

PR1 ではフィールドを足すだけで誰も書き込まない（recorder は PR2 で対応）。`ToJSON` / `FromJSON` 系の関数があれば追従する。

### 1.3 Repo 層の SQL 更新

`Insert` / `InsertAndID` / `Update` / `Select` 系 4 メソッドの SQL に新カラムを足す。PR1 中は値は常に空文字 / 0 で、既存行を読み出した時に新カラム読み出し位置に空が入るだけ。テストで明示的に確認する。

### 1.4 マイグレーション分割

`migrations.go` の末尾に `addDecisionLogV2Columns(db)` ヘルパーを追加し `RunMigrations` から呼ぶ。`addColumnIfNotExists` を 12 回呼ぶ（6 列 × 2 テーブル）。冪等。

---

## 2. ファイル変更マップ

| ファイル | 変更 | 行数目安 |
|---|---|---|
| `backend/internal/domain/entity/market_signal.go` | 新規 | ~50 |
| `backend/internal/domain/entity/market_signal_test.go` | 新規 | ~30 |
| `backend/internal/domain/entity/action_decision.go` | 新規 | ~70 |
| `backend/internal/domain/entity/action_decision_test.go` | 新規 | ~40 |
| `backend/internal/domain/entity/backtest_event.go` | EventType 定数 2 つ追加 | +3 |
| `backend/internal/domain/entity/decision.go` | DecisionRecord に 6 フィールド追加 | +7 |
| `backend/internal/infrastructure/database/migrations.go` | addDecisionLogV2Columns 追加 + 呼び出し | +35 |
| `backend/internal/infrastructure/database/migrations_test.go` | 新カラム存在検証テスト追加 | +50 |
| `backend/internal/infrastructure/database/decision_log_repo.go` | INSERT/UPDATE/SELECT 更新 | ~30 行差分 |
| `backend/internal/infrastructure/database/decision_log_repo_test.go` | 新カラム round-trip テスト | +60 |
| `backend/internal/infrastructure/database/backtest_decision_log_repo.go` | 同上 | ~30 |
| `backend/internal/infrastructure/database/backtest_decision_log_repo_test.go` | 同上 | +60 |

合計：新規 4、編集 8、約 +500 行 / -0 行。

非対象：

- `recorder.go` / `backtest_adapter.go`：PR2 で更新
- `runner.go` / `event_pipeline.go`：PR2 で更新
- frontend：PR5 で更新

---

## 3. 実装タスク

各タスクは独立してテストを通すことを意識して順序付けする。

### Task 1: EventType 定数の追加

**目的**: 後続コードからシンボル参照だけは可能にする。型本体はまだ無くてよい。

**変更**:
- `backtest_event.go` に追加：
  ```go
  EventTypeMarketSignal = "market_signal"
  EventTypeDecision     = "decision"
  ```

**テスト**: 既存の backtest_event_test.go があれば定数値を assert。なければ skip。

**完了判定**: `cd backend && go build ./...` が通る。

---

### Task 2: `MarketSignal` entity の新規作成

**目的**: Strategy → Decision の payload 型を確立する。

**変更**: `backend/internal/domain/entity/market_signal.go` 新規作成。

```go
package entity

// SignalDirection は市況の方向性を表す。Signal レイヤは BUY/SELL のような
// 注文サイドではなく、市場解釈としての BULLISH/BEARISH/NEUTRAL を返す。
// 注文サイドへの変換は Decision レイヤの責務。
type SignalDirection string

const (
    DirectionBullish SignalDirection = "BULLISH"
    DirectionBearish SignalDirection = "BEARISH"
    DirectionNeutral SignalDirection = "NEUTRAL"
)

// MarketSignal は Strategy が指標から導いた状況解釈。Direction は方向、
// Strength は確信度（0.0〜1.0）。Source は由来戦略（例: "contrarian:rsi"）。
type MarketSignal struct {
    SymbolID   int64
    Direction  SignalDirection
    Strength   float64
    Source     string
    Reason     string
    Indicators IndicatorSet
    Timestamp  int64
}

// MarketSignalEvent は EventBus に流れるイベント。Decision レイヤが購読する。
type MarketSignalEvent struct {
    Signal     MarketSignal
    Price      float64
    CurrentATR float64
    Timestamp  int64
}

func (e MarketSignalEvent) EventType() string     { return EventTypeMarketSignal }
func (e MarketSignalEvent) EventTimestamp() int64 { return e.Timestamp }
```

**テスト** (`market_signal_test.go`):
- `MarketSignalEvent.EventType()` が `"market_signal"` を返す
- `EventTimestamp()` が Timestamp フィールドをそのまま返す
- 各 `SignalDirection` 定数の文字列値検証

**完了判定**: `go test ./internal/domain/entity/... -run MarketSignal` 緑。

---

### Task 3: `ActionDecision` entity の新規作成

**目的**: Decision レイヤの出力型。Intent + Side + 由来情報を保持。

**変更**: `backend/internal/domain/entity/action_decision.go` 新規作成。

```go
package entity

// DecisionIntent は Decision レイヤが下した行動意図。Side と組み合わせて
// ExecutionPolicy（Risk/BookGate/Executor）が解釈する。
type DecisionIntent string

const (
    IntentNewEntry        DecisionIntent = "NEW_ENTRY"
    IntentExitCandidate   DecisionIntent = "EXIT_CANDIDATE"
    IntentHold            DecisionIntent = "HOLD"
    IntentCooldownBlocked DecisionIntent = "COOLDOWN_BLOCKED"
)

// ActionDecision は Decision レイヤの判定結果。
// Intent=HOLD/COOLDOWN_BLOCKED の時 Side は空文字。
// Strength と Source は由来 MarketSignal から継承し、サイジング/ログに使う。
type ActionDecision struct {
    SymbolID  int64
    Intent    DecisionIntent
    Side      OrderSide
    Reason    string
    Source    string
    Strength  float64
    Timestamp int64
}

// IsActionable は Intent が実行を伴う種類か（NEW_ENTRY / EXIT_CANDIDATE）。
// HOLD / COOLDOWN_BLOCKED は false。後段ハンドラの分岐用。
func (d ActionDecision) IsActionable() bool {
    return d.Intent == IntentNewEntry || d.Intent == IntentExitCandidate
}

type ActionDecisionEvent struct {
    Decision   ActionDecision
    Price      float64
    CurrentATR float64
    Timestamp  int64
}

func (e ActionDecisionEvent) EventType() string     { return EventTypeDecision }
func (e ActionDecisionEvent) EventTimestamp() int64 { return e.Timestamp }
```

**`OrderSide` の所在確認**: `entity/order.go` を読んで型名を確認。違う名前なら適合させる。

**テスト** (`action_decision_test.go`):
- `IsActionable` が NEW_ENTRY/EXIT_CANDIDATE で true、HOLD/COOLDOWN_BLOCKED で false
- `ActionDecisionEvent.EventType()` が `"decision"` を返す
- 各 `DecisionIntent` 定数の文字列値検証

**完了判定**: `go test ./internal/domain/entity/... -run ActionDecision` 緑。

---

### Task 4: DecisionRecord に新フィールド追加

**目的**: PR2 以降で recorder が新ロジック由来の値を保存できるよう箱を作る。PR1 では何も書かない。

**変更**: `backend/internal/domain/entity/decision.go` の `DecisionRecord` に追加：

```go
// Phase 1 (Signal/Decision/ExecutionPolicy 三層分離) で追加。PR1 時点では
// 全行で空文字 / 0。recorder が値を入れ始めるのは PR2 から。
SignalDirection   string  // SignalDirection の string 形 ("BULLISH" 等)
SignalStrength    float64
DecisionIntent    string  // DecisionIntent の string 形
DecisionSide      string  // OrderSide の string 形
DecisionReason    string
ExitPolicyOutcome string  // PR4 で BookGate 経由の出口判断結果を入れる予定
```

**テスト**: 既存 `decision_test.go` でフィールド初期値が空であることを 1 ケース追加。

**完了判定**: `go build ./...` が通り、既存テストが緑。

---

### Task 5: マイグレーションに ALTER TABLE を追加

**目的**: テーブルに 6 列追加。冪等。既存 DB を破壊しない。

**変更**: `migrations.go` の `RunMigrations` 末尾に呼び出し追加 + ヘルパー関数定義。

```go
// addDecisionLogV2Columns adds Phase 1 columns to decision_log /
// backtest_decision_log. Idempotent via addColumnIfNotExists.
// Defaults are empty string / 0 so existing rows remain valid.
func addDecisionLogV2Columns(db *sql.DB) error {
    cols := []struct{ name, def string }{
        {"signal_direction",    "signal_direction TEXT NOT NULL DEFAULT ''"},
        {"signal_strength",     "signal_strength REAL NOT NULL DEFAULT 0"},
        {"decision_intent",     "decision_intent TEXT NOT NULL DEFAULT ''"},
        {"decision_side",       "decision_side TEXT NOT NULL DEFAULT ''"},
        {"decision_reason",     "decision_reason TEXT NOT NULL DEFAULT ''"},
        {"exit_policy_outcome", "exit_policy_outcome TEXT NOT NULL DEFAULT ''"},
    }
    for _, t := range []string{"decision_log", "backtest_decision_log"} {
        for _, c := range cols {
            if err := addColumnIfNotExists(db, t, c.name, c.def); err != nil {
                return fmt.Errorf("add %s.%s: %w", t, c.name, err)
            }
        }
    }
    return nil
}
```

`RunMigrations` 末尾、`for _, stmt := range decisionLogTables` ループの直後で呼ぶ：

```go
if err := addDecisionLogV2Columns(db); err != nil {
    return err
}
```

**テスト** (`migrations_test.go` に追加):
- 既存 DB に対して `RunMigrations` を 2 回叩いてもエラーにならない（冪等）
- マイグレーション後 `PRAGMA table_info(decision_log)` で 6 列の存在を検証
- 同上 `backtest_decision_log`
- 既存テーブルにダミー行を 1 行入れた状態でマイグレーションを走らせ、データが消えないことを確認

**完了判定**: `go test ./internal/infrastructure/database/... -run TestMigrations` 緑。

---

### Task 6: decision_log_repo の SQL を新カラム対応へ

**目的**: PR1 中は常に空文字 / 0 を書くが、INSERT / UPDATE / SELECT の SQL は新カラムを扱える状態にしておく。PR2 で recorder が値を渡せばすぐ動く。

**変更**: `decision_log_repo.go`

- `Insert` / `InsertAndID` / `Update` の SQL に 6 列追加（順序: signal_direction → exit_policy_outcome の順で末尾に追加）
- バインドする値は `rec.SignalDirection` 等を使う（PR1 では呼び元が空のまま）
- SELECT 系（`GetLatest`, `ListBySymbol` 等）の列リストにも追加し、`Scan` でフィールドに読み込む

`backtest_decision_log_repo.go` も同様。

**テスト** (`decision_log_repo_test.go`, `backtest_decision_log_repo_test.go`):
- 新フィールドに値を詰めて `Insert` → `GetByID` で取り出して同値であることを確認（"BULLISH"/0.7/"NEW_ENTRY"/"BUY"/"reason"/""）
- 空のまま Insert → 取り出した時にゼロ値（空文字 / 0）であること
- `Update` 経由でも同じ往復

**完了判定**: `go test ./internal/infrastructure/database/... -run DecisionLog` 緑、`go test ./internal/infrastructure/database/... -run BacktestDecisionLog` 緑。

---

### Task 7: 全パッケージ緑 + 動作不変の確認

**確認手順**:

```bash
cd backend && go test ./... -race -count=1
```

期待: 全パッケージ緑。新カラム関連のテストが 6〜10 件追加で増える。

```bash
cd backend && go vet ./...
```

期待: 警告なし。

**動作不変の確認**:
- 既存 `live` 起動 → 何もしないで `decision_log` を覗いて、新カラムが空で書かれていることを確認
- 既存 `backtest` を 1 本流して、`backtest_decision_log` の新カラムが空で書かれていることを確認
- `recorder.go` のシグナル受信→記録のロジックは PR1 で触っていないので、旧フィールドは引き続き正しく埋まる

```bash
docker compose up --build -d backend
sleep 5
docker compose logs backend | grep -i "migration\|panic" | head -20
docker compose exec backend sqlite3 /data/trading.db "PRAGMA table_info(decision_log);" | grep -E "signal_direction|decision_intent"
```

期待:
- migration ログに新カラム追加メッセージなし（既存テーブルへの ALTER は正常）
- `PRAGMA table_info` で新カラムが見える
- 既存 `decision_log` 行の新カラムは空文字 / 0
- `/api/v1/status` が `pipelineRunning` を含む正常レスポンス（PR #231 で追加済み）

---

## 4. テスト戦略

### 4.1 単体テスト

| 対象 | テスト |
|---|---|
| `MarketSignalEvent` | EventType / Timestamp |
| `ActionDecision` | IsActionable の 4 ケース |
| `ActionDecisionEvent` | EventType / Timestamp |
| `addDecisionLogV2Columns` | 冪等性、列追加検証、データ保護 |
| `decisionLogRepo` | 新カラム round-trip（INSERT→SELECT、UPDATE→SELECT） |
| `backtestDecisionLogRepo` | 同上 |

### 4.2 既存テストへの影響

- `recorder_test.go` / `backtest_adapter_test.go` は `DecisionRecord` のフィールド追加で影響なし（フィールド追加は加算的変更）
- 既存 `decision_log_repo_test.go` のフィクスチャ生成で `SignalDirection: ""` 等を明示する必要は無い（Go のゼロ値）

### 4.3 動作回帰テスト

PR1 自体は新コードを呼ぶ箇所が無いので、シグナル経路の挙動は理論上変わらない。とはいえ念のため：

- 短い backtest（CSV 1 日分）を 1 本流す
- 結果 metrics（Return, MaxDD, TradeCount）が PR1 適用前と一致することを目視確認

---

## 5. リスクと緩和

| リスク | 影響 | 緩和 |
|---|---|---|
| ALTER TABLE が既存行に NULL を入れる | 既存 SELECT が壊れる | `NOT NULL DEFAULT ''` / `DEFAULT 0` を全列に付ける |
| migration が冪等でない | 2 回目以降 backend 起動でエラー | `addColumnIfNotExists` ヘルパー（既存）を使う、テストで 2 回呼ぶ |
| Repo SQL に新カラム名のスペルミス | 即 INSERT エラー | テーブル定義と repo SQL の列リストを並べた diff レビューを self-check |
| Production DB との順番ずれ | カラム順が test と prod で違う | SELECT は列名を明示しているので順番は問題にならない（位置指定 SCAN ではなく列名駆動） |
| backtest_decision_log のサイズ膨張 | ALTER TABLE が遅い | 6 列追加なので軽量。既存 PR でも実績あり |

---

## 6. PR 作成手順

1. ブランチ: `feat/signal-decision-entity-v2`
2. コミット粒度（レビュー観点）：
   - **Commit 1**: EventType 定数 + `MarketSignal` entity + テスト
   - **Commit 2**: `ActionDecision` entity + テスト
   - **Commit 3**: `DecisionRecord` フィールド追加
   - **Commit 4**: migration `addDecisionLogV2Columns` + テスト
   - **Commit 5**: `decision_log_repo` / `backtest_decision_log_repo` SQL 更新 + テスト
3. PR 本文：
   - 設計書 §6 PR1 の実装である旨を冒頭で明示
   - 「動作不変」を太字で明記
   - 後続 PR（PR2: DecisionHandler 新設）への接続を記す
4. CI 緑で squash merge

---

## 7. 完了の定義（DoD）

- [ ] 6 タスクすべて完了
- [ ] `cd backend && go test ./... -race -count=1` 緑
- [ ] `cd backend && go vet ./...` 警告なし
- [ ] `docker compose up --build -d` で backend が起動し migration が走る
- [ ] `PRAGMA table_info(decision_log)` で 6 新カラムが見える
- [ ] `PRAGMA table_info(backtest_decision_log)` で 6 新カラムが見える
- [ ] backtest を 1 本走らせて旧 metrics と完全一致を確認
- [ ] PR 本文に設計書リンク + 動作不変宣言を含める

---

## 8. 後続 PR への引き継ぎ

PR1 が main にマージされたら、PR2（Decision レイヤ新設 + StrategyHandler 改修）の plan を書く。その時点で確定する／確定したい事項：

- `DecisionHandler` のインターフェース（`OnIndicator(ev IndicatorEvent)` を持つか、別の入口にするか）
- `StrategyHandler` から `MarketSignalEvent` と既存 `SignalEvent` を**両方** publish する shadow 動作の具体形
- recorder が `MarketSignalEvent` / `ActionDecisionEvent` を購読する際の subscribe priority
- backtest runner と event_pipeline の EventBus 設定差分

これらは PR1 の実装中に判断材料が増えるので、PR2 plan を書く際に再評価する。
