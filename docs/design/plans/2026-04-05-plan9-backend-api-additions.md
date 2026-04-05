# Plan 9: バックエンド追加エンドポイント Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** フロントエンドダッシュボードが必要とするローソク足取得・ポジション取得・CORS対応のエンドポイントをバックエンドに追加する。

**Architecture:** 既存のREST APIルーターにcandles/positionsハンドラーを追加。楽天APIをプロキシしてフロントエンドに返す。CORSミドルウェアを追加してフロントエンド（別ポート）からのアクセスを許可する。

**Tech Stack:** Go 1.25, gin-gonic/gin, gin-contrib/cors

---

## ファイル構成

```
backend/
├── internal/
│   └── interfaces/
│       └── api/
│           ├── router.go                            # CORS追加、新ルート追加
│           ├── handler/
│           │   ├── candle.go                        # GET /candles/:symbol
│           │   └── position.go                      # GET /positions
│           └── api_test.go                          # テスト追加
```

---

### Task 1: CORSミドルウェ��追加

**Files:**
- Modify: `backend/internal/interfaces/api/router.go`

- [ ] **Step 1: gin-contrib/corsをインストール**

```bash
cd backend && go get github.com/gin-contrib/cors
```

- [ ] **Step 2: router.goにCORSを追加**

import に `"github.com/gin-contrib/cors"` を追加し、`gin.Default()` の後に CORS ミドルウェアを追加:

```go
r.Use(cors.New(cors.Config{
	AllowOrigins:     []string{"http://localhost:3000"},
	AllowMethods:     []string{"GET", "PUT", "POST", "DELETE"},
	AllowHeaders:     []string{"Content-Type"},
	AllowCredentials: true,
}))
```

- [ ] **Step 3: ビルド確認**

```bash
cd backend && go build ./...
```

- [ ] **Step 4: コミット**

```bash
git add backend/internal/interfaces/api/router.go backend/go.mod backend/go.sum
git commit -m "feat: add CORS middleware for frontend access"
```

---

### Task 2: ���ーソク足ハンドラー

**Files:**
- Create: `backend/internal/interfaces/api/handler/candle.go`
- Modify: `backend/internal/interfaces/api/router.go`

- [ ] **Step 1: candle.go を作成**

```go
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type CandleHandler struct {
	marketDataSvc *usecase.MarketDataService
}

func NewCandleHandler(marketDataSvc *usecase.MarketDataService) *CandleHandler {
	return &CandleHandler{marketDataSvc: marketDataSvc}
}

// GetCandles は銘柄のローソク足データを返す。
func (h *CandleHandler) GetCandles(c *gin.Context) {
	symbolStr := c.Param("symbol")
	symbolID, err := strconv.ParseInt(symbolStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid symbol ID"})
		return
	}

	interval := c.DefaultQuery("interval", "15min")
	limitStr := c.DefaultQuery("limit", "500")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 500 {
		limit = 500
	}

	candles, err := h.marketDataSvc.GetCandles(c.Request.Context(), symbolID, interval, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, candles)
}
```

- [ ] **Step 2: router.go の Dependencies に MarketDataService を追加し、ルートを登録**

Dependencies structに追加:
```go
MarketDataService *usecase.MarketDataService
```

NewRouter内に追加:
```go
candleHandler := handler.NewCandleHandler(deps.MarketDataService)
v1.GET("/candles/:symbol", candleHandler.GetCandles)
```

- [ ] **Step 3: ビルド確認**

```bash
cd backend && go build ./...
```

- [ ] **Step 4: コミット**

```bash
git add backend/internal/interfaces/api/handler/candle.go backend/internal/interfaces/api/router.go
git commit -m "feat: add candles endpoint for chart data"
```

---

### Task 3: ポジションハンドラー

**Files:**
- Create: `backend/internal/interfaces/api/handler/position.go`
- Modify: `backend/internal/interfaces/api/router.go`

- [ ] **Step 1: position.go を作成**

```go
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

type PositionHandler struct {
	orderClient repository.OrderClient
}

func NewPositionHandler(orderClient repository.OrderClient) *PositionHandler {
	return &PositionHandler{orderClient: orderClient}
}

// GetPositions は指定銘柄のポジション一覧を返す。
func (h *PositionHandler) GetPositions(c *gin.Context) {
	symbolStr := c.DefaultQuery("symbolId", "7")
	symbolID, err := strconv.ParseInt(symbolStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid symbol ID"})
		return
	}

	positions, err := h.orderClient.GetPositions(c.Request.Context(), symbolID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, positions)
}
```

- [ ] **Step 2: router.go の Dependencies に OrderClient を追加し、ルートを登録**

Dependencies structに追加:
```go
OrderClient repository.OrderClient
```

import に追加:
```go
"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
```

NewRouter内に追加:
```go
positionHandler := handler.NewPositionHandler(deps.OrderClient)
v1.GET("/positions", positionHandler.GetPositions)
```

- [ ] **Step 3: ビルド確認**

```bash
cd backend && go build ./...
```

- [ ] **Step 4: コミット**

```bash
git add backend/internal/interfaces/api/handler/position.go backend/internal/interfaces/api/router.go
git commit -m "feat: add positions endpoint for portfolio display"
```

---

### Task 4: テスト追加 + main.go更新

**Files:**
- Modify: `backend/internal/interfaces/api/api_test.go`
- Modify: `backend/cmd/main.go`

- [ ] **Step 1: api_test.go の setupRouter に新しい依存を追加し、テスト追加**

setupRouterのDependenciesを更新（MarketDataService, OrderClient追加）し、新エンドポイントのテストを追加:

```go
func TestGetCandles_InvalidSymbol(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/candles/abc")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetPositions_InvalidSymbol(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/positions?symbolId=abc")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: cmd/main.go の api.Dependencies に MarketDataService と OrderClient (restClient) を追加**

```go
router := api.NewRouter(api.Dependencies{
	RiskManager:         riskMgr,
	LLMService:          llmSvc,
	IndicatorCalculator: indicatorCalc,
	MarketDataService:   marketDataSvc,
	OrderClient:         restClient,
})
```

- [ ] **Step 3: テスト実行**

```bash
cd backend && go test ./... -v
```

Expected: 全テストPASS

- [ ] **Step 4: コミット**

```bash
git add backend/internal/interfaces/api/api_test.go backend/cmd/main.go
git commit -m "test: add candles and positions endpoint tests, wire main.go"
```
