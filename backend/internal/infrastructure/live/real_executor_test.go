package live

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/sor"
)

// mockOrderClient implements repository.OrderClient for testing.
type mockOrderClient struct {
	mu             sync.Mutex
	createOrderFn  func(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error)
	getPositionsFn func(ctx context.Context, symbolID int64) ([]entity.Position, error)
	calls          []entity.OrderRequest
}

func (m *mockOrderClient) CreateOrder(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req)
	m.mu.Unlock()
	if m.createOrderFn != nil {
		return m.createOrderFn(ctx, req)
	}
	return []entity.Order{{ID: 1, Price: 100}}, nil
}

func (m *mockOrderClient) CreateOrderRaw(ctx context.Context, req entity.OrderRequest) (repository.CreateOrderOutcome, error) {
	return repository.CreateOrderOutcome{}, fmt.Errorf("not implemented")
}

func (m *mockOrderClient) CancelOrder(ctx context.Context, symbolID, orderID int64) ([]entity.Order, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockOrderClient) GetOrders(ctx context.Context, symbolID int64) ([]entity.Order, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockOrderClient) GetPositions(ctx context.Context, symbolID int64) ([]entity.Position, error) {
	if m.getPositionsFn != nil {
		return m.getPositionsFn(ctx, symbolID)
	}
	return nil, nil
}

func (m *mockOrderClient) GetMyTrades(ctx context.Context, symbolID int64) ([]entity.MyTrade, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockOrderClient) GetAssets(ctx context.Context) ([]entity.Asset, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestRealExecutor_SelectSLTPExit_BuyWorstCase(t *testing.T) {
	exec := NewRealExecutor(&mockOrderClient{}, 7, 0.1)
	price, reason, hit := exec.SelectSLTPExit(
		entity.OrderSideBuy,
		95,  // stop-loss
		105, // take-profit
		94,  // bar low (triggers SL)
		106, // bar high (triggers TP)
	)
	if !hit {
		t.Fatal("expected hit")
	}
	if reason != "stop_loss" {
		t.Fatalf("expected stop_loss, got %s", reason)
	}
	if price != 95 {
		t.Fatalf("expected stop-loss price 95, got %f", price)
	}
}

func TestRealExecutor_SelectSLTPExit_SellWorstCase(t *testing.T) {
	exec := NewRealExecutor(&mockOrderClient{}, 7, 0.1)
	price, reason, hit := exec.SelectSLTPExit(
		entity.OrderSideSell,
		105, // stop-loss
		95,  // take-profit
		94,  // bar low (triggers TP)
		106, // bar high (triggers SL)
	)
	if !hit {
		t.Fatal("expected hit")
	}
	if reason != "stop_loss" {
		t.Fatalf("expected stop_loss, got %s", reason)
	}
	if price != 105 {
		t.Fatalf("expected stop-loss price 105, got %f", price)
	}
}

func TestRealExecutor_SelectSLTPExit_BuyOnlySL(t *testing.T) {
	exec := NewRealExecutor(&mockOrderClient{}, 7, 0.1)
	price, reason, hit := exec.SelectSLTPExit(
		entity.OrderSideBuy,
		95,
		105,
		94,  // SL hit
		104, // TP not hit
	)
	if !hit {
		t.Fatal("expected hit")
	}
	if reason != "stop_loss" {
		t.Fatalf("expected stop_loss, got %s", reason)
	}
	if price != 95 {
		t.Fatalf("expected 95, got %f", price)
	}
}

func TestRealExecutor_SelectSLTPExit_BuyOnlyTP(t *testing.T) {
	exec := NewRealExecutor(&mockOrderClient{}, 7, 0.1)
	price, reason, hit := exec.SelectSLTPExit(
		entity.OrderSideBuy,
		95,
		105,
		96,  // SL not hit
		106, // TP hit
	)
	if !hit {
		t.Fatal("expected hit")
	}
	if reason != "take_profit" {
		t.Fatalf("expected take_profit, got %s", reason)
	}
	if price != 105 {
		t.Fatalf("expected 105, got %f", price)
	}
}

func TestRealExecutor_SelectSLTPExit_NoHit(t *testing.T) {
	exec := NewRealExecutor(&mockOrderClient{}, 7, 0.1)
	_, _, hit := exec.SelectSLTPExit(
		entity.OrderSideBuy,
		95,
		105,
		96,  // SL not hit
		104, // TP not hit
	)
	if hit {
		t.Fatal("expected no hit")
	}
}

func TestRealExecutor_OpenAndPositions(t *testing.T) {
	nextID := int64(100)
	mock := &mockOrderClient{
		createOrderFn: func(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
			id := nextID
			nextID++
			return []entity.Order{{ID: id, Price: 50000}}, nil
		},
	}
	exec := NewRealExecutor(mock, 7, 0.1)

	orderEv, err := exec.Open(7, entity.OrderSideBuy, 50000, 0.01, "test_entry", 1000)
	if err != nil {
		t.Fatalf("open error: %v", err)
	}
	if orderEv.Action != "open" {
		t.Fatalf("expected action=open, got %s", orderEv.Action)
	}
	if orderEv.OrderID != 100 {
		t.Fatalf("expected orderID=100, got %d", orderEv.OrderID)
	}

	positions := exec.Positions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	if positions[0].PositionID != 100 {
		t.Fatalf("expected positionID=100, got %d", positions[0].PositionID)
	}
	if positions[0].Side != entity.OrderSideBuy {
		t.Fatalf("expected BUY, got %s", positions[0].Side)
	}
	if positions[0].Amount != 0.01 {
		t.Fatalf("expected amount=0.01, got %f", positions[0].Amount)
	}
}

func TestRealExecutor_OpenAndClose(t *testing.T) {
	nextID := int64(200)
	callCount := 0
	mock := &mockOrderClient{
		createOrderFn: func(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
			id := nextID
			nextID++
			callCount++
			// First call (open) returns 50000, second call (close) returns 51000.
			price := 50000.0
			if callCount >= 2 {
				price = 51000.0
			}
			return []entity.Order{{ID: id, Price: price}}, nil
		},
	}
	exec := NewRealExecutor(mock, 7, 0.1)

	_, err := exec.Open(7, entity.OrderSideBuy, 50000, 0.01, "entry", 1000)
	if err != nil {
		t.Fatalf("open error: %v", err)
	}

	positions := exec.Positions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}

	orderEv, trade, err := exec.Close(positions[0].PositionID, 51000, "exit", 2000)
	if err != nil {
		t.Fatalf("close error: %v", err)
	}
	if orderEv.Action != "close" {
		t.Fatalf("expected action=close, got %s", orderEv.Action)
	}
	if trade == nil {
		t.Fatal("trade record should not be nil")
	}
	if trade.PnL == 0 {
		t.Fatal("expected non-zero PnL")
	}

	// Position should be removed.
	positions = exec.Positions()
	if len(positions) != 0 {
		t.Fatalf("expected 0 positions after close, got %d", len(positions))
	}
}

func TestRealExecutor_CloseNotFound(t *testing.T) {
	exec := NewRealExecutor(&mockOrderClient{}, 7, 0.1)
	_, _, err := exec.Close(999, 50000, "exit", 1000)
	if err == nil {
		t.Fatal("expected error for missing position")
	}
}

func TestRealExecutor_SyncPositions(t *testing.T) {
	mock := &mockOrderClient{
		getPositionsFn: func(ctx context.Context, symbolID int64) ([]entity.Position, error) {
			return []entity.Position{
				{
					ID:              10,
					SymbolID:        7,
					OrderSide:       entity.OrderSideBuy,
					Price:           50000,
					RemainingAmount: 0.01,
					CreatedAt:       1000,
				},
				{
					ID:              11,
					SymbolID:        7,
					OrderSide:       entity.OrderSideSell,
					Price:           51000,
					RemainingAmount: 0.02,
					CreatedAt:       2000,
				},
			}, nil
		},
	}
	exec := NewRealExecutor(mock, 7, 0.1)

	err := exec.SyncPositions(context.Background())
	if err != nil {
		t.Fatalf("sync error: %v", err)
	}

	positions := exec.Positions()
	if len(positions) != 2 {
		t.Fatalf("expected 2 positions, got %d", len(positions))
	}
	if positions[0].PositionID != 10 {
		t.Fatalf("expected positionID=10, got %d", positions[0].PositionID)
	}
	if positions[1].Side != entity.OrderSideSell {
		t.Fatalf("expected SELL, got %s", positions[1].Side)
	}
}

func TestRealExecutor_OpenReversesOpposite(t *testing.T) {
	nextID := int64(300)
	mock := &mockOrderClient{
		createOrderFn: func(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
			id := nextID
			nextID++
			return []entity.Order{{ID: id, Price: 50000}}, nil
		},
	}
	exec := NewRealExecutor(mock, 7, 0.1)

	// Open a BUY position.
	_, err := exec.Open(7, entity.OrderSideBuy, 50000, 0.01, "buy_entry", 1000)
	if err != nil {
		t.Fatalf("open error: %v", err)
	}
	if len(exec.Positions()) != 1 {
		t.Fatalf("expected 1 position")
	}

	// Open a SELL on the same symbol should close the BUY first.
	_, err = exec.Open(7, entity.OrderSideSell, 51000, 0.01, "sell_entry", 2000)
	if err != nil {
		t.Fatalf("reverse open error: %v", err)
	}

	positions := exec.Positions()
	// Only the SELL position should remain (BUY was closed by reverse signal).
	if len(positions) != 1 {
		t.Fatalf("expected 1 position after reverse, got %d", len(positions))
	}
	if positions[0].Side != entity.OrderSideSell {
		t.Fatalf("expected SELL, got %s", positions[0].Side)
	}
}

// fakeTouch is a minimal TouchSource for SOR-related tests.
type fakeTouch struct {
	bestBid, bestAsk float64
	timestamp        int64
	found            bool
}

func (f *fakeTouch) LatestBefore(_ context.Context, _ int64, _ int64) (entity.Orderbook, bool, error) {
	if !f.found {
		return entity.Orderbook{}, false, nil
	}
	return entity.Orderbook{Timestamp: f.timestamp, BestBid: f.bestBid, BestAsk: f.bestAsk}, true, nil
}

// sorMockClient extends mockOrderClient with cancel + getOrders hooks so we
// can drive the post-only escalation path without cross-coupling existing
// market-only tests.
type sorMockClient struct {
	mockOrderClient
	getOrdersFn   func(ctx context.Context, symbolID int64) ([]entity.Order, error)
	cancelOrderFn func(ctx context.Context, symbolID, orderID int64) ([]entity.Order, error)
}

func (m *sorMockClient) GetOrders(ctx context.Context, symbolID int64) ([]entity.Order, error) {
	if m.getOrdersFn != nil {
		return m.getOrdersFn(ctx, symbolID)
	}
	return nil, nil
}

func (m *sorMockClient) CancelOrder(ctx context.Context, symbolID, orderID int64) ([]entity.Order, error) {
	if m.cancelOrderFn != nil {
		return m.cancelOrderFn(ctx, symbolID, orderID)
	}
	return nil, nil
}

func TestRealExecutor_PostOnlyEscalate_LimitFillsBeforeDeadline(t *testing.T) {
	seen := []entity.OrderRequest{}
	mock := &sorMockClient{}
	mock.createOrderFn = func(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
		seen = append(seen, req)
		// Return a LIMIT order that immediately fills (RemainingAmount=0
		// after one poll).
		return []entity.Order{{ID: 500, Price: 8999.9, RemainingAmount: 0.01}}, nil
	}
	mock.getOrdersFn = func(ctx context.Context, symbolID int64) ([]entity.Order, error) {
		// Order has filled — return empty so RealExecutor treats it as filled.
		return nil, nil
	}

	touch := &fakeTouch{bestBid: 9000, bestAsk: 9011.3, timestamp: 1500, found: true}

	router := sor.New(sor.Config{
		Strategy:         sor.StrategyPostOnlyEscalate,
		LimitOffsetTicks: 1,
		TickSize:         0.1,
		EscalateAfterMs:  1000,
	})
	exec := NewRealExecutor(mock, 7, 0,
		WithSOR(router),
		WithTouchSource(touch),
		WithPollInterval(50*time.Millisecond),
	)

	ev, err := exec.Open(7, entity.OrderSideBuy, 9000, 0.01, "post_only_test", 2000)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if ev.OrderID != 500 {
		t.Fatalf("expected order 500, got %d", ev.OrderID)
	}
	// The first venue call must be a post-only LIMIT.
	if got := len(seen); got < 1 {
		t.Fatalf("expected at least 1 venue call, got %d", got)
	}
	first := seen[0].OrderData
	if first.OrderType != entity.OrderTypeLimit {
		t.Fatalf("expected LIMIT first, got %s", first.OrderType)
	}
	if first.PostOnly == nil || !*first.PostOnly {
		t.Fatalf("expected postOnly=true")
	}
	if first.Price == nil || *first.Price != 8999.9 {
		t.Fatalf("expected price 8999.9, got %v", first.Price)
	}
}

func TestRealExecutor_PostOnlyEscalate_FallsBackOnRejection(t *testing.T) {
	mock := &sorMockClient{}
	calls := 0
	mock.createOrderFn = func(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
		calls++
		if calls == 1 {
			// Venue rejects post-only LIMIT (would have crossed).
			return nil, fmt.Errorf("post-only would have crossed")
		}
		// Fallback MARKET succeeds.
		return []entity.Order{{ID: 700, Price: 9011.3}}, nil
	}

	touch := &fakeTouch{bestBid: 9000, bestAsk: 9011.3, timestamp: 1500, found: true}
	router := sor.New(sor.Config{
		Strategy:         sor.StrategyPostOnlyEscalate,
		LimitOffsetTicks: 1,
		TickSize:         0.1,
		EscalateAfterMs:  500,
	})
	exec := NewRealExecutor(mock, 7, 0,
		WithSOR(router),
		WithTouchSource(touch),
		WithPollInterval(50*time.Millisecond),
	)

	ev, err := exec.Open(7, entity.OrderSideBuy, 9000, 0.01, "rejection_test", 2000)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if ev.OrderID != 700 {
		t.Fatalf("expected fallback order 700, got %d", ev.OrderID)
	}
	if calls != 2 {
		t.Fatalf("expected 2 venue calls (LIMIT rejection + MARKET fallback), got %d", calls)
	}
}

func TestRealExecutor_PostOnlyEscalate_DeadlineEscalatesToMarket(t *testing.T) {
	mock := &sorMockClient{}
	calls := 0
	mock.createOrderFn = func(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
		calls++
		if calls == 1 {
			return []entity.Order{{ID: 800, Price: 8999.9, RemainingAmount: 0.01}}, nil
		}
		// MARKET fallback after deadline.
		return []entity.Order{{ID: 801, Price: 9011.3}}, nil
	}
	mock.getOrdersFn = func(ctx context.Context, symbolID int64) ([]entity.Order, error) {
		// Order remains resting — never fills.
		return []entity.Order{{ID: 800, Price: 8999.9, RemainingAmount: 0.01}}, nil
	}
	cancelCalled := false
	mock.cancelOrderFn = func(ctx context.Context, symbolID, orderID int64) ([]entity.Order, error) {
		cancelCalled = true
		return nil, nil
	}

	touch := &fakeTouch{bestBid: 9000, bestAsk: 9011.3, timestamp: 1500, found: true}
	router := sor.New(sor.Config{
		Strategy:         sor.StrategyPostOnlyEscalate,
		LimitOffsetTicks: 1,
		TickSize:         0.1,
		EscalateAfterMs:  100, // very short so the test is fast
	})
	exec := NewRealExecutor(mock, 7, 0,
		WithSOR(router),
		WithTouchSource(touch),
		WithPollInterval(20*time.Millisecond),
	)

	ev, err := exec.Open(7, entity.OrderSideBuy, 9000, 0.01, "deadline_test", 2000)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if ev.OrderID != 801 {
		t.Fatalf("expected MARKET fallback order 801, got %d", ev.OrderID)
	}
	if !cancelCalled {
		t.Fatal("expected CancelOrder to be called when deadline elapses")
	}
}

func TestPositionsChanged_DetectsCountAndShape(t *testing.T) {
	a := []eventengine.Position{{PositionID: 1, Side: entity.OrderSideBuy, Amount: 0.1, EntryPrice: 100}}
	b := []eventengine.Position{{PositionID: 1, Side: entity.OrderSideBuy, Amount: 0.1, EntryPrice: 100}}
	if positionsChanged(a, b) {
		t.Fatal("identical snapshots should not be 'changed'")
	}
	c := []eventengine.Position{{PositionID: 1, Side: entity.OrderSideBuy, Amount: 0.2, EntryPrice: 100}}
	if !positionsChanged(a, c) {
		t.Fatal("amount change must be detected")
	}
	d := []eventengine.Position{
		{PositionID: 1, Side: entity.OrderSideBuy, Amount: 0.1, EntryPrice: 100},
		{PositionID: 2, Side: entity.OrderSideSell, Amount: 0.1, EntryPrice: 102},
	}
	if !positionsChanged(a, d) {
		t.Fatal("count change must be detected")
	}
	// Order independence (venue may reshuffle).
	e := []eventengine.Position{
		{PositionID: 2, Side: entity.OrderSideSell, Amount: 0.1, EntryPrice: 102},
		{PositionID: 1, Side: entity.OrderSideBuy, Amount: 0.1, EntryPrice: 100},
	}
	if positionsChanged(d, e) {
		t.Fatal("order-independent equality should hold")
	}
}

type capturingPublisher struct {
	events []struct {
		symbolID  int64
		positions []eventengine.Position
	}
}

func (p *capturingPublisher) PublishPositionUpdate(symbolID int64, positions []eventengine.Position) {
	p.events = append(p.events, struct {
		symbolID  int64
		positions []eventengine.Position
	}{symbolID, positions})
}

func TestRealExecutor_SyncPositions_PublishesOnChange(t *testing.T) {
	calls := 0
	mock := &mockOrderClient{
		getPositionsFn: func(ctx context.Context, symbolID int64) ([]entity.Position, error) {
			calls++
			if calls == 1 {
				return []entity.Position{{ID: 10, SymbolID: 7, OrderSide: entity.OrderSideBuy, RemainingAmount: 0.1, Price: 100}}, nil
			}
			// Second call: same shape as first → no publish.
			if calls == 2 {
				return []entity.Position{{ID: 10, SymbolID: 7, OrderSide: entity.OrderSideBuy, RemainingAmount: 0.1, Price: 100}}, nil
			}
			// Third call: amount changed → publish.
			return []entity.Position{{ID: 10, SymbolID: 7, OrderSide: entity.OrderSideBuy, RemainingAmount: 0.2, Price: 100}}, nil
		},
	}
	pub := &capturingPublisher{}
	exec := NewRealExecutor(mock, 7, 0, WithPositionPublisher(pub))

	for i := 0; i < 3; i++ {
		if err := exec.SyncPositions(context.Background()); err != nil {
			t.Fatalf("sync %d: %v", i, err)
		}
	}
	// Publishes: first sync (no prior state → "changed") + third sync (amount).
	if len(pub.events) != 2 {
		t.Fatalf("expected 2 publishes, got %d", len(pub.events))
	}
}

func TestRealExecutor_LastOrderAt_BumpsOnSubmit(t *testing.T) {
	mock := &mockOrderClient{
		createOrderFn: func(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
			return []entity.Order{{ID: 1, Price: 100}}, nil
		},
	}
	exec := NewRealExecutor(mock, 7, 0)
	if exec.LastOrderAt() != 0 {
		t.Fatal("expected zero before any order")
	}
	if _, err := exec.Open(7, entity.OrderSideBuy, 100, 0.1, "test", 1000); err != nil {
		t.Fatalf("open: %v", err)
	}
	if exec.LastOrderAt() == 0 {
		t.Fatal("expected LastOrderAt to advance after Open")
	}
}
