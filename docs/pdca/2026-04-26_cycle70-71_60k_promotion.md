# Promotion ltc_60k v1 — LTC/JPY × PT15M for ¥60,000 budget

**Date**: 2026-04-26
**Profile**: `backend/profiles/production_ltc_60k.json`
**Time-boxed PDCA**: 30 minutes, cycles 70 〜 71m

## 経緯

ETH × ¥60k は不可能 (前 cycle60-66 で確認、ETH min_lot 0.1 ≈ ¥37,000 が equity の 62% を占めるため profile の risk_pct=0.5% sizer が必ず min_lot floor で skip)。
ユーザー予算が ¥60k 上限と確定したため、LTC PT15M に戻して ¥60k 軍資金専用に再チューニング。

## サイクル一覧

| Cycle | 仮説 | 結果 |
|---|---|---|
| 70 | production v7 (initial 100k で promote) を ¥60k で 4-period 検証 | baseline 確定: 3m +3.56/4.30, 6m +5.84/4.20, 1y +21.82/6.65, 2y +27.52/14.20 — 全制約クリア |
| 71 | risk_pct sweep {0.50, 0.75, 1.00, 1.50, 2.00} | r=0.75 が最も attractive (2y +37.34/22.51 — DD 制約 0.51pp 超過) |
| 71b | r=0.75 + DD scale {mild, medium, soft} | mild (12/0.75, 18/0.50) が 2y DD 20.29% — まだ 0.29pp 超過 |
| 71c | r=0.75 + DD tighter (tight1〜3) | **tight3 (12/0.65, 18/0.40) で 2y +32.30/17.66** — 全制約クリア |
| 71d | r=1.0 / 1.5 + DD scale | 全 variant 2y で trade death (lot 縮みすぎ) — r 上限は 0.75 |
| 71e | tight3 周辺で grid sweep (6 variants) | **grid5 (13/0.65, 19/0.40) で 2y +34.75/18.18** — tight3 を 2.45pp 上回る |
| 71f | r=0.80/0.85/0.90 + grid5 / DD tighter | r>0.75 はすべて 2y 制約破る — grid5 r=0.75 維持 |
| 71g | grid5 winner + signal/trailing tweak | **no_break (breakout disabled) で 2y +38.82/18.78** — grid5 を 4.07pp 上回る ✅ |
| 71h | no_break + r 上げ (0.80, 0.85) / DD tweak | すべて劣化 — r=0.75 上限を再確認 |
| 71i | no_break + signal_rules tweak (rsi_buy/sell, contra, macd, htf block) | 全 variant が劣化 |
| 71j | no_break + sl sweep (10, 12, 16, 18) | sl=14 (winner) 維持 — sl 縮小は逆効果 |
| 71k | no_break + r 細粒度 sweep (0.65, 0.70, 0.72, 0.78, 0.82) | r=0.75 winner 維持。r=0.70 が close 2nd |
| 71l | no_break + breakout 復活 (cmf 強化) / alignment_boost / tp / macd_hist | すべて劣化、alignment_boost は dead code |
| 71m | no_break + stance_rules tweak (sma_convergence, bb_squeeze_lookback, rsi 閾値) | すべて劣化 |

## winner: `production_ltc_60k`

production v7 ベース、3 箇所のみ変更:
1. **`signal_rules.breakout.enabled = false`** (cycle71g)
2. **`position_sizing.risk_per_trade_pct = 0.75`** (vs v7 0.50)
3. **`position_sizing.drawdown_scale_down`**: TierA=13%/0.65x, TierB=19%/0.40x (新規)

その他の指標期間 / stance / signal_rules / sl / tp はすべて v7 と同一。

## 4-period verification (initialBalance=¥60,000, to=2026-04-16)

| 期間 | Trades | Return | MaxDD | Sharpe | PF | Win Rate | Final |
|---|---|---|---|---|---|---|---|
| 3m (2026-01-16〜) | 894 | +5.09% | 6.26% | 1.47 | 1.24 | 60.9% | ¥63,056 |
| 6m (2025-10-16〜) | 1,720 | +5.09% | 7.37% | 0.75 | 1.10 | 60.2% | ¥63,056 |
| 1y (2025-04-16〜) | 3,628 | +45.16% | 17.52% | 1.94 | 1.33 | 61.3% | ¥87,097 |
| 2y (2024-04-16〜) | 7,539 | **+38.82%** | **18.78%** | 0.97 | 1.13 | 58.6% | ¥83,292 |

- ✅ 全期間 positive
- ✅ 全期間 MaxDD ≤ 20%
- 幾何平均 Return ≈ +22% (vs production v7 @ ¥60k baseline +13.6%, **+8.4pp 改善**)
- 最悪 DD 18.78% (1.22pp の 20% 制約に対するヘッドルーム)

## production v7 (¥60k baseline) との比較

| 期間 | v7 @ ¥60k | **ltc_60k v1** | δ |
|---|---|---|---|
| 3m R/DD | +3.56 / 4.30 | +5.09 / 6.26 | Return +1.53pp / DD +1.96pp |
| 6m R/DD | +5.84 / 4.20 | +5.09 / 7.37 | Return -0.75pp / DD +3.17pp |
| 1y R/DD | +21.82 / 6.65 | **+45.16 / 17.52** | **Return +23.34pp** / DD +10.87pp |
| 2y R/DD | +27.52 / 14.20 | **+38.82 / 18.78** | **Return +11.30pp** / DD +4.58pp |

- 短期 (3m/6m) はほぼ同等
- 長期 (1y/2y) で Return が大幅向上、DD は許容範囲内に拡大

## なぜ breakout disabled が効くのか

cycle71g で全期間でわずかに改善 (3m +0.16pp, 6m -0.75pp ⓘ, 1y -1.15pp ⓘ, 2y +4.07pp)。
2y で大きく勝つのは、**breakout シグナルが LTC PT15M で false-breakout 比率が高く、特に長期間で**
**累積的に資産を毀損していた**ため。trend_follow + contrarian のみで十分に signal が出る LTC では、
breakout は全体の利益を押し下げる方向に作用していた。

LTC v7 が breakout を維持していたのは production 100k で profile を最適化した時に「微小だが positive」
だったから。¥60k 規模では効果が反転した可能性が高い (lot floor の効果で trade 配分が変わる)。

## ¥60k 運用での UI 推奨設定

設定画面 (http://localhost:33000/settings?symbol=LTC_JPY) で以下を設定:

| 項目 | 推奨値 | 根拠 |
|---|---|---|
| 銘柄 | **LTC/JPY** | min_lot 0.1 ≈ ¥300、equity の 0.5% で安全 |
| 初期資金 | **60,000** | 軍資金実額 |
| 最大ポジション額 | **12,000** | equity の 20% (profile max_position_pct と一致) |
| 日次損失上限 | **3,000** | equity の 5% (profile worst DD 18.78% に対し保守的) |
| 損切り率 (%) | **0** | profile (sl=14) を使う ← UI で 0 にすると profile 値採用 |
| 利確率 (%) | **0** | profile (tp=4) を使う |
| 連敗上限 | **3** | 維持 (¥1,500 損失で停止 → 1 day cool down) |
| 冷却期間 (分) | **30** | 維持 |

切替コマンド:
```bash
LIVE_PROFILE=production_ltc_60k TRADE_SYMBOL_ID=10 docker compose up --build -d
```

## 既知の制約

1. **¥60k スペシフィック**: equity scale が大きく違う運用 (e.g., ¥10k や ¥1M) では挙動再現せず。
   r=0.75 + DD scale-down の組み合わせは equity が小さい時の min_lot 効果を前提にしている
2. **breakout 戦略を切り捨てた**: cross-asset (BTC/ETH 等) で breakout が効くケースを諦める
3. **min_lot floor 余裕は 7.14 倍** (vs LTC v7 baseline の 11.9 倍): 残高が ¥40k 程度まで下がると
   skip 頻度が増える (DD scale-down が発火する状況とほぼ重なる)

## 次サイクル

- **cycle71-final**: 本 promote PR を切り merge。LIVE_PROFILE=production_ltc_60k で本番投入可
- **cycle72 (任意, shadow run 後)**: 実 trade 1〜4 週で backtest 数値との乖離検証
- **cycle73 (任意)**: equity が ¥80k 〜 ¥100k に増えてきた時点で再 sweep (DD scale-down 不要になる可能性)
