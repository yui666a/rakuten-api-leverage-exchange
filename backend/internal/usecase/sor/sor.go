// Package sor implements a Smart Order Router that decides how a single
// approved trading signal should be turned into one or more venue orders.
//
// The router is deliberately small:
//   - StrategyMarket          - reproduce the legacy "one MARKET order" flow
//   - StrategyPostOnlyEscalate - place a post-only LIMIT near the touch and
//     escalate to MARKET after a timeout if it does not fill
//
// Both backtest and live consume the same Plan struct; the executor chooses
// how literally to interpret it. Backtest currently only honours
// StrategyMarket — the post-only path requires a real venue book to model
// queue priority, which the percent-slippage simulator cannot fake without
// adding more state than this PR introduces.
package sor

import (
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// Strategy enumerates supported execution tactics.
type Strategy string

const (
	// StrategyMarket: emit one MARKET order at signal price (current default).
	StrategyMarket Strategy = "market"
	// StrategyPostOnlyEscalate: place one post-only LIMIT inside the touch,
	// then cancel + replace with MARKET if not fully filled within the
	// configured escalation window.
	StrategyPostOnlyEscalate Strategy = "post_only_escalate"
	// StrategyIceberg: split the lot into SliceCount even pieces and submit
	// them one at a time with MinIntervalMs between submissions. Each piece
	// is a plain MARKET order — no maker probing. Useful when a single
	// MARKET would soak too much of the visible book and trip the pre-trade
	// gate, or simply to obscure the trader's full size.
	StrategyIceberg Strategy = "iceberg"
)

// Config controls the router's choice. Zero value selects the legacy
// StrategyMarket path so existing callers stay bit-identical.
type Config struct {
	// Strategy picks the execution tactic.
	Strategy Strategy
	// LimitOffsetTicks is how many ticks INSIDE the touch the post-only
	// LIMIT is placed. 0 = best-bid (buy) / best-ask (sell). 1 = one tick
	// further into the book on the maker-friendly side, increasing the
	// chance of being a maker but reducing fill probability. Default is 1.
	LimitOffsetTicks int
	// TickSize is the venue-defined minimum price increment. Required for
	// PostOnlyEscalate; 0 falls back to a conservative 0.1 which is the
	// LTC/JPY tick size on Rakuten Wallet.
	TickSize float64
	// EscalateAfterMs is the maximum time (millis) the LIMIT may rest on
	// the book before the executor cancels it and emits a MARKET fallback.
	// 0 falls back to DefaultEscalateAfterMs (30_000).
	EscalateAfterMs int64
	// MinIntervalMs bounds back-to-back venue requests so we don't trip
	// the documented 200 ms / user rate limit. Default DefaultMinIntervalMs.
	MinIntervalMs int64
	// SliceCount is how many pieces to split the lot into when using
	// StrategyIceberg. 0 falls back to DefaultSliceCount (5). Capped at
	// DefaultSliceCountMax internally to keep the venue rate-limit budget
	// reasonable.
	SliceCount int
}

// DefaultSliceCount is the iceberg fallback when Config.SliceCount is zero.
const DefaultSliceCount = 5

// DefaultSliceCountMax caps the iceberg slice count so a misconfigured
// SliceCount cannot DoS the venue.
const DefaultSliceCountMax = 20

// DefaultEscalateAfterMs is the fallback timeout when Config.EscalateAfterMs
// is zero. 30 s matches the design discussion: a typical 15 m bar's signal
// has ~14 m of slack, so 30 s of patience for a maker fill is cheap.
const DefaultEscalateAfterMs = int64(30_000)

// DefaultMinIntervalMs leaves headroom over the venue's documented 200 ms
// rate limit. Surfaced here so the live executor can use a single source.
const DefaultMinIntervalMs = int64(250)

// DefaultLimitOffsetTicks places the LIMIT one tick deeper than the touch
// (e.g. BUY at BestBid - 1 tick). One tick is usually enough to dodge
// "post-only would have crossed" rejections without driving the fill
// probability to zero.
const DefaultLimitOffsetTicks = 1

// DefaultTickSize is the LTC/JPY venue tick. BTC/JPY uses a larger tick
// (5 JPY) so callers must set TickSize explicitly for that symbol.
const DefaultTickSize = 0.1

// Step is one phase of a Plan: either submit a fresh order or wait for the
// previous one to reach a terminal state. The executor walks the slice in
// order; each Step is independent so it can be resumed from a stored ID
// after a process restart (future PR).
type Step struct {
	Kind StepKind
	// Order is populated when Kind == StepKindSubmit. It is fully formed
	// (no later mutation by the executor) so the executor's only job is to
	// hand it to OrderClient.CreateOrder.
	Order entity.OrderRequest
	// EscalateAfterMs is the deadline (relative to step start) by which the
	// previous Submit step must have reached a terminal state. Only meaningful
	// for StepKindWaitOrEscalate. 0 = wait indefinitely.
	EscalateAfterMs int64
	// FallbackOrder fires when WaitOrEscalate hits its deadline and the
	// previous Submit step has not fully filled. Populated for
	// StepKindWaitOrEscalate.
	FallbackOrder entity.OrderRequest
	// WaitMs is the fixed pause for StepKindWaitInterval.
	WaitMs int64
}

// StepKind describes what the executor must do for a Step.
type StepKind string

const (
	// StepKindSubmit asks the executor to send Order to OrderClient.CreateOrder.
	StepKindSubmit StepKind = "submit"
	// StepKindWaitOrEscalate asks the executor to wait for the prior Submit
	// step to fill (or be cancelled by the venue) up to EscalateAfterMs;
	// when the deadline is reached without a complete fill, the executor
	// cancels the resting order and submits FallbackOrder.
	StepKindWaitOrEscalate StepKind = "wait_or_escalate"
	// StepKindWaitInterval is a fixed pause between two Submit steps. Used
	// by the iceberg strategy to space out slice submissions and avoid the
	// venue rate limit.
	StepKindWaitInterval StepKind = "wait_interval"
)

// Plan is the full execution plan for one approved signal.
type Plan struct {
	// Steps is the ordered list of phases the executor must run. For
	// StrategyMarket this is a single Submit step; for
	// StrategyPostOnlyEscalate it is Submit + WaitOrEscalate (with a MARKET
	// fallback). The executor never reorders the slice.
	Steps []Step
	// Strategy is the tactic that produced this plan. Surfaced for logging
	// and metrics — the executor does not branch on it.
	Strategy Strategy
}

// Selector turns (signal, lot, book context) into a Plan. The router has no
// state of its own — the same Selector instance can be shared by all goroutines.
type Selector struct {
	cfg Config
}

// New builds a Selector with the supplied config. Zero-value Config picks
// StrategyMarket so the legacy executor path stays untouched.
func New(cfg Config) *Selector {
	if cfg.Strategy == "" {
		cfg.Strategy = StrategyMarket
	}
	if cfg.LimitOffsetTicks <= 0 {
		cfg.LimitOffsetTicks = DefaultLimitOffsetTicks
	}
	if cfg.TickSize <= 0 {
		cfg.TickSize = DefaultTickSize
	}
	if cfg.EscalateAfterMs <= 0 {
		cfg.EscalateAfterMs = DefaultEscalateAfterMs
	}
	if cfg.MinIntervalMs <= 0 {
		cfg.MinIntervalMs = DefaultMinIntervalMs
	}
	return &Selector{cfg: cfg}
}

// SelectInput is everything the router needs to build a Plan. We pass it as
// a struct (rather than positional args) because callers from both
// EventDrivenPipeline and tests need to extend it without rewriting every
// call site.
type SelectInput struct {
	SymbolID int64
	Side     entity.OrderSide
	Amount   float64
	// BestBid / BestAsk are the touch prices observed at signal time. The
	// router reads these directly to compute the LIMIT price; passing them
	// avoids a round-trip back through the BookSource port for callers that
	// already have a snapshot in hand.
	BestBid float64
	BestAsk float64
	// PositionID is required by the venue when the order is a CLOSE on an
	// existing margin position. nil for OPEN orders.
	PositionID *int64
}

// Plan returns the execution Plan for the input signal.
func (s *Selector) Plan(in SelectInput) Plan {
	switch s.cfg.Strategy {
	case StrategyPostOnlyEscalate:
		return s.planPostOnlyEscalate(in)
	case StrategyIceberg:
		return s.planIceberg(in)
	default:
		return s.planMarket(in)
	}
}

// planIceberg splits the input lot into SliceCount equal MARKET orders with
// MinIntervalMs spacing. When the resulting per-slice amount would round
// to 0 (very small lot), falls back to a single MARKET so the signal isn't
// silently dropped.
func (s *Selector) planIceberg(in SelectInput) Plan {
	count := s.cfg.SliceCount
	if count <= 0 {
		count = DefaultSliceCount
	}
	if count > DefaultSliceCountMax {
		count = DefaultSliceCountMax
	}
	if count <= 1 {
		return s.planMarket(in)
	}
	per := in.Amount / float64(count)
	if per <= 0 {
		return s.planMarket(in)
	}
	steps := make([]Step, 0, count*2-1)
	for i := 0; i < count; i++ {
		// Last slice absorbs any rounding so the total amount matches the
		// caller's request exactly.
		amt := per
		if i == count-1 {
			amt = in.Amount - per*float64(count-1)
		}
		sliceIn := in
		sliceIn.Amount = amt
		steps = append(steps, Step{Kind: StepKindSubmit, Order: marketOrder(sliceIn)})
		if i < count-1 {
			steps = append(steps, Step{Kind: StepKindWaitInterval, WaitMs: s.cfg.MinIntervalMs})
		}
	}
	return Plan{Strategy: StrategyIceberg, Steps: steps}
}

func (s *Selector) planMarket(in SelectInput) Plan {
	return Plan{
		Strategy: StrategyMarket,
		Steps:    []Step{{Kind: StepKindSubmit, Order: marketOrder(in)}},
	}
}

func (s *Selector) planPostOnlyEscalate(in SelectInput) Plan {
	// If we have no usable touch price, fall back to a plain MARKET order.
	// We refuse to invent a LIMIT price out of thin air because a wrong
	// guess could either be rejected (post-only crossed) or stay open way
	// past the bar.
	if in.BestBid <= 0 || in.BestAsk <= 0 {
		return s.planMarket(in)
	}

	limitPrice := computeLimitPrice(in.Side, in.BestBid, in.BestAsk, s.cfg.LimitOffsetTicks, s.cfg.TickSize)
	if limitPrice <= 0 {
		return s.planMarket(in)
	}

	postOnly := true
	limit := entity.OrderRequest{
		SymbolID:     in.SymbolID,
		OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{
			OrderBehavior: openOrCloseBehavior(in),
			OrderSide:     in.Side,
			OrderType:     entity.OrderTypeLimit,
			Price:         &limitPrice,
			Amount:        in.Amount,
			PostOnly:      &postOnly,
			PositionID:    in.PositionID,
		},
	}

	return Plan{
		Strategy: StrategyPostOnlyEscalate,
		Steps: []Step{
			{Kind: StepKindSubmit, Order: limit},
			{
				Kind:            StepKindWaitOrEscalate,
				EscalateAfterMs: s.cfg.EscalateAfterMs,
				FallbackOrder:   marketOrder(in),
			},
		},
	}
}

// MinIntervalMs surfaces the configured rate-limit gap so the executor can
// pace its venue calls without re-deriving it.
func (s *Selector) MinIntervalMs() int64 { return s.cfg.MinIntervalMs }

// computeLimitPrice picks the post-only LIMIT price for the given side.
// BUY rests below BestBid (so it cannot cross BestAsk); SELL rests above
// BestAsk. offsetTicks = 0 means "right at the touch" — usable but the
// venue may reject it as crossing. offsetTicks = 1 (the default) is the
// safe choice.
func computeLimitPrice(side entity.OrderSide, bestBid, bestAsk float64, offsetTicks int, tickSize float64) float64 {
	if tickSize <= 0 {
		return 0
	}
	delta := float64(offsetTicks) * tickSize
	switch side {
	case entity.OrderSideBuy:
		// BUY hits asks; for a maker we sit BELOW BestBid (safer than at
		// BestBid because some venues treat BB as crossable).
		return bestBid - delta
	case entity.OrderSideSell:
		return bestAsk + delta
	}
	return 0
}

func marketOrder(in SelectInput) entity.OrderRequest {
	return entity.OrderRequest{
		SymbolID:     in.SymbolID,
		OrderPattern: entity.OrderPatternNormal,
		OrderData: entity.OrderData{
			OrderBehavior: openOrCloseBehavior(in),
			OrderSide:     in.Side,
			OrderType:     entity.OrderTypeMarket,
			Amount:        in.Amount,
			PositionID:    in.PositionID,
		},
	}
}

func openOrCloseBehavior(in SelectInput) entity.OrderBehavior {
	if in.PositionID != nil {
		return entity.OrderBehaviorClose
	}
	return entity.OrderBehaviorOpen
}
