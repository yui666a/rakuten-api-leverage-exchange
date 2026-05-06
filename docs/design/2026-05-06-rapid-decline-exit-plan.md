# 急落時 SELL 経路の補強 — PDCA / ADR 起草

- **作成日**: 2026-05-06
- **Issue**: [#252](https://github.com/yui666a/rakuten-api-leverage-exchange/issues/252)
- **Status**: Proposed (PDCA 検証待ち)
- **対象 profile**: `production` (LTC/JPY × PT15M)
- **関連設計書**:
  - `docs/superpowers/specs/2026-05-04-exit-plan-first-class-design.md` (Phase 4 「シグナル反転 exit」非ゴール議論)
  - `docs/design/2026-04-29-signal-decision-policy-separation-design.md` (PR3 三層分離)

---

## 1. 背景

### 観察された運用事象

2026-05-06 23:00 JST、LTC/JPY PT15M ライブ運用中、価格が **9018 → 8909 (-1.2%)**
の急落フェーズで bot は SELL シグナルを一切出さず、ユーザが手動で全建玉を close。
含み益はピーク `+¥350/建玉` から `+¥16/建玉` まで縮小していた。

| JST | 価格 | RSI | MACD hist | bot 判断 |
|---|---|---|---|---|
| 20:30〜21:00 | 9018 | 76.2 | +0.1〜+1.6 | CONTRARIAN BEARISH → EXIT_CANDIDATE (実発注なし) |
| 21:00〜22:45 | 8970〜8994 | 47〜59 | -7.6〜-3.5 | TREND_FOLLOW、SELL 出ず |
| 23:00 | 8909 | 31.4 | **-11.9** | CONTRARIAN BUY を MACD で見送り、HOLD |

### 構造的な問題

1. `evaluateContrarian` (`backend/internal/usecase/strategy.go:651`) は
   RSI < `ContrarianRSIEntry` でも MACD histogram が `MACDHistogramLimit` (= -10)
   より下にあれば「落ちナイフ掴むな」で BUY を見送る。これは新規エントリの
   安全装置として正しい。
2. しかし bot 全体として **保有中ロングへの「危険」シグナル経路** が無く、
   exit は SL / TP / Trailing にしか任されていない。
3. EXIT_CANDIDATE が出ても `exit_on_signal=false` で実発注に繋がらず
   (PR #240 で promote 不可と判断済)、Decision レイヤの判断は
   `decision_log` には残るが発注パスを動かさない。

### 設計書 Phase 4 との関係

ExitPlan 設計書 (2026-05-04) では「シグナル反転 exit」は **明示的な非ゴール**
として Phase 4 で再検討するスコープ。本ドキュメントはその Phase 4 議論を
具体化するための起点。

---

## 2. 検討候補

### B-1: profile で `exit_on_signal=true` を有効化

- 既に PR #240 で PDCA を回し、production への promote は不可と判断済
- 別パラメータ組合せ (例: `exit_on_signal=true` + `take_profit_percent` 引き下げ)
  での再検証は価値がある
- 別 profile (LTC PT5M、ETH 等) で先に試すのも一案

### B-2: TP を厳しく (4% → 2% 等) して早期利確

- 利益最大化と MaxDD のトレードオフ調整
- PDCA 必須

### B-3: Trailing reversal distance を狭める (ATR×2.5 → ATR×1.5 等)

- 反転時に早く利益確保
- ノイズで早すぎ exit のリスク
- PDCA 必須

### B-4: 「保有中 + MACD hist が急速に陰に振れた」を新しい exit シグナルとして追加

- 「急落モメンタム検知」の新ドメインロジックが必要
- 設計書 Phase 4 (シグナル反転 exit) の発展形
- 大規模変更、設計書追加が必要

### B-5: 何もしない (人間判断 + UI 補強)

- Issue #249 (Phase 3 UI) で SL/TP/Trailing 表示が入れば判断材料が増える
- bot を完全自動化したいなら不適切
- 短期暫定としては有効

---

## 3. 推奨アプローチ

```
短期 (1〜2 週間):  B-5 で運用継続 + Issue #249 で UI 補強
中期 (1〜2 ヶ月):  B-1, B-2, B-3 を PDCA で並行検証
長期 (Phase 4):    B-4 を改めて設計
```

中期の PDCA は **trailing 系 (B-3) → TP 系 (B-2) → exit_on_signal (B-1)** の
順で軽い変更から試す。複数同時の変更は原因切り分けが困難になる。

---

## 4. PDCA Plan (中期サイクル候補)

### Cycle X-1: Trailing reversal を狭める (B-3)

| 項目 | 値 |
|---|---|
| Hypothesis | `trailing_atr_multiplier` を 2.5 → 1.5 にすると、急落フェーズで含み益を保持したまま exit できる回数が増え、prod の MaxDD と Sharpe を改善する |
| Profile | `experiment_2026-05-XX_trailing_atr_1_5.json` |
| 軸 | `trailing_atr_multiplier`: 2.5 → 1.5 |
| 検証期間 | 3m / 6m / 1y / 2y (production と同じ) |
| 採用基準 | 4 期間すべて MaxDD ≤ 20%、3 期間以上で Total Return が production を上回る、Sharpe が 4 期間平均で改善 |

### Cycle X-2: TP を引き下げる (B-2)

| 項目 | 値 |
|---|---|
| Hypothesis | `take_profit_percent` を 4 → 2 にすると、ピーク後の急落で利確を逃す頻度が減り、Total Return が改善する |
| Profile | `experiment_2026-05-XX_tp_2pct.json` |
| 軸 | `take_profit_percent`: 4 → 2 |
| 検証期間 | 3m / 6m / 1y / 2y |
| 採用基準 | Cycle X-1 と同じ |

### Cycle X-3: exit_on_signal 再検証 (B-1)

| 項目 | 値 |
|---|---|
| Hypothesis | Cycle X-1 / X-2 で改善した profile に `exit_on_signal=true` を足すと、reversal 期に bear 判定で exit して MaxDD がさらに縮む |
| Profile | `experiment_2026-05-XX_exit_on_signal.json` |
| 軸 | `exit_on_signal`: false → true (前 cycle 採用案を base) |
| 検証期間 | 3m / 6m / 1y / 2y |
| 採用基準 | PR #240 の reject 条件 (Total Return が下がる、もしくは取引数が膨れて手数料負け) を必ず明示比較 |

各 cycle で `docs/pdca/2026-05-XX_cycleNN.md` を起こす。命名規約は既存に準拠。

---

## 5. ADR 起草

### Title
**ADR: 急落モメンタム時の保有ロングに対する exit 経路を補強するか**

### Status
**Proposed** — PDCA Cycle X-1〜X-3 の結果待ち

### Context
- §1 を参照
- 構造的に「保有中ロングへの撤退判断は SL/TP/Trailing 任せ」になっている
- ユーザが UI を見て手動 close できるが完全自動化の理想からは乖離

### Decision (案)

PDCA 結果に応じて 4 シナリオを想定:

| シナリオ | PDCA 結果 | 採択方針 |
|---|---|---|
| S1 | Cycle X-1 (trailing) で十分改善 | trailing パラメータ調整のみで promote、B-1/B-2/B-4 不採用 |
| S2 | Cycle X-2 (TP) で改善 | TP 引き下げで promote、B-1/B-4 不採用 |
| S3 | Cycle X-3 (exit_on_signal) で改善 | profile に `exit_on_signal=true` を入れて promote |
| S4 | 上記すべて改善せず | B-4 (専用 exit シグナル) の設計に進む or 保留 |

### Consequences (期待)
- bot の自動 exit 範囲が広がり、ユーザの手動介入頻度が下がる
- MaxDD が縮むかわりに Total Return がやや低下する可能性 (TP 引き下げ系)
- 取引回数増による手数料コストの監視が新たに必要

### Consequences (リスク)
- パラメータ過剰最適化で他期間で性能劣化する可能性
- B-4 まで進む場合は設計コストが大きく、設計書 Phase 4 との二重管理に注意

---

## 6. 子 Issue 候補

PDCA 各 cycle と ADR finalize は別 Issue として切り出す:

1. `[pdca] cycle X-1: trailing_atr_multiplier 1.5 検証`
2. `[pdca] cycle X-2: take_profit_percent 2% 検証`
3. `[pdca] cycle X-3: exit_on_signal 再検証`
4. `[adr] ADR-NN: 急落モメンタム時の exit 経路` (PDCA 結論を反映して finalize)

本 Issue (#252) はこれら子 Issue が出揃った時点で close 候補。

---

## 7. 短期措置 (今すぐ実施)

- [ ] Issue #249 (Phase 3 UI) の plan を作成 → 実装すれば B-5 (人間判断) の精度が上がる
- [ ] 観察期間中 (Issue #247) は production 設定変更しない
- [ ] このドキュメントを review、PDCA cycle X-1 の plan が固まったら Issue を切る
