package exitplan

import (
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

// CurrentSLPrice は現時点の SL 価格を返す。ATR モードでは currentATR が
// 揺らぐと結果も揺らぐ（仕様：ATR レジーム変化への追従）。
//
// 計算は既存 backtest.TickRiskHandler.stopLossDistance と完全互換:
//
//	distance = max(entryPrice × percent / 100, currentATR × multiplier)
//
// ATR モードかつ ATR=0 のときは percent にフォールバックする。
func (e *ExitPlan) CurrentSLPrice(currentATR float64) float64 {
	distance := stopLossDistance(e.Policy.StopLoss, e.EntryPrice, currentATR)
	if e.Side == entity.OrderSideBuy {
		return e.EntryPrice - distance
	}
	return e.EntryPrice + distance
}

// CurrentTPPrice は TP 価格を返す。Policy.TakeProfit.Percent は New で
// > 0 が保証されているので無効ケースの分岐は不要。
func (e *ExitPlan) CurrentTPPrice() float64 {
	distance := e.EntryPrice * e.Policy.TakeProfit.Percent / 100.0
	if e.Side == entity.OrderSideBuy {
		return e.EntryPrice + distance
	}
	return e.EntryPrice - distance
}

// CurrentTrailingTriggerPrice は HWM から SL 距離分戻った価格を返す。
// TrailingActivated == false のときと TrailingMode == Disabled のときは nil。
func (e *ExitPlan) CurrentTrailingTriggerPrice(currentATR float64) *float64 {
	if e.Policy.Trailing.Mode == risk.TrailingModeDisabled {
		return nil
	}
	if !e.TrailingActivated || e.TrailingHWM == nil {
		return nil
	}
	distance := trailingDistance(e.Policy, e.EntryPrice, currentATR)
	if distance <= 0 {
		return nil
	}
	hwm := *e.TrailingHWM
	var trigger float64
	if e.Side == entity.OrderSideBuy {
		trigger = hwm - distance
	} else {
		trigger = hwm + distance
	}
	return &trigger
}

// stopLossDistance は SL 距離。max(percent, ATR) で保守的に取る。
// 既存 backtest.TickRiskHandler.stopLossDistance と完全互換。
func stopLossDistance(spec risk.StopLossSpec, entryPrice, currentATR float64) float64 {
	percentDist := entryPrice * spec.Percent / 100.0
	atrDist := 0.0
	if spec.ATRMultiplier > 0 && currentATR > 0 {
		atrDist = currentATR * spec.ATRMultiplier
	}
	if atrDist > percentDist {
		return atrDist
	}
	return percentDist
}

// trailingDistance は Trailing の reversal 距離。Disabled なら 0。
// 既存 backtest.TickRiskHandler.trailingDistance と完全互換。
func trailingDistance(policy risk.RiskPolicy, entryPrice, currentATR float64) float64 {
	switch policy.Trailing.Mode {
	case risk.TrailingModeDisabled:
		return 0
	case risk.TrailingModeATR:
		percentDist := entryPrice * policy.StopLoss.Percent / 100.0
		atrDist := 0.0
		if policy.Trailing.ATRMultiplier > 0 && currentATR > 0 {
			atrDist = currentATR * policy.Trailing.ATRMultiplier
		}
		if atrDist > percentDist {
			return atrDist
		}
		return percentDist
	default: // TrailingModePercent
		return entryPrice * policy.StopLoss.Percent / 100.0
	}
}
