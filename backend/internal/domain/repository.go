package domain

import "context"

// UserRepository defines the interface for user data access
type UserRepository interface {
	FindByID(ctx context.Context, id string) (*User, error)
	Create(ctx context.Context, user *User) error
}
