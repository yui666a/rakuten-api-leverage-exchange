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

type cacheEntry struct {
	advice   *entity.StrategyAdvice
	cachedAt time.Time
}

// LLMService はLLM呼び出しをシンボルごとのキャッシュ付きで管理する。
type LLMService struct {
	client   LLMClient
	cacheTTL time.Duration

	mu    sync.RWMutex
	cache map[int64]*cacheEntry // key: SymbolID
}

func NewLLMService(client LLMClient, cacheTTL time.Duration) *LLMService {
	return &LLMService{
		client:   client,
		cacheTTL: cacheTTL,
		cache:    make(map[int64]*cacheEntry),
	}
}

// GetAdvice はシンボルIDごとにキャッシュされた戦略アドバイスを返す。
// キャッシュが期限切れの場合はLLMに問い合わせて更新する。
// LLMエラー時は古いキャッシュを返すか、キャッシュがなければHOLDを返す。
func (s *LLMService) GetAdvice(ctx context.Context, marketCtx entity.MarketContext) (*entity.StrategyAdvice, error) {
	symbolID := marketCtx.SymbolID

	s.mu.RLock()
	entry := s.cache[symbolID]
	if entry != nil && time.Since(entry.cachedAt) < s.cacheTTL {
		s.mu.RUnlock()
		return entry.advice, nil
	}
	var stale *entity.StrategyAdvice
	if entry != nil {
		stale = entry.advice
	}
	s.mu.RUnlock()

	advice, err := s.client.AnalyzeMarket(ctx, marketCtx)
	if err != nil {
		log.Printf("LLM error (symbol %d), using fallback: %v", symbolID, err)
		if stale != nil {
			// cachedAtを更新してTTLの間は再リクエストを抑制する
			s.mu.Lock()
			if e := s.cache[symbolID]; e != nil {
				e.cachedAt = time.Now()
			}
			s.mu.Unlock()
			return stale, nil
		}
		return &entity.StrategyAdvice{
			Stance:    entity.MarketStanceHold,
			Reasoning: "LLM unavailable, defaulting to HOLD",
			UpdatedAt: time.Now().Unix(),
		}, nil
	}

	s.mu.Lock()
	s.cache[symbolID] = &cacheEntry{advice: advice, cachedAt: time.Now()}
	s.mu.Unlock()

	return advice, nil
}

// GetCachedAdvice はシンボルIDごとのキャッシュされたアドバイスを直接返す（LLM呼び出しなし）。
// キャッシュがなければnilを返す。
func (s *LLMService) GetCachedAdvice(symbolID int64) *entity.StrategyAdvice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if entry := s.cache[symbolID]; entry != nil {
		return entry.advice
	}
	return nil
}
