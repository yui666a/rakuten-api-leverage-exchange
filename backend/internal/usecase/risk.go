package usecase

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type RiskStatus struct {
	Balance         float64           `json:"balance"`
	DailyLoss       float64           `json:"dailyLoss"`
	TotalPosition   float64           `json:"totalPosition"`
	TradingHalted   bool              `json:"tradingHalted"`
	ManuallyStopped bool              `json:"manuallyStopped"`
	// HaltReason carries the latest automatic-halt cause (e.g. "circuit_breaker:price_jump").
	// Empty when trading is running or was halted by a plain manual /stop.
	HaltReason string            `json:"haltReason,omitempty"`
	CurrentATR float64           `json:"currentAtr"`
	Config     entity.RiskConfig `json:"config"`
}

type RiskManager struct {
	config            entity.RiskConfig
	mu                sync.RWMutex
	balance           float64
	peakBalance       float64 // running peak for drawdown computation
	dailyLoss         float64
	positions         []entity.Position
	manualStop        bool
	haltReason        string
	highWaterMarks    map[int64]float64 // positionID → best price
	consecutiveLosses int
	cooldownUntil     time.Time
	currentATR        float64 // latest ATR value for dynamic stop-loss

	// Browser-notification plumbing. realtimeHub is optional: when nil all
	// publish helpers are no-ops, so unit tests that ignore the hub keep
	// working unchanged.
	realtimeHub *RealtimeHub
	// Edge-trigger latches so we publish a warning once per crossing instead
	// of every single Update call.
	ddWarnFired         bool
	ddCriticalFired     bool
	dailyLossWarnFired  bool
	consecutiveLossFlag bool
}

func NewRiskManager(config entity.RiskConfig) *RiskManager {
	return &RiskManager{
		config:         config,
		balance:        config.InitialCapital,
		peakBalance:    config.InitialCapital,
		highWaterMarks: make(map[int64]float64),
	}
}

// Risk-event taxonomy. The string values land in the JSON payload sent to the
// frontend so they double as the Notification kind for the UI to switch on.
type RiskEventKind string

const (
	RiskEventDDWarning        RiskEventKind = "dd_warning"
	RiskEventDDCritical       RiskEventKind = "dd_critical"
	RiskEventConsecutiveLoss  RiskEventKind = "consecutive_losses"
	RiskEventDailyLossWarning RiskEventKind = "daily_loss_warning"
	RiskEventCooldownStarted  RiskEventKind = "cooldown_started"
	// RiskEventCircuitBreaker fires when the watcher halts trading due to an
	// abnormal market or feed condition. Reason carries the trigger label
	// ("price_jump", "abnormal_spread", "book_feed_stale", "empty_book").
	RiskEventCircuitBreaker RiskEventKind = "circuit_breaker"
)

// RiskEventSeverity influences the icon / sound the frontend picks. Kept as
// a separate field rather than inferred from Kind so UI rules can adapt
// without a backend change.
type RiskEventSeverity string

const (
	RiskSeverityInfo     RiskEventSeverity = "info"
	RiskSeverityWarning  RiskEventSeverity = "warning"
	RiskSeverityCritical RiskEventSeverity = "critical"
)

// RiskEventPayload is the JSON shape pushed on the "risk_event" channel.
type RiskEventPayload struct {
	Kind       RiskEventKind     `json:"kind"`
	Severity   RiskEventSeverity `json:"severity"`
	Message    string            `json:"message"`
	Balance    float64           `json:"balance,omitempty"`
	Peak       float64           `json:"peak,omitempty"`
	DDPct      float64           `json:"ddPct,omitempty"`
	DailyLoss  float64           `json:"dailyLoss,omitempty"`
	MaxDaily   float64           `json:"maxDaily,omitempty"`
	StreakLen  int               `json:"streakLen,omitempty"`
	CooldownTo int64             `json:"cooldownTo,omitempty"`
	Timestamp  int64             `json:"timestamp"`
}

// SetRealtimeHub wires browser-notification publishing. nil clears the hub.
func (rm *RiskManager) SetRealtimeHub(hub *RealtimeHub) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.realtimeHub = hub
}

// publishRisk best-effort sends a risk_event. Lock must NOT be held by the
// caller because Publish iterates subscribers and may block on slow channels;
// keep the publish off the hot RW path.
func (rm *RiskManager) publishRisk(p RiskEventPayload) {
	if rm.realtimeHub == nil {
		return
	}
	if p.Timestamp == 0 {
		p.Timestamp = time.Now().UnixMilli()
	}
	if err := rm.realtimeHub.PublishData("risk_event", 0, p); err != nil {
		// Notifications are advisory; log at debug to avoid flooding under
		// fan-out backpressure.
	}
}

// Threshold defaults for risk-event triggers. Kept here so unit tests pin the
// exact crossings.
const (
	ddWarningPct      = 15.0
	ddCriticalPct     = 18.0
	dailyLossWarnFrac = 0.5
	consecutiveLossThreshold = 3
)

func (rm *RiskManager) CheckOrder(ctx context.Context, proposal entity.OrderProposal) entity.RiskCheckResult {
	return rm.CheckOrderAt(ctx, time.Now(), proposal)
}

// CheckOrderAt evaluates a proposal at caller-supplied time (for deterministic backtests).
func (rm *RiskManager) CheckOrderAt(ctx context.Context, now time.Time, proposal entity.OrderProposal) entity.RiskCheckResult {
	if now.IsZero() {
		now = time.Now()
	}

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

	if rm.config.MaxConsecutiveLosses > 0 && !rm.cooldownUntil.IsZero() && now.Before(rm.cooldownUntil) {
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
	rm.dailyLoss += loss
	var emit *RiskEventPayload
	if rm.config.MaxDailyLoss > 0 && !rm.dailyLossWarnFired &&
		rm.dailyLoss >= rm.config.MaxDailyLoss*dailyLossWarnFrac {
		rm.dailyLossWarnFired = true
		emit = &RiskEventPayload{
			Kind:      RiskEventDailyLossWarning,
			Severity:  RiskSeverityWarning,
			Message:   fmt.Sprintf("daily loss reached %.0f / max %.0f (%.0f%%)", rm.dailyLoss, rm.config.MaxDailyLoss, dailyLossWarnFrac*100),
			DailyLoss: rm.dailyLoss,
			MaxDaily:  rm.config.MaxDailyLoss,
		}
	}
	rm.mu.Unlock()
	if emit != nil {
		rm.publishRisk(*emit)
	}
}

func (rm *RiskManager) ResetDailyLoss() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.dailyLoss = 0
	rm.dailyLossWarnFired = false
}

// RecordConsecutiveLoss increments the consecutive loss counter.
// If the counter reaches MaxConsecutiveLosses, a cooldown period is activated.
func (rm *RiskManager) RecordConsecutiveLoss() {
	rm.RecordConsecutiveLossAt(time.Now())
}

// RecordConsecutiveLossAt increments loss counter at caller-supplied time.
func (rm *RiskManager) RecordConsecutiveLossAt(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}

	rm.mu.Lock()
	rm.consecutiveLosses++
	streak := rm.consecutiveLosses
	var emits []RiskEventPayload
	if !rm.consecutiveLossFlag && streak >= consecutiveLossThreshold {
		rm.consecutiveLossFlag = true
		emits = append(emits, RiskEventPayload{
			Kind:      RiskEventConsecutiveLoss,
			Severity:  RiskSeverityWarning,
			Message:   fmt.Sprintf("%d consecutive losses", streak),
			StreakLen: streak,
		})
	}
	if rm.config.MaxConsecutiveLosses > 0 && streak >= rm.config.MaxConsecutiveLosses {
		rm.cooldownUntil = now.Add(time.Duration(rm.config.CooldownMinutes) * time.Minute)
		emits = append(emits, RiskEventPayload{
			Kind:       RiskEventCooldownStarted,
			Severity:   RiskSeverityInfo,
			Message:    fmt.Sprintf("cooldown started for %d min after %d losses", rm.config.CooldownMinutes, streak),
			StreakLen:  streak,
			CooldownTo: rm.cooldownUntil.UnixMilli(),
		})
	}
	rm.mu.Unlock()
	for _, p := range emits {
		rm.publishRisk(p)
	}
}

// ResetConsecutiveLosses resets the consecutive loss counter and clears cooldown.
func (rm *RiskManager) ResetConsecutiveLosses() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.consecutiveLosses = 0
	rm.cooldownUntil = time.Time{}
	rm.consecutiveLossFlag = false
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
	rm.balance = balance
	if balance > rm.peakBalance {
		rm.peakBalance = balance
	}
	var emit *RiskEventPayload
	if rm.peakBalance > 0 {
		ddPct := (rm.peakBalance - balance) / rm.peakBalance * 100
		if ddPct >= ddCriticalPct && !rm.ddCriticalFired {
			rm.ddCriticalFired = true
			rm.ddWarnFired = true // skip warning if we cross critical first
			emit = &RiskEventPayload{
				Kind:     RiskEventDDCritical,
				Severity: RiskSeverityCritical,
				Message:  fmt.Sprintf("MaxDD critical: %.1f%% (peak %.0f → %.0f)", ddPct, rm.peakBalance, balance),
				Balance:  balance,
				Peak:     rm.peakBalance,
				DDPct:    ddPct,
			}
		} else if ddPct >= ddWarningPct && !rm.ddWarnFired {
			rm.ddWarnFired = true
			emit = &RiskEventPayload{
				Kind:     RiskEventDDWarning,
				Severity: RiskSeverityWarning,
				Message:  fmt.Sprintf("MaxDD warning: %.1f%% (peak %.0f → %.0f)", ddPct, rm.peakBalance, balance),
				Balance:  balance,
				Peak:     rm.peakBalance,
				DDPct:    ddPct,
			}
		}
		// Recovery: clear latches when balance comes back near peak so the
		// next deep DD can re-trigger.
		if ddPct < ddWarningPct/2 {
			rm.ddWarnFired = false
			rm.ddCriticalFired = false
		}
	}
	rm.mu.Unlock()
	if emit != nil {
		rm.publishRisk(*emit)
	}
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
	rm.haltReason = ""
}

func (rm *RiskManager) StopTrading() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.manualStop = true
	rm.haltReason = ""
}

// HaltAutomatic stops trading and records the supplied reason so the status
// API and the realtime hub can surface why the bot is parked. Idempotent:
// repeated calls with the same reason are no-ops; a different reason
// updates the field but does not re-publish (the watcher dedups events).
//
// Returns true when this call is what flipped trading from running to halted
// (so the caller can publish the realtime event exactly once).
func (rm *RiskManager) HaltAutomatic(reason string) (firstHalt bool) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.manualStop {
		// Already halted (manual or by an earlier circuit-breaker hit).
		// Keep the most recent reason for visibility.
		rm.haltReason = reason
		return false
	}
	rm.manualStop = true
	rm.haltReason = reason
	return true
}

// HaltReason returns the last automatic-halt label, or empty when trading
// is running or was halted by a plain manual /stop.
func (rm *RiskManager) HaltReason() string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.haltReason
}

// LocalPositions returns a defensive copy of the in-memory position list.
// Used by the reconciler to compare against venue-side state without
// holding the risk-manager lock for the full check.
func (rm *RiskManager) LocalPositions() []entity.Position {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	out := make([]entity.Position, len(rm.positions))
	copy(out, rm.positions)
	return out
}

// LocalBalance returns the bot's last-known JPY balance.
func (rm *RiskManager) LocalBalance() float64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.balance
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
		HaltReason:      rm.haltReason,
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
