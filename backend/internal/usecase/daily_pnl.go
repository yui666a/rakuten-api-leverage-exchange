package usecase

import (
	"context"
	"errors"
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
