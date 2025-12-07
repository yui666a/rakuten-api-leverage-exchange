# Rakuten API Leverage Exchange

A trading platform application with a Gin (Golang) backend using Clean Architecture and a TanStack Start (TypeScript) frontend.

## Architecture

### Backend (Golang + Gin)
- **Clean Architecture** implementation with clear separation of concerns:
  - **Domain Layer**: Business entities and repository interfaces
  - **Usecase Layer**: Business logic and application rules
  - **Interface Layer**: HTTP controllers and API endpoints
  - **Infrastructure Layer**: External API clients and data persistence

### Frontend (TypeScript + TanStack Router)
- Modern React application with TanStack Router for file-based routing
- TypeScript for type safety
- Vite for fast development and building
- API client for backend communication

## Project Structure

```
.
├── backend/
│   ├── cmd/                    # Application entry points
│   │   └── main.go
│   ├── domain/                 # Business entities and interfaces
│   │   ├── market.go
│   │   └── repository.go
│   ├── usecase/                # Business logic
│   │   ├── market_usecase.go
│   │   └── order_usecase.go
│   ├── interface/              # Controllers and HTTP handlers
│   │   ├── controllers/
│   │   └── router.go
│   └── infrastructure/         # External integrations
│       ├── api/                # External API clients
│       └── config/             # Configuration management
│
└── frontend/
    ├── src/
    │   ├── components/         # React components
    │   ├── routes/             # TanStack Router routes
    │   ├── lib/                # Utilities and API client
    │   └── main.tsx            # Application entry point
    ├── package.json
    └── vite.config.ts
```

## Getting Started

### Prerequisites
- Go 1.21 or higher
- Node.js 18 or higher
- npm or yarn

### Backend Setup

1. Navigate to the project root:
```bash
cd rakuten-api-leverage-exchange
```

2. Copy the example environment file:
```bash
cp .env.example .env
```

3. Update the `.env` file with your Rakuten API credentials

4. Install Go dependencies:
```bash
go mod download
```

5. Build and run the backend:
```bash
go run backend/cmd/main.go
```

The backend API will be available at `http://localhost:8080`

### Frontend Setup

1. Navigate to the frontend directory:
```bash
cd frontend
```

2. Install dependencies:
```bash
npm install
```

3. Start the development server:
```bash
npm run dev
```

The frontend will be available at `http://localhost:3000`

## API Endpoints

### Markets
- `GET /api/markets` - Get all markets
- `GET /api/markets/:symbol` - Get specific market data

### Orders
- `POST /api/orders` - Create a new order
- `GET /api/orders` - Get all orders
- `GET /api/orders/:id` - Get specific order
- `DELETE /api/orders/:id` - Cancel an order

### Account
- `GET /api/accounts/:id` - Get account information

## Development

### Building Backend
```bash
go build -o backend/bin/server backend/cmd/main.go
```

### Building Frontend
```bash
cd frontend
npm run build
```

### Testing
```bash
# Backend tests
go test ./...

# Frontend tests
cd frontend
npm test
```

## Features

- Real-time market data display
- Order creation and management (buy/sell, market/limit)
- Account balance tracking
- Clean Architecture for maintainable backend
- Type-safe frontend with TypeScript
- Modern routing with TanStack Router
- CORS-enabled API for local development

## License

This project is licensed under the MIT License - see the LICENSE file for details.