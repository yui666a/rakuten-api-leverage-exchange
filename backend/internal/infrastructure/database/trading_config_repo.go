package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// TradingConfigRepo persists pipeline.symbolID + tradeAmount across restarts.
type TradingConfigRepo struct {
	db *sql.DB
}

func NewTradingConfigRepo(db *sql.DB) *TradingConfigRepo {
	return &TradingConfigRepo{db: db}
}

func (r *TradingConfigRepo) Save(ctx context.Context, state repository.TradingConfigState) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO trading_config (id, symbol_id, trade_amount, updated_at) VALUES (1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET symbol_id = excluded.symbol_id, trade_amount = excluded.trade_amount, updated_at = excluded.updated_at`,
		state.SymbolID, state.TradeAmount, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("save trading config: %w", err)
	}
	return nil
}

func (r *TradingConfigRepo) Load(ctx context.Context) (*repository.TradingConfigState, error) {
	var state repository.TradingConfigState
	err := r.db.QueryRowContext(ctx,
		`SELECT symbol_id, trade_amount, updated_at FROM trading_config WHERE id = 1`,
	).Scan(&state.SymbolID, &state.TradeAmount, &state.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load trading config: %w", err)
	}
	return &state, nil
}
