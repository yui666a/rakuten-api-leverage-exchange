# PDCA Promotion v5 — 2026-04-22

`experiment_2026-04-22_sl14_tf60_35` を `production.json` に昇格（v4b → v5、aggressive 選択）。

> **v4b 時点の前提との違い**: v4b (2026-04-21) は `rsi_oversold=25 / overbought=75`, `adx_min=28` を持つ trend_follow 優先の保守的設定で gM +1.15% に留まっていた。v5 は cycle28-37 の 15 分 PDCA スプリントで探索した `healthy_v3` 系譜の最良点。SL=14 / TP=4 / trailing_atr=2.5 / `rsi_oversold=32` / `block_counter_trend=false` / `require_macd_confirm=false` と複数軸が同時に動いているため、「v4b から 1 フィールドだけ差分」では到達できない。

## 目的

v4b は multi-period で初めて正の 3yr リターンを達成したが `1yr -0.23%` / `2yr -5.32%` のため近年データで依然マイナス、aggregate geomMean は +1.15% と攻撃力不足。「過学習のリスクを負ってでも、直近 3 年の実績を live に繋ぐ」ことを目的に、`healthy_v3` 系譜で探索した最良点を昇格させる。

## 昇格元

- **Promoted profile**: `experiment_2026-04-22_sl14_tf60_35`（攻撃候補）
- **Retained alternative**: `experiment_2026-04-22_sl6_tr30_tp6_tf60_35`（防御候補）— 2022 bear が再来したときの即時 rollback 用に profile として保持
- **由来 cycle**: `docs/pdca/2026-04-22_cycle28-37.md`（cycle28-37 スプリント）
- **Selection cycle**: Cycle 33 (SL plateau 13..18 確認) → Cycle 31-32 (`tf60_35` = rsi_oversold 32 / rsi_sell_min 35 が load-bearing)
- **Extended validation result ID**: `01KPSCWD0NNM315D8F3QJJQXP2`

## 差分（v4b → v5）

| フィールド | v4b | v5 (sl14) |
|---|---:|---:|
| `stance_rules.rsi_oversold` | 25 | **32** |
| `stance_rules.rsi_overbought` | 75 | **68** |
| `stance_rules.sma_convergence_threshold` | 0.001 | **0.002** |
| `signal_rules.trend_follow.require_macd_confirm` | true | **false** |
| `signal_rules.trend_follow.rsi_sell_min` | 40 | **35** |
| `signal_rules.trend_follow.adx_min` | 28 | **(削除)** |
| `signal_rules.contrarian.rsi_entry` | 30 | **32** |
| `signal_rules.contrarian.rsi_exit` | 70 | **68** |
| `signal_rules.contrarian.adx_max` | 0 (無効) | **(削除)** |
| `signal_rules.breakout.adx_min` | 0 (無効) | **(削除)** |
| `strategy_risk.stop_loss_percent` | 5 | **14** |
| `strategy_risk.take_profit_percent` | 4 | 4 |
| `strategy_risk.trailing_atr_multiplier` | 0 | **2.5** |
| `htf_filter.block_counter_trend` | true | **false** |

## 検証結果

### Multi-period on `2023-04..2026-03`（cycle33 SL plateau）

| 指標 | v4b | **v5 (sl14)** |
|---|---:|---:|
| 1yr Return (2025-04..2026-03) | -0.23% | **+9.26%** |
| 2yr Return | -5.32% | **+12.23%** |
| 3yr Return (new3yr) | +9.56% | **+28.03%** |
| 1yr DD | 7.95% | — |
| 2yr DD | 8.35% | — |
| 3yr DD | 8.99% | **6.01%** |
| aggregate geomMean | +1.15% | **+16.22%** |
| worst period | -5.32% | **+9.26%** |

全ての 1yr/2yr/3yr ウィンドウでプラス、DD は 20% 制約内（3yr 6.01%）、geomMean は v4b の 14 倍。

### Old 3yr regime sanity check（cycle35）

| profile | new3yr (2023-04..2026-03) | **old3yr (2022-01..2025-01)** |
|---|---:|---:|
| **v5 sl14_tf60_35** | +28.03% / DD 6.01% | **−57.97% / DD 91.06%** 🚨 |
| v4b current production | +9.56% / DD 8.99% | (未測定、別系譜のため要検証) |
| 防御代替 sl6_tr30_tp6_tf60_35 | +14.69% / DD 15.57% | +28.30% / DD 24.51% |

**v5 は 2022 年水準の bear 相場で −57.97% (DD 91%) の破綻が確認されている**。これは「過去 3 年が未来の regime を代表する」という仮定に基づく承認済みリスク。

### 観点別判定

| 観点 | v4b | **v5 (sl14)** | 判定 |
|---|---|---|---|
| MaxDrawdown ≤ 20%（new3yr）| 8.99% | 6.01% | ✓ 改善 |
| WFO で baseline 超過 | — | Cycle34 で WFO robust は sl14_tf60_**40** (2/3 windows)、v5 は 1/3 windows で curve-fit リスクあり | ⚠ 受容 |
| TotalReturn > 0 on 1yr/2yr/3yr | 1yr ✗ / 2yr ✗ / 3yr ✓ | 全て ✓ | ✓ 改善 |
| 2022 bear で破綻しない | 未検証 | **−57.97% 破綻** | ✗ **明示的に受容** |

## 判定

**採用 (v5 promotion, aggressive 選択)**。

主な採用理由:
1. **全 1yr/2yr/3yr 期間でプラス** — v4b の近年マイナスを解消
2. **geomMean +1.15% → +16.22% (14x)** — 攻撃力の飛躍的改善
3. **3yr DD 8.99% → 6.01%** — 20% 制約をより安全側に
4. **SL=14 は SL plateau 13..18 の下端** — plateau 内なので 1 点の curve-fit ではない

明示的に受容するリスク:
1. **2022 級 bear 相場の再来時、−57.97% の資金毀損**
2. WFO Cycle 34 では `sl14_tf60_40` が 2/3 windows 勝者で、`sl14_tf60_35` (本 v5) は 1/3 window (最新 w2) のみ勝者 → 直近 6 ヶ月への curve-fit リスクあり

## 運用前提（Known Limitations）

1. **2022 regime 監視**: BTC/LTC が直近 3 ヶ月で −30% 超の drawdown を示した場合、**v5 運用を即時停止して防御候補 `experiment_2026-04-22_sl6_tr30_tp6_tf60_35` に切り替える** 判断を人間が下す。自動検知は未実装（cycle40 で regime routing は LTC 15m で構造的に有効化不可と判明済み）。
2. **Max daily loss kill-switch** (`max_daily_loss=50000`) は現状の設定を維持するが、v5 は SL=14 なので 1 トレード当たりの損失が v4b (SL=5) の約 2.8 倍になる点に留意。
3. **3 ヶ月スプリント運用後のレビュー必須**: 2026-07 までに実運用結果と backtest 予測の乖離を再評価し、継続 / v6 探索を決める。

## Rollback Plan

### 即時（防御候補 sl6 に切替）
```bash
cp backend/profiles/experiment_2026-04-22_sl6_tr30_tp6_tf60_35.json backend/profiles/production.json
# name / description を production に書き換え
docker compose up --build -d backend
```

### 完全巻き戻し（v4b に戻す）
```bash
git revert <v5 promotion commit>
# または
git show <v4b commit>:backend/profiles/production.json > backend/profiles/production.json
docker compose up --build -d backend
```

## テスト整合性

v4b promotion で `baseline.json` が既に分離済み（`TestConfigurableStrategy_EquivalentToDefault` は baseline.json を参照）のため、**本 promotion では configurable_strategy 系不変条件テストは破壊されない**。

ただし `productionProfile()` を loader として使っているテスト（`configurable_strategy_test.go` の TestConfigurableStrategy_DisabledTrendFollow 等）は v5 の数値変更により失敗する可能性があるため、`go test ./...` の結果をもって個別に確認・更新する。

## Result IDs

| 検証 | ID |
|---|---|
| v5 (sl14) extended multi-period | `01KPSCWD0NNM315D8F3QJJQXP2` |
| defensive alt (sl6) multi-period | `01KPSD1ZSAMP2Y54DVN6D4F2G0` |
| WFO robust pick (sl14_tf60_40) | `01KPSD3MTYA4AGGD0HMF64YX4P` |
| v4b baseline multi (reference) | `01KPQDP0NN4W2YJJEW94WW1ECF` |

## 関連

- [cycle28-37 詳細](./2026-04-22_cycle28-37.md)
- [v4b promotion](./2026-04-21_promotion_v4.md)
- [2026-04-22 セッション handoff](./2026-04-22_handoff.md)
- Phase B 計画: `docs/design/2026-04-21-pdca-v2-infrastructure-plan.md`
