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
	// FillPriceSource overrides the default percent-based slippage model.
	// When nil, the simulator uses LegacyPercentSlippage{SpreadPercent,
	// SlippagePercent} so existing call sites stay bit-identical.
	FillPriceSource FillPriceSource
	// MakerFeeRate / TakerFeeRate are notional-rate fees applied per fill.
	// Negative values represent rebates (Rakuten Wallet pays -0.01% to
	// makers). Defaults are 0 for both — legacy backtests stay fee-free.
	MakerFeeRate float64
	TakerFeeRate float64
}

type SimPosition struct {
	PositionID     int64
	SymbolID       int64
	Side           entity.OrderSide
	EntryPrice     float64
	Amount         float64
	EntryTimestamp int64
	SpreadCostOpen float64
	OpenFeeJPY     float64 // signed: negative = rebate received on entry
	OpenWasMaker   bool
	ReasonEntry    string
}

type SimExecutor struct {
	positions       []SimPosition
	closedTrades    []entity.BacktestTradeRecord
	balance         float64
	config          SimConfig
	fillPriceSource FillPriceSource
	nextOrderID     int64
	nextTradeID     int64
	nextPosID       int64
}

func NewSimExecutor(config SimConfig) *SimExecutor {
	src := config.FillPriceSource
	if src == nil {
		src = LegacyPercentSlippage{
			SpreadPercent:   config.SpreadPercent,
			SlippagePercent: config.SlippagePercent,
		}
	}
	return &SimExecutor{
		balance:         config.InitialBalance,
		config:          config,
		fillPriceSource: src,
		nextOrderID:     1,
		nextTradeID:     1,
		nextPosID:       1,
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

	fill, err := s.fillPriceSource.FillPrice(FillKindEntry, side, signalPrice, amount, timestamp)
	if err != nil {
		return entity.OrderEvent{}, err
	}
	openMaker := s.lastFillWasMaker()
	openFee := s.feeFor(fill, amount, openMaker)
	// Fee is applied immediately to balance: rebates credit, costs debit.
	s.balance -= openFee
	position := SimPosition{
		PositionID:     s.nextPosID,
		SymbolID:       symbolID,
		Side:           side,
		EntryPrice:     fill,
		Amount:         amount,
		EntryTimestamp: timestamp,
		SpreadCostOpen: signalPrice * amount * (s.config.SpreadPercent / 100.0) / 2.0,
		OpenFeeJPY:     openFee,
		OpenWasMaker:   openMaker,
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
	exitFill, err := s.fillPriceSource.FillPrice(FillKindExit, pos.Side, signalPrice, pos.Amount, timestamp)
	if err != nil {
		return entity.OrderEvent{}, nil, err
	}
	closeMaker := s.lastFillWasMaker()
	closeFee := s.feeFor(exitFill, pos.Amount, closeMaker)
	totalFee := pos.OpenFeeJPY + closeFee
	spreadCostClose := signalPrice * pos.Amount * (s.config.SpreadPercent / 100.0) / 2.0
	spreadCostTotal := pos.SpreadCostOpen + spreadCostClose
	carrying := s.carryingCost(pos, timestamp)
	pnl := s.calcPnL(pos, exitFill) - carrying - closeFee
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
		Fee:          totalFee,
		OpenIsMaker:  pos.OpenWasMaker,
		CloseIsMaker: closeMaker,
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

// Equity returns mark-to-market equity (realized balance + unrealized PnL).
// If a symbol price is not supplied, position entry price is used (unrealized=0).
func (s *SimExecutor) Equity(markPriceBySymbol map[int64]float64) float64 {
	equity := s.balance
	for _, pos := range s.positions {
		markPrice := pos.EntryPrice
		if p, ok := markPriceBySymbol[pos.SymbolID]; ok && p > 0 {
			markPrice = p
		}
		equity += s.calcPnL(pos, markPrice)
	}
	return equity
}

// entryFillPrice / exitFillPrice were folded into FillPriceSource.
// LegacyPercentSlippage in fill_price.go preserves the original arithmetic.

// feeFor returns the JPY fee paid for one fill at the supplied price/amount.
// Sign convention: positive = cost, negative = rebate (Rakuten pays makers
// -0.01% by default).
func (s *SimExecutor) feeFor(price, amount float64, isMaker bool) float64 {
	rate := s.config.TakerFeeRate
	if isMaker {
		rate = s.config.MakerFeeRate
	}
	if rate == 0 {
		return 0
	}
	notional := price * amount
	return notional * rate
}

// lastFillWasMaker queries the FillPriceSource for the most recent fill
// classification. Sources that don't expose this are treated as taker.
func (s *SimExecutor) lastFillWasMaker() bool {
	if mfs, ok := s.fillPriceSource.(MakerFlagSource); ok {
		return mfs.LastFillWasMaker()
	}
	return false
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
