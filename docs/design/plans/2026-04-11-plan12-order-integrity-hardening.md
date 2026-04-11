# Plan 12: 注文整合性の堅牢化 (Order Integrity Hardening)

- **作成日**: 2026-04-11
- **改訂日**: 2026-04-11 (レビュー反映)
- **対象ブランチ**: 未定 (例: `feature/order-integrity-hardening`)
- **想定工数**: 4〜5 日 (Phase 0〜3 合計)
- **背景となった事故**: 2026-04-11 セッション。LTC_JPY を 0.1 LTC で発注したつもりが、Backend のレスポンスパース失敗により Backend が 500 を返したものの楽天側では約定済み。リトライにより同一 `clientOrderId` のはずが楽天側で 2 回約定し、意図せず 0.2 LTC のロングを保有する状態となった。

---

## 1. 問題の本質

現在の注文書き込みフロー (`OrderHandler.CreateOrder`, `PositionHandler.ClosePosition`) には、**「Backend が応答パースに失敗した瞬間に、楽天側で約定済みの注文が Backend から見えなくなる」** という根本的な脆弱性がある。

### 現在のフロー

```
1. Backend → 楽天 API: CreateOrder 送信
2. 楽天 API → Backend: 200 OK + JSON response
3. Backend: response を Unmarshal ← ここで失敗すると...
4. Backend → DB: client_orders に保存 ← 実行されず
5. Backend → クライアント: 500 error

  楽天側: 約定済み
  Backend DB: 痕跡なし
  クライアント: エラーを見て「失敗した」と誤認 → リトライ
  楽天側: 2 回目も約定 → 重複ポジション
```

### 何が悪いか

冪等性の鎖が `clientOrderId` で繋がっている設計になっているにもかかわらず、**鎖の最初の輪 (DB 保存) が一番最後にある**。楽天 API への送信より前に DB に痕跡を残していないため、楽天側との真実が乖離した瞬間にリカバリ手段を失う。

### 直近の応急処置 (2026-04-11 セッションで実施済み)

- `entity.Position` / `entity.Order` にカスタム `UnmarshalJSON` を追加し、`flexFloat` 型で string/number 両対応 → パース失敗の確率自体は下げた
- `POST /api/v1/positions/:id/close` を追加し、決済時の冪等性 (`clientOrderId`) を確保

これらは「パース失敗の発生確率を下げる」「決済経路でも冪等性を持たせる」改善ではあるが、**根本原因 (pre-flight 記録の不在) は未解決**。

---

## 2. ゴール

1. **書き込み系 API (CreateOrder, ClosePosition, CancelOrder) の冪等性を、楽天送信前の段階から成立させる**
2. **Backend が応答を見失っても、楽天側の真実と DB を後から再同期できる手段を持つ (reconcile)**
3. **オペレーター (人間 or エージェント) が「楽天では成功したのか？」を即座に判定できる UI/API**

### 成功基準

1. **再現テスト**: 楽天応答パースを意図的に失敗させた状態で発注 → DB に `submitted` レコードが残る → reconcile が走って `reconciled-confirmed` または `reconciled-not-found` に確定する
2. **重複防止**: `submitted` の `clientOrderId` で再度発注リクエスト → 冪等返却で楽天 API は呼ばれない
3. **並行安全**: 同一 `clientOrderId` の並行リクエストで楽天 API が 2 回呼ばれない (`ON CONFLICT` 保護)
4. **観測可能性**: `GET /api/v1/client-orders?status=submitted` で未確定注文が一覧できる
5. **ドキュメント**: `docs/agent-operation-guide.md` に「`status=submitted` を見たら絶対にリトライしない」が明記されている

---

## 3. アーキテクチャ案

### 3.1 Phase 0: マイグレーション基盤と OrderExecutor の下準備

本計画では **既存の monolithic な `RunMigrations()` 方式 (配列 + `IF NOT EXISTS`) を継続**し、新規カラムは `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` を 1 エントリずつ配列に追記する。マイグレーション番号ファイル方式は本計画のスコープ外とする。

また、後続 Step の共通基盤として `usecase.OrderExecutor` を **`clientOrderId` を受け取る形に拡張する** 事前リファクタを行う (詳細は Step 1.5)。

### 3.2 Phase 1: クライアントオーダーの状態機械化 (pre-flight 記録)

`client_orders` テーブルを単なる「成功記録」から **「注文ライフサイクルの監査ログ」** に格上げする。

#### スキーマ変更

既存 `client_orders` に対して `ALTER TABLE ADD COLUMN` を追加する。SQLite は `ADD COLUMN` で `DEFAULT` を指定できるので、既存行は全て `status='completed'` で保護される。

```sql
ALTER TABLE client_orders ADD COLUMN status TEXT NOT NULL DEFAULT 'completed';
ALTER TABLE client_orders ADD COLUMN symbol_id INTEGER;
ALTER TABLE client_orders ADD COLUMN intent TEXT;        -- "open" | "close" | "cancel"
ALTER TABLE client_orders ADD COLUMN side TEXT;          -- "BUY" | "SELL"
ALTER TABLE client_orders ADD COLUMN amount REAL;
ALTER TABLE client_orders ADD COLUMN position_id INTEGER;
ALTER TABLE client_orders ADD COLUMN raw_response TEXT;  -- 楽天応答の生 JSON
ALTER TABLE client_orders ADD COLUMN error_message TEXT;
ALTER TABLE client_orders ADD COLUMN updated_at INTEGER;

CREATE INDEX IF NOT EXISTS idx_client_orders_status
    ON client_orders(status, updated_at);
```

**運用上の注意**:
- `RunMigrations()` は全環境で冪等に動くよう、`ADD COLUMN` 失敗時 (既に存在) を許容するラッパーを導入する (SQLite には `ADD COLUMN IF NOT EXISTS` が無いため、`PRAGMA table_info` で事前確認する)
- 本番 DB に対する適用は「本番切替前にステージング DB で dry-run → 本番は Backend デプロイ前の 1 回のみ」という手順を Step 6 のドキュメントに明記

#### 状態機械

```
pending ──(send success, parse success)──→ confirmed ──(domain applied)──→ completed
   │
   ├──(client-side failure, pre-send error)──→ failed
   │
   ├──(HTTP/parse/timeout failure)──→ submitted ─┬─→ reconciled-confirmed
   │                                              ├─→ reconciled-not-found
   │                                              ├─→ reconciled-ambiguous
   │                                              └─→ reconciled-timeout (TTL 超過)
   │
   └──(crash before send)──→ pending (reconcile が拾う)
```

| status | 意味 | 楽天側の真実 | クライアント側の対処 |
|---|---|---|---|
| `pending` | Backend が DB に記録した直後。まだ楽天 HTTP を開始していない、または開始直後にプロセスクラッシュ | 未送信 or 不明 | 待機 (reconcile が判定) |
| `submitted` | 楽天 HTTP 送信を試みたが、応答のパース失敗・HTTP タイムアウト・ネットワークエラーで Backend が結果を確定できなかった | **不明** | **絶対にリトライしない** |
| `confirmed` | 応答パース成功、Backend が orderId を取得 | 受理済み | 完了扱い |
| `completed` | 約定処理まで完了し、Backend ドメインに反映 | 約定済み | 完了扱い |
| `failed` | **楽天が明示的に拒否したと判定できる失敗** (4xx かつパース成功) または送信前の client-side バリデーション失敗 | 未約定 | リトライ可 |
| `reconciled-confirmed` | reconcile ジョブが楽天 GetOrders と突合して受理を確定 | 受理済み | 完了扱い |
| `reconciled-not-found` | reconcile ジョブが TTL 経過後に楽天側に対応注文を発見できず確定 | 未約定 | リトライ可 |
| `reconciled-ambiguous` | 候補が複数ヒットし自動判定不能 | 不明 (人間判断) | 人間のオペレーターを待つ |
| `reconciled-timeout` | submitted のまま外部 TTL (24h) を超過し、reconcile もアンビギュアスのまま解決できなかった | 不明 | 人間判断 (手動オペレーションで確定) |

**重要**: `pending`/`submitted`/`failed` の判定は **「楽天側が受理した可能性があるか否か」** を軸に決める。下表が判定ルール:

| 発生事象 | 楽天側の真実 | 遷移先 |
|---|---|---|
| 送信前バリデーション失敗 (client-side) | 未送信 | `failed` |
| HTTP コネクション確立失敗 (DNS, TCP reset) | 未送信 | `failed` |
| HTTP 4xx + エラーボディのパース成功 | 楽天が明示的に拒否 | `failed` |
| HTTP 4xx + エラーボディのパース失敗 | **不明** (受理されて 4xx が誤返却の可能性) | `submitted` |
| HTTP 5xx | **不明** (内部で受理されている可能性) | `submitted` |
| HTTP 200 + レスポンスボディのパース失敗 | **不明** | `submitted` |
| HTTP タイムアウト (応答待ちで打ち切り) | **不明** | `submitted` |
| HTTP 200 + パース成功 | 受理済み | `confirmed` → `completed` |

この判定表は `usecase/order.go` の `ExecutionResult` 返却時に実装する。

#### 書き込みフロー (新)

```
1. Backend: clientOrderId を生成 (or クライアント指定)

2. Backend → DB: INSERT OR IGNORE (status='pending', intent, symbol_id, side, amount, position_id)
   │
   ├─ (a) 新規挿入成功 → 3 へ
   └─ (b) 既存行あり (ON CONFLICT) → 既存行を読み直して status に応じて返却:
          - pending/submitted → HTTP 202 (未確定)
          - confirmed/completed/reconciled-confirmed → HTTP 200 (冪等成功)
          - failed/reconciled-not-found → HTTP 409 (クライアントは新しい clientOrderId で再試行)
          - reconciled-ambiguous/reconciled-timeout → HTTP 409 (人間判断待ち)

3. Backend → 楽天 API: HTTP 送信開始

4a. 200 OK + パース成功:
    Backend → DB: UPDATE status='confirmed', order_id, raw_response
    Backend → クライアント: HTTP 200 (status='confirmed' or 'completed')

4b. 「楽天側の真実が不明」に該当 (判定表参照):
    Backend → DB: UPDATE status='submitted', raw_response, error_message
    Backend → クライアント: HTTP 202 Accepted (status='submitted')

4c. 「楽天側が受理していないと確定できる」に該当 (判定表参照):
    Backend → DB: UPDATE status='failed', error_message
    Backend → クライアント: HTTP 4xx/5xx (status='failed')
```

**並行レース対策**: Step 2 の冪等化は **`INSERT OR IGNORE INTO client_orders ...` + `affected_rows == 0` なら既存行を読み直す** 方式で統一する。現状の `Find → 判定 → Save` パターンは競合で両方が Find=null と判定して二重送信する脆弱性があるため、Step 1 の repository 層で全面的に置き換える。

**ポイント**: パース失敗は「Backend 内部エラー」ではなく「楽天応答が解釈できなかった = 楽天側の真実は不明」という状態として正直に表現する。クライアント (エージェント) は `status=submitted` を見たら **「リトライしてはいけない、reconcile を待て」** と判断できる。

### 3.3 Phase 2: Reconcile ジョブ

楽天 API の `GetOrders(symbolID)` を定期的に叩いて、`status IN ('pending', 'submitted')` の `client_orders` と突合する。

#### バッチ取得方式 (N+1 回避)

pending 件数分 `GetOrders` を呼ぶのではなく、**`symbol_id` ごとに 1 回だけ `GetOrders` を呼び、メモリ上で照合する**。

```go
// usecase/reconcile.go (擬似コード)
func (r *Reconciler) ReconcileOnce(ctx context.Context) error {
    pendings, err := r.repo.ListByStatus(ctx,
        []ClientOrderStatus{StatusPending, StatusSubmitted}, 500)
    if err != nil { return err }

    // symbol_id でグルーピングし、symbol 1 個につき GetOrders 1 回
    bySymbol := groupBySymbolID(pendings)
    for symbolID, cos := range bySymbol {
        rakutenOrders, err := r.client.GetOrders(ctx, symbolID)
        if err != nil {
            r.log.Warn("reconcile: GetOrders failed", "symbol", symbolID, "err", err)
            continue
        }
        for _, co := range cos {
            r.matchAndUpdate(ctx, co, rakutenOrders)
        }
    }
    return nil
}
```

#### 照合ロジック

楽天 API には `client_order_id` 検索が存在しないため、**時刻 + 属性の複合マッチング** を行う。

**マッチングキーと許容誤差**:
- `symbol_id` 完全一致
- `side` 完全一致
- `amount`: `abs(a - b) < 1e-9` の epsilon 比較 (float 誤差対策)
- `position_id`: `intent='close'` の場合のみ、楽天 order の `closePositionId` と一致を要求 (楽天 API が返すなら)
- 時刻ウィンドウ: 楽天の `orderCreatedAt` (秒精度の Unix 時刻) と Backend の `updated_at` の差が **±60 秒以内**
  - ネットワーク遅延 (最大数秒) + クロックスキュー (NTP 同期済み前提で数秒) + 楽天側処理遅延 を見積もった保守的な値

**マッチング結果の判定**:

| 候補数 | 条件 | 遷移先 |
|---|---|---|
| 1 | 時刻ウィンドウ内にユニーク一致 | `reconciled-confirmed` (楽天 orderId をピン留め) |
| 0 | `updated_at` から **30 分経過** | `reconciled-not-found` |
| 0 | 30 分未満 | 変更なし (次回再試行) |
| 2+ | 複数候補 | `reconciled-ambiguous` (人間判断待ち) |
| - | `updated_at` から **24 時間経過**、かつ reconcile で確定できていない | `reconciled-timeout` (最終状態、人間のオペレーション前提) |

**TTL の一本化**: `not-found` 判定は 30 分、`timeout` 判定は 24 時間。**30 分は「楽天側で受理されていればさすがに GetOrders に見えているはず」というビジネス判断、24 時間は「運用者が気づいて手動対応できるはずの上限」**。この 2 つは役割が異なるため両方残す。

**orderId のピン留め**: 一度 `reconciled-confirmed` になったら、以降の reconcile ジョブは **既にピン留めされた orderId を再照合しない**。`client_orders.order_id IS NOT NULL AND status='reconciled-confirmed'` は最終状態として扱う。

#### スケジューリング

- Backend 起動時 1 回 (クラッシュリカバリ)
- 60 秒間隔の定期実行
- `POST /api/v1/reconcile` 手動トリガ
- `POST /api/v1/client-orders/:id/reconcile` 個別実行

### 3.4 Phase 3: 監査・運用 UI

#### REST API 追加

| Method | Path | 用途 |
|---|---|---|
| GET | `/api/v1/client-orders` | 全件一覧 (`status`, `intent` でフィルタ) |
| GET | `/api/v1/client-orders/:id` | 単一参照 |
| POST | `/api/v1/client-orders/:id/reconcile` | 個別 reconcile 強制実行 |
| POST | `/api/v1/reconcile` | 全 pending/submitted を reconcile |

#### Frontend ダッシュボード

「注文監査」タブを追加し、`submitted` / `failed` / `reconciled-*` 状態を可視化。エージェント運用時に「未確定注文があるか」を一目で判断できるようにする。

- 状態に応じた色分け (submitted=黄、reconciled-confirmed=緑、reconciled-not-found=灰、reconciled-ambiguous=赤)
- `submitted` 件数バッジをヘッダーに表示
- 行クリックで raw_response と error_message を展開

#### HTTP 202 の互換移行

現状クライアント (`frontend/src/lib/api.ts`) は 2xx を一律成功として扱っているはず。202 導入は Backend 先行で入れると Frontend が `submitted` を成功と誤認するため、**Backend と Frontend を同一 PR で出す**。移行段階:

1. **Step 2 の PR**: Backend が 200/202 を返し分けるようにする。**同じ PR に Frontend 側のレスポンス型へ `status` フィールドを追加**し、`status==='submitted'` のときは成功トーストではなく「未確定 (reconcile 待ち)」バナーを表示する。
2. **クライアント互換**: レスポンス JSON の既存フィールド (`executed`, `orderId`) は維持。`status='confirmed'|'completed'` のとき `executed=true`、それ以外は `executed=false`。これで古い Frontend をうっかりデプロイしても「成功とは誤認しない」(executed=false 扱い)。

---

## 4. ステップ別 To-Do

### Step 0: マイグレーション基盤の微調整 (0.5 日)
- [ ] `RunMigrations()` に `ADD COLUMN` を安全に流すヘルパーを追加 (`PRAGMA table_info` で事前確認)
- [ ] 既存 DB に対する dry-run 手順をドキュメント化

### Step 1: ドメインモデルとスキーマ (1 日)
- [ ] `ClientOrderStatus` 型を `entity` に追加 (`pending`, `submitted`, `confirmed`, `completed`, `failed`, `reconciled-confirmed`, `reconciled-not-found`, `reconciled-ambiguous`, `reconciled-timeout`)
- [ ] `ClientOrderRecord` を拡張 (status, symbolID, intent, side, amount, positionID, rawResponse, errorMessage, updatedAt)
- [ ] `ClientOrderRepository` インターフェース更新:
  - [ ] `InsertOrGet(ctx, record) (existing *Record, inserted bool, err error)` — `INSERT OR IGNORE` + 既存行読み直し
  - [ ] `UpdateStatus(ctx, id, status, fields...)` — 状態遷移 + 追加フィールド更新
  - [ ] `ListByStatus(ctx, statuses, limit)` — reconcile 用
- [ ] `ALTER TABLE` 追加 (Step 0 のヘルパー経由)、`idx_client_orders_status` インデックス作成
- [ ] `database/client_order_repo.go` の実装更新
- [ ] テスト: `client_order_repo_test.go` を新規作成 (**現状テスト無し**)。各遷移、`InsertOrGet` の並行レース、既存行の status 別返却を検証

### Step 1.5: OrderExecutor の clientOrderId-aware 化 (0.5 日)
- [ ] `OrderExecutor.ExecuteSignal` / `ClosePosition` のシグネチャに `clientOrderID string` を追加
- [ ] `ExecutionResult` に `RawResponse []byte`, `FailureKind` (PreSend/Rejected/Ambiguous) を追加
- [ ] `rakuten.Client.CreateOrder` など低レイヤを **`(rawBytes []byte, parsed *entity.Order, err error)`** のように raw bytes を常に返すよう変更 (パース失敗時でも raw は取れるように)
- [ ] 既存呼び出し元 (handler, pipeline) をシグネチャ変更に追従。本 Step は振る舞い変更なしで型変更のみ
- [ ] テスト: `rakuten` クライアント層で raw bytes が返ることを確認

### Step 2: 注文ハンドラと pipeline の pre-flight 化 + Frontend 対応 (1.5 日)
- [ ] `OrderHandler.CreateOrder`:
  - [ ] `InsertOrGet` で pending 登録 → 既存行があれば status 別に 200/202/409 返却
  - [ ] 楽天送信 → 判定表に従って `confirmed`/`submitted`/`failed` に更新
  - [ ] `submitted` のとき HTTP 202、それ以外は従来通り
- [ ] `PositionHandler.ClosePosition`: 同様
- [ ] `cmd/pipeline.go` の `executeSignal` / 損切クローズも同じフローを通す (Step 1.5 で共通化済み)
- [ ] パイプラインでの `clientOrderId` 採番ルール統一 (`agent-*` 命名と整合)
- [ ] レスポンス型に `status` フィールド追加。既存 `executed` フィールドは `status in ('confirmed','completed','reconciled-confirmed')` のとき `true`
- [ ] **Frontend**: `lib/api.ts` のレスポンス型に `status` 追加、`submitted` を専用バナー表示、`frontend/src/routes/*` で成功判定ロジックを `status` 主体に変更
- [ ] テスト:
  - [ ] ハンドラ単体テストで pending→submitted→confirmed 経路
  - [ ] パース失敗時の submitted 残留
  - [ ] 並行リクエスト (同一 clientOrderId) で楽天 API が 1 回しか呼ばれないこと
  - [ ] パイプラインからの発注 → パース失敗 → submitted 残留 の E2E

### Step 3: Reconcile ジョブ (1 日)
- [ ] `usecase/reconcile.go` を新規作成: `Reconciler` 構造体、`ReconcileOnce(ctx)` メソッド
- [ ] バッチ取得: `symbol_id` グルーピング → `GetOrders` 1 回 / symbol
- [ ] 照合ロジック: epsilon 比較、±60 秒時刻ウィンドウ、`intent='close'` なら position_id も要求
- [ ] 判定: 1 件一致 → `reconciled-confirmed`、0 件 + 30 分経過 → `reconciled-not-found`、複数 → `reconciled-ambiguous`、24 時間経過 → `reconciled-timeout`
- [ ] `reconciled-confirmed` 以降は再照合しない (orderId ピン留め)
- [ ] `cmd/main.go` で `startReconciler(ctx)` を起動 (60 秒間隔 ticker + 起動時 1 回)
- [ ] テスト: モック OrderClient で各判定分岐を検証 (unique, zero-young, zero-old, ambiguous, timeout)

### Step 4: 監査 API + Frontend 監査ビュー (0.5〜1 日)
- [ ] `handler/client_order.go` 新規: List, Get, Reconcile (single/all)
- [ ] `router.go` に登録
- [ ] OpenAPI/型定義の更新 (Frontend 用)
- [ ] Frontend に「Orders」タブ or `OrdersAudit.tsx` コンポーネント追加
- [ ] 状態に応じた色分け、`submitted` 件数バッジ

### Step 5: ドキュメント・運用ガイド (0.5 日)
- [ ] `docs/agent-operation-guide.md` の更新: `submitted` 状態の意味とエージェント側の対処 (「リトライ禁止、reconcile を待て」)
- [ ] 新エンドポイントのドキュメント
- [ ] 失敗ケース別の判断フローチャート
- [ ] 本番 DB マイグレーション適用手順

---

## 5. リスクと検討事項

### 5.1 楽天 API 側に client_order_id 検索が無い問題

reconcile の突合は時刻 + 属性ベースの heuristic マッチングに頼らざるを得ない。アンビギュアスケース (同時刻に同 amount の注文が複数) では確定できず、人間の判断が必要。

**対策**: アンビギュアス時は `reconciled-ambiguous` で停止し、運用 UI で「これはどの注文に対応するか」を手動で紐付けられるようにする。`intent='close'` の場合は `position_id` で一意に絞り込める可能性が高いので、`closePositionId` を楽天レスポンスから取得できるなら優先的に使う。

### 5.2 submitted ステータスの TTL と timeout の関係

**30 分 / 24 時間の使い分け**:
- **30 分 (`reconciled-not-found`)**: 楽天 GetOrders に現れないなら「そもそも楽天側で受理されていない」と判定する閾値。30 分は「ネットワーク障害や楽天側の内部遅延でも、受理されていればこの時間内に GetOrders に現れる」というビジネス判断
- **24 時間 (`reconciled-timeout`)**: `reconciled-ambiguous` のまま解決されない、あるいは何らかの理由で状態が動かないレコードの最終救済。ここに来たら人間のオペレーターが手動で status を書き換えて解決する

### 5.3 パース失敗の根本対策との関係

Phase 0 (`flexFloat`) で確率は下げているが、楽天 API の仕様変更や未知のフィールドで再発する可能性は残る。Reconcile はその「最後の防壁」。

**追加対策**: パース失敗時に raw response を `client_orders.raw_response` に保存しておけば、後から手動で解析・修正可能。

### 5.4 既存パイプラインとの互換性

`cmd/pipeline.go` の発注ロジックは現状 `OrderExecutor.ExecuteSignal` を直接呼んでおり、`clientOrderId` を内部で採番していない (usecase に冪等性の概念がない)。Step 1.5 でシグネチャを変更し、Step 2 で呼び出し元を一斉に追従させる。

### 5.5 並行レース対策

現状の `Find → Save` パターンは、同一 `clientOrderId` の並行リクエストで両方が Find=null と判定し、二重送信する脆弱性がある。**Step 1 で `INSERT OR IGNORE` ベースの `InsertOrGet` に全面的に置き換える**。既存の `Find` / `Save` は deprecated にし、Step 2 完了後に削除。

### 5.6 Frontend 202 互換性

現状クライアントは 2xx を成功扱いしている。Backend を先行リリースして Frontend が `submitted` を成功と誤認するのを避けるため、**Step 2 で Backend と Frontend を同一 PR に入れる**。レスポンス JSON の既存 `executed` フィールドも維持し、古い Frontend を誤ってデプロイしても `executed=false` として「未成功」と扱えるようにする。

### 5.7 実装規模

- Step 0+1+1.5: 2 日
- Step 2: 1.5 日
- Step 3: 1 日
- Step 4+5: 1〜1.5 日
- **合計: 4〜5 日** (初版の 3〜4 日から上方修正)

### 5.8 段階的リリース

- **PR#1**: Step 0 + Step 1 + Step 1.5 (スキーマ + repository + OrderExecutor 型変更のみ。挙動変更なし)
- **PR#2**: Step 2 (Backend + Frontend の pre-flight 化。**必ず同一 PR**)
- **PR#3**: Step 3 (Reconcile ジョブ)
- **PR#4**: Step 4 + Step 5 (監査 UI + ドキュメント)

PR#1 は振る舞い変更なしなので安全に merge できる。PR#2 で初めて挙動が変わるが、この時点で冪等性と pre-flight 記録が揃う。PR#3 で真の自己修復が成立する。

---

## 6. 関連ファイル (修正対象)

### 新規作成
- `backend/internal/usecase/reconcile.go`
- `backend/internal/usecase/reconcile_test.go`
- `backend/internal/infrastructure/database/client_order_repo_test.go` (現状無し)
- `backend/internal/interfaces/api/handler/client_order.go`
- `backend/internal/interfaces/api/handler/client_order_test.go`
- `frontend/src/components/OrdersAudit.tsx` (or 同等)
- `frontend/src/routes/orders.tsx` (新タブの場合)

### 修正
- `backend/internal/domain/entity/client_order.go` (新規 or 既存 entity 拡張)
- `backend/internal/domain/repository/client_order.go`
- `backend/internal/infrastructure/database/client_order_repo.go`
- `backend/internal/infrastructure/database/migrations.go` (`ADD COLUMN` ヘルパー + ALTER 追加)
- `backend/internal/infrastructure/rakuten/private_api.go` (raw bytes を返す)
- `backend/internal/interfaces/api/handler/order.go`
- `backend/internal/interfaces/api/handler/position.go`
- `backend/internal/interfaces/api/router.go`
- `backend/internal/usecase/order.go` (`clientOrderID` 引数 + `RawResponse`/`FailureKind`)
- `backend/cmd/main.go` (Reconciler 起動)
- `backend/cmd/pipeline.go` (pre-flight 化)
- `frontend/src/lib/api.ts` (レスポンス型に `status` 追加、202 ハンドリング)
- `docs/agent-operation-guide.md`

---

## 7. 進め方

PR#1 (Step 0+1+1.5) から着手。ブランチ命名案: `feature/order-integrity-hardening-step1`。

実装着手前に以下を確認：
- [ ] このプランをユーザーがレビュー・承認
- [ ] 既存の `client_orders` テーブルに本番データ (取引履歴) があるか確認 → マイグレーション dry-run
- [ ] 楽天 API の `closePositionId` がレスポンスに含まれるか実機確認 (reconcile の intent=close 照合強度に影響)

---

## 8. 改訂履歴

### 2026-04-11 初版レビュー反映
- レビュー指摘 (High) への対応:
  - **状態定義の整合性**: `pending` = DB 記録済み・楽天 HTTP 未開始、`submitted` = HTTP 送信試行済みだが真実不明、に再定義。書き込みフローも一致させた
  - **エラー判定の一本化**: 「楽天が受理した可能性があるか否か」を軸にした判定表 (3.2 節) を追加し、`submitted` と `failed` の分岐条件を明示
  - **並行レース対策**: `Find → Save` を `InsertOrGet` (`INSERT OR IGNORE` ベース) に統一 (5.5 節、Step 1)
  - **Reconcile TTL の一本化**: 30 分 (`not-found`) / 24 時間 (`timeout`) の役割を明文化 (3.3 節、5.2 節)
- レビュー指摘 (Medium) への対応:
  - **照合強度**: epsilon 比較、±60 秒ウィンドウ、`intent='close'` での `position_id` 追加キーを明記 (3.3 節)
  - **N+1 回避**: `symbol_id` でグルーピングして GetOrders を 1 回/symbol に (3.3 節)
  - **マイグレーション方式**: 既存 `RunMigrations()` 配列方式を維持、`ADD COLUMN` ヘルパー経由で安全適用 (3.1 節、Step 0)
  - **202 互換移行**: Backend + Frontend を同一 PR、`executed` フィールドは維持 (3.4 節、5.6 節)
- その他 (自主レビュー):
  - Step 1.5 (`OrderExecutor` の clientOrderId-aware 化) を独立
  - raw bytes を rakuten クライアント層から返す設計を明記 (Step 1.5)
  - `client_order_repo_test.go` が現状無いことを明記し、新規作成を明示 (Step 1)
  - 工数見積りを 3〜4 日 → 4〜5 日に上方修正
