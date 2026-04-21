# PDCA Promotion v4 — 2026-04-21

`experiment_2026-04-21_v4_candidate` を `production.json` に昇格。

## 目的

HEAD production (v1) は walk-forward OOS 平均 -3.3%、MaxDrawdown 27% で live 運用に耐えない状態だった。Phase B で完成した walk-forward 最適化 (PR-13) を駆使して、**過学習を避けつつ長期頑健な設定に置き換える**。

## 昇格元

- **Profile**: `experiment_2026-04-21_v4_candidate`
- **WFO 由来 cycle**: 2026-04-21_cycle13-19 (詳細は [`2026-04-21_cycle13-19.md`](./2026-04-21_cycle13-19.md))

## 差分（v1 baseline → v4）

| フィールド | v1 | v4 |
|---|---|---|
| `signal_rules.trend_follow.rsi_buy_max` | 70 | **60** |
| `signal_rules.trend_follow.rsi_sell_min` | 30 | **40** |
| `signal_rules.trend_follow.adx_min` | 0 (無効) | **28** |
| `signal_rules.contrarian.adx_max` | 0 (無効) | **25** |
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

| 指標 | v1 baseline | v4 |
|---|---|---|
| 1yr Return | -5.19% | -4.41% |
| 2yr Return | **-19.90%** | **-3.67%** |
| 3yr Return | -12.37% | -5.99% |
| 1yr DD | 10.83% | **5.14%** |
| 2yr DD | **23.13%** | **5.57%** |
| 3yr DD | **27.24%** | **6.72%** |

### 観点別判定

| 観点 | v1 | v4 | 判定 |
|---|---|---|---|
| MaxDrawdown ≤ 20% (必須制約) | 3yr 27.24% で**違反** | 全期間 < 7% | ✓ v4 のみ合格 |
| WFO で baseline 超過 | — | 全指標で改善 | ✓ |
| TotalReturn > 0 | ✗ | ✗ | 両方 ✗ |

## 判定

**採用 (v4 promotion)**。

主な理由:
1. **DD 27.24% → 6.72% (75% 削減)** — 必須制約違反を解消、live 運用可能水準に
2. **Walk-forward OOS で明確に baseline 超過** — 過学習ではなく真の改善
3. **2yr Return -19.9% → -3.7% (16pt 改善)** — 長期頑健性が根本的に違う

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
