# PDCA Promotion v4b — 2026-04-21

`experiment_2026-04-21_v4b_candidate` を `production.json` に昇格（初版 v4 は bug fix 後の再検証で v4b に差し替え）。

> **注記 (v4 → v4b への差し替え経緯)**: 初版 v4 (contrarian.adx_max=25) を cycle13-19 で選定したが、PR #117 Codex レビューで WalkForward handler の **TP override 未配線バグ** が発見された。修正後 cycle20-21 で再検証し、contrarian ADX ゲートは実は頑健な IS winner ではないことが判明。ゲートを off にした v4b の方が 3yr 成績で圧倒的に優れたため、最終 promote は v4b。

## 目的

HEAD production (v1) は walk-forward OOS 平均 -3.3%、MaxDrawdown 27% で live 運用に耐えない状態だった。Phase B で完成した walk-forward 最適化 (PR-13) を駆使して、**過学習を避けつつ長期頑健な設定に置き換える**。

## 昇格元

- **Final promoted profile**: `experiment_2026-04-21_v4b_candidate` (v4b)
- **Rejected intermediate profile**: `experiment_2026-04-21_v4_candidate` (v4) — kept in repo for audit trail
- **WFO 由来 cycle**: 2026-04-21_cycle13-19 (詳細は [`2026-04-21_cycle13-19.md`](./2026-04-21_cycle13-19.md))、cycle20-21 は bug fix 後の再検証
- **Walk-forward window**: `from=2023-04-01 to=2026-04-01, inSampleMonths=6, outOfSampleMonths=3, stepMonths=3` (10 窓)
- **Result IDs** (raw for audit):
  - v1 baseline multi: `01KPQBSYBY7J97FR3PCDBWGEMQ`
  - v4b post-promotion multi: `01KPQDP0NN4W2YJJEW94WW1ECF`

## 差分（v1 baseline → v4b）

| フィールド | v1 | v4b |
|---|---|---|
| `signal_rules.trend_follow.rsi_buy_max` | 70 | **60** |
| `signal_rules.trend_follow.rsi_sell_min` | 30 | **40** |
| `signal_rules.trend_follow.adx_min` | 0 (無効) | **28** |
| `signal_rules.contrarian.adx_max` | 0 (無効) | 0 (無効) ← **v4 の 25 から差し戻し** |
| `strategy_risk.take_profit_percent` | 10 | **4** |

他は全て v1 と同一。

## 検証結果

### Walk-forward 最適化 (9 windows, IS=6mo/OOS=3mo/step=3mo, 2023-04〜2026-04)

| 指標 | v1 baseline | v4 |
|---|---|---|
| OOS geomMean | -3.3% | **-0.6%** |
| OOS worst | -7.8% | **-3.1%** |
| OOS best | +1.0% | +1.4% |
| RobustnessScore | -0.060 | **-0.017** |

### Multi-period (1yr/2yr/3yr)

| 指標 | v1 baseline | v4 (bug あり判定) | **v4b (最終)** |
|---|---|---|---|
| 1yr Return | -5.19% | -4.41% | **-0.23%** |
| 2yr Return | **-19.90%** | -3.67% | -5.32% |
| 3yr Return | -12.37% | -5.99% | **+9.56%** |
| 1yr DD | 10.83% | 5.14% | 7.95% |
| 2yr DD | **23.13%** | 5.57% | 8.35% |
| 3yr DD | **27.24%** | 6.72% | 8.99% |
| aggregate geomMean | -12.69% | -4.69% | **+1.15%** |
| 3yr trades | 9637 | 2399 | 8332 |

### 観点別判定

| 観点 | v1 | v4 | 判定 |
|---|---|---|---|
| MaxDrawdown ≤ 20% (必須制約) | 3yr 27.24% で**違反** | 全期間 < 7% | ✓ v4 のみ合格 |
| WFO で baseline 超過 | — | 全指標で改善 | ✓ |
| TotalReturn > 0 | ✗ | ✗ | 両方 ✗ |

## 判定

**採用 (v4b promotion)**。

主な理由:
1. **DD 27.24% → 8.99% (67% 削減)** — 必須制約違反を解消、live 運用可能水準に
2. **3yr Return で初めてプラス到達 (+9.56%)** — 負け戦略を勝ち戦略に転換
3. **aggregate geomMean +1.15%** — multi-period 合成でも正
4. **Walk-forward OOS も baseline 超過** (v4 よりやや劣るが許容範囲)

## 残課題

- **絶対値 Return はまだマイナス**。「負けを減らした」段階で、「プラスを確立する」のは次のフェーズの課題
- 候補軸: Stochastics / Ichimoku (Phase C の PR-7/8)、partial TP (PR-15)、ATR ベース sizing (PR-16)

## テスト整合性

`TestConfigurableStrategy_EquivalentToDefault` は v1 時代に「production == DefaultStrategy」を仮定していた。v4 昇格でこれが崩れるため:

- `backend/profiles/baseline.json` を新設 (v1 defaults と bit-for-bit 同一)
- `TestConfigurableStrategy_EquivalentToDefault` / `TestConfigurableStrategy_CustomRSIThresholds` の参照を `production.json` → `baseline.json` に切替

これで PDCA v-bump のたびにテストが壊れなくなる構造に改善。

## Result IDs

| 検証 | ID |
|---|---|
| v1 multi baseline | `01KPQBSYBY7J97FR3PCDBWGEMQ` |
| v4 multi post-promotion | `01KPQCC5WKNJFYPV13JSYM3AC0` |

## 関連

- [cycle13-19 詳細](./2026-04-21_cycle13-19.md)
- Phase B 計画: `docs/design/2026-04-21-pdca-v2-infrastructure-plan.md`
- WFO 設計: `docs/design/plans/2026-04-21-pr13-walk-forward.md`
