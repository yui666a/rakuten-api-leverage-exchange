# バックテストエンジン設計

- **作成日**: 2026-04-14
- **更新日**: 2026-04-15
- **ステータス**: Done（Phase 1 + Phase 2 + 将来スコープ 完了）

## 概要

既存の自動売買戦略が有効かを過去データで検証するバックテストエンジンを実装する。MT5 のストラテジーテスターに相当する機能を、既存の Clean Architecture を活かしたイベントドリブン設計で構築する。

### 目的

- 過去データに基づく戦略パフォーマンス検証
- パラメータ最適化による戦略チューニング
- 将来の本番パイプラインのイベントドリブン化への布石

### スコープ

| フェーズ | 内容 |
|---|---|
| **Phase 1（初期）** | バックテスト実行 + サマリー + 全トレードCSV出力 + 結果永続化API |
| **Phase 2（次期）** | パラメータ最適化（段階探索） |
| **将来** | フロントエンド可視化、本番パイプラインのイベントドリブン置換 |

## 0. 実装進捗（2026-04-15 時点）

### 0.1 全体進捗

| 区分 | 進捗 | 補足 |
|---|---|---|
| Phase 1 コア機能 | ✅ 完了 | EventEngine / SimExecutor / CLI run / API保存取得まで実装済み |
| Phase 1 仕上げ | ✅ 完了 | API入力契約固定（リスクパラメータ公開）・統合テスト強化済み |
| Phase 2a 粗探索 | ✅ 完了 | `cmd/backtest optimize` + `--workers` 並列評価あり |
| Phase 2b 局所探索 | ✅ 完了 | `cmd/backtest refine` + `Optimizer.Refine()` 実装済み |

### 0.2 実装済み

- EventBus（FIFO + priority + 連鎖順）とハンドラー群（Indicator/Strategy/Risk/Execution/TickRisk）
- `TickEvent` 擬似生成（Open→High/Low→Close）と同一バー SL/TP 両ヒット時の `worst-case`
- `StrategyEngine`/`RiskManager`/`StanceResolver` の At系メソッド（時刻注入）
- SimExecutor（spread/slippage/carrying cost、反対シグナル時クローズ、評価資産計算）
- Metrics（TotalReturn, WinRate, ProfitFactor, AvgHoldTime）
  - Sharpe: 日次終値（JST）ベース
  - MaxDD: 15分足クローズ時の評価資産カーブ
- CLI
  - `backtest run`（`--stop-loss` / `--take-profit` 対応）
  - `backtest download`（`--from` / `--update`）
  - `backtest optimize`（パラメータ探索、`--workers` 並列、`--stop-loss` / `--take-profit`）
  - `backtest refine`（粗探索→局所近傍探索の一括実行）
- API
  - `POST /api/v1/backtest/run`（リスクパラメータ指定可能）
  - `GET /api/v1/backtest/results`
  - `GET /api/v1/backtest/results/:id`
- SQLite 永続化
  - `backtest_results`, `backtest_trades`
  - 結果ID: ULID
- Retention
  - `BACKTEST_RETENTION_DAYS`（既定 180）
  - 起動時 + 24時間ごとの期限切れ削除
- Phase 2b 局所探索
  - `Optimizer.Refine()`: 上位N件の近傍を細粒度グリッドで再探索
  - `buildNeighborhoodRanges()`: ±1ステップ範囲 × stepDiv分割
  - `deduplicateCombos()`: 重複パラメータ組み合わせ除去
- テスト
  - API統合テスト（Run→List→Get E2E、リスクパラメータ指定、404、不正入力）
  - Refine統合テスト、近傍レンジ生成、クランプ、重複除去

## 設計方針

- **イベントドリブン**: キャンドルを1本ずつ処理し、将来の1分足/ティック対応に拡張可能な形にする。
- **既存ロジック再利用 + 最小拡張**: `StrategyEngine` / `RiskManager` / `infrastructure/indicator` を再利用しつつ、バックテストではイベント時刻を注入できる形に拡張する。
- **時刻基準の単一化**: バックテスト中の判定時刻はすべて「履歴イベント時刻」を使い、`time.Now()`基準での非決定性を排除する。
- **先読みバイアス防止**: マルチタイムフレームは「その時点で確定済みの上位足のみ参照」を必須ルールとする。
- **保守的な約定仮定**: 同一バー内でSL/TPが両ヒットする場合は `worst-case` を採用し、過大評価を避ける。
- **データ管理**: 楽天API → SQLite/CSV キャッシュ。差分更新を前提にAPI呼び出しを最小化する。

## 1. 全体アーキテクチャ

```
┌────────────────────────────────────────────────────────────┐
│                        EventEngine                         │
│                                                            │
│  EventSource ──→ EventBus(FIFO) ──→ Handlers              │
│  (Candle/Tick)             │         ├─ IndicatorHandler   │
│                            │         ├─ StrategyHandler    │
│                            │         ├─ RiskHandler        │
│                            │         └─ ExecutionHandler   │
│                                            │               │
│                                      OrderExecutor         │
│                                      (interface)           │
│                                ┌──────────┴──────────┐     │
│                              SimExecutor       RealExecutor│
│                              (backtest)        (将来)      │
└────────────────────────────────────────────────────────────┘
```

### EventSource の実装

| 実装 | 用途 | データソース |
|---|---|---|
| `HistoricalSource` | バックテスト | CSV または SQLite |
| `LiveSource` | 将来の本番 | 楽天API WebSocket |

## 2. イベント設計

### 2.1 イベントの種類

```go
// Candle.Time は「確定時刻（close時刻）」として扱う
type CandleEvent struct {
    SymbolID  int64
    Interval  string        // "PT15M", "PT1H"
    Candle    entity.Candle // OHLCV + Time(close)
    Timestamp int64         // Candle.Time と同値（Unix ms）
}

// IndicatorHandler が生成し、StrategyHandler の入力契約を固定する
type IndicatorEvent struct {
    SymbolID   int64
    Interval   string // 生成元（主に PT15M）
    Primary    entity.IndicatorSet
    HigherTF   *entity.IndicatorSet // close_time <= Primary.Timestamp の最新1本
    LastPrice  float64
    Timestamp  int64
}

// バー内擬似ティック
type TickEvent struct {
    SymbolID   int64
    Price      float64
    Timestamp  int64 // 疑似時刻（2.3で定義）
    TickType   string // "open" | "high" | "low" | "close"
    ParentTime int64  // 親キャンドルの close 時刻
}

type SignalEvent struct {
    Signal    entity.Signal
    Price     float64
    Timestamp int64
}

type OrderEvent struct {
    OrderID    int64
    SymbolID   int64
    Side       string  // "BUY" | "SELL"
    Action     string  // "open" | "close"
    Price      float64
    Amount     float64
    Reason     string
    Timestamp  int64
}
```

### 2.2 イベントフロー

```
CandleEvent(PT15M)
  → IndicatorHandler: 指標計算 + 上位足同期 → IndicatorEvent
  → StrategyHandler: IndicatorEvent からシグナル生成 → SignalEvent

SignalEvent
  → RiskHandler: ポジション上限、日次損失、クールダウン確認
  → ExecutionHandler: 注文シミュレーション → OrderEvent

TickEvent（バー内価格変動）
  → RiskHandler: SL/TP/トレーリング判定 → OrderEvent（決済）

OrderEvent
  → PositionTracker: ポジション更新（建玉管理料含む）
  → TradeLogger: トレードログ記録
```

### 2.3 バー内ティック擬似生成とSL/TP優先順位

バックテストではティックがないため、1本のキャンドルから4つの `TickEvent` を生成する。

```
陽線(Open < Close): Open → High → Low → Close
陰線(Open > Close): Open → Low → High → Close
同値(Open == Close): Open → High → Low → Close
```

`TickEvent.Timestamp` はキャンドル区間を4分割して割り当てる（決定論保証）。

- `intervalStart = candle.Time - intervalDuration`
- `t1 = intervalStart + 25%`
- `t2 = intervalStart + 50%`
- `t3 = intervalStart + 75%`
- `t4 = candle.Time`

同一バー内で同一ポジションに対して **SL と TP が両方到達**した場合は次を採用する。

- デフォルトは **`worst-case` 固定**
- BUY: `StopLoss` を先に約定したものとして扱う
- SELL: `StopLoss` を先に約定したものとして扱う
- 理由: 楽観的バイアスを避け、再現性と保守性を優先する

## 3. コアコンポーネント

### 3.1 EventBus

バックテストは決定論が必須のため、処理順序を固定する。

- 同期実行 + FIFOキュー（幅優先）
- 1イベント内のハンドラー実行順は `priority` 昇順で固定
- 各ハンドラーが返した連鎖イベントは「返却順」のままキュー末尾へ追加
- 同一入力データ + 同一設定なら常に同一結果になることを要件化

```go
type EventBus struct {
    handlers map[string][]RegisteredHandler // priority で事前ソート
    queue    []Event
}

type RegisteredHandler struct {
    Priority int
    Handler  EventHandler
}
```

### 3.2 IndicatorHandler

`IndicatorHandler` は `infrastructure/indicator/` の純粋関数を呼ぶ薄いラッパーとする。  
`usecase/IndicatorCalculator`（DB依存）はバックテストでは使用しない。

- intervalごとにキャンドルバッファを保持（既定 500 本）
- PT15M 受信時に `Primary` 指標を計算
- 同時に「`close_time <= PT15M時刻` の最新 PT1H」を選び `HigherTF` 指標を計算
- 生成結果を `IndicatorEvent` として発行

### 3.3 StrategyHandler

`StrategyHandler` は `IndicatorEvent` を受けて `StrategyEngine` を呼ぶ。  
主入力契約は以下で固定する。

```
CandleEvent(PT15M)
  -> IndicatorEvent{Primary, HigherTF, LastPrice, Timestamp}
  -> StrategyEngine.EvaluateWithHigherTFAt(...)
```

先読みバイアス防止ルール:

- PT15M 時刻 `T` で参照可能な PT1H は「close 時刻 `<= T` の最新1本のみ」
- `T` より後に確定する PT1H は絶対に参照しない

### 3.4 RiskHandler

`RiskManager` を再利用しつつ、バックテストでは時刻注入APIを使う。

- `CheckOrderAt(now, proposal)` を使用（クールダウン判定をイベント時刻基準に統一）
- `RecordConsecutiveLossAt(now)` を使用（`cooldownUntil` を履歴時刻で計算）
- `CheckStopLoss` / `CheckTakeProfit` / `CheckTrailingStop` は `TickEvent` 駆動

### 3.5 StanceResolver 方針

バックテストではオーバーライド永続化を無効化する。

- `RuleBasedStanceResolver(nil)` を注入（DBアクセスなし）
- `Source` は常に `rule-based`
- TTLベースの override はバックテスト対象外

### 3.6 SimExecutor（注文シミュレーター）

```go
type SimExecutor struct {
    positions    []SimPosition
    closedTrades []TradeRecord
    balance      float64
    config       SimConfig
}

type SimConfig struct {
    InitialBalance      float64 // JPY
    SpreadPercent       float64 // 例: 0.1%
    DailyCarryingCost   float64 // 0.04%/日
    SlippagePercent     float64 // 初期は0
}
```

約定ロジック:

- エントリー: シグナル価格 ± スプレッド/2
- 決済: SL/TPヒット価格またはシグナル決済価格 ± スプレッド/2
- 建玉管理料: `保有日数 × 0.04%` を決済時に差し引く

## 4. データ管理

### 4.1 キャンドルデータのライフサイクル

```
初回: 楽天API → SaveCandles() → SQLite → ExportCSV()
2回目以降: CSV/SQLite キャッシュ利用
更新時: 最新タイムスタンプ以降だけ API で取得し追記
```

### 4.2 CSVフォーマット

CLIは `--symbol BTC_JPY` の文字列指定を受けるため、CSVには `symbol` と `symbol_id` を両方保持する。

```csv
symbol,symbol_id,interval,time,open,high,low,close,volume
BTC_JPY,7,PT15M,1772984700000,15000000,15050000,14980000,15030000,1.5
BTC_JPY,7,PT15M,1772985600000,15030000,15100000,15010000,15080000,2.1
```

- `time`: Unixミリ秒（キャンドル確定時刻）
- ファイル名: `data/candles_{symbol}_{interval}.csv`

### 4.3 データ取得コマンドとAPIパラメータ

```bash
go run ./cmd/backtest download --symbol BTC_JPY --interval PT15M --from 2026-01-01
go run ./cmd/backtest download --symbol BTC_JPY --interval PT15M --update
```

楽天API呼び出しは既存 `GetCandlestick(symbolID, candlestickType, dateFrom, dateTo)` に合わせ、**`dateFrom/dateTo` を使用**する。`before` は使わない。

差分取得ルール:

- `--from` 指定時: `dateFrom = fromTs`, `dateTo = nowTs`
- `--update` 時: `dateFrom = lastCSVTs + 1`, `dateTo = nowTs`
- 1回の応答が上限件数に達した場合は、未取得区間が残っていれば `dateFrom/dateTo` を更新して再取得する

## 5. バックテスト実行

### 5.1 CLI

```bash
go run ./cmd/backtest run \
  --data data/candles_BTC_JPY_PT15M.csv \
  --data-htf data/candles_BTC_JPY_PT1H.csv \
  --from 2026-01-01 \
  --to 2026-04-01 \
  --initial-balance 100000

go run ./cmd/backtest run ... --spread 0.1 --carrying-cost 0.04
go run ./cmd/backtest run ... --output results/
```

### 5.2 APIエンドポイントと永続化

```
POST /api/v1/backtest/run
GET  /api/v1/backtest/results?limit=20&offset=0&sort=created_at:desc
GET  /api/v1/backtest/results/:id
```

`/backtest/results` の保存仕様:

- 保存先: SQLite（`backtest_results`, `backtest_trades`）
- 結果ID: ULID文字列（時系列ソート可能）
- 一覧ソート: `created_at DESC` 固定（同時刻は `id DESC`）
- 保持期間: 180日（設定で変更可能、期限切れはバッチ削除）
- `GET /results/:id`: サマリー + トレード一覧を返す。未存在は `404`

CLIとAPIは同じ `BacktestRunner` を使用する。

### 5.3 出力と指標定義（固定仕様）

サマリー表示例:

```
=== Backtest Result ===
Period:           2026-01-01 ~ 2026-04-01
Initial Balance:  ¥100,000
Final Balance:    ¥112,345
Total Return:     +12.35%
Total Trades:     48
Win Rate:         62.5% (30W / 18L)
Profit Factor:    1.85
Max Drawdown:     -8.2% (¥91,800)
Sharpe Ratio:     1.42
Avg Hold Time:    4h 23m
Carrying Cost:    ¥1,234
Spread Cost:      ¥567
```

評価指標の計算定義:

- `Total Return`: `(final_balance - initial_balance) / initial_balance`
- `Sharpe Ratio`:
  - リターン系列: **日次の資産曲線終値**から算出した日次リターン
  - リスクフリー率: `0.0` 固定
  - 年率化: `sqrt(365)`（365日基準）
- `Max Drawdown`:
  - 基準曲線: **15分足クローズ時点の評価資産（実現+含み）**
  - `peak-to-trough` の最大下落率
- `Avg Hold Time`:
  - `exit_timestamp - entry_timestamp`（秒）
  - 集計は全クローズドトレードの単純平均

トレードCSV:

```csv
trade_id,entry_time,exit_time,side,entry_price,exit_price,amount,pnl,pnl_percent,carrying_cost,spread_cost,reason_entry,reason_exit
1,2026-01-05T10:00:00+09:00,2026-01-05T14:30:00+09:00,BUY,15000000,15120000,0.01,1200,0.80,24,30,trend_follow_ema_cross,take_profit
```

## 6. パラメータ最適化（Phase 2）

### 6.1 最適化対象

| カテゴリ | パラメータ | デフォルト | 探索例 |
|---|---|---|---|
| リスク | StopLossPercent | 5% | 1-10% |
| リスク | TakeProfitPercent | 10% | 3-20% |
| シグナル | MinConfidence | 0.3 | 0.1-0.7 |
| Stance | RSI閾値 | 25/75 | 20-35 / 65-80 |
| 指標 | EMA短期/長期 | 12/26 | 8-20 / 20-50 |

### 6.2 探索戦略（組み合わせ爆発対策）

- Phase 2a: ランダムサーチ（粗探索）
- Phase 2b: 上位N件近傍のみグリッド（局所探索）
- 1ジョブあたりの最大評価件数を設定（例: 10,000）
- スコアは `Sharpe` と `MaxDD` の複合条件でランキング

### 6.3 CLI

```bash
go run ./cmd/backtest optimize \
  --data data/candles_BTC_JPY_PT15M.csv \
  --data-htf data/candles_BTC_JPY_PT1H.csv \
  --param "stop_loss_percent=1:10:1" \
  --param "take_profit_percent=3:20:1" \
  --param "min_confidence=0.1:0.7:0.1" \
  --sort-by sharpe_ratio \
  --top 10 \
  --workers 8
```

## 7. ディレクトリ構成

```
backend/
├── cmd/
│   └── backtest/
│       └── main.go
├── internal/
│   ├── domain/
│   │   └── entity/
│   │       ├── backtest.go
│   │       └── backtest_event.go       # イベント型定義
│   ├── usecase/
│   │   ├── backtest/
│   │   │   ├── engine.go
│   │   │   ├── event_bus.go
│   │   │   ├── handler.go
│   │   │   ├── optimizer.go
│   │   │   └── reporter.go
│   │   ├── strategy.go                 # At系メソッド追加
│   │   ├── risk.go                     # At系メソッド追加
│   │   └── stance.go                   # backtest設定の注入対応
│   ├── infrastructure/
│   │   ├── backtest/
│   │   │   ├── simulator.go
│   │   │   └── result_repository.go
│   │   ├── csv/
│   │   │   └── candle_csv.go
│   │   ├── indicator/
│   │   └── database/
│   └── interfaces/
│       └── api/handler/backtest.go
└── data/
```

## 8. 既存コードへの影響

### 変更が必要なファイル

| ファイル | 変更内容 |
|---|---|
| `interfaces/api/router.go` | バックテストAPIルート追加 |
| `usecase/strategy.go` | `EvaluateWithHigherTFAt` 追加（既存メソッドは互換維持） |
| `usecase/risk.go` | `CheckOrderAt`, `RecordConsecutiveLossAt` 追加 |
| `usecase/stance.go` | バックテスト時のoverride無効化設定 |
| `.gitignore` | `backend/data/` と結果出力を追加 |

### 互換性ポリシー

- 既存の本番エントリポイント（`cmd/main.go`, `cmd/pipeline.go`）は挙動互換を維持
- 既存メソッドは残し、新メソッドを追加してバックテスト側のみ利用
- `infrastructure/indicator/*` の計算ロジックは変更しない

## 9. テスト戦略

| テスト対象 | テスト方法 |
|---|---|
| EventBus | FIFO・priority・連鎖順序の決定論テスト |
| IndicatorHandler | PT15M/PT1H同期・先読み防止（未来HTFを参照しない） |
| StrategyHandler | `IndicatorEvent` 契約どおりに `StrategyEngine` へ入力されること |
| RiskHandler | クールダウンがイベント時刻基準で再現すること |
| SimExecutor | 同一バーSL/TP両ヒットで `worst-case` が適用されること |
| Metrics | Sharpe/MaxDD/AvgHoldTime が定義どおり算出されること |
| API | `/backtest/results` の保存・一覧順・ID検索 |

## 10. 将来拡張

- `SL/TP priority mode` に `best-case` を追加（比較分析用）
- ウォークフォワード分析（学習/検証分離）
- モンテカルロシミュレーション
- ~~`EventEngine + LiveSource + RealExecutor` による本番置換~~ → ✅ 実装済み（PR #78-#81）

### 実装済み将来スコープ（2026-04-15）

| 項目 | PR | 概要 |
|---|---|---|
| EventEngine 共通化 | #78 | `usecase/eventengine/` に EventBus/EventEngine/OrderExecutor を抽出 |
| フロントエンド可視化 | #77, #79 | バックテスト結果一覧・詳細ページ + エクイティカーブチャート |
| LiveSource + RealExecutor | #80 | リアルタイムティッカー→イベント変換 + 楽天API実注文ExecutorD |
| イベント駆動パイプライン | #81 | 60秒ポーリング → EventEngine + LiveSource 駆動に置換 |
