package exitplan

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	domainexitplan "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/exitplan"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/risk"
)

func defaultPolicy() risk.RiskPolicy {
	return risk.RiskPolicy{
		StopLoss:   risk.StopLossSpec{Percent: 1.5, ATRMultiplier: 2.0},
		TakeProfit: risk.TakeProfitSpec{Percent: 3.0},
		Trailing:   risk.TrailingSpec{Mode: risk.TrailingModeATR, ATRMultiplier: 2.5},
	}
}

func TestShadowHandler_OpenedPosition_createsExitPlan(t *testing.T) {
	repo := newMemRepo()
	h := NewShadowHandler(ShadowHandlerConfig{
		Repo:   repo,
		Policy: defaultPolicy(),
	})

	ev := entity.OrderEvent{
		SymbolID:         7,
		Side:             "BUY",
		Action:           "OPEN",
		Price:            10000,
		Amount:           0.1,
		Timestamp:        1700000000000,
		OpenedPositionID: 100,
	}
	out, err := h.Handle(context.Background(), ev)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("shadow handler should emit no events; got %d", len(out))
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 ExitPlan created, got %d", len(repo.created))
	}
	got := repo.created[0]
	if got.PositionID != 100 || got.SymbolID != 7 || got.Side != entity.OrderSideBuy || got.EntryPrice != 10000 {
		t.Errorf("ExitPlan wrong: %+v", got)
	}
}

func TestShadowHandler_ClosedPosition_closesExitPlan(t *testing.T) {
	repo := newMemRepo()
	plan, _ := domainexitplan.New(domainexitplan.NewInput{
		PositionID: 100, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000,
		Policy:    defaultPolicy(),
		CreatedAt: 1700000000000,
	})
	plan.ID = 999
	repo.byPosition[100] = plan

	h := NewShadowHandler(ShadowHandlerConfig{
		Repo:   repo,
		Policy: defaultPolicy(),
	})

	ev := entity.OrderEvent{
		SymbolID:         7,
		Side:             "SELL",
		Action:           "CLOSE",
		Price:            10500,
		Amount:           0.1,
		Timestamp:        1700000099999,
		ClosedPositionID: 100,
	}
	if _, err := h.Handle(context.Background(), ev); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !repo.closeCalled {
		t.Errorf("Repo.Close should be called")
	}
	if repo.closedID != 999 || repo.closedAt != 1700000099999 {
		t.Errorf("close args wrong: id=%d at=%d", repo.closedID, repo.closedAt)
	}
}

func TestShadowHandler_OpenAndClose_inSameEvent(t *testing.T) {
	repo := newMemRepo()
	plan, _ := domainexitplan.New(domainexitplan.NewInput{
		PositionID: 50, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 9500,
		Policy:    defaultPolicy(),
		CreatedAt: 1700000000000,
	})
	plan.ID = 555
	repo.byPosition[50] = plan

	h := NewShadowHandler(ShadowHandlerConfig{
		Repo:   repo,
		Policy: defaultPolicy(),
	})
	ev := entity.OrderEvent{
		SymbolID:         7,
		Price:            10000,
		Timestamp:        1700000050000,
		OpenedPositionID: 200,
		ClosedPositionID: 50,
		Side:             "SELL",
	}
	if _, err := h.Handle(context.Background(), ev); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !repo.closeCalled {
		t.Errorf("close branch should fire")
	}
	if len(repo.created) != 1 || repo.created[0].PositionID != 200 {
		t.Errorf("open branch should create new ExitPlan for pos 200; got %+v", repo.created)
	}
}

func TestShadowHandler_OpenedPosition_inferSide_fromEvent(t *testing.T) {
	repo := newMemRepo()
	h := NewShadowHandler(ShadowHandlerConfig{
		Repo:   repo,
		Policy: defaultPolicy(),
	})
	cases := []struct {
		side string
		want entity.OrderSide
	}{
		{"BUY", entity.OrderSideBuy},
		{"SELL", entity.OrderSideSell},
	}
	for i, tc := range cases {
		ev := entity.OrderEvent{
			SymbolID: 7, Side: tc.side, Price: 10000, Timestamp: int64(1700000000000 + i),
			OpenedPositionID: int64(100 + i),
		}
		if _, err := h.Handle(context.Background(), ev); err != nil {
			t.Fatalf("case %s: %v", tc.side, err)
		}
	}
	if len(repo.created) != 2 {
		t.Fatalf("want 2 plans, got %d", len(repo.created))
	}
	if repo.created[0].Side != entity.OrderSideBuy || repo.created[1].Side != entity.OrderSideSell {
		t.Errorf("side inference failed: %+v %+v", repo.created[0].Side, repo.created[1].Side)
	}
}

func TestShadowHandler_NonOrderEvent_passThrough(t *testing.T) {
	repo := newMemRepo()
	h := NewShadowHandler(ShadowHandlerConfig{
		Repo:   repo,
		Policy: defaultPolicy(),
	})
	out, err := h.Handle(context.Background(), entity.TickEvent{})
	if err != nil {
		t.Fatalf("non-order event should not error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("non-order event should not emit; got %d", len(out))
	}
	if len(repo.created) != 0 || repo.closeCalled {
		t.Errorf("non-order event should not touch repo")
	}
}

func TestShadowHandler_RepoErrorIsSwallowed(t *testing.T) {
	repo := newMemRepo()
	repo.createErr = errors.New("disk full")
	h := NewShadowHandler(ShadowHandlerConfig{
		Repo:   repo,
		Policy: defaultPolicy(),
	})
	ev := entity.OrderEvent{
		SymbolID: 7, Side: "BUY", Price: 10000, Timestamp: 1700000000000,
		OpenedPositionID: 100,
	}
	out, err := h.Handle(context.Background(), ev)
	if err != nil {
		t.Fatalf("shadow handler must not propagate repo errors (got %v)", err)
	}
	if len(out) != 0 {
		t.Errorf("non-order events should not be emitted")
	}
}

func TestShadowHandler_OrphanClose_logsButNoError(t *testing.T) {
	repo := newMemRepo()
	h := NewShadowHandler(ShadowHandlerConfig{
		Repo:   repo,
		Policy: defaultPolicy(),
	})
	ev := entity.OrderEvent{
		SymbolID: 7, Side: "SELL", Price: 10000, Timestamp: 1700000099999,
		ClosedPositionID: 999,
	}
	if _, err := h.Handle(context.Background(), ev); err != nil {
		t.Fatalf("orphan close should not error: %v", err)
	}
	if repo.closeCalled {
		t.Errorf("orphan close should not call repo.Close")
	}
}

// --- in-memory repo for tests ---

type memRepo struct {
	mu          sync.Mutex
	byPosition  map[int64]*domainexitplan.ExitPlan
	created     []*domainexitplan.ExitPlan
	closeCalled bool
	closedID    int64
	closedAt    int64
	createErr   error
	closeErr    error
}

func newMemRepo() *memRepo {
	return &memRepo{byPosition: map[int64]*domainexitplan.ExitPlan{}}
}

func (m *memRepo) Create(_ context.Context, plan *domainexitplan.ExitPlan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	plan.ID = int64(len(m.created) + 1)
	m.created = append(m.created, plan)
	m.byPosition[plan.PositionID] = plan
	return nil
}
func (m *memRepo) FindByPositionID(_ context.Context, positionID int64) (*domainexitplan.ExitPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byPosition[positionID], nil
}
func (m *memRepo) ListOpen(_ context.Context, _ int64) ([]*domainexitplan.ExitPlan, error) {
	return nil, nil
}
func (m *memRepo) UpdateTrailing(_ context.Context, _ int64, _ float64, _ bool, _ int64) error {
	return nil
}
func (m *memRepo) Close(_ context.Context, planID int64, closedAt int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closeErr != nil {
		return m.closeErr
	}
	m.closeCalled = true
	m.closedID = planID
	m.closedAt = closedAt
	return nil
}

var _ domainexitplan.Repository = (*memRepo)(nil)
