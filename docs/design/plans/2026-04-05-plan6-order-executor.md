# Plan 6: Order Executor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Strategy Engineが生成したシグナルをRisk Managerで検証し、承認された注文を楽天APIに送信するOrder Executorを構築する。

**Architecture:** Order Executorはユースケース層に配置。楽天APIクライアント（`RESTClient`）への依存はインターフェース（`OrderClient`）経由で注入し、テストではmockを使用する。シグナル→OrderProposal変換、Risk Manager検証、注文送信、約定記録という一連のパイプラインを担う。レートリミットは既存のRESTClient内で処理済み。

**Tech Stack:** Go 1.25, sync, context

---

## ファイル構成

```
backend/
├── internal/
│   ├── domain/
│   │   └── repository/
│   │       └── order.go                             # OrderClient インターフェース
│   └── usecase/
│       ├── order.go                                 # Order Executor
│       └── order_test.go
```

---

### Task 1: OrderClient インターフェース定義

**Files:**
- Create: `backend/internal/domain/repository/order.go`

- [ ] **Step 1: order.go を作成**

```go
package repository

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// OrderClient は注文操作のインターフェース。
type OrderClient interface {
	CreateOrder(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error)
	CancelOrder(ctx context.Context, symbolID, orderID int64) ([]entity.Order, error)
	GetOrders(ctx context.Context, symbolID int64) ([]entity.Order, error)
	GetPositions(ctx context.Context, symbolID int64) ([]entity.Position, error)
}
```

- [ ] **Step 2: ビルド確認**

```bash
cd backend && go build ./...
```

- [ ] **Step 3: コミット**

```bash
git add backend/internal/domain/repository/order.go
git commit -m "feat: add OrderClient interface for order operations"
```

---

### Task 2: Order Executor テストと実装

**Files:**
- Create: `backend/internal/usecase/order.go`
- Create: `backend/internal/usecase/order_test.go`

- [ ] **Step 1: order_test.go を書く**

```go
package usecase

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type mockOrderClient struct {
	createdOrders []entity.Order
	createErr     error
	cancelledOrders []entity.Order
	cancelErr     error
	positions     []entity.Position
	positionsErr  error
	orders        []entity.Order
	ordersErr     error
	createCallCount int
}

func (m *mockOrderClient) CreateOrder(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
	m.createCallCount++
	if m.createErr != nil {
		return nil, m.createErr
	}
	return m.createdOrders, nil
}

func (m *mockOrderClient) CancelOrder(ctx context.Context, symbolID, orderID int64) ([]entity.Order, error) {
	if m.cancelErr != nil {
		return nil, m.cancelErr
	}
	return m.cancelledOrders, nil
}

func (m *mockOrderClient) GetOrders(ctx context.Context, symbolID int64) ([]entity.Order, error) {
	if m.ordersErr != nil {
		return nil, m.ordersErr
	}
	return m.orders, nil
}

func (m *mockOrderClient) GetPositions(ctx context.Context, symbolID int64) ([]entity.Position, error) {
	if m.positionsErr != nil {
		return nil, m.positionsErr
	}
	return m.positions, nil
}

func TestOrderExecutor_ExecuteSignal_Buy(t *testing.T) {
	orderClient := &mockOrderClient{
		createdOrders: []entity.Order{
			{ID: 100, SymbolID: 7, OrderSide: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket, Amount: 0.001},
		},
	}
	riskMgr := NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      5000,
		StopLossPercent:   5,
		InitialCapital:    10000,
	})

	executor := NewOrderExecutor(orderClient, riskMgr)

	signal := entity.Signal{
		SymbolID:  7,
		Action:    entity.SignalActionBuy,
		Reason:    "trend follow",
		Timestamp: time.Now().Unix(),
	}

	result, err := executor.ExecuteSignal(context.Background(), signal, 4000000, 0.001)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Executed {
		t.Fatalf("expected order to be executed, reason: %s", result.Reason)
	}
	if result.OrderID != 100 {
		t.Fatalf("expected order ID 100, got %d", result.OrderID)
	}
}

func TestOrderExecutor_ExecuteSignal_Sell(t *testing.T) {
	orderClient := &mockOrderClient{
		createdOrders: []entity.Order{
			{ID: 101, SymbolID: 7, OrderSide: entity.OrderSideSell, OrderType: entity.OrderTypeMarket, Amount: 0.001},
		},
	}
	riskMgr := NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      5000,
		StopLossPercent:   5,
		InitialCapital:    10000,
	})

	executor := NewOrderExecutor(orderClient, riskMgr)

	signal := entity.Signal{
		SymbolID:  7,
		Action:    entity.SignalActionSell,
		Reason:    "contrarian",
		Timestamp: time.Now().Unix(),
	}

	result, err := executor.ExecuteSignal(context.Background(), signal, 4000000, 0.001)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Executed {
		t.Fatalf("expected order to be executed")
	}
}

func TestOrderExecutor_ExecuteSignal_HoldSkipsOrder(t *testing.T) {
	orderClient := &mockOrderClient{}
	riskMgr := NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      5000,
		StopLossPercent:   5,
		InitialCapital:    10000,
	})

	executor := NewOrderExecutor(orderClient, riskMgr)

	signal := entity.Signal{
		SymbolID:  7,
		Action:    entity.SignalActionHold,
		Reason:    "no signal",
		Timestamp: time.Now().Unix(),
	}

	result, err := executor.ExecuteSignal(context.Background(), signal, 4000000, 0.001)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Executed {
		t.Fatal("expected HOLD signal to skip execution")
	}
	if orderClient.createCallCount != 0 {
		t.Fatalf("expected 0 API calls for HOLD, got %d", orderClient.createCallCount)
	}
}

func TestOrderExecutor_ExecuteSignal_RiskRejected(t *testing.T) {
	orderClient := &mockOrderClient{}
	riskMgr := NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 100, // 極小の上限
		MaxDailyLoss:      5000,
		StopLossPercent:   5,
		InitialCapital:    10000,
	})

	executor := NewOrderExecutor(orderClient, riskMgr)

	signal := entity.Signal{
		SymbolID:  7,
		Action:    entity.SignalActionBuy,
		Reason:    "trend follow",
		Timestamp: time.Now().Unix(),
	}

	// 0.001 * 4000000 = 4000円 > 100円上限
	result, err := executor.ExecuteSignal(context.Background(), signal, 4000000, 0.001)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Executed {
		t.Fatal("expected risk rejection")
	}
	if orderClient.createCallCount != 0 {
		t.Fatalf("expected 0 API calls after risk rejection, got %d", orderClient.createCallCount)
	}
}

func TestOrderExecutor_ExecuteSignal_APIError(t *testing.T) {
	orderClient := &mockOrderClient{
		createErr: fmt.Errorf("API error (status 500): internal server error"),
	}
	riskMgr := NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      5000,
		StopLossPercent:   5,
		InitialCapital:    10000,
	})

	executor := NewOrderExecutor(orderClient, riskMgr)

	signal := entity.Signal{
		SymbolID:  7,
		Action:    entity.SignalActionBuy,
		Reason:    "trend follow",
		Timestamp: time.Now().Unix(),
	}

	_, err := executor.ExecuteSignal(context.Background(), signal, 4000000, 0.001)
	if err == nil {
		t.Fatal("expected error from API failure")
	}
}

func TestOrderExecutor_ClosePosition(t *testing.T) {
	orderClient := &mockOrderClient{
		createdOrders: []entity.Order{
			{ID: 200, SymbolID: 7, OrderSide: entity.OrderSideSell, OrderType: entity.OrderTypeMarket, Amount: 0.001},
		},
	}
	riskMgr := NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      5000,
		StopLossPercent:   5,
		InitialCapital:    10000,
	})

	executor := NewOrderExecutor(orderClient, riskMgr)

	pos := entity.Position{
		ID:              1,
		SymbolID:        7,
		OrderSide:       entity.OrderSideBuy,
		Price:           4000000,
		Amount:          0.001,
		RemainingAmount: 0.001,
	}

	result, err := executor.ClosePosition(context.Background(), pos, 3800000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Executed {
		t.Fatalf("expected close order to be executed, reason: %s", result.Reason)
	}
	if result.OrderID != 200 {
		t.Fatalf("expected order ID 200, got %d", result.OrderID)
	}
}

func TestOrderExecutor_ClosePosition_SellPosition(t *testing.T) {
	orderClient := &mockOrderClient{
		createdOrders: []entity.Order{
			{ID: 201, SymbolID: 7, OrderSide: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket, Amount: 0.001},
		},
	}
	riskMgr := NewRiskManager(entity.RiskConfig{
		MaxPositionAmount: 5000,
		MaxDailyLoss:      5000,
		StopLossPercent:   5,
		InitialCapital:    10000,
	})

	executor := NewOrderExecutor(orderClient, riskMgr)

	pos := entity.Position{
		ID:              2,
		SymbolID:        7,
		OrderSide:       entity.OrderSideSell,
		Price:           4000000,
		Amount:          0.001,
		RemainingAmount: 0.001,
	}

	result, err := executor.ClosePosition(context.Background(), pos, 4200000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Executed {
		t.Fatal("expected close order to be executed")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd backend && go test ./internal/usecase/ -v -run TestOrderExecutor
```

Expected: コンパイルエラー（`NewOrderExecutor` が未定義）

- [ ] **Step 3: order.go を実装**

```go
package usecase

import (
	"context"
	"fmt"
	"log"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// ExecutionResult は注文実行の結果。
type ExecutionResult struct {
	Executed bool   `json:"executed"`
	OrderID  int64  `json:"orderId,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// OrderExecutor はシグナルに基づいて注文を実行する。
type OrderExecutor struct {
	client  repository.OrderClient
	riskMgr *RiskManager
}

func NewOrderExecutor(client repository.OrderClient, riskMgr *RiskManager) *OrderExecutor {
	return &OrderExecutor{
		client:  client,
		riskMgr: riskMgr,
	}
}

// ExecuteSignal はシグナルを受け取り、Risk Managerで検証後に注文を送信する。
// HOLDシグナルは何もしない。BUY/SELLはOrderProposalを生成してRisk Managerに渡す。
func (e *OrderExecutor) ExecuteSignal(ctx context.Context, signal entity.Signal, price float64, amount float64) (*ExecutionResult, error) {
	if signal.Action == entity.SignalActionHold {
		return &ExecutionResult{
			Executed: false,
			Reason:   "signal is HOLD, no action",
		}, nil
	}

	side := entity.OrderSideBuy
	if signal.Action == entity.SignalActionSell {
		side = entity.OrderSideSell
	}

	proposal := entity.OrderProposal{
		SymbolID:  signal.SymbolID,
		Side:      side,
		OrderType: entity.OrderTypeMarket,
		Amount:    amount,
		Price:     price,
		IsClose:   false,
	}

	check := e.riskMgr.CheckOrder(ctx, proposal)
	if !check.Approved {
		log.Printf("order rejected by risk manager: %s", check.Reason)
		return &ExecutionResult{
			Executed: false,
			Reason:   fmt.Sprintf("risk rejected: %s", check.Reason),
		}, nil
	}

	req := entity.OrderRequest{
		SymbolID:     signal.SymbolID,
		OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{
			OrderBehavior: entity.OrderBehaviorOpen,
			OrderSide:     side,
			OrderType:     entity.OrderTypeMarket,
			Amount:        amount,
		},
	}

	orders, err := e.client.CreateOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	if len(orders) == 0 {
		return nil, fmt.Errorf("API returned no orders")
	}

	log.Printf("order created: id=%d symbol=%d side=%s amount=%.6f",
		orders[0].ID, signal.SymbolID, side, amount)

	return &ExecutionResult{
		Executed: true,
		OrderID:  orders[0].ID,
	}, nil
}

// ClosePosition は指定ポジションを成行決済する。
// 決済注文はRisk Managerを常に通過する（IsClose=true）。
func (e *OrderExecutor) ClosePosition(ctx context.Context, pos entity.Position, currentPrice float64) (*ExecutionResult, error) {
	closeSide := entity.OrderSideSell
	if pos.OrderSide == entity.OrderSideSell {
		closeSide = entity.OrderSideBuy
	}

	proposal := entity.OrderProposal{
		SymbolID:   pos.SymbolID,
		Side:       closeSide,
		OrderType:  entity.OrderTypeMarket,
		Amount:     pos.RemainingAmount,
		Price:      currentPrice,
		IsClose:    true,
		PositionID: &pos.ID,
	}

	check := e.riskMgr.CheckOrder(ctx, proposal)
	if !check.Approved {
		return &ExecutionResult{
			Executed: false,
			Reason:   fmt.Sprintf("risk rejected close: %s", check.Reason),
		}, nil
	}

	posID := pos.ID
	req := entity.OrderRequest{
		SymbolID:     pos.SymbolID,
		OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{
			OrderBehavior: entity.OrderBehaviorClose,
			PositionID:    &posID,
			OrderSide:     closeSide,
			OrderType:     entity.OrderTypeMarket,
			Amount:        pos.RemainingAmount,
		},
	}

	orders, err := e.client.CreateOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to close position: %w", err)
	}

	if len(orders) == 0 {
		return nil, fmt.Errorf("API returned no orders for close")
	}

	log.Printf("position closed: positionId=%d orderId=%d side=%s",
		pos.ID, orders[0].ID, closeSide)

	return &ExecutionResult{
		Executed: true,
		OrderID:  orders[0].ID,
	}, nil
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd backend && go test ./internal/usecase/ -v -run TestOrderExecutor
```

Expected: 全7テストPASS

- [ ] **Step 5: 全テストを実行して回帰がないことを確認**

```bash
cd backend && go test ./... -v
```

Expected: 全テストPASS

- [ ] **Step 6: コミット**

```bash
git add backend/internal/domain/repository/order.go backend/internal/usecase/order.go backend/internal/usecase/order_test.go
git commit -m "feat: add Order Executor with signal execution and position close"
```

---

### Task 3: 設計書更新

**Files:**
- Modify: `docs/design/2026-04-02-auto-trading-system-design.md`

- [ ] **Step 1: 設計書の実装進捗テーブルを更新**

Plan 6の行を更新:

```markdown
| Plan 6 | Order Executor | #TBD | merged | `usecase/order.go`, `repository/order.go` |
```

- [ ] **Step 2: コミット**

```bash
git add docs/design/2026-04-02-auto-trading-system-design.md docs/design/plans/2026-04-05-plan6-order-executor.md
git commit -m "docs: update implementation progress for Plan 6"
```
