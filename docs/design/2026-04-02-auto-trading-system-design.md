# AI自動売買システム 設計書

## 概要

楽天ウォレット証拠金取引所APIを活用し、AIが価格やテクニカル指標を分析して完全自動で売買を行うシステム。
技術研鑽（Goシステムプログラミング、LLM連携、リアルタイムデータ処理）を目的として開発する。

## 前提条件

- 取引対象: 楽天ウォレット証拠金取引所の複数銘柄
- 軍資金: 10,000円
- 同時ポジション上限: 5,000円
- 日次損失上限: 5,000円
- 損切りライン: 含み損5%
- 稼働形態: リアルタイム常時稼働（デーモン）
- LLMプロバイダー: Anthropic Claude

## 楽天ウォレット証拠金取引所API

公式ドキュメント: https://www.rakuten-wallet.co.jp/service/api-leverage-exchange/

### Public API（認証不要）

| エンドポイント | メソッド | 概要 |
|---|---|---|
| `/api/v1/cfd/symbol` | GET | 銘柄一覧取得 |
| `/api/v1/candlestick` | GET | ローソク足取得（最大500件） |
| `/api/v1/orderbook` | GET | 板取得 |
| `/api/v1/ticker` | GET | ティッカー取得 |
| `/api/v1/trades` | GET | 歩み値取得（直近60件） |

### Private API（認証必要）

| エンドポイント | メソッド | 概要 |
|---|---|---|
| `/api/v1/asset` | GET | 残高一覧取得 |
| `/api/v1/cfd/equitydata` | GET | 証拠金関連項目取得 |
| `/api/v1/cfd/order` | GET | 注文一覧取得 |
| `/api/v1/cfd/order` | POST | 注文 |
| `/api/v1/cfd/order` | PUT | 注文訂正 |
| `/api/v1/cfd/order` | DELETE | 注文取消 |
| `/api/v1/cfd/trade` | GET | 約定一覧取得 |
| `/api/v1/cfd/position` | GET | 建玉一覧取得 |

### WebSocket API

- 接続先: `wss://exchange.rakuten-wallet.co.jp/ws`
- 接続時間: 最長2時間、アイドル10分でタイムアウト
- データ種別: ORDERBOOK、TICKER、TRADES
- subscribe/unsubscribe で購読管理

### 認証

- ヘッダー: `API-KEY`, `NONCE`（ミリ秒timestamp）, `SIGNATURE`（HMAC SHA-256）
- GET/DELETE: `NONCE + URI + queryString` をハッシュ
- POST/PUT: `NONCE + JSON body` をハッシュ

### Rate Limit

- ユーザーごとに前回リクエストから200ms間隔が必要

---

## 1. システム全体像

```
┌─────────────────────────────────────────────────────────┐
│                    Trading Engine                        │
│                                                         │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐          │
│  │  Market   │───→│ Strategy │───→│  Order   │          │
│  │ Data Svc  │    │  Engine  │    │ Executor │          │
│  └──────────┘    └──────────┘    └──────────┘          │
│       │               │               │                 │
│       │          ┌──────────┐    ┌──────────┐          │
│       │          │   LLM    │    │   Risk   │          │
│       │          │ Service  │    │ Manager  │          │
│       │          └──────────┘    └──────────┘          │
│       │                                                 │
│  ┌──────────┐                                          │
│  │Indicator │                                          │
│  │Calculator│                                          │
│  └──────────┘                                          │
│                                                         │
├─────────────────────────────────────────────────────────┤
│  REST API / MCP Server (操作・監視用)                    │
├─────────────────────────────────────────────────────────┤
│  Rakuten Wallet API Client (REST + WebSocket)           │
└─────────────────────────────────────────────────────────┘
```

### コアコンポーネント

| コンポーネント | 責務 |
|-------------|------|
| **Market Data Service** | WebSocket/RESTで楽天APIから価格・板・歩み値を取得し、channelで配信 |
| **Indicator Calculator** | 受信した価格データからテクニカル指標（RSI, MACD, 移動平均等）を計算 |
| **Strategy Engine** | テクニカル指標 + LLMの判断を統合し、売買シグナルを生成 |
| **LLM Service** | Claude APIを呼び出し、戦略方針の決定やシグナルの補足判断を行う |
| **Risk Manager** | ポジション上限・日次損失上限・損切りルールを適用し、注文を承認/拒否 |
| **Order Executor** | Risk Managerを通過した注文を楽天APIに送信し、約定を管理 |

---

## 2. データフロー（パイプライン）

アーキテクチャはイベント駆動パイプラインを採用する。各コンポーネントをGoのgoroutine + channelで接続する。

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
                              │           │
                    テクニカル判断    LLM判断(非同期)
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

### パイプラインの設計方針

- **Market Data Service → Indicator Calculator**: 価格tickごとにchannelで送信。Indicator Calculatorは直近N件の価格を保持して指標を計算
- **LLM呼び出しは非同期**: LLMのレスポンスは遅い（数秒）ので、毎tickで呼ばない。戦略方針は定期的（15分ごと）に更新し、キャッシュした方針をStrategy Engineが参照する
- **Risk Managerはゲートキーパー**: すべての注文がここを通過する。条件を満たさなければ注文は拒否されログに残る
- **Rate Limit対応**: 楽天APIの200ms制限はOrder Executor内でレートリミッターとして実装

---

## 3. LLM連携の段階的進化

### フェーズ1: 戦略立案者 (C)

```
LLM Service (定期実行: 15分ごと)
  入力: 直近の価格推移、テクニカル指標サマリー、ボラティリティ
  出力: 戦略方針 (例: "トレンドフォロー/逆張り/様子見")
    ↓
Strategy Engine がその方針に基づいてテクニカル指標のパラメータ・閾値を調整
```

LLMは「今どんな相場か」を判断し、戦略の方向性だけ決める。実際のエントリー/イグジットはテクニカル指標ルールが実行する。

### フェーズ2: アドバイザー (B)

```
テクニカル指標 → 売買シグナル発生
    ↓
LLM Service に確認 (シグナルごと)
  入力: シグナル内容 + 現在の相場状況 + 戦略方針
  出力: "実行/保留/反転" + 理由
    ↓
Strategy Engine がLLMの判断でシグナルを修正
```

フェーズ1の戦略方針に加えて、個別シグナルにもLLMが介入できるようになる。

### フェーズ3: 最終判断者 (A)

```
テクニカル指標 + 相場状況 + ポジション状態
    ↓
LLM Service (全データを渡す)
  出力: 具体的な注文指示 (銘柄/方向/数量/注文タイプ)
    ↓
Risk Manager で安全チェック後、実行
```

LLMが注文内容まで決める。ただし Risk Manager は常にゲートキーパーとして機能し、LLMの暴走を防ぐ。

### 設計方針

どのフェーズでも **Risk Manager は LLM の上位に位置する**。LLMがどんな判断をしても、リスク管理ルールに違反する注文は実行されない。

---

## 4. リスク管理

```
┌─────────────────────────────────────┐
│           Risk Manager              │
│                                     │
│  ┌───────────────────────────────┐  │
│  │ ポジションチェック             │  │
│  │ - 合計ポジション ≤ 5,000円    │  │
│  └──────────────┬────────────────┘  │
│                 ▼                   │
│  ┌───────────────────────────────┐  │
│  │ 日次損失チェック              │  │
│  │ - 当日確定損失 ≤ 5,000円      │  │
│  │ - 超過 → その日は取引停止     │  │
│  └──────────────┬────────────────┘  │
│                 ▼                   │
│  ┌───────────────────────────────┐  │
│  │ 損切り監視 (常時)             │  │
│  │ - 含み損 ≥ 5% → 即時決済注文  │  │
│  └──────────────┬────────────────┘  │
│                 ▼                   │
│  ┌───────────────────────────────┐  │
│  │ 軍資金チェック                │  │
│  │ - 残高が注文に十分か確認      │  │
│  │ - 初期資金: 10,000円          │  │
│  └──────────────┬────────────────┘  │
│                 ▼                   │
│            承認 or 拒否             │
└─────────────────────────────────────┘
```

| ルール | トリガー | アクション |
|-------|---------|-----------|
| ポジション上限 | 新規注文時 | 合計5,000円を超える注文を拒否 |
| 日次損失上限 | 決済約定時 | 当日の累計損失が5,000円に達したらその日の新規注文を全停止 |
| 損切り | 価格tick受信ごと | 保有ポジションの含み損が5%に達したら即座に成行決済注文を発行 |
| 軍資金 | 新規注文時 | 残高不足なら拒否 |

### 補足

- パラメータ（5,000円、5%等）は設定ファイルで管理し、REST API / MCPから変更可能
- 日次損失のリセットは毎日0時（JST）
- 損切りの5%チェックはRisk Managerが独自のgoroutineで価格データを監視し、Strategy Engineの判断を待たずに即時発動

---

## 5. 外部インターフェース

### REST API

```
GET  /api/v1/status              # ボット稼働状態
POST /api/v1/start               # ボット開始
POST /api/v1/stop                # ボット停止

GET  /api/v1/positions           # 現在のポジション一覧
GET  /api/v1/trades              # 取引履歴
GET  /api/v1/pnl                 # 損益情報 (当日/累計)

GET  /api/v1/config              # リスク管理パラメータ取得
PUT  /api/v1/config              # リスク管理パラメータ変更

GET  /api/v1/strategy            # 現在のLLM戦略方針
GET  /api/v1/indicators/:symbol  # 銘柄ごとのテクニカル指標
```

### MCP Server

同じ機能をMCPツールとしても公開。Claude Codeや他のAIエージェントから直接操作できる。

```
tools:
  - get_status          # 稼働状態確認
  - start_bot           # ボット開始
  - stop_bot            # ボット停止
  - get_positions       # ポジション確認
  - get_pnl             # 損益確認
  - update_config       # パラメータ変更
  - get_strategy        # 現在の戦略確認
  - get_indicators      # テクニカル指標確認
```

### 設計方針

REST APIとMCPは同じユースケース層を共有する。インターフェース層だけが異なる。Clean Architectureの恩恵で、REST handler と MCP handler を差し替えるだけで両方に対応できる。

---

## 6. データ永続化

### 保存データ

| データ | 用途 | 特性 |
|-------|------|------|
| 価格データ (ローソク足) | テクニカル指標の計算元 | 時系列、大量、書き込み頻度高 |
| 取引履歴 | 損益計算、振り返り | 追記のみ |
| ポジション状態 | 再起動時の復元 | 頻繁に更新 |
| リスク管理状態 | 日次損失額など | 日次リセット |
| LLM戦略方針 | 現在の戦略キャッシュ | 定期更新 |
| 設定値 | パラメータ管理 | 低頻度更新 |

### ストレージ

SQLite を採用する。

- インストール不要、ファイル1つで完結
- Goとの相性が良い（`modernc.org/sqlite` で cgo不要）
- 個人利用の書き込み頻度なら十分な性能
- 将来的にPostgreSQLへの移行もClean Architectureのリポジトリ層の差し替えだけで対応可能

---

## 7. プロジェクト構成

```
backend/
├── cmd/
│   └── main.go                        # エントリポイント
├── config/
│   └── config.go                      # 設定管理
└── internal/
    ├── domain/                        # ドメイン層
    │   ├── entity/
    │   │   ├── ticker.go             #   価格データ
    │   │   ├── candle.go             #   ローソク足
    │   │   ├── order.go              #   注文
    │   │   ├── position.go           #   ポジション
    │   │   ├── signal.go             #   売買シグナル
    │   │   └── strategy.go           #   戦略方針
    │   └── repository/
    │       ├── market_data.go        #   市場データIF
    │       ├── order.go              #   注文IF
    │       └── position.go           #   ポジションIF
    │
    ├── usecase/                       # ユースケース層
    │   ├── market_data.go            #   Market Data Service
    │   ├── indicator.go              #   Indicator Calculator
    │   ├── strategy.go               #   Strategy Engine
    │   ├── llm.go                    #   LLM Service
    │   ├── risk.go                   #   Risk Manager
    │   └── order.go                  #   Order Executor
    │
    ├── infrastructure/               # インフラ層
    │   ├── rakuten/
    │   │   ├── rest_client.go        #   楽天REST APIクライアント
    │   │   └── ws_client.go          #   楽天WebSocketクライアント
    │   ├── llm/
    │   │   └── claude_client.go      #   Claude APIクライアント
    │   ├── database/
    │   │   └── sqlite.go             #   SQLiteリポジトリ実装
    │   └── indicator/
    │       ├── rsi.go                #   RSI計算
    │       ├── macd.go               #   MACD計算
    │       └── moving_average.go     #   移動平均計算
    │
    └── interfaces/                   # インターフェース層
        ├── api/
        │   ├── router.go             #   Ginルーター
        │   └── handler/
        │       ├── status.go         #   ボット状態
        │       ├── trade.go          #   取引関連
        │       └── config.go         #   設定変更
        └── mcp/
            └── server.go             #   MCPサーバー
```

---

## 実装進捗

| Plan | 内容 | PR | 状態 | 主なファイル |
|------|------|----|------|------------|
| Plan 1 | 楽天APIクライアント (REST + WebSocket + 認証) | #5 | merged | `infrastructure/rakuten/` |
| Plan 2 | Market Data Service + SQLite永続化 | #6 | merged | `infrastructure/database/`, `usecase/market_data.go` |
| Plan 3 | テクニカル指標計算 (SMA, EMA, RSI, MACD) | #7 | merged | `infrastructure/indicator/`, `usecase/indicator.go` |
| Plan 4 | リスク管理 (Risk Manager) | #8 | merged | `usecase/risk.go`, `entity/risk.go` |
| Plan 5 | Strategy Engine + LLM連携 | #9 | merged | `usecase/strategy.go`, `usecase/llm.go`, `infrastructure/llm/` |
| Plan 6 | Order Executor | #10 | merged | `usecase/order.go`, `repository/order.go` |
| Plan 7 | REST API | #11 | merged | `interfaces/api/` |
| Plan 8 | Trading Engine 統合 | #12 | merged | `cmd/main.go` |

---

## 残課題（Next Steps）

Plan 1〜8で全コンポーネントの実装とDI統合が完了した。以下は本番稼働に向けて必要な残課題。

### 高優先度

| 課題 | 内容 | 備考 |
|------|------|------|
| リアルタイム自動売買パイプライン | WebSocket接続→Ticker受信→指標計算→戦略判定→注文実行の自動ループ | `main.go`にTODOとして残置。goroutine + channelで接続する |
| 損切り自動発動 | 価格tick受信ごとに`CheckStopLoss`を実行し、該当ポジションを即時決済 | `CheckStopLoss`は実装済み、監視goroutineが未実装 |
| 起動時ポジション同期 | 起動時に楽天APIから現在のポジション・残高を取得しRisk Managerに反映 | `GetPositions`, `GetAssets`は実装済み |
| 日次損失リセット | 毎日0時(JST)に`ResetDailyLoss`を呼ぶスケジューラー | `ResetDailyLoss`は実装済み、タイマーが未実装 |

### 中優先度

| 課題 | 内容 | 備考 |
|------|------|------|
| 取引履歴・損益の永続化 | 注文結果・約定情報をSQLiteに保存し、再起動後も状態復元可能にする | 現状は再起動でPnL・ポジション状態がリセットされる |
| MCP Server | REST APIと同じユースケース層をMCPツールとして公開 | Claude Codeや他AIエージェントからの直接操作用 |
| ボット起動/停止API | `POST /start`, `POST /stop`でTradingパイプラインの動的制御 | REST APIの`/status`は実装済み、起動/停止は未実装 |

### 低優先度

| 課題 | 内容 | 備考 |
|------|------|------|
| LLMフェーズ2 (アドバイザー) | 個別シグナルにもLLMが介入し「実行/保留/反転」を判断 | 現在はフェーズ1（戦略方針のみ） |
| LLMフェーズ3 (最終判断者) | LLMが具体的な注文指示（銘柄/方向/数量）を決定 | Risk Managerが常にゲートキーパーとして機能 |
| WebSocket再接続 | 切断時の自動再接続・バックオフ | 最長2時間の接続制限への対応 |
| 監視ダッシュボード | リアルタイムのPnL・ポジション・戦略方針の可視化 | REST APIのデータを使ってフロントエンドを構築 |
