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

// PositionConfirmedSink receives one PositionConfirmedEvent per newly
// confirmed position discovered during SyncPositions. It is the
// authoritative hook for downstream handlers (ExitPlan shadow, Risk
// scaffolding) that need to act on the real venue fill price rather than
// the submit-time signalPrice. See
// docs/design/2026-05-12-position-confirmed-only.md.
//
// Implementations must be non-blocking — the executor calls them while
// holding no lock, but a slow sink would delay the next SyncPositions
// invocation. A typical implementation hands the event off to the
// event bus and returns.
type PositionConfirmedSink interface {
	PublishPositionConfirmed(ev entity.PositionConfirmedEvent)
}

// RealExecutor implements eventengine.OrderExecutor by executing real orders
// via the Rakuten API OrderClient.
//
// Contract: Positions() returns only confirmed positions — that is, ones
// whose EntryPrice has been populated from a venue source (SyncPositions or
// GetMyTrades). Positions that are still pending venue confirmation must
// never appear in Positions() and so cannot be observed by downstream Risk /
// Exit handlers. This is the core invariant introduced by the
// docs/design/2026-05-12-position-confirmed-only.md ADR after a production
// incident where a stale signalPrice fallback corrupted TP/SL calculation.
type RealExecutor struct {
	orderClient   repository.OrderClient
	symbolID      int64
	positions     []eventengine.Position
	mu            sync.Mutex
	spreadPercent float64
	nextOrderID   int64
	// pendingOrders tracks venue submissions whose fills have not yet been
	// observed via SyncPositions or GetMyTrades. Entries are removed when
	// the venue confirms a Position for the OrderID. Pending entries are
	// invisible to Positions() and therefore to Risk / Exit handlers.
	pendingOrders map[int64]pendingOpen
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
	// positionConfirmedSink receives a PositionConfirmedEvent for each
	// newly confirmed position observed during SyncPositions. Optional
	// (nil = no event-bus integration); when set, downstream handlers
	// like the ExitPlan shadow can stop relying on OrderEvent.Price and
	// act on the real venue fill instead.
	positionConfirmedSink PositionConfirmedSink
	// pendingTTL bounds how long an unconfirmed order may sit in
	// pendingOrders before SweepStalePending logs it. Defaults to 60 s.
	pendingTTL time.Duration
}

// pendingOpen is the executor's bookkeeping for a venue submission whose
// fill has not yet been confirmed. It is intentionally minimal: anything
// the Risk / Exit layer needs (price, exact fill amount) must come from
// SyncPositions or GetMyTrades, never from this struct.
type pendingOpen struct {
	OrderID     int64
	SymbolID    int64
	Side        entity.OrderSide
	Amount      float64
	SubmittedAt int64
	Reason      string
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

// WithPositionConfirmedSink wires a sink that receives one
// PositionConfirmedEvent per newly confirmed position. This is the
// integration point for the ExitPlan shadow / Risk scaffolding to react
// on real venue fills rather than submit-time OrderEvent prices.
func WithPositionConfirmedSink(sink PositionConfirmedSink) RealExecutorOption {
	return func(r *RealExecutor) { r.positionConfirmedSink = sink }
}

// SetPositionConfirmedSink installs (or replaces) the sink at runtime.
// The pipeline constructs the EventEngine after the executor, so this
// setter lets the wiring connect the two without forcing a constructor
// dependency cycle. Passing nil clears the sink.
func (r *RealExecutor) SetPositionConfirmedSink(sink PositionConfirmedSink) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.positionConfirmedSink = sink
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
		pendingOrders:            make(map[int64]pendingOpen),
		pendingTTL:               60 * time.Second,
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
//
// Per the docs/design/2026-05-12-position-confirmed-only.md ADR, this
// routine no longer appends to r.positions on submit. The position is only
// added once the venue confirms the fill (via SyncPositions / GetMyTrades),
// which closes the window during which Risk handlers could otherwise
// observe a phantom EntryPrice and misfire TP/SL exits.
func (r *RealExecutor) openWithStrategy(symbolID int64, side entity.OrderSide, signalPrice, amount float64, reason string, timestamp int64, strat sor.Strategy) (entity.OrderEvent, error) {
	if amount <= 0 {
		return entity.OrderEvent{}, fmt.Errorf("amount must be positive")
	}
	if signalPrice <= 0 {
		return entity.OrderEvent{}, fmt.Errorf("signal price must be positive")
	}
	r.mu.Lock()
	for i := len(r.positions) - 1; i >= 0; i-- {
		pos := r.positions[i]
		if pos.SymbolID == symbolID && pos.Side != side {
			_, _, _ = r.closeLocked(pos.PositionID, signalPrice, "reverse_signal", timestamp)
		}
	}
	r.mu.Unlock()

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

	r.recordPending(orderID, symbolID, side, amount, reason, timestamp)
	if err := r.confirmPendingViaSync(context.Background(), orderID); err != nil {
		slog.Warn("post-open SyncPositions failed; pending will be confirmed by the next sync cycle",
			"orderID", orderID, "error", err,
		)
	}

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
		OpenedPositionID: orderID,
	}, nil
}

// Open creates a real order via the configured SOR plan.
//
// Per the docs/design/2026-05-12-position-confirmed-only.md ADR, this no
// longer pushes a phantom Position onto r.positions at submit time. The
// fill is registered in pendingOrders and the position only becomes
// visible to Positions() after the venue confirms it via SyncPositions.
func (r *RealExecutor) Open(symbolID int64, side entity.OrderSide, signalPrice, amount float64, reason string, timestamp int64) (entity.OrderEvent, error) {
	if amount <= 0 {
		return entity.OrderEvent{}, fmt.Errorf("amount must be positive")
	}
	if signalPrice <= 0 {
		return entity.OrderEvent{}, fmt.Errorf("signal price must be positive")
	}

	r.mu.Lock()
	// Reverse signal: close opposite positions first.
	for i := len(r.positions) - 1; i >= 0; i-- {
		pos := r.positions[i]
		if pos.SymbolID == symbolID && pos.Side != side {
			_, _, _ = r.closeLocked(pos.PositionID, signalPrice, "reverse_signal", timestamp)
		}
	}
	r.mu.Unlock()

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

	r.recordPending(orderID, symbolID, side, amount, reason, timestamp)
	if err := r.confirmPendingViaSync(context.Background(), orderID); err != nil {
		slog.Warn("post-open SyncPositions failed; pending will be confirmed by the next sync cycle",
			"orderID", orderID, "error", err,
		)
	}

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
		OpenedPositionID: orderID,
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
//
// Contract (Position confirmed-only, per
// docs/design/2026-05-12-position-confirmed-only.md):
//   - Every returned Position has EntryPrice > 0 sourced from the venue
//     (SyncPositions / GetMyTrades), never from a signalPrice fallback.
//   - Pending submissions that have not yet been confirmed by the venue
//     are invisible to this method and so cannot be observed by Risk /
//     Exit handlers.
//   - The slice is a defensive copy: callers may iterate or filter without
//     fearing concurrent SyncPositions mutation. Callers that need a
//     stable snapshot for the duration of a tick should still capture
//     this once at the top of their handler.
func (r *RealExecutor) Positions() []eventengine.Position {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]eventengine.Position, len(r.positions))
	copy(out, r.positions)
	return out
}

// recordPending registers a submitted-but-not-yet-confirmed order so the
// next sync (or a manual SweepStalePending) can match it against venue
// positions. The reason / submittedAt fields are kept for operational
// debugging; Risk / Exit handlers must not consult pendingOrders.
func (r *RealExecutor) recordPending(orderID, symbolID int64, side entity.OrderSide, amount float64, reason string, timestamp int64) {
	if orderID == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.pendingOrders[orderID]; exists {
		// venue-issued OrderIDs are unique, so a duplicate here means we
		// somehow submitted the same order twice without it being
		// confirmed. Log loudly: this is an integrity smell, not a normal
		// state transition.
		slog.Warn("recordPending: pendingOrders already has this OrderID; overwriting",
			"orderID", orderID, "symbolID", symbolID,
		)
	}
	r.pendingOrders[orderID] = pendingOpen{
		OrderID:     orderID,
		SymbolID:    symbolID,
		Side:        side,
		Amount:      amount,
		SubmittedAt: timestamp,
		Reason:      reason,
	}
}

// confirmPendingViaSync immediately polls the venue's GetPositions for the
// open we just submitted, so the position becomes visible to downstream
// handlers without waiting for the periodic SyncPositions tick. Errors
// are propagated to the caller (which logs and falls back to the periodic
// sync) — we do not surface them as a hard failure because the venue has
// already accepted the order.
//
// A cancelled context short-circuits without calling the venue: shutdown
// paths must not block on a sync that has no chance of succeeding.
func (r *RealExecutor) confirmPendingViaSync(ctx context.Context, orderID int64) error {
	if orderID == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.SyncPositions(ctx)
}

// PendingOrdersCount returns the number of submissions still awaiting
// venue confirmation. Intended for tests and operational telemetry; the
// Risk / Exit layer must continue to consult Positions() only.
func (r *RealExecutor) PendingOrdersCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pendingOrders)
}

// StalePendingEntry summarises a pending order that exceeded its TTL
// for operational observability. SweepStalePending returns these so the
// caller can build alerts / metrics without re-implementing the
// matching logic.
type StalePendingEntry struct {
	OrderID     int64
	SymbolID    int64
	Side        entity.OrderSide
	Amount      float64
	SubmittedAt int64
	AgeMs       int64
	Reason      string
}

// SweepStalePending iterates pendingOrders and logs entries that have
// exceeded the configured pendingTTL. It does not remove them — that
// happens during SyncPositions when the venue either reveals a matching
// Position or implicitly disowns the order. The intent is operational
// visibility, not garbage collection.
//
// Returns each stale entry so the caller can surface IDs / ages in
// metrics or alerts. An empty slice means everything pending is healthy.
func (r *RealExecutor) SweepStalePending(nowMillis int64) []StalePendingEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	ttl := r.pendingTTL.Milliseconds()
	var stale []StalePendingEntry
	for orderID, po := range r.pendingOrders {
		age := nowMillis - po.SubmittedAt
		if age > ttl {
			stale = append(stale, StalePendingEntry{
				OrderID:     orderID,
				SymbolID:    po.SymbolID,
				Side:        po.Side,
				Amount:      po.Amount,
				SubmittedAt: po.SubmittedAt,
				AgeMs:       age,
				Reason:      po.Reason,
			})
			slog.Warn("pending order exceeded TTL; venue may have rejected silently or sync is lagging",
				"orderID", orderID,
				"symbolID", po.SymbolID,
				"side", po.Side,
				"amount", po.Amount,
				"submittedAt", po.SubmittedAt,
				"ageMs", age,
			)
		}
	}
	return stale
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
	skippedZeroPrice := 0
	for _, ap := range apiPositions {
		// Confirmed-only contract: only positions whose venue Price was
		// populated may enter our position book. A zero price means the
		// venue has not finished settling this fill yet; we'll pick it up
		// on the next sync cycle rather than silently surface a phantom
		// EntryPrice (the root cause of the 2026-05-12 incident).
		if ap.Price <= 0 {
			skippedZeroPrice++
			slog.Warn("SyncPositions: venue returned position with non-positive price; skipping until confirmed",
				"positionID", ap.ID,
				"symbolID", ap.SymbolID,
				"price", ap.Price,
			)
			continue
		}
		synced = append(synced, eventengine.Position{
			PositionID:     ap.ID,
			SymbolID:       ap.SymbolID,
			Side:           ap.OrderSide,
			EntryPrice:     ap.Price,
			Amount:         ap.RemainingAmount,
			EntryTimestamp: ap.CreatedAt,
		})
	}
	// Guard against venue-side races that briefly return all positions
	// with Price=0 (observed only theoretically — see Codex review on
	// PR #1). Without this guard the executor would atomically wipe the
	// position book, dropping every Risk / Exit handler's reference and
	// re-creating the very race the confirmed-only contract is meant to
	// close. The next sync cycle will catch up to the venue's real state.
	if len(apiPositions) > 0 && len(synced) == 0 && len(r.positions) > 0 {
		slog.Warn("SyncPositions: every venue position had Price<=0; keeping previous snapshot to avoid spurious wipe",
			"symbolID", r.symbolID,
			"previousCount", len(r.positions),
			"skippedZeroPrice", skippedZeroPrice,
		)
		r.mu.Unlock()
		return nil
	}
	// Compute newly-confirmed positions so we can emit one
	// PositionConfirmedEvent per new fill. We snapshot the previous IDs
	// before swapping the slice, then walk apiPositions so the emitted
	// event carries the parent OrderID directly (synced is the local
	// projection and does not preserve OrderID).
	previousIDs := make(map[int64]struct{}, len(r.positions))
	for _, p := range r.positions {
		previousIDs[p.PositionID] = struct{}{}
	}
	changed := positionsChanged(r.positions, synced)
	r.positions = synced
	// Any pending order whose OrderID now matches a venue-confirmed
	// position is no longer pending. Rakuten's `Position.OrderID` is the
	// venue's parent order id — distinct from `Position.ID` (the
	// position id) — so we must match against the source field that
	// pendingOrders is keyed by, not against PositionID. This handles
	// the split-fill case naturally because every child position
	// inherits the parent OrderID. Pending entries that survive here
	// get surfaced by SweepStalePending after the TTL.
	if len(r.pendingOrders) > 0 && len(apiPositions) > 0 {
		for _, ap := range apiPositions {
			if ap.OrderID == 0 {
				continue
			}
			if _, ok := r.pendingOrders[ap.OrderID]; ok {
				delete(r.pendingOrders, ap.OrderID)
			}
		}
	}
	publisher := r.positionPublisher
	confirmedSink := r.positionConfirmedSink
	// Build the list of confirmed events while still under the lock so
	// the snapshot we hand off is consistent. We deliberately emit
	// after Unlock so a slow sink cannot block subsequent syncs.
	var confirmedEvents []entity.PositionConfirmedEvent
	if confirmedSink != nil {
		for _, ap := range apiPositions {
			if ap.Price <= 0 {
				continue
			}
			if _, existed := previousIDs[ap.ID]; existed {
				continue
			}
			confirmedEvents = append(confirmedEvents, entity.PositionConfirmedEvent{
				PositionID:     ap.ID,
				OrderID:        ap.OrderID,
				SymbolID:       ap.SymbolID,
				Side:           ap.OrderSide,
				EntryPrice:     ap.Price,
				Amount:         ap.RemainingAmount,
				EntryTimestamp: ap.CreatedAt,
				Timestamp:      time.Now().UnixMilli(),
			})
		}
	}
	r.mu.Unlock()

	for _, ev := range confirmedEvents {
		confirmedSink.PublishPositionConfirmed(ev)
	}

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
		return primaryOrder.ID, r.resolveFillPrice(ctx, primaryOrder, signalPrice), nil
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
		return fb.ID, r.resolveFillPrice(ctx, fb, signalPrice), nil
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
		return status.ID, r.resolveFillPrice(ctx, *status, signalPrice), nil
	}

	fb, err := r.submit(ctx, wait.FallbackOrder)
	if err != nil {
		return 0, signalPrice, fmt.Errorf("escalation MARKET fallback failed: %w", err)
	}
	return fb.ID, r.resolveFillPrice(ctx, fb, signalPrice), nil
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
			price := r.resolveFillPrice(ctx, ord, signalPrice)
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

// resolveFillPrice attempts to get the actual fill price from the venue.
//
// Background: Rakuten Wallet's create-order response does not populate
// `Order.Price` for MARKET orders that fill immediately. A naive
// `fillPriceOf(o, signalPrice)` then returns the previous-bar close
// (signal price) as the EntryPrice, which corrupts TP/SL calculations
// downstream (positions get exit levels anchored to a phantom price up
// to several percent away from the true fill). Observed in production
// on 2026-05-12 08:45 JST: a BUY filled at ~¥9,219 was recorded with
// EntryPrice=¥9,168.4 (the previous bar's close), causing the TickRisk
// handler to erroneously fire `take_profit` ~18 s later on a spurious
// high tick.
//
// Resolution strategy:
//  1. If the submit response already carries a price, trust it.
//  2. Otherwise poll GetOrders for up to fillPriceMaxAttempts iterations
//     to catch the venue-side fill record.
//  3. As a last resort, return signalPrice but emit a WARN so the bad
//     state is visible in the operational log instead of silently
//     poisoning the position book.
func (r *RealExecutor) resolveFillPrice(ctx context.Context, o entity.Order, signalPrice float64) float64 {
	if o.Price > 0 {
		return o.Price
	}
	if o.ID == 0 {
		slog.Warn("resolveFillPrice: order has no ID; using signalPrice fallback",
			"signalPrice", signalPrice,
		)
		return signalPrice
	}
	const fillPriceMaxAttempts = 3
	for attempt := 0; attempt < fillPriceMaxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				slog.Warn("resolveFillPrice: context cancelled mid-poll; using signalPrice fallback",
					"orderID", o.ID, "signalPrice", signalPrice, "attempt", attempt,
				)
				return signalPrice
			case <-time.After(r.pollInterval):
			}
		}
		status := r.lookupOrder(ctx, o.ID)
		if status != nil && status.Price > 0 {
			return status.Price
		}
	}
	slog.Warn("resolveFillPrice: venue did not populate fill price; using signalPrice fallback (entry/exit levels may be inaccurate until SyncPositions reconciles)",
		"orderID", o.ID,
		"signalPrice", signalPrice,
		"attempts", fillPriceMaxAttempts,
	)
	return signalPrice
}
