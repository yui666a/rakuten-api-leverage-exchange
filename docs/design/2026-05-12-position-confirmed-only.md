# 2026-05-12 — Position confirmed-only: Live↔Backtest 共通 Exit/Risk レイヤー

**Status**: Approved (2026-05-12, 設計承認済 / 実装未着手)
**Author**: Claude Code (3h+1h PDCA セッション後の事故対応設計)
**Reviewers**: Codex (codex-rescue 経由でレビュー受領、本書に反映済)
**Scope**: `backend/internal/infrastructure/live/` + `backend/internal/usecase/exitplan/` + `backend/internal/usecase/backtest/` (handler / simulator / executor) の Exit/Risk レイヤー
**Related**:
- 旧 (不完全な) 修正 PR: #259 `fix(live): MARKET 約定で venue が Price=0 を返した時に GetOrders で実約定価格を取り直す`
- 本番事故メモ: `~/.claude/projects/.../memory/project_exitplan_entryprice_bug.md`
- PDCA: `docs/pdca/2026-05-12_aggressive_search.md` (本事故と直接の関連は無いが、production v9 promote 直後の事故という時系列で並ぶ)

---

## 1. 背景: 2026-05-12 の本番事故

`production v9` (LTC × PT15M, p5b: tp=2.2 / sl=10 / r=2.0) を promote した直後の最初のエントリで以下が発生:

| 時刻 (JST) | 出来事 |
|---|---|
| 08:45:01 | OPEN BUY 0.9 LTC at ¥9,211.9〜¥9,219.0 (3 ピース約定: positionID 275049/050/052) |
| 08:45:01 直後 | `shadow ExitPlan created entryPrice=¥9,168.4` ← **前 bar の close、本来の約定価格ではない** |
| 08:45:01〜19 | TickRiskHandler が close API を 18 回呼ぶも全部 50042 (ポジ未存在) で失敗 |
| 08:45:19 | `SyncPositions` (15s 間隔) が API から真の positionID と Price を取得 |
| 08:45:20〜22 | 3 ポジ全部 `reason=take_profit` で成行決済 — 損失 -¥43 確定 |

旧修正 PR #259 (`resolveFillPrice` で `GetOrders` を 3 回 polling) のマージ + Bot 再起動後、09:30 にも同じ事故 (-¥24) が再発。**楽天 API は `GetOrders` でも fill record の Price=0 のまま** が判明し、PR #259 の対策は不十分と確定した。

### 1.1 真因 (短く)

```
[submit response Price=0] → fillPriceOf(o, signalPrice=前bar close)
   → RealExecutor.positions[i].EntryPrice = 前bar close (誤値)
   → shadow ExitPlan.entryPrice も同じ誤値
   → TickRiskHandler.tpDistance = 誤値 × 2.2% で TP ライン計算
   → 約定後の数秒で spurious tick で誤発火
   → 全 close → 損失確定
```

### 1.2 本質: EntryPrice の権威ある source が定まっていない

| コンポーネント | 「真実」と思っている source |
|---|---|
| `RealExecutor.positions` (in-memory slice) | `runPlan` の `fillPrice` (= 楽天 API レスポンスの Price、しばしば 0 → signalPrice fallback) |
| `SyncPositions` | 楽天 `GetPositions` API の `ap.Price` (venue 真値、ただし 15s 遅延) |
| `domainexitplan.Repository` (SQLite) | `OrderEvent.Price` (= fillPrice、汚染) |
| `TickRiskHandler.policy` (in-memory) | `pos.EntryPrice` (= 汚染値) |
| `SimExecutor` (backtest) | sim の signalPrice (即時 fill、汚染しない) |

**venue API は真値を持っているが、それを誰も即時参照していない**。さらに live と backtest で EntryPrice の出処が違うため挙動が乖離し、backtest で 2y +67.8% だった戦略が live で初回エントリから -¥24/-¥43 を出した。

---

## 2. 設計判断

### 2.1 採用方針 (Codex レビュー後の修正版)

> **未約定の order は `r.positions` に追加しない**。Position に昇格できるのは **venue が EntryPrice を真値で返した瞬間のみ**。

Codex レビューで指摘された通り、当初案 (`PendingPosition` 型を新設して handler 側に「pending を除外」してもらう) は不要に複雑。**`Positions()` API の戻り値そのものを confirmed-only に保証する** だけで handler 側は無変更で済む。

### 2.2 採用しなかった代替

| 案 | 不採用理由 |
|---|---|
| `signalPrice` を fallback として許容し、 SyncPositions が後で上書きする現状を維持 | 15s 間 (= PT15M bar 1/60 相当) は誤 EntryPrice で TP/SL 判定が走るので事故再発不可避 |
| `PendingPosition` 型を新設して handler API を変更 | 新 type + interface 変更で実装規模 2 倍、Codex 指摘どおり「pending を `Positions()` に混ぜたら結局見える」リスク |
| 約定直後の TickRiskHandler 評価を N bar スキップする safety guard | 暗黙の挙動変更で backtest との一貫性が崩れる、根本解決でない |
| `GetMyTrades` の戻り値を **そのまま** Position として採用 | trade と position は別概念 (1 order → 複数 trade → 1 position) で抽象化が崩れる |

---

## 3. 採用設計

### 3.1 ルール

1. **Submit 完了時点では `r.positions` に追加しない**
   - 代わりに `r.pendingOrders map[int64]pendingContext` で TTL 管理 (既存 `reconcile.reconcileOrders` の logic 流用)
2. **Position への昇格 = venue が真値を返した瞬間**
   - 経路 (a): `SyncPositions` を **Open 直後に即時呼び出し** (15s 間隔の通常 sync を待たない)
   - 経路 (b): `GetMyTrades` polling で trade confirmation を取得
   - 経路 (c): WS `position_update` イベント (現コードに `positionPublisher` あり、要確認)
3. **`Positions()` API は confirmed-only を保証**
   - `EntryPrice > 0` を不変条件として API contract に明記
   - Handler 側は無変更
4. **Tick 内で positions snapshot を固定**
   - `TickRiskHandler.Handle` の冒頭で `positions := executor.Positions()` を取り、その tick の間は再取得しない
   - SyncPositions との時系列的 race を防ぐ
5. **shadow_handler は OrderEvent ではなく Position 昇格イベントを起点に変更**
   - 昇格時に shadow ExitPlan を作成、entryPrice は確定値を必ず使う
6. **Live と Backtest で同じ Position contract**
   - `SimExecutor.Open` は今でも即時 EntryPrice 確定で実質 (a) と等価
   - **`OrderExecutor.Positions` の contract (confirmed-only, EntryPrice>0) を共通 test で強制**
   - fake `GetPositions` / `GetMyTrades` を使った live driver の integration test を backtest にも適用

### 3.2 不変条件 (test で守る)

```
For all Position p in executor.Positions():
  p.EntryPrice > 0
  p.PositionID != 0
  p.Side ∈ {BUY, SELL}
  p.Amount > 0
  ∀ tick t: TickRiskHandler.Handle(t, p) は p.EntryPrice 基準で TP/SL 計算
  (pending order は決して見えない)
```

### 3.3 分割約定への対応

楽天は **1 OrderID から複数 PositionID** を生成しうる (本事故の 275049/050/052)。

- 昇格時に `GetPositions` / `GetMyTrades` の **全ての該当 position を一括登録**
- 各 position は独立した `EntryPrice` (各 trade の Price) を持つ
- shadow ExitPlan も**各 PositionID 単位で作成**
- `closeLocked` は `PositionID` 検索なのでこれで自然に動く (現コードのバグ「1 OrderID = 1 PositionID 前提」が解消)

### 3.4 TTL とエラーハンドリング

- `pendingOrders` の TTL 切れ (例: 60s) → **alert ログ + 自動 retry はしない**
  - venue 側で約定したのに sync が遅延した可能性、または order 自体が失敗した可能性
  - 次の `SyncPositions` で venue の position 一覧と突き合わせて整合させる
- pending 中に重複 signal が来た場合 → 重複 submit を防ぐため、同じ symbol で pending がある間は **新規 Open を skip + ログ**

---

## 4. 段階的移行計画

### 4.1 PR 計画 (3 本)

| PR | タイトル | 内容 | 効果 |
|---|---|---|---|
| **#1** | fix(live): submit 完了で positions に追加せず、確定後に昇格させる | `RealExecutor.Open` で positions に append せず、`pendingOrders` map に登録 / `SyncPositions` を Open 直後に即時呼び出し / `GetMyTrades` 補助で昇格 / `Positions()` contract を confirmed-only に明記 / tick 内 positions snapshot 固定 / 既存 unit test を contract 保証付きに更新 | **事故再発を完全停止** |
| #2 | refactor(exitplan): shadow ExitPlan を Position 昇格イベント駆動に変更 | shadow_handler の OrderEvent 起点を Position 昇格イベント起点に変更、ExitPlan.entryPrice は必ず確定値 | 観測ログの整合 |
| #3 | refactor(backtest): SimExecutor を fake venue interface で駆動、Live↔Backtest 共通 integration test 追加 | `SimExecutor` を fake `GetPositions` / `GetMyTrades` で駆動、`OrderExecutor.Positions` contract test を live と backtest 両方で通す | live↔backtest 挙動一致を機械的に保証 |

### 4.2 PR 間の依存

- #1 は単独で本番反映可能 (事故停止が最優先)
- #2 は #1 後、shadow ExitPlan のログ整合のみで critical path には載らない
- #3 は #1 + #2 後、 integration test の追加で再発防止を補強

### 4.3 各 PR で Codex レビューを挟む

> 「コード設計は今は不要で、実際に実装してからcodexレビューを挟むこと」(ユーザー方針)

- PR を上げる前に Codex に diff レビューを依頼
- 観点: 契約違反のリスク、race condition、test カバレッジ漏れ
- Codex の指摘は本 ADR に追記して history を残す

---

## 5. テスト戦略

### 5.1 単体テスト (PR #1 内)

| ケース | 期待挙動 |
|---|---|
| `Open()` で venue submit 成功 (Price=0) → 即 `Positions()` を見る | 空 (まだ pending) |
| `Open()` → `SyncPositions()` 即時呼び出し → API から真値取得 → `Positions()` | 1 件、`EntryPrice > 0` |
| `Open()` 後 `GetPositions` で複数行返却 (分割約定) → `Positions()` | 複数件、各々独立 EntryPrice |
| `Open()` 後 TTL 内に venue が空 → TTL 切れ alert ログ | warn 出力、`Positions()` は空 |
| `Open()` 中に重複 signal → 2 回目 Open は skip | warn 出力、API call は 1 回 |
| `TickRiskHandler.Handle` で `Positions()` 取得後に backend で SyncPositions 走っても tick 内 view は固定 | snapshot 維持 |

### 5.2 Integration テスト (PR #3 内)

- `OrderExecutor.Positions` contract を `live.RealExecutor` と `backtest.SimExecutor` の両方で同じテストスイートに通す
- fake `OrderClient` (実装: `GetPositions` で約定価格を返す、 `CreateOrderRaw` で Price=0 を返す) を共通利用
- 不変条件 (3.2) を property test 形式で検証

### 5.3 Live 検証

楽天 wallet に paper trading が無いため:

1. PR #1 マージ後、**production.json を r=0.5 に一時退避** (現在の v9 = r=2.0 から)
2. compose 再起動 + Bot 再開
3. **24h 観察** — 主要ログ:
   - `Open` から `Positions()` に出現するまでの latency
   - `shadow ExitPlan created entryPrice=X` の X が実約定価格付近か (前 bar close と一致しないか)
   - `TTL exceeded` warn が出ていないか
4. 1 trade でも EntryPrice 誤値があれば即停止 + 追加調査
5. 24h 問題なし → r=2.0 に戻して本格運用再開

---

## 6. リスクと緩和

| リスク | 確率 | 影響 | 緩和策 |
|---|---|---|---|
| `SyncPositions` の即時呼び出しで楽天 API rate limit に抵触 | 中 | Open が遅延 | 既存 `orderretry.OnRateLimit` を流用、polling 間隔を測定して動的調整 |
| `GetMyTrades` が遅延し、昇格が間に合わない | 中 | Open 後 N 秒は `Positions()` が空 → signal が複数発火しても entry できない (損失機会) | TTL を長め (60s) に設定、複数 signal は skip log で監視 |
| WS `position_update` が想定通り動かない | 低 | polling 経路に fallback で機能 | 現コード `positionPublisher` の動作確認を PR #1 内で実施 |
| Backtest の挙動が PR #3 で変わる可能性 | 低 | 既存 backtest 結果との互換性 | PR #3 前後で同じ profile / 期間 / 結果が一致することを CI で確認 |
| TTL 切れ alert が連発しスパム化 | 低 | 運用負荷 | rate limiter 内蔵 (per-symbol、最低 5min 間隔) |

---

## 7. 失敗時のロールバック

### 7.1 PR #1 マージ後に事故が再発した場合

1. **即時 Bot 停止** (`curl -X POST localhost:38080/api/v1/stop`)
2. PR #1 を revert (`git revert <sha>` → 緊急 PR で main へ)
3. 旧 `production.json.bak.20260512` (v8) に切り戻し検討
4. ログから昇格イベントの欠落 / 遅延の証拠を採取し、 ADR に追記

### 7.2 PR #2 / #3 で backtest 結果が変わった場合

- backtest CI が失敗するので merge ブロック
- diff を Codex レビューに回してから対応判断

---

## 8. 関連残課題 (本 ADR 範囲外)

これらは本設計とは独立だが、本事故の周辺で確認された問題。別 Issue 起票推奨:

1. **`trailing_atr_multiplier` バグ** (`handler.go:946`)
   - `trailingDistance` で `percentDist > atrDist` のスケール条件により、LTC ¥9,000 帯では trailing が常に percent SL 経由で動く
   - `trailing_atr_multiplier` の値を変えても挙動が変わらない
2. **`signal_source` タグが全て `unknown`**
   - `bySignalSource` 集計が壊れていて trend_follow / contrarian / breakout の内訳が見えない
   - decision_log には `signal.reason` で文字列として残っているが構造化されていない
3. **2026-05-12 08:45 の `lastPrice=¥9,414.56` 異常 tick**
   - WS の bid/ask 異常 or lagged tick or broken pipe 後の再接続データの可能性
   - 本 ADR の対策で **誤った EntryPrice は防げる**が、異常 tick 自体が pipeline に流入する経路は別途調査要

---

## 9. 関連ファイル

- 旧 fix PR: https://github.com/yui666a/rakuten-api-leverage-exchange/pull/259 (merged commit `2daa4e2`)
- 関連コード (実装着手時に触る範囲):
  - `backend/internal/infrastructure/live/real_executor.go`
  - `backend/internal/usecase/exitplan/shadow_handler.go`
  - `backend/internal/usecase/backtest/handler.go` (`TickRiskHandler` / `RiskHandler`)
  - `backend/internal/infrastructure/backtest/simulator.go`
  - `backend/internal/usecase/reconcile/` (TTL logic 流用元、要確認)
- 関連メモ (Claude memory): `project_exitplan_entryprice_bug.md`

---

## 10. 進め方

1. 本 ADR (この文書) を main にマージ
2. PR #1 着手 → 実装 → ローカル test → Codex レビュー → 修正 → マージ
3. PR #1 マージ後 24h paper-trading 相当 (r=0.5 で本番) で観察
4. PR #2 着手 (同じ flow)
5. PR #3 着手 (同じ flow)
6. 完了後、本 ADR に「実装完了」のステータス追記
