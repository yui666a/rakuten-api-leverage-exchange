package usecase

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type RiskStatus struct {
	Balance       float64           `json:"balance"`
	DailyLoss     float64           `json:"dailyLoss"`
	TotalPosition float64           `json:"totalPosition"`
	TradingHalted bool              `json:"tradingHalted"`
	ManuallyStopped bool            `json:"manuallyStopped"`
	CurrentATR    float64           `json:"currentAtr"`
	Config        entity.RiskConfig `json:"config"`
}

type RiskManager struct {
	config            entity.RiskConfig
	mu                sync.RWMutex
	balance           float64
	dailyLoss         float64
	positions         []entity.Position
	manualStop        bool
	highWaterMarks    map[int64]float64 // positionID → best price
	consecutiveLosses int
	cooldownUntil     time.Time
	currentATR        float64 // latest ATR value for dynamic stop-loss
}

func NewRiskManager(config entity.RiskConfig) *RiskManager {
	return &RiskManager{
		config:         config,
		balance:        config.InitialCapital,
		highWaterMarks: make(map[int64]float64),
	}
}

func (rm *RiskManager) CheckOrder(ctx context.Context, proposal entity.OrderProposal) entity.RiskCheckResult {
	if proposal.IsClose {
		return entity.RiskCheckResult{Approved: true}
	}

	rm.mu.RLock()
	defer rm.mu.RUnlock()

	orderValue := proposal.Amount * proposal.Price

	if rm.manualStop {
		return entity.RiskCheckResult{
			Approved: false,
			Reason:   "trading is manually stopped",
		}
	}

	if rm.config.MaxConsecutiveLosses > 0 && !rm.cooldownUntil.IsZero() && time.Now().Before(rm.cooldownUntil) {
		return entity.RiskCheckResult{
			Approved: false,
			Reason:   fmt.Sprintf("cooldown: %d consecutive losses, trading paused until %s", rm.consecutiveLosses, rm.cooldownUntil.Format("15:04")),
		}
	}

	if rm.dailyLoss >= rm.config.MaxDailyLoss {
		return entity.RiskCheckResult{
			Approved: false,
			Reason:   fmt.Sprintf("daily loss limit exceeded: %.0f/%.0f", rm.dailyLoss, rm.config.MaxDailyLoss),
		}
	}

	totalPosition := rm.calcTotalPositionValue()
	if totalPosition+orderValue > rm.config.MaxPositionAmount {
		return entity.RiskCheckResult{
			Approved: false,
			Reason:   fmt.Sprintf("position limit exceeded: %.0f+%.0f > %.0f", totalPosition, orderValue, rm.config.MaxPositionAmount),
		}
	}

	if orderValue > rm.balance {
		return entity.RiskCheckResult{
			Approved: false,
			Reason:   fmt.Sprintf("insufficient balance: %.0f > %.0f", orderValue, rm.balance),
		}
	}

	return entity.RiskCheckResult{Approved: true}
}

func (rm *RiskManager) CheckStopLoss(symbolID int64, currentPrice float64) []entity.Position {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var result []entity.Position
	for _, pos := range rm.positions {
		if pos.SymbolID != symbolID {
			continue
		}
		slDistance := rm.stopLossDistance(pos.Price)
		var loss float64
		if pos.OrderSide == entity.OrderSideBuy {
			loss = pos.Price - currentPrice
		} else {
			loss = currentPrice - pos.Price
		}
		if loss >= slDistance {
			result = append(result, pos)
		}
	}
	return result
}

func (rm *RiskManager) CheckTakeProfit(symbolID int64, currentPrice float64) []entity.Position {
	if rm.config.TakeProfitPercent <= 0 {
		return nil
	}
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var result []entity.Position
	for _, pos := range rm.positions {
		if pos.SymbolID != symbolID {
			continue
		}
		var profitPercent float64
		if pos.OrderSide == entity.OrderSideBuy {
			profitPercent = (currentPrice - pos.Price) / pos.Price * 100
		} else {
			profitPercent = (pos.Price - currentPrice) / pos.Price * 100
		}
		if profitPercent >= rm.config.TakeProfitPercent {
			result = append(result, pos)
		}
	}
	return result
}

func (rm *RiskManager) RecordLoss(loss float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.dailyLoss += loss
}

func (rm *RiskManager) ResetDailyLoss() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.dailyLoss = 0
}

// RecordConsecutiveLoss increments the consecutive loss counter.
// If the counter reaches MaxConsecutiveLosses, a cooldown period is activated.
func (rm *RiskManager) RecordConsecutiveLoss() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.consecutiveLosses++
	if rm.config.MaxConsecutiveLosses > 0 && rm.consecutiveLosses >= rm.config.MaxConsecutiveLosses {
		rm.cooldownUntil = time.Now().Add(time.Duration(rm.config.CooldownMinutes) * time.Minute)
	}
}

// ResetConsecutiveLosses resets the consecutive loss counter and clears cooldown.
func (rm *RiskManager) ResetConsecutiveLosses() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.consecutiveLosses = 0
	rm.cooldownUntil = time.Time{}
}

// UpdateHighWaterMark updates the best price for a position.
// For BUY positions: tracks highest price. For SELL positions: tracks lowest price.
func (rm *RiskManager) UpdateHighWaterMark(positionID int64, currentPrice float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	existing, ok := rm.highWaterMarks[positionID]
	if !ok {
		rm.highWaterMarks[positionID] = currentPrice
		return
	}

	// Find position direction
	var isBuy bool
	for _, pos := range rm.positions {
		if pos.ID == positionID {
			isBuy = pos.OrderSide == entity.OrderSideBuy
			break
		}
	}

	if isBuy && currentPrice > existing {
		rm.highWaterMarks[positionID] = currentPrice
	} else if !isBuy && currentPrice < existing {
		rm.highWaterMarks[positionID] = currentPrice
	}
}

// CheckTrailingStop returns positions where price has reversed by the stop-loss
// distance from the high water mark. Only activates when the position is in profit.
// Uses ATR-based distance when available, otherwise falls back to fixed percentage.
func (rm *RiskManager) CheckTrailingStop(symbolID int64, currentPrice float64) []entity.Position {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var result []entity.Position
	for _, pos := range rm.positions {
		if pos.SymbolID != symbolID {
			continue
		}
		hwm, ok := rm.highWaterMarks[pos.ID]
		if !ok {
			continue
		}

		slDistance := rm.stopLossDistance(pos.Price)

		if pos.OrderSide == entity.OrderSideBuy {
			// Only activate if position has been in profit
			if hwm <= pos.Price {
				continue
			}
			if hwm-currentPrice >= slDistance {
				result = append(result, pos)
			}
		} else {
			// Sell position: low water mark
			if hwm >= pos.Price {
				continue
			}
			if currentPrice-hwm >= slDistance {
				result = append(result, pos)
			}
		}
	}
	return result
}

func (rm *RiskManager) UpdatePositions(positions []entity.Position) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.positions = positions

	// Clean up high water marks for closed positions
	active := make(map[int64]bool, len(positions))
	for _, pos := range positions {
		active[pos.ID] = true
	}
	for id := range rm.highWaterMarks {
		if !active[id] {
			delete(rm.highWaterMarks, id)
		}
	}
}

func (rm *RiskManager) UpdateBalance(balance float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.balance = balance
}

func (rm *RiskManager) UpdateConfig(config entity.RiskConfig) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.config = config
}

// UpdateATR updates the current ATR value used for dynamic stop-loss calculation.
func (rm *RiskManager) UpdateATR(atr float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.currentATR = atr
}

// stopLossDistance returns the stop-loss distance in price units for a given entry price.
// If ATR-based stop-loss is configured and ATR is available, uses ATR * multiplier.
// Otherwise falls back to entry price * StopLossPercent / 100.
func (rm *RiskManager) stopLossDistance(entryPrice float64) float64 {
	if rm.config.StopLossATRMultiplier > 0 && rm.currentATR > 0 {
		return rm.currentATR * rm.config.StopLossATRMultiplier
	}
	return entryPrice * rm.config.StopLossPercent / 100
}

func (rm *RiskManager) StartTrading() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.manualStop = false
}

func (rm *RiskManager) StopTrading() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.manualStop = true
}

func (rm *RiskManager) GetStatus() RiskStatus {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return RiskStatus{
		Balance:         rm.balance,
		DailyLoss:       rm.dailyLoss,
		TotalPosition:   rm.calcTotalPositionValue(),
		TradingHalted:   rm.dailyLoss >= rm.config.MaxDailyLoss,
		ManuallyStopped: rm.manualStop,
		CurrentATR:      rm.currentATR,
		Config:          rm.config,
	}
}

func (rm *RiskManager) calcTotalPositionValue() float64 {
	total := 0.0
	for _, pos := range rm.positions {
		total += pos.Price * pos.RemainingAmount
	}
	return total
}
