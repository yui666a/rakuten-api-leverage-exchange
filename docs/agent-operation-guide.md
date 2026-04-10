# エージェント運用ガイド

このドキュメントは、Claude Code（LLM エージェント）がこのトレーディングシステムを操作するための手順書です。

## システム概要

楽天ウォレット証拠金取引所 API を使った BTC_JPY 自動売買システムです。

- **Backend**: Go (Gin) — Docker コンテナで稼働（ポート `38080`）
- **自動売買パイプライン**: 60秒間隔でテクニカル指標を計算し、ルールベースの Stance に基づいて売買シグナルを生成・実行
- **エージェントの役割**: REST API 経由でニュース・指標を総合判断し、Stance のオーバーライドや直接注文を行う

## ベースURL

```
http://localhost:38080/api/v1
```

## 運用フロー

### 1. 状況確認

まず現在の相場状況とシステム状態を把握する。

```bash
# ティッカー（現在価格）
curl -s 'localhost:38080/api/v1/ticker?symbolId=7'

# テクニカル指標（SMA, EMA, RSI, MACD）
curl -s 'localhost:38080/api/v1/indicators/7'

# 板情報
curl -s 'localhost:38080/api/v1/orderbook?symbolId=7'

# ポジション
curl -s 'localhost:38080/api/v1/positions?symbolId=7'

# 損益
curl -s 'localhost:38080/api/v1/pnl'

# Bot状態（残高、日次損失、停止状態）
curl -s 'localhost:38080/api/v1/status'

# 現在の戦略方針
curl -s 'localhost:38080/api/v1/strategy'

# リスク設定
curl -s 'localhost:38080/api/v1/config'

# ローソク足データ
curl -s 'localhost:38080/api/v1/candles/7?interval=15min&limit=100'
```

### 2. 情報収集（Web検索・ニュース）

上記の数値データに加えて、以下の情報ソースから BTC の市場動向を収集する:

- **Web 検索**: 「BTC 最新ニュース」「Bitcoin market analysis」など
- **仮想通貨ニュース API**: CoinGecko, CryptoCompare
- **RSS / X**: 主要な仮想通貨メディア

### 3. 判断と行動

テクニカル指標 + ニュース + ポジション状況を総合して:

#### 方針（Stance）を設定する場合

```bash
curl -s -X PUT 'localhost:38080/api/v1/strategy' \
  -H 'Content-Type: application/json' \
  -d '{"stance":"TREND_FOLLOW","reasoning":"判断理由をここに書く","ttlMinutes":60}'
```

- `stance`: `TREND_FOLLOW`, `CONTRARIAN`, `HOLD` のいずれか
- `reasoning`: なぜその方針にしたかの理由
- `ttlMinutes`: 有効期間（1〜1440分、デフォルト60分、上限24時間）

オーバーライドを解除（Backend のルールベース判定に戻す）:
```bash
curl -s -X DELETE 'localhost:38080/api/v1/strategy/override'
```

#### 直接注文する場合

```bash
curl -s -X POST 'localhost:38080/api/v1/orders' \
  -H 'Content-Type: application/json' \
  -d '{"symbolId":7,"side":"BUY","amount":0.001,"orderType":"MARKET","clientOrderId":"agent-20260410-130000-buy"}'
```

- `symbolId`: 7 = BTC_JPY
- `side`: `BUY` または `SELL`
- `amount`: 注文数量（BTC）
- `orderType`: `MARKET`（成行のみ）
- `clientOrderId`: **必須**。一意な ID。同じ ID で再リクエストすると二重発注を防止する。命名規則: `agent-YYYYMMDD-HHMMSS-side`

### 4. Bot の起動・停止

```bash
# 起動（自動売買パイプラインを開始）
curl -s -X POST 'localhost:38080/api/v1/start'

# 停止
curl -s -X POST 'localhost:38080/api/v1/stop'
```

### 5. リスク設定の変更

```bash
curl -s -X PUT 'localhost:38080/api/v1/config' \
  -H 'Content-Type: application/json' \
  -d '{"maxPositionAmount":5000,"maxDailyLoss":5000,"stopLossPercent":5,"initialCapital":10000}'
```

## Stance（方針）の仕組み

### ルールベース自動判定（Backend）

エージェントがオーバーライドしない場合、Backend が以下のルールで自動判定する:

| 優先度 | 条件 | Stance |
|-------|------|--------|
| 1 | RSI14 < 25 または RSI14 > 75 | CONTRARIAN |
| 2 | SMA20 と SMA50 の乖離率 < 0.1% | HOLD |
| 3 | SMA20 > SMA50 または SMA20 < SMA50 | TREND_FOLLOW |
| 4 | それ以外 | HOLD |

### 各 Stance での売買ロジック

**TREND_FOLLOW:**
- SMA20 > SMA50 かつ RSI < 70 → BUY
- SMA20 < SMA50 かつ RSI > 30 → SELL
- それ以外 → HOLD

**CONTRARIAN:**
- RSI < 30 → BUY（売られすぎ → 反発期待）
- RSI > 70 → SELL（買われすぎ → 下落期待）
- RSI 30〜70 → HOLD

**HOLD:**
- 何もしない

### エージェントのオーバーライド

エージェントが `PUT /api/v1/strategy` で Stance を設定すると、TTL の間はエージェントの判断が優先される。TTL が切れると自動的にルールベース判定に戻る。

## レスポンス形式

### GET /api/v1/ticker

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

### GET /api/v1/indicators/7

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
  "timestamp": 1775825100000
}
```

### GET /api/v1/strategy

```json
{
  "stance": "TREND_FOLLOW",
  "reasoning": "SMA trend detected",
  "source": "rule-based",
  "updatedAt": 1775826300
}
```

`source` が `"override"` の場合は `expiresAt` フィールドも含まれる。

### GET /api/v1/pnl

```json
{
  "balance": 10000,
  "dailyLoss": 0,
  "totalPosition": 0,
  "tradingHalted": false
}
```

### GET /api/v1/status

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

### GET /api/v1/config

```json
{
  "maxPositionAmount": 5000,
  "maxDailyLoss": 5000,
  "stopLossPercent": 5,
  "initialCapital": 10000
}
```

### POST /api/v1/orders（成功）

```json
{
  "clientOrderId": "agent-20260410-130000-buy",
  "executed": true,
  "orderId": 12345,
  "reason": ""
}
```

### POST /api/v1/orders（リスクチェック拒否）

```json
{
  "clientOrderId": "agent-20260410-130000-buy",
  "executed": false,
  "orderId": 0,
  "reason": "risk rejected: daily loss limit exceeded"
}
```

### POST /api/v1/orders（重複リクエスト）

```json
{
  "clientOrderId": "agent-20260410-130000-buy",
  "duplicate": true,
  "executed": true,
  "orderId": 12345
}
```

## 判断の指針

### BUY を検討する状況
- SMA20 > SMA50（上昇トレンド）で RSI が 30〜70 の範囲
- RSI < 30（売られすぎ）で反発の兆候
- ポジティブなニュース（ETF 承認、機関投資家参入など）

### SELL を検討する状況
- SMA20 < SMA50（下降トレンド）で RSI が 30〜70 の範囲
- RSI > 70（買われすぎ）で反落の兆候
- ネガティブなニュース（規制強化、ハッキングなど）

### HOLD を維持する状況
- トレンドが不明瞭（SMA がほぼ同値）
- 重要指標発表直前
- ポジション上限に近い

### リスク管理の注意点
- `dailyLoss` が `maxDailyLoss` に達すると自動的に取引停止
- `totalPosition` が `maxPositionAmount` を超える注文はリスク管理で拒否される
- 損切りは `stopLossPercent` に基づいてパイプラインが自動実行する

## Docker 運用

```bash
# コンテナの状態確認
docker ps --filter "name=rakuten"

# ログ確認
docker logs rakuten-api-leverage-exchange-backend-1 --tail 30

# パイプラインログだけ確認
docker logs rakuten-api-leverage-exchange-backend-1 2>&1 | grep -E "pipeline|signal" | tail -10

# 再ビルド
docker compose up -d --build backend
```
