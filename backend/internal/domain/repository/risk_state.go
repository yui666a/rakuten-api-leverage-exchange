package repository

import "context"

// RiskState は永続化されたリスク管理状態。
type RiskState struct {
	DailyLoss float64 `json:"dailyLoss"`
	Balance   float64 `json:"balance"`
	UpdatedAt int64   `json:"updatedAt"`
}

// RiskStateRepository はリスク管理状態の永続化インターフェース。
type RiskStateRepository interface {
	Save(ctx context.Context, state RiskState) error
	Load(ctx context.Context) (*RiskState, error)
}
