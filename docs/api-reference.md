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

### GET /indicators/:symbol

テクニカル指標を取得する。`:symbol` は symbolId（例: `7`）。

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
  "atr14": 150000.0,
  "bollingerUpper": 11600000.0,
  "bollingerMiddle": 11459941.1,
  "bollingerLower": 11319882.2,
  "timestamp": 1775825100000
}
```

### GET /candles/:symbol?interval=15min&limit=100

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

現在の戦略方針を取得する。

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

戦略方針をオーバーライドする。

リクエスト:
```json
{
  "stance": "TREND_FOLLOW",
  "reasoning": "判断理由をここに書く",
  "ttlMinutes": 60
}
```

- `stance`: `TREND_FOLLOW`, `CONTRARIAN`, `HOLD` のいずれか
- `ttlMinutes`: 有効期間（1〜1440分、デフォルト60分）

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
  "initialCapital": 10000
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
  "tradingHalted": false
}
```
