# フロントエンド ダッシュボード設計書

## 概要

AI自動売買ボットの監視・操作用ダッシュボードを構築する。残高・損益・戦略方針・テクニカル指標・ポジションを一画面で把握できるグリッドレイアウトを採用する。

## 技術スタック

| 項目 | 技術 |
|------|------|
| フレームワーク | TanStack Start (Vite + React + TypeScript) |
| スタイリング | TailwindCSS v4 |
| データ取得 | TanStack Query (ポーリング、将来WebSocket移行) |
| チャート | Lightweight Charts (TradingView製) |
| ルーティング | TanStack Router (file-based) |

## レイアウト

グリッドダッシュボード。上段にKPIカード4枚、下段にチャート（2/3幅）とサイドパネル（1/3幅）。

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

## カラーテーマ

ダークネイビーベース。

| 用途 | カラー |
|------|--------|
| 背景 | `#0f0f23` |
| カード背景 | `#1a1a3e` |
| テキスト | `#e0e0e0` |
| サブテキスト | `#666` |
| 上昇/利益 | `#00d4aa` (緑) |
| 下落/損失 | `#ff4757` (赤) |
| アクセント | `#3742fa` (青) |

## コンポーネント構成

### KPIカード (4枚)

| カード | データソース | 更新頻度 |
|--------|------------|---------|
| 残高 | `GET /api/v1/pnl` → `balance` | 10秒 |
| 日次損益 | `GET /api/v1/pnl` → `dailyLoss` | 10秒 |
| 戦略方針 | `GET /api/v1/strategy` → `stance` | 30秒 |
| ステータス | `GET /api/v1/status` → `status`, `tradingHalted` | 10秒 |

### ローソク足チャート

- ライブラリ: Lightweight Charts
- データ: 楽天API `/api/v1/candlestick` をバックエンド経由で取得
- 表示期間: 直近500本（15分足デフォルト）
- SMA20/SMA50のオーバーレイ表示

### テクニカル指標パネル

- データ: `GET /api/v1/indicators/:symbol`
- 表示項目: RSI14, SMA20, SMA50, EMA12, EMA26, MACD Line, Signal Line, Histogram
- 更新頻度: 30秒

### ポジションパネル

- データ: バックエンドに新規エンドポイント追加が必要（`GET /api/v1/positions`）
- 表示: 銘柄、方向(LONG/SHORT)、数量、約定価格、含み損益
- 更新頻度: 10秒

## データ取得戦略

TanStack Queryを使用したポーリング。将来的にWebSocketへ移行。

```
フロントエンド (TanStack Query)
  │
  ├── useQuery("status", 10s)    → GET /api/v1/status
  ├── useQuery("pnl", 10s)       → GET /api/v1/pnl
  ├── useQuery("strategy", 30s)  → GET /api/v1/strategy
  ├── useQuery("indicators", 30s)→ GET /api/v1/indicators/7
  └── useQuery("candles", 60s)   → GET /api/v1/candles/7  ※新規
```

## バックエンド追加エンドポイント

フロントエンドが必要とするが、現在未実装のエンドポイント:

| エンドポイント | 用途 |
|--------------|------|
| `GET /api/v1/candles/:symbol` | ローソク足データ取得（Lightweight Charts用） |
| `GET /api/v1/positions` | ポジション一覧（楽天APIプロキシ） |

## プロジェクト構成

```
frontend/
├── app.config.ts                    # TanStack Start設定
├── package.json
├── tsconfig.json
├── src/
│   ├── routes/
│   │   ├── __root.tsx              # ルートレイアウト
│   │   └── index.tsx               # ダッシュボード（トップページ）
│   ├── components/
│   │   ├── KpiCard.tsx             # KPIカード
│   │   ├── CandlestickChart.tsx    # ローソク足チャート
│   │   ├── IndicatorPanel.tsx      # テクニカル指標パネル
│   │   └── PositionPanel.tsx       # ポジションパネル
│   ├── hooks/
│   │   ├── useStatus.ts            # ステータス取得
│   │   ├── usePnl.ts              # 損益取得
│   │   ├── useStrategy.ts         # 戦略方針取得
│   │   ├── useIndicators.ts       # テクニカル指標取得
│   │   └── useCandles.ts          # ローソク足取得
│   ├── lib/
│   │   └── api.ts                  # APIクライアント（fetch wrapper）
│   └── styles/
│       └── app.css                 # TailwindCSS + カスタムテーマ
```

## 実装スコープ

### Phase 1（今回）
- TanStack Startプロジェクトセットアップ
- ダッシュボード画面（KPIカード、チャート、指標、ポジション）
- バックエンドにcandles/positionsエンドポイント追加
- TanStack Queryでポーリング

### Phase 2（将来）
- WebSocketによるリアルタイム更新
- 設定変更画面
- 取引履歴画面
- ボット起動/停止操作
