package backtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// MultiPeriodResultRepository persists the multi-period envelope into
// multi_period_results. Per-period BacktestResult rows are NOT saved here;
// callers are expected to save them via ResultRepository first and pass
// their IDs as part of the MultiPeriodResult.Periods slice so FindByID can
// rehydrate via a subsequent lookup through BacktestResultRepository.
type MultiPeriodResultRepository struct {
	db     *sql.DB
	btRepo repository.BacktestResultRepository
}

// NewMultiPeriodResultRepository takes the same db handle and the
// BacktestResultRepository used for per-period rows so that FindByID can
// rehydrate the full Periods slice from their IDs.
func NewMultiPeriodResultRepository(db *sql.DB, btRepo repository.BacktestResultRepository) *MultiPeriodResultRepository {
	return &MultiPeriodResultRepository{db: db, btRepo: btRepo}
}

// multiPeriodRow is the on-disk shape that scanMultiPeriodRow reads.
// period_result_ids is a JSON array of (label, resultId) pairs so labels
// can be restored without a separate table.
type multiPeriodRow struct {
	ID              string
	CreatedAt       int64
	ProfileName     string
	PDCACycleID     string
	Hypothesis      string
	ParentResultID  sql.NullString
	AggregateJSON   string
	PeriodResultIDs string
}

type periodRefEntry struct {
	Label    string `json:"label"`
	ResultID string `json:"resultId"`
}

func (r *MultiPeriodResultRepository) Save(ctx context.Context, result entity.MultiPeriodResult) error {
	// Serialise aggregate + period refs. Period refs intentionally store
	// only (label, resultId) to keep this table cheap; full period content
	// is rehydrated on read via btRepo.FindByID.
	aggJSON, err := json.Marshal(result.Aggregate)
	if err != nil {
		return fmt.Errorf("marshal aggregate: %w", err)
	}
	refs := make([]periodRefEntry, 0, len(result.Periods))
	for _, p := range result.Periods {
		refs = append(refs, periodRefEntry{Label: p.Label, ResultID: p.Result.ID})
	}
	refsJSON, err := json.Marshal(refs)
	if err != nil {
		return fmt.Errorf("marshal period refs: %w", err)
	}

	parentID := sql.NullString{}
	if result.ParentResultID != nil {
		parentID = sql.NullString{String: *result.ParentResultID, Valid: true}
	}

	_, err = r.db.ExecContext(ctx, `INSERT INTO multi_period_results (
		id, created_at, profile_name, pdca_cycle_id, hypothesis, parent_result_id,
		aggregate_json, period_result_ids
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		result.ID,
		result.CreatedAt,
		result.ProfileName,
		result.PDCACycleID,
		result.Hypothesis,
		parentID,
		string(aggJSON),
		string(refsJSON),
	)
	if err != nil {
		return fmt.Errorf("insert multi_period_results: %w", err)
	}
	return nil
}

func (r *MultiPeriodResultRepository) List(ctx context.Context, filter repository.MultiPeriodResultFilter) ([]entity.MultiPeriodResult, error) {
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
	if filter.ProfileName != "" {
		clauses = append(clauses, "profile_name = ?")
		args = append(args, filter.ProfileName)
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

	query := `SELECT id, created_at, profile_name, pdca_cycle_id, hypothesis,
		parent_result_id, aggregate_json, period_result_ids
		FROM multi_period_results` + where + `
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list multi_period_results: %w", err)
	}
	defer rows.Close()

	var results []entity.MultiPeriodResult
	for rows.Next() {
		row, err := scanMultiPeriodRow(rows)
		if err != nil {
			return nil, err
		}
		// For list view we don't rehydrate full per-period bodies — the
		// envelope (aggregate + refs) is enough. Callers that need
		// per-period detail call FindByID.
		mp, err := buildEnvelopeOnly(row)
		if err != nil {
			return nil, err
		}
		results = append(results, mp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate multi_period_results: %w", err)
	}
	return results, nil
}

func (r *MultiPeriodResultRepository) FindByID(ctx context.Context, id string) (*entity.MultiPeriodResult, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, created_at, profile_name,
		pdca_cycle_id, hypothesis, parent_result_id, aggregate_json, period_result_ids
		FROM multi_period_results WHERE id = ?`, id)

	raw, err := scanMultiPeriodRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	mp, err := buildEnvelopeOnly(raw)
	if err != nil {
		return nil, err
	}

	// Rehydrate per-period results via BacktestResultRepository. If the
	// referenced row has since been deleted, we keep the label and leave
	// Result blank so consumers can still see the list of period labels.
	var refs []periodRefEntry
	if err := json.Unmarshal([]byte(raw.PeriodResultIDs), &refs); err != nil {
		return nil, fmt.Errorf("unmarshal period refs: %w", err)
	}
	mp.Periods = make([]entity.LabeledBacktestResult, 0, len(refs))
	for _, ref := range refs {
		labeled := entity.LabeledBacktestResult{Label: ref.Label}
		if r.btRepo != nil && ref.ResultID != "" {
			bt, err := r.btRepo.FindByID(ctx, ref.ResultID)
			if err != nil {
				return nil, fmt.Errorf("rehydrate period %s: %w", ref.Label, err)
			}
			if bt != nil {
				labeled.Result = *bt
			}
		}
		mp.Periods = append(mp.Periods, labeled)
	}

	return &mp, nil
}

type scanTarget interface {
	Scan(dest ...any) error
}

func scanMultiPeriodRow(s scanTarget) (*multiPeriodRow, error) {
	var row multiPeriodRow
	err := s.Scan(
		&row.ID,
		&row.CreatedAt,
		&row.ProfileName,
		&row.PDCACycleID,
		&row.Hypothesis,
		&row.ParentResultID,
		&row.AggregateJSON,
		&row.PeriodResultIDs,
	)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func buildEnvelopeOnly(row *multiPeriodRow) (entity.MultiPeriodResult, error) {
	var agg entity.MultiPeriodAggregate
	if err := json.Unmarshal([]byte(row.AggregateJSON), &agg); err != nil {
		return entity.MultiPeriodResult{}, fmt.Errorf("unmarshal aggregate: %w", err)
	}
	mp := entity.MultiPeriodResult{
		ID:          row.ID,
		CreatedAt:   row.CreatedAt,
		ProfileName: row.ProfileName,
		PDCACycleID: row.PDCACycleID,
		Hypothesis:  row.Hypothesis,
		Aggregate:   agg,
	}
	if row.ParentResultID.Valid {
		v := row.ParentResultID.String
		mp.ParentResultID = &v
	}
	return mp, nil
}

// compile-time assertion so interface drift is caught at build time.
var _ repository.MultiPeriodResultRepository = (*MultiPeriodResultRepository)(nil)
