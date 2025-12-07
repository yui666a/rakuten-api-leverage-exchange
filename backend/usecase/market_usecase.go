package usecase

import (
	"context"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/domain"
)

// MarketUsecase handles market-related business logic
type MarketUsecase struct {
	marketRepo domain.MarketRepository
}

// NewMarketUsecase creates a new MarketUsecase instance
func NewMarketUsecase(marketRepo domain.MarketRepository) *MarketUsecase {
	return &MarketUsecase{
		marketRepo: marketRepo,
	}
}

// GetMarket retrieves market data for a specific symbol
func (u *MarketUsecase) GetMarket(ctx context.Context, symbol string) (*domain.Market, error) {
	return u.marketRepo.GetMarket(ctx, symbol)
}

// GetAllMarkets retrieves all available markets
func (u *MarketUsecase) GetAllMarkets(ctx context.Context) ([]domain.Market, error) {
	return u.marketRepo.GetAllMarkets(ctx)
}
