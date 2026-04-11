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
