package usecase

import (
	"context"
	"fmt"
	"sync"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type RiskStatus struct {
	Balance       float64           `json:"balance"`
	DailyLoss     float64           `json:"dailyLoss"`
	TotalPosition float64           `json:"totalPosition"`
	TradingHalted bool              `json:"tradingHalted"`
	Config        entity.RiskConfig `json:"config"`
}

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

func (rm *RiskManager) CheckOrder(ctx context.Context, proposal entity.OrderProposal) entity.RiskCheckResult {
	if proposal.IsClose {
		return entity.RiskCheckResult{Approved: true}
	}

	rm.mu.RLock()
	defer rm.mu.RUnlock()

	orderValue := proposal.Amount * proposal.Price

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
		var lossPercent float64
		if pos.OrderSide == entity.OrderSideBuy {
			lossPercent = (pos.Price - currentPrice) / pos.Price * 100
		} else {
			lossPercent = (currentPrice - pos.Price) / pos.Price * 100
		}
		if lossPercent >= rm.config.StopLossPercent {
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

func (rm *RiskManager) UpdatePositions(positions []entity.Position) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.positions = positions
}

func (rm *RiskManager) UpdateBalance(balance float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.balance = balance
	rm.config.MaxPositionAmount = balance
}

func (rm *RiskManager) UpdateConfig(config entity.RiskConfig) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.config = config
}

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
