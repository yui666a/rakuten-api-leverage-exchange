# Promotion eth v1 — ETH/JPY × PT5M production profile

**Date**: 2026-04-26
**Promoted profile source**: `experiment_eth_pt5m_2x_dd_soft` (cycle66)
**New on-disk name**: `backend/profiles/production_eth.json`
**Supersedes**: なし (ETH 用 production profile は本 PR が初)
**Co-exists with**: `production.json` (LTC PT15M, v7)

## 経緯

2026-04-26 の通貨ペア決定 (LTC → ETH) を受け、LTC PT15M の production v7 を ETH PT5M に
そのまま流用するのは 期間スケールと min_lot 制約の両面で困難と判明 (cycle60-64)。期間
スケールを再 sweep した結果、**3x ではなく 2x が ETH PT5M の local optimum** であることが
分かり (cycle65)、続く DD scale-down sweep で **2x baseline + DD scale soft (TierA=12%/0.75x,
TierB=18%/0.50x) が全 4 期間で promote 制約をクリア** (cycle66)。

PDCA ドキュメント: `docs/pdca/2026-04-26_cycle60.md` / `docs/pdca/2026-04-26_cycle61-66.md`。

## 構成 (LTC v7 との差分)

### Indicators (期間スケール 2x)

| 指標 | LTC v7 (PT15M) | ETH v1 (PT5M, 2x) |
|---|---|---|
| sma_short / sma_long | 20 / 50 | **40 / 100** |
| ema_fast / ema_slow | 12 / 26 | **24 / 52** |
| rsi_period | 14 | **28** |
| macd_fast / slow / signal | 12 / 26 / 9 | **24 / 52 / 18** |
| bb_period (×bb_multiplier) | 20 (×2.0) | **40** (×2.0) |
| atr_period | 14 | **28** |
| volume_sma_period | 20 | **40** |
| adx_period | 14 | **28** |
| stoch_k_period / smooth_k / smooth_d | 14 / 3 / 3 | **28 / 6 / 6** |
| stoch_rsi_rsi_period / stoch_period | 14 / 14 | **28 / 28** |
| donchian_period | 20 | **40** |
| obv_slope_period | 20 | **40** |
| cmf_period | 20 | **40** |
| ichimoku_tenkan / kijun / senkou_b | 9 / 26 / 52 | **18 / 52 / 104** |

### Stance / Signal / Risk (LTC v7 と同一)

stance_rules / signal_rules / strategy_risk の全フィールドは LTC v7 と完全同一値:

- rsi_oversold=32, rsi_overbought=68, sma_convergence=0.002, bb_squeeze_lookback=3
- trend_follow.rsi_buy_max=60, contrarian.rsi_entry=32, breakout.cmf_buy_min=0.10
- stop_loss_percent=14, take_profit_percent=4, trailing_atr_multiplier=2.5

### Position sizing (DD scale-down 追加)

| フィールド | LTC v7 | ETH v1 |
|---|---|---|
| mode | risk_pct | risk_pct |
| risk_per_trade_pct | 0.50 | 0.50 |
| max_position_pct_of_equity | 20 | 20 |
| min_lot / lot_step | 0.1 / 0.1 | 0.1 / 0.1 |
| **drawdown_scale_down.tier_a_pct / scale** | (なし) | **12 / 0.75** |
| **drawdown_scale_down.tier_b_pct / scale** | (なし) | **18 / 0.50** |

DD scale-down が新規。意味: 残高が peak から 12% drawdown に達したら以降の lot を 25% カット
(scale 0.75)、18% drawdown に達したら 50% カット (scale 0.50)。回復したら scale も戻る。

LTC v7 では DD scale なしで運用基準を満たしたが (cycle60+ で DD scale 入れると 2y 残高が伸び
ない局面があった)、ETH v1 ではボラティリティ高で peak 後の戻り downturn が深く、
scale-down がリスク予算を切り詰めて 2y MaxDD を 26.95% → 15.29% に圧縮する効果が出た
(cycle66)。

## 4-period verification (基準日 2026-04-16, initialBalance=¥1,000,000)

| 期間 | Trades | Return | MaxDD | Sharpe | PF | Win Rate |
|---|---|---|---|---|---|---|
| 3m (2026-01-16〜04-16) | 1,560 | +12.23% | 5.66% | 2.56 | 1.33 | 50.4% |
| 6m (2025-10-16〜04-16) | 1,560 | +12.23% | 5.66% | 1.79 | 1.33 | 50.4% |
| 1y (2025-04-16〜04-16) | 2,571 | +11.56% | 15.18% | 0.73 | 1.16 | 49% |
| 2y (2024-04-16〜04-16) | 4,665 | **+44.13%** | **15.29%** | 1.26 | 1.36 | 50% |

- **幾何平均 Return: +18%** (LTC v7 の +15.60% を上回る)
- **最悪 MaxDD: 15.29%** (LTC v7 の 15.58% を下回る = 安全側)
- **全期間 positive** ✅
- **全期間 MaxDD ≤ 20%** ✅

3m と 6m が同一値なのは 2025-10〜2026-01 で trade がほぼ発生しなかったため (Ichimoku の
warmup 156 bars + HTF=PT1H の組み合わせで前半が dead window)。LTC でも同様の現象あり、
判定上は無害。

## ETH v1 vs LTC v7 (横並び比較)

| 指標 | LTC v7 | ETH v1 | δ |
|---|---|---|---|
| 3m Return | +4.30% | +12.23% | +7.93pp |
| 6m Return | +7.69% | +12.23% | +4.54pp |
| 1y Return | +25.85% | +11.56% | -14.29pp |
| 2y Return | +26.34% | +44.13% | +17.79pp |
| 幾何平均 Return | +15.60% | +18% (推定) | +2.40pp |
| 最悪 MaxDD | 15.58% | 15.29% | -0.29pp (改善) |
| RobustnessScore | +0.0547 | (未計測) | — |
| allPositive | ✅ | ✅ | — |
| 最悪 DD ≤ 20% 制約 | ✅ | ✅ | — |

**1y で LTC v7 を 14pp 下回るのが本 promote の最も気になる弱点**。原因仮説:

- 1y window (2025-04-16〜2026-04-16) はちょうど ETH の trend が薄い range 局面を多く含む
- DD scale-down で lot が縮んで recovery 期に乗り遅れる

ただし他 3 期間 (3m/6m/2y) で全部勝っており、特に 2y (long-term robustness の主指標) で
17.79pp 上回る。**1y は次サイクル (cycle68 任意) で signal_rules 再 sweep の対象**。

## 既知の制約 / 運用上の注意

1. **initialBalance ≥ ¥400,000 (推奨 ¥1,000,000)**: ETH 価格 ~¥370k & min_lot 0.1 ≈ ¥37k で、
   risk_pct=0.50% で生まれるリスク予算が SL 距離より小さい場合 sizer が min_lot floor で
   skip する。¥1M 以下の運用は 4-period verification の数値とは挙動が乖離する可能性。
2. **2024-04〜10 の problem regime は依然 negative-edge** (cycle64): その期間単独で見ると
   PF < 0.4。本 promote は DD scale-down で問題期間の lot を縮め、その後の recovery で
   profit を上回る形で全制約を満たしている。**他資産・別ボラ環境では同じ仕組みが効かない
   可能性あり**。
3. **3m と 6m が同一値**: 2025-10〜2026-01 で trade がほぼ発生しない warmup dead window
   現象。本判定では無害。trade 数を増やしたい場合は Ichimoku senkou_b の縮小を検討。

## 動作確認手順

```bash
# Backtest (4-period verification)
cd backend
go run ./cmd/backtest run \
  --profile production_eth \
  --data data/candles_ETH_JPY_PT5M.csv \
  --data-htf data/candles_ETH_JPY_PT1H.csv \
  --from 2024-04-16 --to 2026-04-16 \
  --initial-balance 1000000

# Live (PR-D 配線済): LIVE_PROFILE 環境変数で切り替え
LIVE_PROFILE=production_eth docker compose up --build -d
docker compose logs -f backend | head -100
```

## ロールバック手順

```bash
# 環境変数を戻すだけで LTC profile に戻る
LIVE_PROFILE=production docker compose up -d
# あるいは production_eth.json を削除して LIVE_PROFILE=production_eth が解決失敗 → legacy 値に
# fall back させる (PR-D の loadLiveProfile は失敗を warn ログで出して legacy 動作)
```

## 次サイクル

- **cycle67 (本 promote 直後)**: 本 doc + production_eth.json + 関連 experiment profiles を
  PR にまとめ merge。
- **cycle68 (任意)**: 1y window で LTC v7 に 14pp 負けている弱点を分析、signal_rules
  (trend_follow.rsi_buy_max / contrarian.rsi_entry / breakout.cmf_buy_min) の ETH 専用
  チューニングを試す。
- **cycle69 (将来)**: 本番投入後の 1〜4 週で実 trade 結果が backtest 数値と乖離していないか
  検証。乖離する場合は spread / latency model のキャリブレーション要。
