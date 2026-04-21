package entity

// WalkForwardPersisted is the database-facing envelope for a walk-forward
// run. The full per-window result payload is serialised to JSON upstream
// (usecase/backtest owns the struct, so the domain layer does not pull
// in that dependency); only the metadata the repository filters on
// surfaces here as structured fields.
//
// Fields:
//   - ID:           same UUID the runner put into WalkForwardResult.ID.
//   - CreatedAt:    unix seconds, set by the runner.
//   - BaseProfile:  profile name at the time of the run.
//   - Objective:    "return" | "sharpe" | "profit_factor" | "".
//   - PDCACycleID / Hypothesis / ParentResultID: PDCA metadata echoing the
//     request body (same three columns as multi_period_results).
//   - RequestJSON:  the full request body as received by the handler so a
//     saved row can be replayed byte-for-byte.
//   - ResultJSON:   the full WalkForwardResult envelope as returned by the
//     runner (includes per-window BacktestResult bodies verbatim).
//   - AggregateOOSJSON: denormalised copy of ResultJSON's aggregateOOS
//     field so list views can rank runs by RobustnessScore without parsing
//     the potentially-large ResultJSON.
type WalkForwardPersisted struct {
	ID             string
	CreatedAt      int64
	BaseProfile    string
	Objective      string
	PDCACycleID    string
	Hypothesis     string
	ParentResultID *string

	RequestJSON      string
	ResultJSON       string
	AggregateOOSJSON string
}
