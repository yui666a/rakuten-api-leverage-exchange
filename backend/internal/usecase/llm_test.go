package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// mockLLMClient はテスト用のLLMクライアント。
type mockLLMClient struct {
	response  *entity.StrategyAdvice
	err       error
	callCount int
}

func (m *mockLLMClient) AnalyzeMarket(ctx context.Context, marketCtx entity.MarketContext) (*entity.StrategyAdvice, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func TestLLMService_GetAdvice_ReturnsCachedAdvice(t *testing.T) {
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend detected",
			UpdatedAt: time.Now().Unix(),
		},
	}
	svc := NewLLMService(mock, 15*time.Minute)

	marketCtx := entity.MarketContext{
		SymbolID:  7,
		LastPrice: 5000000,
		Indicators: entity.IndicatorSet{
			SymbolID: 7,
		},
	}
	advice, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advice.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW, got %s", advice.Stance)
	}

	mock.response = &entity.StrategyAdvice{
		Stance:    entity.MarketStanceContrarian,
		Reasoning: "should not be used",
		UpdatedAt: time.Now().Unix(),
	}
	advice2, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advice2.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected cached TREND_FOLLOW, got %s", advice2.Stance)
	}
}

func TestLLMService_GetAdvice_CacheIsolatedBySymbol(t *testing.T) {
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "BTC uptrend",
			UpdatedAt: time.Now().Unix(),
		},
	}
	svc := NewLLMService(mock, 15*time.Minute)

	// Symbol 7 (BTC) のアドバイスを取得してキャッシュ
	btcCtx := entity.MarketContext{SymbolID: 7, LastPrice: 5000000}
	advice1, err := svc.GetAdvice(context.Background(), btcCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advice1.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected TREND_FOLLOW for BTC, got %s", advice1.Stance)
	}

	// mockを変更: Symbol 8 (ETH) は別の方針
	mock.response = &entity.StrategyAdvice{
		Stance:    entity.MarketStanceContrarian,
		Reasoning: "ETH oversold",
		UpdatedAt: time.Now().Unix(),
	}

	// Symbol 8 (ETH) は別のキャッシュエントリを使うべき
	ethCtx := entity.MarketContext{SymbolID: 8, LastPrice: 300000}
	advice2, err := svc.GetAdvice(context.Background(), ethCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advice2.Stance != entity.MarketStanceContrarian {
		t.Fatalf("expected CONTRARIAN for ETH, got %s (BTC cache leaked)", advice2.Stance)
	}

	// BTCのキャッシュが影響を受けていないことを確認
	advice3, err := svc.GetAdvice(context.Background(), btcCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advice3.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected cached TREND_FOLLOW for BTC, got %s", advice3.Stance)
	}
}

func TestLLMService_GetAdvice_ExpiredCacheRefreshes(t *testing.T) {
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			UpdatedAt: time.Now().Unix(),
		},
	}
	svc := NewLLMService(mock, 0)

	marketCtx := entity.MarketContext{SymbolID: 7, LastPrice: 5000000}
	_, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mock.response = &entity.StrategyAdvice{
		Stance:    entity.MarketStanceContrarian,
		Reasoning: "reversal",
		UpdatedAt: time.Now().Unix(),
	}

	advice, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advice.Stance != entity.MarketStanceContrarian {
		t.Fatalf("expected CONTRARIAN after refresh, got %s", advice.Stance)
	}
}

func TestLLMService_GetAdvice_FallbackToHoldOnError(t *testing.T) {
	mock := &mockLLMClient{
		err: context.DeadlineExceeded,
	}
	svc := NewLLMService(mock, 15*time.Minute)

	marketCtx := entity.MarketContext{SymbolID: 7, LastPrice: 5000000}
	advice, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("should not return error, got: %v", err)
	}
	if advice.Stance != entity.MarketStanceHold {
		t.Fatalf("expected HOLD fallback on error, got %s", advice.Stance)
	}
}

func TestLLMService_GetAdvice_UseStaleCacheOnError(t *testing.T) {
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			UpdatedAt: time.Now().Unix(),
		},
	}
	svc := NewLLMService(mock, 0)

	marketCtx := entity.MarketContext{SymbolID: 7, LastPrice: 5000000}
	_, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mock.err = context.DeadlineExceeded
	mock.response = nil

	advice, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("should not return error: %v", err)
	}
	if advice.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected stale cache TREND_FOLLOW, got %s", advice.Stance)
	}
}

func TestLLMService_GetAdvice_StaleCacheBackoffOnError(t *testing.T) {
	mock := &mockLLMClient{
		response: &entity.StrategyAdvice{
			Stance:    entity.MarketStanceTrendFollow,
			Reasoning: "uptrend",
			UpdatedAt: time.Now().Unix(),
		},
	}
	// TTL 15分でキャッシュ
	svc := NewLLMService(mock, 15*time.Minute)

	marketCtx := entity.MarketContext{SymbolID: 7, LastPrice: 5000000}
	_, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.callCount != 1 {
		t.Fatalf("expected 1 LLM call, got %d", mock.callCount)
	}

	// キャッシュを強制期限切れにする
	svc.mu.Lock()
	svc.cache[7].cachedAt = time.Now().Add(-20 * time.Minute)
	svc.mu.Unlock()

	// LLMがエラーを返すようにする
	mock.err = context.DeadlineExceeded
	mock.response = nil

	// 1回目のエラー: staleキャッシュを返し、cachedAtを更新
	advice, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("should not return error: %v", err)
	}
	if advice.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected stale TREND_FOLLOW, got %s", advice.Stance)
	}
	if mock.callCount != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", mock.callCount)
	}

	// 2回目: cachedAtが更新されたのでTTL内 → LLMは呼ばれない
	advice2, err := svc.GetAdvice(context.Background(), marketCtx)
	if err != nil {
		t.Fatalf("should not return error: %v", err)
	}
	if advice2.Stance != entity.MarketStanceTrendFollow {
		t.Fatalf("expected cached TREND_FOLLOW, got %s", advice2.Stance)
	}
	if mock.callCount != 2 {
		t.Fatalf("expected still 2 LLM calls (backoff), got %d", mock.callCount)
	}
}
