# Promotion v7 — Dynamic Position Sizing (risk_pct = 0.50)

**Date**: 2026-04-24
**Promoted profile source**: `experiment_2026-04-24_sizing_r050`
**Supersedes**: v6 (signal logic unchanged; adds `position_sizing` block)
**Rationale source**: PR-B sizing sweep (cycle60+)

## 変更差分（v6 → v7）

v6 の戦略ロジック（indicators / stance_rules / signal_rules / htf_filter）はすべて不変。`strategy_risk` に `position_sizing` ブロックを追加:

```json
"position_sizing": {
  "mode": "risk_pct",
  "risk_per_trade_pct": 0.50,
  "max_position_pct_of_equity": 20,
  "min_lot": 0.1,
  "lot_step": 0.1
}
```

### 意味
- **mode=risk_pct**: ロット = equity × risk_per_trade_pct / (entry_price × stop_loss_percent)
- **risk_per_trade_pct=0.50**: 1取引あたり口座残高の 0.5% をリスクに晒す（SL到達で0.5%損失）
- **max_position_pct_of_equity=20**: 1建玉の想定元本が口座の20%を超えないように上限キャップ
- **min_lot / lot_step = 0.1**: 楽天ウォレット証拠金取引のLTC最小発注単位（0.1 LTC）
- DD scale-down は無効（r=0.5では20%制約内に収まるため不要）

## sizing sweep の結果（LTC/JPY 4期間, 基準日 2026-04-16）

| r | 3m Ret | 6m Ret | 1y Ret | 2y Ret | gM | 最悪DD | 20%制約 |
|---|---|---|---|---|---|---|---|
| 0.0 (v6 fixed) | +1.18% | +2.44% | +11.08% | +15.23% | +7.32% | 6.53% | ✅ |
| 0.25 | +2.27% | +3.53% | +12.01% | +7.56% | +6.27% | 9.15% | ✅ |
| **0.50** | **+4.30%** | **+7.69%** | **+25.85%** | **+26.34%** | **+15.60%** | **15.58%** | **✅ (採用)** |
| 0.75 | +6.34% | +9.67% | +46.71% | +41.31% | +24.70% | **24.48%** | ❌ |
| 1.00 | +8.08% | +15.71% | +64.71% | +0.22% | +19.87% | **22.97%** | ❌ |

### DD scale-down 実験（r=0.75 / 1.0 を 20% 制約内に押し込めるか）

| Profile | gM | 最悪DD | allPositive |
|---|---|---|---|
| r=0.75 + DD(10%=0.5x, 15%=0.25x) | +11.36% | 17.51% | **❌ (2y -10.11%)** |
| r=1.00 + DD(8%=0.5x, 12%=0.25x) | +9.98% | 23.89% | **❌ (2y -6.37%)** |

→ DD scale を入れると制約は満たしやすくなるが、**2yが大きなDDから回復できずマイナス**に転落。r=0.50 の方が単純かつ堅牢。

## v6 vs v7 の比較（本番採用候補）

| 指標 | v6 (fixed) | v7 (r=0.50) | 差分 |
|---|---|---|---|
| 3m Return | +1.18% | +4.30% | **+3.12pp** |
| 6m Return | +2.44% | +7.69% | **+5.25pp** |
| 1y Return | +11.08% | +25.85% | **+14.77pp** |
| 2y Return | +15.23% | +26.34% | **+11.11pp** |
| 幾何平均 Return | +7.32% | +15.60% | **2.13x** |
| 最悪 MaxDD | 6.53% | 15.58% | +9.05pp (まだ制約内) |
| allPositive | true | true | 維持 |
| RobustnessScore | +0.0144 | +0.0547 | **3.80x** |

### 結果 envelope ID
- v6 (production) LTC multi: `01KQ03ZNAEB75NEVB1JGX3T39R`
- v7 (candidate, experiment_2026-04-24_sizing_r050) LTC multi: `01KQ04004RX2TE54SES5SQ0AT6`

### 判定
- **全4期間プラス**（allPositive=true）
- **全4期間で MaxDrawdown ≤ 20% 制約を満たす**
- **幾何平均 Return が 2.13 倍** に向上
- **RobustnessScore も 3.80 倍** に向上
- sweep で r=0.75/1.0 がすべて失格、r=0.25 は利益が伸びず、**r=0.50 が local optimum**

→ 主目的（TotalReturn 最大化）で大幅上回り、副目的（制約遵守）も満たすため **昇格**。

## 既知の制約 (v6から引き継ぎ)

- **2022 bear regime**: 2022-01..2025-01 で -57% 級の破綻（cycle28-37 で確認）。サイジングは攻撃側の wedge のみで防御性能は変わらない
- **LTC/JPY 以外の資産には転用不可**: cycle40 で LTC 固定の parameter set であることを確定
- **BTC/JPY 側での同等効果は未検証**: BTC は venue lot が 0.01 なので別 profile 必要。本 PR では LTC のみ

## 動的サイジングの挙動

残高が増えると取引量が自動的に増える。具体例:

- 残高 10万円, LTC 12,000円, SL=14% → リスク予算 500円, SL距離 1,680円 → **base lot 0.29 LTC → lot_step 丸め 0.2 LTC**
- 残高が 20万円になると → base lot 0.59 LTC → **0.5 LTC**
- 残高が 50万円になると → base lot 1.49 LTC → **1.4 LTC**
- 残高が 100万円になると → base lot 2.98 LTC → max_position 20%キャップ ((100万*0.2)/12000 = 16.67) は効かず、**2.9 LTC**

実取引ではこれが live pipeline でも同じ計算式で適用される（`positionsize.Sizer` が backtest と live で共有）。

## ロールバック手順

問題が発生した場合:

```bash
git revert <この PR の merge commit>
# または
git checkout HEAD~1 backend/profiles/production.json
```

v6 の fixed sizing 相当は profile JSON の `position_sizing` ブロックを消すだけで復元可能（後方互換は sizer 側で保証済）。v6 の完全な JSON は v6 昇格 PR #152 のcommit で取得可能。

## 監視項目

- 初日: 約定ロットサイズが 0.1 の倍数で刻まれているか（`lot_step=0.1` が効いているか）
- 1週間: 最大建玉額が初期残高の20%を超えていないか（`max_position_pct_of_equity` が効いているか）
- 1ヶ月: 実際の MaxDD が 20% を超えていないか（超えそうなら DD scale-down を追加する判断）
