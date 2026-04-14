package eventengine

import "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"

// OrderExecutor is the interface that both SimExecutor (backtest) and RealExecutor (live) implement.
type OrderExecutor interface {
	Open(symbolID int64, side entity.OrderSide, signalPrice, amount float64, reason string, timestamp int64) (entity.OrderEvent, error)
	Close(positionID int64, signalPrice float64, reason string, timestamp int64) (entity.OrderEvent, *entity.BacktestTradeRecord, error)
	Positions() []Position
	SelectSLTPExit(side entity.OrderSide, stopLossPrice, takeProfitPrice, barLow, barHigh float64) (float64, string, bool)
}

// Position represents an open position for the event engine.
type Position struct {
	PositionID     int64
	SymbolID       int64
	Side           entity.OrderSide
	EntryPrice     float64
	Amount         float64
	EntryTimestamp int64
}
