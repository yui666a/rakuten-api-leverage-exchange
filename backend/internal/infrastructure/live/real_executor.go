package live

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/sor"
)

// TouchSource provides the latest BestBid / BestAsk for the symbol so the
// SOR can place a sane LIMIT price. It mirrors booklimit.BookSource but
// returns only the touch (the SOR does not need full depth).
type TouchSource interface {
	LatestBefore(ctx context.Context, symbolID, ts int64) (entity.Orderbook, bool, error)
}

// RealExecutor implements eventengine.OrderExecutor by executing real orders
// via the Rakuten API OrderClient.
type RealExecutor struct {
	orderClient   repository.OrderClient
	symbolID      int64
	positions     []eventengine.Position
	mu            sync.Mutex
	spreadPercent float64
	nextOrderID   int64
	// router decides the execution tactic per signal. Always non-nil after
	// NewRealExecutor (defaults to StrategyMarket).
	router *sor.Selector
	// touchSrc is consulted only when router picks a non-MARKET strategy.
	touchSrc TouchSource
	// pollInterval bounds how often we poll order status during the
	// post-only escalation wait. Defaults to 1 s.
	pollInterval time.Duration
	// rejectionFallbackEnabled controls whether a post-only rejection
	// (e.g. crossed-the-touch) immediately retries with MARKET. Defaults
	// to true so live trading never silently drops a signal.
	rejectionFallbackEnabled bool
}

// RealExecutorOption configures a RealExecutor at construction time.
type RealExecutorOption func(*RealExecutor)

// WithSOR replaces the default StrategyMarket router. Pass a Selector
// constructed with sor.New(...). nil leaves the default in place.
func WithSOR(s *sor.Selector) RealExecutorOption {
	return func(r *RealExecutor) {
		if s != nil {
			r.router = s
		}
	}
}

// WithTouchSource wires the BestBid / BestAsk lookup the SOR uses for LIMIT
// pricing. Without it the executor degrades to MARKET on any signal that
// would have used a LIMIT (the SOR itself also short-circuits when the
// touch is missing).
func WithTouchSource(src TouchSource) RealExecutorOption {
	return func(r *RealExecutor) { r.touchSrc = src }
}

// WithPollInterval overrides the default 1-second status poll cadence.
// Tests pass a small interval to keep total wall time low.
func WithPollInterval(d time.Duration) RealExecutorOption {
	return func(r *RealExecutor) {
		if d > 0 {
			r.pollInterval = d
		}
	}
}

func NewRealExecutor(orderClient repository.OrderClient, symbolID int64, spreadPercent float64, opts ...RealExecutorOption) *RealExecutor {
	r := &RealExecutor{
		orderClient:              orderClient,
		symbolID:                 symbolID,
		spreadPercent:            spreadPercent,
		nextOrderID:              1,
		router:                   sor.New(sor.Config{Strategy: sor.StrategyMarket}),
		pollInterval:             1 * time.Second,
		rejectionFallbackEnabled: true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

// Open creates a real order via the configured SOR plan.
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

	plan := r.planFor(symbolID, side, amount, nil, timestamp)
	orderID, fillPrice, err := r.runPlan(context.Background(), plan, signalPrice)
	if err != nil {
		return entity.OrderEvent{}, fmt.Errorf("failed to execute open plan: %w", err)
	}

	slog.Info("live order opened",
		"orderID", orderID,
		"symbolID", symbolID,
		"side", side,
		"amount", amount,
		"reason", reason,
		"strategy", plan.Strategy,
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

// planFor consults the SOR with the most recent touch (when a TouchSource
// is wired) to produce an execution plan for a single open. Without a
// touch the SOR itself falls back to MARKET so this method is safe to call
// even before the WS book cache is populated.
func (r *RealExecutor) planFor(symbolID int64, side entity.OrderSide, amount float64, positionID *int64, timestamp int64) sor.Plan {
	in := sor.SelectInput{
		SymbolID:   symbolID,
		Side:       side,
		Amount:     amount,
		PositionID: positionID,
	}
	if r.touchSrc != nil {
		ob, ok, err := r.touchSrc.LatestBefore(context.Background(), symbolID, timestamp)
		if err == nil && ok {
			in.BestBid = ob.BestBid
			in.BestAsk = ob.BestAsk
		}
	}
	return r.router.Plan(in)
}

// runPlan walks a Plan to completion. Returns the venue order ID and the
// realised fill price (or signalPrice as a last-resort fallback for legacy
// flows that do not surface a price).
//
// The control flow is intentionally simple — at most two Submit steps and
// one Wait between them, matching the two strategies the SOR currently
// produces. Future strategies (TWAP, iceberg) will need more state, but
// adding a generic state machine before that is YAGNI.
func (r *RealExecutor) runPlan(ctx context.Context, plan sor.Plan, signalPrice float64) (orderID int64, fillPrice float64, err error) {
	fillPrice = signalPrice
	if len(plan.Steps) == 0 {
		return 0, 0, errors.New("empty plan")
	}

	// First step is always Submit by construction (verified in tests).
	primary := plan.Steps[0]
	if primary.Kind != sor.StepKindSubmit {
		return 0, 0, fmt.Errorf("unexpected first step kind: %s", primary.Kind)
	}

	primaryOrder, primaryErr := r.submit(ctx, primary.Order)
	primaryRejected := primaryErr != nil

	// Single-step plan (StrategyMarket) — no escalation phase.
	if len(plan.Steps) == 1 {
		if primaryRejected {
			return 0, signalPrice, primaryErr
		}
		return primaryOrder.ID, fillPriceOf(primaryOrder, signalPrice), nil
	}

	wait := plan.Steps[1]
	if wait.Kind != sor.StepKindWaitOrEscalate {
		return 0, 0, fmt.Errorf("unexpected second step kind: %s", wait.Kind)
	}

	// Post-only rejected outright (e.g. crossed). Per design discussion, we
	// fall straight through to the MARKET fallback so the signal is not
	// silently dropped.
	if primaryRejected {
		if !r.rejectionFallbackEnabled {
			return 0, signalPrice, primaryErr
		}
		slog.Warn("post-only LIMIT rejected, escalating to MARKET", "error", primaryErr)
		fb, fbErr := r.submit(ctx, wait.FallbackOrder)
		if fbErr != nil {
			return 0, signalPrice, fmt.Errorf("post-only rejected and MARKET fallback failed: %w", fbErr)
		}
		return fb.ID, fillPriceOf(fb, signalPrice), nil
	}

	// Wait for the LIMIT to fill, polling status. The post-only LIMIT can
	// transition to (a) fully filled, (b) cancelled by the venue, or
	// (c) still resting. (a) is success, (b) we treat like (c) and fall
	// through to MARKET, (c) we wait until the deadline then cancel + MARKET.
	deadline := time.Now().Add(time.Duration(wait.EscalateAfterMs) * time.Millisecond)
	if filled := r.pollUntilFilledOrDeadline(ctx, primaryOrder.ID, deadline); filled != nil {
		return filled.ID, fillPriceOf(*filled, signalPrice), nil
	}

	// Deadline reached. Cancel best-effort then submit MARKET fallback.
	if _, err := r.orderClient.CancelOrder(ctx, r.symbolID, primaryOrder.ID); err != nil {
		// A cancel failure usually means the order has already filled or
		// disappeared; we log and continue. The fallback still goes through
		// because the venue is the source of truth.
		slog.Warn("cancel after escalation deadline failed (likely already filled)", "orderID", primaryOrder.ID, "error", err)
	}

	// Re-check status after cancel. If the order in fact filled before our
	// cancel arrived, do not double up by sending a fallback MARKET.
	if status := r.lookupOrder(ctx, primaryOrder.ID); status != nil && status.RemainingAmount <= 0 {
		return status.ID, fillPriceOf(*status, signalPrice), nil
	}

	fb, err := r.submit(ctx, wait.FallbackOrder)
	if err != nil {
		return 0, signalPrice, fmt.Errorf("escalation MARKET fallback failed: %w", err)
	}
	return fb.ID, fillPriceOf(fb, signalPrice), nil
}

// submit posts a single order and returns the venue's first response row.
// Errors propagate up unchanged so the caller can decide whether to escalate
// or surface the failure.
func (r *RealExecutor) submit(ctx context.Context, req entity.OrderRequest) (entity.Order, error) {
	orders, err := r.orderClient.CreateOrder(ctx, req)
	if err != nil {
		return entity.Order{}, err
	}
	if len(orders) == 0 {
		return entity.Order{}, errors.New("venue returned no order rows")
	}
	return orders[0], nil
}

// pollUntilFilledOrDeadline polls GetOrders until the target order is
// fully filled (RemainingAmount == 0) or the deadline elapses. Returns the
// terminal Order on fill, nil on timeout/cancel/error.
func (r *RealExecutor) pollUntilFilledOrDeadline(ctx context.Context, orderID int64, deadline time.Time) *entity.Order {
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(r.pollInterval):
		}

		status := r.lookupOrder(ctx, orderID)
		if status == nil {
			// Vanished from the open-orders list — most likely fully filled
			// (the venue removes filled orders from the list). Treat as
			// success and let the caller fall back to signalPrice.
			return &entity.Order{ID: orderID}
		}
		if status.RemainingAmount <= 0 {
			return status
		}
	}
	return nil
}

func (r *RealExecutor) lookupOrder(ctx context.Context, orderID int64) *entity.Order {
	orders, err := r.orderClient.GetOrders(ctx, r.symbolID)
	if err != nil {
		slog.Warn("GetOrders during SOR poll failed", "error", err)
		return nil
	}
	for i := range orders {
		if orders[i].ID == orderID {
			return &orders[i]
		}
	}
	return nil
}

func fillPriceOf(o entity.Order, fallback float64) float64 {
	if o.Price > 0 {
		return o.Price
	}
	return fallback
}
