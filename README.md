# Rakuten Wallet 証拠金取引所 API アプリケーション

楽天ウォレットの証拠金取引所APIを活用したアプリケーションです。  
技術研鑽を目的として開発しています。

## 技術スタック

| レイヤー | 技術 |
|---------|------|
| Backend | Go 1.25 (Gin) / Clean Architecture / SQLite |
| Frontend | TanStack Router + React 19 + Vite 7 / TypeScript / Tailwind CSS v4 |
| Infra | Docker Compose |
| パッケージ管理 | Go Modules / pnpm |

## プロジェクト構成

```
.
├── backend/                              # バックエンド (Go)
│   ├── cmd/main.go                      #   エントリポイント
│   ├── config/                          #   設定 (環境変数)
│   └── internal/
│       ├── domain/                      #   ドメイン層 (エンティティ, リポジトリIF)
│       ├── usecase/                     #   ユースケース層 (ビジネスロジック)
│       ├── infrastructure/             #   インフラ層 (外部API, DB)
│       └── interfaces/                 #   インターフェース層 (HTTPハンドラー)
│
├── frontend/                             # フロントエンド (TypeScript)
│   ├── app/
│   │   ├── routes/                     #   ルートコンポーネント
│   │   ├── router.tsx                  #   ルーター設定
│   │   ├── client.tsx                  #   クライアントエントリ
│   │   └── ssr.tsx                     #   SSRエントリ
│   ├── app.config.ts                    #   TanStack Start 設定
│   └── package.json
│
└── .agent/                               # MCP サーバー設定
    ├── mcp.json
    └── MCP_SETUP.md
```

## セットアップ

### 前提条件

- Docker + Docker Compose（推奨）。ローカル実行する場合は Go 1.25+ / Node.js 20+ / pnpm
- `.env`（`backend/.env.example` をコピー）

### Docker Compose（推奨）

```bash
docker compose up --build -d
# Backend: http://localhost:38080  Frontend: http://localhost:33000
```

### ローカル実行

```bash
# Backend
cd backend && go mod download && go run ./cmd   # http://localhost:8080

# Frontend
cd frontend && pnpm install && pnpm dev         # http://localhost:3000
```

## 主な機能

- ルールベース自動売買（Stance: `TREND_FOLLOW` / `CONTRARIAN` / `BREAKOUT` / `HOLD`）
- テクニカル指標: SMA / EMA / RSI / MACD / Bollinger / ATR / Volume / ADX(+DI/-DI) / Stochastics / Ichimoku
- バックテスト: 単発・複数期間・Walk-Forward 最適化。結果は SQLite に永続化し、ダッシュボードからランキング・系譜を参照
- PDCA ワークフロー: プロファイル駆動（`backend/profiles/*.json`）で戦略を外部設定化し、`parentResultId` で系譜を追跡

## 参考

- [楽天ウォレット 証拠金取引所API ドキュメント](https://www.rakuten-wallet.co.jp/service/api-leverage-exchange/)

## ドキュメント

- [AGENTS.md](AGENTS.md) — プロジェクトルール・構成・コマンド（最初に読むべき一次情報）
- [Clean Architecture](docs/clean-architecture.md) — バックエンドのアーキテクチャ設計
- [API Reference](docs/api-reference.md) — 全 API エンドポイント仕様
- [Project Structure](docs/project-structure.md) — ディレクトリ構成
- [Agent Operation Guide](docs/agent-operation-guide.md) — エージェントからの運用手順
- [PDCA README](docs/pdca/README.md) / [PDCA Agent Guide](docs/pdca/agent-guide.md) — 戦略最適化サイクルの回し方

## ライセンス

MIT
