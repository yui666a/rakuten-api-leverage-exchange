package usecase

import (
	"context"
	"log/slog"
	"sync"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// MarketDataService manages receiving, distributing, and persisting market data.
type MarketDataService struct {
	repo repository.MarketDataRepository

	mu         sync.RWMutex
	tickerSubs []chan entity.Ticker
	realtimeHub *RealtimeHub
}

func NewMarketDataService(repo repository.MarketDataRepository) *MarketDataService {
	return &MarketDataService{
		repo: repo,
	}
}

func (s *MarketDataService) SetRealtimeHub(hub *RealtimeHub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.realtimeHub = hub
}

// SubscribeTicker starts a ticker subscription and returns a receive channel.
func (s *MarketDataService) SubscribeTicker() <-chan entity.Ticker {
	ch := make(chan entity.Ticker, 100)
	s.mu.Lock()
	s.tickerSubs = append(s.tickerSubs, ch)
	s.mu.Unlock()
	return ch
}

// UnsubscribeTicker removes a subscription and closes the channel.
func (s *MarketDataService) UnsubscribeTicker(ch <-chan entity.Ticker) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, sub := range s.tickerSubs {
		if sub == ch {
			close(sub)
			s.tickerSubs = append(s.tickerSubs[:i], s.tickerSubs[i+1:]...)
			return
		}
	}
}

// HandleTicker persists a received ticker and distributes it to all subscribers.
func (s *MarketDataService) HandleTicker(ctx context.Context, ticker entity.Ticker) {
	if err := s.repo.SaveTicker(ctx, ticker); err != nil {
		slog.Warn("failed to save ticker", "error", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, ch := range s.tickerSubs {
		select {
		case ch <- ticker:
		default:
			// Drop if channel full (don't block slow consumers)
		}
	}

	if s.realtimeHub != nil {
		if err := s.realtimeHub.PublishData("ticker", ticker.SymbolID, ticker); err != nil {
			slog.Warn("failed to publish ticker event", "error", err)
		}
	}
}

// GetCandles retrieves candlestick data from the repository.
// If before > 0, only candles older than that timestamp are returned.
func (s *MarketDataService) GetCandles(ctx context.Context, symbolID int64, interval string, limit int, before int64) ([]entity.Candle, error) {
	return s.repo.GetCandles(ctx, symbolID, interval, limit, before)
}

// GetLatestTicker returns the most recently persisted ticker for a symbol.
func (s *MarketDataService) GetLatestTicker(ctx context.Context, symbolID int64) (*entity.Ticker, error) {
	return s.repo.GetLatestTicker(ctx, symbolID)
}

// SaveCandles persists candlestick data.
func (s *MarketDataService) SaveCandles(ctx context.Context, symbolID int64, interval string, candles []entity.Candle) error {
	return s.repo.SaveCandles(ctx, symbolID, interval, candles)
}
