# Claude Code 自律トレーディングシステム設計

- **作成日**: 2026-04-10
- **ステータス**: Draft

## 概要

現状の LLM (Claude API) による Stance 判定を廃止し、以下の2層構造に置き換える:

1. **Backend**: ルールベースの自動方針判定 + 売買シグナル生成 + 注文実行
2. **Claude Code**: ニュース・Web 検索・RSS 等で情報を収集し、REST API 経由で Stance をオーバーライド。定期実行 + 手動介入の両方で運用

## 設計方針

- Backend に生成 AI は持たせない。ルールベースのロジックのみ
- Claude Code のオーバーライドがあればそちらを優先、なければ Backend の自動判定で動く
- 既存の REST API を拡張する（MCP は使わない）

## 1. Backend ルールベース方針判定

### 現状

```
LLM (Claude API) → Stance (TREND_FOLLOW / CONTRARIAN / HOLD)
  → StrategyEngine がテクニカル指標でシグナル生成
```

### 変更後

```
RuleBasedStanceResolver → Stance (TREND_FOLLOW / CONTRARIAN / HOLD)
  ↑ Claude Code のオーバーライドがあればそちらを優先
  → StrategyEngine がテクニカル指標でシグナル生成（既存ロジック維持）
```

### ルールベース判定ロジック

| 優先度 | 条件 | Stance |
|-------|------|--------|
| 1 | Claude Code のオーバーライドが有効（TTL 内） | オーバーライド値 |
| 2 | RSI14 < 25 または RSI14 > 75 | CONTRARIAN |
| 3 | SMA20 と SMA50 の乖離率が 0.1% 未満 | HOLD（トレンド不明瞭） |
| 4 | SMA20 > SMA50（上昇トレンド） | TREND_FOLLOW |
| 5 | SMA20 < SMA50（下降トレンド） | TREND_FOLLOW |
| 6 | 上記いずれにも該当しない | HOLD |

### オーバーライド管理

```go
type StanceOverride struct {
    Stance    MarketStance
    Reasoning string
    SetAt     time.Time
    TTL       time.Duration // min: 1分, max: 1440分 (24時間)
}
```

- オーバーライドには TTL（有効期限）を持たせる（上限 24 時間、操作ミスによる長期固定を防止）
- 期限切れで自動的にルールベース判定に戻る
- `DELETE /api/v1/strategy/override` で手動解除も可能
- SQLite に永続化し、Backend 再起動後も有効期限内であれば復元する

## 2. 新規 REST API エンドポイント

### PUT /api/v1/strategy

Stance のオーバーライドを設定する。

**Request:**
```json
{
  "stance": "TREND_FOLLOW",
  "reasoning": "BTC ETF 承認ニュースで上昇トレンド期待",
  "ttlMinutes": 60
}
```

**Response:**
```json
{
  "stance": "TREND_FOLLOW",
  "reasoning": "BTC ETF 承認ニュースで上昇トレンド期待",
  "source": "override",
  "expiresAt": "2026-04-10T12:00:00Z"
}
```

**バリデーション:**
- `stance` は `TREND_FOLLOW`, `CONTRARIAN`, `HOLD` のいずれか
- `ttlMinutes` は 1〜1440（デフォルト: 60, 上限 24 時間）

### DELETE /api/v1/strategy/override

オーバーライドを解除し、ルールベース判定に戻す。

**Response:**
```json
{
  "message": "override cleared, using rule-based stance"
}
```

### GET /api/v1/strategy（既存変更）

レスポンスに `source` フィールドを追加し、現在の Stance がオーバーライドか自動判定かを区別する。

**Response:**
```json
{
  "stance": "TREND_FOLLOW",
  "reasoning": "SMA20 > SMA50, uptrend detected",
  "source": "rule-based",
  "updatedAt": 1712736000
}
```

`source` は `"override"` または `"rule-based"` のいずれか。

### POST /api/v1/orders

Claude Code から直接注文を実行する。リスク管理チェックを通す。

**Request:**
```json
{
  "symbolId": 7,
  "side": "BUY",
  "amount": 0.001,
  "orderType": "MARKET",
  "clientOrderId": "claude-20260410-114500-buy"
}
```

**Response (成功):**
```json
{
  "executed": true,
  "orderId": 12345,
  "clientOrderId": "claude-20260410-114500-buy",
  "side": "BUY",
  "amount": 0.001,
  "price": 11500000
}
```

**Response (リスクチェック拒否):**
```json
{
  "executed": false,
  "reason": "daily loss limit exceeded"
}
```

**Response (重複リクエスト):**
```json
{
  "executed": true,
  "orderId": 12345,
  "clientOrderId": "claude-20260410-114500-buy",
  "duplicate": true
}
```

**バリデーション:**
- `side` は `BUY` または `SELL`
- `amount` は正の値
- `orderType` は `MARKET`（初期実装では成行のみ）
- `clientOrderId` は必須。同一 ID のリクエストは二重発注せず、前回の結果を返す（冪等性保証）
- リスク管理チェック（日次損失上限、ポジション上限）を通す

**冪等性:**
- `clientOrderId` をキーに、注文結果を SQLite に記録する
- 同一 `clientOrderId` で再リクエストされた場合、発注せず記録済みの結果を返す
- 記録は 24 時間で自動削除する

**排他制御:**
- パイプラインの自動注文と Claude Code の直接注文は、同一の `OrderExecutor` を経由する
- `RiskManager.CheckOrder` は mutex で排他制御されており、同時実行でもリスク上限を超えない

### GET /api/v1/orderbook

最新の板情報を返す。現状 WebSocket でのみ取得可能だったものを REST でも取得可能にする。

**Query Parameters:**
- `symbolId` (default: 7)

**Response:**
```json
{
  "symbolId": 7,
  "asks": [{"price": 11500100, "amount": 0.5}, ...],
  "bids": [{"price": 11499900, "amount": 0.3}, ...],
  "bestAsk": 11500100,
  "bestBid": 11499900,
  "spread": 200,
  "timestamp": 1712736000
}
```

### GET /api/v1/ticker

最新のティッカー情報を返す。

**Query Parameters:**
- `symbolId` (default: 7)

**Response:**
```json
{
  "symbolId": 7,
  "bestAsk": 11500100,
  "bestBid": 11499900,
  "last": 11500000,
  "high": 11600000,
  "low": 11400000,
  "volume": 123.45,
  "timestamp": 1712736000
}
```

## 3. 変更対象ファイル

### 新規作成

| ファイル | 内容 |
|---------|------|
| `usecase/stance.go` | `RuleBasedStanceResolver` — ルールベース判定 + オーバーライド管理 |
| `domain/repository/stance_override.go` | オーバーライド永続化リポジトリインターフェース |
| `domain/repository/client_order.go` | 冪等性用の注文結果記録リポジトリインターフェース |
| `infrastructure/database/stance_override_repo.go` | オーバーライド永続化の SQLite 実装 |
| `infrastructure/database/client_order_repo.go` | 冪等性用の注文結果記録の SQLite 実装 |
| `interfaces/api/handler/order.go` | 注文ハンドラー |
| `interfaces/api/handler/orderbook.go` | 板情報ハンドラー |
| `interfaces/api/handler/ticker.go` | ティッカーハンドラー |

### 変更

| ファイル | 変更内容 |
|---------|---------|
| `usecase/strategy.go` | `LLMService` 依存を `StanceResolver` インターフェースに置換 |
| `interfaces/api/handler/strategy.go` | PUT / DELETE エンドポイント追加、レスポンスに `source` 追加 |
| `interfaces/api/router.go` | 新規エンドポイントのルーティング追加 |
| `cmd/main.go` | LLM 関連の初期化を除去、`RuleBasedStanceResolver` に置き換え |

### 削除候補

| ファイル | 理由 |
|---------|------|
| `infrastructure/llm/claude_client.go` | LLM 直接呼び出しが不要になるため |
| `usecase/llm.go` | LLMService が不要になるため |

## 4. Claude Code 運用フロー

### 定期実行（cron）

1. `GET /api/v1/ticker?symbolId=7` で現在価格を取得
2. `GET /api/v1/indicators/7` でテクニカル指標を取得
3. `GET /api/v1/positions?symbolId=7` でポジション状況を確認
4. Web 検索 / ニュース API / RSS で BTC 関連情報を収集
5. 総合判断して `PUT /api/v1/strategy` で Stance を設定
6. 必要に応じて `POST /api/v1/orders` で直接注文

### 手動介入

ユーザーが「判断して」と指示 → 上記フローを即座に実行

### 情報ソース

- **仮想通貨ニュース API**: CoinGecko, CryptoCompare
- **Web 検索**: 最新ニュース・市場動向
- **RSS / X**: 特定のソースを定期チェック

## 5. テスト方針

### ユニットテスト
- `RuleBasedStanceResolver` のルール判定（各条件の境界値、SMA 乖離率 0.1% 前後）
- オーバーライドの TTL 管理（設定・期限切れ・手動解除）
- オーバーライドの永続化と再起動復元
- 新規ハンドラーのバリデーション（正常系・異常系）
- `StrategyEngine` の既存テストが新しい `StanceResolver` でも動くことを確認

### 冪等性・排他制御テスト
- 同一 `clientOrderId` での二重リクエストが重複発注しないこと
- パイプラインの自動注文と直接注文の同時実行でリスク上限を超えないこと
