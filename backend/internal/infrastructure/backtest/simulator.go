package backtest

import (
	"fmt"
	"math"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type SimConfig struct {
	InitialBalance    float64
	SpreadPercent     float64
	DailyCarryingCost float64
	SlippagePercent   float64
}

type SimPosition struct {
	PositionID     int64
	SymbolID       int64
	Side           entity.OrderSide
	EntryPrice     float64
	Amount         float64
	EntryTimestamp int64
	SpreadCostOpen float64
	ReasonEntry    string
}

type SimExecutor struct {
	positions    []SimPosition
	closedTrades []entity.BacktestTradeRecord
	balance      float64
	config       SimConfig
	nextOrderID  int64
	nextTradeID  int64
	nextPosID    int64
}

func NewSimExecutor(config SimConfig) *SimExecutor {
	return &SimExecutor{
		balance:     config.InitialBalance,
		config:      config,
		nextOrderID: 1,
		nextTradeID: 1,
		nextPosID:   1,
	}
}

func (s *SimExecutor) Open(symbolID int64, side entity.OrderSide, signalPrice, amount float64, reason string, timestamp int64) (entity.OrderEvent, error) {
	if amount <= 0 {
		return entity.OrderEvent{}, fmt.Errorf("amount must be positive")
	}
	if signalPrice <= 0 {
		return entity.OrderEvent{}, fmt.Errorf("signal price must be positive")
	}

	// Reverse signal: close opposite positions first.
	for i := len(s.positions) - 1; i >= 0; i-- {
		pos := s.positions[i]
		if pos.SymbolID == symbolID && pos.Side != side {
			_, _, _ = s.Close(pos.PositionID, signalPrice, "reverse_signal", timestamp)
		}
	}

	fill := s.entryFillPrice(side, signalPrice)
	position := SimPosition{
		PositionID:     s.nextPosID,
		SymbolID:       symbolID,
		Side:           side,
		EntryPrice:     fill,
		Amount:         amount,
		EntryTimestamp: timestamp,
		SpreadCostOpen: signalPrice * amount * (s.config.SpreadPercent / 100.0) / 2.0,
		ReasonEntry:    reason,
	}
	s.positions = append(s.positions, position)
	s.nextPosID++

	order := entity.OrderEvent{
		OrderID:   s.nextOrderID,
		SymbolID:  symbolID,
		Side:      string(side),
		Action:    "open",
		Price:     fill,
		Amount:    amount,
		Reason:    reason,
		Timestamp: timestamp,
	}
	s.nextOrderID++
	return order, nil
}

func (s *SimExecutor) Close(positionID int64, signalPrice float64, reason string, timestamp int64) (entity.OrderEvent, *entity.BacktestTradeRecord, error) {
	idx := -1
	for i := range s.positions {
		if s.positions[i].PositionID == positionID {
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

	pos := s.positions[idx]
	exitFill := s.exitFillPrice(pos.Side, signalPrice)
	spreadCostClose := signalPrice * pos.Amount * (s.config.SpreadPercent / 100.0) / 2.0
	spreadCostTotal := pos.SpreadCostOpen + spreadCostClose
	carrying := s.carryingCost(pos, timestamp)
	pnl := s.calcPnL(pos, exitFill) - carrying
	pnlPct := 0.0
	if pos.EntryPrice != 0 {
		if pos.Side == entity.OrderSideBuy {
			pnlPct = (exitFill-pos.EntryPrice)/pos.EntryPrice*100 - (carrying/(pos.EntryPrice*pos.Amount))*100
		} else {
			pnlPct = (pos.EntryPrice-exitFill)/pos.EntryPrice*100 - (carrying/(pos.EntryPrice*pos.Amount))*100
		}
	}

	s.balance += pnl
	s.positions = append(s.positions[:idx], s.positions[idx+1:]...)

	sideText := string(pos.Side)
	order := entity.OrderEvent{
		OrderID:   s.nextOrderID,
		SymbolID:  pos.SymbolID,
		Side:      sideText,
		Action:    "close",
		Price:     exitFill,
		Amount:    pos.Amount,
		Reason:    reason,
		Timestamp: timestamp,
	}
	s.nextOrderID++

	trade := entity.BacktestTradeRecord{
		TradeID:      s.nextTradeID,
		SymbolID:     pos.SymbolID,
		EntryTime:    pos.EntryTimestamp,
		ExitTime:     timestamp,
		Side:         sideText,
		EntryPrice:   pos.EntryPrice,
		ExitPrice:    exitFill,
		Amount:       pos.Amount,
		PnL:          pnl,
		PnLPercent:   pnlPct,
		CarryingCost: carrying,
		SpreadCost:   spreadCostTotal,
		ReasonEntry:  pos.ReasonEntry,
		ReasonExit:   reason,
	}
	s.nextTradeID++
	s.closedTrades = append(s.closedTrades, trade)
	return order, &trade, nil
}

// SelectSLTPExit chooses exit level for same-bar SL/TP hits.
// Policy: worst-case fixed (always stop-loss when both hit).
func (s *SimExecutor) SelectSLTPExit(
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

func (s *SimExecutor) Balance() float64 {
	return s.balance
}

func (s *SimExecutor) Positions() []SimPosition {
	out := make([]SimPosition, len(s.positions))
	copy(out, s.positions)
	return out
}

func (s *SimExecutor) ClosedTrades() []entity.BacktestTradeRecord {
	out := make([]entity.BacktestTradeRecord, len(s.closedTrades))
	copy(out, s.closedTrades)
	return out
}

func (s *SimExecutor) entryFillPrice(side entity.OrderSide, signalPrice float64) float64 {
	adjust := (s.config.SpreadPercent / 100.0 / 2.0) + (s.config.SlippagePercent / 100.0)
	switch side {
	case entity.OrderSideSell:
		return signalPrice * (1 - adjust)
	default:
		return signalPrice * (1 + adjust)
	}
}

func (s *SimExecutor) exitFillPrice(side entity.OrderSide, signalPrice float64) float64 {
	adjust := (s.config.SpreadPercent / 100.0 / 2.0) + (s.config.SlippagePercent / 100.0)
	switch side {
	case entity.OrderSideSell:
		// Close SELL by BUY.
		return signalPrice * (1 + adjust)
	default:
		// Close BUY by SELL.
		return signalPrice * (1 - adjust)
	}
}

func (s *SimExecutor) carryingCost(pos SimPosition, exitTimestamp int64) float64 {
	if s.config.DailyCarryingCost <= 0 {
		return 0
	}
	entry := time.UnixMilli(pos.EntryTimestamp)
	exit := time.UnixMilli(exitTimestamp)
	if !exit.After(entry) {
		return 0
	}
	days := exit.Sub(entry).Hours() / 24.0
	notional := pos.EntryPrice * pos.Amount
	cost := notional * (s.config.DailyCarryingCost / 100.0) * days
	return math.Max(cost, 0)
}

func (s *SimExecutor) calcPnL(pos SimPosition, exitPrice float64) float64 {
	switch pos.Side {
	case entity.OrderSideSell:
		return (pos.EntryPrice - exitPrice) * pos.Amount
	default:
		return (exitPrice - pos.EntryPrice) * pos.Amount
	}
}
