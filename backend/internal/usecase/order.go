package usecase

import (
	"context"
	"fmt"
	"log/slog"

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
		slog.Info("order rejected by risk manager", "reason", check.Reason)
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

	slog.Info("order created", "orderID", orders[0].ID, "symbolID", signal.SymbolID, "side", side, "amount", amount)

	return &ExecutionResult{
		Executed: true,
		OrderID:  orders[0].ID,
	}, nil
}

// ClosePosition は指定ポジションを成行決済する。
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

	slog.Info("position closed", "positionID", pos.ID, "orderID", orders[0].ID, "side", closeSide)

	return &ExecutionResult{
		Executed: true,
		OrderID:  orders[0].ID,
	}, nil
}
