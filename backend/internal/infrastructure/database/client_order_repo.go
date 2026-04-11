package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// ClientOrderRepo はクライアント注文の冪等性キーとライフサイクルの永続化を行う。
type ClientOrderRepo struct {
	db *sql.DB
}

func NewClientOrderRepo(db *sql.DB) *ClientOrderRepo {
	return &ClientOrderRepo{db: db}
}

const clientOrderColumns = `
	client_order_id, executed, order_id, created_at,
	status, symbol_id, intent, side, amount, position_id,
	raw_response, error_message, updated_at`

func scanClientOrder(scanner interface {
	Scan(dest ...any) error
}) (*repository.ClientOrderRecord, error) {
	var (
		rec      repository.ClientOrderRecord
		statusS  string
		intentS  string
		sideS    string
		executed int64
	)
	err := scanner.Scan(
		&rec.ClientOrderID,
		&executed,
		&rec.OrderID,
		&rec.CreatedAt,
		&statusS,
		&rec.SymbolID,
		&intentS,
		&sideS,
		&rec.Amount,
		&rec.PositionID,
		&rec.RawResponse,
		&rec.ErrorMessage,
		&rec.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	rec.Status = entity.ClientOrderStatus(statusS)
	rec.Intent = entity.ClientOrderIntent(intentS)
	rec.Side = entity.OrderSide(sideS)
	rec.Executed = executed != 0
	return &rec, nil
}

// Find はクライアント注文IDで検索し、存在しなければ nil を返す。
func (r *ClientOrderRepo) Find(ctx context.Context, clientOrderID string) (*repository.ClientOrderRecord, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+clientOrderColumns+` FROM client_orders WHERE client_order_id = ?`,
		clientOrderID,
	)
	rec, err := scanClientOrder(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find client order: %w", err)
	}
	return rec, nil
}

// Save はクライアント注文レコードを保存 (UPSERT) する。
// 後方互換のために残す。新規コードは InsertOrGet + UpdateStatus を使うこと。
func (r *ClientOrderRepo) Save(ctx context.Context, record repository.ClientOrderRecord) error {
	if record.Status == "" {
		// 旧呼び出し元 (Find→Save パターン) からの保存。
		// executed フラグから既存ステータスにマップする。
		if record.Executed {
			record.Status = entity.ClientOrderStatusCompleted
		} else {
			record.Status = entity.ClientOrderStatusFailed
		}
	}
	if record.UpdatedAt == 0 {
		record.UpdatedAt = record.CreatedAt
	}
	executed := int64(0)
	if record.Executed {
		executed = 1
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO client_orders (
			client_order_id, executed, order_id, created_at,
			status, symbol_id, intent, side, amount, position_id,
			raw_response, error_message, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(client_order_id) DO UPDATE SET
			executed=excluded.executed,
			order_id=excluded.order_id,
			status=excluded.status,
			symbol_id=excluded.symbol_id,
			intent=excluded.intent,
			side=excluded.side,
			amount=excluded.amount,
			position_id=excluded.position_id,
			raw_response=excluded.raw_response,
			error_message=excluded.error_message,
			updated_at=excluded.updated_at`,
		record.ClientOrderID, executed, record.OrderID, record.CreatedAt,
		string(record.Status), record.SymbolID, string(record.Intent), string(record.Side),
		record.Amount, record.PositionID, record.RawResponse, record.ErrorMessage, record.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("save client order: %w", err)
	}
	return nil
}

// InsertOrGet は INSERT OR IGNORE で record を挿入する。既に存在していた場合は
// 既存行を読み直して existing に返し、inserted=false を返す。
func (r *ClientOrderRepo) InsertOrGet(ctx context.Context, record repository.ClientOrderRecord) (*repository.ClientOrderRecord, bool, error) {
	if record.Status == "" {
		record.Status = entity.ClientOrderStatusPending
	}
	if record.UpdatedAt == 0 {
		record.UpdatedAt = record.CreatedAt
	}
	executed := int64(0)
	if record.Executed {
		executed = 1
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO client_orders (
			client_order_id, executed, order_id, created_at,
			status, symbol_id, intent, side, amount, position_id,
			raw_response, error_message, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ClientOrderID, executed, record.OrderID, record.CreatedAt,
		string(record.Status), record.SymbolID, string(record.Intent), string(record.Side),
		record.Amount, record.PositionID, record.RawResponse, record.ErrorMessage, record.UpdatedAt,
	)
	if err != nil {
		return nil, false, fmt.Errorf("insert or get client order: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, false, fmt.Errorf("rows affected: %w", err)
	}
	if affected == 1 {
		// 新規挿入。引数を返すと呼び出し側のローカル変更で食い違うため、DB から読み直す。
		stored, err := r.Find(ctx, record.ClientOrderID)
		if err != nil {
			return nil, true, err
		}
		return stored, true, nil
	}
	existing, err := r.Find(ctx, record.ClientOrderID)
	if err != nil {
		return nil, false, err
	}
	if existing == nil {
		return nil, false, fmt.Errorf("insert or get: row vanished after conflict for %s", record.ClientOrderID)
	}
	return existing, false, nil
}

// UpdateStatus は status と任意フィールドを差分更新する。
func (r *ClientOrderRepo) UpdateStatus(
	ctx context.Context,
	clientOrderID string,
	status entity.ClientOrderStatus,
	now int64,
	update repository.ClientOrderUpdate,
) error {
	sets := []string{"status = ?", "updated_at = ?"}
	args := []any{string(status), now}

	executed := isExecutedStatus(status)
	sets = append(sets, "executed = ?")
	args = append(args, boolToInt(executed))

	if update.OrderID != nil {
		sets = append(sets, "order_id = ?")
		args = append(args, *update.OrderID)
	}
	if update.RawResponse != nil {
		sets = append(sets, "raw_response = ?")
		args = append(args, *update.RawResponse)
	}
	if update.ErrorMessage != nil {
		sets = append(sets, "error_message = ?")
		args = append(args, *update.ErrorMessage)
	}

	args = append(args, clientOrderID)
	stmt := `UPDATE client_orders SET ` + strings.Join(sets, ", ") + ` WHERE client_order_id = ?`
	res, err := r.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return fmt.Errorf("update client order status: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("update client order status: no row for %s", clientOrderID)
	}
	return nil
}

// ListByStatus は指定 status のレコードを updated_at 昇順で最大 limit 件返す。
func (r *ClientOrderRepo) ListByStatus(
	ctx context.Context,
	statuses []entity.ClientOrderStatus,
	limit int,
) ([]repository.ClientOrderRecord, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	placeholders := make([]string, len(statuses))
	args := make([]any, 0, len(statuses)+1)
	for i, s := range statuses {
		placeholders[i] = "?"
		args = append(args, string(s))
	}
	args = append(args, limit)

	stmt := `SELECT ` + clientOrderColumns + ` FROM client_orders
		WHERE status IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY updated_at ASC
		LIMIT ?`
	rows, err := r.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("list client orders by status: %w", err)
	}
	defer rows.Close()

	var out []repository.ClientOrderRecord
	for rows.Next() {
		rec, err := scanClientOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("scan client order: %w", err)
		}
		out = append(out, *rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate client orders: %w", err)
	}
	return out, nil
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

func isExecutedStatus(s entity.ClientOrderStatus) bool {
	switch s {
	case entity.ClientOrderStatusConfirmed,
		entity.ClientOrderStatusCompleted,
		entity.ClientOrderStatusReconciledConfirmed:
		return true
	default:
		return false
	}
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
