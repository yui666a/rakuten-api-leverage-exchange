// Package decisionlog persists every pipeline decision (BUY/SELL/HOLD plus
// the reasons each gate produced) into SQLite. Recorder is an EventBus
// subscriber registered at priority 99 so it runs after all primary
// handlers; it never blocks or modifies the pipeline.
package decisionlog

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// RecorderConfig binds the recorder to one pipeline instance.
//
// StanceProvider is called every IndicatorEvent to snapshot the pipeline's
// current stance. A nil provider falls back to "UNKNOWN" — useful for tests
// and for the wiring step before pipeline integration lands.
type RecorderConfig struct {
	SymbolID        int64
	CurrencyPair    string
	PrimaryInterval string
	StanceProvider  func() string
}

// Recorder observes EventBus events and writes one DecisionRecord per
// completed cycle.
//
// Flush model: every IndicatorEvent immediately INSERTs a row with HOLD /
// SKIPPED / NOOP defaults. Subsequent SignalEvent / ApprovedSignalEvent /
// RejectedSignalEvent / OrderEvent within the same bar UPDATE that row in
// place. The trade-off vs. the legacy "wait for next IndicatorEvent to
// flush" model is one extra UPDATE per active bar, in exchange for: (1)
// HOLD bars become visible immediately (no 15-min delay), (2) a daemon
// crash mid-bar still leaves the BAR_CLOSE row persisted.
//
// Recorder is NOT goroutine-safe; one Recorder must be bound to exactly
// one EventBus chain (the EventBus dispatch loop is single-threaded per
// chain so this matches the runtime invariant).
type Recorder struct {
	repo   repository.DecisionLogRepository
	cfg    RecorderConfig
	nowFn  func() time.Time
	logger *slog.Logger

	// pendingRec carries the most recent BAR_CLOSE row for in-place UPDATE
	// as later events arrive. ID == 0 means "no row yet for the current
	// bar" (e.g. immediately after construction or after an Insert error).
	pendingRec        entity.DecisionRecord
	hasPending        bool
	nextSequenceInBar int
	lastIndicatorJSON string
	lastHigherTFJSON  string
}

func NewRecorder(repo repository.DecisionLogRepository, cfg RecorderConfig) *Recorder {
	return &Recorder{
		repo:   repo,
		cfg:    cfg,
		nowFn:  time.Now,
		logger: slog.Default(),
	}
}

// SetClock overrides the timestamp source. Tests use this to make CreatedAt
// deterministic; production never calls it.
func (r *Recorder) SetClock(fn func() time.Time) { r.nowFn = fn }

// Handle implements eventengine.EventHandler. It returns nil chained events
// so the bus stays unaffected by recorder activity.
func (r *Recorder) Handle(ctx context.Context, event entity.Event) ([]entity.Event, error) {
	switch ev := event.(type) {
	case entity.IndicatorEvent:
		r.onIndicator(ctx, ev)
	case entity.SignalEvent:
		r.onSignal(ctx, ev)
	case entity.ApprovedSignalEvent:
		r.onApproved(ctx)
	case entity.RejectedSignalEvent:
		r.onRejected(ctx, ev)
	case entity.OrderEvent:
		r.onOrder(ctx, ev)
	}
	return nil, nil
}

func (r *Recorder) stance() string {
	if r.cfg.StanceProvider == nil {
		return "UNKNOWN"
	}
	return r.cfg.StanceProvider()
}

func (r *Recorder) onIndicator(ctx context.Context, ev entity.IndicatorEvent) {
	// New bar: discard the previous bar's pending state (it's already
	// persisted with whatever final values we knew) and start fresh.
	r.hasPending = false
	r.nextSequenceInBar = 1

	indicatorsJSON, err := json.Marshal(ev.Primary)
	if err != nil {
		r.logger.Warn("decisionlog: marshal indicators failed", "error", err)
		indicatorsJSON = []byte("{}")
	}
	var higherJSON []byte
	if ev.HigherTF != nil {
		higherJSON, err = json.Marshal(ev.HigherTF)
		if err != nil {
			r.logger.Warn("decisionlog: marshal higher-tf indicators failed", "error", err)
			higherJSON = []byte("{}")
		}
	} else {
		higherJSON = []byte("{}")
	}
	r.lastIndicatorJSON = string(indicatorsJSON)
	r.lastHigherTFJSON = string(higherJSON)

	rec := entity.DecisionRecord{
		BarCloseAt:             ev.Timestamp,
		SequenceInBar:          0,
		TriggerKind:            entity.DecisionTriggerBarClose,
		SymbolID:               r.cfg.SymbolID,
		CurrencyPair:           r.cfg.CurrencyPair,
		PrimaryInterval:        r.cfg.PrimaryInterval,
		Stance:                 r.stance(),
		LastPrice:              ev.LastPrice,
		SignalAction:           string(entity.SignalActionHold),
		RiskOutcome:            entity.DecisionRiskSkipped,
		BookGateOutcome:        entity.DecisionBookSkipped,
		OrderOutcome:           entity.DecisionOrderNoop,
		IndicatorsJSON:         r.lastIndicatorJSON,
		HigherTFIndicatorsJSON: r.lastHigherTFJSON,
		CreatedAt:              r.nowFn().UnixMilli(),
	}
	id, err := r.repo.InsertAndID(ctx, rec)
	if err != nil {
		r.logger.Warn("decisionlog: bar-close insert failed", "error", err, "barCloseAt", ev.Timestamp)
		return
	}
	rec.ID = id
	r.pendingRec = rec
	r.hasPending = true
}

func (r *Recorder) onSignal(ctx context.Context, ev entity.SignalEvent) {
	if !r.hasPending {
		return
	}
	r.pendingRec.SignalAction = string(ev.Signal.Action)
	r.pendingRec.SignalConfidence = ev.Signal.Confidence
	r.pendingRec.SignalReason = ev.Signal.Reason
	r.persistPending(ctx, "signal")
}

func (r *Recorder) onApproved(ctx context.Context) {
	if !r.hasPending {
		return
	}
	r.pendingRec.RiskOutcome = entity.DecisionRiskApproved
	r.pendingRec.BookGateOutcome = entity.DecisionBookAllowed
	r.persistPending(ctx, "approved")
}

func (r *Recorder) onRejected(ctx context.Context, ev entity.RejectedSignalEvent) {
	if !r.hasPending {
		return
	}
	switch ev.Stage {
	case entity.RejectedStageRisk:
		r.pendingRec.RiskOutcome = entity.DecisionRiskRejected
		r.pendingRec.RiskReason = ev.Reason
	case entity.RejectedStageBookGate:
		r.pendingRec.RiskOutcome = entity.DecisionRiskApproved
		r.pendingRec.BookGateOutcome = entity.DecisionBookVetoed
		r.pendingRec.BookGateReason = ev.Reason
	}
	r.persistPending(ctx, "rejected")
}

func (r *Recorder) onOrder(ctx context.Context, ev entity.OrderEvent) {
	switch ev.Trigger {
	case entity.DecisionTriggerTickSLTP, entity.DecisionTriggerTickTrailing:
		r.persistTickOrder(ctx, ev)
	default:
		r.applyBarOrder(ctx, ev)
	}
}

func (r *Recorder) applyBarOrder(ctx context.Context, ev entity.OrderEvent) {
	if !r.hasPending {
		return
	}
	if ev.OrderID > 0 {
		r.pendingRec.OrderOutcome = entity.DecisionOrderFilled
	} else {
		r.pendingRec.OrderOutcome = entity.DecisionOrderFailed
	}
	r.pendingRec.OrderID = ev.OrderID
	r.pendingRec.ExecutedAmount = ev.Amount
	r.pendingRec.ExecutedPrice = ev.Price
	r.pendingRec.OpenedPositionID = ev.OpenedPositionID
	r.pendingRec.ClosedPositionID = ev.ClosedPositionID
	r.persistPending(ctx, "order")
}

func (r *Recorder) persistTickOrder(ctx context.Context, ev entity.OrderEvent) {
	rec := entity.DecisionRecord{
		BarCloseAt:             ev.Timestamp,
		SequenceInBar:          r.nextSequenceInBar,
		TriggerKind:            ev.Trigger,
		SymbolID:               r.cfg.SymbolID,
		CurrencyPair:           r.cfg.CurrencyPair,
		PrimaryInterval:        r.cfg.PrimaryInterval,
		Stance:                 r.stance(),
		LastPrice:              ev.Price,
		SignalAction:           string(entity.SignalActionHold),
		SignalReason:           ev.Reason,
		RiskOutcome:            entity.DecisionRiskSkipped,
		BookGateOutcome:        entity.DecisionBookSkipped,
		OrderOutcome:           entity.DecisionOrderFilled,
		OrderID:                ev.OrderID,
		ExecutedAmount:         ev.Amount,
		ExecutedPrice:          ev.Price,
		ClosedPositionID:       ev.ClosedPositionID,
		OpenedPositionID:       ev.OpenedPositionID,
		IndicatorsJSON:         r.lastIndicatorJSON,
		HigherTFIndicatorsJSON: r.lastHigherTFJSON,
		CreatedAt:              r.nowFn().UnixMilli(),
	}
	if rec.IndicatorsJSON == "" {
		rec.IndicatorsJSON = "{}"
	}
	if rec.HigherTFIndicatorsJSON == "" {
		rec.HigherTFIndicatorsJSON = "{}"
	}
	if ev.OrderID == 0 {
		rec.OrderOutcome = entity.DecisionOrderFailed
	}
	if err := r.repo.Insert(ctx, rec); err != nil {
		r.logger.Warn("decisionlog: tick insert failed", "error", err)
		return
	}
	r.nextSequenceInBar++
}

// persistPending UPDATEs the in-DB row with the current pending state.
// stage is included in the warn log so operators can tell which event
// triggered a failed update.
func (r *Recorder) persistPending(ctx context.Context, stage string) {
	if !r.hasPending {
		return
	}
	if err := r.repo.Update(ctx, r.pendingRec); err != nil {
		r.logger.Warn("decisionlog: update failed", "stage", stage, "id", r.pendingRec.ID, "error", err)
	}
}
