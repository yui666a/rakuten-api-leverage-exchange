package decision

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
)

// ExecutorPositionView adapts an OrderExecutor (live RealExecutor or backtest
// SimExecutor) into a PositionView. It computes the *net* side per symbol —
// not the gross sum — so that, for example, ¥10,613 long + ¥2,665 short
// reports OrderSideBuy (net long ¥7,948) rather than triggering a
// double-counted "position limit exceeded" check downstream.
//
// This is the structural fix for the two-sided sum bug that motivated the
// Signal/Decision/ExecutionPolicy separation.
type ExecutorPositionView struct {
	Executor eventengine.OrderExecutor
}

// CurrentSide returns the net direction at the given symbol:
//   - OrderSideBuy when long-side amount strictly exceeds short-side amount
//   - OrderSideSell when short-side strictly exceeds long-side
//   - "" when flat OR perfectly hedged (treated conservatively as flat)
//   - "" when Executor is nil (defensive — adapter can be wired before the
//     executor is fully initialized in some startup paths)
func (v ExecutorPositionView) CurrentSide(_ context.Context, symbolID int64) entity.OrderSide {
	if v.Executor == nil {
		return ""
	}
	var longAmount, shortAmount float64
	for _, p := range v.Executor.Positions() {
		if p.SymbolID != symbolID {
			continue
		}
		switch p.Side {
		case entity.OrderSideBuy:
			longAmount += p.Amount
		case entity.OrderSideSell:
			shortAmount += p.Amount
		}
	}
	switch {
	case longAmount > shortAmount:
		return entity.OrderSideBuy
	case shortAmount > longAmount:
		return entity.OrderSideSell
	default:
		return ""
	}
}
