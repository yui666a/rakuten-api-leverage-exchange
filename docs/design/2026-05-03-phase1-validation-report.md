# Phase 1 (Signal/Decision/ExecutionPolicy) 検証レポート

- 作成日: 2026-05-03
- 親設計書: `docs/design/2026-04-29-signal-decision-policy-separation-design.md`
- 対象 PR: #232〜#236（すべて main マージ済み）
- 環境: Docker compose 上の live (`production` profile = LTC/JPY × PT15M @ ¥60k)、backtest CSV (LTC/JPY 6 ヶ月)

PR1〜5 マージ後に行った 4 系統の動作検証の記録。Phase 6+ への引き継ぎ事項を含む。

---

## 検証 (1): Frontend UI 目視確認

### 結果

`/history?tab=decisions` に新列「方向」「判断」「板ガード」が描画され、ラベル和訳もマップ通り：

| 時刻 (JST) | スタンス | 方向 | 判断 | シグナル | 信頼度 | 理由 |
|---|---|---|---|---|---|---|
| 2026-05-03 12:30 | CONTRARIAN | 買い優勢 | 見送り | BUY | 70.9% | ロング保有中: 下落シグナルではない |
| 2026-05-03 12:15 | CONTRARIAN | 買い優勢 | 見送り | BUY | 70.2% | ロング保有中: 下落シグナルではない |
| (省略) | (省略) | (省略) | 見送り | (省略) | (省略) | (省略) |

### 確認できた事象

- `/history?tab=decisions` テーブルに 11 列レンダリング (PR4 まで 9 列)
- DecisionDetailPanel に「Signal / Decision (Phase 1)」セクション
- 監視画面の RecentDecisionsCard ミニテーブルにも「判断」列追加
- `decisionReasonI18n` で英語 reason が和訳

→ **PR5 完全動作**。

---

## 検証 (2): バグ再現 backtest（設計書 §1.1 のシナリオ）

### 入力

- 期間: 2026-04-28 〜 2026-04-29 (バグ発生窓)
- profile: `production_ltc_60k`
- run id: `01KQNY815G0G84F4245VN0GNK8`

### 結果（旧コードで「両建て総額判定」で REJECTED されていた行）

| 時刻 (JST) | signal_action | signal_direction | decision_intent | decision_side | risk_outcome |
|---|---|---|---|---|---|
| 2026-04-29 04:45 | SELL | BEARISH | EXIT_CANDIDATE | SELL | SKIPPED |
| 2026-04-29 06:00 | SELL | BEARISH | EXIT_CANDIDATE | SELL | SKIPPED |
| 2026-04-29 07:00 | SELL | BEARISH | EXIT_CANDIDATE | SELL | SKIPPED |
| 2026-04-29 08:45 | SELL | BEARISH | EXIT_CANDIDATE | SELL | SKIPPED |
| 2026-04-29 12:15 | SELL | BEARISH | EXIT_CANDIDATE | SELL | SKIPPED |
| 2026-04-29 12:30 | SELL | BEARISH | EXIT_CANDIDATE | SELL | SKIPPED |
| 2026-04-29 12:45 | SELL | BEARISH | EXIT_CANDIDATE | SELL | SKIPPED |
| 2026-04-29 13:30 | SELL | BEARISH | EXIT_CANDIDATE | SELL | SKIPPED |
| 2026-04-29 13:45 | SELL | BEARISH | EXIT_CANDIDATE | SELL | SKIPPED |
| 2026-04-29 16:30 | SELL | BEARISH | EXIT_CANDIDATE | SELL | SKIPPED |

### 確認できた事象

- 旧コードで `risk_outcome=REJECTED` (`position limit exceeded: 10613+1750 > 12000`) になっていた 10 件すべてが、新ルートでは **EXIT_CANDIDATE/SELL/SKIPPED** として正しく分類されている。
- Risk が SKIPPED なのは PR3 設計通り（実 exit は TP/SL 経路に任せる、設計書 §4.1）。

→ **両建て総額判定バグの根本治療を実装上で確認**。

---

## 検証 (3): PDCA sweep — 重要な発見

### 当初予定

設計書 §10.2 の sweep 候補で 4×4×3 = 48 通りを実行：
- `entry_cooldown_sec`: 30 / 60 / 120 / 300
- `max_slippage_bps`: 10 / 15 / 20 / 30
- `max_book_side_pct`: 10 / 20 / 30

### 実走結果（部分集合）

期間 2025-11-02 〜 2026-05-02 (6ヶ月) で先頭 6 件:

| cooldown | slip_bps | side_pct | trades | winRate | return | maxDD | pf | bookGateRejects |
|---|---|---|---|---|---|---|---|---|
| 30 | 10 | 10 | 41 | 75.61% | -0.27% | 2.63% | 0.95 | **0** |
| 30 | 10 | 20 | 41 | 75.61% | -0.27% | 2.63% | 0.95 | 0 |
| 30 | 10 | 30 | 41 | 75.61% | -0.27% | 2.63% | 0.95 | 0 |
| 30 | 15 | 10 | 41 | 75.61% | -0.27% | 2.63% | 0.95 | 0 |
| 30 | 15 | 20 | 41 | 75.61% | -0.27% | 2.63% | 0.95 | 0 |
| 30 | 15 | 30 | 41 | 75.61% | -0.27% | 2.63% | 0.95 | 0 |

**完全に同一**。差が一切観測されない。

### cooldown 単軸 sweep（slip/sidepct 固定）

`entry_cooldown_sec` を 0/30/60/120/300/600 で振った 6 件：

| cooldown | trades | winRate | return | maxDD | pf | avgHoldSec |
|---|---|---|---|---|---|---|
| 0 | 41 | 75.61% | -0.27% | 2.63% | 0.95 | 370536 |
| 30 | 41 | 75.61% | -0.27% | 2.63% | 0.95 | 370536 |
| 60 | 41 | 75.61% | -0.27% | 2.63% | 0.95 | 370536 |
| 120 | 41 | 75.61% | -0.27% | 2.63% | 0.95 | 370536 |
| 300 | 41 | 75.61% | -0.27% | 2.63% | 0.95 | 370536 |
| 600 | 41 | 75.61% | -0.27% | 2.63% | 0.95 | 370536 |

→ **すべての設定で完全に同一の結果**。

### 原因分析

#### `max_slippage_bps` / `max_book_side_pct`: backtest 経路で無効化

`backend/internal/usecase/backtest/runner.go:259` の条件：

```go
if input.BookSource != nil && (riskCfg.MaxSlippageBps > 0 || riskCfg.MaxBookSidePct > 0) {
    riskHandler.BookGate = booklimit.New(input.BookSource, ...)
}
```

CSV-only backtest では `BookSource == nil` なので **BookGate がアタッチされない**。値を変えても挙動同一は当然。

→ 検証経路: `slippageModel="orderbook"` + persisted L2 snapshots。

#### `entry_cooldown_sec`: PT15M では発動条件が成立しない

NoteClose は OrderEvent.Timestamp（close 約定時刻）で起動：

```
entryCooldownUntil = close_ts + cooldown_sec
```

DecisionHandler が IsEntryCooldown を読むのは StrategyHandler 発火時、つまり**次バーの IndicatorEvent**（バー終値）。

```
時刻 t  →  close 約定 (cooldown armed)
時刻 t + cooldown_sec  →  cooldown expires
時刻 next_bar_close  →  StrategyHandler fires
                        IsEntryCooldown(next_bar_close) を判定
```

active となる条件:

```
next_bar_close - close_ts < cooldown_sec
```

PT15M (interval = 900s) で cooldown_sec が 60〜600 のとき、よほど特殊な close タイミング（バー終わり直前）以外は次バーまでに expire。実測 cooldown=600 でも 0 件発動。

→ **PT15M で意味ある cooldown_sec ≥ 900**（1 バー分）以上が必要。

### 対処（本 PR でロールイン）

1. **profile 値を 60 → 1800** へ修正（`production.json` / `production_ltc_60k.json`）
2. **設計書 §10.2 の sweep 候補を更新**: 30/60/120/300 → 900/1800/3600/7200
3. **slip/sidepct sweep は CSV では走らせない方針** を §10.2 に注記

---

## 検証 (4): live 動作観察

### 観測時系列 (2026-05-02 21:15 〜 2026-05-03 12:30 JST)

- 2026-05-02 21:15 JST: NEW_ENTRY/BUY が約定（0.2 lot）→ LONG 0.1×2 を保有開始
- 21:30〜12:30 (15+ 時間、22 バー連続): すべての BUY シグナル / NEUTRAL バーで `decision_intent=HOLD` `decision_reason="long held; not bearish"`
- 一度も増し玉発注なし、一度も EXIT_CANDIDATE 不発（=BEARISH シグナルが来なかった）

### 確認できた事象

- ロング保有中の同方向シグナルが DecisionHandler で正しく見送られている（増し玉抑制）
- 旧コードなら：
  - 同方向 (BULLISH) → 増し玉発注でレバレッジ膨張
  - 反対方向 (BEARISH) → 両建て総額 REJECTED でストーム発生
- 新コード後：両方とも秩序ある HOLD / EXIT_CANDIDATE で記録

→ **Phase 1 の意味論が live で実発動している**。

### 副次的に観察された問題（Phase 1 範囲外）

`/positions` API (= []) と `/status` の `totalPosition` (= 1735) の不整合。設計書 §3 で「非ゴール」とされていた `/positions` と RiskManager の整合性問題が live で観測された。Phase 6+ の独立タスクとして繰り越し。

---

## Phase 6+ への引き継ぎ事項

### 即時対応した事項（本 PR）

- ✅ `entry_cooldown_sec` の PT15M 向け値 (1800) に修正
- ✅ 設計書 §10.2 sweep 候補を更新

### Phase 6+ で取り組むべき事項

1. **`/positions` と RiskManager の不整合解消**: live で `/positions=[]` だが `totalPosition=1735` が観測される。in-memory 状態と exchange-side 状態のずれ調査
2. **orderbook backtest 経路の整備**: `slippageModel="orderbook"` + persisted L2 snapshots での BookGate sweep を可能にする運用整備
3. **EXIT_CANDIDATE の opt-in 発注 (`exit_on_signal`)**: 設計書 §1.4 で言及、現状 PR3 で skip 固定
4. **指値 close API**: 成行依存からの脱却（市場インパクト軽減）
5. **`entity.Signal` の完全削除**: PR5 で Deprecated コメントのみ。Phase 6+ で synthSignal 経路ごと書き換え

### 現状のまま運用可能な事項

- live: 1800 秒 cooldown で運用、Phase 1 意味論に依存して取引秩序が保たれる
- backtest: cooldown / BookGate sweep は意味を持たないが、profile 駆動の indicator / signal_rules / position_sizing sweep は通常通り有効
