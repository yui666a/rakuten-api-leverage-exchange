package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/domain"
)

// InMemoryOrderRepository is an in-memory implementation of OrderRepository
type InMemoryOrderRepository struct {
	orders map[string]*domain.Order
	mu     sync.RWMutex
}

// NewInMemoryOrderRepository creates a new in-memory order repository
func NewInMemoryOrderRepository() *InMemoryOrderRepository {
	return &InMemoryOrderRepository{
		orders: make(map[string]*domain.Order),
	}
}

// CreateOrder stores a new order
func (r *InMemoryOrderRepository) CreateOrder(ctx context.Context, order *domain.Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.orders[order.ID] = order
	return nil
}

// GetOrder retrieves an order by ID
func (r *InMemoryOrderRepository) GetOrder(ctx context.Context, id string) (*domain.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	order, exists := r.orders[id]
	if !exists {
		return nil, errors.New("order not found")
	}

	return order, nil
}

// GetOrders retrieves all orders
func (r *InMemoryOrderRepository) GetOrders(ctx context.Context) ([]domain.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	orders := make([]domain.Order, 0, len(r.orders))
	for _, order := range r.orders {
		orders = append(orders, *order)
	}

	return orders, nil
}

// CancelOrder cancels an order
func (r *InMemoryOrderRepository) CancelOrder(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	order, exists := r.orders[id]
	if !exists {
		return errors.New("order not found")
	}

	order.Status = "cancelled"
	order.UpdatedAt = time.Now()
	return nil
}
