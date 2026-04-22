# API Reference

ベース URL: `http://localhost:38080/api/v1`

## Bot 制御

### GET /status

Bot の状態を取得する。

```json
{
  "balance": 10000,
  "dailyLoss": 0,
  "manuallyStopped": false,
  "status": "running",
  "totalPosition": 0,
  "tradingHalted": false
}
```

### POST /start

自動売買パイプラインを開始する。

### POST /stop

自動売買パイプラインを停止する。

## 市場データ

### GET /ticker?symbolId=7

現在価格（ティッカー）を取得する。

```json
{
  "symbolId": 7,
  "bestAsk": 11500100,
  "bestBid": 11499900,
  "last": 11500000,
  "high": 11600000,
  "low": 11400000,
  "volume": 20.14,
  "timestamp": 1775825698324
}
```

### GET /indicators/:symbol?interval=PT15M

テクニカル指標を取得する。`:symbol` は symbolId（例: `7`）、`interval` はデフォルト `PT15M`。

データ不足で計算できない指標は `null`。

```json
{
  "symbolId": 7,
  "sma20": 11459941.1,
  "sma50": 11461099.9,
  "ema12": 11482453.58,
  "ema26": 11468867.49,
  "rsi14": 62.90,
  "macdLine": 13586.08,
  "signalLine": 7534.66,
  "histogram": 6051.42,
  "bbUpper": 11600000.0,
  "bbMiddle": 11459941.1,
  "bbLower": 11319882.2,
  "bbBandwidth": 0.0244,
  "atr14": 150000.0,
  "volumeSma20": 18.4,
  "volumeRatio": 1.12,
  "recentSqueeze": false,

  "adx14": 27.4,
  "plusDi14": 32.1,
  "minusDi14": 14.7,

  "stochK14_3": 72.3,
  "stochD14_3": 68.1,
  "stochRsi14": 82.4,

  "ichimoku": {
    "tenkan": 11500000,
    "kijun": 11450000,
    "senkouA": 11470000,
    "senkouB": 11400000,
    "chikou": 11500000
  },

  "timestamp": 1775825100000
}
```

- ADX (+DI/-DI) は Wilder smoothing, 期間 14。`2*period+1` 本未満で `null`。
- Stochastics は `(k=14, dSmooth=3, d=3)`、StochRSI は `(rsi=14, stoch=14)`。フラット窓は 50 を返す FE 互換仕様。
- Ichimoku は `(tenkan=9, kijun=26, senkouB=52)`。各ラインは warmup 不足で個別に欠ける。

### GET /candles/:symbol?interval=PT15M&limit=100

ローソク足データを取得する。

### GET /orderbook?symbolId=7

板情報を取得する。

### GET /symbols

取引可能なシンボル一覧を取得する。

### GET /ws

WebSocket 接続。リアルタイムで Ticker / Orderbook / Trades を受信する。

## 注文・ポジション

### POST /orders

注文を作成する。

リクエスト:
```json
{
  "symbolId": 7,
  "side": "BUY",
  "amount": 0.001,
  "orderType": "MARKET",
  "clientOrderId": "agent-20260414-120000-buy"
}
```

- `clientOrderId` は **必須**。一意な ID。同じ ID で再リクエストすると二重発注を防止する。
- 命名規則: `agent-YYYYMMDD-HHMMSS-side`

成功レスポンス:
```json
{
  "clientOrderId": "agent-20260414-120000-buy",
  "executed": true,
  "orderId": 12345,
  "reason": ""
}
```

リスクチェック拒否:
```json
{
  "clientOrderId": "agent-20260414-120000-buy",
  "executed": false,
  "orderId": 0,
  "reason": "risk rejected: daily loss limit exceeded"
}
```

重複リクエスト:
```json
{
  "clientOrderId": "agent-20260414-120000-buy",
  "duplicate": true,
  "executed": true,
  "orderId": 12345
}
```

### GET /positions?symbolId=7

ポジション一覧を取得する。

### POST /positions/:id/close

指定ポジションを決済する。

### GET /trades

約定履歴を取得する。

### GET /trades/all

全約定履歴を取得する。

## 戦略

### GET /strategy

現在の戦略方針を取得する。`stance` は `TREND_FOLLOW` / `CONTRARIAN` / `BREAKOUT` / `HOLD`。

```json
{
  "stance": "TREND_FOLLOW",
  "reasoning": "SMA trend detected",
  "source": "rule-based",
  "updatedAt": 1775826300
}
```

`source` が `"override"` の場合は `expiresAt` フィールドも含まれる。

### PUT /strategy

戦略方針をオーバーライドする。オーバーライド可能な stance は `TREND_FOLLOW` / `CONTRARIAN` / `HOLD` の 3 種。`BREAKOUT` はルールベース自動判定専用。

リクエスト:
```json
{
  "stance": "TREND_FOLLOW",
  "reasoning": "判断理由をここに書く",
  "ttlMinutes": 60
}
```

### DELETE /strategy/override

オーバーライドを解除し、ルールベース判定に戻す。

## 設定

### GET /config

リスク設定を取得する。

```json
{
  "maxPositionAmount": 5000,
  "maxDailyLoss": 5000,
  "stopLossPercent": 5,
  "takeProfitPercent": 10,
  "stopLossAtrMultiplier": 0,
  "trailingAtrMultiplier": 0,
  "initialCapital": 10000,
  "maxConsecutiveLosses": 0,
  "cooldownMinutes": 0
}
```

### PUT /config

リスク設定を更新する。

### GET /trading-config

取引設定（対象シンボル・取引金額）を取得する。

### PUT /trading-config

取引設定を更新する（シンボル切替含む）。

## 損益

### GET /pnl

損益情報を取得する。

```json
{
  "balance": 10000,
  "dailyLoss": 0,
  "totalPosition": 0,
  "tradingHalted": false,
  "dailyPnl": {
    "realized": 0,
    "unrealized": 0,
    "total": 0,
    "stale": false,
    "computedAt": 1775826300
  }
}
```

## バックテスト

### POST /backtest/run

単発バックテストを実行し、結果を保存して返す。

リクエスト:
```json
{
  "data": "data/candles_LTC_JPY_PT15M.csv",
  "dataHtf": "data/candles_LTC_JPY_PT1H.csv",
  "from": "2024-01-01",
  "to": "2025-01-01",
  "initialBalance": 100000,
  "spread": 0.1,
  "carryingCost": 0.04,
  "slippage": 0.0,
  "tradeAmount": 0.01,
  "stopLossPercent": 5,
  "stopLossAtrMultiplier": 0,
  "trailingAtrMultiplier": 0,
  "takeProfitPercent": 10,
  "profileName": "production",
  "pdcaCycleId": "2026-04-22_cycle22",
  "hypothesis": "baseline vs PR-7 Stoch gate",
  "parentResultId": null
}
```

- `data` は必須。`profileName` 指定時はプロファイル値がベースになり、**非ゼロ**の個別フィールドのみ上書き。
- `stopLossAtrMultiplier` / `trailingAtrMultiplier` は 0 で「percent-based にフォールバック」。> 0 で ATR based に切替。

レスポンスは `BacktestResult`（`summary` に PR-1/3 フィールドを含む）:

```json
{
  "id": "01KPRF...",
  "createdAt": 1761123456,
  "profileName": "production",
  "pdcaCycleId": "2026-04-22_cycle22",
  "config": { ... },
  "summary": {
    "totalReturn": 0.0942,
    "totalTrades": 310,
    "winRate": 54.2,
    "profitFactor": 1.18,
    "maxDrawdown": 0.089,
    "sharpeRatio": 0.74,
    "biweeklyWinRate": 62.3,

    "byExitReason": { "stop_loss": { "trades": 12, "totalPnL": -8400, "...": "..." } },
    "bySignalSource": { "trend_follow": { "trades": 180, "...": "..." } },

    "drawdownPeriods": [ { "fromTimestamp": ..., "toTimestamp": ..., "depth": 0.08, "durationBars": 180, "recoveryBars": 420, "recoveredAt": ... } ],
    "unrecoveredDrawdown": null,
    "drawdownThreshold": 0.02,

    "timeInMarketRatio": 0.62,
    "longestFlatStreakBars": 88,
    "expectancyPerTrade": 130.5,
    "avgWinJpy": 840.2,
    "avgLossJpy": 620.4
  },
  "trades": [ ... ]
}
```

### GET /backtest/results?limit=20&offset=0&profileName=...&pdcaCycleId=...&hasParent=true&parentResultId=...

保存済みバックテスト結果の一覧。PDCA フィルタ対応。

### GET /backtest/results/:id

指定 ID の結果（サマリ + トレード一覧 + PR-1/3 詳細）。未存在 ID は 404。

### GET /backtest/csv-meta?data=...

CSV メタ情報（symbol / interval / 行数 / from/to）を返す。

## 複数期間バックテスト（PR-2）

### POST /backtest/run-multi

同一プロファイルで複数期間を並列実行し、envelope と集約スコアを保存。

リクエスト: `POST /backtest/run` と同形に加えて `periods` を必須化。

```json
{
  "data": "...",
  "profileName": "production",
  "periods": [
    { "label": "1yr", "from": "2024-01-01", "to": "2025-01-01" },
    { "label": "2yr", "from": "2023-01-01", "to": "2025-01-01" },
    { "label": "3yr", "from": "2022-01-01", "to": "2025-01-01" }
  ]
}
```

レスポンス:

```json
{
  "id": "01KMP...",
  "profileName": "production",
  "periods": [ { "label": "1yr", "result": { "id": "...", "summary": { ... } } }, ... ],
  "aggregate": {
    "geomMeanReturn": 0.0115,
    "returnStdDev": 0.082,
    "worstReturn": -0.053,
    "bestReturn": 0.0956,
    "worstDrawdown": 0.089,
    "allPositive": false,
    "robustnessScore": -0.070
  }
}
```

- NaN / ±Inf（例: ruin path で `periodReturn ≤ -1.0`）は JSON `null` としてシリアライズ。
- 個別の per-period `BacktestResult` は `backtest_results` に保存済。envelope から `id` 参照でリハイドレート可能。

### GET /backtest/multi-results?limit=20&profileName=...&pdcaCycleId=...

複数期間ランの envelope 一覧（per-period 本体は含まない）。

### GET /backtest/multi-results/:id

envelope + per-period 本体を rehydrate して返す。

## Walk-Forward 最適化（PR-13 + #120）

### POST /backtest/walk-forward

In-sample 窓で grid 探索 → 勝者パラメータで Out-of-sample 測定を全窓で実行し、envelope を SQLite に永続化。

```json
{
  "data": "data/candles_LTC_JPY_PT15M.csv",
  "dataHtf": "data/candles_LTC_JPY_PT1H.csv",
  "from": "2022-01-01",
  "to": "2025-01-01",
  "inSampleMonths": 12,
  "outOfSampleMonths": 6,
  "stepMonths": 6,
  "baseProfile": "production",
  "objective": "return",
  "parameterGrid": [
    { "path": "signal_rules.contrarian.stoch_entry_max", "values": [0, 15, 25] },
    { "path": "strategy_risk.stop_loss_percent",        "values": [3, 5, 7] }
  ],
  "pdcaCycleId": "2026-04-22_cycle22",
  "hypothesis": "Stoch %K oversold gate"
}
```

- `objective` は `return` / `sharpe` / `profit_factor` / `""`（= return）。
- `parameterGrid` の `path` は `ApplyOverrides` が受け付けるパスのみ。未サポートは 400。
- grid サイズは最大 100 コンビネーション。

レスポンスは `WalkForwardResult`:

```json
{
  "id": "01KPRF...",
  "createdAt": 1761123456,
  "baseProfile": "production",
  "objective": "return",
  "pdcaCycleId": "2026-04-22_cycle22",
  "windows": [
    {
      "index": 0,
      "inSampleFrom": 1640995200000, "inSampleTo": 1672531200000,
      "oosFrom":      1672531200000, "oosTo":      1688169600000,
      "bestParameters": { "signal_rules.contrarian.stoch_entry_max": 0 },
      "isResults": [ { "parameters": {...}, "summary": {...}, "score": 0.014 } ],
      "oosResult":  { "id": "01K...", "summary": {...} }
    }
  ],
  "aggregateOOS": { "geomMeanReturn": 0.0042, "robustnessScore": -0.0026, "..." : "..." }
}
```

### GET /backtest/walk-forward?limit=20&baseProfile=...&pdcaCycleId=...

envelope 一覧（`result_json` は含まない。list 用の軽量レスポンス）。

### GET /backtest/walk-forward/:id

envelope 全体（`request` / `result` / `aggregateOOS` が pre-parsed JSON で返る）。

`walkForwardRepo` 未ワイヤの場合、両 GET は 503 を返す。POST は常に計算しレスポンスを返す（保存失敗は `X-WalkForward-Persist-Error` ヘッダに出るが本文 200）。

## CLI

`cd backend` 配下で:

```bash
# 単発バックテスト
go run ./cmd/backtest run --profile production \
  --data data/candles_LTC_JPY_PT15M.csv --data-htf data/candles_LTC_JPY_PT1H.csv \
  --from 2024-01-01 --to 2025-01-01

# Grid 探索（Phase 2a → 2b refine）
go run ./cmd/backtest optimize --profile production ... --param "stop_loss_percent=3:10:1"
go run ./cmd/backtest refine   --profile production ... --param "stop_loss_percent=3:10:1"

# Walk-forward（PR-13 follow-up）
go run ./cmd/backtest walk-forward --profile production \
  --data data/candles_LTC_JPY_PT15M.csv \
  --from 2022-01-01 --to 2025-01-01 --in 12 --oos 6 --step 6 \
  --grid "signal_rules.contrarian.stoch_entry_max=0,15,25" \
  --output /tmp/wfo.json

# CSV ダウンロード (楽天 public API)
go run ./cmd/backtest download --symbol LTC_JPY --interval PT15M --from 2022-01-01
```
