package quality

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type fakeVenue struct {
	trades []entity.MyTrade
	err    error
}

func (f *fakeVenue) GetMyTrades(_ context.Context, _ int64) ([]entity.MyTrade, error) {
	return f.trades, f.err
}

// fakeRepo implements just GetTickersBetween + the rest of MarketDataRepository
// as no-ops. Tests for the reporter only exercise GetTickersBetween.
type fakeRepo struct {
	tickers []entity.Ticker
}

func (r *fakeRepo) SaveCandle(_ context.Context, _ int64, _ string, _ entity.Candle) error {
	return nil
}
func (r *fakeRepo) SaveCandles(_ context.Context, _ int64, _ string, _ []entity.Candle) error {
	return nil
}
func (r *fakeRepo) GetCandles(_ context.Context, _ int64, _ string, _ int, _ int64) ([]entity.Candle, error) {
	return nil, nil
}
func (r *fakeRepo) SaveTicker(_ context.Context, _ entity.Ticker) error  { return nil }
func (r *fakeRepo) GetLatestTicker(_ context.Context, _ int64) (*entity.Ticker, error) { return nil, nil }
func (r *fakeRepo) GetTickersBetween(_ context.Context, _ int64, from, to int64, _ int) ([]entity.Ticker, error) {
	var out []entity.Ticker
	for _, t := range r.tickers {
		if from > 0 && t.Timestamp < from {
			continue
		}
		if to > 0 && t.Timestamp > to {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}
func (r *fakeRepo) SaveTrades(_ context.Context, _ int64, _ []entity.MarketTrade) error { return nil }
func (r *fakeRepo) SaveOrderbook(_ context.Context, _ entity.Orderbook, _ int) error    { return nil }
func (r *fakeRepo) GetOrderbookHistory(_ context.Context, _ int64, _, _ int64, _ int) ([]entity.Orderbook, error) {
	return nil, nil
}
func (r *fakeRepo) PurgeOldMarketData(_ context.Context, _ int64) (int64, error) { return 0, nil }

func mustReporter(t *testing.T, v *fakeVenue, repo *fakeRepo, halts HaltSource) *Reporter {
	t.Helper()
	r := New(v, repo, halts)
	r.SetClock(func() time.Time { return time.UnixMilli(10_000_000) })
	return r
}

func TestReporter_EmptyTrades(t *testing.T) {
	v := &fakeVenue{}
	r := mustReporter(t, v, &fakeRepo{}, nil)
	rep, err := r.Build(context.Background(), 7, 86400)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if rep.Trades.Count != 0 {
		t.Fatalf("expected 0 trades, got %d", rep.Trades.Count)
	}
	if rep.WindowSec != 86400 {
		t.Fatalf("expected windowSec=86400, got %d", rep.WindowSec)
	}
}

func TestReporter_MakerTakerRatioAndFee(t *testing.T) {
	now := int64(10_000_000)
	v := &fakeVenue{
		trades: []entity.MyTrade{
			{ID: 1, OrderBehavior: entity.OrderBehaviorOpen, OrderSide: entity.OrderSideBuy, TradeAction: "MAKER", Price: 100, Fee: 10, CreatedAt: now - 1000},
			{ID: 2, OrderBehavior: entity.OrderBehaviorOpen, OrderSide: entity.OrderSideBuy, TradeAction: "TAKER", Price: 101, Fee: 50, CreatedAt: now - 800},
			{ID: 3, OrderBehavior: entity.OrderBehaviorClose, OrderSide: entity.OrderSideSell, TradeAction: "MAKER", Price: 102, Fee: 8, CreatedAt: now - 500},
		},
	}
	r := mustReporter(t, v, &fakeRepo{}, nil)
	rep, err := r.Build(context.Background(), 7, 86400)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if rep.Trades.Count != 3 {
		t.Fatalf("expected 3, got %d", rep.Trades.Count)
	}
	if rep.Trades.MakerCount != 2 || rep.Trades.TakerCount != 1 {
		t.Fatalf("unexpected mix: %+v", rep.Trades)
	}
	if math.Abs(rep.Trades.MakerRatio-2.0/3.0) > 1e-9 {
		t.Fatalf("makerRatio = %f", rep.Trades.MakerRatio)
	}
	if rep.Trades.TotalFeeJPY != 68 {
		t.Fatalf("totalFee = %f, want 68", rep.Trades.TotalFeeJPY)
	}
	if rep.Trades.AvgSlippageBps != nil {
		t.Fatalf("expected nil slippage when no tickers, got %v", *rep.Trades.AvgSlippageBps)
	}

	openBucket := rep.Trades.ByOrderBehavior["OPEN"]
	if openBucket.Count != 2 || openBucket.MakerCount != 1 {
		t.Fatalf("OPEN bucket: %+v", openBucket)
	}
	closeBucket := rep.Trades.ByOrderBehavior["CLOSE"]
	if closeBucket.Count != 1 || closeBucket.MakerCount != 1 {
		t.Fatalf("CLOSE bucket: %+v", closeBucket)
	}
}

func TestReporter_SignedSlippageBpsBuyAndSell(t *testing.T) {
	now := int64(10_000_000)
	v := &fakeVenue{
		trades: []entity.MyTrade{
			// BUY at 101 vs mid 100 → +100 bps (worse)
			{ID: 1, OrderSide: entity.OrderSideBuy, TradeAction: "TAKER", Price: 101, CreatedAt: now - 1000},
			// SELL at 99 vs mid 100 → +100 bps (worse, after sign flip)
			{ID: 2, OrderSide: entity.OrderSideSell, TradeAction: "TAKER", Price: 99, CreatedAt: now - 500},
		},
	}
	repo := &fakeRepo{
		tickers: []entity.Ticker{
			{SymbolID: 7, BestBid: 99, BestAsk: 101, Timestamp: now - 1100}, // mid=100
			{SymbolID: 7, BestBid: 99, BestAsk: 101, Timestamp: now - 600},  // mid=100
		},
	}
	r := mustReporter(t, v, repo, nil)
	rep, err := r.Build(context.Background(), 7, 86400)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if rep.Trades.AvgSlippageBps == nil {
		t.Fatal("expected slippage to be populated")
	}
	if got := *rep.Trades.AvgSlippageBps; got < 99 || got > 101 {
		t.Fatalf("avg slippage = %f, want ~100", got)
	}
}

func TestReporter_FiltersOutOfWindowTrades(t *testing.T) {
	now := int64(10_000_000)
	v := &fakeVenue{
		trades: []entity.MyTrade{
			{ID: 1, TradeAction: "MAKER", Price: 100, CreatedAt: now - 100},
			{ID: 2, TradeAction: "TAKER", Price: 100, CreatedAt: now - 86400_000 - 1000}, // 24h+ ago
		},
	}
	r := mustReporter(t, v, &fakeRepo{}, nil)
	rep, _ := r.Build(context.Background(), 7, 86400)
	if rep.Trades.Count != 1 {
		t.Fatalf("expected 1 in-window trade, got %d", rep.Trades.Count)
	}
}

func TestReporter_VenueErrorPropagates(t *testing.T) {
	v := &fakeVenue{err: fmt.Errorf("HTTP 500")}
	r := mustReporter(t, v, &fakeRepo{}, nil)
	_, err := r.Build(context.Background(), 7, 86400)
	if err == nil {
		t.Fatal("expected error from venue propagated")
	}
}

func TestReporter_HaltStatusFunc(t *testing.T) {
	v := &fakeVenue{}
	halts := HaltStatusFunc(func() (bool, string) {
		return true, "circuit_breaker:price_jump"
	})
	r := mustReporter(t, v, &fakeRepo{}, halts)
	rep, _ := r.Build(context.Background(), 7, 86400)
	if !rep.CircuitBreaker.Halted || rep.CircuitBreaker.HaltReason == "" {
		t.Fatalf("expected halted status, got %+v", rep.CircuitBreaker)
	}
}

func TestReporter_UnknownTradeActionCounted(t *testing.T) {
	v := &fakeVenue{
		trades: []entity.MyTrade{
			{ID: 1, TradeAction: "", Price: 100, CreatedAt: 9_999_500}, // empty
			{ID: 2, TradeAction: "MAKER", Price: 100, CreatedAt: 9_999_700},
		},
	}
	r := mustReporter(t, v, &fakeRepo{}, nil)
	rep, _ := r.Build(context.Background(), 7, 86400)
	if rep.Trades.UnknownCount != 1 || rep.Trades.MakerCount != 1 {
		t.Fatalf("unexpected counts: %+v", rep.Trades)
	}
}
