# AI自動売買システム 設計書 (2026-04-05 改訂)

## 概要

楽天ウォレット証拠金取引所APIを活用し、AIが価格やテクニカル指標を分析して完全自動で売買を行うシステム。
技術研鑽（Goシステムプログラミング、LLM連携、リアルタイムデータ処理）を目的として開発する。

### 前提条件

| 項目 | 値 |
|------|-----|
| 取引対象 | 楽天ウォレット証拠金取引所（BTC_JPY = symbolID: 7） |
| 軍資金 | 10,000円 |
| 同時ポジション上限 | 5,000円 |
| 日次損失上限 | 5,000円 |
| 損切りライン | 含み損5% |
| 稼働形態 | Docker Compose（backend + frontend） |
| LLMプロバイダー | Anthropic Claude（claude-haiku-3-5-latest） |

---

## 1. アーキテクチャ

Clean Architectureを採用。ドメイン層→ユースケース層→インフラ層→インターフェース層の依存方向を遵守する。

### 1.1 システム全体像

```
┌─────────────────────────────────────────────────────────────────┐
│                        Docker Compose                           │
│                                                                 │
│  ┌─────────────────────────────────────────────┐  ┌──────────┐ │
│  │              Backend (Go)                    │  │ Frontend │ │
│  │                                              │  │ (React)  │ │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  │  │          │ │
│  │  │  Market   │→│ Strategy │→│  Order   │  │  │ Dashboard│ │
│  │  │ Data Svc  │  │  Engine  │  │ Executor │  │  │ + Charts │ │
│  │  └──────────┘  └──────────┘  └──────────┘  │  │          │ │
│  │       │             │             │          │  │ WebSocket│ │
│  │       │        ┌──────────┐  ┌──────────┐  │  │ + REST   │ │
│  │       │        │   LLM    │  │   Risk   │  │  └──────────┘ │
│  │       │        │ Service  │  │ Manager  │  │       ↑        │
│  │       │        └──────────┘  └──────────┘  │       │        │
│  │       │                                     │       │        │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐ │       │        │
│  │  │Indicator │  │ Realtime │  │ REST API │──────────┘        │
│  │  │Calculator│  │   Hub    │  │ + WS     │ │                 │
│  │  └──────────┘  └──────────┘  └──────────┘ │                 │
│  │                                     │      │                 │
│  │  ┌──────────────────────────────────┐     │                 │
│  │  │  Rakuten API (REST + WebSocket)  │     │                 │
│  │  └──────────────────────────────────┘     │                 │
│  │  ┌──────────────────────────────────┐     │                 │
│  │  │         SQLite (永続化)           │     │                 │
│  │  └──────────────────────────────────┘     │                 │
│  └───────────────────────────────────────────┘                 │
└─────────────────────────────────────────────────────────────────┘
```

### 1.2 コンポーネント一覧

| コンポーネント | 責務 | 実装状態 |
|-------------|------|---------|
| **Market Data Service** | WebSocket/RESTで楽天APIから価格・板・歩み値を取得し配信 | 完了 |
| **Indicator Calculator** | 価格データからテクニカル指標（SMA, EMA, RSI, MACD）を計算 | 完了 |
| **Strategy Engine** | テクニカル指標 + LLMの判断を統合し、売買シグナルを生成 | 完了 |
| **LLM Service** | Claude APIで戦略方針（TREND_FOLLOW/CONTRARIAN/HOLD）を判断 | 完了 |
| **Risk Manager** | ポジション上限・日次損失・損切り・残高チェック | 完了 |
| **Order Executor** | Risk Managerを通過した注文を楽天APIに送信 | 完了 |
| **Realtime Hub** | Pub-Subでフロントエンドへイベントをストリーミング | 完了 |
| **REST API** | 全12エンドポイント（Gin） | 完了 |
| **Frontend Dashboard** | KPIカード、ローソク足チャート、指標、ポジション表示 | 完了 |
| **Trading Pipeline** | 上記を goroutine + channel で接続する自動売買ループ | **未実装** |

---

## 2. 楽天ウォレット証拠金取引所API

公式ドキュメント: https://www.rakuten-wallet.co.jp/service/api-leverage-exchange/

### Public API（認証不要）

| エンドポイント | メソッド | 概要 | 実装 |
|---|---|---|---|
| `/api/v1/cfd/symbol` | GET | 銘柄一覧取得 | 完了 |
| `/api/v1/candlestick` | GET | ローソク足取得（最大500件） | 完了 |
| `/api/v1/orderbook` | GET | 板取得 | 完了 |
| `/api/v1/ticker` | GET | ティッカー取得 | 完了 |
| `/api/v1/trades` | GET | 歩み値取得（直近60件） | 完了 |

### Private API（認証必要）

| エンドポイント | メソッド | 概要 | 実装 |
|---|---|---|---|
| `/api/v1/asset` | GET | 残高一覧取得 | 完了 |
| `/api/v1/cfd/equitydata` | GET | 証拠金関連項目取得 | 完了 |
| `/api/v1/cfd/order` | GET | 注文一覧取得 | 完了 |
| `/api/v1/cfd/order` | POST | 注文 | 完了 |
| `/api/v1/cfd/order` | PUT | 注文訂正 | 完了 |
| `/api/v1/cfd/order` | DELETE | 注文取消 | 完了 |
| `/api/v1/cfd/trade` | GET | 約定一覧取得 | 完了 |
| `/api/v1/cfd/position` | GET | 建玉一覧取得 | 完了 |

### WebSocket API

- 接続先: `wss://exchange.rakuten-wallet.co.jp/ws`
- 接続時間: 最長2時間、アイドル10分でタイムアウト
- データ種別: ORDERBOOK、TICKER、TRADES
- 再接続: 3秒間隔で自動再接続（実装済み）

### 認証

- ヘッダー: `API-KEY`, `NONCE`（ミリ秒timestamp）, `SIGNATURE`（HMAC SHA-256）
- GET/DELETE: `NONCE + URI + queryString` をハッシュ
- POST/PUT: `NONCE + JSON body` をハッシュ

### Rate Limit

- ユーザーごとに前回リクエストから200ms間隔が必要（RESTClient内で制御済み）

---

## 3. データフロー

### 3.1 現在のフロー（監視のみ）

```
楽天 WebSocket
     │
     ▼
Market Data Service ──→ SQLite (candles, tickers, trades)
     │
     ├──→ Realtime Hub ──→ Frontend (WebSocket /ws)
     │
     └──→ REST API ──→ Frontend (TanStack Query polling)
                           │
                      ┌────┴────────────────────┐
                      │ GET /status              │
                      │ GET /pnl                 │
                      │ GET /strategy            │
                      │ GET /indicators/:symbol  │
                      │ GET /candles/:symbol     │
                      │ GET /positions           │
                      │ GET /trades              │
                      └─────────────────────────┘
```

### 3.2 目標フロー（自動売買パイプライン）

```
楽天 WebSocket
     │
     ▼
Market Data Service ──channel──→ Indicator Calculator
     │                                   │
     │                              指標データ
     │                                   │
     ▼                                   ▼
  価格データ保持              Strategy Engine
  + Realtime Hub             │           │
                    テクニカル判断    LLM判断(15分キャッシュ)
                              │           │
                              ▼           ▼
                           シグナル統合
                              │
                              ▼
                         Risk Manager
                         │         │
                       承認       拒否(ログ)
                         │
                         ▼
                    Order Executor
                         │
                         ▼
                  楽天 REST API (注文)
```

### 3.3 パイプラインの設計方針

- **Market Data Service → Indicator Calculator**: 価格tickごとにchannelで送信。直近500件の価格から指標を計算
- **LLM呼び出しは非同期**: 戦略方針は15分キャッシュ（`LLM_CACHE_TTL_MIN`で設定可能）。毎tickではLLMを呼ばない
- **Risk Managerはゲートキーパー**: すべての注文がここを通過する。LLMの判断に関わらず、リスク管理ルールに違反する注文は実行されない
- **Rate Limit対応**: 楽天APIの200ms制限はRESTClient内でレートリミッターとして実装済み

---

## 4. LLM連携の段階的進化

### フェーズ1: 戦略立案者（現在）

```
LLM Service (15分キャッシュ)
  入力: 直近の価格推移、テクニカル指標サマリー
  出力: 戦略方針 (TREND_FOLLOW / CONTRARIAN / HOLD)
    ↓
Strategy Engine が方針に基づいてテクニカル指標の閾値を調整
  - TREND_FOLLOW: SMA20 vs SMA50のゴールデン/デッドクロス + RSIフィルター
  - CONTRARIAN: RSI 30以下で買い / 70以上で売り
  - HOLD: 何もしない
```

### フェーズ2: アドバイザー（将来）

```
テクニカル指標 → 売買シグナル発生
    ↓
LLM Service に確認 (シグナルごと)
  入力: シグナル内容 + 現在の相場状況 + 戦略方針
  出力: "実行/保留/反転" + 理由
    ↓
Strategy Engine がLLMの判断でシグナルを修正
```

### フェーズ3: 最終判断者（将来）

```
テクニカル指標 + 相場状況 + ポジション状態
    ↓
LLM Service (全データを渡す)
  出力: 具体的な注文指示 (銘柄/方向/数量/注文タイプ)
    ↓
Risk Manager で安全チェック後、実行
```

**原則: どのフェーズでも Risk Manager は LLM の上位に位置する。**

---

## 5. リスク管理

```
┌─────────────────────────────────────┐
│           Risk Manager              │
│                                     │
│  ┌───────────────────────────────┐  │
│  │ 手動停止チェック              │  │
│  │ - manualStop == true → 拒否   │  │
│  └──────────────┬────────────────┘  │
│                 ▼                   │
│  ┌───────────────────────────────┐  │
│  │ 日次損失チェック              │  │
│  │ - 当日確定損失 ≤ 5,000円      │  │
│  │ - 超過 → その日は取引停止     │  │
│  └──────────────┬────────────────┘  │
│                 ▼                   │
│  ┌───────────────────────────────┐  │
│  │ ポジションチェック             │  │
│  │ - 合計ポジション ≤ 5,000円    │  │
│  └──────────────┬────────────────┘  │
│                 ▼                   │
│  ┌───────────────────────────────┐  │
│  │ 軍資金チェック                │  │
│  │ - 残高が注文に十分か確認      │  │
│  │ - 初期資金: 10,000円          │  │
│  └──────────────┬────────────────┘  │
│                 ▼                   │
│  ┌───────────────────────────────┐  │
│  │ 損切り監視 (常時)             │  │
│  │ - 含み損 ≥ 5% → 即時決済注文  │  │
│  └──────────────┬────────────────┘  │
│                 ▼                   │
│            承認 or 拒否             │
└─────────────────────────────────────┘
```

| ルール | トリガー | アクション | 実装状態 |
|-------|---------|-----------|---------|
| 手動停止 | `/stop` API | 全注文を拒否 | 完了 |
| 日次損失上限 | 決済約定時 | 5,000円超過でその日の新規注文停止 | 完了（リセットタイマー未実装） |
| ポジション上限 | 新規注文時 | 5,000円超の注文を拒否 | 完了 |
| 残高チェック | 新規注文時 | 残高不足なら拒否 | 完了 |
| 損切り | 価格tick受信ごと | 含み損5%で即時成行決済 | `CheckStopLoss`実装済み、**監視goroutine未実装** |

- パラメータは環境変数 + REST API `PUT /config` で動的変更可能
- 決済注文（`IsClose: true`）はリスクチェックをバイパス

---

## 6. REST API

### エンドポイント一覧

| メソッド | エンドポイント | 概要 | 実装 |
|---------|--------------|------|------|
| GET | `/api/v1/status` | ボット稼働状態・残高・ポジション概要 | 完了 |
| POST | `/api/v1/start` | ボット開始 | 完了 |
| POST | `/api/v1/stop` | ボット停止 | 完了 |
| GET | `/api/v1/config` | リスク管理パラメータ取得 | 完了 |
| PUT | `/api/v1/config` | リスク管理パラメータ変更 | 完了 |
| GET | `/api/v1/pnl` | 損益情報（当日/累計） | 完了 |
| GET | `/api/v1/strategy` | 現在のLLM戦略方針 | 完了 |
| GET | `/api/v1/indicators/:symbol` | 銘柄ごとのテクニカル指標 | 完了 |
| GET | `/api/v1/candles/:symbol` | ローソク足データ | 完了 |
| GET | `/api/v1/positions` | ポジション一覧（楽天APIプロキシ） | 完了 |
| GET | `/api/v1/trades` | 約定一覧（楽天APIプロキシ） | 完了 |
| WS | `/api/v1/ws` | リアルタイムイベントストリーム | 完了 |

### WebSocket イベント種別

| type | 内容 |
|------|------|
| `ticker` | ティッカー更新 |
| `orderbook` | 板情報更新 |
| `market_trades` | 歩み値更新 |
| `status` | ボット状態変更（start/stop） |

---

## 7. フロントエンド

### 技術スタック

| 項目 | 技術 |
|------|------|
| ツールチェーン | Vite+ |
| フレームワーク | TanStack Start (React 19 + TypeScript) |
| スタイリング | TailwindCSS v4 |
| データ取得 | TanStack Query (ポーリング) + WebSocket (リアルタイム) |
| チャート | Lightweight Charts (TradingView製) |
| ルーティング | TanStack Router (file-based) |

### 画面構成

```
┌──────────┬──────────┬──────────┬──────────┐
│  残高     │ 日次損益  │  戦略方針 │ ステータス │
│ ¥10,000  │  -¥120   │  TREND   │  稼働中   │
└──────────┴──────────┴──────────┴──────────┘
┌─────────────────────────┬────────────────┐
│                         │ テクニカル指標   │
│   BTC/JPY ローソク足     │ RSI: 55.2      │
│   (Lightweight Charts)  │ SMA20: ¥13.4M  │
│                         │ MACD: +12,340  │
│                         ├────────────────┤
│                         │ ポジション      │
│                         │ BTC LONG 0.01  │
└─────────────────────────┴────────────────┘
```

### ページ

| パス | 内容 |
|------|------|
| `/` | ダッシュボード（KPI + チャート + 指標 + ポジション） |
| `/history` | 取引履歴 |
| `/settings` | リスク管理設定 |

### コンポーネント

| コンポーネント | 用途 |
|--------------|------|
| `AppFrame` | メインレイアウト |
| `KpiCard` | KPI表示カード |
| `CandlestickChart` | ローソク足チャート（SMA20/50オーバーレイ） |
| `IndicatorPanel` | テクニカル指標パネル |
| `PositionPanel` | ポジション一覧 |
| `BotControlCard` | ボット起動/停止 |
| `LiveTickerCard` | リアルタイムティッカー |
| `TradeHistoryTable` | 取引履歴テーブル |

### データ取得

| Hook | ポーリング間隔 | エンドポイント |
|------|-------------|--------------|
| `useStatus` | 10秒 | GET /status |
| `usePnl` | 10秒 | GET /pnl |
| `useStrategy` | 30秒 | GET /strategy |
| `useIndicators` | 30秒 | GET /indicators/7 |
| `useCandles` | 60秒 | GET /candles/7 |
| `usePositions` | 10秒 | GET /positions |
| `useTradeHistory` | 30秒 | GET /trades |
| `useConfig` | - | GET/PUT /config |
| `useBotControl` | - | POST /start, /stop |
| `useMarketTickerStream` | リアルタイム | WS /ws |

---

## 8. データ永続化

### SQLite テーブル

| テーブル | 用途 | カラム |
|---------|------|--------|
| `candles` | ローソク足（指標計算元） | symbol_id, open, high, low, close, volume, time, interval |
| `tickers` | ティッカー履歴 | symbol_id, best_ask, best_bid, open, high, low, last, volume, timestamp |
| `trades` | 歩み値 | symbol_id, order_side, price, amount, asset_amount, traded_at |

### インデックス

- `idx_candles_symbol_time`: `(symbol_id, time DESC)`
- `idx_tickers_symbol_time`: `(symbol_id, timestamp DESC)`
- `idx_trades_symbol_time`: `(symbol_id, traded_at DESC)`

### ストレージ方針

- SQLite（`modernc.org/sqlite`、cgo不要）
- Docker Volume `backend-data` でコンテナ再起動後もデータ永続化
- パス: `data/trading.db`（環境変数 `DATABASE_PATH` で変更可能）

---

## 9. Docker構成

```yaml
services:
  backend:
    build: ./backend/Dockerfile
    ports: 38080 → 8080
    volumes: backend-data:/app/backend/data
    healthcheck: curl -f http://127.0.0.1:8080/api/v1/status
    restart: unless-stopped

  frontend:
    build: ./frontend/Dockerfile
    ports: 33000 → 3000
    volumes: ./frontend:/app/frontend (ライブリロード)
    env: VITE_API_HOST=localhost:38080
    depends_on: backend (healthy)
```

---

## 10. 設定一覧

| 環境変数 | デフォルト | 説明 |
|---------|----------|------|
| `SERVER_PORT` | 8080 | REST APIポート |
| `DATABASE_PATH` | data/trading.db | SQLiteファイルパス |
| `RISK_MAX_POSITION_AMOUNT` | 5000 | 同時ポジション上限（円） |
| `RISK_MAX_DAILY_LOSS` | 5000 | 日次損失上限（円） |
| `RISK_STOP_LOSS_PERCENT` | 5 | 損切りライン（%） |
| `RISK_INITIAL_CAPITAL` | 10000 | 軍資金（円） |
| `RAKUTEN_API_BASE_URL` | https://exchange.rakuten-wallet.co.jp | 楽天API |
| `RAKUTEN_WS_URL` | wss://exchange.rakuten-wallet.co.jp/ws | 楽天WebSocket |
| `RAKUTEN_API_KEY` | - | 楽天APIキー |
| `RAKUTEN_API_SECRET` | - | 楽天APIシークレット |
| `ANTHROPIC_API_KEY` | - | Claude APIキー |
| `LLM_MODEL` | claude-haiku-3-5-latest | 使用モデル |
| `LLM_MAX_TOKENS` | 1024 | 最大トークン数 |
| `LLM_CACHE_TTL_MIN` | 15 | 戦略キャッシュTTL（分） |

---

## 11. プロジェクト構成

```
.
├── compose.yaml
├── docs/design/
│   ├── 2026-04-05-auto-trading-system-design.md  # 本ドキュメント
│   ├── 2026-04-05-frontend-dashboard-design.md
│   └── plans/                                     # Plan 1〜10 実装計画
│
├── backend/
│   ├── Dockerfile
│   ├── cmd/
│   │   ├── main.go                        # エントリポイント
│   │   └── check/main.go                  # 残高確認ユーティリティ
│   ├── config/
│   │   └── config.go                      # 環境変数ベースの設定管理
│   └── internal/
│       ├── domain/                        # ドメイン層
│       │   ├── entity/
│       │   │   ├── asset.go              # 残高
│       │   │   ├── candle.go             # ローソク足
│       │   │   ├── indicator.go          # テクニカル指標セット
│       │   │   ├── order.go              # 注文
│       │   │   ├── orderbook.go          # 板情報
│       │   │   ├── position.go           # ポジション
│       │   │   ├── risk.go               # リスク管理パラメータ
│       │   │   ├── signal.go             # 売買シグナル (BUY/SELL/HOLD)
│       │   │   ├── strategy.go           # 戦略方針 (TREND_FOLLOW/CONTRARIAN/HOLD)
│       │   │   ├── stringfloat.go        # WS文字列数値デコーダー
│       │   │   ├── symbol.go             # 銘柄
│       │   │   ├── ticker.go             # ティッカー
│       │   │   └── trade.go              # 約定
│       │   └── repository/
│       │       ├── market_data.go         # 市場データIF
│       │       └── order.go               # 注文IF
│       │
│       ├── usecase/                       # ユースケース層
│       │   ├── market_data.go             # Market Data Service + Pub-Sub
│       │   ├── indicator.go               # Indicator Calculator (SMA/EMA/RSI/MACD)
│       │   ├── strategy.go               # Strategy Engine (指標+LLM→シグナル)
│       │   ├── llm.go                    # LLM Service (TTLキャッシュ付き)
│       │   ├── risk.go                   # Risk Manager (全チェック)
│       │   ├── order.go                  # Order Executor (注文送信+決済)
│       │   └── realtime.go              # Realtime Hub (Pub-Sub)
│       │
│       ├── infrastructure/               # インフラ層
│       │   ├── rakuten/
│       │   │   ├── rest_client.go        # RESTクライアント (200msレートリミッター)
│       │   │   ├── public_api.go         # Public API実装
│       │   │   ├── private_api.go        # Private API実装
│       │   │   ├── ws_client.go          # WebSocketクライアント (自動再接続)
│       │   │   └── auth.go               # HMAC-SHA256認証
│       │   ├── llm/
│       │   │   └── claude_client.go      # Claude APIクライアント
│       │   ├── database/
│       │   │   ├── sqlite.go             # DB接続
│       │   │   ├── market_data_repo.go   # 市場データリポジトリ
│       │   │   └── migrations.go         # スキーマ管理
│       │   └── indicator/
│       │       ├── sma.go                # SMA計算
│       │       ├── ema.go                # EMA計算
│       │       ├── rsi.go                # RSI計算
│       │       └── macd.go               # MACD計算
│       │
│       └── interfaces/                   # インターフェース層
│           └── api/
│               ├── router.go             # Ginルーター + CORS
│               └── handler/
│                   ├── bot.go            # 起動/停止
│                   ├── status.go         # ステータス
│                   ├── risk.go           # リスク設定 + PnL
│                   ├── strategy.go       # 戦略方針
│                   ├── indicator.go      # テクニカル指標
│                   ├── candle.go         # ローソク足
│                   ├── position.go       # ポジション
│                   ├── trade.go          # 約定
│                   └── realtime.go       # WebSocket配信
│
└── frontend/
    ├── Dockerfile
    ├── src/
    │   ├── routes/
    │   │   ├── __root.tsx               # ルートレイアウト
    │   │   ├── index.tsx                # ダッシュボード
    │   │   ├── history.tsx              # 取引履歴
    │   │   └── settings.tsx             # 設定
    │   ├── components/                  # UIコンポーネント (8個)
    │   ├── hooks/                       # データ取得フック (10個)
    │   └── lib/
    │       └── api.ts                   # APIクライアント + 型定義
    └── app.config.ts
```

---

## 12. 実装進捗

| Plan | 内容 | PR | 状態 |
|------|------|----|------|
| Plan 1 | 楽天APIクライアント (REST + WebSocket + 認証) | #5 | merged |
| Plan 2 | Market Data Service + SQLite永続化 | #6 | merged |
| Plan 3 | テクニカル指標計算 (SMA, EMA, RSI, MACD) | #7 | merged |
| Plan 4 | リスク管理 (Risk Manager) | #8 | merged |
| Plan 5 | Strategy Engine + LLM連携 | #9 | merged |
| Plan 6 | Order Executor | #10 | merged |
| Plan 7 | REST API (Gin, 全エンドポイント) | #11 | merged |
| Plan 8 | Trading Engine 統合 (DI + main.go) | #12 | merged |
| Plan 9 | バックエンド追加API (candles, positions, ws) | #13 | merged |
| Plan 10 | フロントエンド ダッシュボード | #13-#16 | merged |
| Hotfix | 楽天WS文字列数値デコード + Docker環境変数 | #18 | merged |
| Hotfix | ローソク足タイムスタンプ修正 + volume mount | #19 | merged |

---

## 13. 残課題（Next Steps）

### Phase A: 自動売買パイプライン（高優先度）

本番稼働に必要な自動売買ループの実装。各コンポーネントは実装済みだが、goroutine + channel でパイプラインとして接続されていない。

| # | 課題 | 内容 | 依存コンポーネント |
|---|------|------|-----------------|
| A-1 | **Trading Pipeline ループ** | Ticker受信→指標計算→戦略判定→注文実行の自動ループを `main.go` に実装 | StrategyEngine, OrderExecutor, IndicatorCalculator |
| A-2 | **損切り監視 goroutine** | 価格tick受信ごとに `CheckStopLoss` を実行し、該当ポジションを `ClosePosition` で即時決済 | RiskManager, OrderExecutor |
| A-3 | **起動時ポジション・残高同期** | 起動時に楽天APIから現在のポジション・残高を取得し、RiskManagerに反映 | `GetPositions`, `GetAssets` (実装済み) |
| A-4 | **日次損失リセットタイマー** | 毎日0時(JST)に `ResetDailyLoss` を呼ぶスケジューラー | RiskManager (メソッド実装済み) |
| A-5 | **注文数量の決定ロジック** | 残高・リスク設定から適切な注文数量を計算するロジック | RiskManager |

### Phase B: 状態永続化（中優先度）

再起動後の状態復元と履歴管理。

| # | 課題 | 内容 |
|---|------|------|
| B-1 | **注文・約定履歴のSQLite永続化** | `orders`, `executions` テーブルを追加し、注文結果・約定情報を保存 |
| B-2 | **PnL・ポジション状態の永続化** | 再起動後も日次損失・ポジション状態を復元可能にする |
| B-3 | **ローソク足の差分更新** | 起動時に不足分のみbootstrapし、既存データと重複しない |

### Phase C: 運用強化（中優先度）

安定運用のための機能追加。

| # | 課題 | 内容 |
|---|------|------|
| C-1 | **WebSocket再接続の改善** | 指数バックオフ + 最長2時間の接続制限への事前再接続 |
| C-2 | **MCP Server** | REST APIと同じユースケース層をMCPツールとして公開（Claude Codeから直接操作） |
| C-3 | **アラート通知** | 損切り発動・日次上限到達時のSlack/Discord通知 |
| C-4 | **取引ログの構造化** | `log.Printf` → 構造化ログ（slog等）に移行 |

### Phase D: LLM進化（低優先度）

| # | 課題 | 内容 |
|---|------|------|
| D-1 | **LLMフェーズ2（アドバイザー）** | 個別シグナルにもLLMが介入し「実行/保留/反転」を判断 |
| D-2 | **LLMフェーズ3（最終判断者）** | LLMが具体的な注文指示（銘柄/方向/数量）を決定。Risk Managerは常にゲートキーパー |
| D-3 | **複数銘柄対応** | symbolID: 7 (BTC_JPY) 以外の銘柄も同時に監視・売買 |
| D-4 | **バックテスト機能** | 過去データでの戦略検証 |

### 優先順位の方針

```
Phase A (自動売買パイプライン)
  → 本番稼働の最低条件。これがないと手動操作のみ。
  ↓
Phase B (状態永続化)
  → 再起動でリセットされるのは運用上問題。
  ↓
Phase C (運用強化)
  → 安定稼働のための品質向上。
  ↓
Phase D (LLM進化)
  → 利益改善のための段階的強化。
```
