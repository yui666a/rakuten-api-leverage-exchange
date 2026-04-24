package positionsize

// SignalSizedFunc adapts Sizer.Compute into the narrow flat-argument signature
// consumed by the backtest RiskHandler and live pipeline. Kept in this package
// so both callers import the same function shape without leaking the Input
// struct across package boundaries (backtest handler.go cannot import
// positionsize directly without an import cycle in future wiring — the func
// form dodges that).
func (s *Sizer) Sized(requested, entryPrice, slPercent, equity, atr, ddPct, confidence, minConfidence float64) (float64, string) {
	out := s.Compute(Input{
		RequestedAmount:    requested,
		EntryPrice:         entryPrice,
		StopLossPercent:    slPercent,
		Equity:             equity,
		CurrentATR:         atr,
		CurrentDrawdownPct: ddPct,
		Confidence:         confidence,
		MinConfidence:      minConfidence,
	})
	return out.Amount, out.SkipReason
}
