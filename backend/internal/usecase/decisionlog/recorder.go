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
// completed cycle. It is NOT goroutine-safe; one Recorder must be bound to
// exactly one EventBus chain (the EventBus dispatch loop is single-threaded
// per chain so this matches the runtime invariant).
type Recorder struct {
	repo   repository.DecisionLogRepository
	cfg    RecorderConfig
	nowFn  func() time.Time
	logger *slog.Logger

	pending           *draft
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
		r.onIndicator(ev)
	case entity.SignalEvent:
		r.onSignal(ev)
	case entity.ApprovedSignalEvent:
		r.onApproved()
	case entity.RejectedSignalEvent:
		r.onRejected(ctx, ev)
	case entity.OrderEvent:
		r.onOrder(ctx, ev)
	}
	return nil, nil
}

// draft is the in-progress record for one bar. Fields are mutated as more
// events flow in; flush() persists and clears it.
type draft struct {
	rec entity.DecisionRecord
}

func (r *Recorder) stance() string {
	if r.cfg.StanceProvider == nil {
		return "UNKNOWN"
	}
	return r.cfg.StanceProvider()
}

func (r *Recorder) onIndicator(ev entity.IndicatorEvent) {
	// Flushing of the previous bar's pending draft happens lazily on the
	// next IndicatorEvent so we do not need a goroutine timer to know when
	// "the bar is over". A residual draft means the strategy returned HOLD
	// for that bar (no SignalEvent / ApprovedSignalEvent / OrderEvent
	// arrived) so we persist it as such.
	r.flushPending(context.Background())

	// Reset sequence numbering for the new bar.
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

	r.pending = &draft{
		rec: entity.DecisionRecord{
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
		},
	}
}

func (r *Recorder) onSignal(ev entity.SignalEvent) {
	if r.pending == nil {
		return
	}
	r.pending.rec.SignalAction = string(ev.Signal.Action)
	r.pending.rec.SignalConfidence = ev.Signal.Confidence
	r.pending.rec.SignalReason = ev.Signal.Reason
}

func (r *Recorder) onApproved() {
	if r.pending == nil {
		return
	}
	r.pending.rec.RiskOutcome = entity.DecisionRiskApproved
	r.pending.rec.BookGateOutcome = entity.DecisionBookAllowed
}

func (r *Recorder) onRejected(ctx context.Context, ev entity.RejectedSignalEvent) {
	if r.pending == nil {
		return
	}
	switch ev.Stage {
	case entity.RejectedStageRisk:
		r.pending.rec.RiskOutcome = entity.DecisionRiskRejected
		r.pending.rec.RiskReason = ev.Reason
	case entity.RejectedStageBookGate:
		r.pending.rec.RiskOutcome = entity.DecisionRiskApproved
		r.pending.rec.BookGateOutcome = entity.DecisionBookVetoed
		r.pending.rec.BookGateReason = ev.Reason
	}
	r.flushPending(ctx)
}

func (r *Recorder) onOrder(ctx context.Context, ev entity.OrderEvent) {
	switch ev.Trigger {
	case entity.DecisionTriggerTickSLTP, entity.DecisionTriggerTickTrailing:
		r.persistTickOrder(ctx, ev)
	default:
		// Treat empty Trigger as a bar-close order (legacy callers haven't
		// been migrated yet — until PR #4 lands, the ExecutionHandler still
		// emits OrderEvent with Trigger == "").
		r.persistBarOrder(ctx, ev)
	}
}

func (r *Recorder) persistBarOrder(ctx context.Context, ev entity.OrderEvent) {
	if r.pending == nil {
		return
	}
	if ev.OrderID > 0 {
		r.pending.rec.OrderOutcome = entity.DecisionOrderFilled
	} else {
		r.pending.rec.OrderOutcome = entity.DecisionOrderFailed
	}
	r.pending.rec.OrderID = ev.OrderID
	r.pending.rec.ExecutedAmount = ev.Amount
	r.pending.rec.ExecutedPrice = ev.Price
	r.pending.rec.OpenedPositionID = ev.OpenedPositionID
	r.pending.rec.ClosedPositionID = ev.ClosedPositionID
	r.flushPending(ctx)
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

func (r *Recorder) flushPending(ctx context.Context) {
	if r.pending == nil {
		return
	}
	rec := r.pending.rec
	rec.CreatedAt = r.nowFn().UnixMilli()
	if rec.IndicatorsJSON == "" {
		rec.IndicatorsJSON = "{}"
	}
	if rec.HigherTFIndicatorsJSON == "" {
		rec.HigherTFIndicatorsJSON = "{}"
	}
	if err := r.repo.Insert(ctx, rec); err != nil {
		r.logger.Warn("decisionlog: insert failed", "error", err)
	}
	r.pending = nil
}
