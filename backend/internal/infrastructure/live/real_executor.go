package live

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
)

// RealExecutor implements eventengine.OrderExecutor by executing real orders
// via the Rakuten API OrderClient.
type RealExecutor struct {
	orderClient   repository.OrderClient
	symbolID      int64
	positions     []eventengine.Position
	mu            sync.Mutex
	spreadPercent float64
	nextOrderID   int64
}

func NewRealExecutor(orderClient repository.OrderClient, symbolID int64, spreadPercent float64) *RealExecutor {
	return &RealExecutor{
		orderClient:   orderClient,
		symbolID:      symbolID,
		spreadPercent: spreadPercent,
		nextOrderID:   1,
	}
}

// Open creates a real market order via orderClient.CreateOrder.
func (r *RealExecutor) Open(symbolID int64, side entity.OrderSide, signalPrice, amount float64, reason string, timestamp int64) (entity.OrderEvent, error) {
	if amount <= 0 {
		return entity.OrderEvent{}, fmt.Errorf("amount must be positive")
	}
	if signalPrice <= 0 {
		return entity.OrderEvent{}, fmt.Errorf("signal price must be positive")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Reverse signal: close opposite positions first.
	for i := len(r.positions) - 1; i >= 0; i-- {
		pos := r.positions[i]
		if pos.SymbolID == symbolID && pos.Side != side {
			_, _, _ = r.closeLocked(pos.PositionID, signalPrice, "reverse_signal", timestamp)
		}
	}

	req := entity.OrderRequest{
		SymbolID:     symbolID,
		OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{
			OrderBehavior: entity.OrderBehaviorOpen,
			OrderSide:     side,
			OrderType:     entity.OrderTypeMarket,
			Amount:        amount,
		},
	}

	orders, err := r.orderClient.CreateOrder(context.Background(), req)
	if err != nil {
		return entity.OrderEvent{}, fmt.Errorf("failed to create open order: %w", err)
	}

	var orderID int64
	fillPrice := signalPrice
	if len(orders) > 0 {
		orderID = orders[0].ID
		if orders[0].Price > 0 {
			fillPrice = orders[0].Price
		}
	}

	slog.Info("live order opened",
		"orderID", orderID,
		"symbolID", symbolID,
		"side", side,
		"amount", amount,
		"reason", reason,
	)

	// Track position in-memory. Use API order ID as position ID.
	posID := orderID
	if posID == 0 {
		posID = r.nextOrderID
		r.nextOrderID++
	}
	r.positions = append(r.positions, eventengine.Position{
		PositionID:     posID,
		SymbolID:       symbolID,
		Side:           side,
		EntryPrice:     fillPrice,
		Amount:         amount,
		EntryTimestamp: timestamp,
	})

	return entity.OrderEvent{
		OrderID:   orderID,
		SymbolID:  symbolID,
		Side:      string(side),
		Action:    "open",
		Price:     fillPrice,
		Amount:    amount,
		Reason:    reason,
		Timestamp: timestamp,
	}, nil
}

// Close creates a close order via orderClient.CreateOrder with BehaviorClose.
func (r *RealExecutor) Close(positionID int64, signalPrice float64, reason string, timestamp int64) (entity.OrderEvent, *entity.BacktestTradeRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closeLocked(positionID, signalPrice, reason, timestamp)
}

// closeLocked performs the close while the mutex is already held.
func (r *RealExecutor) closeLocked(positionID int64, signalPrice float64, reason string, timestamp int64) (entity.OrderEvent, *entity.BacktestTradeRecord, error) {
	idx := -1
	for i := range r.positions {
		if r.positions[i].PositionID == positionID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return entity.OrderEvent{}, nil, fmt.Errorf("position not found: %d", positionID)
	}
	if signalPrice <= 0 {
		return entity.OrderEvent{}, nil, fmt.Errorf("signal price must be positive")
	}

	pos := r.positions[idx]

	closeSide := entity.OrderSideSell
	if pos.Side == entity.OrderSideSell {
		closeSide = entity.OrderSideBuy
	}

	posID := pos.PositionID
	req := entity.OrderRequest{
		SymbolID:     pos.SymbolID,
		OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{
			OrderBehavior: entity.OrderBehaviorClose,
			PositionID:    &posID,
			OrderSide:     closeSide,
			OrderType:     entity.OrderTypeMarket,
			Amount:        pos.Amount,
		},
	}

	orders, err := r.orderClient.CreateOrder(context.Background(), req)
	if err != nil {
		return entity.OrderEvent{}, nil, fmt.Errorf("failed to create close order: %w", err)
	}

	var orderID int64
	exitPrice := signalPrice
	if len(orders) > 0 {
		orderID = orders[0].ID
		if orders[0].Price > 0 {
			exitPrice = orders[0].Price
		}
	}

	slog.Info("live position closed",
		"positionID", positionID,
		"orderID", orderID,
		"side", closeSide,
		"reason", reason,
	)

	// Remove from in-memory tracking.
	r.positions = append(r.positions[:idx], r.positions[idx+1:]...)

	// Build trade record for compatibility with event engine.
	pnl := r.calcPnL(pos, exitPrice)
	pnlPct := 0.0
	if pos.EntryPrice != 0 {
		if pos.Side == entity.OrderSideBuy {
			pnlPct = (exitPrice - pos.EntryPrice) / pos.EntryPrice * 100
		} else {
			pnlPct = (pos.EntryPrice - exitPrice) / pos.EntryPrice * 100
		}
	}

	holding := time.UnixMilli(timestamp).Sub(time.UnixMilli(pos.EntryTimestamp))
	_ = holding // available for future carrying cost

	trade := &entity.BacktestTradeRecord{
		TradeID:     positionID,
		SymbolID:    pos.SymbolID,
		EntryTime:   pos.EntryTimestamp,
		ExitTime:    timestamp,
		Side:        string(pos.Side),
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   exitPrice,
		Amount:      pos.Amount,
		PnL:         pnl,
		PnLPercent:  pnlPct,
		ReasonEntry: "", // not tracked in live yet
		ReasonExit:  reason,
	}

	return entity.OrderEvent{
		OrderID:   orderID,
		SymbolID:  pos.SymbolID,
		Side:      string(pos.Side),
		Action:    "close",
		Price:     exitPrice,
		Amount:    pos.Amount,
		Reason:    reason,
		Timestamp: timestamp,
	}, trade, nil
}

// Positions returns a copy of tracked positions.
func (r *RealExecutor) Positions() []eventengine.Position {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]eventengine.Position, len(r.positions))
	copy(out, r.positions)
	return out
}

// SelectSLTPExit uses worst-case logic: when both SL and TP are hit in the same bar,
// stop-loss wins. Same logic as SimExecutor.
func (r *RealExecutor) SelectSLTPExit(
	side entity.OrderSide,
	stopLossPrice float64,
	takeProfitPrice float64,
	barLow float64,
	barHigh float64,
) (exitPrice float64, reason string, hit bool) {
	switch side {
	case entity.OrderSideBuy:
		slHit := barLow <= stopLossPrice
		tpHit := barHigh >= takeProfitPrice
		if slHit && tpHit {
			return stopLossPrice, "stop_loss", true
		}
		if slHit {
			return stopLossPrice, "stop_loss", true
		}
		if tpHit {
			return takeProfitPrice, "take_profit", true
		}
	case entity.OrderSideSell:
		slHit := barHigh >= stopLossPrice
		tpHit := barLow <= takeProfitPrice
		if slHit && tpHit {
			return stopLossPrice, "stop_loss", true
		}
		if slHit {
			return stopLossPrice, "stop_loss", true
		}
		if tpHit {
			return takeProfitPrice, "take_profit", true
		}
	}
	return 0, "", false
}

// SyncPositions fetches current positions from the API and reconciles in-memory state.
func (r *RealExecutor) SyncPositions(ctx context.Context) error {
	apiPositions, err := r.orderClient.GetPositions(ctx, r.symbolID)
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	synced := make([]eventengine.Position, 0, len(apiPositions))
	for _, ap := range apiPositions {
		synced = append(synced, eventengine.Position{
			PositionID:     ap.ID,
			SymbolID:       ap.SymbolID,
			Side:           ap.OrderSide,
			EntryPrice:     ap.Price,
			Amount:         ap.RemainingAmount,
			EntryTimestamp: ap.CreatedAt,
		})
	}
	r.positions = synced

	slog.Info("positions synced from API",
		"symbolID", r.symbolID,
		"count", len(synced),
	)
	return nil
}

// calcPnL computes profit/loss for a position at a given exit price.
func (r *RealExecutor) calcPnL(pos eventengine.Position, exitPrice float64) float64 {
	switch pos.Side {
	case entity.OrderSideSell:
		return (pos.EntryPrice - exitPrice) * pos.Amount
	default:
		return (exitPrice - pos.EntryPrice) * pos.Amount
	}
}
