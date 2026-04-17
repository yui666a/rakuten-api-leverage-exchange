# PDCA Cycle NN — YYYY-MM-DD

- **Profile**: `experiment_YYYY-MM-DD_NN`
- **Parent result ID**: `<前サイクルの backtest_results.id / 初回はなし>`
- **Level**: Level 1 / 2 / 3

## 実行条件

- データ: `data/candles_LTC_JPY_PT15M.csv`
- HTF データ: `data/candles_LTC_JPY_PT1H.csv`
- 期間: YYYY-MM-DD ～ YYYY-MM-DD
- 実行コマンド:

```bash
cd backend
go run ./cmd/backtest run \
  --profile experiment_YYYY-MM-DD_NN \
  --data data/candles_LTC_JPY_PT15M.csv \
  --data-htf data/candles_LTC_JPY_PT1H.csv \
  --pdca-cycle-id YYYY-MM-DD_cycleNN \
  --hypothesis "仮説の要約"
```

## 仮説

（何をどう変えるか、なぜ改善すると考えるか）

## 変更内容

- プロファイル: `backend/profiles/experiment_YYYY-MM-DD_NN.json`
- 変更パラメータ:
  - `indicators.<field>`: before → after
  - `signal_rules.<...>`: before → after

## 結果

| 指標 | before | after | 判定 |
|---|---|---|---|
| Total Return | | | |
| MaxDrawdown | | | |
| BiweeklyWinRate (2 週間勝率) | | | |
| SharpeRatio | | | |
| WinRate | | | |
| ProfitFactor | | | |
| TotalTrades | | | |

## 判定

採用 / ロールバック / 部分改善

- 必須制約 `MaxDrawdown ≤ 20%`: ✓ / ✗
- 主目的 TotalReturn 改善: ✓ / ✗
- 副目的 BiweeklyWinRate 改善: ✓ / ✗

## 学び

（次のサイクルに活かす知見・気づき。頭打ち兆候があれば次レベルへのエスカレーション判断もここに書く）
