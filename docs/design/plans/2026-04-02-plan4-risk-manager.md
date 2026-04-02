# Plan 4: リスク管理 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ポジション上限・日次損失上限・損切り・軍資金チェックを行うRisk Managerを構築し、すべての注文がこのゲートキーパーを通過するようにする

**Architecture:** Risk Managerはユースケース層に配置し、リスクパラメータを設定構造体で管理する。注文承認/拒否の判定ロジックは純粋関数としてテストしやすく保つ。損切り監視は独立したgoroutineで価格データを監視し、即時発動する。日次損失は毎日0時（JST）にリセットする。

**Tech Stack:** Go 1.21, sync, time

---

## ファイル構成

```
backend/
├── config/
│   └── config.go                                   # RiskConfig追加
├── internal/
│   ├── domain/
│   │   └── entity/
│   │       └── risk.go                            # リスク関連エンティティ
│   └── usecase/
│       ├── risk.go                                # Risk Manager
│       └── risk_test.go
```

---

### Task 1: リスク関連エンティティと設定

**Files:**
- Create: `backend/internal/domain/entity/risk.go`
- Modify: `backend/config/config.go`

- [ ] **Step 1: risk.go を作成**

```go
package entity

// RiskConfig はリスク管理のパラメータ。
type RiskConfig struct {
	MaxPositionAmount float64 `json:"maxPositionAmount"` // 同時ポジション上限（円）
	MaxDailyLoss      float64 `json:"maxDailyLoss"`      // 日次損失上限（円）
	StopLossPercent   float64 `json:"stopLossPercent"`    // 損切りライン（%）
	InitialCapital    float64 `json:"initialCapital"`     // 軍資金（円）
}

// OrderProposal はRisk Managerに承認を求める注文提案。
type OrderProposal struct {
	SymbolID      int64
	Side          OrderSide
	OrderType     OrderType
	Amount        float64 // 数量
	Price         float64 // 概算価格（成行の場合はBestAsk/BestBid）
	IsClose       bool    // 決済注文かどうか
	PositionID    *int64  // 決済対象ポジションID
}

// RiskCheckResult はRisk Managerの判定結果。
type RiskCheckResult struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}
```

- [ ] **Step 2: config.go に RiskConfig を追加**

Config struct に追加:

```go
type Config struct {
	Server   ServerConfig
	Rakuten  RakutenConfig
	Database DatabaseConfig
	Risk     RiskConfig
}

type RiskConfig struct {
	MaxPositionAmount float64
	MaxDailyLoss      float64
	StopLossPercent   float64
	InitialCapital    float64
}
```

Load() に追加:

```go
Risk: RiskConfig{
	MaxPositionAmount: getEnvFloat("RISK_MAX_POSITION_AMOUNT", 5000),
	MaxDailyLoss:      getEnvFloat("RISK_MAX_DAILY_LOSS", 5000),
	StopLossPercent:   getEnvFloat("RISK_STOP_LOSS_PERCENT", 5),
	InitialCapital:    getEnvFloat("RISK_INITIAL_CAPITAL", 10000),
},
```

getEnvFloat ヘルパーを追加:

```go
func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return defaultValue
}
```

import に `"strconv"` を追加。

- [ ] **Step 3: .env.example に追記**

```
# Risk Management
RISK_MAX_POSITION_AMOUNT=5000
RISK_MAX_DAILY_LOSS=5000
RISK_STOP_LOSS_PERCENT=5
RISK_INITIAL_CAPITAL=10000
```

- [ ] **Step 4: ビルド確認**

```bash
cd backend
go build ./...
```

- [ ] **Step 5: コミット**

```bash
git add -A
git commit -m "feat: add risk management entities and configuration"
```

---

### Task 2: Risk Manager コア（注文承認判定）

**Files:**
- Create: `backend/internal/usecase/risk.go`
- Create: `backend/internal/usecase/risk_test.go`

- [ ] **Step 1: risk_test.go を書く**

```go
package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func defaultRiskConfig() entity.RiskConfig {
	return entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      5000,
		StopLossPercent:   5,
		InitialCapital:    10000,
	}
}

func TestRiskManager_ApproveNewOrder(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())

	proposal := entity.OrderProposal{
		SymbolID:  7,
		Side:      entity.OrderSideBuy,
		OrderType: entity.OrderTypeMarket,
		Amount:    0.001,
		Price:     4000000, // 0.001 * 4000000 = 4000円 < 5000円上限
	}

	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("order should be approved: %s", result.Reason)
	}
}

func TestRiskManager_RejectExceedingPositionLimit(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())

	// 既存ポジションを登録: 3000円分
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 3000000, Amount: 0.001, RemainingAmount: 0.001},
	})

	proposal := entity.OrderProposal{
		SymbolID:  7,
		Side:      entity.OrderSideBuy,
		OrderType: entity.OrderTypeMarket,
		Amount:    0.001,
		Price:     3000000, // 3000円追加 → 合計6000円 > 5000円上限
	}

	result := rm.CheckOrder(context.Background(), proposal)
	if result.Approved {
		t.Fatal("order should be rejected: exceeds position limit")
	}
}

func TestRiskManager_AllowCloseOrder(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())

	// 上限ギリギリのポジション
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})

	posID := int64(1)
	proposal := entity.OrderProposal{
		SymbolID:   7,
		Side:       entity.OrderSideSell,
		OrderType:  entity.OrderTypeMarket,
		Amount:     0.001,
		Price:      5000000,
		IsClose:    true,
		PositionID: &posID,
	}

	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("close order should always be approved: %s", result.Reason)
	}
}

func TestRiskManager_RejectAfterDailyLossExceeded(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())

	// 日次損失を記録: 5000円
	rm.RecordLoss(5000)

	proposal := entity.OrderProposal{
		SymbolID:  7,
		Side:      entity.OrderSideBuy,
		OrderType: entity.OrderTypeMarket,
		Amount:    0.001,
		Price:     1000000,
	}

	result := rm.CheckOrder(context.Background(), proposal)
	if result.Approved {
		t.Fatal("order should be rejected: daily loss limit exceeded")
	}
}

func TestRiskManager_DailyLossReset(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())

	rm.RecordLoss(5000)

	// 日次リセット
	rm.ResetDailyLoss()

	proposal := entity.OrderProposal{
		SymbolID:  7,
		Side:      entity.OrderSideBuy,
		OrderType: entity.OrderTypeMarket,
		Amount:    0.001,
		Price:     1000000,
	}

	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("order should be approved after daily reset: %s", result.Reason)
	}
}

func TestRiskManager_RejectInsufficientBalance(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.InitialCapital = 100 // 軍資金100円
	rm := NewRiskManager(cfg)

	proposal := entity.OrderProposal{
		SymbolID:  7,
		Side:      entity.OrderSideBuy,
		OrderType: entity.OrderTypeMarket,
		Amount:    0.001,
		Price:     5000000, // 5000円 > 100円
	}

	result := rm.CheckOrder(context.Background(), proposal)
	if result.Approved {
		t.Fatal("order should be rejected: insufficient balance")
	}
}

func TestRiskManager_UpdateBalance(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdateBalance(20000)

	proposal := entity.OrderProposal{
		SymbolID:  7,
		Side:      entity.OrderSideBuy,
		OrderType: entity.OrderTypeMarket,
		Amount:    0.001,
		Price:     15000000, // 15000円 > 初期10000円だが、残高20000円
	}

	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("order should be approved with updated balance: %s", result.Reason)
	}
}

func TestRiskManager_CheckStopLoss(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())

	positions := []entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	}
	rm.UpdatePositions(positions)

	// 価格が5%下落: 5000000 * 0.95 = 4750000
	stopLossPositions := rm.CheckStopLoss(7, 4700000)

	if len(stopLossPositions) != 1 {
		t.Fatalf("expected 1 stop loss position, got %d", len(stopLossPositions))
	}
	if stopLossPositions[0].ID != 1 {
		t.Fatalf("expected position ID 1, got %d", stopLossPositions[0].ID)
	}
}

func TestRiskManager_CheckStopLoss_NoTrigger(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())

	positions := []entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	}
	rm.UpdatePositions(positions)

	// 価格が3%下落: まだ損切りラインに達しない
	stopLossPositions := rm.CheckStopLoss(7, 4850000)

	if len(stopLossPositions) != 0 {
		t.Fatalf("expected 0 stop loss positions, got %d", len(stopLossPositions))
	}
}

func TestRiskManager_CheckStopLoss_SellPosition(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())

	positions := []entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideSell, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	}
	rm.UpdatePositions(positions)

	// ショートポジションの場合、価格が5%上昇で損切り
	stopLossPositions := rm.CheckStopLoss(7, 5300000)

	if len(stopLossPositions) != 1 {
		t.Fatalf("expected 1 stop loss position, got %d", len(stopLossPositions))
	}
}

func TestRiskManager_GetStatus(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.RecordLoss(1000)
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, Price: 3000000, Amount: 0.001, RemainingAmount: 0.001},
	})

	status := rm.GetStatus()
	if status.DailyLoss != 1000 {
		t.Fatalf("expected daily loss 1000, got %f", status.DailyLoss)
	}
	if status.TradingHalted {
		t.Fatal("trading should not be halted")
	}
}

func TestRiskManager_TradingHalted(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.RecordLoss(5000) // ちょうど上限

	status := rm.GetStatus()
	if !status.TradingHalted {
		t.Fatal("trading should be halted after max daily loss")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd backend
go test ./internal/usecase/ -v -run TestRiskManager
```

Expected: コンパイルエラー

- [ ] **Step 3: risk.go を実装**

```go
package usecase

import (
	"context"
	"fmt"
	"sync"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// RiskStatus はRisk Managerの現在の状態。
type RiskStatus struct {
	Balance        float64 `json:"balance"`
	DailyLoss      float64 `json:"dailyLoss"`
	TotalPosition  float64 `json:"totalPosition"`
	TradingHalted  bool    `json:"tradingHalted"`
	Config         entity.RiskConfig `json:"config"`
}

// RiskManager はすべての注文に対するゲートキーパー。
type RiskManager struct {
	config    entity.RiskConfig
	mu        sync.RWMutex
	balance   float64
	dailyLoss float64
	positions []entity.Position
}

func NewRiskManager(config entity.RiskConfig) *RiskManager {
	return &RiskManager{
		config:  config,
		balance: config.InitialCapital,
	}
}

// CheckOrder は注文提案をリスクルールに照らして承認/拒否する。
// 決済注文は常に承認する（ポジションを閉じる行為はリスクを下げる）。
func (rm *RiskManager) CheckOrder(ctx context.Context, proposal entity.OrderProposal) entity.RiskCheckResult {
	// 決済注文は常に承認
	if proposal.IsClose {
		return entity.RiskCheckResult{Approved: true}
	}

	rm.mu.RLock()
	defer rm.mu.RUnlock()

	orderValue := proposal.Amount * proposal.Price

	// 1. 日次損失チェック
	if rm.dailyLoss >= rm.config.MaxDailyLoss {
		return entity.RiskCheckResult{
			Approved: false,
			Reason:   fmt.Sprintf("daily loss limit exceeded: %.0f/%.0f", rm.dailyLoss, rm.config.MaxDailyLoss),
		}
	}

	// 2. ポジション上限チェック
	totalPosition := rm.calcTotalPositionValue()
	if totalPosition+orderValue > rm.config.MaxPositionAmount {
		return entity.RiskCheckResult{
			Approved: false,
			Reason:   fmt.Sprintf("position limit exceeded: %.0f+%.0f > %.0f", totalPosition, orderValue, rm.config.MaxPositionAmount),
		}
	}

	// 3. 軍資金チェック
	if orderValue > rm.balance {
		return entity.RiskCheckResult{
			Approved: false,
			Reason:   fmt.Sprintf("insufficient balance: %.0f > %.0f", orderValue, rm.balance),
		}
	}

	return entity.RiskCheckResult{Approved: true}
}

// CheckStopLoss は指定銘柄の現在価格に対して損切りが必要なポジションを返す。
func (rm *RiskManager) CheckStopLoss(symbolID int64, currentPrice float64) []entity.Position {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var result []entity.Position
	for _, pos := range rm.positions {
		if pos.SymbolID != symbolID {
			continue
		}

		var lossPercent float64
		if pos.OrderSide == entity.OrderSideBuy {
			// ロング: 価格下落が損失
			lossPercent = (pos.Price - currentPrice) / pos.Price * 100
		} else {
			// ショート: 価格上昇が損失
			lossPercent = (currentPrice - pos.Price) / pos.Price * 100
		}

		if lossPercent >= rm.config.StopLossPercent {
			result = append(result, pos)
		}
	}
	return result
}

// RecordLoss は確定損失を日次損失に加算する。
func (rm *RiskManager) RecordLoss(loss float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.dailyLoss += loss
}

// ResetDailyLoss は日次損失をリセットする（毎日0時JSTに呼ばれる）。
func (rm *RiskManager) ResetDailyLoss() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.dailyLoss = 0
}

// UpdatePositions は現在のポジション一覧を更新する。
func (rm *RiskManager) UpdatePositions(positions []entity.Position) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.positions = positions
}

// UpdateBalance は残高を更新する。
func (rm *RiskManager) UpdateBalance(balance float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.balance = balance
}

// UpdateConfig はリスクパラメータを更新する。
func (rm *RiskManager) UpdateConfig(config entity.RiskConfig) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.config = config
}

// GetStatus は現在のリスク管理状態を返す。
func (rm *RiskManager) GetStatus() RiskStatus {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return RiskStatus{
		Balance:       rm.balance,
		DailyLoss:     rm.dailyLoss,
		TotalPosition: rm.calcTotalPositionValue(),
		TradingHalted: rm.dailyLoss >= rm.config.MaxDailyLoss,
		Config:        rm.config,
	}
}

func (rm *RiskManager) calcTotalPositionValue() float64 {
	total := 0.0
	for _, pos := range rm.positions {
		total += pos.Price * pos.RemainingAmount
	}
	return total
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd backend
go test ./internal/usecase/ -v -run TestRiskManager
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
git commit -m "feat: add Risk Manager with position limit, daily loss, stop loss, and balance checks"
```
