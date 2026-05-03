# PR5 Plan: UI 表示 + ドキュメント更新 + cleanup（Phase 1 仕上げ）

- 作成日: 2026-05-02
- 親設計書: `docs/design/2026-04-29-signal-decision-policy-separation-design.md`
- 前段 PR: #232 / #233 / #234 / #235（PR1〜4 全マージ済み）
- スコープ: Phase 1 / Stacked PR シリーズ (PR5 ÷ 5 — 最終)
- **動作変更なし** — UI 拡張、ドキュメント更新、deprecate 表記の追加のみ

---

## 0. このドキュメントの位置付け

PR4 までで Phase 1 の機能要件と動作変更はすべて入った。PR5 は **観察と保守の仕上げ**：

- フロントエンドが新カラム (`signal_direction`, `decision_intent`, `decision_side`, `decision_reason`, `book_gate_outcome` の表示) を見えるようにする
- "REJECTED (両建て総額)" だった行が "EXIT_CANDIDATE" / "COOLDOWN_BLOCKED" / "VETOED" に変わったので、UI 文言も整える
- `entity.Signal{Action}` 経路の使用を deprecated 表記する（完全削除は Phase 6+）
- AGENTS.md / docs/clean-architecture.md / docs/decision-log-health-check.md を Phase 1 後の現実に追従させる

PR5 マージで Phase 1 は完了。Phase 6+（Exit Policy 拡張、指値 close、複数銘柄）は別ロードマップ。

---

## 1. 現状調査（PR5 plan 策定時に確認した事実）

### 1.1 frontend のデータ型と画面

- `frontend/src/lib/api.ts` の `DecisionLogItem` 型は `signal: { action, confidence, reason }` / `risk` / `bookGate` / `order` のネスト構造（旧スキーマ）
- 新カラム (`signal_direction`, `signal_strength`, `decision_intent`, `decision_side`, `decision_reason`, `exit_policy_outcome`) は **API 応答にも frontend にも未着信**
- 表示先は `frontend/src/components/DecisionLogTable.tsx`（`/history?tab=decisions`）と `RecentDecisionsCard.tsx`（監視画面の直近判断カード）
- 現状の `signal.action` は "BUY" / "SELL" / "HOLD" のいずれか。新カラムが流れれば direction（BULLISH/BEARISH/NEUTRAL）と intent（NEW_ENTRY/EXIT_CANDIDATE/HOLD/COOLDOWN_BLOCKED）が表示できる

### 1.2 backend API レスポンスの形

`backend/internal/interfaces/api/handler/decision.go:105-144` の `decisionRecordToJSON` が `DecisionRecord` を JSON に変換しているが、PR1 で追加した 6 フィールドを **まだ含めていない**。これを足すのが PR5 の起点。

### 1.3 backtest の decision endpoint

`/api/v1/backtest/results/:id/decisions` も同じく旧フィールドのみ返している（同じヘルパーを使っているか別か要確認）。frontend `backtest-decisions.tsx` で表示。

### 1.4 docs の現状

- `AGENTS.md`: Stance / Indicators / Backtest の説明は最新だが、Signal/Decision/ExecutionPolicy 三層分離は未言及
- `docs/clean-architecture.md`: Trading Pipeline の構成が "60 秒 polling 旧 + EventDriven 新" の頃の説明
- `docs/decision-log-health-check.md`: 6.4 KB と充実、ただし新カラムの SQL 例は未記載
- `docs/agent-operation-guide.md`: Bot 操作手順、新カラムの存在は未記載（直接の影響は薄い）

### 1.5 廃止予定 (deprecated) すべき箇所

- `entity.Signal` 型（`SignalAction = "BUY"|"SELL"|"HOLD"`）。新ルートでは `entity.MarketSignal{Direction}` と `entity.ActionDecision{Intent, Side}` の組が真実だが、`entity.Signal` は `RiskHandler` 内部で synthSignal として使われ、`ApprovedSignalEvent.Signal` フィールド経由で `ExecutionHandler` まで届く。完全削除は Phase 6+ の cleanup PR (設計書 §5.3 通り)
- 設計書 §5.3 の方針通り、PR5 では **削除せず deprecated コメントだけ付ける**

---

## 2. ファイル変更マップ

| ファイル | 変更 | 行数目安 |
|---|---|---|
| `backend/internal/interfaces/api/handler/decision.go` | レスポンスに 6 新フィールド追加 | +20 |
| `backend/internal/interfaces/api/handler/decision_test.go` (もしあれば) | レスポンス検証 | +30 |
| `backend/internal/interfaces/api/handler/backtest.go` | backtest_decision_log JSON 化に 6 新フィールド追加 | +20 |
| `backend/internal/domain/entity/signal.go` | Signal 型に deprecated コメント | +5 |
| `frontend/src/lib/api.ts` | DecisionLogItem に 6 新フィールド | +10 |
| `frontend/src/components/DecisionLogTable.tsx` | 列追加（Direction / Intent / BookGate label 整備） | +60 |
| `frontend/src/components/DecisionDetailPanel.tsx` | 詳細パネルに 5 フィールド表示 | +30 |
| `frontend/src/components/RecentDecisionsCard.tsx` | 直近カードに intent 表示 | +20 |
| `frontend/src/lib/decisionReasonI18n.ts` | 新 outcome の翻訳追加 | +20 |
| `AGENTS.md` | 三層分離の追記、Phase 1 完了の文脈 | +15 |
| `docs/clean-architecture.md` | Trading Pipeline 説明を Decision レイヤ追加へ更新 | +20 |
| `docs/decision-log-health-check.md` | 新カラム存在チェック SQL 例 | +30 |

合計：新規 0、編集 12、約 +280 行（純増）。

---

## 3. 実装タスク

### Task 1: backend API レスポンスに 6 フィールド追加

**変更**: `backend/internal/interfaces/api/handler/decision.go` の `decisionRecordToJSON`

```go
return gin.H{
    // ... 既存
    "decision": gin.H{
        "intent":    r.DecisionIntent,    // NEW_ENTRY / EXIT_CANDIDATE / HOLD / COOLDOWN_BLOCKED / ""
        "side":      r.DecisionSide,      // BUY / SELL / ""
        "reason":    r.DecisionReason,
    },
    "marketSignal": gin.H{
        "direction": r.SignalDirection,   // BULLISH / BEARISH / NEUTRAL / ""
        "strength":  r.SignalStrength,
    },
    "exitPolicyOutcome": r.ExitPolicyOutcome, // PR4 で BookGate 経由の出口判断記録 (現状未使用)
    // ... 残り
}
```

ネスト構造を採用する理由：旧 `signal: {action, confidence, reason}` と並列に置くことで、frontend が「旧レコード（空フィールド）」と「新レコード」を区別せずに 1 つの型で扱える。

**テスト**: `decision_test.go` があれば `decisionRecordToJSON` の round-trip テストを追加。新カラム値を持った DecisionRecord を入れて、JSON 出力に正しく現れることを検証。

**完了判定**: `go test ./internal/interfaces/api/handler/... -run Decision` 緑。

---

### Task 2: backtest_decision_log の JSON 化も同期

`backend/internal/interfaces/api/handler/backtest.go` の `/backtest/results/:id/decisions` エンドポイント — Task 1 と同じヘルパーを共有しているか別実装か確認し、後者なら同じ追加を行う。

backtest 結果の history タブで PR2 以降の新カラムが見える状態を作る（既に backtest 経由で 36k+ 行の新カラム記録あり）。

---

### Task 3: frontend DecisionLogItem 型拡張

**変更**: `frontend/src/lib/api.ts`

```ts
export type DecisionLogItem = {
  // ... 既存
  signal: { action: 'BUY' | 'SELL' | 'HOLD'; confidence: number; reason: string }

  // PR5 (Phase 1): new shadow / decision-route columns. All fields are
  // optional with empty-string default so old rows (signal_direction = "")
  // pre-PR2 / post-PR3 mixed runs both render cleanly.
  marketSignal?: { direction: 'BULLISH' | 'BEARISH' | 'NEUTRAL' | ''; strength: number }
  decision?: {
    intent: 'NEW_ENTRY' | 'EXIT_CANDIDATE' | 'HOLD' | 'COOLDOWN_BLOCKED' | ''
    side: 'BUY' | 'SELL' | ''
    reason: string
  }
  exitPolicyOutcome?: string
  // ... 既存の risk / bookGate / order
}
```

`?` で optional にしているのは、まだ backend 改修前の API でも壊れず、新フィールドが空文字 / 0 で来てもクラッシュしないようにするため。

---

### Task 4: DecisionLogTable に列追加 + 文言調整

**変更**: `frontend/src/components/DecisionLogTable.tsx`

- 列追加: 「方向」(Direction) / 「判断」(Intent) を追加。スタンス列の右隣
- 「シグナル」列は旧 BUY/SELL/HOLD のままで残し、新カラム列との対比で「Phase 1 移行が一目で分かる」状態を維持
- BookGate label を `BookGate` から「板厚」(または「板ガード」) に変更し、`VETOED` → "拒否（板薄/スリッページ過大）" の文言を追加
- `rowBackground` に `intent === 'COOLDOWN_BLOCKED'` の薄グレー背景を追加

```tsx
const INTENT_LABEL: Record<NonNullable<DecisionLogItem['decision']>['intent'], string> = {
  NEW_ENTRY: '新規エントリー',
  EXIT_CANDIDATE: '利確/損切り候補',
  HOLD: '見送り',
  COOLDOWN_BLOCKED: 'クールダウン',
  '': '—',
}

const DIRECTION_LABEL: Record<NonNullable<DecisionLogItem['marketSignal']>['direction'], string> = {
  BULLISH: '買い優勢',
  BEARISH: '売り優勢',
  NEUTRAL: '中立',
  '': '—',
}
```

---

### Task 5: DecisionDetailPanel 拡張

詳細パネル（行クリック展開）に以下を 1 ブロックで表示：

- 市場シグナル: Direction / Strength / Source
- Decision: Intent / Side / Reason
- Exit Policy Outcome（あれば）

旧の Signal action / confidence / reason はそのまま残し、「旧経路（PR3 以前の出力）」とラベル付けして並列表示。Phase 6+ で旧経路が消えれば自然に空欄化する。

---

### Task 6: RecentDecisionsCard の Intent 表示

監視画面の直近判断カードは現在 `signal.action` を主役に表示している。新カラムが入っていれば Intent を表示し、空なら旧 action にフォールバック：

```tsx
const primaryLabel = item.decision?.intent
  ? INTENT_LABEL[item.decision.intent]
  : item.signal.action
```

---

### Task 7: decisionReasonI18n 拡張

新 reason のいくつかは英語のままなので翻訳マップを追加：

- `"no position; bullish signal -> new long"` → "保有なし、上昇シグナル → 新規ロング"
- `"long held; bearish signal -> exit candidate"` → "ロング保有中、下落シグナル → 利確候補"
- `"entry cooldown active after recent close"` → "直近の決済後クールダウン中"
- `"thin_book_pre_trade"` → "板薄（取引前）"
- `"depth_above_threshold"` → "板の自ロット占有率が上限超過"

---

### Task 8: AGENTS.md / docs 更新

**AGENTS.md** に追記（Architecture セクション）：

```md
- Signal / Decision / ExecutionPolicy: 三層分離 (Phase 1 完了 2026-05-02)。
  Strategy が市場解釈 (BULLISH/BEARISH/NEUTRAL + Strength) のみを返し、
  Decision レイヤがポジション保有状況と cooldown を加味して
  NEW_ENTRY / EXIT_CANDIDATE / HOLD / COOLDOWN_BLOCKED を出す。
  ExecutionPolicy (Risk + BookGate) で実発注に絞り込む。
  詳細: `docs/design/2026-04-29-signal-decision-policy-separation-design.md`
```

**docs/clean-architecture.md** に Decision レイヤの位置付けを追記。

**docs/decision-log-health-check.md** に新カラムの存在検証 SQL を追加：

```sql
-- Phase 1 PR1 マイグレーション後、新 6 カラムが書かれているか
SELECT
  signal_direction, decision_intent, decision_side,
  COUNT(*) AS rows
FROM decision_log
WHERE bar_close_at > strftime('%s','now','-24 hours')*1000
GROUP BY signal_direction, decision_intent, decision_side
ORDER BY rows DESC;
```

---

### Task 9: entity.Signal 型に deprecated コメント

```go
// Signal はStrategy Engineが生成する売買シグナル。
//
// Deprecated (Phase 1, 2026-05-02): Signal は旧ルートの 1st-class entity
// だったが、Phase 1 で MarketSignal (Direction + Strength) と ActionDecision
// (Intent + Side) の二層に分割された。Signal は現在 RiskHandler 内で
// synthSignal として組み立てられ ApprovedSignalEvent / RejectedSignalEvent
// の payload として残っているだけ。Phase 6+ で完全に置換予定。
type Signal struct { ... }
```

完全削除しないので touch する場所は最小（コメントのみ）。

---

### Task 10: 全パッケージ緑 + 動作確認

- `go test ./... -race -count=1` 緑
- `go vet ./...` 警告なし
- `cd frontend && pnpm test` 緑
- `cd frontend && pnpm tsc --noEmit` で TypeScript 型エラーなし
- Docker rebuild → frontend が新カラムを画面に表示することを目視確認
  - `/history?tab=decisions` で 「方向」「判断」列が出る
  - 行クリックで DecisionDetailPanel に Decision セクションが出る
  - 監視画面の直近判断カードに Intent が出る

---

## 4. テスト戦略

### 4.1 backend

- `decisionRecordToJSON` round-trip テスト（新フィールド込みで JSON が期待値）
- backtest path も同じ shape を返すことの確認（または共有ヘルパー化）

### 4.2 frontend

- `DecisionLogTable` のスナップショット or 簡易テスト（新列のレンダー）
- Intent ラベルが空文字でも crash しないこと

### 4.3 動作確認 (manual)

Docker で backend + frontend を立てて：

1. `/history?tab=decisions&symbol=LTC_JPY` でテーブルに 「方向」「判断」列が見える
2. 過去（PR2 以降の）行で `BULLISH/BEARISH/NEUTRAL` と `NEW_ENTRY/EXIT_CANDIDATE/HOLD` が表示される
3. 監視画面の直近判断カードで Intent ラベルが出る
4. 詳細パネル展開で 5 フィールドが見える

---

## 5. リスクと緩和

| リスク | 影響 | 緩和 |
|---|---|---|
| API レスポンス shape 変更で既存 frontend が break | 画面壊れる | 新フィールドは optional (`?`) で追加、旧キーは touch しない |
| backtest と live で API shape が違うことが固定化 | 保守性悪化 | Task 2 で同じヘルパー化 (or shape を揃える) |
| docs 更新漏れで Phase 1 後の運用がブレる | 運用混乱 | AGENTS.md / clean-architecture.md / decision-log-health-check.md の 3 箇所で必ず更新 |
| Signal 型の deprecated 表記で linter が騒ぐ | CI 失敗 | コメントのみで実コードは触らない、go vet レベルで問題なし |
| frontend の i18n マップ漏れ | 英語が残る | `translateReason` のフォールバックで raw 文字列が出る (既存挙動) |

---

## 6. PR 作成手順

1. ブランチ: `feat/phase1-ui-and-cleanup`
2. コミット粒度（5〜6 コミット）：
   - **Commit 1**: backend API レスポンスに 6 新フィールド追加 (+ plan)
   - **Commit 2**: frontend DecisionLogItem 型拡張 + i18n マップ
   - **Commit 3**: DecisionLogTable に列追加 + 文言調整
   - **Commit 4**: DecisionDetailPanel + RecentDecisionsCard 拡張
   - **Commit 5**: docs 更新 (AGENTS.md / clean-architecture.md / decision-log-health-check.md)
   - **Commit 6**: entity.Signal に deprecated コメント
3. PR 本文：「PR5 of 5 (Phase 1 最終)」、動作変更なし、UI で新カラム可視化、Phase 1 完了宣言
4. CI 緑で squash merge

---

## 7. 完了の定義（DoD）

- [ ] 10 タスクすべて完了
- [ ] backend tests / frontend tests 緑
- [ ] `go vet` / `tsc --noEmit` 警告なし
- [ ] Docker 再起動で frontend に新列が表示される（目視）
- [ ] 詳細パネルで Decision セクションが表示される
- [ ] 直近判断カードで Intent が表示される
- [ ] AGENTS.md / clean-architecture.md / decision-log-health-check.md が Phase 1 後の状態
- [ ] entity.Signal の Deprecated コメントが付いている
- [ ] PR 本文に「Phase 1 完了」を明記

---

## 8. Phase 1 完了後の引き継ぎ

PR5 マージで Phase 1 は完了。以後の課題（設計書 §10）：

### 後続で取り組む

- **Exit Policy 拡張**: BEARISH 連続でロング利確を早めるロジック (Phase 6+)
- **指値 close API**: 成行依存からの脱却 (市場インパクト軽減)
- **/positions と RiskManager の整合性問題**: in-memory 状態と exchange-side のずれ調査
- **EXIT_CANDIDATE の opt-in 発注 (`exit_on_signal` 設定)**: Phase 6+

### PDCA で sweep する新変数（PR4 マージ済み）

- `entry_cooldown_sec`: 30 / 60 / 120 / 300 で比較
- `max_slippage_bps`: 10 / 15 / 20 / 30 で reject 率と net pnl
- `max_book_side_pct`: 10 / 20 / 30 で同上

これらの sweep は PR5 とは独立した PDCA cycle として運用。

### 構造の自然な拡張余地

C のレイヤ分離が定着すれば、以下が同じ構造に乗る（設計書 §10.3）：

- 複数 stance の同時評価（一票方式 / 加重平均）
- 複数銘柄横断のポートフォリオ判定
- 機械学習モデルを Strategy or Decision に差し込み
