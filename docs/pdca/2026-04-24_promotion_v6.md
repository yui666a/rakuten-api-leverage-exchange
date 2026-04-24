# Promotion v6 — `experiment_2026-04-22_sl14_bblk3_cmf010` → production

**Date**: 2026-04-24
**Promoted profile**: `experiment_2026-04-22_sl14_bblk3_cmf010`
**Supersedes**: v5 (`experiment_2026-04-22_sl14_tf60_35`, promoted 2026-04-22)
**Rationale source**: cycle45-58, 10連続reject後のLocal optimum確定

## 変更差分（v5 → v6）

| パラメータ | v5 | v6 (昇格) | 由来 |
|---|---|---|---|
| `stance_rules.bb_squeeze_lookback` | 5 | **3** | cycle44 winner |
| `signal_rules.breakout.cmf_buy_min` | なし | **0.10** | cycle42 borderline |

他のパラメータは v5 と同一。

## 検証結果 — LTC/JPY 4期間（基準日 2026-04-16）

`initialBalance=100,000`, `tradeAmount=0.1`, `spread=0.1%`, `carry=0.04%/day` で再計測:

| 期間 | production (v5) | experiment (v6) |
|---|---|---|
| 3m | +1.44% / WR 60.26% / DD 1.36% | +1.18% / WR 59.45% / DD 1.30% |
| 6m | +2.66% / WR 59.50% / DD 2.48% | +2.44% / WR 59.10% / DD 2.45% |
| 1y | +10.97% / WR 60.20% / DD 4.17% | +11.08% / WR 60.56% / DD 4.17% |
| 2y | +13.08% / WR 57.06% / DD 6.85% | **+15.23%** / WR 57.49% / DD 6.53% |
| **幾何平均Return** | +6.92% | **+7.32%** |
| **最悪MaxDD** | 6.85% | 6.53% |
| **allPositive** | true | true |

### 結果 envelope ID

- v5 (production) LTC multi: `01KQ00CRCHVAHERXQX4QY8EYVM`
- v6 (experiment) LTC multi: `01KQ00DGWAFV5Q7RYGYKCYQ6CK`

### 判定

- **全4期間プラス** (allPositive=true)
- **全4期間で MaxDrawdown ≤ 20% 制約を満たす**
- **幾何平均Return が v5 を上回る** (+7.32% > +6.92%)
- **最悪期 MaxDD も v5 を下回る** (6.53% < 6.85%)
- 10サイクル連続で超えるプロファイルが現れず、Local optimum 確定

→ 主目的（TotalReturn最大化）で上回り、副目的（制約遵守）も満たすため **昇格**。

## クロスアセット検証 — BTC/JPY 4期間

| 期間 | production (v5) | experiment (v6) |
|---|---|---|
| 3m | +3.97% / DD 20.21% | +13.20% / DD 14.59% |
| 6m | +25.07% / DD 18.12% | +34.97% / DD 23.66% |
| 1y | +18.04% / DD 19.52% | +25.88% / DD 25.16% |
| 2y | **-35.75%** / DD **79.36%** ❌ | +26.46% / DD 48.98% ❌ |
| 幾何平均Return | -0.34% | **+24.88%** |
| RobustnessScore | -0.239 | +0.171 |

### 結果 envelope ID

- v5 BTC multi: `01KQ00E7W5EAVNQ1HCDREKBMQ9`
- v6 BTC multi: `01KQ00EW6X9DFFQ9BRS2VCSFXH`

### 注意

- **BTC/JPY では v6 も MaxDD 20% 制約を超過** (3m=14.6%のみ OK、6m以降はNG)
- **BTC/JPY での本番運用は不可**。本 profile は **LTC/JPY 固定** で使用する
- cross-asset generalization は将来の PR-14（asset-aware profile routing）で対応

## 既知の制約

- **2022 bear regime**: 2022-01..2025-01 期間で -57.97% に破綻（cycle28-37 で確認）
- **LTC/JPY 以外の資産への転用不可**: 戦略パラメータが LTC の mean-reversion 性向・ボラ幅・出来高スケールにチューニングされている
- **regime routing 無効**: cycle40 で LTC 15m regime detector が構造的に bull/bear-trend を emit しないことが確定。現行 profile は single-regime 仮定のまま

## ロールバック手順

問題が発生した場合は:

```bash
git revert <この PR の merge commit>
# または
git checkout HEAD~1 backend/profiles/production.json
```

v5 のフル設定は `experiment_2026-04-22_sl14_tf60_35.json` に保全されている（削除しない）。
