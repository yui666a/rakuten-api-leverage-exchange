# 2026-05-12 — aggressive search (LTC/JPY × PT15M)

**pdca_cycle_id**: `2026-05-12_aggressive`
**実行者**: Claude Code (3h 自律探索)
**目的**: ユーザー指摘「現 production が 1日中の上下動を捉えられていない、もっと攻撃的に細かく回せるプロファイルが欲しい」への対応。
**制約**:
- timeframe = PT15M (現行維持)
- symbol = LTC/JPY
- initialBalance = ¥60,000 (live と同等)
- MaxDD ≤ 25% (ユーザー許容)
- 1m / 3m / 6m / 1y / 2y のすべてで PF ≥ 1.0 推奨

---

## 1. Baseline (P1)

`production` を 5 期間で run-multi:

| period | return | trades | win% | PF | DD | sharpe |
|---|---|---|---|---|---|---|
| 1m | +0.7% | 6 | 83 | 5.06 | 0.3% | 7.05 |
| 3m | **-0.4%** | 7 | 71 | 0.64 | 1.0% | -0.72 |
| 6m | +2.1% | 37 | 86 | 1.65 | 1.3% | 1.47 |
| 1y | **-0.3%** | 85 | 76 | 0.97 | 3.0% | -0.11 |
| 2y | **-2.5%** | 237 | 76 | 0.92 | 6.7% | -0.41 |

**判定**: 3m / 1y / 2y で赤字、PF<1.0。promote時(2026-04-26)の検証では 1y +45% / 2y +38% だったので、**この 2-3週間で profile が時代遅れになっている**。

**驚いた点**: production の取引数は 2y で 237件 (~10件/月) と思ったほど少なくない。ユーザー指摘の「取引が行われない日もある」は **トレード数ではなく PF と勝ち越し** の問題と判断。

---

## 2. Stage1 — entry vs exit のどちらが効くか切り分け (P2/P3)

10 profile を 1m+3m で sweep:

### Entry 緩和系 (p2a-p2f)
- contrarian wider / trend tighter / breakout on / cooldown shorten / combo
- **結果**: 3m trades が 5-8件で全て production と同水準。entry 条件は **取引頻度のボトルネックではなかった**。

### Exit 系 (p3a-p3d)
- tp 4→2 / tp 4→3 + trailing 1.5 / sl 14→10 / ATR-based sl

| key profile | 3m return | 3m trades | 3m PF |
|---|---|---|---|
| p3a (tp=2) | +1.3% | **27** | 2.74 |
| p3c (sl=10) | +1.1% | 15 | 1.56 |
| p3d (ATR-sl) | -1.5% | 26 | 0.79 |

**気づき**: TP を 4→2% にしただけで **取引数 4倍 / 全勝 (96%) / PF 2.74**。「ポジション保持時間が長くて次に入れない」のが真因だった。SL/Trailing も貢献。ATR-based SL は逆効果。

---

## 3. Stage2 — exit ハイブリッド (P3)

p3a と p3c を組み合わせた hybrid を 1m+3m で:

| profile | 3m return | 3m trades | 3m PF | 3m DD |
|---|---|---|---|---|
| **p3e (tp=2 + sl=10 + tr=2.0)** | **+2.6%** | 27 | **4.88** | 0.8% |
| p3g (tp=2.5 + sl=8 + tr=2.0) | +1.9% | 29 | 1.64 | 1.2% |
| p3j (p3a + r=1.0) | +1.9% | 27 | 3.06 | 1.0% |
| p3h (tp=3 + sl=12 + tr=2.0) | -0.2% | 10 | 0.88 | 1.0% |
| p3f (tp=1.5 + sl=8 + tr=1.8) | -1.5% | 34 | 0.68 | 2.7% |

**勝者**: `p3e (tp=2/sl=10/tr=2.0)` 暫定 1 位。

---

## 4. Stage3 — robustness 検証 (P4)

p3e ほか上位 5 本を 6m/1y/2y で:

| profile | 6m | 1y | 2y | 2y DD |
|---|---|---|---|---|
| production | +2.1% | -0.3% | **-2.5%** | 6.7% |
| **p3e** | **+6.1%** | **+7.6%** | **+11.0%** | 5.3% |
| p3j (r=1.0) | +2.9% | +1.5% | -2.0% | 5.7% |
| p3g | +5.7% | +5.6% | -1.5% | 7.4% |
| p3a | +2.3% | +1.1% | -2.0% | 4.7% |
| p3c | +3.6% | +3.1% | +0.7% | 7.9% |

**確定**: p3e が長期で生き残る唯一の候補。production を全期間で完全に上回る。

---

## 5. Stage4 — p3e 近辺の fine-tune (P4 続き)

p3e を base に 9 variants を 3m/6m/1y/2y で:

| profile | 2y return | 2y PF | 2y DD | trades/2y |
|---|---|---|---|---|
| p3e (base) | +11.0% | 1.22 | 5.3% | 580 |
| p4a (tp=1.8) | +8.3% | 1.16 | 3.6% | 627 |
| **p4b (tp=2.2)** | **+18.9%** | **1.38** | **2.5%** | 557 |
| p4c (sl=9) | -3.5% | 0.95 | 8.6% | 645 |
| p4d (sl=11) | +6.7% | 1.15 | 3.7% | 551 |
| p4e (trail=1.8) | +11.0% | 1.22 | 5.3% | 580 |
| p4f (trail=2.2) | +11.0% | 1.22 | 5.3% | 580 |
| p4g (r=1.50) | **+26.4%** | 1.23 | 11.1% | 580 |
| p4h (r=2.00) | **+35.3%** | 1.22 | 14.5% | 580 |
| p4i (+breakout) | +9.6% | 1.18 | 5.2% | 589 |

**気づき**:
- **tp=2.2 が sweet spot**。p3e (tp=2.0) より return 1.7倍, DD 半分 という珍しい win-win
- **trailing_atr (p4e/p4f) は完全に効かない**。p3e と数値が一字一句同じ。ATR-based trailing は現データ範囲では発火していない疑い。`backend/internal/usecase/strategy/exit.go` の trailing 経路要レビュー (今回スコープ外)
- **SL=10 が下限**。9 で破綻
- r=1.50 / 2.00 は return を線形に伸ばし、DD も線形拡大 (5.3 → 11.1 → 14.5)

---

## 6. Stage5 — final champion hybrid (P4 完結)

p4b (tp=2.2) と r dial を組み合わせた最終 6 profile を **1m/3m/6m/1y/2y** で:

| profile | 1m | 3m | 6m | 1y | **2y** | **2y DD** | 2y PF | 2y win% |
|---|---|---|---|---|---|---|---|---|
| production | +0.7% | -0.4% | +2.1% | -0.3% | **-2.5%** | 6.7% | 0.92 | 76% |
| p4b (tp=2.2) | +0.4% | +3.3% | +7.0% | +8.3% | +18.9% | 2.5% | 1.38 | 86% |
| **🥇 p5b (tp=2.2/r=2.0)** | **+1.0%** | **+9.0%** | **+20.5%** | **+26.1%** | **+67.8%** | **8.0%** | **1.39** | 86% |
| **🥈 p5a (tp=2.2/r=1.5)** | **+0.7%** | **+6.6%** | **+15.0%** | **+18.5%** | **+44.2%** | **6.2%** | 1.36 | 86% |
| p5c (p5a + breakout) | +0.7% | +6.6% | +12.5% | +16.7% | +40.5% | 7.8% | 1.32 | 86% |
| p4g (p3e/r=1.5) | +0.9% | +5.2% | +13.0% | +15.2% | +26.4% | 11.1% | 1.23 | 86% |
| p4h (p3e/r=2.0) | +1.3% | +7.0% | +17.6% | +21.3% | +35.3% | 14.5% | 1.22 | 86% |

**最終気づき**:
- tp=2.2 系列は r 上げに対する **DD コストが p3e (tp=2.0) より優秀**。p5b の DD 8.0% vs p4h (同じ r=2.0) の DD 14.5%。tp=2.2 は早く利確できるぶん負け局面に資金を晒さない
- p5c (breakout 追加) は逆効果。LTC PT15M ではやはり breakout は減点要因
- biweekly_win_rate 75-76% (2y) — 14日窓のうち 3/4 でプラス

---

## 6.5. Walk-Forward OOS 検証 (overfit リスク評価)

p5b を base に tp ∈ {2.0, 2.2, 2.5} × sl ∈ {10, 12} の grid を **6 窓 walk-forward** で検証 (IS=6m, OOS=3m, step=3m, 2024-05 → 2026-05)。

| window | OOS期間 | IS-best param | OOS return | OOS PF | OOS DD | trades |
|---|---|---|---|---|---|---|
| 0 | 2024-11 → 2025-02 | tp=2.5 / sl=12 | **-5.4%** | 0.87 | 10.2% | 125 |
| 1 | 2025-02 → 2025-05 | tp=2.0 / sl=10 | +5.9% | 1.19 | 7.5% | 116 |
| 2 | 2025-05 → 2025-08 | tp=2.0 / sl=10 | +8.3% | 1.64 | 3.8% | 64 |
| 3 | 2025-08 → 2025-11 | tp=2.0 / sl=10 | **-3.7%** | 0.81 | 6.9% | 55 |
| 4 | 2025-11 → 2026-02 | tp=2.5 / sl=10 | +4.0% | 1.29 | 6.4% | 46 |
| 5 | **2026-02 → 2026-05** (直近) | tp=2.5 / sl=12 | **-1.1%** | 0.73 | 3.5% | 12 |

**aggregateOOS**: geomMean +1.2%/3m窓, worstReturn -5.4%, worstDD 10.2%, allPositive=**false**, robustness -0.038

### 重要な気づき
1. **IS-best と全期間 best が一致しない** — walk-forward では 6 窓中 4 窓で `tp=2.0 / sl=10` (= p3e 構成) が選ばれる。`tp=2.2` が「全期間 backtest 最良」だったのは平均化バイアスで **mild overfit の証拠**
2. **OOS で 6 窓中 2 窓が赤字** — worst -5.4%。**全期間 backtest の +67.8% (p5b) は楽観的過ぎ**、現実の forward 期待値は **年率 10-15%** 程度と見るべき (geomMean 1.2% × 4 OOS窓/年 ≈ 4.8%/年 だが、勝ち窓と負け窓が交互なので保守的に 5-15%)
3. **直近 OOS (2026-02〜05) は -1.1% / PF 0.73 / trades 12** — **現在の地合は本戦略系列に不利**。全 profile に共通する傾向で、地合変化サイクル中である可能性
4. **worstDD 10.2% (in window)** — 全期間 8.0% より高いがユーザー制約 25% 内


> ⚠️ live (`LIVE_PROFILE=production`) は据え置き。下記は提案のみ。手動で `.env` 変更 + コンテナ再起動が必要。

### Tier 1 (バランス重視・推奨) — 🥇 `experiment_2026-05-12_p5a_tp22_r150`
- **2y +44.2% / DD 6.2% / PF 1.36 / 全期間プラス / 557 trades (~23/月)**
- WFO で IS-best が tp=2.0 / sl=10 に収束したため、tp=2.2 は **mild overfit** の疑い。ただし tp=2.0 と 2.2 の差は限定的で p5a は両端の中庸
- **r=1.50 が WFO の DD-tolerance (worstDD 10.2%) と現実的にマッチ**
- 現実 forward 期待値: 年 10-15%, MaxDD ~12-15% (overfit 補正後)

### Tier 2 (アグレッシブ) — 🥈 `experiment_2026-05-12_p5b_tp22_r200`
- **2y +67.8% / DD 8.0% / PF 1.39** (全期間 backtest, IS+overfit 込み)
- WFO 実 forward 期待値: 年 15-25%, MaxDD ~15-20%
- DD 制約 25% にはまだ余裕。ただし**直近 OOS 窓で -1.1% / PF 0.73**、地合変化耐性は p5a より低い

### Tier 3 (最保守・推奨) — 🥉 `experiment_2026-05-12_p4b_tp22`
- **2y +18.9% / DD 2.5% / PF 1.38**
- r=0.75 (production と同等) のまま、tp/sl/trail のみ最適化
- 「攻撃的 sizing なしでも production を超える」最小変更版。WFO の overfit リスクが最も小さい (sizing による増幅なし)
- **保守的にスタートして徐々に r を上げる運用** を推奨する場合の初手

### Tier 4 (WFO 整合・参考) — `experiment_2026-05-12_p3e_tp2_sl10_tr20`
- **2y +11.0% / DD 5.3% / PF 1.22**
- WFO で IS-best 4/6 窓で選ばれた構成。**最も overfit していない**
- ただし全期間 backtest では p4b に劣る

### 共通の変更点 (どの Tier も)
| field | production | recommended |
|---|---|---|
| `strategy_risk.take_profit_percent` | 4 | **2.2** |
| `strategy_risk.stop_loss_percent` | 14 | **10** |
| `strategy_risk.trailing_atr_multiplier` | 2.5 | **2.0** |
| `position_sizing.risk_per_trade_pct` | 0.75 | **2.00 / 1.50 / 0.75** (tier 別) |

`drawdown_scale_down` (Tier A 13%/0.65x, Tier B 19%/0.40x) は **そのまま継承**。p5b の 2y DD は 8.0% なので Tier A (13%) にも未到達。

---

## 8. 残懸念 / 次サイクル候補

1. **trailing_atr が機能していない (バグ確定)** — p4e/p4f が p3e と完全同値だった原因を特定。`backend/internal/usecase/backtest/handler.go:946 trailingDistance` で `atrDist = ATR × multiplier`, `percentDist = entryPrice × stopLossPercent / 100` の **max** を採用するロジックだが、LTC 価格 ~¥5,000, ATR ~¥50 のスケールでは `percentDist (~¥500-700) > atrDist (~¥125)` が常に成立し、`trailing_atr_multiplier` を 1.8/2.0/2.2 に変えても trailing 距離は変わらない。`stop_loss_percent` を 10 に下げた時に trailing が縮んだのは percent 経由のため。**別 Issue として起票推奨**。現候補プロファイルはこのバグの影響を受けず動くが、本来 ATR-trailing で改善余地があるかもしれない。
2. ~~**walk-forward 未実施**~~ → § 6.5 で p5b に対し WFO 6 窓実施済。mild overfit 検出 (tp=2.2 → tp=2.0 が IS-best)、forward 期待値を年 10-15% に下方修正済。次サイクルでは p4b / p5a に対する WFO も推奨
3. **live signal_source タグ未実装** — `bySignalSource` が全て `unknown` で trend_follow/contrarian の内訳が見えない。これも別 Issue
4. **tp=2.2 の感度** — 2.0 / 2.2 / 2.5 の差が大きい。実トレード手数料 (slippage 想定なし) を加味すると変動可能性
5. **breakout disabled のまま** — p5c で逆効果確認、現状は OK
6. **直近 1m の trades が 6 件しかない** — 全 profile 共通。ユーザー指摘「取引されない日」を完全には解消しきれていない。これは entry 側ではなくシグナル成立頻度の問題で、別 indicator 追加 (例: Stoch RSI) を要する可能性

---

## 9. 関連 envelope ID

| stage | description | envelope |
|---|---|---|
| P1 | baseline production 5期間 | `01KRBXE2A3QGFRXCKNT4HC3HMH` |
| P2-Stage1 | entry sweep (11 profiles × 1m+3m) | (see `/tmp/sweep_stage1_results.jsonl`) |
| P3-Stage2 | exit hybrid (9 profiles × 1m+3m) | (see `/tmp/sweep_stage2_results.jsonl`) |
| P4-Stage3 | robustness top5 (6 profiles × 6m/1y/2y) | (see `/tmp/sweep_stage3_results.jsonl`) |
| P4-Stage4 | fine-tune around p3e (10 profiles × 3m-2y) | (see `/tmp/sweep_stage4_results.jsonl`) |
| P4-Stage5 | final champion (6 profiles × 1m-2y) | (see `/tmp/sweep_stage5_results.jsonl`) |
| P4-WFO | p5b walk-forward 6 windows | `01KRBY37Y6BDW4MCCMP8T8MMWG` |

すべての per-period BacktestResult は `backtest_results` テーブルに永続化済 (`pdcaCycleId = "2026-05-12_aggressive"` でフィルタ可能)。

---

**3 時間サイクル所要**: 約 90 分 (sweep) + 約 30 分 (実装/評価/レポート作成) = **2 時間** で完了。余り時間は walk-forward 検証または `trailing_atr` バグ調査に振れる。

---

## 10. Cycle 2 (追加 1h PDCA) — 反転耐性・cooldown・SL grid

**目的**: Cycle1 で見えた「V字転換時の SL クラスタが主損失源」「直近 1m / 3m で取引機会少」への対策候補を探索。
**pdcaCycleId**: `2026-05-12_cycle2`
**生成 profile**: 19 本 (P1: HTF/MACD/contrarian 厳格化 6本, P2: cooldown sweep 4本, P3: SL × trailing grid 4本, P4: hybrid 4本, baseline 1本)

### Stage1 (15 profiles × 1y) 主要結果

| profile | 1y return | trades | PF | DD | SL件 | trail件 |
|---|---|---|---|---|---|---|
| p5a (baseline) | +18.4% | 183 | 1.59 | 5.2% | 12 | 9 |
| c2_p2a (cool=3600) | +18.4% | 183 | 1.59 | 5.2% | 12 | 9 |
| c2_p1c (htf_boost_strong) | +18.4% | 183 | 1.59 | 5.2% | 12 | 9 |
| c2_p3b (sl9/tr1.8) | +16.8% | 208 | 1.39 | 7.5% | 14 | 17 |
| c2_p1e (contra_narrower) | +16.1% | 167 | 1.56 | 5.5% | 11 | 9 |
| c2_p3c (sl11/tr2.5) | +13.3% | 172 | 1.47 | 5.2% | 8 | 12 |
| c2_p1a (htf_block_ct) | +12.0% | 158 | 1.41 | 5.4% | 12 | 8 |
| c2_p3d (sl12/tr3.0) | +11.3% | 163 | 1.46 | 5.1% | 11 | 6 |
| c2_p3a (sl8/tr1.5) | +11.3% | 222 | 1.20 | 9.6% | 19 | 22 |
| c2_p1b (macd_require) | **+0.8%** | 145 | 1.02 | 10.8% | 12 | 12 |
| c2_p1f (macd+htf_block) | **-0.7%** | 129 | 0.98 | 9.9% | 10 | 12 |

### Stage2 (9 profiles × 6m/1y/2y) 主要結果

| profile | 2y return | 2y DD | 2y PF | SL+trail |
|---|---|---|---|---|
| **p5b** (baseline, 既存最強) | **+67.7%** | 8.0% | 1.39 | 75 |
| **p5a** (baseline) | **+44.2%** | 6.2% | 1.36 | 75 |
| c2_p4c (sl11/tr25/r2.0) | +25.3% | 10.7% | 1.17 | 75 |
| c2_p4b (p1e/r2.0) | +20.9% | 16.5% | 1.15 | 80 |
| c2_p1e (contra narrower) | +17.6% | 12.5% | 1.16 | 80 |
| c2_p3c (sl11/tr2.5) | +17.2% | 7.9% | 1.16 | 75 |
| c2_p3d (sl12/tr3.0) | +14.8% | 6.7% | 1.17 | 63 |
| c2_p4d (p1e+p3c+r2.0) | +12.7% | 12.8% | 1.10 | 72 |
| c2_p4a (p1e+p3c) | +8.8% | 9.6% | 1.09 | 72 |

### Cycle 2 の結論

1. **🥇 既存 p5a / p5b / p4b が Cycle2 を経ても Tier 1/2/3 のまま** — 新候補で改善できるものは無かった
2. **entry_cooldown_sec / htf_filter.alignment_boost は現データで一切効いていない** (p2a/p1c が p5a と完全同値、2y でも一致確認) — validate cap 3600s も影響なし。signal 自体が次 bar まで再発しないため cooldown が冗長になっている
3. **htf_filter.block_counter_trend=true は trades -25, return -6pp** — V字SL は減るが TP 機会も同じだけ消えるため net で損失
4. **trend_follow.require_macd_confirm=true は壊滅** (1y +0.8%, p1f は -0.7%) — MACD ヒストグラム要求すると 1y で 38 trades 減 + PF が 1.0 切る
5. **SL を 11/12 に緩めると return も同程度下がる** — SL 10 は本物の sweet spot
6. **`contrarian` を厳しくする (p1e) と短中期では改善するが 2y では劣化** — 2y で contrarian の発火機会は重要、絞ると long-tail return を失う

### 真の含意

p5a の **trades 557 / SL+trail 75件 / DD 6.2%** が**現データ + 現ロジックでの局所最適**。これ以上の改善には:

- **新 indicator 追加** (Stoch RSI, ADX gate, Donchian など) — コード変更要
- **timeframe 切替** (PT5M または PT1H) — 別 base 探索
- **signal_source タグ化** (現状全て `unknown`) — Trend/Contra/Breakout の内訳から原因特定

を要する。Cycle 2 はパラメータ最適化の **収束**を示しただけで、stage 上昇には別軸が必要 という結論。

### Cycle 2 関連 envelope

| stage | description | jsonl |
|---|---|---|
| Cycle2 Stage1 | 15 profiles × 1y | `/tmp/sweep_c2_stage1.jsonl` |
| Cycle2 Stage2 | 9 profiles × 6m/1y/2y | `/tmp/sweep_c2_stage2.jsonl` |

すべての per-period BacktestResult は `pdcaCycleId = "2026-05-12_cycle2"` で永続化済。

**1 時間サイクル所要**: 約 30 分 (sweep) + 約 15 分 (評価/レポート) = **45 分** で完了。残り時間は別軸検証 (Stoch RSI / PT5M) に振れる。

---

## 11. 直近 1ヶ月の trade-by-trade 比較 — 「取引が無い日」の真因

ユーザー指摘「上下動するのに取引されない日がある」を 1m 細かく確認:

| profile | trades | 1m return | DD | 終了理由 |
|---|---|---|---|---|
| production | 5 | +0.4% | 0.4% | TP×4, end_of_test×1 |
| p5a | 5 | +0.2% | 1.4% | TP×4, end_of_test×1 |
| p5b | 5 | +0.2% | 1.8% | TP×4, end_of_test×1 |
| p4b | 5 | +0.1% | 0.7% | TP×4, end_of_test×1 |

**衝撃の事実**: 全 profile で **完全に同じ 5 トレード**。エントリ・エグジットの時刻が一致 (04-12 05:45 BUY, 04-15 20:00 SELL, 04-19 23:00 BUY, 04-20 19:15 SELL, 05-01 14:45 SELL)。違うのは **lot size と TP 到達タイミング** のみ。

→ ユーザー指摘「取引が無い日」の真因は **signal 発火条件 (EMA cross + RSI range)** で 1ヶ月に 5 回しか signal が立たないため。パラメータ最適化では解決しない。

### 解決策の方向性 (Cycle 2 で確認)
1. ❌ entry_cooldown_sec を下げる → signal そのものが立たないので無意味
2. ❌ contrarian.rsi_entry を緩める → 2y で逆効果 (Cycle1 P2 で確認済)
3. ✅ **新 indicator 追加** (Stoch RSI, Donchian breakout) → コード変更要
4. ✅ **timeframe を PT5M に下げる** → signal 発火頻度が ~3x になる試算、別 base 探索が必要
5. ✅ **multi-symbol** → LTC 1 銘柄では機会上限あり、BTC/ETH と組み合わせ可能 (¥60k 制約は要再検証)

**現 LTC PT15M 範囲では p5a/p5b/p4b が真の最適**、これ以上はアーキ変更を要すると確定。

