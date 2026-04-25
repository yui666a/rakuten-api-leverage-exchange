package usecase

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// mockMarketDataRepo is a test mock for MarketDataRepository
type mockMarketDataRepo struct {
	mu         sync.Mutex
	candles    []entity.Candle
	tickers    []entity.Ticker
	trades     []entity.MarketTrade
	orderbooks []entity.Orderbook
}

func newMockRepo() *mockMarketDataRepo {
	return &mockMarketDataRepo{}
}

func (m *mockMarketDataRepo) SaveCandle(_ context.Context, _ int64, _ string, c entity.Candle) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.candles = append(m.candles, c)
	return nil
}

func (m *mockMarketDataRepo) SaveCandles(_ context.Context, _ int64, _ string, cs []entity.Candle) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.candles = append(m.candles, cs...)
	return nil
}

func (m *mockMarketDataRepo) GetCandles(_ context.Context, _ int64, _ string, limit int, before int64) ([]entity.Candle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	src := m.candles
	if before > 0 {
		var filtered []entity.Candle
		for _, c := range src {
			if c.Time < before {
				filtered = append(filtered, c)
			}
		}
		src = filtered
	}

	if limit > len(src) {
		limit = len(src)
	}
	// リポジトリ契約通り新しい順で返す
	result := make([]entity.Candle, limit)
	for i := 0; i < limit; i++ {
		result[i] = src[len(src)-1-i]
	}
	return result, nil
}

func (m *mockMarketDataRepo) SaveTicker(_ context.Context, t entity.Ticker) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickers = append(m.tickers, t)
	return nil
}

func (m *mockMarketDataRepo) GetLatestTicker(_ context.Context, symbolID int64) (*entity.Ticker, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.tickers) - 1; i >= 0; i-- {
		if m.tickers[i].SymbolID == symbolID {
			return &m.tickers[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockMarketDataRepo) SaveTrades(_ context.Context, _ int64, ts []entity.MarketTrade) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trades = append(m.trades, ts...)
	return nil
}

func (m *mockMarketDataRepo) SaveOrderbook(_ context.Context, ob entity.Orderbook, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.orderbooks = append(m.orderbooks, ob)
	return nil
}

func (m *mockMarketDataRepo) GetOrderbookHistory(_ context.Context, symbolID int64, from, to int64, limit int) ([]entity.Orderbook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.Orderbook
	for _, ob := range m.orderbooks {
		if ob.SymbolID != symbolID {
			continue
		}
		if from > 0 && ob.Timestamp < from {
			continue
		}
		if to > 0 && ob.Timestamp > to {
			continue
		}
		out = append(out, ob)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *mockMarketDataRepo) PurgeOldMarketData(_ context.Context, cutoffMillis int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var deleted int64

	keptTickers := m.tickers[:0]
	for _, t := range m.tickers {
		if t.Timestamp < cutoffMillis {
			deleted++
			continue
		}
		keptTickers = append(keptTickers, t)
	}
	m.tickers = keptTickers

	keptTrades := m.trades[:0]
	for _, t := range m.trades {
		if t.TradedAt < cutoffMillis {
			deleted++
			continue
		}
		keptTrades = append(keptTrades, t)
	}
	m.trades = keptTrades

	keptObs := m.orderbooks[:0]
	for _, ob := range m.orderbooks {
		if ob.Timestamp < cutoffMillis {
			deleted++
			continue
		}
		keptObs = append(keptObs, ob)
	}
	m.orderbooks = keptObs

	return deleted, nil
}

func TestMarketDataService_SubscribeTicker(t *testing.T) {
	repo := newMockRepo()
	svc := NewMarketDataService(repo)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tickerCh := svc.SubscribeTicker()

	// Inject ticker data
	svc.HandleTicker(ctx, entity.Ticker{
		SymbolID: 7, Last: 5000000, Timestamp: 1000,
	})

	select {
	case tick := <-tickerCh:
		if tick.Last != 5000000 {
			t.Fatalf("expected last 5000000, got %f", tick.Last)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for ticker")
	}

	// Also saved to repo
	if len(repo.tickers) != 1 {
		t.Fatalf("expected 1 ticker saved, got %d", len(repo.tickers))
	}
}

func TestMarketDataService_MultipleSubscribers(t *testing.T) {
	repo := newMockRepo()
	svc := NewMarketDataService(repo)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch1 := svc.SubscribeTicker()
	ch2 := svc.SubscribeTicker()

	svc.HandleTicker(ctx, entity.Ticker{SymbolID: 7, Last: 100, Timestamp: 1000})

	// Both subscribers receive data
	for _, ch := range []<-chan entity.Ticker{ch1, ch2} {
		select {
		case tick := <-ch:
			if tick.Last != 100 {
				t.Fatalf("expected last 100, got %f", tick.Last)
			}
		case <-ctx.Done():
			t.Fatal("timeout")
		}
	}
}

func TestMarketDataService_UnsubscribeTicker(t *testing.T) {
	repo := newMockRepo()
	svc := NewMarketDataService(repo)

	ch := svc.SubscribeTicker()
	svc.UnsubscribeTicker(ch)

	ctx := context.Background()
	svc.HandleTicker(ctx, entity.Ticker{SymbolID: 7, Last: 100, Timestamp: 1000})

	// After unsubscribe, no data received
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("should not receive after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		// Expected: timeout = no data
	}
}

func TestMarketDataService_GetLatestTicker(t *testing.T) {
	repo := newMockRepo()
	svc := NewMarketDataService(repo)

	ctx := context.Background()
	svc.HandleTicker(ctx, entity.Ticker{SymbolID: 7, Last: 100, Timestamp: 1000})
	svc.HandleTicker(ctx, entity.Ticker{SymbolID: 7, Last: 200, Timestamp: 2000})

	ticker, err := svc.GetLatestTicker(ctx, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ticker.Last != 200 {
		t.Fatalf("expected latest last 200, got %f", ticker.Last)
	}
}

func TestMarketDataService_PublishesRealtimeTicker(t *testing.T) {
	repo := newMockRepo()
	svc := NewMarketDataService(repo)
	hub := NewRealtimeHub()
	svc.SetRealtimeHub(hub)

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	svc.HandleTicker(context.Background(), entity.Ticker{SymbolID: 7, Last: 123, Timestamp: 1000})

	select {
	case event := <-ch:
		if event.Type != "ticker" {
			t.Fatalf("expected ticker event, got %s", event.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for realtime event")
	}
}

func TestMarketDataService_TickerThrottle(t *testing.T) {
	repo := newMockRepo()
	cfg := DefaultPersistenceConfig()
	cfg.QueueSize = 16
	svc := NewMarketDataServiceWithConfig(repo, cfg)

	ctx := context.Background()
	// 1 秒スロットリング下では 100ms 間隔で 5 件流しても 1 件しか保存されない。
	base := int64(1_700_000_000_000)
	for i := 0; i < 5; i++ {
		svc.HandleTicker(ctx, entity.Ticker{SymbolID: 7, Last: float64(100 + i), Timestamp: base + int64(i)*100})
	}

	if got := len(repo.tickers); got != 1 {
		t.Fatalf("expected 1 ticker after throttle, got %d", got)
	}

	// 1 秒経過した最初の ts は新規保存される。
	svc.HandleTicker(ctx, entity.Ticker{SymbolID: 7, Last: 999, Timestamp: base + 1000})
	if got := len(repo.tickers); got != 2 {
		t.Fatalf("expected 2 tickers after 1s gap, got %d", got)
	}
}

func TestMarketDataService_OrderbookThrottle(t *testing.T) {
	repo := newMockRepo()
	cfg := DefaultPersistenceConfig()
	svc := NewMarketDataServiceWithConfig(repo, cfg)

	ctx := context.Background()
	base := int64(1_700_000_000_000)
	// 5 秒スロットリングで 1 秒刻みの 5 件 (base+0..base+4000) は最初だけ通る。
	for i := 0; i < 5; i++ {
		svc.HandleOrderbook(ctx, entity.Orderbook{SymbolID: 7, Timestamp: base + int64(i)*1000})
	}
	if got := len(repo.orderbooks); got != 1 {
		t.Fatalf("expected 1 orderbook after throttle, got %d", got)
	}

	// ts - last == 5000 で境界を満たすので通過し 2 件目になる。
	svc.HandleOrderbook(ctx, entity.Orderbook{SymbolID: 7, Timestamp: base + 5000})
	if got := len(repo.orderbooks); got != 2 {
		t.Fatalf("expected 2 orderbooks after 5s gap, got %d", got)
	}
}

func TestMarketDataService_TradesAlwaysPersist(t *testing.T) {
	repo := newMockRepo()
	cfg := DefaultPersistenceConfig()
	svc := NewMarketDataServiceWithConfig(repo, cfg)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		svc.HandleTrades(ctx, entity.MarketTradesResponse{
			SymbolID:  7,
			Timestamp: int64(1000 + i),
			Trades: []entity.MarketTrade{
				{ID: int64(i + 1), OrderSide: "BUY", Price: 100, Amount: 1, TradedAt: int64(1000 + i)},
			},
		})
	}
	if got := len(repo.trades); got != 3 {
		t.Fatalf("expected 3 trades persisted, got %d", got)
	}
}

func TestMarketDataService_DisabledPersistence(t *testing.T) {
	repo := newMockRepo()
	cfg := PersistenceConfig{Enable: false, QueueSize: 16}
	svc := NewMarketDataServiceWithConfig(repo, cfg)

	svc.HandleTicker(context.Background(), entity.Ticker{SymbolID: 7, Last: 100, Timestamp: 1000})
	svc.HandleOrderbook(context.Background(), entity.Orderbook{SymbolID: 7, Timestamp: 1000})

	if len(repo.tickers) != 0 || len(repo.orderbooks) != 0 {
		t.Fatalf("disabled persistence should not write: tickers=%d obs=%d",
			len(repo.tickers), len(repo.orderbooks))
	}
}
