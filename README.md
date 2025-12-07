# Rakuten API Leverage Exchange - Learning Project

## Tech Stack

### Backend
- **Framework**: Gin (Golang)
- **Architecture**: Clean Architecture
- **Features**: 外部API連携対応

### Frontend
- **Framework**: TanStack Start (TypeScript)
- **Package Manager**: pnpm

## Project Structure

```
.
├── backend/                    # Backend (Golang)
│   ├── cmd/                   # Application entry points
│   │   └── main.go           # Main server
│   ├── internal/             # Internal packages
│   │   ├── domain/          # Domain layer (entities, repositories)
│   │   ├── usecase/         # Use case layer (business logic)
│   │   ├── infrastructure/  # Infrastructure layer (DB, external APIs)
│   │   └── interfaces/      # Interface layer (HTTP handlers)
│   ├── config/              # Configuration
│   └── go.mod               # Go module definition
│
└── frontend/                  # Frontend (TypeScript)
    ├── app/                  # Application source
    │   ├── routes/          # Route components
    │   ├── router.tsx       # Router configuration
    │   ├── client.tsx       # Client entry
    │   └── ssr.tsx          # SSR entry
    ├── package.json         # Dependencies
    ├── tsconfig.json        # TypeScript config
    └── app.config.ts        # TanStack Start config
```

## Getting Started

### Backend

```bash
cd backend
go mod download
go run cmd/main.go
```

The server will start on `http://localhost:8080`

### Frontend

```bash
cd frontend
pnpm install
pnpm dev
```

The frontend will start on `http://localhost:3000`

## Clean Architecture Layers

### Domain Layer (`internal/domain`)
- エンティティ（ビジネスロジックの核心）
- リポジトリインターフェース

### Use Case Layer (`internal/usecase`)
- ビジネスロジックの実装
- ドメイン層に依存

### Infrastructure Layer (`internal/infrastructure`)
- リポジトリの実装
- 外部APIクライアント
- データベース接続

### Interface Layer (`internal/interfaces`)
- HTTPハンドラー
- リクエスト/レスポンスの処理

