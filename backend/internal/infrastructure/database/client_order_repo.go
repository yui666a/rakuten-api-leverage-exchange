package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// ClientOrderRepo はクライアント注文の冪等性キーの永続化を行う。
type ClientOrderRepo struct {
	db *sql.DB
}

func NewClientOrderRepo(db *sql.DB) *ClientOrderRepo {
	return &ClientOrderRepo{db: db}
}

// Find はクライアント注文IDで検索し、存在しなければ nil を返す。
func (r *ClientOrderRepo) Find(ctx context.Context, clientOrderID string) (*repository.ClientOrderRecord, error) {
	var rec repository.ClientOrderRecord
	err := r.db.QueryRowContext(ctx,
		`SELECT client_order_id, executed, order_id, created_at FROM client_orders WHERE client_order_id = ?`,
		clientOrderID,
	).Scan(&rec.ClientOrderID, &rec.Executed, &rec.OrderID, &rec.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find client order: %w", err)
	}
	return &rec, nil
}

// Save はクライアント注文レコードを保存する。
func (r *ClientOrderRepo) Save(ctx context.Context, record repository.ClientOrderRecord) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO client_orders (client_order_id, executed, order_id, created_at) VALUES (?, ?, ?, ?)`,
		record.ClientOrderID, record.Executed, record.OrderID, record.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("save client order: %w", err)
	}
	return nil
}

// DeleteExpired は指定した時刻より前に作成されたレコードを削除する。
func (r *ClientOrderRepo) DeleteExpired(ctx context.Context, beforeUnix int64) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM client_orders WHERE created_at < ?`,
		beforeUnix,
	)
	if err != nil {
		return fmt.Errorf("delete expired client orders: %w", err)
	}
	return nil
}
