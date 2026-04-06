package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// RiskStateRepo はリスク管理状態の永続化を行う。
type RiskStateRepo struct {
	db *sql.DB
}

func NewRiskStateRepo(db *sql.DB) *RiskStateRepo {
	return &RiskStateRepo{db: db}
}

// Save は現在のリスク状態を保存する（UPSERT）。
func (r *RiskStateRepo) Save(ctx context.Context, state repository.RiskState) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO risk_state (id, daily_loss, balance, updated_at) VALUES (1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET daily_loss = excluded.daily_loss, balance = excluded.balance, updated_at = excluded.updated_at`,
		state.DailyLoss, state.Balance, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("save risk state: %w", err)
	}
	return nil
}

// Load は保存されたリスク状態を読み込む。存在しなければ nil を返す。
func (r *RiskStateRepo) Load(ctx context.Context) (*repository.RiskState, error) {
	var state repository.RiskState
	err := r.db.QueryRowContext(ctx,
		`SELECT daily_loss, balance, updated_at FROM risk_state WHERE id = 1`,
	).Scan(&state.DailyLoss, &state.Balance, &state.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load risk state: %w", err)
	}
	return &state, nil
}
