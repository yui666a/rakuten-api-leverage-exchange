package usecase

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/domain"
	"time"
)

// OrderUsecase handles order-related business logic
type OrderUsecase struct {
	orderRepo   domain.OrderRepository
	accountRepo domain.AccountRepository
}

// NewOrderUsecase creates a new OrderUsecase instance
func NewOrderUsecase(orderRepo domain.OrderRepository, accountRepo domain.AccountRepository) *OrderUsecase {
	return &OrderUsecase{
		orderRepo:   orderRepo,
		accountRepo: accountRepo,
	}
}

// CreateOrder creates a new trading order
func (u *OrderUsecase) CreateOrder(ctx context.Context, order *domain.Order) error {
	// Validate order
	if order.Symbol == "" {
		return errors.New("symbol is required")
	}
	if order.Amount <= 0 {
		return errors.New("amount must be positive")
	}
	if order.Side != "buy" && order.Side != "sell" {
		return errors.New("side must be 'buy' or 'sell'")
	}

	// Set order defaults
	order.ID = uuid.New().String()
	order.Status = "pending"
	order.CreatedAt = time.Now()
	order.UpdatedAt = time.Now()

	return u.orderRepo.CreateOrder(ctx, order)
}

// GetOrder retrieves an order by ID
func (u *OrderUsecase) GetOrder(ctx context.Context, id string) (*domain.Order, error) {
	return u.orderRepo.GetOrder(ctx, id)
}

// GetOrders retrieves all orders
func (u *OrderUsecase) GetOrders(ctx context.Context) ([]domain.Order, error) {
	return u.orderRepo.GetOrders(ctx)
}

// CancelOrder cancels an existing order
func (u *OrderUsecase) CancelOrder(ctx context.Context, id string) error {
	order, err := u.orderRepo.GetOrder(ctx, id)
	if err != nil {
		return err
	}
	
	if order.Status == "completed" || order.Status == "cancelled" {
		return errors.New("cannot cancel completed or already cancelled order")
	}

	return u.orderRepo.CancelOrder(ctx, id)
}
