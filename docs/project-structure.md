# Project Structure

```
.
├── backend/                              # Go バックエンド
│   ├── cmd/
│   │   ├── main.go                      #   エントリポイント + WebSocket relay + pipeline 組み立て
│   │   ├── pipeline.go                  #   TradingPipeline（自動売買ループ）
│   │   ├── retry.go                     #   リトライユーティリティ
│   │   ├── sync_state_test.go           #   状態同期テスト
│   │   ├── check/main.go               #   ヘルスチェック CLI
│   │   └── mcp/main.go                 #   MCP サーバーエントリ
│   ├── config/config.go                 #   環境変数 → Config 構造体
│   ├── internal/
│   │   ├── domain/
│   │   │   ├── entity/                  #     エンティティ
│   │   │   │   ├── ticker.go            #       Ticker (現在価格)
│   │   │   │   ├── candle.go            #       ローソク足
│   │   │   │   ├── order.go             #       注文
│   │   │   │   ├── client_order.go      #       クライアント注文 (冪等性キー)
│   │   │   │   ├── position.go          #       ポジション
│   │   │   │   ├── signal.go            #       売買シグナル
│   │   │   │   ├── indicator.go         #       テクニカル指標
│   │   │   │   ├── risk.go              #       リスク設定
│   │   │   │   ├── strategy.go          #       戦略 (Stance)
│   │   │   │   ├── orderbook.go         #       板情報
│   │   │   │   ├── asset.go             #       資産
│   │   │   │   ├── symbol.go            #       シンボル
│   │   │   │   ├── trade.go             #       約定
│   │   │   │   └── stringfloat.go       #       JSON 文字列 → float64 変換
│   │   │   └── repository/             #     リポジトリインターフェース
│   │   │       ├── order.go             #       注文 + API クライアント IF
│   │   │       ├── market_data.go       #       市場データ IF
│   │   │       ├── client_order.go      #       クライアント注文 IF
│   │   │       ├── risk_state.go        #       リスク状態 IF
│   │   │       ├── trade_history.go     #       取引履歴 IF
│   │   │       └── stance_override.go   #       Stance オーバーライド IF
│   │   ├── usecase/                     #     ビジネスロジック
│   │   │   ├── strategy.go             #       戦略エンジン (Stance → Signal)
│   │   │   ├── risk.go                 #       リスクマネージャー
│   │   │   ├── order.go                #       注文実行 (リスクチェック付き)
│   │   │   ├── indicator.go            #       テクニカル指標計算の統合
│   │   │   ├── market_data.go          #       市場データ管理 (Ticker/Candle 保存)
│   │   │   ├── stance.go               #       Stance 解決 (ルールベース + オーバーライド)
│   │   │   ├── daily_pnl.go            #       日次損益計算
│   │   │   └── realtime.go             #       WebSocket Hub (SSE/WS ブロードキャスト)
│   │   ├── infrastructure/
│   │   │   ├── database/               #       SQLite リポジトリ実装 + マイグレーション
│   │   │   │   ├── sqlite.go            #         DB 接続
│   │   │   │   ├── migrations.go        #         スキーママイグレーション
│   │   │   │   ├── market_data_repo.go  #         市場データリポジトリ
│   │   │   │   ├── client_order_repo.go #         クライアント注文リポジトリ
│   │   │   │   ├── trade_history_repo.go#         取引履歴リポジトリ
│   │   │   │   ├── risk_state_repo.go   #         リスク状態リポジトリ
│   │   │   │   └── stance_override_repo.go #      Stance オーバーライドリポジトリ
│   │   │   ├── indicator/              #       テクニカル指標の個別計算
│   │   │   │   ├── sma.go / ema.go      #         移動平均
│   │   │   │   ├── rsi.go              #         RSI
│   │   │   │   ├── macd.go             #         MACD
│   │   │   │   ├── atr.go              #         ATR
│   │   │   │   └── bollinger.go        #         ボリンジャーバンド
│   │   │   └── rakuten/                #       楽天ウォレット API クライアント
│   │   │       ├── rest_client.go       #         REST (Public + Private)
│   │   │       ├── auth.go             #         HMAC 署名・認証
│   │   │       ├── public_api.go       #         Public API (Ticker, Candle 等)
│   │   │       ├── private_api.go      #         Private API (Order, Position 等)
│   │   │       └── ws_client.go        #         WebSocket クライアント
│   │   └── interfaces/
│   │       ├── api/                     #       Gin HTTP ハンドラー + ルーター
│   │       │   ├── router.go            #         ルーティング定義
│   │       │   └── handler/             #         各エンドポイントのハンドラー
│   │       └── mcp/                     #       MCP サーバー実装
│   │           └── server.go
│   ├── Dockerfile
│   └── .env.example                     #   環境変数テンプレート
│
├── frontend/                             # React + TanStack フロントエンド
│   ├── src/
│   │   ├── components/                  #   UI コンポーネント
│   │   │   ├── AppFrame.tsx             #     レイアウト枠
│   │   │   ├── CandlestickChart.tsx     #     ローソク足チャート
│   │   │   ├── LiveTickerCard.tsx       #     リアルタイム価格カード
│   │   │   ├── BotControlCard.tsx       #     Bot 起動/停止カード
│   │   │   ├── IndicatorPanel.tsx       #     テクニカル指標パネル
│   │   │   ├── PositionPanel.tsx        #     ポジションパネル
│   │   │   ├── TradeHistoryTable.tsx    #     取引履歴テーブル
│   │   │   ├── KpiCard.tsx              #     KPI カード
│   │   │   └── SymbolSelector.tsx       #     シンボル選択
│   │   ├── hooks/                       #   カスタムフック
│   │   │   ├── useBotControl.ts         #     Bot 制御
│   │   │   ├── useMarketTickerStream.ts #     WebSocket Ticker
│   │   │   ├── usePositions.ts          #     ポジション取得
│   │   │   ├── useCandles.ts            #     ローソク足取得
│   │   │   ├── useIndicators.ts         #     指標取得
│   │   │   ├── useTradeHistory.ts       #     取引履歴
│   │   │   ├── usePnl.ts               #     損益
│   │   │   ├── useStrategy.ts           #     戦略
│   │   │   ├── useConfig.ts             #     設定
│   │   │   ├── useTradingConfig.ts      #     取引設定
│   │   │   ├── useSymbols.ts            #     シンボル一覧
│   │   │   ├── useStatus.ts             #     ステータス
│   │   │   ├── useAllTickers.ts         #     全ティッカー
│   │   │   └── useAllTrades.ts          #     全約定
│   │   ├── contexts/SymbolContext.tsx    #   シンボル選択 Context
│   │   ├── lib/api.ts                   #   API クライアント (fetch ラッパー)
│   │   ├── routes/                      #   ページ
│   │   │   ├── index.tsx                #     ダッシュボード (メイン画面)
│   │   │   ├── history.tsx              #     取引履歴ページ
│   │   │   └── settings.tsx             #     設定ページ
│   │   ├── router.tsx                   #   TanStack Router 設定
│   │   └── styles.css                   #   Tailwind エントリ
│   ├── Dockerfile
│   └── package.json
│
├── docs/                                 # ドキュメント
│   ├── project-structure.md             #   本ファイル
│   ├── api-reference.md                 #   API エンドポイント仕様
│   ├── agent-operation-guide.md         #   エージェント運用手順書
│   ├── clean-architecture.md            #   アーキテクチャ設計
│   ├── rakuten-api/error-codes.md       #   楽天 API エラーコード
│   └── design/                          #   設計書 + 実装計画
│
├── .agent/mcp.json                       # MCP サーバー設定
├── compose.yaml                          # Docker Compose 定義
├── Makefile                              # make up/down/logs/restart
├── AGENTS.md                             # エージェント共通ルール (エントリポイント)
└── CLAUDE.md                             # Claude Code 用 (AGENTS.md を参照)
```
