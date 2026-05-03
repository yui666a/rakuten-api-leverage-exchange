# ExitPlan を第一級概念に昇格する設計

- **作成日**: 2026-05-04
- **対象**: 楽天ウォレット証拠金取引所 Bot（個人開発）
- **関連設計**: `docs/design/2026-04-29-signal-decision-policy-separation-design.md`（Phase 1 完了の三層分離）
- **位置づけ**: 三層分離の続きとして、既存ポジションの出口管理を独立した第四レイヤに昇格する

---

## 1. 背景

### 1.1 きっかけとなった疑問

UI 上の「ポジション・手動取引」エリアに SL / TP / Trailing を表示できないか、というユーザの問いから発展した。表示そのものは API 拡張で実現できるが、調べてみると現状は次のような構造的問題が露呈する:

- SL / TP / Trailing の状態は `RiskManager` の **in-memory state** に閉じている (`stopLossDistance` 計算、`highWaterMarks` map)
- API レスポンス (`/api/v1/positions`) にも WebSocket にも出ていない
- bot 再起動で **HWM が消失**する。Trailing は再構築が必要
- 履歴に残らない。SL ライン引き上げの軌跡が観察できない
- ATR モードの SL は tick ごとの ATR 揺らぎで価格が動く（バグでなく仕様だが、明示されていない）
- 既存設計書では「Exit ロジック (TP/SL/Trailing) は変更しない」が非ゴールに入っており、**一級市民として扱われていない**

### 1.2 既存三層分離との関係

`Signal / Decision / ExecutionPolicy` の三層分離は **新規エントリー側の責務分離** を解決した（2026-04-29 design）。Decision レイヤは `NEW_ENTRY` / `EXIT_CANDIDATE` / `HOLD` / `COOLDOWN_BLOCKED` を出すが、`EXIT_CANDIDATE` は実発注されず TP/SL/Trailing 任せという "歯抜け" 状態だった。

理想を追うなら、入口（新規エントリー）と出口（既存ポジションの決済）は対称な責務であり、それぞれ独立したレイヤで扱うべき。本設計は **Exit レイヤを第四層として追加**し、出口管理を一級市民化する。

### 1.3 ユーザ要件（議論で確定した方針）

- **表示の理想化**だけでなく、Exit を構造的に再定義する（症状対処でなく根本対処）
- **永続化は変化点のみ**: tick ごとの DB write はしない。"人間が見て意味のある状態変化" だけ残す
- **SL/TP はルール保存型・動的計算**: ATR レジーム変化への追従は機械売買の強み。固定化はしない
- **decision_log は状態遷移イベントのみ**: tick 単位の揺らぎはログに残さない
- **Exit は Decision とは独立した第四レイヤ**: 入口と出口を対称な構造で扱う

---

## 2. ゴール

### 2.1 アーキテクチャ目標

`Signal / Decision / Exit / ExecutionPolicy` の四層分離を導入する:

1. **Signal**: 市況解釈に専念（既存）
2. **Decision**: 新規エントリー意図のみ。`EXIT_CANDIDATE` を撤廃
3. **Exit (新設)**: 既存ポジションの出口管理。SL/TP/Trailing 発火、シグナル反転 exit（将来拡張）、手動 close をすべて一元管理
4. **ExecutionPolicy**: Risk + BookGate + 発注（既存）

### 2.2 機能要件

- 建玉と 1:1 対応する `ExitPlan` ドメインエンティティを定義し、SQLite に永続化する
- `ExitPlan` の状態遷移（作成 / Trailing 活性化 / HWM 引き上げ / close）が `decision_log` に残る
- bot 再起動後、SL/TP は ExitPlan の保存済みルールから復元される（HWM のみ揮発許容）
- `/api/v1/positions` レスポンスに ExitPlan を合成し、フロント `PositionsAndTradeCard.tsx` で SL/TP/Trailing を表示する
- 楽天 API 建玉と DB の ExitPlan の整合性を起動時 + 5 分間隔で reconciliation する

### 2.3 非機能要件

- バックテスト過去結果（`production_ltc_60k`）との Return / MaxDD / トレード回数の差が **±1% 以内** に収まること（リグレッション検証）
- tick ごとの DB write を増やさない（HWM 引き上げ時のみ write）
- 既存の三層分離アーキテクチャ・既存 decision_log スキーマを破壊しない（拡張のみ）

---

## 3. 非ゴール

明示的に本設計の対象外とする:

- **既存プロファイルの数値最適化**: SL/TP の数値変更は PDCA の仕事
- **複数銘柄ヘッジ運用**: 1 symbol × 1 ExitPlan の 1:1 を堅持
- **指値での SL/TP 発注**: 成行のみ（既存 `OrderExecutor.ClosePosition` の制約踏襲）
- **ExitPlan のユーザ手動編集 UI**: bot が決めた SL/TP をユーザが画面から動かす機能（誤操作リスク、YAGNI）
- **HWM の永続復元**: HWM は揮発で割り切る。再起動後は現在価格から再構築する
- **シグナル反転 exit の実装**: 第四レイヤの拡張枠は確保するが、Phase 1〜3 の実装範囲外（Phase 4 で検討）

---

## 4. アーキテクチャ

### 4.1 四層構造（Exit を追加）

```
[Indicator] → [Signal] → ┬→ [Decision] ────┐
                          │                 ├→ [ExecutionPolicy] → [OrderExecutor]
                          └→ [Exit] ────────┘
```

| レイヤ | 入力 | 出力 | 責務 |
|---|---|---|---|
| Signal | IndicatorEvent | MarketSignalEvent (Direction, Strength) | 市況解釈のみ |
| Decision | MarketSignalEvent + Position | ActionDecisionEvent (NEW_ENTRY / HOLD / COOLDOWN_BLOCKED) | **新規エントリー意図のみ** |
| **Exit (新設)** | MarketSignalEvent + ExitPlan + Tick | ExitDecisionEvent (HOLD / EXIT_TRIGGERED) | **既存ポジションの出口管理** |
| ExecutionPolicy | ActionDecisionEvent / ExitDecisionEvent | OrderEvent | Risk + BookGate + 発注 |

### 4.2 Decision からの `EXIT_CANDIDATE` 撤廃

現行 Decision は新規エントリーと「シグナル反転による exit 候補」の両方を扱っているが、後者は実発注されず TP/SL/Trailing 任せの歯抜けだった。理想設計では:

- Decision は **新規入口だけ**に責務集中
- 既存ポジションへの全判断は Exit レイヤに集約（SL/TP/Trailing 発火、シグナル反転 exit、手動 close）
- ロング保有中の下落シグナルは「Decision=HOLD（保有中はナンピンしない）+ Exit が判断（Phase 1〜3 では HOLD のまま、Phase 4 でシグナル反転を検討）」

### 4.3 イベントフロー

1. **tick 到着** → Exit レイヤが ExitPlan を読み、SL/TP/Trailing 発火を判定
2. **プライマリ足確定** → Signal が Direction を出す → Decision と Exit が並列で受信
3. **Exit が ExitTriggered** → ExecutionPolicy が close 注文を発行
4. **Decision が NEW_ENTRY** → ExecutionPolicy が new 注文を発行
5. **約定イベント** → Exit レイヤが ExitPlan を作成（new）／ closed に更新（close）

---

## 5. ドメインモデル

### 5.1 ExitPlan エンティティ

```go
// backend/internal/domain/entity/exit_plan.go
type ExitPlan struct {
    ID                  int64       // 主キー
    PositionID          int64       // 建玉と 1:1（unique）
    SymbolID            int64
    Side                OrderSide   // 建玉の方向（BUY=ロング / SELL=ショート）
    EntryPrice          float64     // 約定価格（不変）

    // SL/TP のルール（ルール保存型・動的計算）
    StopLossRule        StopLossRule    // ATR×Multiplier or 建値%
    TakeProfitRule      TakeProfitRule  // %のみ（現状仕様踏襲）

    // Trailing の動的状態
    TrailingMode        TrailingMode    // Disabled / ATR / Percent
    TrailingActivated   bool            // 含み益超えで true
    TrailingHWM         *float64        // 建玉開始からの最良値（Activated 後のみ非 nil）

    CreatedAt           int64           // unix ms
    UpdatedAt           int64           // 状態遷移ごとに更新
    ClosedAt            *int64          // close で確定
}

type StopLossRule struct {
    Mode        StopLossMode    // ModeATR / ModePercent
    ATRMult     float64         // ModeATR 時のみ意味あり
    Percent     float64         // ModePercent 時のみ意味あり
}

type TakeProfitRule struct {
    Percent     float64         // 0 で無効
}
```

### 5.2 ルールから現在価格を導出（read 時に毎回計算）

```go
// CurrentSLPrice は現時点の SL 価格を返す。ATR モードでは currentATR が
// 揺らぐと結果も揺らぐ（仕様：ATR レジーム変化への追従）。
func (e *ExitPlan) CurrentSLPrice(currentATR float64) float64 {
    distance := e.StopLossRule.Distance(e.EntryPrice, currentATR)
    if e.Side == OrderSideBuy {
        return e.EntryPrice - distance
    }
    return e.EntryPrice + distance
}

// CurrentTrailingTriggerPrice は HWM から SL 距離分戻った価格。
// TrailingActivated == false の間は nil を返す（未発動状態を表現）。
func (e *ExitPlan) CurrentTrailingTriggerPrice(currentATR float64) *float64 { ... }
```

### 5.3 不変条件

1. `PositionID` は unique（建玉に対して ExitPlan は 1 つだけ）
2. `EntryPrice` はライフサイクル中に不変
3. `TrailingHWM` はロングなら単調増加、ショートなら単調減少のみ許容
4. `TrailingActivated == false` のとき `TrailingHWM == nil`
5. `ClosedAt != nil` の ExitPlan に対する更新は禁止

### 5.4 Repository インタフェース

```go
// backend/internal/domain/repository/exit_plan_repository.go
type ExitPlanRepository interface {
    Create(ctx context.Context, plan ExitPlan) error
    FindByPositionID(ctx context.Context, positionID int64) (*ExitPlan, error)
    ListOpen(ctx context.Context, symbolID int64) ([]ExitPlan, error)
    UpdateTrailing(ctx context.Context, planID int64, hwm float64, activated bool) error
    Close(ctx context.Context, planID int64, closedAt int64) error
}
```

### 5.5 設計上の鍵

- **EntryPrice の不変性** が「SL/TP のルールは保存、現在価格は read 時に計算」を支える土台
- **TrailingHWM だけが動的状態** で、永続化が必要なのはこれと `TrailingActivated` のみ。SL/TP 価格は計算で導出できるので保存不要
- 1:1 制約により「ポジションごとに独立したライン」が保証される

---

## 6. データフロー

### 6.1 イベント追加

```go
ExitPlanCreatedEvent     // ポジション約定時
ExitPlanTrailingEvent    // HWM 引き上げ・Activated 切り替え
ExitDecisionEvent        // Exit レイヤの判断（HOLD or EXIT_TRIGGERED）
ExitPlanClosedEvent      // close 約定時
```

### 6.2 Tick 処理フロー（SL/TP/Trailing 発火経路）

```
TickEvent
  └→ ExitHandler.OnTick()
       1. repo.ListOpen(symbolID) で保有 ExitPlan を全取得
       2. 各 plan について:
            a. CurrentSLPrice / TP / Trailing を計算
            b. SL/TP/Trailing いずれかにヒット?
                 yes → ExitDecisionEvent{ EXIT_TRIGGERED, planID, reason } emit
                       + decision_log に記録
            c. ロングで currentPrice > EntryPrice かつ HWM 未活性 →
                 ExitPlanTrailingEvent{ Activated: true, HWM: currentPrice } emit
                 + repo.UpdateTrailing で永続化
            d. 既に Activated で HWM 更新条件を満たす →
                 ExitPlanTrailingEvent{ HWMRaised, HWM: currentPrice } emit
                 + repo.UpdateTrailing で永続化
       3. ExitDecisionEvent を ExecutionPolicy に流す
```

### 6.3 プライマリ足確定時（シグナル反転 exit の将来拡張ポイント）

```
MarketSignalEvent
  ├→ DecisionHandler  (新規エントリー意図のみ)
  └→ ExitHandler.OnSignal()  ← 将来拡張枠
        Phase 1〜3: 何もしない（現行踏襲）
        Phase 4: シグナル反転 + 含み益条件などで EXIT_TRIGGERED
```

### 6.4 約定時フロー（ExitPlan のライフサイクル）

```
新規約定 (OrderExecutor が new long を約定)
  └→ ExitPlanCreatedEvent emit
       └→ ExitHandler.OnPositionOpened()
            - StrategyProfile.Risk から StopLossRule / TakeProfitRule / TrailingMode を読む
            - ExitPlan を組み立てて repo.Create()
            - decision_log に "exit_plan_created" を記録

close 約定 (Exit 発火 or 手動 close)
  └→ ExitPlanClosedEvent emit
       └→ ExitHandler.OnPositionClosed()
            - repo.Close(planID, now)
            - decision_log に "exit_plan_closed: {triggered_by}" を記録
```

### 6.5 既存 RiskManager との関係

- `RiskManager.CheckStopLoss / CheckTakeProfit / CheckTrailingStop / UpdateHighWaterMark` は **Exit レイヤに完全移管して廃止**
- `RiskManager.UpdateATR` は残す（ATR は ExitHandler が読み込む共有状態）
- `MaxPositionAmount` などの新規エントリーガードは ExecutionPolicy 側に残る

### 6.6 永続化の I/O コスト

tick ごとの DB write は以下の場合のみ:
- TrailingActivated 切り替え（建玉ごとに 1 回）
- HWM 更新（ロングなら新高値到達時のみ、tick の大半は no-op）
- Close

通常の tick は read-only（メモリキャッシュ可）。HWM の write は build-up 期は多いが、安定後は数分〜数十分に 1 回程度に減衰する想定。

---

## 7. UI（ポジション・手動取引エリア）

### 7.1 API レスポンス拡張

```go
// backend/internal/interfaces/api/handler/position.go
type PositionWithExitDTO struct {
    // 既存 entity.Position フィールド
    ID, SymbolID, OrderSide, Amount, RemainingAmount, Price, FloatingProfit, ...

    // Exit レイヤから合成
    ExitPlan *ExitPlanDTO `json:"exitPlan,omitempty"`
}

type ExitPlanDTO struct {
    StopLossPrice           float64  `json:"stopLossPrice"`        // 現時点の動的計算結果
    StopLossRule            string   `json:"stopLossRule"`         // "ATR×1.5" or "1.2%" 等の表示用
    TakeProfitPrice         *float64 `json:"takeProfitPrice"`      // null = TP 無効プロファイル
    TrailingMode            string   `json:"trailingMode"`         // "DISABLED" / "ATR" / "PERCENT"
    TrailingActivated       bool     `json:"trailingActivated"`
    TrailingHWM             *float64 `json:"trailingHwm"`          // null = 未活性
    TrailingTriggerPrice    *float64 `json:"trailingTriggerPrice"` // null = 未活性
}
```

### 7.2 動的揺らぎへの UI 配慮

ATR モード SL は tick ごとに価格が揺れる。誤認させないために:

- `stopLossRule` をルール文字列で同梱（"ATR×1.5"）して、価格は導出値であることを明示
- WebSocket で tick ごとに ExitPlan を push して画面表示と実発火を同期
- 揺らぎの大きさは ATR レジーム次第なので、UI 上は「今この瞬間の値」として淡々と表示

### 7.3 `PositionsAndTradeCard.tsx` のレイアウト

```
┌─────────────────────────────────────────┐
│ LONG 0.1                       +¥1,234  │
│ 建値 ¥10,000                      [決済] │
│ ─────────────────────────────────────── │
│ SL  ¥9,800   (ATR×1.5)                  │
│ TP  ¥10,300  (3.0%)                     │
│ Trailing  HWM ¥10,250 → 発動 ¥10,170    │
└─────────────────────────────────────────┘
```

未活性・無効時のバリエーション:
- TP 無効プロファイル → `TP  —` 灰色表示
- Trailing 未活性（含み益未到達）→ `Trailing  含み益到達待ち` 灰色表示
- Trailing 無効モード → 行ごと非表示

### 7.4 フロント `Position` 型拡張

```ts
// frontend/src/lib/api.ts
export type ExitPlan = {
  stopLossPrice: number
  stopLossRule: string
  takeProfitPrice: number | null
  trailingMode: 'DISABLED' | 'ATR' | 'PERCENT'
  trailingActivated: boolean
  trailingHwm: number | null
  trailingTriggerPrice: number | null
}

export type Position = {
  // 既存フィールド
  ...
  exitPlan: ExitPlan | null  // null = ExitPlan 未作成（手動建玉等）
}
```

### 7.5 手動建玉への対応

手動取引で建てた建玉にも ExitPlan を自動生成する。bot 自動 SL/TP/Trailing の対象になる。これは現行 `RiskManager.UpdatePositions` の挙動と同じ。

将来「手動建玉に SL を付けたくない」ニーズが出たら、ExitPlan に `Source` フィールド（`AUTO` / `MANUAL` / `IMPORTED`）を追加して制御する余地を残す。今は実装しない（YAGNI）。

---

## 8. エラー処理・整合性

### 8.1 整合性が崩れるシナリオと対策

| シナリオ | 結果 | 対策 |
|---|---|---|
| 約定したが ExitPlan 作成 DB write が失敗 | SL/TP/Trailing が効かない建玉が放置 | §8.2 |
| close 約定したが ExitPlan close 更新が失敗 | 既に存在しない建玉に対して Exit が発火し続ける | §8.3 |
| bot 再起動時、楽天 API 上の建玉と DB の ExitPlan がズレている | 孤児 ExitPlan / ExitPlan のない建玉 | §8.4 |
| 楽天 API で手動 close（楽天サイトから操作）された | bot 知らずに ExitPlan が残る | §8.4 |

### 8.2 約定時の ExitPlan 作成失敗

- `ExitHandler.OnPositionOpened` 内で **最大 3 回までリトライ**
- 最終失敗で **`HaltAutomatic("exit_plan_create_failed")` でトレード自動停止** + decision_log に critical 記録
- "建玉はあるが ExitPlan がない" 状態を絶対に放置しない

### 8.3 close 時の ExitPlan close 失敗

- close 約定時点で楽天 API 上の建玉は既に消えているので、ExitPlan close 失敗は整合性影響が小さい
- 次の tick で `ListOpen` が返してきても positionID が楽天側で見つからず空打ちになる
- 孤児 ExitPlan は §8.4 の reconciliation で掃除

### 8.4 起動時 / 定期 reconciliation

```
exchange_positions = orderClient.GetPositions(symbolID)  // 楽天 API
db_exit_plans     = repo.ListOpen(symbolID)              // DB

// 楽天にあるが DB に ExitPlan がない → 作成（手動建玉や DB 欠損の救済）
// DB にあるが楽天にない → ExitPlan を closed_at = now, closed_by = "reconciler_orphan" で閉じる
// 両方にあるが Side / EntryPrice 不一致 → critical log + Halt
```

- 起動時 + 5 分間隔で実行（AGENTS.md の `STATE_SYNC_INTERVAL_SEC` 15 秒とは別、reconciliation は重いので低頻度）

### 8.5 Trailing HWM の永続化失敗

- HWM 更新 DB write 失敗は **トレード継続に影響しない critical event ではない**（メモリ上の HWM は残る）
- 再起動で失われるが、次の HWM 更新で上書きされるのを待つ（リトライしない、保守的）
- 失敗が連続したら警告ログ

### 8.6 ATR が取れない場合のフォールバック

- ATR ベース SL ルールで `currentATR == 0` のとき:
  - **保守的に固定% SL ルールへフォールバック**（profile から `StopLossPercent` を補完）
  - decision_log に warning 記録
  - ATR 復活後の次 tick で自動的に ATR 計算に戻る
- "ATR が取れないから SL なし" は絶対にやらない

### 8.7 設計上の要点

- **整合性ガードは「無防備な建玉を作らない」が最優先**: SL/TP がない建玉を絶対に許さない
- **reconciliation は楽天 API を真実の源として整合させる**: bot 内部 DB を信じすぎない
- **HWM だけは失われても致命傷ではない**: 揮発的 state として割り切る

---

## 9. テスト戦略

### 9.1 ドメイン層
- `ExitPlan.CurrentSLPrice` / `CurrentTrailingTriggerPrice` / `Distance`: ATR/Percent モード × Long/Short × Activated 状態の table-driven test
- 不変条件: EntryPrice 不変、HWM 単調性、ClosedAt 後の更新拒否

### 9.2 Repository 層
- SQLite 統合テスト（既存 `database/migrations.go` パターン踏襲）
- 1:1 制約（`PositionID unique`）の違反検出
- `ListOpen` の filter 正当性

### 9.3 Exit Handler 層
- TickEvent → SL/TP/Trailing 発火判定の各ケース
- HWM 引き上げ発火条件
- 約定イベントから ExitPlan 作成までのワイヤリング
- ATR 0 フォールバックの動作確認

### 9.4 Reconciler
- 楽天 API モック × DB 状態の組み合わせ:
  - 楽天にあり DB にない → ExitPlan が作られる
  - DB にあり楽天にない → ExitPlan が closed になる
  - 両方にあるが Side 不一致 → Halt が呼ばれる

### 9.5 バックテスト整合性
- `backtest/handler.go` の `TickRiskHandler` を Exit レイヤと同じドメインロジックに差し替え
- `production_ltc_60k` プロファイルで Phase 1 完了直後と比較し、Return / MaxDD / トレード回数の差が **±1% 以内**

### 9.6 E2E（Phase 3 リリース前）
- 手動建玉 → ExitPlan 自動作成 → SL ヒット → close 約定 → ExitPlan closed のフルフロー
- bot 再起動を挟んで HWM が再構築される

---

## 10. ロールアウト計画

### Phase 1: ExitPlan ドメイン + Repository + シャドウ運用
- ExitPlan エンティティ + Repository 実装
- `RiskManager` の挙動は変えず、約定時にシャドウで ExitPlan を作るだけ（影武者運用）
- API/UI 変更なし
- **検証**: ExitPlan の DB 書き込みが楽天 API 建玉と整合しているか観察

### Phase 2: Exit レイヤを EventDrivenPipeline に組み込み、SL/TP/Trailing 発火を移管
- ExitHandler を tick handler chain に追加
- `RiskManager.CheckStopLoss/TakeProfit/Trailing/UpdateHWM` を **削除**
- Reconciler 起動
- **検証**: バックテスト整合性テスト合格、`production_ltc_60k` で 1 週間並走観察

### Phase 3: API/UI 拡張
- `/api/v1/positions` レスポンス拡張
- WebSocket で ExitPlan を push
- `PositionsAndTradeCard.tsx` の建玉カード拡張（§7 のレイアウト）
- **検証**: ブラウザで SL/TP/Trailing 表示が tick ごとに更新される

### Phase 4（将来）: シグナル反転 exit
- `ExitHandler.OnSignal` に判断ロジック追加
- Phase 3 までのインフラがそのまま使える

---

## 11. 用語集

- **ExitPlan**: 建玉に 1:1 で対応する出口管理エンティティ。SL/TP のルールと Trailing の動的状態を保持
- **HWM (High Water Mark)**: 建玉開始からの最良値（ロング: 最高値、ショート: 最安値）。Trailing Stop の基準点
- **TrailingActivated**: 建玉が含み益に入って Trailing 追跡が活性化した状態フラグ
- **Reconciler**: 楽天 API 建玉と DB ExitPlan の整合性を確認・修正する定期処理
- **Phase 1〜3**: 本設計の段階的導入計画（§10）
