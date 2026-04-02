# Plan 1: 楽天API クライアント Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 楽天ウォレット証拠金取引所APIのREST + WebSocketクライアントを実装し、認証・Rate Limitを含む基盤を構築する

**Architecture:** Clean Architectureのインフラ層に `infrastructure/rakuten/` パッケージとして実装。REST クライアントは認証ヘッダー生成・Rate Limit制御を内包し、WebSocket クライアントはgoroutineで接続を維持しchannelでデータを配信する。ドメイン層にAPIレスポンスに対応するエンティティを定義する。

**Tech Stack:** Go 1.21, `net/http`, `crypto/hmac`, `nhooyr.io/websocket`, `encoding/json`

---

## ファイル構成

```
backend/
├── go.mod                                          # 依存追加
├── internal/
│   ├── domain/
│   │   └── entity/
│   │       ├── ticker.go                          # ティッカー
│   │       ├── candle.go                          # ローソク足
│   │       ├── orderbook.go                       # 板情報
│   │       ├── trade.go                           # 歩み値
│   │       ├── symbol.go                          # 銘柄
│   │       ├── order.go                           # 注文
│   │       ├── position.go                        # ポジション
│   │       └── asset.go                           # 残高
│   └── infrastructure/
│       └── rakuten/
│           ├── auth.go                            # 認証ヘッダー生成
│           ├── auth_test.go
│           ├── rest_client.go                     # RESTクライアント本体
│           ├── rest_client_test.go
│           ├── public_api.go                      # Public APIメソッド群
│           ├── public_api_test.go
│           ├── private_api.go                     # Private APIメソッド群
│           ├── private_api_test.go
│           ├── ws_client.go                       # WebSocketクライアント
│           └── ws_client_test.go
└── config/
    └── config.go                                  # 楽天API設定追加
```

---

### Task 1: プロジェクト基盤の整理

**Files:**
- Modify: `backend/go.mod`
- Delete: `backend/internal/domain/entity.go`
- Delete: `backend/internal/domain/repository.go`
- Delete: `backend/internal/infrastructure/repository/user_repository.go`
- Delete: `backend/internal/interfaces/handler/user_handler.go`
- Delete: `backend/internal/usecase/user_usecase.go`
- Delete: `backend/internal/infrastructure/external/api_client.go`
- Modify: `backend/cmd/main.go`
- Modify: `backend/config/config.go`

- [ ] **Step 1: サンプルコードを削除**

サンプル用の User 関連コードを削除する。

```bash
cd backend
rm -f internal/domain/entity.go
rm -f internal/domain/repository.go
rm -rf internal/infrastructure/repository
rm -rf internal/infrastructure/external
rm -rf internal/interfaces/handler
rm -rf internal/usecase
```

- [ ] **Step 2: ドメインエンティティのディレクトリを作成**

```bash
mkdir -p internal/domain/entity
mkdir -p internal/domain/repository
mkdir -p internal/infrastructure/rakuten
```

- [ ] **Step 3: go.mod を更新**

`backend/go.mod` を以下に置き換える:

```go
module github.com/yui666a/rakuten-api-leverage-exchange/backend

go 1.21

require (
	github.com/gin-gonic/gin v1.10.0
	nhooyr.io/websocket v1.8.11
)
```

依存を取得:

```bash
cd backend
go mod tidy
```

- [ ] **Step 4: config.go に楽天API設定を追加**

`backend/config/config.go` を以下に置き換える:

```go
package config

import "os"

type Config struct {
	Server  ServerConfig
	Rakuten RakutenConfig
}

type ServerConfig struct {
	Port string
}

type RakutenConfig struct {
	BaseURL   string
	WSURL     string
	APIKey    string
	APISecret string
}

func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "8080"),
		},
		Rakuten: RakutenConfig{
			BaseURL:   getEnv("RAKUTEN_API_BASE_URL", "https://exchange.rakuten-wallet.co.jp"),
			WSURL:     getEnv("RAKUTEN_WS_URL", "wss://exchange.rakuten-wallet.co.jp/ws"),
			APIKey:    getEnv("RAKUTEN_API_KEY", ""),
			APISecret: getEnv("RAKUTEN_API_SECRET", ""),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
```

- [ ] **Step 5: main.go を最小限に整理**

`backend/cmd/main.go` を以下に置き換える:

```go
package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/config"
)

func main() {
	cfg := config.Load()

	r := gin.Default()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})

	log.Printf("Server starting on :%s", cfg.Server.Port)
	if err := r.Run(":" + cfg.Server.Port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
```

- [ ] **Step 6: ビルド確認**

```bash
cd backend
go build ./...
```

Expected: ビルド成功、エラーなし

- [ ] **Step 7: コミット**

```bash
git add -A
git commit -m "refactor: remove sample code and set up project structure for trading system"
```

---

### Task 2: ドメインエンティティの定義

**Files:**
- Create: `backend/internal/domain/entity/symbol.go`
- Create: `backend/internal/domain/entity/ticker.go`
- Create: `backend/internal/domain/entity/orderbook.go`
- Create: `backend/internal/domain/entity/candle.go`
- Create: `backend/internal/domain/entity/trade.go`
- Create: `backend/internal/domain/entity/asset.go`
- Create: `backend/internal/domain/entity/order.go`
- Create: `backend/internal/domain/entity/position.go`

- [ ] **Step 1: symbol.go を作成**

```go
package entity

type Symbol struct {
	ID                   int64   `json:"id"`
	Authority            string  `json:"authority"`
	TradeType            string  `json:"tradeType"`
	CurrencyPair         string  `json:"currencyPair"`
	BaseCurrency         string  `json:"baseCurrency"`
	QuoteCurrency        string  `json:"quoteCurrency"`
	BaseScale            int     `json:"baseScale"`
	QuoteScale           int     `json:"quoteScale"`
	BaseStepAmount       float64 `json:"baseStepAmount"`
	MinOrderAmount       float64 `json:"minOrderAmount"`
	MaxOrderAmount       float64 `json:"maxOrderAmount"`
	MakerTradeFeePercent float64 `json:"makerTradeFeePercent"`
	TakerTradeFeePercent float64 `json:"takerTradeFeePercent"`
	CloseOnly            bool    `json:"closeOnly"`
	ViewOnly             bool    `json:"viewOnly"`
	Enabled              bool    `json:"enabled"`
}
```

- [ ] **Step 2: ticker.go を作成**

```go
package entity

type Ticker struct {
	SymbolID  int64   `json:"symbolId"`
	BestAsk   float64 `json:"bestAsk"`
	BestBid   float64 `json:"bestBid"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Last      float64 `json:"last"`
	Volume    float64 `json:"volume"`
	Timestamp int64   `json:"timestamp"`
}
```

- [ ] **Step 3: orderbook.go を作成**

```go
package entity

type OrderbookEntry struct {
	Price  float64 `json:"price"`
	Amount float64 `json:"amount"`
}

type Orderbook struct {
	SymbolID  int64            `json:"symbolId"`
	Asks      []OrderbookEntry `json:"asks"`
	Bids      []OrderbookEntry `json:"bids"`
	BestAsk   float64          `json:"bestAsk"`
	BestBid   float64          `json:"bestBid"`
	MidPrice  float64          `json:"midPrice"`
	Spread    float64          `json:"spread"`
	Timestamp int64            `json:"timestamp"`
}
```

- [ ] **Step 4: candle.go を作成**

```go
package entity

type Candle struct {
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
	Time   int64   `json:"time"`
}

type CandlestickResponse struct {
	SymbolID     int64    `json:"symbolId"`
	Candlesticks []Candle `json:"candlesticks"`
	Timestamp    int64    `json:"timestamp"`
}
```

- [ ] **Step 5: trade.go を作成**

```go
package entity

type MarketTrade struct {
	ID          int64   `json:"id"`
	OrderSide   string  `json:"orderSide"`
	Price       float64 `json:"price"`
	Amount      float64 `json:"amount"`
	AssetAmount float64 `json:"assetAmount"`
	TradedAt    int64   `json:"tradedAt"`
}

type MarketTradesResponse struct {
	SymbolID  int64         `json:"symbolId"`
	Trades    []MarketTrade `json:"trades"`
	Timestamp int64         `json:"timestamp"`
}
```

- [ ] **Step 6: asset.go を作成**

```go
package entity

type Asset struct {
	Currency     string  `json:"currency"`
	OnhandAmount float64 `json:"onhandAmount"`
}
```

- [ ] **Step 7: order.go を作成**

```go
package entity

type OrderSide string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"
)

type OrderType string

const (
	OrderTypeMarket OrderType = "MARKET"
	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeStop   OrderType = "STOP"
)

type OrderPattern string

const (
	OrderPatternNormal OrderPattern = "NORMAL"
	OrderPatternOCO    OrderPattern = "OCO"
	OrderPatternIFD    OrderPattern = "IFD"
	OrderPatternIFDOCO OrderPattern = "IFD_OCO"
)

type OrderBehavior string

const (
	OrderBehaviorOpen  OrderBehavior = "OPEN"
	OrderBehaviorClose OrderBehavior = "CLOSE"
)

type OrderStatus string

const (
	OrderStatusWorkingOrder OrderStatus = "WORKING_ORDER"
	OrderStatusPartialFill  OrderStatus = "PARTIAL_FILL"
)

type OrderRequest struct {
	SymbolID     int64        `json:"symbolId"`
	OrderPattern OrderPattern `json:"orderPattern"`
	OrderData    OrderData    `json:"orderData"`
}

type OrderData struct {
	OrderBehavior      OrderBehavior `json:"orderBehavior"`
	PositionID         *int64        `json:"positionId,omitempty"`
	OrderSide          OrderSide     `json:"orderSide"`
	OrderType          OrderType     `json:"orderType"`
	Price              *float64      `json:"price,omitempty"`
	Amount             float64       `json:"amount"`
	OrderExpire        *int64        `json:"orderExpire,omitempty"`
	Leverage           *float64      `json:"leverage,omitempty"`
	CloseBehavior      *string       `json:"closeBehavior,omitempty"`
	PostOnly           *bool         `json:"postOnly,omitempty"`
	IFDCloseLimitPrice *float64      `json:"ifdCloseLimitPrice,omitempty"`
	IFDCloseStopPrice  *float64      `json:"ifdCloseStopPrice,omitempty"`
}

type Order struct {
	ID              int64         `json:"id"`
	SymbolID        int64         `json:"symbolId"`
	OrderBehavior   OrderBehavior `json:"orderBehavior"`
	OrderSide       OrderSide     `json:"orderSide"`
	OrderPattern    OrderPattern  `json:"orderPattern"`
	OrderType       OrderType     `json:"orderType"`
	Price           float64       `json:"price"`
	Amount          float64       `json:"amount"`
	RemainingAmount float64       `json:"remainingAmount"`
	OrderStatus     OrderStatus   `json:"orderStatus"`
	Leverage        float64       `json:"leverage"`
	OrderCreatedAt  int64         `json:"orderCreatedAt"`
}
```

- [ ] **Step 8: position.go を作成**

```go
package entity

type PositionStatus string

const (
	PositionStatusOpen            PositionStatus = "OPEN"
	PositionStatusPartiallyClosed PositionStatus = "PARTIALLY_CLOSED"
)

type Position struct {
	ID              int64          `json:"id"`
	SymbolID        int64          `json:"symbolId"`
	PositionStatus  PositionStatus `json:"positionStatus"`
	OrderSide       OrderSide      `json:"orderSide"`
	Price           float64        `json:"price"`
	Amount          float64        `json:"amount"`
	RemainingAmount float64        `json:"remainingAmount"`
	Leverage        float64        `json:"leverage"`
	FloatingProfit  float64        `json:"floatingProfit"`
	Profit          float64        `json:"profit"`
	BestPrice       float64        `json:"bestPrice"`
	OrderID         int64          `json:"orderId"`
	CreatedAt       int64          `json:"createdAt"`
}
```

- [ ] **Step 9: ビルド確認**

```bash
cd backend
go build ./...
```

Expected: ビルド成功

- [ ] **Step 10: コミット**

```bash
git add -A
git commit -m "feat: add domain entities for Rakuten Wallet API"
```

---

### Task 3: 認証モジュール

**Files:**
- Create: `backend/internal/infrastructure/rakuten/auth.go`
- Create: `backend/internal/infrastructure/rakuten/auth_test.go`

- [ ] **Step 1: auth_test.go のテストを書く**

```go
package rakuten

import (
	"testing"
)

func TestGenerateSignature_GET(t *testing.T) {
	secret := "test-secret-key"
	nonce := "1586345939000"
	uri := "/api/v1/ticker"
	queryString := "symbolId=7"

	sig := GenerateSignatureForGET(secret, nonce, uri, queryString)

	if sig == "" {
		t.Fatal("signature should not be empty")
	}

	// 同じ入力で同じ出力が得られることを確認
	sig2 := GenerateSignatureForGET(secret, nonce, uri, queryString)
	if sig != sig2 {
		t.Fatalf("signatures should be deterministic: got %s and %s", sig, sig2)
	}
}

func TestGenerateSignature_GET_NoQuery(t *testing.T) {
	secret := "test-secret-key"
	nonce := "1586345939000"
	uri := "/api/v1/asset"

	sig := GenerateSignatureForGET(secret, nonce, uri, "")

	if sig == "" {
		t.Fatal("signature should not be empty")
	}
}

func TestGenerateSignature_POST(t *testing.T) {
	secret := "test-secret-key"
	nonce := "1586345939000"
	body := `{"symbolId":7,"orderPattern":"NORMAL"}`

	sig := GenerateSignatureForPOST(secret, nonce, body)

	if sig == "" {
		t.Fatal("signature should not be empty")
	}

	sig2 := GenerateSignatureForPOST(secret, nonce, body)
	if sig != sig2 {
		t.Fatalf("signatures should be deterministic: got %s and %s", sig, sig2)
	}
}

func TestGenerateSignature_DifferentInputs(t *testing.T) {
	secret := "test-secret-key"

	sig1 := GenerateSignatureForGET(secret, "1000", "/api/v1/ticker", "symbolId=7")
	sig2 := GenerateSignatureForGET(secret, "2000", "/api/v1/ticker", "symbolId=7")

	if sig1 == sig2 {
		t.Fatal("different nonces should produce different signatures")
	}
}

func TestGenerateHeaders(t *testing.T) {
	apiKey := "my-api-key"
	apiSecret := "my-api-secret"

	headers := GenerateAuthHeaders(apiKey, apiSecret, "GET", "/api/v1/ticker", "symbolId=7", "")

	if headers["API-KEY"] != apiKey {
		t.Fatalf("API-KEY should be %s, got %s", apiKey, headers["API-KEY"])
	}

	if headers["NONCE"] == "" {
		t.Fatal("NONCE should not be empty")
	}

	if headers["SIGNATURE"] == "" {
		t.Fatal("SIGNATURE should not be empty")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd backend
go test ./internal/infrastructure/rakuten/ -v -run TestGenerate
```

Expected: コンパイルエラー（関数が未定義）

- [ ] **Step 3: auth.go を実装**

```go
package rakuten

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// GenerateSignatureForGET はGET/DELETEリクエスト用のSIGNATUREを生成する。
// 署名対象: NONCE + URI + queryString
func GenerateSignatureForGET(secret, nonce, uri, queryString string) string {
	message := nonce + uri
	if queryString != "" {
		message += "?" + queryString
	}
	return computeHMACSHA256(secret, message)
}

// GenerateSignatureForPOST はPOST/PUTリクエスト用のSIGNATUREを生成する。
// 署名対象: NONCE + JSON body
func GenerateSignatureForPOST(secret, nonce, jsonBody string) string {
	message := nonce + jsonBody
	return computeHMACSHA256(secret, message)
}

// GenerateAuthHeaders は認証に必要な3つのヘッダーを生成する。
func GenerateAuthHeaders(apiKey, apiSecret, method, uri, queryString, jsonBody string) map[string]string {
	nonce := fmt.Sprintf("%d", time.Now().UnixMilli())

	var signature string
	switch method {
	case "POST", "PUT":
		signature = GenerateSignatureForPOST(apiSecret, nonce, jsonBody)
	default:
		signature = GenerateSignatureForGET(apiSecret, nonce, uri, queryString)
	}

	return map[string]string{
		"API-KEY":   apiKey,
		"NONCE":     nonce,
		"SIGNATURE": signature,
	}
}

func computeHMACSHA256(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd backend
go test ./internal/infrastructure/rakuten/ -v -run TestGenerate
```

Expected: 全テストPASS

- [ ] **Step 5: コミット**

```bash
git add -A
git commit -m "feat: add authentication module for Rakuten Wallet API"
```

---

### Task 4: RESTクライアント本体

**Files:**
- Create: `backend/internal/infrastructure/rakuten/rest_client.go`
- Create: `backend/internal/infrastructure/rakuten/rest_client_test.go`

- [ ] **Step 1: rest_client_test.go のテストを書く**

```go
package rakuten

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRESTClient_RateLimiting(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "key", "secret")

	ctx := context.Background()

	start := time.Now()
	_, _ = client.DoPublic(ctx, "GET", "/test1", "", nil)
	_, _ = client.DoPublic(ctx, "GET", "/test2", "", nil)
	elapsed := time.Since(start)

	if elapsed < 200*time.Millisecond {
		t.Fatalf("rate limiting not working: 2 requests completed in %v, expected >= 200ms", elapsed)
	}

	if callCount != 2 {
		t.Fatalf("expected 2 calls, got %d", callCount)
	}
}

func TestRESTClient_DoPublic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/ticker" {
			t.Fatalf("expected /api/v1/ticker, got %s", r.URL.Path)
		}
		if r.URL.RawQuery != "symbolId=7" {
			t.Fatalf("expected symbolId=7, got %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"symbolId":7,"last":5000000}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	ctx := context.Background()

	body, err := client.DoPublic(ctx, "GET", "/api/v1/ticker", "symbolId=7", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(body) == 0 {
		t.Fatal("body should not be empty")
	}
}

func TestRESTClient_DoPrivate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("API-KEY")
		nonce := r.Header.Get("NONCE")
		signature := r.Header.Get("SIGNATURE")

		if apiKey != "test-key" {
			t.Fatalf("expected API-KEY 'test-key', got '%s'", apiKey)
		}
		if nonce == "" {
			t.Fatal("NONCE header should not be empty")
		}
		if signature == "" {
			t.Fatal("SIGNATURE header should not be empty")
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"currency":"JPY","onhandAmount":10000}]`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "test-key", "test-secret")
	ctx := context.Background()

	body, err := client.DoPrivate(ctx, "GET", "/api/v1/asset", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(body) == 0 {
		t.Fatal("body should not be empty")
	}
}

func TestRESTClient_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":"INVALID_PARAMETER","message":"invalid symbolId"}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	ctx := context.Background()

	_, err := client.DoPublic(ctx, "GET", "/api/v1/ticker", "symbolId=999", nil)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd backend
go test ./internal/infrastructure/rakuten/ -v -run TestRESTClient
```

Expected: コンパイルエラー

- [ ] **Step 3: rest_client.go を実装**

```go
package rakuten

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type RESTClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string

	mu       sync.Mutex
	lastCall time.Time
}

func NewRESTClient(baseURL, apiKey, apiSecret string) *RESTClient {
	return &RESTClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    baseURL,
		apiKey:     apiKey,
		apiSecret:  apiSecret,
	}
}

// DoPublic は認証不要のPublic APIリクエストを実行する。
func (c *RESTClient) DoPublic(ctx context.Context, method, path, query string, body []byte) ([]byte, error) {
	return c.do(ctx, method, path, query, body, false)
}

// DoPrivate は認証付きのPrivate APIリクエストを実行する。
func (c *RESTClient) DoPrivate(ctx context.Context, method, path, query string, body []byte) ([]byte, error) {
	return c.do(ctx, method, path, query, body, true)
}

func (c *RESTClient) do(ctx context.Context, method, path, query string, body []byte, authenticated bool) ([]byte, error) {
	c.waitForRateLimit()

	url := c.baseURL + path
	if query != "" {
		url += "?" + query
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = strings.NewReader(string(body))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if authenticated {
		headers := GenerateAuthHeaders(c.apiKey, c.apiSecret, method, path, query, string(body))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// waitForRateLimit は楽天APIの200ms間隔制限を遵守する。
func (c *RESTClient) waitForRateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()

	elapsed := time.Since(c.lastCall)
	if elapsed < 200*time.Millisecond {
		time.Sleep(200*time.Millisecond - elapsed)
	}
	c.lastCall = time.Now()
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd backend
go test ./internal/infrastructure/rakuten/ -v -run TestRESTClient
```

Expected: 全テストPASS

- [ ] **Step 5: コミット**

```bash
git add -A
git commit -m "feat: add REST client with rate limiting for Rakuten Wallet API"
```

---

### Task 5: Public API メソッド群

**Files:**
- Create: `backend/internal/infrastructure/rakuten/public_api.go`
- Create: `backend/internal/infrastructure/rakuten/public_api_test.go`

- [ ] **Step 1: public_api_test.go のテストを書く**

```go
package rakuten

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSymbols(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":7,"currencyPair":"BTC_JPY","baseCurrency":"BTC","quoteCurrency":"JPY","minOrderAmount":0.0001,"maxOrderAmount":5,"enabled":true}]`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	symbols, err := client.GetSymbols(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].ID != 7 {
		t.Fatalf("expected symbol ID 7, got %d", symbols[0].ID)
	}
	if symbols[0].CurrencyPair != "BTC_JPY" {
		t.Fatalf("expected BTC_JPY, got %s", symbols[0].CurrencyPair)
	}
}

func TestGetTicker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("symbolId") != "7" {
			t.Fatalf("expected symbolId=7, got %s", r.URL.Query().Get("symbolId"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"symbolId":7,"bestAsk":5000100,"bestBid":5000000,"open":4900000,"high":5100000,"low":4800000,"last":5000050,"volume":123.45,"timestamp":1700000000000}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	ticker, err := client.GetTicker(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ticker.SymbolID != 7 {
		t.Fatalf("expected symbolId 7, got %d", ticker.SymbolID)
	}
	if ticker.Last != 5000050 {
		t.Fatalf("expected last 5000050, got %f", ticker.Last)
	}
}

func TestGetOrderbook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"symbolId":7,"asks":[{"price":5000100,"amount":0.5}],"bids":[{"price":5000000,"amount":1.0}],"bestAsk":5000100,"bestBid":5000000,"midPrice":5000050,"spread":100,"timestamp":1700000000000}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	ob, err := client.GetOrderbook(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ob.Asks) != 1 {
		t.Fatalf("expected 1 ask, got %d", len(ob.Asks))
	}
	if ob.Spread != 100 {
		t.Fatalf("expected spread 100, got %f", ob.Spread)
	}
}

func TestGetCandlestick(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("candlestickType") != "PT1M" {
			t.Fatalf("expected candlestickType=PT1M, got %s", r.URL.Query().Get("candlestickType"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"symbolId":7,"candlesticks":[{"open":5000000,"high":5010000,"low":4990000,"close":5005000,"volume":10.5,"time":1700000000000}],"timestamp":1700000000000}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	resp, err := client.GetCandlestick(context.Background(), 7, "PT1M", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Candlesticks) != 1 {
		t.Fatalf("expected 1 candle, got %d", len(resp.Candlesticks))
	}
	if resp.Candlesticks[0].Close != 5005000 {
		t.Fatalf("expected close 5005000, got %f", resp.Candlesticks[0].Close)
	}
}

func TestGetTrades(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"symbolId":7,"trades":[{"id":1,"orderSide":"BUY","price":5000000,"amount":0.1,"assetAmount":500000,"tradedAt":1700000000000}],"timestamp":1700000000000}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "", "")
	resp, err := client.GetTrades(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(resp.Trades))
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd backend
go test ./internal/infrastructure/rakuten/ -v -run "TestGet(Symbols|Ticker|Orderbook|Candlestick|Trades)"
```

Expected: コンパイルエラー

- [ ] **Step 3: public_api.go を実装**

```go
package rakuten

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func (c *RESTClient) GetSymbols(ctx context.Context) ([]entity.Symbol, error) {
	body, err := c.DoPublic(ctx, "GET", "/api/v1/cfd/symbol", "", nil)
	if err != nil {
		return nil, fmt.Errorf("GetSymbols: %w", err)
	}

	var symbols []entity.Symbol
	if err := json.Unmarshal(body, &symbols); err != nil {
		return nil, fmt.Errorf("GetSymbols unmarshal: %w", err)
	}
	return symbols, nil
}

func (c *RESTClient) GetTicker(ctx context.Context, symbolID int64) (*entity.Ticker, error) {
	query := fmt.Sprintf("symbolId=%d", symbolID)
	body, err := c.DoPublic(ctx, "GET", "/api/v1/ticker", query, nil)
	if err != nil {
		return nil, fmt.Errorf("GetTicker: %w", err)
	}

	var ticker entity.Ticker
	if err := json.Unmarshal(body, &ticker); err != nil {
		return nil, fmt.Errorf("GetTicker unmarshal: %w", err)
	}
	return &ticker, nil
}

func (c *RESTClient) GetOrderbook(ctx context.Context, symbolID int64) (*entity.Orderbook, error) {
	query := fmt.Sprintf("symbolId=%d", symbolID)
	body, err := c.DoPublic(ctx, "GET", "/api/v1/orderbook", query, nil)
	if err != nil {
		return nil, fmt.Errorf("GetOrderbook: %w", err)
	}

	var ob entity.Orderbook
	if err := json.Unmarshal(body, &ob); err != nil {
		return nil, fmt.Errorf("GetOrderbook unmarshal: %w", err)
	}
	return &ob, nil
}

func (c *RESTClient) GetCandlestick(ctx context.Context, symbolID int64, candlestickType string, dateFrom, dateTo *int64) (*entity.CandlestickResponse, error) {
	query := fmt.Sprintf("symbolId=%d&candlestickType=%s", symbolID, candlestickType)
	if dateFrom != nil {
		query += fmt.Sprintf("&dateFrom=%d", *dateFrom)
	}
	if dateTo != nil {
		query += fmt.Sprintf("&dateTo=%d", *dateTo)
	}

	body, err := c.DoPublic(ctx, "GET", "/api/v1/candlestick", query, nil)
	if err != nil {
		return nil, fmt.Errorf("GetCandlestick: %w", err)
	}

	var resp entity.CandlestickResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("GetCandlestick unmarshal: %w", err)
	}
	return &resp, nil
}

func (c *RESTClient) GetTrades(ctx context.Context, symbolID int64) (*entity.MarketTradesResponse, error) {
	query := fmt.Sprintf("symbolId=%d", symbolID)
	body, err := c.DoPublic(ctx, "GET", "/api/v1/trades", query, nil)
	if err != nil {
		return nil, fmt.Errorf("GetTrades: %w", err)
	}

	var resp entity.MarketTradesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("GetTrades unmarshal: %w", err)
	}
	return &resp, nil
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd backend
go test ./internal/infrastructure/rakuten/ -v -run "TestGet(Symbols|Ticker|Orderbook|Candlestick|Trades)"
```

Expected: 全テストPASS

- [ ] **Step 5: コミット**

```bash
git add -A
git commit -m "feat: add Public API methods (symbols, ticker, orderbook, candlestick, trades)"
```

---

### Task 6: Private API メソッド群

**Files:**
- Create: `backend/internal/infrastructure/rakuten/private_api.go`
- Create: `backend/internal/infrastructure/rakuten/private_api_test.go`

- [ ] **Step 1: private_api_test.go のテストを書く**

```go
package rakuten

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestGetAssets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"currency":"JPY","onhandAmount":10000}]`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "key", "secret")
	assets, err := client.GetAssets(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(assets))
	}
	if assets[0].OnhandAmount != 10000 {
		t.Fatalf("expected 10000, got %f", assets[0].OnhandAmount)
	}
}

func TestGetPositions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":1,"symbolId":7,"positionStatus":"OPEN","orderSide":"BUY","price":5000000,"amount":0.001,"remainingAmount":0.001,"leverage":2,"floatingProfit":100,"profit":0,"bestPrice":5000100,"orderId":10,"createdAt":1700000000000}]`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "key", "secret")
	positions, err := client.GetPositions(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	if positions[0].PositionStatus != entity.PositionStatusOpen {
		t.Fatalf("expected OPEN, got %s", positions[0].PositionStatus)
	}
}

func TestCreateOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}

		body, _ := io.ReadAll(r.Body)
		var req entity.OrderRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to unmarshal request: %v", err)
		}
		if req.SymbolID != 7 {
			t.Fatalf("expected symbolId 7, got %d", req.SymbolID)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":100,"symbolId":7,"orderBehavior":"OPEN","orderSide":"BUY","orderPattern":"NORMAL","orderType":"MARKET","price":0,"amount":0.001,"remainingAmount":0.001,"orderStatus":"WORKING_ORDER","leverage":2,"orderCreatedAt":1700000000000}]`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "key", "secret")
	orders, err := client.CreateOrder(context.Background(), entity.OrderRequest{
		SymbolID:     7,
		OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{
			OrderBehavior: entity.OrderBehaviorOpen,
			OrderSide:     entity.OrderSideBuy,
			OrderType:     entity.OrderTypeMarket,
			Amount:        0.001,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orders))
	}
	if orders[0].ID != 100 {
		t.Fatalf("expected order ID 100, got %d", orders[0].ID)
	}
}

func TestCancelOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		if r.Method != "DELETE" {
			t.Fatalf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":100,"symbolId":7,"orderStatus":"WORKING_ORDER"}]`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "key", "secret")
	orders, err := client.CancelOrder(context.Background(), 7, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orders))
	}
}

func TestGetMyTrades(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":1,"symbolId":7,"orderSide":"BUY","price":5000000,"amount":0.001,"profit":0,"fee":5,"positionFee":0,"closeTradeProfit":0,"orderId":100,"positionId":1,"createdAt":1700000000000}]`))
	}))
	defer server.Close()

	client := NewRESTClient(server.URL, "key", "secret")
	trades, err := client.GetMyTrades(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
}

func assertAuthHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Header.Get("API-KEY") == "" {
		t.Fatal("API-KEY header missing")
	}
	if r.Header.Get("NONCE") == "" {
		t.Fatal("NONCE header missing")
	}
	if r.Header.Get("SIGNATURE") == "" {
		t.Fatal("SIGNATURE header missing")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd backend
go test ./internal/infrastructure/rakuten/ -v -run "TestGet(Assets|Positions|MyTrades)|TestCreate|TestCancel"
```

Expected: コンパイルエラー

- [ ] **Step 3: private_api.go にMyTrade型を追加してから実装**

まず `backend/internal/domain/entity/trade.go` にMyTrade型を追加:

ファイル末尾に追記:

```go
type MyTrade struct {
	ID               int64     `json:"id"`
	SymbolID         int64     `json:"symbolId"`
	OrderSide        OrderSide `json:"orderSide"`
	Price            float64   `json:"price"`
	Amount           float64   `json:"amount"`
	Profit           float64   `json:"profit"`
	Fee              float64   `json:"fee"`
	PositionFee      float64   `json:"positionFee"`
	CloseTradeProfit float64   `json:"closeTradeProfit"`
	OrderID          int64     `json:"orderId"`
	PositionID       int64     `json:"positionId"`
	CreatedAt        int64     `json:"createdAt"`
}
```

次に `backend/internal/infrastructure/rakuten/private_api.go` を作成:

```go
package rakuten

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func (c *RESTClient) GetAssets(ctx context.Context) ([]entity.Asset, error) {
	body, err := c.DoPrivate(ctx, "GET", "/api/v1/asset", "", nil)
	if err != nil {
		return nil, fmt.Errorf("GetAssets: %w", err)
	}

	var assets []entity.Asset
	if err := json.Unmarshal(body, &assets); err != nil {
		return nil, fmt.Errorf("GetAssets unmarshal: %w", err)
	}
	return assets, nil
}

func (c *RESTClient) GetPositions(ctx context.Context, symbolID int64) ([]entity.Position, error) {
	query := fmt.Sprintf("symbolId=%d", symbolID)
	body, err := c.DoPrivate(ctx, "GET", "/api/v1/cfd/position", query, nil)
	if err != nil {
		return nil, fmt.Errorf("GetPositions: %w", err)
	}

	var positions []entity.Position
	if err := json.Unmarshal(body, &positions); err != nil {
		return nil, fmt.Errorf("GetPositions unmarshal: %w", err)
	}
	return positions, nil
}

func (c *RESTClient) GetOrders(ctx context.Context, symbolID int64) ([]entity.Order, error) {
	query := fmt.Sprintf("symbolId=%d", symbolID)
	body, err := c.DoPrivate(ctx, "GET", "/api/v1/cfd/order", query, nil)
	if err != nil {
		return nil, fmt.Errorf("GetOrders: %w", err)
	}

	var orders []entity.Order
	if err := json.Unmarshal(body, &orders); err != nil {
		return nil, fmt.Errorf("GetOrders unmarshal: %w", err)
	}
	return orders, nil
}

func (c *RESTClient) CreateOrder(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("CreateOrder marshal: %w", err)
	}

	body, err := c.DoPrivate(ctx, "POST", "/api/v1/cfd/order", "", reqBody)
	if err != nil {
		return nil, fmt.Errorf("CreateOrder: %w", err)
	}

	var orders []entity.Order
	if err := json.Unmarshal(body, &orders); err != nil {
		return nil, fmt.Errorf("CreateOrder unmarshal: %w", err)
	}
	return orders, nil
}

func (c *RESTClient) CancelOrder(ctx context.Context, symbolID, orderID int64) ([]entity.Order, error) {
	query := fmt.Sprintf("symbolId=%d&id=%d", symbolID, orderID)
	body, err := c.DoPrivate(ctx, "DELETE", "/api/v1/cfd/order", query, nil)
	if err != nil {
		return nil, fmt.Errorf("CancelOrder: %w", err)
	}

	var orders []entity.Order
	if err := json.Unmarshal(body, &orders); err != nil {
		return nil, fmt.Errorf("CancelOrder unmarshal: %w", err)
	}
	return orders, nil
}

func (c *RESTClient) GetMyTrades(ctx context.Context, symbolID int64) ([]entity.MyTrade, error) {
	query := fmt.Sprintf("symbolId=%d", symbolID)
	body, err := c.DoPrivate(ctx, "GET", "/api/v1/cfd/trade", query, nil)
	if err != nil {
		return nil, fmt.Errorf("GetMyTrades: %w", err)
	}

	var trades []entity.MyTrade
	if err := json.Unmarshal(body, &trades); err != nil {
		return nil, fmt.Errorf("GetMyTrades unmarshal: %w", err)
	}
	return trades, nil
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd backend
go test ./internal/infrastructure/rakuten/ -v -run "TestGet(Assets|Positions|MyTrades)|TestCreate|TestCancel"
```

Expected: 全テストPASS

- [ ] **Step 5: コミット**

```bash
git add -A
git commit -m "feat: add Private API methods (assets, positions, orders, trades)"
```

---

### Task 7: WebSocketクライアント

**Files:**
- Create: `backend/internal/infrastructure/rakuten/ws_client.go`
- Create: `backend/internal/infrastructure/rakuten/ws_client_test.go`

- [ ] **Step 1: ws_client_test.go のテストを書く**

```go
package rakuten

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestWSClient_SubscribeAndReceive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("failed to accept websocket: %v", err)
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")

		// クライアントからsubscribeメッセージを受信
		_, msg, err := c.Read(context.Background())
		if err != nil {
			return
		}
		if !strings.Contains(string(msg), `"subscribe"`) {
			t.Fatalf("expected subscribe message, got %s", string(msg))
		}

		// ティッカーデータを送信
		tickerData := `{"symbolId":7,"bestAsk":5000100,"bestBid":5000000,"last":5000050,"timestamp":1700000000000}`
		c.Write(context.Background(), websocket.MessageText, []byte(tickerData))
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewWSClient(wsURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msgCh, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	err = client.Subscribe(ctx, 7, DataTypeTicker)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	select {
	case msg := <-msgCh:
		if !strings.Contains(string(msg), `"symbolId":7`) {
			t.Fatalf("unexpected message: %s", string(msg))
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for message")
	}

	client.Close()
}

func TestWSClient_Unsubscribe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")

		// 2つのメッセージを受信 (subscribe + unsubscribe)
		_, msg1, err := c.Read(context.Background())
		if err != nil {
			return
		}
		if !strings.Contains(string(msg1), `"subscribe"`) {
			t.Fatalf("expected subscribe, got %s", string(msg1))
		}

		_, msg2, err := c.Read(context.Background())
		if err != nil {
			return
		}
		if !strings.Contains(string(msg2), `"unsubscribe"`) {
			t.Fatalf("expected unsubscribe, got %s", string(msg2))
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewWSClient(wsURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	err = client.Subscribe(ctx, 7, DataTypeTicker)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	err = client.Unsubscribe(ctx, 7, DataTypeTicker)
	if err != nil {
		t.Fatalf("failed to unsubscribe: %v", err)
	}

	client.Close()
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd backend
go test ./internal/infrastructure/rakuten/ -v -run TestWSClient
```

Expected: コンパイルエラー

- [ ] **Step 3: ws_client.go を実装**

```go
package rakuten

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"nhooyr.io/websocket"
)

type DataType string

const (
	DataTypeTicker    DataType = "TICKER"
	DataTypeOrderbook DataType = "ORDERBOOK"
	DataTypeTrades    DataType = "TRADES"
)

type wsMessage struct {
	SymbolID int64    `json:"symbolId"`
	Type     string   `json:"type"`
	Data     DataType `json:"data"`
}

type WSClient struct {
	url  string
	conn *websocket.Conn
	mu   sync.Mutex
}

func NewWSClient(url string) *WSClient {
	return &WSClient{url: url}
}

// Connect はWebSocketサーバーに接続し、受信メッセージを配信するchannelを返す。
func (c *WSClient) Connect(ctx context.Context) (<-chan []byte, error) {
	conn, _, err := websocket.Dial(ctx, c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	c.conn = conn

	msgCh := make(chan []byte, 100)

	go func() {
		defer close(msgCh)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			select {
			case msgCh <- data:
			case <-ctx.Done():
				return
			}
		}
	}()

	return msgCh, nil
}

// Subscribe は指定した銘柄・データ種別の購読を開始する。
func (c *WSClient) Subscribe(ctx context.Context, symbolID int64, dataType DataType) error {
	return c.sendMessage(ctx, symbolID, "subscribe", dataType)
}

// Unsubscribe は指定した銘柄・データ種別の購読を停止する。
func (c *WSClient) Unsubscribe(ctx context.Context, symbolID int64, dataType DataType) error {
	return c.sendMessage(ctx, symbolID, "unsubscribe", dataType)
}

// Close はWebSocket接続を閉じる。
func (c *WSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}
	return c.conn.Close(websocket.StatusNormalClosure, "")
}

func (c *WSClient) sendMessage(ctx context.Context, symbolID int64, msgType string, dataType DataType) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	msg := wsMessage{
		SymbolID: symbolID,
		Type:     msgType,
		Data:     dataType,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	return c.conn.Write(ctx, websocket.MessageText, data)
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd backend
go test ./internal/infrastructure/rakuten/ -v -run TestWSClient
```

Expected: 全テストPASS

- [ ] **Step 5: 全テストを実行して回帰がないことを確認**

```bash
cd backend
go test ./... -v
```

Expected: 全テストPASS

- [ ] **Step 6: コミット**

```bash
git add -A
git commit -m "feat: add WebSocket client for real-time market data"
```

---

### Task 8: .env.example の更新

**Files:**
- Modify: `backend/.env.example` (新規作成)

- [ ] **Step 1: backend/.env.example を作成**

```bash
# Rakuten Wallet API
RAKUTEN_API_KEY=your_api_key_here
RAKUTEN_API_SECRET=your_api_secret_here
RAKUTEN_API_BASE_URL=https://exchange.rakuten-wallet.co.jp
RAKUTEN_WS_URL=wss://exchange.rakuten-wallet.co.jp/ws

# Server
SERVER_PORT=8080
```

- [ ] **Step 2: .gitignore に backend/.env が含まれていることを確認**

既存の `.gitignore` に `.env` がすでに含まれていることを確認。

```bash
grep "\.env" .gitignore
```

Expected: `.env` がマッチする

- [ ] **Step 3: コミット**

```bash
git add backend/.env.example
git commit -m "docs: add .env.example for Rakuten Wallet API configuration"
```
