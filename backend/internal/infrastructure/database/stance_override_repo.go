package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// StanceOverrideRepo はスタンスオーバーライドの永続化を行う。
type StanceOverrideRepo struct {
	db *sql.DB
}

func NewStanceOverrideRepo(db *sql.DB) *StanceOverrideRepo {
	return &StanceOverrideRepo{db: db}
}

// Save は現在のスタンスオーバーライドを保存する（UPSERT）。
func (r *StanceOverrideRepo) Save(ctx context.Context, record repository.StanceOverrideRecord) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO stance_overrides (id, stance, reasoning, set_at, ttl_sec) VALUES (1, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET stance = excluded.stance, reasoning = excluded.reasoning, set_at = excluded.set_at, ttl_sec = excluded.ttl_sec`,
		record.Stance, record.Reasoning, record.SetAt, record.TTLSec,
	)
	if err != nil {
		return fmt.Errorf("save stance override: %w", err)
	}
	return nil
}

// Load は保存されたスタンスオーバーライドを読み込む。存在しなければ nil を返す。
func (r *StanceOverrideRepo) Load(ctx context.Context) (*repository.StanceOverrideRecord, error) {
	var record repository.StanceOverrideRecord
	err := r.db.QueryRowContext(ctx,
		`SELECT stance, reasoning, set_at, ttl_sec FROM stance_overrides WHERE id = 1`,
	).Scan(&record.Stance, &record.Reasoning, &record.SetAt, &record.TTLSec)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load stance override: %w", err)
	}
	return &record, nil
}

// Delete は保存されたスタンスオーバーライドを削除する。
func (r *StanceOverrideRepo) Delete(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM stance_overrides WHERE id = 1`,
	)
	if err != nil {
		return fmt.Errorf("delete stance override: %w", err)
	}
	return nil
}
