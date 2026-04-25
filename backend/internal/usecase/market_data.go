package usecase

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// PersistenceConfig controls how high-frequency market data is written to disk.
// Tickers come in at ~1 Hz from the venue WS and orderbook snapshots arrive
// even faster, so we throttle and run all DB writes on a single background
// goroutine to keep the trading hot path lock-free.
type PersistenceConfig struct {
	// Enable toggles persistence of tickers / trades / orderbooks. When false,
	// HandleTicker still distributes events to subscribers and the realtime
	// hub but no DB writes occur.
	Enable bool
	// TickerInterval is the minimum gap between ticker rows per symbol. 0
	// disables throttling.
	TickerInterval time.Duration
	// OrderbookInterval is the minimum gap between orderbook snapshots per
	// symbol. 0 disables throttling.
	OrderbookInterval time.Duration
	// OrderbookDepth caps how many ask/bid levels are serialized per snapshot.
	OrderbookDepth int
	// RetentionDays drops rows older than this many days. 0 disables
	// retention sweeps.
	RetentionDays int
	// QueueSize bounds the writer channel. When full, additional events are
	// dropped (counted in DropCount) rather than blocking the producer.
	QueueSize int
}

// DefaultPersistenceConfig returns the values used when env vars are unset.
// Sized so that 90 days of LTC/JPY tickers (~1/sec) and 5s orderbook snapshots
// land near 2 GB on disk — well within the SQLite-on-named-volume budget.
func DefaultPersistenceConfig() PersistenceConfig {
	return PersistenceConfig{
		Enable:            true,
		TickerInterval:    1 * time.Second,
		OrderbookInterval: 5 * time.Second,
		OrderbookDepth:    20,
		RetentionDays:     90,
		QueueSize:         1024,
	}
}

// persistTask is what the writer goroutine consumes. Exactly one of the
// pointer fields is non-nil per task.
type persistTask struct {
	ticker    *entity.Ticker
	orderbook *entity.Orderbook
	trades    *entity.MarketTradesResponse
}

// MarketDataService manages receiving, distributing, and persisting market data.
type MarketDataService struct {
	repo repository.MarketDataRepository

	mu          sync.RWMutex
	tickerSubs  []chan entity.Ticker
	realtimeHub *RealtimeHub

	cfg PersistenceConfig

	// throttle bookkeeping (per-symbol last-saved unix-millis).
	throttleMu        sync.Mutex
	lastTickerSavedMs map[int64]int64
	lastOrderbookMs   map[int64]int64

	// writer channel + lifecycle
	writerCh   chan persistTask
	writerDone chan struct{}
	dropCount  atomic.Int64
}

func NewMarketDataService(repo repository.MarketDataRepository) *MarketDataService {
	return NewMarketDataServiceWithConfig(repo, DefaultPersistenceConfig())
}

func NewMarketDataServiceWithConfig(repo repository.MarketDataRepository, cfg PersistenceConfig) *MarketDataService {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1024
	}
	return &MarketDataService{
		repo:              repo,
		cfg:               cfg,
		lastTickerSavedMs: make(map[int64]int64),
		lastOrderbookMs:   make(map[int64]int64),
	}
}

// StartPersistenceWorker spins up the single writer goroutine + the retention
// sweeper. Safe to call once at startup; subsequent calls are no-ops. Stops
// when ctx is cancelled.
func (s *MarketDataService) StartPersistenceWorker(ctx context.Context) {
	if !s.cfg.Enable {
		return
	}
	s.mu.Lock()
	if s.writerCh != nil {
		s.mu.Unlock()
		return
	}
	s.writerCh = make(chan persistTask, s.cfg.QueueSize)
	s.writerDone = make(chan struct{})
	s.mu.Unlock()

	go s.runWriter(ctx)
	if s.cfg.RetentionDays > 0 {
		go s.runRetention(ctx)
	}
}

func (s *MarketDataService) runWriter(ctx context.Context) {
	defer close(s.writerDone)
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-s.writerCh:
			if !ok {
				return
			}
			s.executeTask(ctx, task)
		}
	}
}

func (s *MarketDataService) executeTask(ctx context.Context, task persistTask) {
	switch {
	case task.ticker != nil:
		if err := s.repo.SaveTicker(ctx, *task.ticker); err != nil {
			slog.Warn("failed to save ticker", "error", err)
		}
	case task.orderbook != nil:
		if err := s.repo.SaveOrderbook(ctx, *task.orderbook, s.cfg.OrderbookDepth); err != nil {
			slog.Warn("failed to save orderbook", "error", err)
		}
	case task.trades != nil:
		if err := s.repo.SaveTrades(ctx, task.trades.SymbolID, task.trades.Trades); err != nil {
			slog.Warn("failed to save trades", "error", err)
		}
	}
}

func (s *MarketDataService) runRetention(ctx context.Context) {
	// 起動直後に 1 度走らせ、その後は 24h 周期。コンテナ再起動が頻繁な
	// 環境でも古いデータを確実に掃除できる。
	sweep := func() {
		cutoff := time.Now().Add(-time.Duration(s.cfg.RetentionDays) * 24 * time.Hour).UnixMilli()
		deleted, err := s.repo.PurgeOldMarketData(ctx, cutoff)
		if err != nil {
			slog.Warn("market data retention sweep failed", "error", err)
			return
		}
		if deleted > 0 {
			slog.Info("market data retention sweep", "deleted", deleted, "retentionDays", s.cfg.RetentionDays)
		}
	}
	sweep()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}

// enqueue tries to push a task onto the writer channel without blocking.
// Returns true when accepted, false when the queue was full (caller-visible
// via DropCount).
//
// Fallback: if the writer goroutine has not been started (writerCh==nil), we
// run the task inline. This keeps tests and CLI tools that construct the
// service without StartPersistenceWorker working as before, at the cost of a
// short DB hop on the caller's goroutine. Production wires StartPersistenceWorker
// at boot, so the synchronous path is only hit in the test mocks and one-shot
// scripts.
func (s *MarketDataService) enqueue(task persistTask) bool {
	if !s.cfg.Enable {
		return false
	}
	if s.writerCh == nil {
		s.executeTask(context.Background(), task)
		return true
	}
	select {
	case s.writerCh <- task:
		return true
	default:
		s.dropCount.Add(1)
		return false
	}
}

// DropCount returns the cumulative number of persistence tasks dropped because
// the writer queue was full. Exposed for test assertions and future metrics.
func (s *MarketDataService) DropCount() int64 {
	return s.dropCount.Load()
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

// shouldPersistTicker reports whether enough time has elapsed since the last
// persisted ticker for symbolID. Updates the bookkeeping when accepting.
func (s *MarketDataService) shouldPersistTicker(symbolID, ts int64) bool {
	if s.cfg.TickerInterval <= 0 {
		return true
	}
	gap := s.cfg.TickerInterval.Milliseconds()
	s.throttleMu.Lock()
	defer s.throttleMu.Unlock()
	last := s.lastTickerSavedMs[symbolID]
	if ts-last < gap {
		return false
	}
	s.lastTickerSavedMs[symbolID] = ts
	return true
}

func (s *MarketDataService) shouldPersistOrderbook(symbolID, ts int64) bool {
	if s.cfg.OrderbookInterval <= 0 {
		return true
	}
	gap := s.cfg.OrderbookInterval.Milliseconds()
	s.throttleMu.Lock()
	defer s.throttleMu.Unlock()
	last := s.lastOrderbookMs[symbolID]
	if ts-last < gap {
		return false
	}
	s.lastOrderbookMs[symbolID] = ts
	return true
}

// HandleTicker distributes a ticker to subscribers, publishes a realtime event,
// and (when persistence is enabled and the per-symbol throttle allows it)
// enqueues it for the writer goroutine.
func (s *MarketDataService) HandleTicker(ctx context.Context, ticker entity.Ticker) {
	if s.cfg.Enable && s.shouldPersistTicker(ticker.SymbolID, ticker.Timestamp) {
		t := ticker
		s.enqueue(persistTask{ticker: &t})
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

// HandleOrderbook persists an orderbook snapshot (subject to throttle) and
// publishes a realtime event so the frontend panel updates immediately.
func (s *MarketDataService) HandleOrderbook(ctx context.Context, ob entity.Orderbook) {
	if s.cfg.Enable && s.shouldPersistOrderbook(ob.SymbolID, ob.Timestamp) {
		o := ob
		s.enqueue(persistTask{orderbook: &o})
	}

	s.mu.RLock()
	hub := s.realtimeHub
	s.mu.RUnlock()
	if hub != nil {
		if err := hub.PublishData("orderbook", ob.SymbolID, ob); err != nil {
			slog.Warn("failed to publish orderbook event", "error", err)
		}
	}
}

// HandleTrades enqueues a market trade batch for persistence and publishes
// the realtime event. Unlike tickers/orderbooks there is no throttle —
// individual trades are uniquely identified and de-duped at write time.
func (s *MarketDataService) HandleTrades(ctx context.Context, trades entity.MarketTradesResponse) {
	if s.cfg.Enable && len(trades.Trades) > 0 {
		t := trades
		s.enqueue(persistTask{trades: &t})
	}

	s.mu.RLock()
	hub := s.realtimeHub
	s.mu.RUnlock()
	if hub != nil {
		if err := hub.PublishData("market_trades", trades.SymbolID, trades); err != nil {
			slog.Warn("failed to publish market_trades event", "error", err)
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

// GetOrderbookHistory exposes the underlying repo query for replay tooling.
func (s *MarketDataService) GetOrderbookHistory(ctx context.Context, symbolID int64, from, to int64, limit int) ([]entity.Orderbook, error) {
	return s.repo.GetOrderbookHistory(ctx, symbolID, from, to, limit)
}
