package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// mockLLMClient はテスト用のLLMクライアント。
type mockLLMClient struct {
	response *entity.StrategyAdvice
	err      error
}

func (m *mockLLMClient) AnalyzeMarket(ctx context.Context, marketCtx entity.MarketContext) (*entity.StrategyAdvice, error) {
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
