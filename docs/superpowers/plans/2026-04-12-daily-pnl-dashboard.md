# Daily PnL Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ダッシュボードの「日次損益」カードを、全通貨ペア合算の実現損益 (JST 当日分) + 未実現損益で計算する新しいユースケースに置き換える。

**Architecture:** バックエンドに `usecase.DailyPnLCalculator` を新設し、`RiskManager` とは独立させる。`GetSymbols` + `GetMyTrades`/`GetPositions` を銘柄数ぶん並列で叩き、10 秒 TTL のキャッシュと `singleflight` で API 呼び出しを抑制する。`RiskHandler.GetPnL` から新ユースケースを呼んでレスポンスに `dailyPnl` ブロックを追加し、フロントは `dailyPnl.total` を表示する。既存 `dailyLoss` フィールドはリスク制限用に残し、後方互換を維持する。

**Tech Stack:** Go (gin, `golang.org/x/sync/singleflight`, `golang.org/x/sync/errgroup`), TypeScript (React, TanStack Query), vitest.

**Spec reference:** `docs/superpowers/specs/2026-04-11-daily-pnl-dashboard-design.md`

---

## File Structure

**Backend — new:**
- `backend/internal/usecase/daily_pnl.go` — `DailyPnLCalculator`、`DailyPnL` 型、`rakutenClient` interface、`Clock` interface、キャッシュと singleflight ロジック
- `backend/internal/usecase/daily_pnl_test.go` — fake client + fake clock による単体テスト

**Backend — modify:**
- `backend/internal/interfaces/api/router.go` — `Dependencies` に `DailyPnLCalculator` を追加し `NewRiskHandler` に渡す
- `backend/internal/interfaces/api/handler/risk.go` — `RiskHandler` に calculator を埋め込み、`GetPnL` でレスポンスに `dailyPnl` を追加
- `backend/internal/interfaces/api/api_test.go` — `setupRouter` で fake calculator を注入し、`TestGetPnL` で `dailyPnl` を検証
- `backend/cmd/main.go` — 起動時に `DailyPnLCalculator` を組み立てて `Dependencies` に渡す
- `backend/go.mod` / `backend/go.sum` — `golang.org/x/sync` を direct 依存に昇格

**Frontend — modify:**
- `frontend/src/lib/api.ts` — `DailyPnLBreakdown` 型追加、`PnlResponse` 拡張
- `frontend/src/routes/index.tsx` — `dailyPnl.total` を使うよう表示ロジック差し替え

---

## Task 1: `golang.org/x/sync` を direct 依存に昇格

**Files:**
- Modify: `backend/go.mod`
- Modify: `backend/go.sum` (go get が自動更新)

- [ ] **Step 1: Run `go get` to promote the dep**

Run (in `backend/` directory):
```bash
cd backend && go get golang.org/x/sync@v0.20.0
```
Expected: `backend/go.mod` の `require` に `golang.org/x/sync v0.20.0` が `// indirect` なしで現れる。

- [ ] **Step 2: Verify module graph**

Run:
```bash
cd backend && go mod tidy && go build ./...
```
Expected: エラーなし。

- [ ] **Step 3: Commit**

```bash
git add backend/go.mod backend/go.sum
git commit -m "chore(backend): promote golang.org/x/sync to direct dependency"
```

---

## Task 2: `DailyPnLCalculator` の骨組みと型定義

**Files:**
- Create: `backend/internal/usecase/daily_pnl.go`

- [ ] **Step 1: Create the file with types and interfaces**

Create `backend/internal/usecase/daily_pnl.go`:

```go
package usecase

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"golang.org/x/sync/singleflight"
)

// DailyPnL は画面表示用の日次損益ブレークダウン。
// Realized: JST 当日分の決済損益 (MyTrade.CloseTradeProfit の合算)
// Unrealized: 現在保有中ポジションの含み損益 (Position.FloatingProfit の合算)
// Total: Realized + Unrealized
// Stale: 個別銘柄の API 呼び出しに一部失敗した場合、または古いキャッシュを返した場合 true
// ComputedAt: キャッシュ生成時刻 (unix seconds)
type DailyPnL struct {
	Realized   float64 `json:"realized"`
	Unrealized float64 `json:"unrealized"`
	Total      float64 `json:"total"`
	Stale      bool    `json:"stale"`
	ComputedAt int64   `json:"computedAt"`
}

// Clock は時刻を抽象化する。テストで固定時刻を注入するために interface にしている。
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// rakutenClient は DailyPnLCalculator が楽天 REST から必要とするメソッドだけを切り出した interface。
// infrastructure/rakuten.RESTClient はこれを満たす (compile-time で確認)。
type rakutenClient interface {
	GetSymbols(ctx context.Context) ([]entity.Symbol, error)
	GetMyTrades(ctx context.Context, symbolID int64) ([]entity.MyTrade, error)
	GetPositions(ctx context.Context, symbolID int64) ([]entity.Position, error)
}

// cachedPnL はキャッシュ 1 エントリ。atomic.Pointer 経由で lock-free に読み出す。
type cachedPnL struct {
	value     DailyPnL
	expiresAt time.Time
}

type DailyPnLCalculator struct {
	rest  rakutenClient
	clock Clock
	ttl   time.Duration

	cache atomic.Pointer[cachedPnL]
	group singleflight.Group

	// mu は cache を書き換える時だけ使う。Compute 経路は atomic.Load で lock-free。
	mu sync.Mutex
}

// NewDailyPnLCalculator は実行時構築用のコンストラクタ。
// ttl が 0 以下の場合は 10 秒にフォールバックする。
func NewDailyPnLCalculator(rest rakutenClient, ttl time.Duration) *DailyPnLCalculator {
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	return &DailyPnLCalculator{
		rest:  rest,
		clock: realClock{},
		ttl:   ttl,
	}
}

// Compute は最新または TTL 内キャッシュから DailyPnL を返す。
// 実装は後続タスクで埋める。
func (c *DailyPnLCalculator) Compute(ctx context.Context) (DailyPnL, error) {
	return DailyPnL{}, nil
}
```

- [ ] **Step 2: Verify compile**

Run:
```bash
cd backend && go build ./internal/usecase/...
```
Expected: エラーなし。

- [ ] **Step 3: Commit**

```bash
git add backend/internal/usecase/daily_pnl.go
git commit -m "feat(backend): scaffold DailyPnLCalculator types and interfaces"
```

---

## Task 3: 単純正常系テスト (実現 + 未実現集計)

**Files:**
- Create: `backend/internal/usecase/daily_pnl_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/usecase/daily_pnl_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
cd backend && go test ./internal/usecase/ -run TestDailyPnLCalculator_Compute_SumsRealizedAndUnrealized -v
```
Expected: FAIL — `Compute` はまだ zero 値を返す実装なので `Realized = 0, want 64` 等で落ちる。

- [ ] **Step 3: Implement Compute**

Replace the `Compute` function in `backend/internal/usecase/daily_pnl.go`:

```go
func (c *DailyPnLCalculator) Compute(ctx context.Context) (DailyPnL, error) {
	now := c.clock.Now()

	// 1. キャッシュが生きていれば返す
	if cached := c.cache.Load(); cached != nil && now.Before(cached.expiresAt) {
		return cached.value, nil
	}

	// 2. singleflight で同時リクエストを 1 コールに収束
	key := "daily_pnl"
	res, err, _ := c.group.Do(key, func() (any, error) {
		return c.fetchAndCompute(ctx, now)
	})
	if err != nil {
		return DailyPnL{}, err
	}
	return res.(DailyPnL), nil
}

// fetchAndCompute は楽天 API から trades/positions を取得し、JST 今日分の realized と全 unrealized を計算する。
func (c *DailyPnLCalculator) fetchAndCompute(ctx context.Context, now time.Time) (DailyPnL, error) {
	symbols, err := c.rest.GetSymbols(ctx)
	if err != nil {
		return DailyPnL{}, err
	}

	nowJST := now.In(jstZone)
	todayStart := time.Date(nowJST.Year(), nowJST.Month(), nowJST.Day(), 0, 0, 0, 0, jstZone)
	cutoffMillis := todayStart.UnixMilli()

	var (
		mu         sync.Mutex
		realized   float64
		unrealized float64
		failed     int
	)

	var wg sync.WaitGroup
	for _, sym := range symbols {
		sym := sym
		wg.Add(1)
		go func() {
			defer wg.Done()
			trades, tErr := c.rest.GetMyTrades(ctx, sym.ID)
			if tErr != nil {
				mu.Lock()
				failed++
				mu.Unlock()
			} else {
				var sum float64
				for _, tr := range trades {
					if tr.CreatedAt >= cutoffMillis {
						sum += float64(tr.CloseTradeProfit)
					}
				}
				mu.Lock()
				realized += sum
				mu.Unlock()
			}

			positions, pErr := c.rest.GetPositions(ctx, sym.ID)
			if pErr != nil {
				mu.Lock()
				failed++
				mu.Unlock()
			} else {
				var sum float64
				for _, pos := range positions {
					sum += pos.FloatingProfit
				}
				mu.Lock()
				unrealized += sum
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// 全呼び出し失敗 (symbols × 2 = trades + positions) ならエラーにする。
	// 1 つでも成功していれば stale フラグを立てて結果を返す。
	totalCalls := len(symbols) * 2
	if totalCalls > 0 && failed == totalCalls {
		return DailyPnL{}, errors.New("daily_pnl: all rakuten API calls failed")
	}

	result := DailyPnL{
		Realized:   realized,
		Unrealized: unrealized,
		Total:      realized + unrealized,
		Stale:      failed > 0,
		ComputedAt: now.Unix(),
	}

	c.mu.Lock()
	c.cache.Store(&cachedPnL{
		value:     result,
		expiresAt: now.Add(c.ttl),
	})
	c.mu.Unlock()

	return result, nil
}

// jstZone は pipeline.go の restoreRiskState と同じ JST 固定ゾーン。
var jstZone = time.FixedZone("JST", 9*60*60)
```

Also add the import for `errors` at the top of `daily_pnl.go`:

```go
import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"golang.org/x/sync/singleflight"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
cd backend && go test ./internal/usecase/ -run TestDailyPnLCalculator_Compute_SumsRealizedAndUnrealized -v
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/usecase/daily_pnl.go backend/internal/usecase/daily_pnl_test.go
git commit -m "feat(backend): DailyPnLCalculator computes realized+unrealized pnl"
```

---

## Task 4: JST 境界テスト

**Files:**
- Modify: `backend/internal/usecase/daily_pnl_test.go`

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/usecase/daily_pnl_test.go`:

```go
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
```

- [ ] **Step 2: Run test**

Run:
```bash
cd backend && go test ./internal/usecase/ -run TestDailyPnLCalculator_Compute_JSTBoundary -v
```
Expected: PASS (実装は Task 3 で既に境界ロジックを含んでいる)

- [ ] **Step 3: Commit**

```bash
git add backend/internal/usecase/daily_pnl_test.go
git commit -m "test(backend): verify JST boundary in DailyPnLCalculator"
```

---

## Task 5: キャッシュと TTL 経過テスト

**Files:**
- Modify: `backend/internal/usecase/daily_pnl_test.go`

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/usecase/daily_pnl_test.go`:

```go
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
```

- [ ] **Step 2: Run tests**

Run:
```bash
cd backend && go test ./internal/usecase/ -run TestDailyPnLCalculator_Compute_Cache -v
```
Expected: PASS (両方)

- [ ] **Step 3: Commit**

```bash
git add backend/internal/usecase/daily_pnl_test.go
git commit -m "test(backend): verify DailyPnLCalculator cache hit and TTL expiry"
```

---

## Task 6: singleflight 並行テスト

**Files:**
- Modify: `backend/internal/usecase/daily_pnl_test.go`

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/usecase/daily_pnl_test.go`:

```go
func TestDailyPnLCalculator_Compute_SingleflightCollapsesConcurrent(t *testing.T) {
	// 100 goroutine が同時に Compute しても、楽天 API は 1 セット分 (symbols=1, trades=1, positions=1) しか呼ばれない
	fake := newFakeRakutenClient()
	fake.symbols = []entity.Symbol{{ID: 7}}
	fake.positions[7] = nil

	// 楽天 API に遅延を入れて singleflight の効果を観測する
	// (fake 自体は同期的なので GetSymbols に sleep を仕込むためラッパーを使う)
	slow := &slowFakeClient{inner: fake, delay: 50 * time.Millisecond}

	c := NewDailyPnLCalculator(slow, 10*time.Second)
	c.clock = &fixedClock{t: time.Date(2026, 4, 12, 12, 0, 0, 0, jst)}

	var wg sync.WaitGroup
	const N = 100
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Compute(context.Background()); err != nil {
				t.Errorf("Compute: %v", err)
			}
		}()
	}
	wg.Wait()

	if fake.symbolsCalls.Load() != 1 {
		t.Errorf("symbolsCalls = %d, want 1 (singleflight collapses concurrent callers)",
			fake.symbolsCalls.Load())
	}
	if fake.tradesCalls.Load() != 1 {
		t.Errorf("tradesCalls = %d, want 1", fake.tradesCalls.Load())
	}
	if fake.positionsCalls.Load() != 1 {
		t.Errorf("positionsCalls = %d, want 1", fake.positionsCalls.Load())
	}
}

// slowFakeClient は GetSymbols/GetMyTrades/GetPositions に人工的な遅延を加える。
// singleflight 検証用に「最初の呼び出しが完了する前に後続が合流できる」状況を作る。
type slowFakeClient struct {
	inner *fakeRakutenClient
	delay time.Duration
}

func (s *slowFakeClient) GetSymbols(ctx context.Context) ([]entity.Symbol, error) {
	time.Sleep(s.delay)
	return s.inner.GetSymbols(ctx)
}
func (s *slowFakeClient) GetMyTrades(ctx context.Context, symbolID int64) ([]entity.MyTrade, error) {
	time.Sleep(s.delay)
	return s.inner.GetMyTrades(ctx, symbolID)
}
func (s *slowFakeClient) GetPositions(ctx context.Context, symbolID int64) ([]entity.Position, error) {
	time.Sleep(s.delay)
	return s.inner.GetPositions(ctx, symbolID)
}
```

- [ ] **Step 2: Run test**

Run:
```bash
cd backend && go test ./internal/usecase/ -run TestDailyPnLCalculator_Compute_Singleflight -v
```
Expected: PASS (singleflight が 1 コールに収束)

- [ ] **Step 3: Commit**

```bash
git add backend/internal/usecase/daily_pnl_test.go
git commit -m "test(backend): verify DailyPnLCalculator singleflight collapses concurrent callers"
```

---

## Task 7: 部分失敗 / 全失敗 / 空ケース のテスト

**Files:**
- Modify: `backend/internal/usecase/daily_pnl_test.go`

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/usecase/daily_pnl_test.go`:

```go
func TestDailyPnLCalculator_Compute_PartialFailureReturnsStale(t *testing.T) {
	fake := newFakeRakutenClient()
	fake.symbols = []entity.Symbol{{ID: 7}, {ID: 10}}

	fake.trades[7] = []entity.MyTrade{
		{ID: 1, SymbolID: 7, CloseTradeProfit: 20, CreatedAt: jstMillis(2026, 4, 12, 12, 0, 0, 0)},
	}
	// symbol 10 の positions だけ失敗させる
	fake.failPositionSymbol[10] = true
	fake.positions[7] = []entity.Position{{FloatingProfit: 5}}

	c := newCalculatorForTest(t, fake, time.Date(2026, 4, 12, 13, 0, 0, 0, jst))
	got, err := c.Compute(context.Background())
	if err != nil {
		t.Fatalf("Compute should not error on partial failure: %v", err)
	}
	if !got.Stale {
		t.Errorf("Stale = false, want true on partial failure")
	}
	// realized は symbol 7 の 20
	if got.Realized != 20 {
		t.Errorf("Realized = %v, want 20", got.Realized)
	}
	// unrealized は symbol 7 の 5 のみ (symbol 10 は失敗で 0 扱い)
	if got.Unrealized != 5 {
		t.Errorf("Unrealized = %v, want 5", got.Unrealized)
	}
}

func TestDailyPnLCalculator_Compute_AllFailureReturnsError(t *testing.T) {
	fake := newFakeRakutenClient()
	fake.symbols = []entity.Symbol{{ID: 7}}
	fake.failTradesSymbol[7] = true
	fake.failPositionSymbol[7] = true

	c := newCalculatorForTest(t, fake, time.Date(2026, 4, 12, 12, 0, 0, 0, jst))
	_, err := c.Compute(context.Background())
	if err == nil {
		t.Fatalf("expected error on all-failure, got nil")
	}
}

func TestDailyPnLCalculator_Compute_Empty(t *testing.T) {
	fake := newFakeRakutenClient()
	fake.symbols = []entity.Symbol{{ID: 7}}
	fake.trades[7] = nil
	fake.positions[7] = nil

	c := newCalculatorForTest(t, fake, time.Date(2026, 4, 12, 12, 0, 0, 0, jst))
	got, err := c.Compute(context.Background())
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got.Total != 0 || got.Realized != 0 || got.Unrealized != 0 || got.Stale {
		t.Errorf("empty result got %+v, want all zero and Stale=false", got)
	}
}
```

- [ ] **Step 2: Run tests**

Run:
```bash
cd backend && go test ./internal/usecase/ -run "TestDailyPnLCalculator_Compute_(PartialFailureReturnsStale|AllFailureReturnsError|Empty)" -v
```
Expected: PASS (全 3 ケース)

- [ ] **Step 3: Commit**

```bash
git add backend/internal/usecase/daily_pnl_test.go
git commit -m "test(backend): cover partial/all failure and empty cases in DailyPnLCalculator"
```

---

## Task 8: `rakuten.RESTClient` が interface を満たすことのコンパイル時検証

**Files:**
- Modify: `backend/internal/usecase/daily_pnl.go`

- [ ] **Step 1: Add compile-time assertion**

Add to the end of `backend/internal/usecase/daily_pnl.go` (before the `jstZone` declaration):

```go
// Compile-time check: *rakuten.RESTClient must implement rakutenClient.
// 直接 import すると usecase → infrastructure 方向になり import サイクルを誘発する可能性があるため、
// この assertion は main.go 側で行う (`var _ usecase.rakutenClient = (*rakuten.RESTClient)(nil)` も不可)。
// 代わりに NewDailyPnLCalculator に interface 型で受け取ることで構造的にのみ担保する。
// ここには noop の説明コメントのみを残す。
```

**Rationale:** `usecase` パッケージから `infrastructure/rakuten` を import すると既存のレイヤリングに反する。`rakutenClient` は構造的 interface なので、`main.go` で `NewDailyPnLCalculator(restClient, ...)` を書いた時点でコンパイラが自動的に型整合を検証する。そのため明示的な assertion コードは不要。

- [ ] **Step 2: Verify still compiles**

Run:
```bash
cd backend && go build ./internal/usecase/...
```
Expected: エラーなし。

- [ ] **Step 3: (No commit)**

このタスクはコードコメント追加のみで動作に変更なし。Task 9 のコミットに含める。

---

## Task 9: `api.Dependencies` に `DailyPnLCalculator` を追加

**Files:**
- Modify: `backend/internal/interfaces/api/router.go`

- [ ] **Step 1: Add field to Dependencies struct**

Edit `backend/internal/interfaces/api/router.go` (around line 22-36):

```go
type Dependencies struct {
	RiskManager         *usecase.RiskManager
	StanceResolver      *usecase.RuleBasedStanceResolver
	IndicatorCalculator *usecase.IndicatorCalculator
	MarketDataService   *usecase.MarketDataService
	RealtimeHub         *usecase.RealtimeHub
	OrderClient         repository.OrderClient
	OrderExecutor       *usecase.OrderExecutor
	Pipeline            PipelineController
	RESTClient          *rakuten.RESTClient
	ClientOrderRepo     repository.ClientOrderRepository
	DailyPnLCalculator  *usecase.DailyPnLCalculator
	// OnSymbolSwitch はシンボル切替時に pipeline から呼び出されるコールバック。
	// main 側で WebSocket 購読切替とローソク足 bootstrap を実行する。
	OnSymbolSwitch func(oldID, newID int64)
}
```

- [ ] **Step 2: Pass it to NewRiskHandler**

In the same file, find the `riskHandler := handler.NewRiskHandler(...)` line (around line 56) and change to:

```go
	riskHandler := handler.NewRiskHandler(deps.RiskManager, deps.RealtimeHub, deps.DailyPnLCalculator)
```

- [ ] **Step 3: Verify compile (expected to fail until Task 10)**

Run:
```bash
cd backend && go build ./internal/interfaces/api/...
```
Expected: FAIL — `NewRiskHandler` の signature がまだ 3 引数を受け付けていない。次タスクで合わせる。

- [ ] **Step 4: (No commit)**

次タスクで一緒にコミットする。

---

## Task 10: `RiskHandler` に calculator を注入し `GetPnL` を拡張

**Files:**
- Modify: `backend/internal/interfaces/api/handler/risk.go`

- [ ] **Step 1: Add field and constructor param**

Edit `backend/internal/interfaces/api/handler/risk.go`:

```go
package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

type RiskHandler struct {
	riskMgr     *usecase.RiskManager
	realtimeHub *usecase.RealtimeHub
	pnlCalc     *usecase.DailyPnLCalculator
}

func NewRiskHandler(riskMgr *usecase.RiskManager, realtimeHub *usecase.RealtimeHub, pnlCalc *usecase.DailyPnLCalculator) *RiskHandler {
	return &RiskHandler{riskMgr: riskMgr, realtimeHub: realtimeHub, pnlCalc: pnlCalc}
}
```

Keep `GetConfig`, `UpdateConfig`, `validateRiskConfig` unchanged.

- [ ] **Step 2: Extend GetPnL**

Replace `GetPnL` in the same file:

```go
func (h *RiskHandler) GetPnL(c *gin.Context) {
	status := h.riskMgr.GetStatus()

	resp := gin.H{
		"balance":       status.Balance,
		"dailyLoss":     status.DailyLoss,
		"totalPosition": status.TotalPosition,
		"tradingHalted": status.TradingHalted,
	}

	if h.pnlCalc != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		pnl, err := h.pnlCalc.Compute(ctx)
		if err != nil {
			slog.Warn("daily pnl compute failed", "error", err)
			// エラー時は dailyPnl ブロック自体を省略 (フロントは undefined として handle する)
		} else {
			resp["dailyPnl"] = pnl
		}
	}

	c.JSON(http.StatusOK, resp)
}
```

- [ ] **Step 3: Verify compile**

Run:
```bash
cd backend && go build ./...
```
Expected: エラーなし (Task 9 の router.go と整合)。

- [ ] **Step 4: Commit**

```bash
git add backend/internal/interfaces/api/router.go backend/internal/interfaces/api/handler/risk.go backend/internal/usecase/daily_pnl.go
git commit -m "feat(backend): inject DailyPnLCalculator into RiskHandler and extend /api/v1/pnl"
```

---

## Task 11: `api_test.go` を新 signature に合わせて修正 + `dailyPnl` 検証

**Files:**
- Modify: `backend/internal/interfaces/api/api_test.go`

- [ ] **Step 1: Add fake rakutenClient for handler tests**

既存の `mockOrderClient` はそのまま。ただし `DailyPnLCalculator` は nil を許容するようにしてあるので、**ハンドラテスト側では calculator を nil のまま渡しても既存テストはパスする**。ただし `TestGetPnL` で `dailyPnl` ブロックが存在しうることを検証したい。

以下の方針で進める:
1. `setupRouter` を nil calculator のままにし、既存テストの後方互換を確認
2. `setupRouterWithPnl` を新規追加し、固定値を返す stub calculator を使う別テストを追加

まず `setupRouter` は今まで通り (calculator 未設定) で動くことを確認するだけ。Dependencies の `DailyPnLCalculator` は zero value (`nil`) で OK。

実際に編集が必要なのは `TestGetPnL` で**既存フィールドが維持されていること**の確認のみ。

Append after `TestGetPnL`:

```go
func TestGetPnL_PreservesLegacyFields(t *testing.T) {
	router := setupRouter()
	w := doRequest(router, "GET", "/api/v1/pnl", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// 既存フィールドが引き続き返ること
	for _, key := range []string{"balance", "dailyLoss", "totalPosition", "tradingHalted"} {
		if _, ok := body[key]; !ok {
			t.Errorf("expected field %q in response, got keys=%v", key, mapKeys(body))
		}
	}

	// calculator は setupRouter では nil なので dailyPnl は省略されるはず
	if _, ok := body["dailyPnl"]; ok {
		t.Errorf("dailyPnl should be absent when calculator is nil, got %v", body["dailyPnl"])
	}
}

func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
```

- [ ] **Step 2: Run full api_test**

Run:
```bash
cd backend && go test ./internal/interfaces/api/... -v
```
Expected: 全 PASS。特に `TestGetPnL` と `TestGetPnL_PreservesLegacyFields` が通る。

- [ ] **Step 3: Commit**

```bash
git add backend/internal/interfaces/api/api_test.go
git commit -m "test(backend): assert /api/v1/pnl preserves legacy fields"
```

---

## Task 12: `main.go` で calculator を組み立てて注入

**Files:**
- Modify: `backend/cmd/main.go`

- [ ] **Step 1: Find the deps assembly site**

Run:
```bash
cd backend && grep -n "Dependencies{" cmd/main.go
```
Expected: `deps := api.Dependencies{` もしくは類似の行が見つかる。

- [ ] **Step 2: Add DailyPnLCalculator construction and wire it**

`backend/cmd/main.go` の `deps := api.Dependencies{...}` を組み立てている箇所の直前に以下を追加:

```go
	dailyPnLCalc := usecase.NewDailyPnLCalculator(restClient, 10*time.Second)
```

そして `deps` の初期化フィールドに追加:

```go
	DailyPnLCalculator: dailyPnLCalc,
```

(変数名 `restClient` は既存のもの。`time` パッケージは既に import 済みのはずだが、確認して必要なら追加する。)

- [ ] **Step 3: Build**

Run:
```bash
cd backend && go build ./...
```
Expected: エラーなし。

- [ ] **Step 4: Run all backend tests**

Run:
```bash
cd backend && go test ./...
```
Expected: 全 PASS。

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/main.go
git commit -m "feat(backend): wire DailyPnLCalculator in main"
```

---

## Task 13: Frontend 型拡張

**Files:**
- Modify: `frontend/src/lib/api.ts`

- [ ] **Step 1: Add type and extend PnlResponse**

Edit `frontend/src/lib/api.ts`, after the `StatusResponse` type (around line 16), insert:

```ts
export type DailyPnLBreakdown = {
  realized: number
  unrealized: number
  total: number
  stale: boolean
  computedAt: number
}
```

And change `PnlResponse`:

```ts
export type PnlResponse = {
  balance: number
  dailyLoss: number
  totalPosition: number
  tradingHalted: boolean
  dailyPnl?: DailyPnLBreakdown
}
```

(`dailyPnl?` を optional にするのはバックエンドが失敗時省略するため。)

- [ ] **Step 2: Type check**

Run:
```bash
cd frontend && pnpm tsc --noEmit
```
Expected: エラーなし。

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/api.ts
git commit -m "feat(frontend): extend PnlResponse with dailyPnl breakdown"
```

---

## Task 14: ダッシュボードの表示ロジック差し替え

**Files:**
- Modify: `frontend/src/routes/index.tsx`

- [ ] **Step 1: Replace daily pnl derivation**

Edit `frontend/src/routes/index.tsx` (around line 40-46):

```tsx
  const dailyPnlTotal = pnl?.dailyPnl?.total ?? null
  const dailyPnlStale = pnl?.dailyPnl?.stale ?? false
  const dailyPnlLabel =
    dailyPnlTotal === null
      ? '\u2014'
      : `${dailyPnlTotal < 0 ? '-' : ''}¥${Math.abs(dailyPnlTotal).toLocaleString()}${dailyPnlStale ? '*' : ''}`
```

And update the `KpiCard` for 日次損益 (around line 65-69):

```tsx
        <KpiCard
          label="日次損益"
          value={dailyPnlLabel}
          color={dailyPnlTotal !== null && dailyPnlTotal < 0 ? 'text-accent-red' : 'text-accent-green'}
        />
```

- [ ] **Step 2: Type check**

Run:
```bash
cd frontend && pnpm tsc --noEmit
```
Expected: エラーなし。

- [ ] **Step 3: Run frontend tests (if any exist)**

Run:
```bash
cd frontend && pnpm test
```
Expected: 既存テストが通る (もしテストが無い場合は "no tests found" でも可)。

- [ ] **Step 4: Commit**

```bash
git add frontend/src/routes/index.tsx
git commit -m "feat(frontend): show dailyPnl.total (realized+unrealized) in dashboard"
```

---

## Task 15: 手動 E2E 検証 (楽天本番の実残高で動作確認)

**Files:** なし (検証のみ)

- [ ] **Step 1: Start or verify the stack is running**

Run:
```bash
docker ps --format '{{.Names}}\t{{.Ports}}' | grep rakuten-api-leverage-exchange
```
Expected: `rakuten-api-leverage-exchange-backend-1` と `...-frontend-1` が UP。  
なければ `docker compose up -d` で起動。

- [ ] **Step 2: Hit /api/v1/pnl and check dailyPnl shape**

Run:
```bash
curl -s http://localhost:38080/api/v1/pnl | python3 -m json.tool
```
Expected: `dailyPnl` ブロックに `realized`, `unrealized`, `total`, `stale`, `computedAt` が含まれる。

- [ ] **Step 3: Cross-check with /trades**

Run:
```bash
curl -s 'http://localhost:38080/api/v1/trades/all' | python3 -m json.tool | head -80
```
Expected: 今日 JST 分の `closeTradeProfit` 合算が `/pnl` の `dailyPnl.realized` と一致する。

- [ ] **Step 4: Verify frontend shows it**

Playwright MCP で `http://localhost:33000/?symbol=LTC_JPY` を開いて「日次損益」カードを確認:

```
mcp__playwright-noext__browser_navigate → http://localhost:33000/?symbol=LTC_JPY
mcp__playwright-noext__browser_snapshot
```

Expected: 日次損益カードが `¥0` ではなく実際の値 (例: `-¥6`) を赤文字で表示している。

- [ ] **Step 5: Verify cache behavior**

Run:
```bash
for i in 1 2 3 4 5; do curl -s http://localhost:38080/api/v1/pnl | python3 -c "import sys,json; d=json.load(sys.stdin); p=d.get('dailyPnl',{}); print(p.get('computedAt'), p.get('total'))"; sleep 2; done
```
Expected: 10 秒以内の呼び出しでは `computedAt` が変化しない (同一エポック秒)。10 秒以上経つと更新される。

- [ ] **Step 6: (No commit) Report results**

検証結果を会話に報告して完了。

---

## Self-Review

Spec section → task mapping:
- `DailyPnLCalculator` 構造と interface → Task 2 ✓
- 実現 + 未実現集計 → Task 3 ✓
- JST 境界 → Task 3 (実装) + Task 4 (テスト) ✓
- エラー処理 (部分失敗 stale / 全失敗エラー) → Task 3 (実装) + Task 7 (テスト) ✓
- キャッシュ 10 秒 TTL + singleflight → Task 3 (実装) + Task 5/6 (テスト) ✓
- API スキーマ拡張 (`dailyPnl` ブロック) → Task 10 ✓
- 後方互換 (`dailyLoss` 等維持) → Task 10 + Task 11 (テスト) ✓
- Frontend 型 → Task 13 ✓
- Frontend 表示差し替え → Task 14 ✓
- 手動 E2E 検証 → Task 15 ✓
- `golang.org/x/sync` direct 化 → Task 1 ✓

No placeholders. Types consistent between tasks (`DailyPnL`, `rakutenClient`, `pnlCalc`).
