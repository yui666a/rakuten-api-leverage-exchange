package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/domain"
)

// InMemoryAccountRepository is an in-memory implementation of AccountRepository
type InMemoryAccountRepository struct {
	accounts map[string]*domain.Account
	mu       sync.RWMutex
}

// NewInMemoryAccountRepository creates a new in-memory account repository
func NewInMemoryAccountRepository() *InMemoryAccountRepository {
	repo := &InMemoryAccountRepository{
		accounts: make(map[string]*domain.Account),
	}
	
	// Initialize with a default account for demonstration
	repo.accounts["default"] = &domain.Account{
		ID:          "default",
		Balance:     1000000.0,
		Currency:    "JPY",
		LockedFunds: 0,
		UpdatedAt:   time.Now(),
	}
	
	return repo
}

// GetAccount retrieves an account by ID
func (r *InMemoryAccountRepository) GetAccount(ctx context.Context, id string) (*domain.Account, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	account, exists := r.accounts[id]
	if !exists {
		return nil, errors.New("account not found")
	}

	return account, nil
}

// UpdateBalance updates the account balance
func (r *InMemoryAccountRepository) UpdateBalance(ctx context.Context, id string, balance float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	account, exists := r.accounts[id]
	if !exists {
		return errors.New("account not found")
	}

	account.Balance = balance
	account.UpdatedAt = time.Now()
	return nil
}
