# Signal / Decision / ExecutionPolicy 三層分離 設計

- 作成日: 2026-04-29
- 著者: 共同 (Daisy + Claude)
- ステータス: ドラフト (レビュー前)
- 関連 Issue: #221 (BookGate disabled)、`MEMORY.md` の "/status running は嘘をつく"
- 関連 PR: TBD (stacked、Phase 1〜5)

---

## 1. 背景と問題提起

### 1.1 きっかけになった事象

2026-04-28 23:45〜2026-04-29 12:15 (LTC/JPY、production_ltc_60k 運用) で、CONTRARIAN 戦略が連続して BUY → SELL シグナルを生成したが、いずれも `risk_outcome=REJECTED` で却下された。decision_log の risk_reason は全て同じ：

```
position limit exceeded: 10613+1750 > 12000   (BUY 連発時)
position limit exceeded: 10613+2665 > 12000   (SELL 連発時)
```

### 1.2 直接原因

`backend/internal/usecase/risk.go:174-180` の `MaxPositionAmount` ガードは、**新規発注 side を考慮せず、既存ポジション評価額に発注額を単純加算して判定している**。既存ロング ¥10,613 を保有中に、反対側の SELL ¥2,665 を出そうとすると合計 ¥13,278 として扱われ、12,000 上限を超えるため拒否された。

本来であれば：
- 既存ロングの**決済 (close)** であれば `IsClose=true` 経路で素通り (現行コード `risk.go:144-146` で対応済み)
- **新規ショート (open)** であればネットエクスポージャ |ロング - ショート| = ¥7,948 で枠内
- どちらでも通るべき発注が、両建て総額判定で全て弾かれた

### 1.3 構造的問題

直接原因を symptom-fix で直すこともできる (case-by-case で risk.go を直す) が、根は構造にある：

**Signal レイヤが状況解釈と意思決定を兼ねている**

現状の `StrategyHandler` は IndicatorEvent を受けて `SignalEvent{Signal{Action: BUY|SELL|HOLD}}` を吐く。この `Action` の意味論は文脈依存で曖昧：

- 「ロング保有なし」での SELL → 新規ショート開設
- 「ロング保有中」での SELL → 利確 close を期待される (が、現実装は新規ショートとして risk に投げられる)
- 「ショート保有中」での SELL → 増し玉

受け手 (RiskHandler / ExecutionHandler) が「この SELL がどの SELL なのか」を逆算する仕組みになっていない。結果として：

1. **Risk チェックが side 不可知のまま総額判定**: 上記バグ
2. **Stance / Signal の反対方向が exit 候補として機能しない**: 出口は ATR ベース TP/SL 専属で、シグナルの反転を見ない
3. **テスト容易性が低い**: "状況解釈"と"意思決定"が同じ関数の中に閉じている

### 1.4 ユーザ要件 (本設計の出発点)

ユーザとの議論で確定した方針：

- **シグナル ≠ 売却の合図**。シグナルはあくまで「これから取りたい新規エントリー方向」を表すものとする
- **売却 (利確/損切り) は別ロジック**。既存の TP/SL/Trailing をそのまま信頼する
- **売却後の再エントリーは別判断**。close 直後にすぐ次の発注を出すことはしない (cooldown)
- **市場インパクトを最小化したい**。LTC/JPY 楽天 CFD は板が薄く、自分の発注で価格が動きやすい
- **理想構造を採用する**。既存実装からの最小差分ではなく、システムとして整合する設計を選ぶ

---

## 2. ゴール

### 2.1 アーキテクチャ目標

`Signal` / `Decision` / `ExecutionPolicy` の三層分離を導入する：

1. **Signal レイヤ**: 市況の解釈に専念。`Direction (BULLISH/BEARISH/NEUTRAL)` と `Strength (0.0-1.0)` のみを返す。`BUY/SELL` の言語は持たない
2. **Decision レイヤ**: Signal とポジション保有状況、cooldown 状態を組み合わせて `Intent (NEW_ENTRY/EXIT_CANDIDATE/HOLD/COOLDOWN_BLOCKED)` と `Side (BUY/SELL)` を決める
3. **ExecutionPolicy レイヤ**: Decision を受け、Risk チェック、BookGate チェックを通したうえで Order に落とす

### 2.2 機能要件

- ロング保有中に CONTRARIAN BEARISH シグナルが発生しても、両建て総額判定で誤拒否されない
- close (= EXIT_CANDIDATE) 直後 N 秒は新規エントリーを抑制する (open 同士の cooldown は無し)
- BookGate (`booklimit.Gate`) を production_ltc_60k で有効化する
- 出口ロジック (TP/SL/Trailing) は変更しない
- backtest と live が同じレイヤ構成・同じ判定ロジックで動く

### 2.3 非機能要件

- 既存 decision_log のスキーマは破壊しない (新カラム追加のみ)
- 既存 UI (history タブ等) は Phase 1〜4 中も継続して動く
- backtest の過去結果と新ロジックの結果がパフォーマンス指標 (Return, MaxDD, etc.) で大きく乖離しないこと (本質的には決済タイミングが変わっていないため、同等のはず)

---

## 3. 非ゴール

以下は本設計の対象外とする：

- **出口ロジックの拡張**: TP/SL/Trailing 以外の出口判定 (例: signal 反転を exit 条件にする) は将来課題
- **指値 close API の実装**: 現状 `OrderExecutor.ClosePosition` は成行のみ。指値での close 機能拡張は別 PR
- **複数銘柄対応の見直し**: シンボル横断ポジション集計、ヘッジ運用などは将来課題
- **既存プロファイルの数値最適化**: パラメータ sweep は PDCA の仕事であって、本設計のスコープではない
- **楽天 API 直叩きでの手動 close フロー**: 本設計後は bot 経由で完結する想定
- **/positions と /risk/status の整合性問題** (in-memory positions と exchange-side のずれ): 別途切り出す

---

## 4. アーキテクチャ

### 4.1 三層の責務

```
┌──────────────────────────────────────────────────────────┐
│ 1. Signal レイヤ (StrategyHandler)                        │
│    入力: IndicatorEvent                                    │
│    出力: MarketSignalEvent                                │
│    責務: 指標の状況解釈のみ。Direction / Strength を吐く  │
│         BUY/SELL の言語は使わない                          │
└──────────────────────────────────────────────────────────┘
                          ↓ MarketSignalEvent
┌──────────────────────────────────────────────────────────┐
│ 2. Decision レイヤ (DecisionHandler ← 新設)               │
│    入力: MarketSignalEvent + 現在のポジション + cooldown   │
│    出力: ActionDecisionEvent                              │
│    責務: Direction を Intent + Side に翻訳                │
│      - 保有なし + BULLISH        → NEW_ENTRY/BUY          │
│      - 保有なし + BEARISH        → NEW_ENTRY/SELL         │
│      - ロング中 + BEARISH        → EXIT_CANDIDATE/SELL    │
│        (※ Phase 1 では HOLD として扱う。実 exit は       │
│         TP/SL に任せる。Phase 2 以降に exit 拡張余地)     │
│      - ロング中 + BULLISH        → HOLD (増し玉しない)    │
│      - cooldown 中              → COOLDOWN_BLOCKED        │
│      - NEUTRAL                   → HOLD                    │
└──────────────────────────────────────────────────────────┘
                          ↓ ActionDecisionEvent
┌──────────────────────────────────────────────────────────┐
│ 3. ExecutionPolicy レイヤ                                 │
│    (RiskHandler + BookGate, 既存 OrderExecutor)           │
│    入力: ActionDecisionEvent                              │
│    出力: ApprovedDecisionEvent / RejectedDecisionEvent    │
│         → OrderExecutor で実発注                          │
│    責務:                                                   │
│      - サイジング (riskMgr.SizeOrder)                     │
│      - Risk ガード (残高、daily loss、cooldown 残り等)    │
│      - BookGate (板厚、想定スリッページ)                  │
│      - cooldown 状態の更新 (close 約定検知 → cooldown 開始)│
└──────────────────────────────────────────────────────────┘
```

### 4.2 EventBus 配線

既存 priority 体系に乗せる。`backend/internal/usecase/backtest/runner.go:276-` の登録順を踏襲。

```
EventTypeCandle      → priority 5  : tickGenerator
EventTypeCandle      → priority 10 : indicatorHandler
EventTypeIndicator   → priority 12 : tickRiskHandler (TP/SL 用、変更なし)
EventTypeTick        → priority 15 : tickRiskHandler (TP/SL 用、変更なし)
EventTypeIndicator   → priority 20 : strategyHandler ← (Direction/Strength のみに変更)
EventTypeMarketSig   → priority 25 : decisionHandler ← (新設)
EventTypeDecision    → priority 30 : riskHandler     ← (リネーム + Decision 受け取りに変更)
EventTypeApproved    → priority 40 : executionHandler
EventTypeIndicator   → priority 25 : indicatorEventTap (recorder 用、変更なし)
EventTypeXxx         → priority 99 : recorder (新 Event を購読するよう拡張)
```

priority 25 が indicatorEventTap と decisionHandler で重複する場合は、decision を 27、tap を 25 のまま等で調整する (実装時に確認)。

### 4.3 Cooldown の所在

Cooldown 状態は **RiskManager** に持たせる。理由：

- 既存の `cooldownUntil`, `consecutiveLosses` 等のフィールドが既にある
- DecisionHandler は自分自身の状態を持たない (RiskManager から保有・cooldown を読むだけ)。ハンドラ自体はテスト時に mockable な依存注入の形にする
- ExecutionHandler が close 約定を検知した時に `RiskManager.NoteClose()` を呼ぶ → RiskManager が `entryCooldownUntil` を更新

DecisionHandler は判定時に `RiskManager.IsEntryCooldown(now)` を読むだけ。

---

## 5. 主要 entity / 型の変更

### 5.1 新規型

```go
// backend/internal/domain/entity/market_signal.go (新規ファイル)

type SignalDirection string

const (
    DirectionBullish SignalDirection = "BULLISH"
    DirectionBearish SignalDirection = "BEARISH"
    DirectionNeutral SignalDirection = "NEUTRAL"
)

type MarketSignal struct {
    SymbolID   int64
    Direction  SignalDirection
    Strength   float64 // 0.0 - 1.0
    Source     string  // "contrarian:rsi_overbought" など
    Reason     string  // 既存 signal_reason と同等
    Indicators IndicatorSet
}

type MarketSignalEvent struct {
    Signal     MarketSignal
    Price      float64
    CurrentATR float64
    Timestamp  int64
}

func (e MarketSignalEvent) EventType() string     { return EventTypeMarketSignal }
func (e MarketSignalEvent) EventTimestamp() int64 { return e.Timestamp }
```

```go
// backend/internal/domain/entity/decision.go に追記

type DecisionIntent string

const (
    IntentNewEntry        DecisionIntent = "NEW_ENTRY"
    IntentExitCandidate   DecisionIntent = "EXIT_CANDIDATE"
    IntentHold            DecisionIntent = "HOLD"
    IntentCooldownBlocked DecisionIntent = "COOLDOWN_BLOCKED"
)

type ActionDecision struct {
    SymbolID  int64
    Intent    DecisionIntent
    Side      OrderSide // BUY | SELL (Intent=HOLD/COOLDOWN_BLOCKED の時は空文字)
    Reason    string
    Source    string    // 由来の signal source を継承
    Strength  float64   // 由来の signal strength を継承 (sizer が参照)
}

type ActionDecisionEvent struct {
    Decision   ActionDecision
    Price      float64
    CurrentATR float64
    Timestamp  int64
}
```

### 5.2 EventType 追加

```go
// backend/internal/domain/entity/backtest_event.go

const (
    // ... 既存
    EventTypeMarketSignal = "market_signal"
    EventTypeDecision     = "decision"
)
```

### 5.3 既存 `Signal` 型の扱い

既存 `entity.Signal{Action, Confidence, Reason}` は **削除しない**。SignalEvent → MarketSignalEvent への移行 PR で並行存続させ、Phase 5 で deprecated コメントを付ける。完全削除は Phase 6+ の cleanup PR で。Phase 3 でルート (EventBus 配線) は新ルート一本化するが、型定義自体はしばらく残す。

### 5.4 RiskManager の拡張

```go
// backend/internal/usecase/risk.go

type RiskManager struct {
    // ... 既存
    entryCooldownUntil time.Time // 新規。close 約定後に伸ばす
}

// 新メソッド
func (rm *RiskManager) IsEntryCooldown(now time.Time) bool {
    rm.mu.RLock()
    defer rm.mu.RUnlock()
    return now.Before(rm.entryCooldownUntil)
}

func (rm *RiskManager) NoteClose(now time.Time) {
    rm.mu.Lock()
    defer rm.mu.Unlock()
    cooldown := time.Duration(rm.config.EntryCooldownSec) * time.Second
    rm.entryCooldownUntil = now.Add(cooldown)
}
```

### 5.5 RiskConfig の拡張

```go
type RiskConfig struct {
    // ... 既存
    EntryCooldownSec int // 0 → cooldown 無効、既定 60
    MaxSlippageBps   int // 既存だがプロファイル未設定なら 0
    MaxBookSidePct   int // 既存だがプロファイル未設定なら 0
}
```

### 5.6 decision_log スキーマ追加 (互換維持)

```sql
ALTER TABLE decision_log ADD COLUMN signal_direction    TEXT NOT NULL DEFAULT '';
ALTER TABLE decision_log ADD COLUMN signal_strength     REAL NOT NULL DEFAULT 0;
ALTER TABLE decision_log ADD COLUMN decision_intent     TEXT NOT NULL DEFAULT '';
ALTER TABLE decision_log ADD COLUMN decision_side       TEXT NOT NULL DEFAULT '';
ALTER TABLE decision_log ADD COLUMN decision_reason     TEXT NOT NULL DEFAULT '';
ALTER TABLE decision_log ADD COLUMN exit_policy_outcome TEXT NOT NULL DEFAULT '';
-- backtest_decision_log にも同じ ALTER を当てる
```

旧カラム (`signal_action`, `signal_confidence`, `signal_reason`) は引き続き埋める。新ロジックでは：
- `signal_action` ← `decision_side` (Intent=HOLD/COOLDOWN_BLOCKED の時は "HOLD")
- `signal_confidence` ← `signal_strength`
- `signal_reason` ← `decision_reason` で上書きせず、由来の `signal.Reason` を維持

---

## 6. 移行計画 (Stacked PR)

各 PR は独立してレビュー可能・main にマージ可能な粒度を保つ。間で本番運用ゲートは挟まない (ユーザ方針 = B)。

### PR1: entity / event 型の追加 (互換維持)

- `MarketSignal`, `MarketSignalEvent`, `ActionDecision`, `ActionDecisionEvent` を新規追加
- `EventTypeMarketSignal`, `EventTypeDecision` を定数追加
- 既存 `Signal` / `SignalEvent` は touch しない
- decision_log への ALTER TABLE migration を追加 (新カラムは空文字 / 0)
- recorder は新カラムを書き込めるようにフィールド追加 (Phase 1 中は空のまま)
- **動作不変**: 既存 backtest / live は何も変わらない
- テスト: 新型の単純な構築テストだけ

### PR2: Decision レイヤ新設 + StrategyHandler 改修

- `DecisionHandler` を新規追加 (`backend/internal/usecase/decision/handler.go`)
- `StrategyHandler` を改修：従来の `Signal{Action}` を出すパスに加えて、`MarketSignal{Direction, Strength}` も並列で publish できるようにする
- backtest / live の runner で：
  - `EventTypeIndicator → strategyHandler` (旧)
  - `EventTypeMarketSignal → decisionHandler` (新)
  - `EventTypeDecision → riskHandler` を新ルートとして配線
  - 既存 `EventTypeSignal → riskHandler` ルートも当面残す (後段の PR で削除)
- **動作不変**: 旧ルート (Signal 経由) が引き続き有効。新ルートは shadow 動作 (decision_log の新カラムに記録するだけ) で並走
- recorder が `MarketSignalEvent` / `ActionDecisionEvent` を購読し、新カラムを埋め始める
- テスト: DecisionHandler の単体テスト (保有状況 × Direction × cooldown のマトリクス)

### PR3: RiskHandler の Decision 化 + cooldown 実装

- `RiskHandler` を `ActionDecisionEvent` を入力とするように改修
- `RiskManager` に `EntryCooldownSec`, `IsEntryCooldown`, `NoteClose` を追加
- `OrderExecutor.ClosePosition` 約定後に `RiskManager.NoteClose()` を呼ぶ配線
- 既存ルート (`EventTypeSignal → riskHandler`) を削除し、新ルートに一本化
- **動作変更**: ここで意味論が切り替わる。両建て総額判定バグが解消される
- テスト:
  - 旧バグ再現テスト (ロング保有中の BEARISH → 旧コードで REJECTED、新コードで EXIT_CANDIDATE → HOLD)
  - cooldown 動作テスト (close → N 秒以内の new entry が COOLDOWN_BLOCKED になる)

### PR4: BookGate 有効化 + テスト拡充

- `production_ltc_60k.json` に `MaxSlippageBps`, `MaxBookSidePct` を追加 (初期値: bps=15, sidepct=20)
- `production.json` (= production_ltc_60k 系列) の同設定値を揃える
- live (event_pipeline.go) と backtest (runner.go) で BookGate が同じ条件で有効化されることを確認するテストを追加
- BookGate のエッジケーステスト追加 (snapshot stale, snapshot empty, top-N 不足)
- **動作変更**: 板厚不足時に BookGate で REJECTED され始める
- テスト: 既存の `book_limit_test.go` を拡充

### PR5: UI / 旧型 deprecate / cleanup

- frontend の history タブで `decision_intent` / `signal_direction` を表示
- "REJECTED (両建て総額)" だった行が "HOLD (保有中)" or "COOLDOWN_BLOCKED" に変わるはずなので UI 文言調整
- `entity.Signal{Action}` の使用箇所を `MarketSignal` / `ActionDecision` に置換、deprecated コメント追加
- 完全削除はせず Phase 6+ で別途 (互換維持目的)
- ドキュメント更新: AGENTS.md, docs/clean-architecture.md, docs/decision-log-health-check.md

---

## 7. テスト戦略

### 7.1 単体テスト

- **Strategy** (Direction 評価): CONTRARIAN/TREND_FOLLOW/BREAKOUT/HOLD ごとに、IndicatorSet → Direction/Strength のマトリクス
- **Decision**: (保有状況 × Direction × cooldown) の全組合せ → Intent/Side
- **RiskHandler**: ActionDecision → Approved/Rejected の境界条件 (残高、ポジション枠、cooldown)
- **BookGate**: snapshot fresh/stale/missing × top-N 充足/不足 × 自分のサイズが top-N の M%

### 7.2 統合テスト

- backtest runner が新 EventBus 配線で同じ candle を流して、`decision_log` の主要カラムが期待通り埋まる
- 過去の代表的バックテスト期間 (3m / 6m / 1y / 2y) で、Phase 1 適用前後のパフォーマンス指標 (Return, MaxDD, Sharpe) が大きく乖離しないこと
  - 「乖離しない」のしきい値: Return が ±2pp 以内、MaxDD が ±2pp 以内
  - 乖離した場合は decision_log を比較し、どの bar で挙動が変わったか分析
- 旧バグの再現テスト: 「ロング保有中の BEARISH」を Phase 1 適用前は REJECTED、適用後は HOLD として記録する

### 7.3 live と backtest の整合性

- 同じ profile (`production_ltc_60k.json`) を読ませた時、両 runner で BookGate / cooldown 設定が同じ値で有効化されることをテスト
- 既存の `runner_test.go` / `handler_test.go` を Phase 4 まで段階的に新型に追従させる

---

## 8. 互換性・運用影響

### 8.1 decision_log

- スキーマ変更は ADD COLUMN のみ。既存行は新カラムが空 / 0 で残る
- 旧カラム (`signal_action` 等) は新ロジックでも埋め続ける → 既存 UI / SQL クエリは動く
- 新カラムの利用は段階的：Phase 2 で recorder が埋め始め、Phase 5 で UI が表示

### 8.2 backtest_decision_log

decision_log と同じ ALTER。既存の比較レポート (PDCA) は旧カラムベースで動き続ける。新カラムは Phase 2 以降に新たな PDCA 軸として活用。

### 8.3 UI / API

- `/decisions` API は新カラムを response に追加 (Phase 2)
- 既存フロントは新カラムを無視して旧フォーマットで表示 (Phase 5 まで)
- Phase 5 でフロント拡張、追加カラムを画面表示

### 8.4 プロファイル JSON

- `production_ltc_60k.json` (および `production.json`) に追加：
  ```json
  {
    "risk": {
      "entry_cooldown_sec": 60,
      "max_slippage_bps": 15,
      "max_book_side_pct": 20
    }
  }
  ```
- 既存プロファイルでこれらが未設定なら 0 として扱う (= cooldown 無効、BookGate 無効)
- 後方互換のため、旧プロファイルは何も変更しなくても動く

### 8.5 Bot 運用

- Phase 1〜3 は shadow 動作 (新ルートが decision_log に記録するだけ)、本番挙動に影響なし
- Phase 3 マージ後に本番挙動が切り替わる。LTC 運用は事前に flat にしてから Phase 3 をマージする運用とする
- Phase 4 マージ後、BookGate が動き始める → 板薄時間帯に rejected が増える可能性。decision_log を監視

---

## 9. ロールアウト・ロールバック

### 9.1 各 PR のマージ手順

1. PR を main にマージ
2. `docker compose up --build -d` で本番コンテナ更新
3. `/api/v1/status` で `pipelineRunning=true` を確認
4. 30 分は decision_log を監視 (異常な REJECTED / panic / NULL カラム)

### 9.2 ロールバック

- DB schema: ADD COLUMN は revert しない (空のまま残す。データは消えない)
- コード: `git revert <PR>` で前バージョンに戻す。各 PR は独立しているので可能
- Phase 3 をロールバックしたい場合は Phase 4 もまとめて revert (依存関係)

### 9.3 緊急停止

- 既存の `/api/v1/stop` でいつでも bot 停止可能
- `manuallyStopped=true` 中は新規エントリーが完全に止まる (新ロジックでも RiskHandler の最初に `manualStop` チェックを残す)

---

## 10. 未解決事項 / 後続課題

### 10.1 後続で取り組む

- **Exit Policy の拡張**: BEARISH signal が連続したらロングの利確を早めるロジック (Phase 6+)
- **指値 close API**: 成行依存からの脱却 (市場インパクト軽減)
- **/positions と RiskManager の整合性問題**: in-memory 状態と exchange-side 状態のずれ調査 (MEMORY の "/status running は嘘をつく" 系列)

### 10.2 PDCA で sweep する新変数

- `entry_cooldown_sec`: 30 / 60 / 120 / 300 で比較。LTC 板回復時間を実測
- `max_slippage_bps`: 10 / 15 / 20 / 30 で reject 率と net pnl を測定
- `max_book_side_pct`: 10 / 20 / 30 で同上

### 10.3 構造の自然な拡張余地

C のレイヤ分離が定着すれば、以下が同じ構造に乗せられる：

- 複数 stance の同時評価 (一票方式 / 加重平均)
- 複数銘柄横断のポートフォリオ判定
- 機械学習モデルを Strategy or Decision に差し込み (Direction 出力を統合)

---

## 11. 参考

- 関連コード:
  - `backend/internal/usecase/risk.go:144-180` (バグの場所)
  - `backend/internal/usecase/booklimit/book_limit.go` (BookGate 実装)
  - `backend/cmd/event_pipeline.go:737-748` (BookGate live 配線、有効化条件)
  - `backend/internal/usecase/backtest/runner.go:276-` (EventBus priority 配線)
- 関連 Issue: #221 (BookGate disabled)
- 関連設計書: `docs/design/2026-04-05-auto-trading-system-design.md` (現状アーキテクチャ)
- ユーザ MEMORY:
  - `project_bookgate_disabled.md`
  - `project_status_running_lie.md`
