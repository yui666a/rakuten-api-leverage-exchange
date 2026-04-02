package usecase

import (
	"context"
	"testing"

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
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 4000000,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("order should be approved: %s", result.Reason)
	}
}

func TestRiskManager_RejectExceedingPositionLimit(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 3000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 3000000,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if result.Approved {
		t.Fatal("order should be rejected: exceeds position limit")
	}
}

func TestRiskManager_AllowCloseOrder(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	posID := int64(1)
	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideSell, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 5000000, IsClose: true, PositionID: &posID,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("close order should always be approved: %s", result.Reason)
	}
}

func TestRiskManager_RejectAfterDailyLossExceeded(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.RecordLoss(5000)
	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 1000000,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if result.Approved {
		t.Fatal("order should be rejected: daily loss limit exceeded")
	}
}

func TestRiskManager_DailyLossReset(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.RecordLoss(5000)
	rm.ResetDailyLoss()
	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 1000000,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("order should be approved after daily reset: %s", result.Reason)
	}
}

func TestRiskManager_RejectInsufficientBalance(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.InitialCapital = 100
	rm := NewRiskManager(cfg)
	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 5000000,
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
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 15000000,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("order should be approved with updated balance: %s", result.Reason)
	}
}

func TestRiskManager_CheckStopLoss(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	stopLossPositions := rm.CheckStopLoss(7, 4700000)
	if len(stopLossPositions) != 1 {
		t.Fatalf("expected 1 stop loss position, got %d", len(stopLossPositions))
	}
}

func TestRiskManager_CheckStopLoss_NoTrigger(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	stopLossPositions := rm.CheckStopLoss(7, 4850000)
	if len(stopLossPositions) != 0 {
		t.Fatalf("expected 0 stop loss positions, got %d", len(stopLossPositions))
	}
}

func TestRiskManager_CheckStopLoss_SellPosition(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideSell, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
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
	rm.RecordLoss(5000)
	status := rm.GetStatus()
	if !status.TradingHalted {
		t.Fatal("trading should be halted after max daily loss")
	}
}
