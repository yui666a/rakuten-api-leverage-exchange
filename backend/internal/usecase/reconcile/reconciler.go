// Package reconcile compares the bot's local view of the world (open
// client_orders, in-memory positions, last-known balance) against the venue's
// authoritative state every minute. Discrepancies escalate through three
// tiers:
//
//  1. Auto-repair (orders): when the bot is missing a venue-side acknowledgement
//     for an order, the reconciler patches client_orders to "reconciled-*".
//  2. Warn (positions / balance drift below halt threshold): publish a
//     "risk_event" so operators see the drift on the dashboard.
//  3. Halt (severe drift): call RiskManager.HaltAutomatic so the bot stops
//     trading until a human inspects.
//
// The reconciler is read-only against the venue. It only writes to the bot's
// own state (client_orders status updates, RiskManager halt) — never sends
// orders or modifies venue positions.
package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// VenueClient is the narrow surface the reconciler needs. It is intentionally
// smaller than repository.OrderClient so test fakes are easy to write.
type VenueClient interface {
	GetOrders(ctx context.Context, symbolID int64) ([]entity.Order, error)
	GetPositions(ctx context.Context, symbolID int64) ([]entity.Position, error)
	GetMyTrades(ctx context.Context, symbolID int64) ([]entity.MyTrade, error)
	GetAssets(ctx context.Context) ([]entity.Asset, error)
}

// LocalState is the bot-side view the reconciler compares against the venue.
// RiskManager satisfies it through tiny adapter methods.
type LocalState interface {
	// Positions returns a defensive copy of the in-memory position list.
	LocalPositions() []entity.Position
	// Balance returns the JPY balance the bot currently believes it has.
	LocalBalance() float64
}

// Halter narrows RiskManager.HaltAutomatic so the test layer doesn't pull in
// the full risk manager.
type Halter interface {
	HaltAutomatic(reason string) bool
}

// Publisher surfaces drift / halt events to the realtime hub. Nil disables
// publishing.
type Publisher interface {
	PublishDrift(kind string, severity string, message string, ts int64)
}

// Config bundles the thresholds. All values are in plain ratios (0.05 = 5%).
type Config struct {
	// Enable toggles the reconciler.
	Enable bool
	// IntervalSec is the reconcile cadence in seconds. 0 falls back to 60.
	IntervalSec int

	// PositionHaltPct halts trading when the absolute difference between
	// venue and local total notional, divided by the larger of the two,
	// exceeds this. 0 disables the halt branch (warn only). 0.5 = 50%.
	PositionHaltPct float64
	// PositionWarnPct emits a warning for any drift above this ratio.
	// Defaults to 0.05 (5%) when 0.
	PositionWarnPct float64

	// BalanceWarnPct emits a warning for any drift above this ratio.
	// Defaults to 0.01 (1%) when 0.
	BalanceWarnPct float64
	// BalanceHaltPct halts trading when balance drift exceeds this ratio.
	// 0 disables. 0.05 = 5%.
	BalanceHaltPct float64

	// OrderTTL is how long a pending/submitted order may stay un-confirmed
	// before the reconciler marks it reconciled-timeout. 0 falls back to 5m.
	OrderTTL time.Duration
}

// DefaultConfig returns conservative thresholds. Everything is opt-in
// (Enable=false) — the composition root flips it on after env wiring.
func DefaultConfig() Config {
	return Config{
		Enable:          false,
		IntervalSec:     60,
		PositionHaltPct: 0.5,
		PositionWarnPct: 0.05,
		BalanceWarnPct:  0.01,
		BalanceHaltPct:  0.05,
		OrderTTL:        5 * time.Minute,
	}
}

// Reconciler runs the three checks. It owns no goroutines on its own; the
// caller drives Start/Run.
type Reconciler struct {
	cfg       Config
	venue     VenueClient
	local     LocalState
	halter    Halter
	publisher Publisher
	orders    repository.ClientOrderRepository
	now       func() time.Time
	symbolID  int64
}

// New constructs a reconciler. halter is required (panics on nil).
func New(
	cfg Config,
	venue VenueClient,
	local LocalState,
	halter Halter,
	orders repository.ClientOrderRepository,
	publisher Publisher,
	symbolID int64,
) *Reconciler {
	if halter == nil {
		panic("reconcile.New: halter must not be nil")
	}
	if cfg.IntervalSec <= 0 {
		cfg.IntervalSec = 60
	}
	if cfg.PositionWarnPct <= 0 {
		cfg.PositionWarnPct = 0.05
	}
	if cfg.BalanceWarnPct <= 0 {
		cfg.BalanceWarnPct = 0.01
	}
	if cfg.OrderTTL <= 0 {
		cfg.OrderTTL = 5 * time.Minute
	}
	return &Reconciler{
		cfg:       cfg,
		venue:     venue,
		local:     local,
		halter:    halter,
		publisher: publisher,
		orders:    orders,
		now:       time.Now,
		symbolID:  symbolID,
	}
}

// SetClock overrides the now() source. Tests inject a fixed clock.
func (r *Reconciler) SetClock(now func() time.Time) {
	if now != nil {
		r.now = now
	}
}

// SetSymbolID updates the symbol the next Run will reconcile against. Called
// from the live pipeline whenever SwitchSymbol fires.
func (r *Reconciler) SetSymbolID(id int64) {
	r.symbolID = id
}

// Run executes one reconciliation pass: orders → positions → balance.
// Errors from individual checks log a warning but do not abort subsequent
// checks; a misbehaving orders endpoint should not blind the operator to
// position drift.
func (r *Reconciler) Run(ctx context.Context) {
	if !r.cfg.Enable {
		return
	}
	if err := r.reconcileOrders(ctx); err != nil {
		slog.Warn("reconcile: orders pass failed", "error", err)
	}
	if err := r.reconcilePositions(ctx); err != nil {
		slog.Warn("reconcile: positions pass failed", "error", err)
	}
	if err := r.reconcileBalance(ctx); err != nil {
		slog.Warn("reconcile: balance pass failed", "error", err)
	}
}

// reconcileOrders walks the pending/submitted client_orders and consults the
// venue's open-orders + my-trades endpoints to confirm or close them out.
func (r *Reconciler) reconcileOrders(ctx context.Context) error {
	if r.orders == nil {
		return nil
	}
	pending, err := r.orders.ListByStatus(ctx, []entity.ClientOrderStatus{
		entity.ClientOrderStatusPending,
		entity.ClientOrderStatusSubmitted,
	}, 200)
	if err != nil {
		return fmt.Errorf("list pending orders: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	openOrders, err := r.venue.GetOrders(ctx, r.symbolID)
	if err != nil {
		return fmt.Errorf("venue GetOrders: %w", err)
	}
	openIDs := make(map[int64]struct{}, len(openOrders))
	for _, o := range openOrders {
		openIDs[o.ID] = struct{}{}
	}

	trades, err := r.venue.GetMyTrades(ctx, r.symbolID)
	if err != nil {
		// trade history failures should not block the rest of reconciliation —
		// fall back to "no fills observed" so timeout logic still fires.
		slog.Warn("reconcile: GetMyTrades failed", "error", err)
		trades = nil
	}
	filledIDs := make(map[int64]struct{}, len(trades))
	for _, t := range trades {
		filledIDs[t.OrderID] = struct{}{}
	}

	now := r.now().UnixMilli()
	ttlMs := r.cfg.OrderTTL.Milliseconds()
	for _, rec := range pending {
		// We can only reconcile records that have an OrderID; pre-submission
		// pending rows (OrderID==0) need to wait for the orderclient layer
		// to fill that in.
		if rec.OrderID == 0 {
			if now-rec.CreatedAt > ttlMs {
				_ = r.orders.UpdateStatus(ctx, rec.ClientOrderID,
					entity.ClientOrderStatusReconciledTimeout, now,
					repository.ClientOrderUpdate{})
			}
			continue
		}

		switch {
		case fmtFilled(filledIDs, rec.OrderID):
			_ = r.orders.UpdateStatus(ctx, rec.ClientOrderID,
				entity.ClientOrderStatusReconciledConfirmed, now,
				repository.ClientOrderUpdate{})
		case fmtOpen(openIDs, rec.OrderID):
			// Still resting on the venue — leave as-is, the order is healthy.
		case now-rec.CreatedAt > ttlMs:
			// TTL elapsed and the order is neither resting nor filled.
			// Most likely cancelled outside our control or a stale row.
			_ = r.orders.UpdateStatus(ctx, rec.ClientOrderID,
				entity.ClientOrderStatusReconciledNotFound, now,
				repository.ClientOrderUpdate{})
		}
	}
	return nil
}

func fmtFilled(filled map[int64]struct{}, id int64) bool {
	_, ok := filled[id]
	return ok
}

func fmtOpen(open map[int64]struct{}, id int64) bool {
	_, ok := open[id]
	return ok
}

// reconcilePositions compares the bot's in-memory positions to the venue's
// authoritative position list and emits drift events / halts when needed.
//
// The metric is signed-net-amount: sum(BUY) − sum(SELL). A 100% drift means
// the bot believes it is long while the venue says flat (or vice versa) —
// the most dangerous case.
func (r *Reconciler) reconcilePositions(ctx context.Context) error {
	venuePositions, err := r.venue.GetPositions(ctx, r.symbolID)
	if err != nil {
		return fmt.Errorf("venue GetPositions: %w", err)
	}
	venueNet := signedNetAmount(venuePositions)
	localNet := signedNetAmount(r.local.LocalPositions())

	delta := math.Abs(venueNet - localNet)
	denom := math.Max(math.Abs(venueNet), math.Abs(localNet))
	if denom <= 0 {
		// Both sides report flat — perfectly aligned, nothing to do.
		return nil
	}
	driftRatio := delta / denom

	now := r.now().UnixMilli()
	if r.cfg.PositionHaltPct > 0 && driftRatio >= r.cfg.PositionHaltPct {
		r.haltAndPublish("reconciliation:position_mismatch",
			fmt.Sprintf("position drift %.1f%% (venue=%.4f local=%.4f)",
				driftRatio*100, venueNet, localNet),
			now)
		return nil
	}
	if driftRatio >= r.cfg.PositionWarnPct {
		r.publish("position_drift", "warning",
			fmt.Sprintf("position drift %.1f%% (venue=%.4f local=%.4f)",
				driftRatio*100, venueNet, localNet),
			now)
	}
	return nil
}

// signedNetAmount collapses a position list into one number: BUY adds,
// SELL subtracts. Symbol mismatches are not handled here — the caller is
// expected to scope the reconciler to one symbol.
func signedNetAmount(pos []entity.Position) float64 {
	net := 0.0
	for _, p := range pos {
		amt := p.RemainingAmount
		if amt <= 0 {
			amt = 0
		}
		if p.OrderSide == entity.OrderSideSell {
			net -= amt
		} else {
			net += amt
		}
	}
	return net
}

// reconcileBalance compares the bot's last-known JPY balance to the venue's
// asset list. Drift can come from manual deposits, withdrawals, or fees the
// bot did not anticipate.
func (r *Reconciler) reconcileBalance(ctx context.Context) error {
	assets, err := r.venue.GetAssets(ctx)
	if err != nil {
		return fmt.Errorf("venue GetAssets: %w", err)
	}
	venueBalance := 0.0
	for _, a := range assets {
		if a.Currency == "JPY" {
			venueBalance = parseFloatSafe(a.OnhandAmount)
			break
		}
	}
	localBalance := r.local.LocalBalance()
	if venueBalance <= 0 || localBalance <= 0 {
		return nil
	}
	delta := math.Abs(venueBalance - localBalance)
	denom := math.Max(venueBalance, localBalance)
	driftRatio := delta / denom

	now := r.now().UnixMilli()
	if r.cfg.BalanceHaltPct > 0 && driftRatio >= r.cfg.BalanceHaltPct {
		r.haltAndPublish("reconciliation:balance_drift",
			fmt.Sprintf("balance drift %.1f%% (venue=%.0f local=%.0f)",
				driftRatio*100, venueBalance, localBalance),
			now)
		return nil
	}
	if driftRatio >= r.cfg.BalanceWarnPct {
		r.publish("balance_drift", "warning",
			fmt.Sprintf("balance drift %.1f%% (venue=%.0f local=%.0f)",
				driftRatio*100, venueBalance, localBalance),
			now)
	}
	return nil
}

func (r *Reconciler) haltAndPublish(reason, detail string, ts int64) {
	r.halter.HaltAutomatic(reason)
	r.publish("halt", "critical", reason+": "+detail, ts)
}

func (r *Reconciler) publish(kind, severity, message string, ts int64) {
	if r.publisher == nil {
		return
	}
	r.publisher.PublishDrift(kind, severity, message, ts)
}

func parseFloatSafe(s string) float64 {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	if err != nil {
		return 0
	}
	return f
}
