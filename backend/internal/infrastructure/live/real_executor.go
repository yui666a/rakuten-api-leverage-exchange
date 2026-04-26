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
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/orderretry"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/sor"
)

// TouchSource provides the latest BestBid / BestAsk for the symbol so the
// SOR can place a sane LIMIT price. It mirrors booklimit.BookSource but
// returns only the touch (the SOR does not need full depth).
type TouchSource interface {
	LatestBefore(ctx context.Context, symbolID, ts int64) (entity.Orderbook, bool, error)
}

// PositionChangePublisher is the side effect SyncPositions runs whenever the
// venue-side position state changes vs the executor's last snapshot. Used to
// push immediate updates to the dashboard so a manual /positions page does
// not have to wait the full sync interval.
type PositionChangePublisher interface {
	PublishPositionUpdate(symbolID int64, positions []eventengine.Position)
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
	// lastOrderAtMillis records the most recent timestamp at which the
	// executor placed an order with the venue. The pipeline reads this via
	// LastOrderAt() to switch position polling into a faster cadence right
	// after activity, when state drift is most likely to matter.
	lastOrderAtMillis int64
	// positionPublisher is invoked from SyncPositions when the local view
	// changes (count or amount), so the dashboard updates immediately.
	positionPublisher PositionChangePublisher
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

// WithPositionPublisher wires a PositionChangePublisher so SyncPositions can
// push immediate updates to the realtime hub when the venue state changes.
func WithPositionPublisher(p PositionChangePublisher) RealExecutorOption {
	return func(r *RealExecutor) { r.positionPublisher = p }
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

// OpenWithUrgency dispatches to the SOR strategy that best matches the
// caller's urgency hint:
//   - urgent  → StrategyMarket (forces immediate market lift)
//   - passive → StrategyPostOnlyEscalate (favours the maker rebate)
//   - normal / "" → honours the configured default router
//
// Strategies the router was not configured for (e.g. urgent on a Market-only
// deployment) just round-trip back to the configured default.
func (r *RealExecutor) OpenWithUrgency(symbolID int64, side entity.OrderSide, signalPrice, amount float64, reason string, timestamp int64, urgency entity.SignalUrgency) (entity.OrderEvent, error) {
	switch urgency {
	case entity.SignalUrgencyUrgent:
		return r.openWithStrategy(symbolID, side, signalPrice, amount, reason, timestamp, sor.StrategyMarket)
	case entity.SignalUrgencyPassive:
		return r.openWithStrategy(symbolID, side, signalPrice, amount, reason, timestamp, sor.StrategyPostOnlyEscalate)
	default:
		return r.Open(symbolID, side, signalPrice, amount, reason, timestamp)
	}
}

// openWithStrategy is the core open routine parameterised by SOR strategy.
// The default router stays untouched so subsequent Open calls keep using
// the configured defaults.
func (r *RealExecutor) openWithStrategy(symbolID int64, side entity.OrderSide, signalPrice, amount float64, reason string, timestamp int64, strat sor.Strategy) (entity.OrderEvent, error) {
	if amount <= 0 {
		return entity.OrderEvent{}, fmt.Errorf("amount must be positive")
	}
	if signalPrice <= 0 {
		return entity.OrderEvent{}, fmt.Errorf("signal price must be positive")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := len(r.positions) - 1; i >= 0; i-- {
		pos := r.positions[i]
		if pos.SymbolID == symbolID && pos.Side != side {
			_, _, _ = r.closeLocked(pos.PositionID, signalPrice, "reverse_signal", timestamp)
		}
	}

	tempRouter := sor.New(sor.Config{Strategy: strat})
	in := sor.SelectInput{SymbolID: symbolID, Side: side, Amount: amount}
	if r.touchSrc != nil {
		ob, ok, err := r.touchSrc.LatestBefore(context.Background(), symbolID, timestamp)
		if err == nil && ok {
			in.BestBid = ob.BestBid
			in.BestAsk = ob.BestAsk
		}
	}
	plan := tempRouter.Plan(in)
	orderID, fillPrice, err := r.runPlan(context.Background(), plan, signalPrice)
	if err != nil {
		return entity.OrderEvent{}, fmt.Errorf("failed to execute open plan (urgency-routed): %w", err)
	}

	slog.Info("live order opened with urgency",
		"orderID", orderID, "symbolID", symbolID, "side", side,
		"amount", amount, "reason", reason, "strategy", plan.Strategy,
	)

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
		OrderID:          orderID,
		SymbolID:         symbolID,
		Side:             string(side),
		Action:           "open",
		Price:            fillPrice,
		Amount:           amount,
		Reason:           reason,
		Timestamp:        timestamp,
		Trigger:          entity.DecisionTriggerBarClose,
		OpenedPositionID: posID,
	}, nil
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
		OrderID:          orderID,
		SymbolID:         symbolID,
		Side:             string(side),
		Action:           "open",
		Price:            fillPrice,
		Amount:           amount,
		Reason:           reason,
		Timestamp:        timestamp,
		Trigger:          entity.DecisionTriggerBarClose,
		OpenedPositionID: posID,
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

	closeOrder, err := r.submit(context.Background(), req)
	if err != nil {
		return entity.OrderEvent{}, nil, fmt.Errorf("failed to create close order: %w", err)
	}

	orderID := closeOrder.ID
	exitPrice := signalPrice
	if closeOrder.Price > 0 {
		exitPrice = closeOrder.Price
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
		OrderID:          orderID,
		SymbolID:         pos.SymbolID,
		Side:             string(pos.Side),
		Action:           "close",
		Price:            exitPrice,
		Amount:           pos.Amount,
		Reason:           reason,
		Timestamp:        timestamp,
		ClosedPositionID: pos.PositionID,
		// Trigger is left empty here. The caller (TickRiskHandler for SL/TP/
		// trailing closes; ExecutionHandler-driven reverse for bar-close
		// closes) sets Trigger to TICK_SLTP / TICK_TRAILING / BAR_CLOSE
		// before forwarding the event onto the bus.
	}, trade, nil
}

// LastOrderAt returns the unix-millis at which the executor most recently
// placed an order with the venue. 0 means "no order placed since startup".
// The live pipeline uses this to gate adaptive position-sync polling.
func (r *RealExecutor) LastOrderAt() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastOrderAtMillis
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
// When the new snapshot differs from the previous in-memory state and a
// PositionChangePublisher is wired, an immediate "position_update" event is
// published — this is what closes the gap a polling-only executor leaves on
// fast fills and venue-side cancels.
func (r *RealExecutor) SyncPositions(ctx context.Context) error {
	apiPositions, err := r.orderClient.GetPositions(ctx, r.symbolID)
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	r.mu.Lock()

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
	changed := positionsChanged(r.positions, synced)
	r.positions = synced
	publisher := r.positionPublisher
	r.mu.Unlock()

	slog.Info("positions synced from API",
		"symbolID", r.symbolID,
		"count", len(synced),
		"changed", changed,
	)

	if changed && publisher != nil {
		// Defensive copy: the executor must keep mutating the slice on the
		// next sync so callers should not assume slice ownership.
		out := make([]eventengine.Position, len(synced))
		copy(out, synced)
		publisher.PublishPositionUpdate(r.symbolID, out)
	}
	return nil
}

// positionsChanged reports whether two snapshots represent different state
// for the purposes of triggering a publish. Order is not stable from the
// venue so we compare keyed by PositionID.
func positionsChanged(prev, next []eventengine.Position) bool {
	if len(prev) != len(next) {
		return true
	}
	prevByID := make(map[int64]eventengine.Position, len(prev))
	for _, p := range prev {
		prevByID[p.PositionID] = p
	}
	for _, n := range next {
		p, ok := prevByID[n.PositionID]
		if !ok {
			return true
		}
		if p.Side != n.Side || p.Amount != n.Amount || p.EntryPrice != n.EntryPrice {
			return true
		}
	}
	return false
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

	// Iceberg flow: only Submit + WaitInterval steps, no WaitOrEscalate.
	// We send slices in order, sleep between them, and surface the *last*
	// successful slice's order ID + fillPrice. Errors on intermediate
	// slices are logged and skipped so a single transient failure doesn't
	// orphan the rest of the iceberg.
	if plan.Strategy == sor.StrategyIceberg {
		return r.runIcebergPlan(ctx, plan, signalPrice)
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

// runIcebergPlan sequentially submits each Submit step in the plan with the
// configured WaitInterval pauses between them. Returns the first successful
// slice's order ID and the volume-weighted average fill price across all
// completed slices. Per-slice failures are logged and skipped so partial
// fills still produce useful order tracking.
func (r *RealExecutor) runIcebergPlan(ctx context.Context, plan sor.Plan, signalPrice float64) (int64, float64, error) {
	var (
		firstID       int64
		costAccum     float64
		amtAccum      float64
		anySucceeded  bool
		lastErr       error
	)
	for _, step := range plan.Steps {
		select {
		case <-ctx.Done():
			return firstID, fallbackVWAP(costAccum, amtAccum, signalPrice), ctx.Err()
		default:
		}
		switch step.Kind {
		case sor.StepKindSubmit:
			ord, err := r.submit(ctx, step.Order)
			if err != nil {
				slog.Warn("iceberg slice submit failed", "error", err)
				lastErr = err
				continue
			}
			anySucceeded = true
			if firstID == 0 {
				firstID = ord.ID
			}
			price := fillPriceOf(ord, signalPrice)
			costAccum += price * step.Order.OrderData.Amount
			amtAccum += step.Order.OrderData.Amount
		case sor.StepKindWaitInterval:
			if step.WaitMs <= 0 {
				continue
			}
			select {
			case <-ctx.Done():
				return firstID, fallbackVWAP(costAccum, amtAccum, signalPrice), ctx.Err()
			case <-time.After(time.Duration(step.WaitMs) * time.Millisecond):
			}
		default:
			// Skip unexpected kinds — Plan was constructed by the SOR so
			// hitting one means a mismatch between Selector and runPlan,
			// not a venue issue.
			slog.Warn("iceberg unexpected step kind", "kind", step.Kind)
		}
	}
	if !anySucceeded {
		return 0, signalPrice, lastErr
	}
	return firstID, fallbackVWAP(costAccum, amtAccum, signalPrice), nil
}

func fallbackVWAP(cost, amount, fallback float64) float64 {
	if amount <= 0 {
		return fallback
	}
	return cost / amount
}

// submit posts a single order and returns the venue's first response row.
// Errors propagate up unchanged so the caller can decide whether to escalate
// or surface the failure. Records the submission timestamp so the pipeline
// can adapt its position-polling cadence.
//
// Uses CreateOrderRaw + orderretry.OnRateLimit so a 20010 (rate limit) hit at
// order-submission time is automatically retried. Other failure modes (transport
// error, business errors like 50048) propagate unchanged so the post-only
// rejection path in runPlan keeps working.
func (r *RealExecutor) submit(ctx context.Context, req entity.OrderRequest) (entity.Order, error) {
	out, fnErr := orderretry.OnRateLimit(ctx, time.Sleep, func() (repository.CreateOrderOutcome, error) {
		return r.orderClient.CreateOrderRaw(ctx, req)
	})
	r.lastOrderAtMillis = time.Now().UnixMilli()
	if fnErr != nil {
		return entity.Order{}, fnErr
	}
	if out.TransportError != nil {
		return entity.Order{}, out.TransportError
	}
	if out.HTTPError != nil {
		return entity.Order{}, out.HTTPError
	}
	if out.ParseError != nil {
		return entity.Order{}, out.ParseError
	}
	if len(out.Orders) == 0 {
		return entity.Order{}, errors.New("venue returned no order rows")
	}
	return out.Orders[0], nil
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
