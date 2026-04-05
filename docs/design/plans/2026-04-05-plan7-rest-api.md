# Plan 7: REST API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ボットの状態確認、リスク管理パラメータ変更、テクニカル指標取得、LLM戦略方針確認のためのREST APIを構築する。MCP Serverは後続のPlan 8で統合時に追加する。

**Architecture:** GinルーターとハンドラーをClean Architectureのインターフェース層に配置。各ハンドラーはユースケース層（RiskManager, LLMService, IndicatorCalculator）を直接利用する。ボットの起動/停止はPlan 8のTrading Engineに依存するため、このPlanではステータス確認のみ実装する。

**Tech Stack:** Go 1.25, gin-gonic/gin (既にgo.modに含まれる)

---

## ファイル構成

```
backend/
├── internal/
│   └── interfaces/
│       └── api/
│           ├── router.go                            # Ginルーター設定
│           ├── handler/
│           │   ├── status.go                        # GET /status
│           │   ├── risk.go                          # GET/PUT /config, GET /pnl
│           │   ├── strategy.go                      # GET /strategy
│           │   └── indicator.go                     # GET /indicators/:symbol
│           └── api_test.go                          # 全ハンドラーの統合テスト
```

---

### Task 1: ルーターとステータスハンドラー

**Files:**
- Create: `backend/internal/interfaces/api/router.go`
- Create: `backend/internal/interfaces/api/handler/status.go`

- [ ] **Step 1: status.go を作成**

```go
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type StatusHandler struct {
	riskMgr *usecase.RiskManager
}

func NewStatusHandler(riskMgr *usecase.RiskManager) *StatusHandler {
	return &StatusHandler{riskMgr: riskMgr}
}

// GetStatus はボットの稼働状態とリスク管理状態を返す。
func (h *StatusHandler) GetStatus(c *gin.Context) {
	status := h.riskMgr.GetStatus()
	c.JSON(http.StatusOK, gin.H{
		"status":        "running",
		"tradingHalted": status.TradingHalted,
		"balance":       status.Balance,
		"dailyLoss":     status.DailyLoss,
		"totalPosition": status.TotalPosition,
	})
}
```

- [ ] **Step 2: router.go を作成**

```go
package api

import (
	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/interfaces/api/handler"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type Dependencies struct {
	RiskManager         *usecase.RiskManager
	LLMService          *usecase.LLMService
	IndicatorCalculator *usecase.IndicatorCalculator
}

func NewRouter(deps Dependencies) *gin.Engine {
	r := gin.Default()

	v1 := r.Group("/api/v1")

	statusHandler := handler.NewStatusHandler(deps.RiskManager)
	v1.GET("/status", statusHandler.GetStatus)

	riskHandler := handler.NewRiskHandler(deps.RiskManager)
	v1.GET("/config", riskHandler.GetConfig)
	v1.PUT("/config", riskHandler.UpdateConfig)
	v1.GET("/pnl", riskHandler.GetPnL)

	strategyHandler := handler.NewStrategyHandler(deps.LLMService)
	v1.GET("/strategy", strategyHandler.GetStrategy)

	indicatorHandler := handler.NewIndicatorHandler(deps.IndicatorCalculator)
	v1.GET("/indicators/:symbol", indicatorHandler.GetIndicators)

	return r
}
```

- [ ] **Step 3: ビルド確認**

```bash
cd backend && go build ./...
```

（他のハンドラーがまだないのでコンパイルエラーになる場合はTask 2以降で解決）

- [ ] **Step 4: コミット**

```bash
git add backend/internal/interfaces/api/
git commit -m "feat: add REST API router and status handler"
```

---

### Task 2: リスク管理ハンドラー (config, pnl)

**Files:**
- Create: `backend/internal/interfaces/api/handler/risk.go`

- [ ] **Step 1: risk.go を作成**

```go
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type RiskHandler struct {
	riskMgr *usecase.RiskManager
}

func NewRiskHandler(riskMgr *usecase.RiskManager) *RiskHandler {
	return &RiskHandler{riskMgr: riskMgr}
}

// GetConfig はリスク管理パラメータを返す。
func (h *RiskHandler) GetConfig(c *gin.Context) {
	status := h.riskMgr.GetStatus()
	c.JSON(http.StatusOK, status.Config)
}

// UpdateConfig はリスク管理パラメータを更新する。
func (h *RiskHandler) UpdateConfig(c *gin.Context) {
	var req entity.RiskConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.riskMgr.UpdateConfig(req)
	c.JSON(http.StatusOK, req)
}

// GetPnL は損益情報を返す。
func (h *RiskHandler) GetPnL(c *gin.Context) {
	status := h.riskMgr.GetStatus()
	c.JSON(http.StatusOK, gin.H{
		"balance":       status.Balance,
		"dailyLoss":     status.DailyLoss,
		"totalPosition": status.TotalPosition,
		"tradingHalted": status.TradingHalted,
	})
}
```

- [ ] **Step 2: コミット**

```bash
git add backend/internal/interfaces/api/handler/risk.go
git commit -m "feat: add risk config and PnL handlers"
```

---

### Task 3: 戦略・指標ハンドラー

**Files:**
- Create: `backend/internal/interfaces/api/handler/strategy.go`
- Create: `backend/internal/interfaces/api/handler/indicator.go`

- [ ] **Step 1: strategy.go を作成**

```go
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type StrategyHandler struct {
	llmService *usecase.LLMService
}

func NewStrategyHandler(llmService *usecase.LLMService) *StrategyHandler {
	return &StrategyHandler{llmService: llmService}
}

// GetStrategy は現在キャッシュされているLLM戦略方針を返す。
func (h *StrategyHandler) GetStrategy(c *gin.Context) {
	// キャッシュから取得（LLM呼び出しなし）。symbolID=0で全体方針を返す。
	// 実運用ではsymbolIDをクエリパラメータで指定する。
	advice := h.llmService.GetCachedAdvice(0)
	if advice == nil {
		c.JSON(http.StatusOK, gin.H{
			"stance":    "NONE",
			"reasoning": "no strategy advice cached yet",
		})
		return
	}
	c.JSON(http.StatusOK, advice)
}
```

- [ ] **Step 2: indicator.go を作成**

```go
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type IndicatorHandler struct {
	calculator *usecase.IndicatorCalculator
}

func NewIndicatorHandler(calculator *usecase.IndicatorCalculator) *IndicatorHandler {
	return &IndicatorHandler{calculator: calculator}
}

// GetIndicators は銘柄のテクニカル指標を返す。
func (h *IndicatorHandler) GetIndicators(c *gin.Context) {
	symbolStr := c.Param("symbol")
	symbolID, err := strconv.ParseInt(symbolStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid symbol ID"})
		return
	}

	interval := c.DefaultQuery("interval", "15min")

	indicators, err := h.calculator.Calculate(c.Request.Context(), symbolID, interval)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, indicators)
}
```

- [ ] **Step 3: ビルド確認**

```bash
cd backend && go build ./...
```

- [ ] **Step 4: コミット**

```bash
git add backend/internal/interfaces/api/handler/strategy.go backend/internal/interfaces/api/handler/indicator.go
git commit -m "feat: add strategy and indicator handlers"
```

---

### Task 4: 統合テスト

**Files:**
- Create: `backend/internal/interfaces/api/api_test.go`

- [ ] **Step 1: api_test.go を作成**

```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

func setupRouter() *httptest.Server {
	riskMgr := usecase.NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      5000,
		StopLossPercent:   5,
		InitialCapital:    10000,
	})

	// LLMServiceにはnilクライアントでもGetCachedAdviceは動作する
	llmSvc := usecase.NewLLMService(nil, 15*time.Minute)

	// IndicatorCalculatorにはnilリポジトリ（テストではエンドポイントのルーティングのみ確認）
	deps := Dependencies{
		RiskManager:         riskMgr,
		LLMService:          llmSvc,
		IndicatorCalculator: nil,
	}

	router := NewRouter(deps)
	return httptest.NewServer(router)
}

func TestGetStatus(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/status")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "running" {
		t.Fatalf("expected status 'running', got %v", body["status"])
	}
}

func TestGetConfig(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/config")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var config entity.RiskConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if config.MaxPositionAmount != 5000 {
		t.Fatalf("expected maxPositionAmount 5000, got %f", config.MaxPositionAmount)
	}
}

func TestUpdateConfig(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	newConfig := entity.RiskConfig{
		MaxPositionAmount: 8000,
		MaxDailyLoss:      3000,
		StopLossPercent:   3,
		InitialCapital:    10000,
	}
	body, _ := json.Marshal(newConfig)

	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// 確認: GETで変更が反映されていること
	resp2, _ := http.Get(ts.URL + "/api/v1/config")
	defer resp2.Body.Close()

	var updated entity.RiskConfig
	json.NewDecoder(resp2.Body).Decode(&updated)
	if updated.MaxPositionAmount != 8000 {
		t.Fatalf("expected updated maxPositionAmount 8000, got %f", updated.MaxPositionAmount)
	}
}

func TestGetPnL(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/pnl")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["balance"] != float64(10000) {
		t.Fatalf("expected balance 10000, got %v", body["balance"])
	}
}

func TestGetStrategy_NoCached(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/strategy")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["stance"] != "NONE" {
		t.Fatalf("expected stance NONE, got %v", body["stance"])
	}
}

func TestGetIndicators_InvalidSymbol(t *testing.T) {
	ts := setupRouter()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/indicators/abc")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: テスト実行**

```bash
cd backend && go test ./internal/interfaces/api/ -v
```

Expected: 全6テストPASS

- [ ] **Step 3: 全テスト実行**

```bash
cd backend && go test ./... -v
```

Expected: 全テストPASS

- [ ] **Step 4: コミット**

```bash
git add backend/internal/interfaces/api/api_test.go
git commit -m "test: add REST API integration tests"
```

---

### Task 5: 設計書更新

**Files:**
- Modify: `docs/design/2026-04-02-auto-trading-system-design.md`

- [ ] **Step 1: 設計書の実装進捗を更新**

Plan 7の行を更新:

```markdown
| Plan 7 | REST API | #TBD | merged | `interfaces/api/` |
```

- [ ] **Step 2: コミット**

```bash
git add docs/design/2026-04-02-auto-trading-system-design.md docs/design/plans/2026-04-05-plan7-rest-api.md
git commit -m "docs: update implementation progress for Plan 7"
```
