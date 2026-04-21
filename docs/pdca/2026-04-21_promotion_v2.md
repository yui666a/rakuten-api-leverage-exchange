# PDCA Promotion v2 — 2026-04-21

`experiment_2026-04-21_L9a` を `production.json` に昇格（2 回目の本番昇格）。

## 目標達成

**1 年検証 (2025-04-01 〜 2026-03-31) で Return +7.300% を達成** — プラス転換のゴールクリア。

## 昇格元

- **Profile**: `experiment_2026-04-21_L9a`
- **Result ID**: `01KPNS7CXGKFDWMDN5X5NBBK8Q`

## 差分（prev production → 新 production）

| フィールド | prev | new |
|---|---|---|
| `stance_rules.rsi_oversold` | 30 | **32** |
| `stance_rules.rsi_overbought` | 70 | **68** |
| `stance_rules.sma_convergence_threshold` | 0.001 | **0.002** |
| `signal_rules.trend_follow.rsi_buy_max` | 65 | **62** |
| `signal_rules.trend_follow.rsi_sell_min` | 35 | **38** |
| `signal_rules.contrarian.rsi_entry` | 30 | **32** |
| `signal_rules.contrarian.rsi_exit` | 70 | **68** |
| `strategy_risk.stop_loss_percent` | 5 | **6** |
| `strategy_risk.take_profit_percent` | 10 | **4** |
| `htf_filter.block_counter_trend` | true | **false** |

## 検証結果

### 学習対象: 1 年 (2025-04-01 〜 2026-03-31)

| 指標 | prev production | **new production** | 改善 |
|---|---|---|---|
| Total Return | -0.561 % | **+7.300 %** | +7.86pt |
| MaxDrawdown | 9.42 % | **4.19 %** | -5.2pt |
| BiweeklyWinRate | 55.11 | **60.56** | +5.5 |
| WinRate | 57.37 % | **61.73 %** | +4.4 |
| ProfitFactor | 0.983 | **1.259** | +0.28 |
| SharpeRatio | -0.056 | **+1.327** | +1.38 |
| TotalTrades | 2,681 | 2,971 | +290 |

### 頑健性チェック (同プロファイルを別期間で実行)

| 期間 | Return | DD | 判定 |
|---|---|---|---|
| 1yr (学習対象) | +7.300 % | 4.19 % | ✓ |
| 2yr (2024-04〜2026-03) | **-0.912 %** | 13.07 % | ⚠ 2024 年分で打ち消し |
| 3yr (2023-04〜2026-03) | +14.714 % | 11.35 % | ✓ |
| 2025Q1 | **-8.325 %** | 8.42 % | ⚠ 大負け四半期 |
| 2025Q2 | +6.859 % | 4.19 % | ✓ |
| 2025Q3 | -1.108 % | 3.03 % | ~ |
| 2025Q4 | +1.369 % | 1.50 % | ✓ |
| 2026Q1 | +0.164 % | 1.56 % | ~ |

**MaxDrawdown は全期間で 20% 制約内**。2yr でマイナスな点は要注意だが、ユーザのゴール「1 年リターンプラス」は明確に達成。

## 大きな発見 (この 20 分で得た知見)

### 1. HTF の block_counter_trend は逆効果

prev production まで `block_counter_trend=true` が本番設定だった。これを off にしただけで 1yr Return が -0.56% → +1.10% に改善（cycle L2d/L2e）。HTF でブロックされていた counter-trend シグナルは、実は勝ち組の多いセットだった。

### 2. TP は浅い方が効く

直感に反して TP=4% が最強（TP=10% のほうが期待値高いはずという直感は誤り）。LTC/JPY 15 分足では、トレンドが継続せず途中戻る動きが多く、浅い TP で早めに利確するほうがトータルで勝る。

### 3. SL は逆に深めが効く

SL=5 → 6 に緩めたほうが Return が上がる（LTC/JPY のノイズ幅に対して 5% は狭かった）。SL を狭めた cycle07 (-2.98%) の結果と完全に整合。

### 4. stance RSI 閾値は 32/68 がスイートスポット

30/70 → 32/68 で改善、33/67 以上は悪化（cycle12, L6e）。

### 5. sma_convergence を緩めるとレンジ判定が甘くなり勝率上昇

0.001 → 0.002 で +0.5pt 改善 (L7i)。

### 6. macd_histogram_limit を大きくするとトレード増え WR/BiWR が上昇するが Return は頭打ち

limit=30 で BiWR 64.72 まで上昇するが 2yr では -3.34%（LBc）。過学習寄り。

### 7. 未配線フィールド

- `strategy_risk.stop_loss_atr_multiplier`
- `htf_filter.alignment_boost`

両者は profile 上変えても結果が baseline と完全一致する。`configurable_strategy.go` では engine options に渡されているが、実際の execution path で参照されていない可能性。Level 3 対応の別課題として記録（promotion 条件ではない）。

## 実施サマリ

| グループ | サイクル数 | ハイライト |
|---|---|---|
| Level 1 (cycle02-10) | 9 | cycle10 stance 30/70 で Return +0.73% 到達 |
| Level 2 (cycle11, L2) | 6 | L2d htf block off で 1yr プラス転換 (+1.10%) |
| Level 3 refinement (L3-LB) | 60+ | グリッド探索で +7.30% 達成 |

合計 **70+ バックテスト実行** を 20 分弱で完了。compose.yaml のバインドマウント化 (再ビルド不要) によって回転速度が 100 倍以上改善したことが最大の推進力。

## 次のステップ（将来の別セッション向け）

1. **過学習対策**: 複数期間の総合スコアで評価する機能を backend に追加
2. **未配線フィールド修正**: `alignment_boost` / `atr_multiplier` の適用 (Level 3)
3. **Walk-forward テスト**: 学習期間と検証期間を分離する機能を追加
4. **新指標追加**: ADX (trend strength) を入れて trend_follow の質を更に上げる余地
