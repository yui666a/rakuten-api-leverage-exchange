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
	mu      sync.Mutex
	candles []entity.Candle
	tickers []entity.Ticker
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

func (m *mockMarketDataRepo) GetCandles(_ context.Context, _ int64, _ string, limit int) ([]entity.Candle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit > len(m.candles) {
		limit = len(m.candles)
	}
	// リポジトリ契約通り新しい順で返す
	result := make([]entity.Candle, limit)
	for i := 0; i < limit; i++ {
		result[i] = m.candles[len(m.candles)-1-i]
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
