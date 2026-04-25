package handler

import (
	"context"
	"log/slog"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase"
)

// symbolProvider is the narrow port live_snapshot uses to read the active
// trading symbol. Mirrors the PipelineController.SymbolID() method on the
// router-level interface so the handler layer doesn't depend on the pipeline
// package directly.
type symbolProvider interface {
	SymbolID() int64
}

// pipelineLiveSnapshot computes the current indicator set + price for the
// *active* trading symbol. It's intentionally a thin shim so the handler
// layer does not import the pipeline directly — it only needs SymbolID()
// from whatever PipelineController the router passes in.
type pipelineLiveSnapshot struct {
	symbol    symbolProvider
	calc      *usecase.IndicatorCalculator
	market    *usecase.MarketDataService
	primaryTF string
}

// NewPipelineLiveSnapshot returns a LiveMarketSnapshot bound to the pipeline's
// currently-selected symbol. nil-safety: if any of symbol/calc/market is nil,
// the shim returns an empty IndicatorSet + 0 so the resolver falls back to the
// legacy warmup branch. primaryTF defaults to "PT15M" when empty (current
// production interval).
func NewPipelineLiveSnapshot(
	symbol symbolProvider,
	calc *usecase.IndicatorCalculator,
	market *usecase.MarketDataService,
	primaryTF string,
) LiveMarketSnapshot {
	if primaryTF == "" {
		primaryTF = "PT15M"
	}
	return &pipelineLiveSnapshot{
		symbol:    symbol,
		calc:      calc,
		market:    market,
		primaryTF: primaryTF,
	}
}

func (p *pipelineLiveSnapshot) Snapshot(ctx context.Context) (entity.IndicatorSet, float64) {
	if p == nil || p.symbol == nil || p.calc == nil || p.market == nil {
		return entity.IndicatorSet{}, 0
	}
	symbolID := p.symbol.SymbolID()
	if symbolID <= 0 {
		return entity.IndicatorSet{}, 0
	}

	ind, err := p.calc.Calculate(ctx, symbolID, p.primaryTF)
	if err != nil || ind == nil {
		slog.Debug("strategy snapshot: calculate indicators failed, falling back",
			"symbolID", symbolID, "interval", p.primaryTF, "err", err)
		return entity.IndicatorSet{}, 0
	}

	ticker, err := p.market.GetLatestTicker(ctx, symbolID)
	if err != nil || ticker == nil {
		slog.Debug("strategy snapshot: latest ticker unavailable",
			"symbolID", symbolID, "err", err)
		return *ind, 0
	}
	return *ind, ticker.Last
}
