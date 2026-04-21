package backtest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// WalkForwardResultRepository persists a WalkForwardResult envelope into
// walk_forward_results. Mirror of MultiPeriodResultRepository in shape,
// but the per-window BacktestResult bodies are stored inline (inside
// result_json) because the walk-forward handler does not write per-window
// rows into backtest_results — the envelope is self-contained.
type WalkForwardResultRepository struct {
	db *sql.DB
}

func NewWalkForwardResultRepository(db *sql.DB) *WalkForwardResultRepository {
	return &WalkForwardResultRepository{db: db}
}

func (r *WalkForwardResultRepository) Save(ctx context.Context, rec entity.WalkForwardPersisted) error {
	if rec.ID == "" {
		return fmt.Errorf("walk_forward_results: id must not be empty")
	}
	if rec.ResultJSON == "" || rec.AggregateOOSJSON == "" {
		return fmt.Errorf("walk_forward_results: result_json and aggregate_oos_json are required")
	}
	if rec.RequestJSON == "" {
		// Keep the column NOT NULL but empty JSON object is fine for
		// CLI-initiated runs that don't have a literal HTTP body.
		rec.RequestJSON = "{}"
	}
	parentID := sql.NullString{}
	if rec.ParentResultID != nil {
		parentID = sql.NullString{String: *rec.ParentResultID, Valid: true}
	}

	_, err := r.db.ExecContext(ctx, `INSERT INTO walk_forward_results (
		id, created_at, base_profile, pdca_cycle_id, hypothesis, objective,
		parent_result_id, request_json, result_json, aggregate_oos_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID,
		rec.CreatedAt,
		rec.BaseProfile,
		rec.PDCACycleID,
		rec.Hypothesis,
		rec.Objective,
		parentID,
		rec.RequestJSON,
		rec.ResultJSON,
		rec.AggregateOOSJSON,
	)
	if err != nil {
		return fmt.Errorf("insert walk_forward_results: %w", err)
	}
	return nil
}

func (r *WalkForwardResultRepository) List(ctx context.Context, filter repository.WalkForwardResultFilter) ([]entity.WalkForwardPersisted, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	var clauses []string
	var args []any
	if filter.BaseProfile != "" {
		clauses = append(clauses, "base_profile = ?")
		args = append(args, filter.BaseProfile)
	}
	if filter.PDCACycleID != "" {
		clauses = append(clauses, "pdca_cycle_id = ?")
		args = append(args, filter.PDCACycleID)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit, offset)

	// List intentionally omits result_json (potentially large) — callers
	// that need the full envelope should issue FindByID.
	query := `SELECT id, created_at, base_profile, pdca_cycle_id, hypothesis,
		objective, parent_result_id, request_json, aggregate_oos_json
		FROM walk_forward_results` + where + `
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list walk_forward_results: %w", err)
	}
	defer rows.Close()

	var out []entity.WalkForwardPersisted
	for rows.Next() {
		var (
			rec      entity.WalkForwardPersisted
			parentID sql.NullString
		)
		if err := rows.Scan(
			&rec.ID, &rec.CreatedAt, &rec.BaseProfile, &rec.PDCACycleID,
			&rec.Hypothesis, &rec.Objective, &parentID,
			&rec.RequestJSON, &rec.AggregateOOSJSON,
		); err != nil {
			return nil, fmt.Errorf("scan walk_forward row: %w", err)
		}
		if parentID.Valid {
			v := parentID.String
			rec.ParentResultID = &v
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate walk_forward_results: %w", err)
	}
	return out, nil
}

func (r *WalkForwardResultRepository) FindByID(ctx context.Context, id string) (*entity.WalkForwardPersisted, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, created_at, base_profile,
		pdca_cycle_id, hypothesis, objective, parent_result_id,
		request_json, result_json, aggregate_oos_json
		FROM walk_forward_results WHERE id = ?`, id)

	var (
		rec      entity.WalkForwardPersisted
		parentID sql.NullString
	)
	err := row.Scan(
		&rec.ID, &rec.CreatedAt, &rec.BaseProfile, &rec.PDCACycleID,
		&rec.Hypothesis, &rec.Objective, &parentID,
		&rec.RequestJSON, &rec.ResultJSON, &rec.AggregateOOSJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan walk_forward row: %w", err)
	}
	if parentID.Valid {
		v := parentID.String
		rec.ParentResultID = &v
	}
	return &rec, nil
}

// Compile-time assertion.
var _ repository.WalkForwardResultRepository = (*WalkForwardResultRepository)(nil)
