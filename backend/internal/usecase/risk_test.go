package usecase

import (
	"context"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func defaultRiskConfig() entity.RiskConfig {
	return entity.RiskConfig{
		MaxPositionAmount:    5000,
		MaxDailyLoss:         5000,
		StopLossPercent:      5,
		TakeProfitPercent:    10,
		InitialCapital:       10000,
		MaxConsecutiveLosses: 3,
		CooldownMinutes:      30,
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
	cfg := defaultRiskConfig()
	cfg.InitialCapital = 100 // 初期残高100円
	rm := NewRiskManager(cfg)

	// 残高不足で拒否されることを確認
	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 4000000, // 4000円 > 100円
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if result.Approved {
		t.Fatal("order should be rejected with insufficient initial balance")
	}

	// 残高を更新して承認されることを確認
	rm.UpdateBalance(20000)
	result = rm.CheckOrder(context.Background(), proposal)
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

func TestRiskManager_CheckTakeProfit_BuyPosition(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	// 5,500,000 is 10% above 5,000,000 → should trigger take-profit
	tpPositions := rm.CheckTakeProfit(7, 5500000)
	if len(tpPositions) != 1 {
		t.Fatalf("expected 1 take-profit position, got %d", len(tpPositions))
	}
}

func TestRiskManager_CheckTakeProfit_NoTrigger(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	// 5,250,000 is 5% above 5,000,000 → should NOT trigger (threshold is 10%)
	tpPositions := rm.CheckTakeProfit(7, 5250000)
	if len(tpPositions) != 0 {
		t.Fatalf("expected 0 take-profit positions, got %d", len(tpPositions))
	}
}

func TestRiskManager_CheckTakeProfit_SellPosition(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideSell, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	// 4,500,000 is 10% below 5,000,000 → should trigger take-profit for sell
	tpPositions := rm.CheckTakeProfit(7, 4500000)
	if len(tpPositions) != 1 {
		t.Fatalf("expected 1 take-profit position, got %d", len(tpPositions))
	}
}

func TestRiskManager_CheckTakeProfit_ZeroConfig_NeverTriggers(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.TakeProfitPercent = 0
	rm := NewRiskManager(cfg)
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	// Even with 20% profit, should return nil when TakeProfitPercent is 0
	tpPositions := rm.CheckTakeProfit(7, 6000000)
	if len(tpPositions) != 0 {
		t.Fatalf("expected 0 take-profit positions when disabled, got %d", len(tpPositions))
	}
}

func TestRiskManager_TrailingStop_BuyPosition(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	// Price went up to 5500000 then drops
	rm.UpdateHighWaterMark(1, 5500000)
	// Trail = 5500000 * 0.95 = 5225000. Price 5200000 < 5225000 → trigger
	targets := rm.CheckTrailingStop(7, 5200000)
	if len(targets) != 1 {
		t.Fatalf("expected 1 trailing stop position, got %d", len(targets))
	}
}

func TestRiskManager_TrailingStop_NoTriggerAboveTrail(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	rm.UpdateHighWaterMark(1, 5500000)
	// Trail = 5225000. Price 5300000 > 5225000 → no trigger
	targets := rm.CheckTrailingStop(7, 5300000)
	if len(targets) != 0 {
		t.Fatalf("expected 0 trailing stop positions, got %d", len(targets))
	}
}

func TestRiskManager_TrailingStop_OnlyActivatesAfterProfit(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	// No high water mark above entry → trailing stop should not activate
	targets := rm.CheckTrailingStop(7, 4800000)
	if len(targets) != 0 {
		t.Fatalf("expected 0 — trailing stop should not activate before profit, got %d", len(targets))
	}
}

func TestRiskManager_TrailingStop_SellPosition(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideSell, Price: 5000000, Amount: 0.001, RemainingAmount: 0.001},
	})
	// Sell: low water mark = 4500000
	rm.UpdateHighWaterMark(1, 4500000)
	// Trail = 4500000 * 1.05 = 4725000. Price 4750000 > 4725000 → trigger
	targets := rm.CheckTrailingStop(7, 4750000)
	if len(targets) != 1 {
		t.Fatalf("expected 1 trailing stop for sell position, got %d", len(targets))
	}
}

func TestRiskManager_ConsecutiveLossBreaker_BlocksAfterNLosses(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	// Record 3 consecutive losses (MaxConsecutiveLosses=3)
	rm.RecordConsecutiveLoss()
	rm.RecordConsecutiveLoss()
	rm.RecordConsecutiveLoss()

	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 1000000,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if result.Approved {
		t.Fatal("order should be rejected after 3 consecutive losses")
	}
	if result.Reason == "" {
		t.Fatal("rejection reason should not be empty")
	}
}

func TestRiskManager_ConsecutiveLossBreaker_ResetOnWin(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	// Record 2 losses then reset (simulating a take-profit win)
	rm.RecordConsecutiveLoss()
	rm.RecordConsecutiveLoss()
	rm.ResetConsecutiveLosses()

	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 1000000,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("order should be approved after reset: %s", result.Reason)
	}
}

func TestRiskManager_ConsecutiveLossBreaker_DisabledWhenZero(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.MaxConsecutiveLosses = 0
	rm := NewRiskManager(cfg)

	// Record many losses — should never trigger cooldown when disabled
	for i := 0; i < 10; i++ {
		rm.RecordConsecutiveLoss()
	}

	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 1000000,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("order should be approved when MaxConsecutiveLosses=0: %s", result.Reason)
	}
}

func TestRiskManager_ConsecutiveLossBreaker_CloseOrdersAllowed(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	// Trigger cooldown
	rm.RecordConsecutiveLoss()
	rm.RecordConsecutiveLoss()
	rm.RecordConsecutiveLoss()

	posID := int64(1)
	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideSell, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 1000000, IsClose: true, PositionID: &posID,
	}
	result := rm.CheckOrder(context.Background(), proposal)
	if !result.Approved {
		t.Fatalf("close order should always be approved during cooldown: %s", result.Reason)
	}
}
