package usecase

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

type mockOrderClient struct {
	createdOrders   []entity.Order
	createErr       error
	cancelledOrders []entity.Order
	cancelErr       error
	positions       []entity.Position
	positionsErr    error
	orders          []entity.Order
	ordersErr       error
	createCallCount int
}

func (m *mockOrderClient) CreateOrder(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
	m.createCallCount++
	if m.createErr != nil {
		return nil, m.createErr
	}
	return m.createdOrders, nil
}

func (m *mockOrderClient) CreateOrderRaw(ctx context.Context, req entity.OrderRequest) (repository.CreateOrderOutcome, error) {
	m.createCallCount++
	if m.createErr != nil {
		return repository.CreateOrderOutcome{TransportError: m.createErr}, nil
	}
	return repository.CreateOrderOutcome{HTTPStatus: 200, Orders: m.createdOrders}, nil
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

func (m *mockOrderClient) GetMyTrades(ctx context.Context, symbolID int64) ([]entity.MyTrade, error) {
	return nil, nil
}

func (m *mockOrderClient) GetAssets(ctx context.Context) ([]entity.Asset, error) {
	return nil, nil
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

	result, err := executor.ExecuteSignal(context.Background(), "co-test", signal, 4000000, 0.001)
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

	result, err := executor.ExecuteSignal(context.Background(), "co-test", signal, 4000000, 0.001)
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

	result, err := executor.ExecuteSignal(context.Background(), "co-test", signal, 4000000, 0.001)
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
		MaxPositionAmount: 100,
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

	result, err := executor.ExecuteSignal(context.Background(), "co-test", signal, 4000000, 0.001)
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

	_, err := executor.ExecuteSignal(context.Background(), "co-test", signal, 4000000, 0.001)
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

	result, err := executor.ClosePosition(context.Background(), "co-close", pos, 3800000)
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

	result, err := executor.ClosePosition(context.Background(), "co-close", pos, 4200000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Executed {
		t.Fatal("expected close order to be executed")
	}
}
