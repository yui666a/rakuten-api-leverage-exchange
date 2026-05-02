package usecase

import (
	"context"
	"testing"
	"time"

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

func TestRiskManager_ATR_StopLoss(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.StopLossATRMultiplier = 2.0
	cfg.StopLossPercent = 5 // fallback
	rm := NewRiskManager(cfg)

	rm.UpdateATR(50000) // ATR = 50,000 → stop distance = 100,000
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.01, RemainingAmount: 0.01},
	})

	// Price dropped 90,000 → within ATR stop (100,000), no trigger
	targets := rm.CheckStopLoss(7, 4910000)
	if len(targets) != 0 {
		t.Fatalf("expected no stop-loss at 90k drop (ATR stop = 100k), got %d targets", len(targets))
	}

	// Price dropped 100,000 → exactly at ATR stop, trigger
	targets = rm.CheckStopLoss(7, 4900000)
	if len(targets) != 1 {
		t.Fatalf("expected stop-loss trigger at 100k drop, got %d targets", len(targets))
	}
}

func TestRiskManager_ATR_FallbackToPercent(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.StopLossATRMultiplier = 2.0
	cfg.StopLossPercent = 5
	rm := NewRiskManager(cfg)

	// ATR not set (0) → fallback to 5% = 250,000 at price 5,000,000
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.01, RemainingAmount: 0.01},
	})

	// 4% drop → no trigger
	targets := rm.CheckStopLoss(7, 4800000)
	if len(targets) != 0 {
		t.Fatalf("expected no trigger at 4%% drop, got %d", len(targets))
	}

	// 5% drop → trigger
	targets = rm.CheckStopLoss(7, 4750000)
	if len(targets) != 1 {
		t.Fatalf("expected trigger at 5%% drop, got %d", len(targets))
	}
}

func TestRiskManager_ATR_TrailingStop(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.StopLossATRMultiplier = 1.5
	rm := NewRiskManager(cfg)

	rm.UpdateATR(40000) // trailing distance = 60,000
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.01, RemainingAmount: 0.01},
	})

	// Price went up to 5,100,000 (in profit)
	rm.UpdateHighWaterMark(1, 5100000)

	// Dropped to 5,050,000 → 50k reversal from HWM, within 60k distance
	targets := rm.CheckTrailingStop(7, 5050000)
	if len(targets) != 0 {
		t.Fatalf("expected no trailing stop at 50k reversal (distance=60k), got %d", len(targets))
	}

	// Dropped to 5,040,000 → 60k reversal, trigger
	targets = rm.CheckTrailingStop(7, 5040000)
	if len(targets) != 1 {
		t.Fatalf("expected trailing stop at 60k reversal, got %d", len(targets))
	}
}

func TestRiskManager_ATR_DisabledUsesPercent(t *testing.T) {
	cfg := defaultRiskConfig()
	cfg.StopLossATRMultiplier = 0 // ATR disabled
	cfg.StopLossPercent = 5
	rm := NewRiskManager(cfg)

	rm.UpdateATR(50000) // ATR set but disabled by multiplier=0
	rm.UpdatePositions([]entity.Position{
		{ID: 1, SymbolID: 7, OrderSide: entity.OrderSideBuy, Price: 5000000, Amount: 0.01, RemainingAmount: 0.01},
	})

	// Should use 5% = 250,000, not ATR
	targets := rm.CheckStopLoss(7, 4800000) // 200k drop, within 5%
	if len(targets) != 0 {
		t.Fatalf("expected no trigger (ATR disabled, using 5%%), got %d", len(targets))
	}

	targets = rm.CheckStopLoss(7, 4750000) // exactly 5%
	if len(targets) != 1 {
		t.Fatalf("expected trigger at 5%%, got %d", len(targets))
	}
}

func TestRiskManager_CheckOrderAt_UsesInjectedTimeForCooldown(t *testing.T) {
	rm := NewRiskManager(defaultRiskConfig())
	base := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

	rm.RecordConsecutiveLossAt(base)
	rm.RecordConsecutiveLossAt(base)
	rm.RecordConsecutiveLossAt(base)

	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 1000000,
	}

	blocked := rm.CheckOrderAt(context.Background(), base.Add(29*time.Minute), proposal)
	if blocked.Approved {
		t.Fatal("order should be rejected before injected cooldown end")
	}

	allowed := rm.CheckOrderAt(context.Background(), base.Add(31*time.Minute), proposal)
	if !allowed.Approved {
		t.Fatalf("order should be approved after injected cooldown end: %s", allowed.Reason)
	}
}

// TestRiskManager_EntryCooldown_NoteCloseTransitions verifies the new entry
// cooldown introduced in PR3. NoteClose must extend the window by exactly
// EntryCooldownSec seconds; IsEntryCooldown returns true while inside it and
// false after it elapses.
func TestRiskManager_EntryCooldown_NoteCloseTransitions(t *testing.T) {
	rm := NewRiskManager(entity.RiskConfig{
		InitialCapital:   100000,
		EntryCooldownSec: 60,
	})
	base := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)

	if rm.IsEntryCooldown(base) {
		t.Fatal("fresh RiskManager should not report cooldown active")
	}

	rm.NoteClose(base)

	if !rm.IsEntryCooldown(base.Add(30 * time.Second)) {
		t.Error("cooldown should be active 30s after close")
	}
	if !rm.IsEntryCooldown(base.Add(59 * time.Second)) {
		t.Error("cooldown should still be active just before window end")
	}
	if rm.IsEntryCooldown(base.Add(60 * time.Second)) {
		t.Error("cooldown should expire at exactly EntryCooldownSec")
	}
	if rm.IsEntryCooldown(base.Add(120 * time.Second)) {
		t.Error("cooldown should remain expired thereafter")
	}
}

// TestRiskManager_EntryCooldown_ZeroSecIsNoop confirms profiles without the
// new field keep their existing behaviour: NoteClose must not arm the
// cooldown when EntryCooldownSec=0.
func TestRiskManager_EntryCooldown_ZeroSecIsNoop(t *testing.T) {
	rm := NewRiskManager(entity.RiskConfig{InitialCapital: 100000})
	base := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)

	rm.NoteClose(base)
	if rm.IsEntryCooldown(base.Add(time.Second)) {
		t.Error("EntryCooldownSec=0 must leave NoteClose as a no-op")
	}
}

// TestRiskManager_EntryCooldown_IndependentFromConsecutiveLossCooldown checks
// the two cooldown fields do not interact: arming the loss-streak cooldown
// must not arm the entry cooldown, and vice versa.
func TestRiskManager_EntryCooldown_IndependentFromConsecutiveLossCooldown(t *testing.T) {
	rm := NewRiskManager(entity.RiskConfig{
		InitialCapital:       100000,
		MaxConsecutiveLosses: 3,
		CooldownMinutes:      30,
		EntryCooldownSec:     60,
	})
	base := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)

	// Arm only the loss-streak cooldown.
	rm.RecordConsecutiveLossAt(base)
	rm.RecordConsecutiveLossAt(base)
	rm.RecordConsecutiveLossAt(base)
	if rm.IsEntryCooldown(base.Add(time.Second)) {
		t.Error("RecordConsecutiveLoss must not arm IsEntryCooldown")
	}

	// Arm only the entry cooldown. Use realistic limits so unrelated guards
	// (daily loss / position cap) do not falsely fail the assertion.
	rm2 := NewRiskManager(entity.RiskConfig{
		InitialCapital:       100000,
		MaxPositionAmount:    1_000_000_000,
		MaxDailyLoss:         1_000_000_000,
		MaxConsecutiveLosses: 3,
		CooldownMinutes:      30,
		EntryCooldownSec:     60,
	})
	rm2.NoteClose(base)
	proposal := entity.OrderProposal{
		SymbolID: 7, Side: entity.OrderSideBuy, OrderType: entity.OrderTypeMarket,
		Amount: 0.001, Price: 1000000,
	}
	check := rm2.CheckOrderAt(context.Background(), base.Add(time.Second), proposal)
	if !check.Approved {
		t.Errorf("NoteClose must not engage the loss-streak cooldown path: %s", check.Reason)
	}
}
