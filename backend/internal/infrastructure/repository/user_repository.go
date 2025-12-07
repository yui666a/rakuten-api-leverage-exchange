package repository

import (
	"context"
	"errors"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain"
)

// UserRepositoryImpl implements domain.UserRepository
type UserRepositoryImpl struct {
	// Add database client here (e.g., *sql.DB, *gorm.DB)
}

// NewUserRepository creates a new UserRepositoryImpl
func NewUserRepository() *UserRepositoryImpl {
	return &UserRepositoryImpl{}
}

// FindByID finds a user by ID
func (r *UserRepositoryImpl) FindByID(ctx context.Context, id string) (*domain.User, error) {
	// TODO: Implement actual database query
	return nil, errors.New("not implemented")
}

// Create creates a new user
func (r *UserRepositoryImpl) Create(ctx context.Context, user *domain.User) error {
	// TODO: Implement actual database insert
	return errors.New("not implemented")
}
