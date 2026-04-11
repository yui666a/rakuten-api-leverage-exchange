package usecase

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// fakeRakutenClient は DailyPnLCalculator 単体テスト用のフェイク。
// 呼び出し回数をカウントしてキャッシュ/singleflight 動作を検証できる。
type fakeRakutenClient struct {
	mu sync.Mutex

	symbols []entity.Symbol

	// trades[symbolID] = []MyTrade
	trades map[int64][]entity.MyTrade
	// positions[symbolID] = []Position
	positions map[int64][]entity.Position

	// 失敗設定
	failSymbols        bool
	failTradesSymbol   map[int64]bool
	failPositionSymbol map[int64]bool

	// call counters (atomic)
	symbolsCalls   atomic.Int64
	tradesCalls    atomic.Int64
	positionsCalls atomic.Int64
}

func newFakeRakutenClient() *fakeRakutenClient {
	return &fakeRakutenClient{
		trades:             map[int64][]entity.MyTrade{},
		positions:          map[int64][]entity.Position{},
		failTradesSymbol:   map[int64]bool{},
		failPositionSymbol: map[int64]bool{},
	}
}

func (f *fakeRakutenClient) GetSymbols(_ context.Context) ([]entity.Symbol, error) {
	f.symbolsCalls.Add(1)
	if f.failSymbols {
		return nil, errors.New("symbols failure")
	}
	return f.symbols, nil
}

func (f *fakeRakutenClient) GetMyTrades(_ context.Context, symbolID int64) ([]entity.MyTrade, error) {
	f.tradesCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failTradesSymbol[symbolID] {
		return nil, errors.New("trades failure")
	}
	return f.trades[symbolID], nil
}

func (f *fakeRakutenClient) GetPositions(_ context.Context, symbolID int64) ([]entity.Position, error) {
	f.positionsCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failPositionSymbol[symbolID] {
		return nil, errors.New("positions failure")
	}
	return f.positions[symbolID], nil
}

// fixedClock は固定時刻を返す Clock 実装。
type fixedClock struct{ t time.Time }

func (c *fixedClock) Now() time.Time { return c.t }

// jst は JST 固定ゾーン。プロジェクトの既存コード (pipeline.go:500) と一致。
var jst = time.FixedZone("JST", 9*60*60)

// jstMillis は 指定した JST 日時の unix milliseconds を返すヘルパ。
func jstMillis(year int, month time.Month, day, hour, minute, second, millis int) int64 {
	return time.Date(year, month, day, hour, minute, second, millis*int(time.Millisecond), jst).UnixMilli()
}

func newCalculatorForTest(t *testing.T, fake *fakeRakutenClient, now time.Time) *DailyPnLCalculator {
	t.Helper()
	c := NewDailyPnLCalculator(fake, 10*time.Second)
	c.clock = &fixedClock{t: now}
	return c
}

func TestDailyPnLCalculator_Compute_SumsRealizedAndUnrealized(t *testing.T) {
	fake := newFakeRakutenClient()
	fake.symbols = []entity.Symbol{{ID: 7}, {ID: 10}}

	// 今日 JST 2026-04-12 の trades
	todayNoon := time.Date(2026, 4, 12, 12, 0, 0, 0, jst)

	fake.trades[7] = []entity.MyTrade{
		{ID: 1, SymbolID: 7, CloseTradeProfit: 100, CreatedAt: jstMillis(2026, 4, 12, 9, 0, 0, 0)},
		{ID: 2, SymbolID: 7, CloseTradeProfit: -30, CreatedAt: jstMillis(2026, 4, 12, 10, 0, 0, 0)},
	}
	fake.trades[10] = []entity.MyTrade{
		{ID: 3, SymbolID: 10, CloseTradeProfit: -6, CreatedAt: jstMillis(2026, 4, 12, 11, 0, 0, 0)},
	}

	fake.positions[7] = []entity.Position{
		{ID: 100, SymbolID: 7, FloatingProfit: 50},
	}
	fake.positions[10] = []entity.Position{
		{ID: 200, SymbolID: 10, FloatingProfit: -4},
	}

	c := newCalculatorForTest(t, fake, todayNoon)
	got, err := c.Compute(context.Background())
	if err != nil {
		t.Fatalf("Compute returned error: %v", err)
	}

	// realized = 100 - 30 - 6 = 64
	// unrealized = 50 - 4 = 46
	// total = 110
	if got.Realized != 64 {
		t.Errorf("Realized = %v, want 64", got.Realized)
	}
	if got.Unrealized != 46 {
		t.Errorf("Unrealized = %v, want 46", got.Unrealized)
	}
	if got.Total != 110 {
		t.Errorf("Total = %v, want 110", got.Total)
	}
	if got.Stale {
		t.Errorf("Stale = true, want false")
	}
	if got.ComputedAt != todayNoon.Unix() {
		t.Errorf("ComputedAt = %v, want %v", got.ComputedAt, todayNoon.Unix())
	}
}

func TestDailyPnLCalculator_Compute_JSTBoundary(t *testing.T) {
	fake := newFakeRakutenClient()
	fake.symbols = []entity.Symbol{{ID: 7}}

	// now = 2026-04-12 00:00:01 JST (今日になった直後)
	now := time.Date(2026, 4, 12, 0, 0, 1, 0, jst)

	fake.trades[7] = []entity.MyTrade{
		// 昨日 23:59:59.999 JST の trade → 除外されるべき
		{ID: 1, SymbolID: 7, CloseTradeProfit: 1000, CreatedAt: jstMillis(2026, 4, 11, 23, 59, 59, 999)},
		// 今日 00:00:00.000 JST ちょうど → 含まれるべき
		{ID: 2, SymbolID: 7, CloseTradeProfit: 7, CreatedAt: jstMillis(2026, 4, 12, 0, 0, 0, 0)},
	}
	fake.positions[7] = nil

	c := newCalculatorForTest(t, fake, now)
	got, err := c.Compute(context.Background())
	if err != nil {
		t.Fatalf("Compute returned error: %v", err)
	}

	if got.Realized != 7 {
		t.Errorf("Realized = %v, want 7 (yesterday trade should be excluded, today 00:00 should be included)", got.Realized)
	}
}

func TestDailyPnLCalculator_Compute_CacheHitAvoidsAPICalls(t *testing.T) {
	fake := newFakeRakutenClient()
	fake.symbols = []entity.Symbol{{ID: 7}}
	fake.trades[7] = []entity.MyTrade{
		{ID: 1, SymbolID: 7, CloseTradeProfit: 10, CreatedAt: jstMillis(2026, 4, 12, 12, 0, 0, 0)},
	}
	fake.positions[7] = nil

	now := time.Date(2026, 4, 12, 12, 0, 0, 0, jst)
	c := newCalculatorForTest(t, fake, now)

	// 1 回目: 楽天 API を叩く
	if _, err := c.Compute(context.Background()); err != nil {
		t.Fatalf("first Compute: %v", err)
	}
	firstSymbols := fake.symbolsCalls.Load()
	firstTrades := fake.tradesCalls.Load()
	firstPositions := fake.positionsCalls.Load()

	if firstSymbols != 1 || firstTrades != 1 || firstPositions != 1 {
		t.Fatalf("first call counts: symbols=%d trades=%d positions=%d, want 1/1/1",
			firstSymbols, firstTrades, firstPositions)
	}

	// 2 回目: キャッシュヒット → 呼び出しゼロ
	if _, err := c.Compute(context.Background()); err != nil {
		t.Fatalf("second Compute: %v", err)
	}
	if fake.symbolsCalls.Load() != firstSymbols ||
		fake.tradesCalls.Load() != firstTrades ||
		fake.positionsCalls.Load() != firstPositions {
		t.Errorf("cached call should not invoke rakuten API; got calls symbols=%d trades=%d positions=%d",
			fake.symbolsCalls.Load(), fake.tradesCalls.Load(), fake.positionsCalls.Load())
	}
}

func TestDailyPnLCalculator_Compute_CacheExpiresAfterTTL(t *testing.T) {
	fake := newFakeRakutenClient()
	fake.symbols = []entity.Symbol{{ID: 7}}
	fake.positions[7] = nil

	clock := &fixedClock{t: time.Date(2026, 4, 12, 12, 0, 0, 0, jst)}
	c := NewDailyPnLCalculator(fake, 10*time.Second)
	c.clock = clock

	if _, err := c.Compute(context.Background()); err != nil {
		t.Fatalf("first Compute: %v", err)
	}

	// 10 秒ちょうどはまだ有効 (expiresAt は排他境界) ではないので、
	// 少し進めて TTL 経過扱いにする
	clock.t = clock.t.Add(10*time.Second + time.Millisecond)

	if _, err := c.Compute(context.Background()); err != nil {
		t.Fatalf("second Compute: %v", err)
	}

	if fake.symbolsCalls.Load() != 2 {
		t.Errorf("after TTL expiry, symbolsCalls = %d, want 2", fake.symbolsCalls.Load())
	}
}
