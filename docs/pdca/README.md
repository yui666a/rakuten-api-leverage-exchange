# PDCA Strategy Optimizer — 運用ガイド

LTC/JPY 向けの戦略最適化サイクル記録ディレクトリ。Claude Code セッション内で PDCA を回す際の一次情報源。

## 目的

- **MaxDrawdown 20% 以下** の制約のもと **Total Return 最大化**。
- 2 週間スライド勝率の平均 **80%** を目標に、副目的として追跡。
- 本番戦略（`production.json`）とは独立した実験プロファイルで仮説検証 → 良いものだけ手動で本番昇格。

仕様の詳細は [`docs/superpowers/specs/2026-04-16-pdca-strategy-optimizer-design.md`](../superpowers/specs/2026-04-16-pdca-strategy-optimizer-design.md) を参照。
エージェントがこの機能を使うときの実践ガイド・使用例・ヒント・アンチパターンは [`docs/pdca/agent-guide.md`](./agent-guide.md) を参照。

## ディレクトリ構成

```
docs/pdca/
├── README.md                       ← このファイル
├── _template.md                    ← サイクル記録テンプレート
├── 2026-04-16_cycle01.md           ← 実サイクル記録（命名規約: YYYY-MM-DD_cycleNN.md）
└── ...
```

プロファイル JSON は別ツリー:

```
backend/profiles/
├── production.json                 ← 本番戦略（現行ロジックの literal 再現）
└── experiment_YYYY-MM-DD_NN.json   ← PDCA 実験プロファイル
```

## PDCA サイクルの進め方

### Plan

1. `backend/profiles/production.json` と直近の実験結果（`docs/pdca/` 既存サイクル）を読む。
2. 改善仮説を立てる（どのパラメータ・条件・指標を、なぜ、どう変えると良くなるか）。

### Do

1. `backend/profiles/experiment_YYYY-MM-DD_NN.json` を作成（`production.json` を起点にコピーして差分適用）。
2. （Level 3 のみ）必要な指標を `backend/internal/infrastructure/indicator/` と `IndicatorSet` に追加。
3. CLI でバックテスト実行:
   ```bash
   cd backend
   go run ./cmd/backtest run \
     --profile experiment_YYYY-MM-DD_NN \
     --data data/candles_LTC_JPY_PT15M.csv \
     --data-htf data/candles_LTC_JPY_PT1H.csv \
     --pdca-cycle-id YYYY-MM-DD_cycleNN \
     --hypothesis "仮説の要約"
   ```
   - `--profile` はファイル名のみ（拡張子なし）。プロファイルの値を起点とし、個別フラグ（`--stop-loss` 等）で明示指定した値のみオーバーライドされる。
   - `--data` は必須、`--data-htf` / `--from` / `--to` は任意（プロファイルではなく CLI 引数で指定）。再現性のためこのコマンドをサイクル記録に転記する。

### Check

1. 出力された `BacktestSummary` を前回と比較。
   - **必須制約**: `MaxDrawdown` ≤ 20%。超過なら即 reject。
   - **主目的**: `TotalReturn` の最大化。
   - **副目的**: `BiweeklyWinRate` → 80% を目標。
   - **参考**: `SharpeRatio`, `ProfitFactor`, `WinRate`。
2. `BiweeklyWinRate` は 2 週間スライドウィンドウの平均（0–100 スケール、0=信頼不可）。
   - ウィンドウ内トレード数 < 3 件はペナルティ（そのウィンドウ値を 0 に、ただし分母に残す）。
   - カバレッジ率（≥3 件ウィンドウの割合）< 50% の場合は全体 0 を返す。

### Act

1. 改善 → 次サイクルのベースラインに採用、同系の仮説を深掘り。
2. 悪化 → ロールバック（プロファイル削除 or 差分破棄）。
3. 頭打ち → 次のレベルへエスカレーション（Level 1 パラメータ → Level 2 ロジック組替 → Level 3 新指標）。
4. サイクル記録 `docs/pdca/YYYY-MM-DD_cycleNN.md` を `_template.md` から作成し結果を記録。
5. 親子関係を辿れるよう、APIで`parentResultId` に前サイクルの result ID を指定して保存する。

## バックテスト結果の確認

- **単発 一覧 API**: `GET /api/v1/backtest/results?profileName=experiment_...&pdcaCycleId=YYYY-MM-DD_cycleNN`
- **複数期間 一覧 API**（PR-2）: `GET /api/v1/backtest/multi-results?profileName=...&pdcaCycleId=...` ― 1 ラン ＝ N 期間の envelope。`RobustnessScore` で頑健性を一発比較。
- **Walk-Forward 一覧 API**（PR-13 + #120）: `GET /api/v1/backtest/walk-forward?baseProfile=...&pdcaCycleId=...` ― IS 窓で grid 探索 → OOS で検証した envelope。
- **フロント**:
  - `/backtest` ― 単発結果一覧 + PR-1 breakdown / PR-3 drawdown・time-in-market・expectancy を含む詳細パネル。
  - `/backtest-multi` ― 複数期間ランキング（RobustnessScore 降順）＋ 期間別サマリ詳細。
  - `/walk-forward` ― WFO ランキング + 窓別 OOS Return チャート + **Best Parameter 頻度表**（≥60% の窓で選ばれたパラメータは robust 判定）。

## 段階的エスカレーション

| サイクル | レベル | 内容 | 例 |
|---|---|---|---|
| 1〜3 | Level 1: パラメータ | 数値の調整 | RSI閾値、SMA期間、SL/TP% |
| 4〜6 | Level 2: 条件組替 | ロジック構造の変更 | MACD確認を外す、BB Squeeze厳格化 |
| 7〜 | Level 3: 新指標 | Go コード追加 | ADX(PR-6)、Stochastics(PR-7)、Ichimoku(PR-8) 等（既に実装済） |

頭打ちになったら次のレベルに上がる。

## Walk-Forward で過学習を排除する

単発サイクルの IS 結果だけで promotion するのは過学習リスクが高い。v4 以降は **WFO を必須の gate にする**。

```bash
# CLI（推奨）
cd backend
go run ./cmd/backtest walk-forward \
  --profile production \
  --data data/candles_LTC_JPY_PT15M.csv \
  --from 2022-01-01 --to 2025-01-01 --in 12 --oos 6 --step 6 \
  --grid "signal_rules.contrarian.stoch_entry_max=0,15,25" \
  --output docs/pdca/wfo-YYYY-MM-DD.json

# API
curl -X POST http://localhost:38080/api/v1/backtest/walk-forward -H 'Content-Type: application/json' -d @- <<'JSON'
{ "data": "...", "from": "2022-01-01", "to": "2025-01-01",
  "inSampleMonths": 12, "outOfSampleMonths": 6, "stepMonths": 6,
  "baseProfile": "production", "objective": "return",
  "parameterGrid": [ { "path": "signal_rules.contrarian.stoch_entry_max", "values": [0, 15, 25] } ],
  "pdcaCycleId": "YYYY-MM-DD_cycleNN" }
JSON
```

判定ルール:

- **IS best 頻度 ≥ 6/10 窓**（または 3/4 窓）で robust と見なす。
- 全窓の OOS Return が負でないこと（一つでも深い負なら reject）。
- `aggregateOOS.robustnessScore` が baseline より高いこと。

## 本番昇格

- 手動オペレーション（自動化なし）。
- 実験プロファイルが十分な期間・条件で本番を上回ることを確認 → `backend/profiles/production.json` を上書き → コミット → 再デプロイ。
