# Rakuten Wallet 証拠金取引所 API アプリケーション

楽天ウォレットの証拠金取引所APIを活用したアプリケーションです。  
技術研鑽を目的として開発しています。

## 技術スタック

| レイヤー | 技術 |
|---------|------|
| Backend | Go (Gin) / Clean Architecture |
| Frontend | TanStack Start (TypeScript, React) |
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

- Go 1.21+
- Node.js 18+
- pnpm

### バックエンド

```bash
cd backend
go mod download
go run cmd/main.go
```

サーバーが `http://localhost:8080` で起動します。

### フロントエンド

```bash
cd frontend
pnpm install
pnpm dev
```

`http://localhost:3000` で起動します。

## 参考

- [楽天ウォレット 証拠金取引所API ドキュメント](https://www.rakuten-wallet.co.jp/service/api-leverage-exchange/)

## ドキュメント

- [Clean Architecture](docs/clean-architecture.md) - バックエンドのアーキテクチャ設計

## ライセンス

MIT
