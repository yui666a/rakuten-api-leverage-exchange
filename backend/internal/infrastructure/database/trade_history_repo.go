package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// TradeRecord は取引履歴の1レコード。
type TradeRecord struct {
	ID         int64   `json:"id"`
	SymbolID   int64   `json:"symbolId"`
	OrderID    int64   `json:"orderId"`
	Side       string  `json:"side"`
	Action     string  `json:"action"` // "open" or "close"
	Price      float64 `json:"price"`
	Amount     float64 `json:"amount"`
	Reason     string  `json:"reason"`
	IsStopLoss bool    `json:"isStopLoss"`
	CreatedAt  int64   `json:"createdAt"`
}

// TradeHistoryRepo は取引履歴の永続化を行う。
type TradeHistoryRepo struct {
	db *sql.DB
}

func NewTradeHistoryRepo(db *sql.DB) *TradeHistoryRepo {
	return &TradeHistoryRepo{db: db}
}

func (r *TradeHistoryRepo) Save(ctx context.Context, record TradeRecord) error {
	if record.CreatedAt == 0 {
		record.CreatedAt = time.Now().Unix()
	}
	isStopLoss := 0
	if record.IsStopLoss {
		isStopLoss = 1
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO trade_history (symbol_id, order_id, side, action, price, amount, reason, is_stop_loss, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.SymbolID, record.OrderID, record.Side, record.Action,
		record.Price, record.Amount, record.Reason, isStopLoss, record.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("save trade history: %w", err)
	}
	return nil
}

func (r *TradeHistoryRepo) GetRecent(ctx context.Context, symbolID int64, limit int) ([]TradeRecord, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, symbol_id, order_id, side, action, price, amount, reason, is_stop_loss, created_at
		 FROM trade_history
		 WHERE symbol_id = ?
		 ORDER BY created_at DESC LIMIT ?`,
		symbolID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query trade history: %w", err)
	}
	defer rows.Close()

	var records []TradeRecord
	for rows.Next() {
		var rec TradeRecord
		var isStopLoss int
		if err := rows.Scan(&rec.ID, &rec.SymbolID, &rec.OrderID, &rec.Side, &rec.Action,
			&rec.Price, &rec.Amount, &rec.Reason, &isStopLoss, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan trade history: %w", err)
		}
		rec.IsStopLoss = isStopLoss == 1
		records = append(records, rec)
	}
	return records, rows.Err()
}
