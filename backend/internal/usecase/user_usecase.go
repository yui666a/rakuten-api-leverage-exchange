package usecase

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain"
)

// UserUsecase handles business logic for users
type UserUsecase struct {
	userRepo domain.UserRepository
}

// NewUserUsecase creates a new UserUsecase
func NewUserUsecase(userRepo domain.UserRepository) *UserUsecase {
	return &UserUsecase{
		userRepo: userRepo,
	}
}

// GetUser retrieves a user by ID
func (u *UserUsecase) GetUser(ctx context.Context, id string) (*domain.User, error) {
	return u.userRepo.FindByID(ctx, id)
}

// CreateUser creates a new user
func (u *UserUsecase) CreateUser(ctx context.Context, user *domain.User) error {
	return u.userRepo.Create(ctx, user)
}
