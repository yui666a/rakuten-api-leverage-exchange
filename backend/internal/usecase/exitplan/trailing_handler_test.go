package exitplan

import (
	"context"
	"sync"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	domainexitplan "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/exitplan"
)

func TestTrailingHandler_long_activation(t *testing.T) {
	repo := newTrailingMemRepo()
	plan, _ := domainexitplan.New(domainexitplan.NewInput{
		PositionID: 100, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000,
		Policy:    defaultPolicy(),
		CreatedAt: 1,
	})
	plan.ID = 555
	repo.byPosition[100] = plan
	repo.openList = []*domainexitplan.ExitPlan{plan}

	h := NewTrailingPersistenceHandler(TrailingPersistenceConfig{Repo: repo})

	if _, err := h.Handle(context.Background(), entity.TickEvent{
		SymbolID: 7, Price: 9990, Timestamp: 100,
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if repo.updateCalled {
		t.Errorf("loss-side tick should not persist")
	}
	if plan.TrailingActivated {
		t.Errorf("HWM should not activate")
	}

	if _, err := h.Handle(context.Background(), entity.TickEvent{
		SymbolID: 7, Price: 10050, Timestamp: 200,
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !repo.updateCalled {
		t.Errorf("activation should persist")
	}
	if !plan.TrailingActivated || plan.TrailingHWM == nil || *plan.TrailingHWM != 10050 {
		t.Errorf("plan state wrong: %+v", plan)
	}
	if repo.updateHWM != 10050 || !repo.updateActivated {
		t.Errorf("update args wrong: hwm=%v activated=%v", repo.updateHWM, repo.updateActivated)
	}
}

func TestTrailingHandler_long_higherHigh(t *testing.T) {
	repo := newTrailingMemRepo()
	plan, _ := domainexitplan.New(domainexitplan.NewInput{
		PositionID: 100, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000,
		Policy:    defaultPolicy(),
		CreatedAt: 1,
	})
	plan.ID = 555
	plan.RaiseTrailingHWM(10050, 100)
	repo.byPosition[100] = plan
	repo.openList = []*domainexitplan.ExitPlan{plan}

	h := NewTrailingPersistenceHandler(TrailingPersistenceConfig{Repo: repo})

	h.Handle(context.Background(), entity.TickEvent{SymbolID: 7, Price: 10030, Timestamp: 200})
	if repo.updateCalled {
		t.Errorf("lower tick should not persist")
	}
	h.Handle(context.Background(), entity.TickEvent{SymbolID: 7, Price: 10100, Timestamp: 300})
	if !repo.updateCalled {
		t.Errorf("new high should persist")
	}
	if repo.updateHWM != 10100 {
		t.Errorf("HWM = %v, want 10100", repo.updateHWM)
	}
}

func TestTrailingHandler_otherSymbol_skipped(t *testing.T) {
	repo := newTrailingMemRepo()
	plan, _ := domainexitplan.New(domainexitplan.NewInput{
		PositionID: 100, SymbolID: 7, Side: entity.OrderSideBuy, EntryPrice: 10000,
		Policy:    defaultPolicy(),
		CreatedAt: 1,
	})
	plan.ID = 555
	repo.byPosition[100] = plan
	repo.openList = []*domainexitplan.ExitPlan{plan}

	h := NewTrailingPersistenceHandler(TrailingPersistenceConfig{Repo: repo})
	h.Handle(context.Background(), entity.TickEvent{SymbolID: 8, Price: 10100, Timestamp: 100})
	if repo.updateCalled {
		t.Errorf("different symbol tick should not persist")
	}
}

func TestTrailingHandler_repoListErrorSwallowed(t *testing.T) {
	repo := newTrailingMemRepo()
	repo.listErr = errFake{}
	h := NewTrailingPersistenceHandler(TrailingPersistenceConfig{Repo: repo})
	if _, err := h.Handle(context.Background(), entity.TickEvent{
		SymbolID: 7, Price: 10100, Timestamp: 100,
	}); err != nil {
		t.Fatalf("repo error must not propagate, got %v", err)
	}
}

func TestTrailingHandler_NonTickEvent_passThrough(t *testing.T) {
	repo := newTrailingMemRepo()
	h := NewTrailingPersistenceHandler(TrailingPersistenceConfig{Repo: repo})
	if _, err := h.Handle(context.Background(), entity.IndicatorEvent{}); err != nil {
		t.Fatalf("non-tick: %v", err)
	}
	if repo.listCalled {
		t.Errorf("non-tick should not query repo")
	}
}

// --- helpers ---

type errFake struct{}

func (errFake) Error() string { return "fake repo error" }

type trailingMemRepo struct {
	mu              sync.Mutex
	byPosition      map[int64]*domainexitplan.ExitPlan
	openList        []*domainexitplan.ExitPlan
	listCalled      bool
	updateCalled    bool
	updateID        int64
	updateHWM       float64
	updateActivated bool
	listErr         error
}

func newTrailingMemRepo() *trailingMemRepo {
	return &trailingMemRepo{byPosition: map[int64]*domainexitplan.ExitPlan{}}
}

func (m *trailingMemRepo) Create(_ context.Context, _ *domainexitplan.ExitPlan) error {
	return nil
}
func (m *trailingMemRepo) FindByPositionID(_ context.Context, posID int64) (*domainexitplan.ExitPlan, error) {
	return m.byPosition[posID], nil
}
func (m *trailingMemRepo) ListOpen(_ context.Context, symbolID int64) ([]*domainexitplan.ExitPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listCalled = true
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]*domainexitplan.ExitPlan, 0, len(m.openList))
	for _, p := range m.openList {
		if p.SymbolID == symbolID {
			out = append(out, p)
		}
	}
	return out, nil
}
func (m *trailingMemRepo) UpdateTrailing(_ context.Context, planID int64, hwm float64, activated bool, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalled = true
	m.updateID = planID
	m.updateHWM = hwm
	m.updateActivated = activated
	return nil
}
func (m *trailingMemRepo) Close(_ context.Context, _ int64, _ int64) error { return nil }

var _ domainexitplan.Repository = (*trailingMemRepo)(nil)
