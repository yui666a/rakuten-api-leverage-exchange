package usecase

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// LLMClient はLLMプロバイダーへのインターフェース。
type LLMClient interface {
	AnalyzeMarket(ctx context.Context, marketCtx entity.MarketContext) (*entity.StrategyAdvice, error)
}

// LLMService はLLM呼び出しをキャッシュ付きで管理する。
type LLMService struct {
	client   LLMClient
	cacheTTL time.Duration

	mu       sync.RWMutex
	cached   *entity.StrategyAdvice
	cachedAt time.Time
}

func NewLLMService(client LLMClient, cacheTTL time.Duration) *LLMService {
	return &LLMService{
		client:   client,
		cacheTTL: cacheTTL,
	}
}

// GetAdvice はキャッシュされた戦略アドバイスを返す。
// キャッシュが期限切れの場合はLLMに問い合わせて更新する。
// LLMエラー時は古いキャッシュを返すか、キャッシュがなければHOLDを返す。
func (s *LLMService) GetAdvice(ctx context.Context, marketCtx entity.MarketContext) (*entity.StrategyAdvice, error) {
	s.mu.RLock()
	if s.cached != nil && time.Since(s.cachedAt) < s.cacheTTL {
		cached := s.cached
		s.mu.RUnlock()
		return cached, nil
	}
	stale := s.cached
	s.mu.RUnlock()

	advice, err := s.client.AnalyzeMarket(ctx, marketCtx)
	if err != nil {
		log.Printf("LLM error, using fallback: %v", err)
		if stale != nil {
			return stale, nil
		}
		return &entity.StrategyAdvice{
			Stance:    entity.MarketStanceHold,
			Reasoning: "LLM unavailable, defaulting to HOLD",
			UpdatedAt: time.Now().Unix(),
		}, nil
	}

	s.mu.Lock()
	s.cached = advice
	s.cachedAt = time.Now()
	s.mu.Unlock()

	return advice, nil
}

// GetCachedAdvice はキャッシュされたアドバイスを直接返す（LLM呼び出しなし）。
// キャッシュがなければnilを返す。
func (s *LLMService) GetCachedAdvice() *entity.StrategyAdvice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cached
}
