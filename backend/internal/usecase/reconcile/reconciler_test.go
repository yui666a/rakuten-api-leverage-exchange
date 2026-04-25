package reconcile

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// --- fakes -------------------------------------------------------------

type fakeVenue struct {
	orders    []entity.Order
	positions []entity.Position
	trades    []entity.MyTrade
	jpy       float64
	getOrdersErr    error
	getPositionsErr error
	getTradesErr    error
	getAssetsErr    error
}

func (f *fakeVenue) GetOrders(_ context.Context, _ int64) ([]entity.Order, error) {
	if f.getOrdersErr != nil {
		return nil, f.getOrdersErr
	}
	return f.orders, nil
}
func (f *fakeVenue) GetPositions(_ context.Context, _ int64) ([]entity.Position, error) {
	if f.getPositionsErr != nil {
		return nil, f.getPositionsErr
	}
	return f.positions, nil
}
func (f *fakeVenue) GetMyTrades(_ context.Context, _ int64) ([]entity.MyTrade, error) {
	if f.getTradesErr != nil {
		return nil, f.getTradesErr
	}
	return f.trades, nil
}
func (f *fakeVenue) GetAssets(_ context.Context) ([]entity.Asset, error) {
	if f.getAssetsErr != nil {
		return nil, f.getAssetsErr
	}
	return []entity.Asset{{Currency: "JPY", OnhandAmount: strconv.FormatFloat(f.jpy, 'f', 2, 64)}}, nil
}

type fakeLocal struct {
	pos     []entity.Position
	balance float64
}

func (f *fakeLocal) LocalPositions() []entity.Position { return f.pos }
func (f *fakeLocal) LocalBalance() float64             { return f.balance }

type fakeHalter struct{ reasons []string }

func (h *fakeHalter) HaltAutomatic(reason string) bool {
	h.reasons = append(h.reasons, reason)
	return true
}

type fakePublisher struct {
	mu     sync.Mutex
	events []struct {
		kind, severity, message string
	}
}

func (p *fakePublisher) PublishDrift(kind, severity, message string, _ int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, struct{ kind, severity, message string }{kind, severity, message})
}

// fakeOrderRepo implements just the methods the reconciler exercises.
type fakeOrderRepo struct {
	listing []repository.ClientOrderRecord
	updates []struct {
		id     string
		status entity.ClientOrderStatus
	}
}

func (r *fakeOrderRepo) Find(_ context.Context, _ string) (*repository.ClientOrderRecord, error) {
	return nil, nil
}
func (r *fakeOrderRepo) Save(_ context.Context, _ repository.ClientOrderRecord) error { return nil }
func (r *fakeOrderRepo) InsertOrGet(_ context.Context, _ repository.ClientOrderRecord) (*repository.ClientOrderRecord, bool, error) {
	return nil, false, nil
}
func (r *fakeOrderRepo) UpdateStatus(_ context.Context, id string, status entity.ClientOrderStatus, _ int64, _ repository.ClientOrderUpdate) error {
	r.updates = append(r.updates, struct {
		id     string
		status entity.ClientOrderStatus
	}{id, status})
	return nil
}
func (r *fakeOrderRepo) ListByStatus(_ context.Context, _ []entity.ClientOrderStatus, _ int) ([]repository.ClientOrderRecord, error) {
	return r.listing, nil
}
func (r *fakeOrderRepo) DeleteExpired(_ context.Context, _ int64) error { return nil }

// --- helpers -----------------------------------------------------------

func newReconciler(t *testing.T, cfg Config, v *fakeVenue, l *fakeLocal, h *fakeHalter, repo repository.ClientOrderRepository, pub Publisher) *Reconciler {
	t.Helper()
	cfg.Enable = true
	r := New(cfg, v, l, h, repo, pub, 7)
	r.SetClock(func() time.Time { return time.UnixMilli(10_000_000) })
	return r
}

// --- tests -------------------------------------------------------------

func TestReconciler_DisabledIsNoop(t *testing.T) {
	v := &fakeVenue{positions: []entity.Position{{OrderSide: entity.OrderSideBuy, RemainingAmount: 5}}}
	l := &fakeLocal{}
	h := &fakeHalter{}
	r := New(Config{Enable: false}, v, l, h, nil, nil, 7)
	r.Run(context.Background())
	if len(h.reasons) > 0 {
		t.Fatalf("expected no halts, got %v", h.reasons)
	}
}

func TestReconciler_PositionHaltOnLargeMismatch(t *testing.T) {
	v := &fakeVenue{
		positions: []entity.Position{}, // venue says flat
	}
	l := &fakeLocal{
		pos: []entity.Position{{OrderSide: entity.OrderSideBuy, RemainingAmount: 1.0}},
	}
	h := &fakeHalter{}
	pub := &fakePublisher{}
	r := newReconciler(t, Config{PositionHaltPct: 0.5, PositionWarnPct: 0.05}, v, l, h, nil, pub)
	r.Run(context.Background())
	if len(h.reasons) != 1 || h.reasons[0] != "reconciliation:position_mismatch" {
		t.Fatalf("expected position_mismatch halt, got %v", h.reasons)
	}
}

func TestReconciler_PositionWarnOnly(t *testing.T) {
	// venue=1.0 BUY, local=1.05 BUY → 5% drift below 50% halt threshold
	v := &fakeVenue{positions: []entity.Position{{OrderSide: entity.OrderSideBuy, RemainingAmount: 1.0}}}
	l := &fakeLocal{pos: []entity.Position{{OrderSide: entity.OrderSideBuy, RemainingAmount: 1.05}}}
	h := &fakeHalter{}
	pub := &fakePublisher{}
	r := newReconciler(t, Config{PositionHaltPct: 0.5, PositionWarnPct: 0.04}, v, l, h, nil, pub)
	r.Run(context.Background())
	if len(h.reasons) > 0 {
		t.Fatalf("expected no halt, got %v", h.reasons)
	}
	if len(pub.events) != 1 || pub.events[0].kind != "position_drift" {
		t.Fatalf("expected one position_drift event, got %v", pub.events)
	}
}

func TestReconciler_PositionFlatOnBothSidesIsAligned(t *testing.T) {
	v := &fakeVenue{}
	l := &fakeLocal{}
	h := &fakeHalter{}
	pub := &fakePublisher{}
	r := newReconciler(t, Config{PositionHaltPct: 0.5}, v, l, h, nil, pub)
	r.Run(context.Background())
	if len(h.reasons) > 0 || len(pub.events) > 0 {
		t.Fatalf("flat-flat is aligned: halts=%v events=%v", h.reasons, pub.events)
	}
}

func TestReconciler_BalanceHalt(t *testing.T) {
	v := &fakeVenue{jpy: 50_000}
	l := &fakeLocal{balance: 100_000} // 50% drift
	h := &fakeHalter{}
	pub := &fakePublisher{}
	r := newReconciler(t, Config{BalanceHaltPct: 0.05, BalanceWarnPct: 0.01}, v, l, h, nil, pub)
	r.Run(context.Background())
	if len(h.reasons) != 1 || h.reasons[0] != "reconciliation:balance_drift" {
		t.Fatalf("expected balance halt, got %v", h.reasons)
	}
}

func TestReconciler_BalanceWarnOnly(t *testing.T) {
	v := &fakeVenue{jpy: 99_000}
	l := &fakeLocal{balance: 100_000} // ~1% drift
	h := &fakeHalter{}
	pub := &fakePublisher{}
	r := newReconciler(t, Config{BalanceHaltPct: 0.05, BalanceWarnPct: 0.005}, v, l, h, nil, pub)
	r.Run(context.Background())
	if len(h.reasons) > 0 {
		t.Fatalf("expected no halt, got %v", h.reasons)
	}
	if len(pub.events) != 1 || pub.events[0].kind != "balance_drift" {
		t.Fatalf("expected balance_drift event, got %v", pub.events)
	}
}

func TestReconciler_OrderConfirmsFromMyTrades(t *testing.T) {
	now := time.UnixMilli(10_000_000)
	v := &fakeVenue{
		orders: []entity.Order{},
		trades: []entity.MyTrade{{OrderID: 555}},
	}
	repo := &fakeOrderRepo{
		listing: []repository.ClientOrderRecord{
			{ClientOrderID: "co-A", Status: entity.ClientOrderStatusSubmitted, OrderID: 555, CreatedAt: now.Add(-1 * time.Minute).UnixMilli()},
		},
	}
	r := newReconciler(t, Config{OrderTTL: 5 * time.Minute}, v, &fakeLocal{}, &fakeHalter{}, repo, nil)
	r.Run(context.Background())
	if len(repo.updates) != 1 || repo.updates[0].status != entity.ClientOrderStatusReconciledConfirmed {
		t.Fatalf("expected reconciled-confirmed, got %v", repo.updates)
	}
}

func TestReconciler_OrderTimesOutAfterTTL(t *testing.T) {
	now := time.UnixMilli(10_000_000)
	v := &fakeVenue{} // no open orders, no trades
	repo := &fakeOrderRepo{
		listing: []repository.ClientOrderRecord{
			{ClientOrderID: "co-stale", Status: entity.ClientOrderStatusPending, OrderID: 0, CreatedAt: now.Add(-10 * time.Minute).UnixMilli()},
			{ClientOrderID: "co-stale-with-id", Status: entity.ClientOrderStatusSubmitted, OrderID: 999, CreatedAt: now.Add(-10 * time.Minute).UnixMilli()},
			{ClientOrderID: "co-fresh", Status: entity.ClientOrderStatusPending, OrderID: 0, CreatedAt: now.Add(-1 * time.Minute).UnixMilli()},
		},
	}
	r := newReconciler(t, Config{OrderTTL: 5 * time.Minute}, v, &fakeLocal{}, &fakeHalter{}, repo, nil)
	r.Run(context.Background())
	want := map[string]entity.ClientOrderStatus{
		"co-stale":         entity.ClientOrderStatusReconciledTimeout,
		"co-stale-with-id": entity.ClientOrderStatusReconciledNotFound,
	}
	if len(repo.updates) != len(want) {
		t.Fatalf("expected %d updates, got %v", len(want), repo.updates)
	}
	for _, u := range repo.updates {
		if want[u.id] != u.status {
			t.Fatalf("unexpected update for %s: got %s, want %s", u.id, u.status, want[u.id])
		}
	}
}

func TestReconciler_OrderStillOpenStaysUnchanged(t *testing.T) {
	now := time.UnixMilli(10_000_000)
	v := &fakeVenue{
		orders: []entity.Order{{ID: 555}}, // venue still has it resting
	}
	repo := &fakeOrderRepo{
		listing: []repository.ClientOrderRecord{
			{ClientOrderID: "co-resting", Status: entity.ClientOrderStatusSubmitted, OrderID: 555, CreatedAt: now.Add(-2 * time.Minute).UnixMilli()},
		},
	}
	r := newReconciler(t, Config{OrderTTL: 5 * time.Minute}, v, &fakeLocal{}, &fakeHalter{}, repo, nil)
	r.Run(context.Background())
	if len(repo.updates) != 0 {
		t.Fatalf("resting orders must not be touched, got %v", repo.updates)
	}
}

func TestReconciler_VenueErrorsDoNotAbortLaterChecks(t *testing.T) {
	v := &fakeVenue{
		getOrdersErr:    fmt.Errorf("HTTP 500"),
		getPositionsErr: nil,
		positions:       []entity.Position{}, // flat
		jpy:             100_000,
	}
	l := &fakeLocal{balance: 100_000}
	h := &fakeHalter{}
	pub := &fakePublisher{}
	repo := &fakeOrderRepo{listing: []repository.ClientOrderRecord{
		{ClientOrderID: "co-x", Status: entity.ClientOrderStatusSubmitted, OrderID: 1, CreatedAt: time.Now().UnixMilli()},
	}}
	r := newReconciler(t, Config{PositionHaltPct: 0.5, BalanceHaltPct: 0.5}, v, l, h, repo, pub)
	r.Run(context.Background())
	// Orders pass failed (no updates), but positions/balance still ran cleanly.
	if len(h.reasons) > 0 {
		t.Fatalf("balanced state must not halt, got %v", h.reasons)
	}
}

func TestReconciler_PanicsOnNilHalter(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil halter")
		}
	}()
	_ = New(Config{}, &fakeVenue{}, &fakeLocal{}, nil, nil, nil, 7)
}
