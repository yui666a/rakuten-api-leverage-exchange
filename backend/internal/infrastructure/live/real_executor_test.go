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
	// real_executor.submit is now CreateOrderRaw-based; mirror createOrderFn
	// so existing tests that exercise submit() continue to work.
	orders, err := m.CreateOrder(ctx, req)
	if err != nil {
		return repository.CreateOrderOutcome{
			HTTPStatus: 500,
			HTTPError:  err,
		}, nil
	}
	return repository.CreateOrderOutcome{
		HTTPStatus: 200,
		Orders:     orders,
	}, nil
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
	// Per the confirmed-only contract, the executor consults
	// GetPositions immediately after each open. Mock the venue so the
	// open transitions from pending → confirmed in a single test step;
	// without this the position would remain pending and Positions()
	// would correctly return empty.
	nextID := int64(100)
	mock := &mockOrderClient{
		createOrderFn: func(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
			id := nextID
			nextID++
			return []entity.Order{{ID: id, Price: 50000}}, nil
		},
		getPositionsFn: func(ctx context.Context, symbolID int64) ([]entity.Position, error) {
			return []entity.Position{{
				ID:              100,
				SymbolID:        7,
				OrderSide:       entity.OrderSideBuy,
				Price:           50000,
				RemainingAmount: 0.01,
				CreatedAt:       1000,
			}}, nil
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
	if positions[0].EntryPrice <= 0 {
		t.Fatalf("confirmed-only contract violated: EntryPrice = %v, want > 0", positions[0].EntryPrice)
	}
	if positions[0].Side != entity.OrderSideBuy {
		t.Fatalf("expected BUY, got %s", positions[0].Side)
	}
	if positions[0].Amount != 0.01 {
		t.Fatalf("expected amount=0.01, got %f", positions[0].Amount)
	}
	if orderEv.Trigger != entity.DecisionTriggerBarClose {
		t.Errorf("Trigger = %q, want BAR_CLOSE", orderEv.Trigger)
	}
	if orderEv.OpenedPositionID != 100 {
		t.Errorf("OpenedPositionID = %d, want 100", orderEv.OpenedPositionID)
	}
	if orderEv.ClosedPositionID != 0 {
		t.Errorf("ClosedPositionID = %d, want 0 on Open", orderEv.ClosedPositionID)
	}
}

func TestRealExecutor_OpenAndClose(t *testing.T) {
	nextID := int64(200)
	callCount := 0
	// venueHasPosition flips false after Close so the post-close sync
	// drops the confirmed position. This mirrors the real venue lifecycle
	// (Open visible → Close → venue position disappears) without us
	// having to track ID-to-index plumbing in the mock.
	venueHasPosition := true
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
		getPositionsFn: func(ctx context.Context, symbolID int64) ([]entity.Position, error) {
			if !venueHasPosition {
				return nil, nil
			}
			return []entity.Position{{
				ID:              200,
				SymbolID:        7,
				OrderSide:       entity.OrderSideBuy,
				Price:           50000,
				RemainingAmount: 0.01,
				CreatedAt:       1000,
			}}, nil
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
	venueHasPosition = false // mimic the venue removing the position on close

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
	if orderEv.ClosedPositionID == 0 {
		t.Errorf("ClosedPositionID must be set on Close, got 0")
	}
	if orderEv.OpenedPositionID != 0 {
		t.Errorf("OpenedPositionID must remain 0 on Close, got %d", orderEv.OpenedPositionID)
	}
	// Trigger is intentionally left empty by the executor; the caller
	// (TickRiskHandler etc.) sets it before forwarding the event.
	if orderEv.Trigger != "" {
		t.Errorf("Trigger must be empty (caller fills it), got %q", orderEv.Trigger)
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
	// Venue state machine: starts with one BUY @ 50000, then after the
	// reverse-and-open path runs the BUY is gone and a SELL @ 51000
	// remains. The mock toggles based on createOrderFn call count so
	// confirmPendingViaSync sees the right snapshot at each step.
	nextID := int64(300)
	state := "initial" // "initial" → "after_open" (sell visible)
	mock := &mockOrderClient{
		createOrderFn: func(ctx context.Context, req entity.OrderRequest) ([]entity.Order, error) {
			id := nextID
			nextID++
			return []entity.Order{{ID: id, Price: 50000}}, nil
		},
		getPositionsFn: func(ctx context.Context, symbolID int64) ([]entity.Position, error) {
			switch state {
			case "initial":
				return []entity.Position{{
					ID:              300,
					SymbolID:        7,
					OrderSide:       entity.OrderSideBuy,
					Price:           50000,
					RemainingAmount: 0.01,
					CreatedAt:       1000,
				}}, nil
			case "after_open":
				return []entity.Position{{
					ID:              302,
					SymbolID:        7,
					OrderSide:       entity.OrderSideSell,
					Price:           51000,
					RemainingAmount: 0.01,
					CreatedAt:       2000,
				}}, nil
			}
			return nil, nil
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

	state = "after_open"
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

func TestRealExecutor_Iceberg_SubmitsAllSlices(t *testing.T) {
	calls := 0
	mock := &mockOrderClient{
		createOrderFn: func(_ context.Context, req entity.OrderRequest) ([]entity.Order, error) {
			calls++
			return []entity.Order{{ID: int64(900 + calls), Price: 100}}, nil
		},
	}
	router := sor.New(sor.Config{Strategy: sor.StrategyIceberg, SliceCount: 3, MinIntervalMs: 1})
	exec := NewRealExecutor(mock, 7, 0, WithSOR(router))

	if _, err := exec.Open(7, entity.OrderSideBuy, 100, 0.6, "iceberg", 1000); err != nil {
		t.Fatalf("open: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 venue submissions, got %d", calls)
	}
	// All three submits must be MARKET orders with positive amount.
	for i, c := range mock.calls {
		if c.OrderData.OrderType != entity.OrderTypeMarket {
			t.Fatalf("call %d not MARKET: %s", i, c.OrderData.OrderType)
		}
		if c.OrderData.Amount <= 0 {
			t.Fatalf("call %d amount must be positive: %f", i, c.OrderData.Amount)
		}
	}
}

func TestRealExecutor_OpenWithUrgency_UrgentForcesMarketEvenWhenDefaultIsPostOnly(t *testing.T) {
	calls := 0
	mock := &mockOrderClient{
		createOrderFn: func(_ context.Context, req entity.OrderRequest) ([]entity.Order, error) {
			calls++
			return []entity.Order{{ID: int64(1000 + calls), Price: 100}}, nil
		},
	}
	// Default router would be post-only-escalate; urgency=urgent must override.
	router := sor.New(sor.Config{Strategy: sor.StrategyPostOnlyEscalate, TickSize: 0.1})
	exec := NewRealExecutor(mock, 7, 0, WithSOR(router))

	if _, err := exec.OpenWithUrgency(7, entity.OrderSideBuy, 100, 0.01, "urgent_test", 1000, entity.SignalUrgencyUrgent); err != nil {
		t.Fatalf("open: %v", err)
	}
	if calls != 1 {
		t.Fatalf("urgent must produce a single MARKET submission, got %d calls", calls)
	}
	if mock.calls[0].OrderData.OrderType != entity.OrderTypeMarket {
		t.Fatalf("expected MARKET, got %s", mock.calls[0].OrderData.OrderType)
	}
	if mock.calls[0].OrderData.PostOnly != nil {
		t.Fatalf("urgent must not carry postOnly: %+v", mock.calls[0].OrderData.PostOnly)
	}
}

func TestRealExecutor_OpenWithUrgency_NormalRespectsDefaultRouter(t *testing.T) {
	calls := 0
	mock := &mockOrderClient{
		createOrderFn: func(_ context.Context, req entity.OrderRequest) ([]entity.Order, error) {
			calls++
			return []entity.Order{{ID: int64(2000 + calls), Price: 100}}, nil
		},
	}
	// Default = market. Normal urgency must just round-trip to Open which
	// produces a single MARKET.
	router := sor.New(sor.Config{Strategy: sor.StrategyMarket})
	exec := NewRealExecutor(mock, 7, 0, WithSOR(router))

	if _, err := exec.OpenWithUrgency(7, entity.OrderSideBuy, 100, 0.01, "normal_test", 1000, entity.SignalUrgencyNormal); err != nil {
		t.Fatalf("open: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 venue call, got %d", calls)
	}
}

// === Position confirmed-only contract tests ===
//
// docs/design/2026-05-12-position-confirmed-only.md pins the executor to
// only surface Positions sourced from the venue's confirmed snapshot.
// These regression tests cover the 2026-05-12 incident path: a MARKET BUY
// whose submit response had Order.Price=0 used to seed positions with a
// stale signalPrice fallback, which then misled TP/SL exits ~18 s later.
// Under the new contract that path produces an OrderEvent but no visible
// Position until the venue confirms the fill.

func TestRealExecutor_OpenMarket_PositionAppearsOnlyAfterVenueConfirms(t *testing.T) {
	// Venue submit returns Price=0 (Rakuten's actual MARKET fast-fill
	// behaviour) but GetPositions exposes the real fill on the next sync.
	// The executor must wait for that sync before letting the position
	// participate in any Risk / Exit decision.
	mock := &mockOrderClient{
		createOrderFn: func(_ context.Context, _ entity.OrderRequest) ([]entity.Order, error) {
			return []entity.Order{{ID: 999, Price: 0}}, nil
		},
		getPositionsFn: func(_ context.Context, _ int64) ([]entity.Position, error) {
			return []entity.Position{{
				ID:              999,
				SymbolID:        10,
				OrderSide:       entity.OrderSideBuy,
				Price:           9219,
				RemainingAmount: 0.3,
				CreatedAt:       0,
			}}, nil
		},
	}
	exec := NewRealExecutor(mock, 10, 0)

	if _, err := exec.Open(10, entity.OrderSideBuy, 9168.4 /* signalPrice */, 0.3, "test", 0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	positions := exec.Positions()
	if len(positions) != 1 {
		t.Fatalf("Positions() len = %d, want 1 (post-Open sync should have confirmed the fill)", len(positions))
	}
	if positions[0].EntryPrice != 9219 {
		t.Fatalf("EntryPrice = %v, want 9219 (venue truth, not signalPrice 9168.4)",
			positions[0].EntryPrice)
	}
	if positions[0].EntryPrice <= 0 {
		t.Fatalf("confirmed-only contract violated: EntryPrice = %v, want > 0", positions[0].EntryPrice)
	}
}

func TestRealExecutor_OpenMarket_NoPhantomPositionWhileVenueSilent(t *testing.T) {
	// If GetPositions does not yet show the fill (slow venue settlement
	// or a same-tick race with the periodic sync), Positions() must
	// remain empty. The OrderEvent still carries the submit response
	// price for observability, but downstream Risk / Exit cannot see
	// a phantom EntryPrice — which is exactly what corrupted the
	// 2026-05-12 LTC fill and triggered the spurious take_profit close.
	mock := &mockOrderClient{
		createOrderFn: func(_ context.Context, _ entity.OrderRequest) ([]entity.Order, error) {
			return []entity.Order{{ID: 999, Price: 0}}, nil
		},
		getPositionsFn: func(_ context.Context, _ int64) ([]entity.Position, error) {
			return nil, nil
		},
	}
	exec := NewRealExecutor(mock, 10, 0)

	if _, err := exec.Open(10, entity.OrderSideBuy, 9168.4, 0.3, "test", 0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if positions := exec.Positions(); len(positions) != 0 {
		t.Fatalf("Positions() len = %d, want 0 (venue has not confirmed the fill yet)", len(positions))
	}
	// The pending entry must survive — that's how SweepStalePending and
	// the next periodic sync find the orphan once the venue catches up.
	if pending := exec.PendingOrdersCount(); pending != 1 {
		t.Fatalf("PendingOrdersCount = %d, want 1 (pending must survive a silent venue)", pending)
	}
}

func TestRealExecutor_SyncPositions_KeepsSnapshotWhenAllRowsZeroPrice(t *testing.T) {
	// Once a position has been confirmed we must never wipe the book
	// just because the venue briefly returns the same row with
	// Price=0 (defence in depth against a venue settlement race).
	// First sync confirms; second sync returns Price=0 only; the
	// previous snapshot must survive.
	visible := true
	confirmedPrice := 9219.0
	mock := &mockOrderClient{
		getPositionsFn: func(_ context.Context, _ int64) ([]entity.Position, error) {
			price := confirmedPrice
			if !visible {
				price = 0
			}
			return []entity.Position{{
				ID:              999,
				SymbolID:        10,
				OrderSide:       entity.OrderSideBuy,
				Price:           price,
				RemainingAmount: 0.3,
			}}, nil
		},
	}
	exec := NewRealExecutor(mock, 10, 0)

	if err := exec.SyncPositions(context.Background()); err != nil {
		t.Fatalf("sync 1: %v", err)
	}
	if positions := exec.Positions(); len(positions) != 1 {
		t.Fatalf("first sync should confirm 1 position, got %d", len(positions))
	}

	// venue regresses to Price=0 — must not drop the confirmed snapshot.
	visible = false
	if err := exec.SyncPositions(context.Background()); err != nil {
		t.Fatalf("sync 2: %v", err)
	}
	positions := exec.Positions()
	if len(positions) != 1 || positions[0].EntryPrice != confirmedPrice {
		t.Fatalf("snapshot lost during venue regression: positions = %+v", positions)
	}
}

// captureSink records each PositionConfirmedEvent the executor emits.
type captureSink struct {
	mu     sync.Mutex
	events []entity.PositionConfirmedEvent
}

func (c *captureSink) PublishPositionConfirmed(ev entity.PositionConfirmedEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *captureSink) snapshot() []entity.PositionConfirmedEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]entity.PositionConfirmedEvent, len(c.events))
	copy(out, c.events)
	return out
}

func TestRealExecutor_SyncPositions_EmitsPositionConfirmedForNewFill(t *testing.T) {
	// ADR #260 PR #2: SyncPositions must emit one PositionConfirmedEvent
	// per newly-observed PositionID so the ExitPlan shadow (and any
	// future Position-driven Risk layer) can act on the real venue
	// fill price. Existing positions must NOT re-emit on subsequent
	// syncs — that would cause duplicate plans.
	state := "fresh"
	mock := &mockOrderClient{
		getPositionsFn: func(_ context.Context, _ int64) ([]entity.Position, error) {
			switch state {
			case "fresh":
				return []entity.Position{
					{ID: 1001, OrderID: 555, SymbolID: 10, OrderSide: entity.OrderSideBuy, Price: 9219, RemainingAmount: 0.1},
					{ID: 1002, OrderID: 555, SymbolID: 10, OrderSide: entity.OrderSideBuy, Price: 9220, RemainingAmount: 0.2},
				}, nil
			case "stable":
				// same two positions — must NOT re-emit.
				return []entity.Position{
					{ID: 1001, OrderID: 555, SymbolID: 10, OrderSide: entity.OrderSideBuy, Price: 9219, RemainingAmount: 0.1},
					{ID: 1002, OrderID: 555, SymbolID: 10, OrderSide: entity.OrderSideBuy, Price: 9220, RemainingAmount: 0.2},
				}, nil
			case "added":
				// a third confirmed position — only that one should emit.
				return []entity.Position{
					{ID: 1001, OrderID: 555, SymbolID: 10, OrderSide: entity.OrderSideBuy, Price: 9219, RemainingAmount: 0.1},
					{ID: 1002, OrderID: 555, SymbolID: 10, OrderSide: entity.OrderSideBuy, Price: 9220, RemainingAmount: 0.2},
					{ID: 1003, OrderID: 777, SymbolID: 10, OrderSide: entity.OrderSideSell, Price: 9300, RemainingAmount: 0.5},
				}, nil
			}
			return nil, nil
		},
	}
	sink := &captureSink{}
	exec := NewRealExecutor(mock, 10, 0, WithPositionConfirmedSink(sink))

	if err := exec.SyncPositions(context.Background()); err != nil {
		t.Fatalf("fresh sync: %v", err)
	}
	if got := sink.snapshot(); len(got) != 2 {
		t.Fatalf("fresh sync: emitted %d events, want 2", len(got))
	}
	for _, ev := range sink.snapshot() {
		if ev.EntryPrice <= 0 || ev.OrderID == 0 || ev.PositionID == 0 {
			t.Errorf("emitted event missing required fields: %+v", ev)
		}
	}

	state = "stable"
	if err := exec.SyncPositions(context.Background()); err != nil {
		t.Fatalf("stable sync: %v", err)
	}
	if got := sink.snapshot(); len(got) != 2 {
		t.Fatalf("stable sync re-emitted; cumulative = %d, want still 2", len(got))
	}

	state = "added"
	if err := exec.SyncPositions(context.Background()); err != nil {
		t.Fatalf("added sync: %v", err)
	}
	got := sink.snapshot()
	if len(got) != 3 {
		t.Fatalf("added sync: cumulative emissions = %d, want 3 (only the new PositionID)", len(got))
	}
	last := got[len(got)-1]
	if last.PositionID != 1003 || last.OrderID != 777 || last.Side != entity.OrderSideSell {
		t.Errorf("added sync: third event identifies wrong position: %+v", last)
	}
}

func TestRealExecutor_SyncPositions_DoesNotEmitForSkippedZeroPriceRows(t *testing.T) {
	// Price<=0 rows are filtered out for the snapshot; they must also
	// not generate PositionConfirmedEvent. Otherwise the ExitPlan
	// shadow would receive an invalid event and either reject it (best
	// case, log spam) or persist a degenerate plan.
	mock := &mockOrderClient{
		getPositionsFn: func(_ context.Context, _ int64) ([]entity.Position, error) {
			return []entity.Position{
				{ID: 1001, OrderID: 555, SymbolID: 10, OrderSide: entity.OrderSideBuy, Price: 0, RemainingAmount: 0.1},
			}, nil
		},
	}
	sink := &captureSink{}
	exec := NewRealExecutor(mock, 10, 0, WithPositionConfirmedSink(sink))

	if err := exec.SyncPositions(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := sink.snapshot(); len(got) != 0 {
		t.Fatalf("zero-price row should not emit confirmation; got %d events: %+v", len(got), got)
	}
}

func TestRealExecutor_ConfirmPendingViaSync_RespectsCancelledContext(t *testing.T) {
	// A cancelled context (shutdown path) must short-circuit before
	// hitting the venue, otherwise the sync would either block or
	// surface a stale error.
	venueCalls := 0
	mock := &mockOrderClient{
		createOrderFn: func(_ context.Context, _ entity.OrderRequest) ([]entity.Order, error) {
			return []entity.Order{{ID: 4242, Price: 0}}, nil
		},
		getPositionsFn: func(_ context.Context, _ int64) ([]entity.Position, error) {
			venueCalls++
			return nil, nil
		},
	}
	exec := NewRealExecutor(mock, 10, 0)
	exec.recordPending(4242, 10, entity.OrderSideBuy, 0.1, "test", 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := exec.confirmPendingViaSync(ctx, 4242); err == nil {
		t.Fatalf("confirmPendingViaSync(cancelled) returned nil error")
	}
	if venueCalls != 0 {
		t.Fatalf("venue was polled %d times despite cancelled context", venueCalls)
	}
}

func TestRealExecutor_SyncPositions_DropsVenuePositionsWithZeroPrice(t *testing.T) {
	// Defence in depth: even if the venue returns a position row with
	// Price=0 (unfinished settlement) we must not surface it. The next
	// sync will pick it up once Price > 0.
	visible := false
	mock := &mockOrderClient{
		getPositionsFn: func(_ context.Context, _ int64) ([]entity.Position, error) {
			price := 0.0
			if visible {
				price = 9219
			}
			return []entity.Position{{
				ID:              999,
				SymbolID:        10,
				OrderSide:       entity.OrderSideBuy,
				Price:           price,
				RemainingAmount: 0.3,
			}}, nil
		},
	}
	exec := NewRealExecutor(mock, 10, 0)

	if err := exec.SyncPositions(context.Background()); err != nil {
		t.Fatalf("sync 1: %v", err)
	}
	if positions := exec.Positions(); len(positions) != 0 {
		t.Fatalf("first sync: Positions() len = %d, want 0 (venue price was 0)", len(positions))
	}

	visible = true
	if err := exec.SyncPositions(context.Background()); err != nil {
		t.Fatalf("sync 2: %v", err)
	}
	positions := exec.Positions()
	if len(positions) != 1 || positions[0].EntryPrice != 9219 {
		t.Fatalf("second sync: positions = %+v, want one with EntryPrice=9219", positions)
	}
}

func TestRealExecutor_SyncPositions_ClearsPendingByOrderIDNotPositionID(t *testing.T) {
	// Rakuten distinguishes Position.ID (the position record) from
	// Position.OrderID (the parent venue order). pendingOrders is keyed
	// by the venue OrderID we got back from CreateOrder; the sync must
	// match against `ap.OrderID`, not `ap.ID`. A regression that
	// matched on `ap.ID` would silently leak pending entries every
	// time the venue's PositionID happened to differ from its OrderID
	// (which is the normal case for Rakuten Wallet).
	venueOrderID := int64(7777)
	venuePositionID := int64(880811) // intentionally != OrderID
	mock := &mockOrderClient{
		createOrderFn: func(_ context.Context, _ entity.OrderRequest) ([]entity.Order, error) {
			return []entity.Order{{ID: venueOrderID, Price: 0}}, nil
		},
		getPositionsFn: func(_ context.Context, _ int64) ([]entity.Position, error) {
			return []entity.Position{{
				ID:              venuePositionID,
				OrderID:         venueOrderID,
				SymbolID:        10,
				OrderSide:       entity.OrderSideBuy,
				Price:           9219,
				RemainingAmount: 0.1,
			}}, nil
		},
	}
	exec := NewRealExecutor(mock, 10, 0)

	if _, err := exec.Open(10, entity.OrderSideBuy, 9168, 0.1, "test", 0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if pending := exec.PendingOrdersCount(); pending != 0 {
		t.Fatalf("PendingOrdersCount = %d, want 0 (venue confirmed the OrderID even though PositionID differs)", pending)
	}
}

func TestRealExecutor_SyncPositions_ClearsPendingForSplitFills(t *testing.T) {
	// One submitted OrderID can yield multiple confirmed Positions when
	// the venue fills the order in slices. Every child Position carries
	// the same parent OrderID, so a single pending entry must clear and
	// all positions must be visible to Risk / Exit handlers.
	parentOrderID := int64(4242)
	mock := &mockOrderClient{
		createOrderFn: func(_ context.Context, _ entity.OrderRequest) ([]entity.Order, error) {
			return []entity.Order{{ID: parentOrderID, Price: 0}}, nil
		},
		getPositionsFn: func(_ context.Context, _ int64) ([]entity.Position, error) {
			return []entity.Position{
				{ID: 100001, OrderID: parentOrderID, SymbolID: 10, OrderSide: entity.OrderSideBuy, Price: 9212, RemainingAmount: 0.4},
				{ID: 100002, OrderID: parentOrderID, SymbolID: 10, OrderSide: entity.OrderSideBuy, Price: 9218, RemainingAmount: 0.2},
				{ID: 100003, OrderID: parentOrderID, SymbolID: 10, OrderSide: entity.OrderSideBuy, Price: 9219, RemainingAmount: 0.3},
			}, nil
		},
	}
	exec := NewRealExecutor(mock, 10, 0)

	if _, err := exec.Open(10, entity.OrderSideBuy, 9168, 0.9, "test", 0); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if pending := exec.PendingOrdersCount(); pending != 0 {
		t.Fatalf("split fill should clear the single pending entry: PendingOrdersCount = %d, want 0", pending)
	}
	positions := exec.Positions()
	if len(positions) != 3 {
		t.Fatalf("split fill should produce 3 positions, got %d", len(positions))
	}
	for _, p := range positions {
		if p.EntryPrice <= 0 {
			t.Fatalf("confirmed-only contract violated for split fill: position %+v", p)
		}
	}
}

func TestRealExecutor_SweepStalePending_LogsAgedEntries(t *testing.T) {
	// Pending entries that survive past the TTL must be visible to the
	// operator. SweepStalePending only logs (no GC): the next venue
	// sync is still responsible for removing the entry when the venue
	// confirms or disowns it.
	mock := &mockOrderClient{
		createOrderFn: func(_ context.Context, _ entity.OrderRequest) ([]entity.Order, error) {
			return []entity.Order{{ID: 7777, Price: 0}}, nil
		},
		getPositionsFn: func(_ context.Context, _ int64) ([]entity.Position, error) {
			return nil, nil // venue is silent → pending survives
		},
	}
	exec := NewRealExecutor(mock, 10, 0)
	exec.pendingTTL = 10 * time.Millisecond

	if _, err := exec.Open(10, entity.OrderSideBuy, 100, 0.1, "test", 0); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Inside TTL: no sweep yet.
	if stale := exec.SweepStalePending(5); len(stale) != 0 {
		t.Fatalf("inside TTL: stale = %v, want empty", stale)
	}
	// Past TTL: one stale entry observed with its identifying details.
	stale := exec.SweepStalePending(1_000)
	if len(stale) != 1 {
		t.Fatalf("past TTL: len(stale) = %d, want 1", len(stale))
	}
	if stale[0].OrderID != 7777 {
		t.Fatalf("stale[0].OrderID = %d, want 7777", stale[0].OrderID)
	}
	if stale[0].AgeMs <= 0 {
		t.Fatalf("stale[0].AgeMs = %d, want > 0", stale[0].AgeMs)
	}
}
