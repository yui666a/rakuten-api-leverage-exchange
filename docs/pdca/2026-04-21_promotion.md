# PDCA Promotion — 2026-04-21

cycle11 の設定を `production.json` に昇格。

## 昇格元

- **Profile**: `experiment_2026-04-21_11`
- **Cycle**: `2026-04-21_cycle11`
- **Best Result ID (6mo)**: `01KPNRFD5EYPM419PN213XEBQM`

## 差分（旧 production → 新 production）

| フィールド | 旧 | 新 |
|---|---|---|
| `stance_rules.rsi_oversold` | 25 | **30** |
| `stance_rules.rsi_overbought` | 75 | **70** |
| `signal_rules.trend_follow.rsi_buy_max` | 70 | **65** |
| `signal_rules.trend_follow.rsi_sell_min` | 30 | **35** |

他は全て同一。

## 検証結果

### 6ヶ月 (2025-10-01 〜 2026-03-31)

| 指標 | 旧 production | 新 production (=cycle11) |
|---|---|---|
| Total Return | -0.371 % | **+1.080 %** |
| MaxDrawdown | 2.38 % | 1.33 % |
| BiweeklyWinRate | 50.63 | 57.67 |
| WinRate | 53.36 % | 60.17 % |
| ProfitFactor | 0.971 | 1.090 |
| Sharpe | -0.189 | +0.639 |

### 1年 (2025-04-01 〜 2026-03-31) — 昇格前検証

| 指標 | 旧 production | 新 production | 判定 |
|---|---|---|---|
| Total Return | -5.256 % | **-0.561 %** | ✓ +4.7pt |
| MaxDrawdown | 10.83 % | 9.42 % | ✓ 20% 制約内 |
| BiweeklyWinRate | 50.74 | 55.11 | ✓ +4.4 |
| WinRate | 53.32 % | 57.37 % | ✓ +4.0 |
| ProfitFactor | 0.844 | 0.983 | ✓ +0.14 |
| Sharpe | -0.759 | -0.056 | ✓ |
| Validation Result ID (旧) | `01KPNRRJMGQAFR8S31EGA0150Z` | — |
| Validation Result ID (新) | — | `01KPNRRNSC0Z6KKNRK3D9FN6VG` |

### 昇格後の post-check (6mo)

- Result ID: 昇格後の production で再実行し +1.080% / DD 1.33% / PF 1.090 / Trades 1,205 を確認。cycle11 と完全一致。

## 注意点

- **1年の絶対値はまだマイナス (-0.56%)**。旧 production (-5.26%) と比較して明確な優位性があるため昇格するが、「勝てる戦略」ではなくまだ「負けを減らした戦略」の段階。継続して PDCA を回す前提。
- 本番昇格は compose.yaml のバインドマウント (`./backend/profiles:/app/backend/profiles:ro`) 経由で即時反映。再ビルド不要。
- 次サイクルは新 production をベースに Level 2（条件組替）に移る。Level 1 パラメータ調整は cycle12 で頭打ち確認済み。

## 関連

- cycle01 (旧 baseline): [`2026-04-21_cycle01.md`](./2026-04-21_cycle01.md)
- cycle11 (昇格元): [`2026-04-21_cycle11.md`](./2026-04-21_cycle11.md)
- compose.yaml バインドマウント追加により PDCA 回転速度が大幅改善（再ビルド不要化）
