# Project Structure

```
.
├── backend/                              # Go バックエンド
│   ├── cmd/
│   │   ├── main.go                      #   エントリポイント + WebSocket relay + pipeline 組み立て
│   │   ├── event_pipeline.go            #   EventDrivenPipeline（現行: ローソク足確定駆動の自動売買）
│   │   ├── pipeline.go                  #   TradingPipeline（legacy: 60秒polling、現環境では未使用）
│   │   ├── retry.go                     #   リトライユーティリティ
│   │   ├── sync_state_test.go           #   状態同期テスト
│   │   ├── backtest/main.go             #   バックテスト CLI (run/optimize/refine/download/walk-forward)
│   │   ├── backtest/walkforward.go      #   walk-forward サブコマンド (PR-13 follow-up / #120)
│   │   ├── check/main.go               #   ヘルスチェック CLI
│   │   └── mcp/main.go                 #   MCP サーバーエントリ
│   ├── config/config.go                 #   環境変数 → Config 構造体
│   ├── profiles/                        #   Strategy プロファイル (JSON)
│   │   ├── production.json              #     現行本番戦略
│   │   ├── baseline.json                #     デフォルトロジックの literal 再現
│   │   └── experiment_*.json            #     PDCA 実験プロファイル
│   ├── internal/
│   │   ├── domain/
│   │   │   ├── entity/
│   │   │   │   ├── ticker.go / candle.go / order.go / client_order.go
│   │   │   │   ├── position.go / signal.go / orderbook.go / asset.go
│   │   │   │   ├── symbol.go / trade.go / stringfloat.go
│   │   │   │   ├── indicator.go         #     IndicatorSet + IchimokuSnapshot (SMA/EMA/RSI/MACD/BB/ATR/Volume/ADX/Stoch/Ichimoku)
│   │   │   │   ├── risk.go              #     リスク設定
│   │   │   │   ├── strategy.go          #     Stance (TREND_FOLLOW/CONTRARIAN/BREAKOUT/HOLD)
│   │   │   │   ├── strategy_config.go   #     StrategyProfile + ネスト設定（PR-6 ADX / PR-7 Stoch / PR-8 Ichimoku mode 対応）
│   │   │   │   ├── backtest.go          #     BacktestResult / Summary / DrawdownPeriod / MultiPeriodAggregate 等
│   │   │   │   ├── aggregate_json.go    #     NaN/±Inf → JSON null round-trip
│   │   │   │   ├── walk_forward.go      #     WalkForwardPersisted（DB envelope 型）
│   │   │   │   └── backtest_event.go    #     バックテスト用イベント定義
│   │   │   ├── port/strategy.go         #     Strategy インターフェース
│   │   │   └── repository/              #     リポジトリインターフェース
│   │   │       ├── order.go / market_data.go / client_order.go
│   │   │       ├── risk_state.go / trade_history.go / stance_override.go
│   │   │       ├── backtest_result.go   #       PR-2 PDCA フィルタ対応
│   │   │       ├── multi_period_result.go #     PR-2 複数期間 envelope
│   │   │       ├── walk_forward_result.go #     PR-13 follow-up WFO envelope
│   │   │       └── errors.go            #       ErrParentResultNotFound / SelfReference
│   │   ├── usecase/                     #     ビジネスロジック
│   │   │   ├── strategy.go              #       StrategyEngine + Options (ADX/Stoch gates / HTF mode)
│   │   │   ├── stance.go                #       RuleBasedStanceResolver
│   │   │   ├── risk.go                  #       RiskManager (ATR stop / trailing)
│   │   │   ├── order.go                 #       OrderExecutor
│   │   │   ├── indicator.go             #       IndicatorCalculator（live）
│   │   │   ├── market_data.go           #       Ticker/Candle 管理
│   │   │   ├── daily_pnl.go             #       日次損益計算
│   │   │   ├── realtime.go              #       WS Hub
│   │   │   ├── strategy/                #       Strategy 実装
│   │   │   │   ├── default_strategy.go  #         DefaultStrategy（legacy ラッパ）
│   │   │   │   ├── configurable_strategy.go #     ConfigurableStrategy（profile 駆動）
│   │   │   │   ├── registry.go          #         StrategyRegistry
│   │   │   │   ├── adx_gate_test.go     #         PR-6 配線確認
│   │   │   │   ├── stoch_gate_test.go   #         PR-7 配線確認
│   │   │   │   └── htf_ichimoku_test.go #         PR-8 配線確認
│   │   │   ├── backtest/                #       バックテストエンジン
│   │   │   │   ├── runner.go            #         BacktestRunner
│   │   │   │   ├── handler.go           #         イベントハンドラチェーン (Tick/Indicator/Strategy/Risk)
│   │   │   │   ├── simulator.go 相当は infrastructure/backtest/ 側
│   │   │   │   ├── biweekly.go          #         BiweeklyWinRate
│   │   │   │   ├── breakdown.go         #         PR-1 Exit 理由別 / シグナル別サマリ
│   │   │   │   ├── drawdown_detail.go   #         PR-3 DD 履歴 / TiM / Expectancy
│   │   │   │   ├── aggregate.go         #         PR-2 MultiPeriodAggregate 計算
│   │   │   │   ├── multi_period_runner.go #       PR-2 複数期間並列実行
│   │   │   │   ├── walkforward.go       #         PR-13 ComputeWindows / ExpandGrid / ApplyOverrides
│   │   │   │   ├── walkforward_runner.go #        PR-13 WFO runner (IS/OOS)
│   │   │   │   ├── reporter.go          #         結果整形
│   │   │   │   ├── optimizer.go         #         Phase 2a/2b パラメータ探索
│   │   │   │   └── ulid.go              #         result ID 生成
│   │   │   └── eventengine/             #       イベント駆動 pipeline (手動売買と共有)
│   │   ├── infrastructure/
│   │   │   ├── database/                #       SQLite 実装 + マイグレーション
│   │   │   │   ├── sqlite.go / migrations.go
│   │   │   │   └── 各種 _repo.go（market_data / client_order / trade_history / risk_state / stance_override）
│   │   │   ├── indicator/               #       指標計算ロジック
│   │   │   │   ├── sma.go / ema.go / rsi.go / macd.go / atr.go / bollinger.go / volume.go
│   │   │   │   ├── adx.go               #         PR-6 ADX + DI
│   │   │   │   ├── stochastics.go       #         PR-7 %K/%D + StochRSI
│   │   │   │   └── ichimoku.go          #         PR-8 Tenkan/Kijun/SenkouA-B/Chikou + CloudPosition
│   │   │   ├── backtest/                #       SQLite 永続化
│   │   │   │   ├── result_repository.go #         BacktestResult (PDCA フィルタ + PR-1/3 JSON カラム)
│   │   │   │   ├── multi_period_repository.go #   PR-2 envelope
│   │   │   │   ├── walk_forward_repository.go #   PR-13 follow-up envelope (result_json 全体保存)
│   │   │   │   └── simulator.go         #         注文約定シミュレータ
│   │   │   ├── strategyprofile/         #       Profile loader + パス検証
│   │   │   ├── csv/                     #       CSV ローダ（バックテストデータ）
│   │   │   ├── live/                    #       Live pipeline helper
│   │   │   └── rakuten/                 #       楽天ウォレット API クライアント
│   │   │       ├── rest_client.go / auth.go / public_api.go / private_api.go / ws_client.go
│   │   └── interfaces/
│   │       ├── api/                     #       Gin HTTP ハンドラー + ルーター
│   │       │   ├── router.go            #         /api/v1/* ルーティング
│   │       │   └── handler/
│   │       │       ├── backtest.go              # /backtest/run & /backtest/results (PDCA)
│   │       │       ├── backtest_multi.go        # /backtest/run-multi & /backtest/multi-results (PR-2)
│   │       │       ├── backtest_walkforward.go  # /backtest/walk-forward (GET/POST/list) (PR-13 + #120)
│   │       │       └── 各種 *_test.go
│   │       └── mcp/                     #       MCP サーバー実装
│   ├── Dockerfile
│   └── .env.example
│
├── frontend/                             # React + TanStack フロントエンド
│   ├── src/
│   │   ├── components/                  #   UI コンポーネント
│   │   │   ├── AppFrame.tsx             #     レイアウト枠 + グローバルナビ (dashboard/settings/history/backtest/multi/WFO)
│   │   │   ├── CandlestickChart.tsx     #     ローソク足チャート（指標スタック: MACD→RSI→Stoch→ADX）
│   │   │   ├── MACDChart.tsx / RSIChart.tsx / StochasticsChart.tsx / ADXChart.tsx #  指標パネル群
│   │   │   ├── EquityCurveChart.tsx     #     バックテスト資産推移
│   │   │   ├── IndicatorPanel.tsx / LiveTickerCard.tsx / BotControlCard.tsx / PositionPanel.tsx
│   │   │   ├── TradeHistoryTable.tsx / KpiCard.tsx / SymbolSelector.tsx
│   │   ├── hooks/                       #   カスタムフック
│   │   │   ├── useBotControl.ts / useStatus.ts / usePnl.ts / useConfig.ts
│   │   │   ├── useMarketTickerStream.ts / useAllTickers.ts
│   │   │   ├── useCandles.ts / useIndicators.ts
│   │   │   ├── usePositions.ts / useTradeHistory.ts / useAllTrades.ts / useSymbols.ts / useTradingConfig.ts / useStrategy.ts
│   │   │   ├── useBacktest.ts           #     単発バックテスト + フィルタ付き結果一覧
│   │   │   ├── useMultiPeriod.ts        #     PR-2 複数期間 list / detail
│   │   │   └── useWalkForward.ts        #     PR-13 follow-up list / detail
│   │   ├── contexts/SymbolContext.tsx
│   │   ├── lib/api.ts                   #   API クライアント + 型定義（BacktestSummary / MultiPeriodAggregate / WalkForwardResult 等）
│   │   ├── routes/                      #   ページ
│   │   │   ├── __root.tsx
│   │   │   ├── index.tsx                #     ダッシュボード
│   │   │   ├── history.tsx              #     取引履歴
│   │   │   ├── settings.tsx             #     設定
│   │   │   ├── backtest.tsx             #     バックテスト実行 + 詳細（PR-1 breakdown / PR-3 DD 表示対応）
│   │   │   ├── backtest-multi.tsx       #     PR-2 複数期間ランキング + 詳細
│   │   │   └── walk-forward.tsx         #     PR-13 follow-up ランキング + 窓別 OOS + Best パラメータ頻度
│   │   ├── router.tsx                   #   TanStack Router 設定
│   │   ├── routeTree.gen.ts             #   自動生成ルートツリー
│   │   └── styles.css                   #   Tailwind エントリ
│   ├── Dockerfile
│   └── package.json
│
├── docs/                                 # ドキュメント
│   ├── project-structure.md             #   本ファイル
│   ├── api-reference.md                 #   API エンドポイント仕様
│   ├── agent-operation-guide.md         #   エージェント運用手順
│   ├── clean-architecture.md            #   アーキテクチャ設計
│   ├── backtest-csv-data-guide.md       #   バックテスト CSV 取得・運用手順
│   ├── rakuten-api/error-codes.md       #   楽天 API エラーコード
│   ├── design/                          #   設計書 + 実装計画
│   │   ├── 2026-04-21-pdca-v2-infrastructure-plan.md  # Phase A/B/C 計画
│   │   └── plans/                       #   PR 毎の設計ドキュメント（PR-1〜PR-13 等）
│   ├── pdca/                            #   PDCA 運用ガイド + サイクル記録
│   │   ├── README.md / agent-guide.md / _template.md
│   │   ├── 2026-04-21_cycle*.md         #     既存サイクル記録
│   │   ├── 2026-04-21_promotion_v*.md   #     昇格記録（v1〜v4b）
│   │   └── 2026-04-22_cycle22-23.md     #     PR-7 Stoch gate WFO 負結果記録
│   └── superpowers/specs/               #   システム仕様書
│
├── .agent/mcp.json                       # MCP サーバー設定
├── compose.yaml                          # Docker Compose 定義
├── Makefile                              # make up/down/logs/restart
├── AGENTS.md                             # エージェント共通ルール (エントリポイント)
├── CLAUDE.md                             # Claude Code 用 (AGENTS.md を参照)
├── README.md                             # プロジェクト概要
└── ONBOARDING.md                         # 新規参加者向け手順
```
