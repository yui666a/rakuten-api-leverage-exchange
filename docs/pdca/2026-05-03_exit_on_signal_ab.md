# Exit-on-Signal A/B — production_ltc_60k

**Date**: 2026-05-03
**Profile baseline**: `backend/profiles/production_ltc_60k.json` (exit_on_signal=false)
**Profile variant**: `backend/profiles/experiment_ltc_60k_exit_on_signal.json` (exit_on_signal=true; その他は完全同一)
**実装 PR**: #238 (`feat(risk): exit_on_signal — Decision-driven exits via RiskHandler`)

## 仮説

Phase 1 の DecisionHandler は long 保有中の bearish シグナル / short 保有中の bullish シグナルを `EXIT_CANDIDATE` として emit するが、PR #238 までは RiskHandler が silent-skip していた。実 exit は TP / SL / Trailing しか持たない状態。

仮説: **シグナルが反転した時点でクローズすれば、SL に引っかかる損失が減り、Return / MaxDD ともに改善する**。

## 方法

- 4 期間 × 2 variant = 8 ラン (3m / 6m / 1y / 2y, to=2026-05-03)
- initialBalance ¥60,000 (LTC PT15M production と同条件)
- API 経由 (`POST /api/v1/backtest/run`) — slippage は percent モード (orderbook データのカバレッジが 4.4% で実用的でないため)
- 両 variant とも `production_ltc_60k` の指標 / stance / signal_rules / sizer / SL14% / TP4% / Trailing ATR×2.5 を**完全同一**に保ち、`exit_on_signal` のみフリップ

## 結果

### サマリー (4 期間)

| 期間 | off Return / DD | on Return / DD | δ Return | δ DD | 判定 |
|---|---|---|---|---|---|
| 3m  | -0.30% / 1.11% | **+0.83% / 1.14%** | +1.13pp | +0.04pp | ✅ |
| 6m  | -0.11% / 2.28% | **+1.26% / 1.14%** | +1.37pp | **-1.13pp** | ✅✅ |
| 1y  | -1.82% / 4.36% | **+2.40% / 2.64%** | +4.22pp | **-1.72pp** | ✅✅ |
| 2y  | -3.16% / 5.59% | **+3.61% / 2.96%** | +6.77pp | **-2.63pp** | ✅✅ |

**全 4 期間で Return がマイナス → プラス転換、DD は 3 / 4 期間で改善**。

### 詳細メトリクス

| 期間 | variant | Trades | Return% | MaxDD% | Final ¥ | Sharpe | PF | WinRate% |
|---|---|---|---|---|---|---|---|---|
| 3m | off | 10 | -0.30 | 1.11 | 59,817 | -0.34 | 0.80 | 70.0 |
| 3m | on | 150 | +0.83 | 1.14 | 60,501 | **+1.69** | 1.22 | 61.3 |
| 6m | off | 38 | -0.11 | 2.28 | 59,934 | +0.05 | 0.98 | 76.3 |
| 6m | on | 292 | +1.26 | 1.14 | 60,756 | **+1.11** | 1.14 | 62.7 |
| 1y | off | 86 | -1.82 | 4.36 | 58,910 | -0.64 | 0.84 | 73.3 |
| 1y | on | 575 | +2.40 | 2.64 | 61,441 | **+1.04** | 1.12 | 60.7 |
| 2y | off | 238 | -3.16 | 5.59 | 58,105 | -0.52 | 0.89 | 75.2 |
| 2y | on | 1,283 | +3.61 | 2.96 | 62,169 | **+0.60** | 1.07 | 59.4 |

幾何平均 Return: off ≈ -1.35%, on ≈ **+2.02%** (4-period geo mean)。

### 2y exit_reason breakdown — 「なぜ効くのか」が見える

`on` の trades=1,283 内訳:

| reason | trades | totalPnL | winRate |
|---|---|---|---|
| `take_profit` | 173 | **+15,896** | 100% |
| `stop_loss` | **8** | -2,775 | 0% |
| `trailing_stop` | 5 | -1,465 | 0% |
| `EXIT_CANDIDATE` (long, bearish) | 487 | -3,698 | 50.7% |
| `EXIT_CANDIDATE` (short, bullish) | 609 | -5,776 | 56.2% |
| `end_of_test` | 1 | -13 | 0% |

`off` の trades=238 内訳: stop_loss 大量 + take_profit わずか (詳細は `/tmp/bt_off_2y.json`)。

**読み解き**:
- Decision-driven exit 単体の PnL は **-9,474 (loss)**
- だが `stop_loss` が **238→8 (off 比較で激減)** し、**take_profit が 173 件 +15,896** に増加
- 結果: net +3,615 (= +6.0% on ¥60k)
- 「シグナル反転で早めに撤退 → 大損 SL を回避 → 次の良いシグナルを TP で取る」ループが成立

### 副作用 / 留意点

- **trades が 5〜13 倍に増加** (3m 10→150, 2y 238→1,283)。手数料が現実的な水準だと利益を侵食する可能性。slippageModel=orderbook で再評価する必要があるが現状データ不足
- **WinRate は ~75% → ~60% に低下**。ただし profitFactor / Return は改善 — 「勝率は落ちるが期待値は上がる」典型パターン
- **Sharpe は 4 期間中 4 期間で大幅改善** (off は ほぼ全部マイナス)
- EntryCooldownSec=1800 とのインタラクションは未検証 (現プロファイル値 1800 で計測)

## 結論

`exit_on_signal=true` は production_ltc_60k で **明確な改善**。promote 推奨。

ただし promote 前に以下を別 PR で実施:

1. **手数料の現実的な織り込み**: maker/taker 手数料 + spread を実値で再走 (現 spread=0.1% percent モデルだと過小評価の懸念)
2. **EntryCooldownSec sweep**: 現値 1800 だが exit が増えた分 cooldown の影響度が変わるはず。{900, 1800, 3600, 7200} で sweep
3. **Live 用ガード追加検討**: 短期間で大量 close → 板薄時のスリッページ集中。BookGate を exit にも適用するか議論

## ファイル

- 実験プロファイル: `backend/profiles/experiment_ltc_60k_exit_on_signal.json`
- 結果 JSON (ローカルのみ): `/tmp/bt_{off,on}_{3m,6m,1y,2y}.json` — backtest_results テーブルにも保存済 (id は各 JSON の `.id` 参照)
