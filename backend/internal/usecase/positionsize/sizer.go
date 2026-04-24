// Package positionsize computes the per-trade lot size from a declarative
// PositionSizingConfig plus a runtime Input snapshot.
//
// The sizer is intentionally a pure function — the same implementation runs
// inside the backtest simulator and the live trading pipeline, so a strategy
// promoted to production behaves bit-identically to what the backtest
// measured.
//
// Design goals:
//   - nil config or Mode="" / "fixed" → legacy pass-through (amount unchanged).
//   - risk_pct mode = equity * risk_pct / (entry_price * stop_loss_percent).
//   - Signal confidence, current ATR, and running drawdown all compose
//     multiplicatively on top of the base lot.
//   - Venue lot-step rounding is applied last; lot below MinLot rejects the
//     trade with a human-readable SkipReason.
package positionsize

import (
	"fmt"
	"math"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// Input carries the runtime snapshot the sizer needs at a single entry point.
type Input struct {
	// RequestedAmount is the trade amount configured by the caller (CLI flag
	// or pipeline config). In "fixed" mode this value is returned as-is.
	RequestedAmount float64
	EntryPrice      float64
	// StopLossPercent is the strategy SL distance (percent units, e.g. 14 for
	// 14%). Required for risk_pct sizing.
	StopLossPercent float64
	// Equity is the account balance (JPY) at decision time. Must be > 0 for
	// risk_pct sizing; 0/negative triggers a zero-lot rejection.
	Equity float64
	// CurrentATR is the latest ATR value in price units. 0 disables ATR
	// adjustment even when ATRAdjust is true.
	CurrentATR float64
	// CurrentDrawdownPct is the current DD from peak equity, in percent
	// (12 = 12% below peak). 0 or negative disables DD-based scaling.
	CurrentDrawdownPct float64
	// Confidence is the signal confidence, in [0, 1].
	Confidence float64
	// MinConfidence mirrors the pipeline's rejection threshold; used for
	// confidence-based scaling.
	MinConfidence float64
}

// Output carries the sized lot plus a decision trail for logging.
type Output struct {
	Amount         float64
	Mode           string
	BaseAmount     float64
	AppliedATR     float64
	AppliedDD      float64
	AppliedConf    float64
	Capped         bool
	SkipReason     string
}

// Defaults holds fallback values applied when the config leaves a field at
// zero. Centralised so both the sizer and its tests agree on what "unset"
// means.
type Defaults struct {
	MinLot         float64
	LotStep        float64
	ATRScaleMin    float64
	ATRScaleMax    float64
	TargetATRPct   float64
}

// DefaultDefaults returns LTC/JPY venue defaults for Rakuten Wallet margin
// trading. Source: https://www.rakuten-wallet.co.jp/service/overview.html —
// the "取引金額・数量" section lists LTC/JPY minimum order size as 0.1 LTC.
// Other symbols have different minimums (BTC 0.01, ETH 0.1, BCH 0.1, XRP 100)
// — production code must override MinLot/LotStep per symbol rather than
// relying on these defaults outside of LTC/JPY.
func DefaultDefaults() Defaults {
	return Defaults{
		MinLot:       0.1,
		LotStep:      0.1,
		ATRScaleMin:  0.5,
		ATRScaleMax:  2.0,
		TargetATRPct: 2.0,
	}
}

// VenueDefaults returns Rakuten Wallet margin-trading lot constraints for the
// given symbol. Unknown symbols fall back to DefaultDefaults (LTC values).
// Source: https://www.rakuten-wallet.co.jp/service/overview.html
func VenueDefaults(symbol string) Defaults {
	d := DefaultDefaults()
	switch symbol {
	case "BTC_JPY", "BTC/JPY":
		d.MinLot = 0.01
		d.LotStep = 0.01
	case "ETH_JPY", "ETH/JPY", "BCH_JPY", "BCH/JPY", "LTC_JPY", "LTC/JPY":
		d.MinLot = 0.1
		d.LotStep = 0.1
	case "XRP_JPY", "XRP/JPY":
		d.MinLot = 100
		d.LotStep = 100
	}
	return d
}

// Sizer holds an immutable config + defaults snapshot.
type Sizer struct {
	cfg      *entity.PositionSizingConfig
	defaults Defaults
}

// New returns a Sizer. cfg may be nil, in which case fixed pass-through is
// used.
func New(cfg *entity.PositionSizingConfig, d Defaults) *Sizer {
	return &Sizer{cfg: cfg, defaults: d}
}

func (s *Sizer) mode() string {
	if s.cfg == nil || s.cfg.Mode == "" {
		return "fixed"
	}
	return s.cfg.Mode
}

// Compute returns the sized lot for the given input.
func (s *Sizer) Compute(in Input) Output {
	mode := s.mode()

	if mode == "fixed" {
		amt := in.RequestedAmount
		if amt < 0 {
			amt = 0
		}
		return Output{Amount: amt, Mode: "fixed", BaseAmount: amt, AppliedATR: 1, AppliedDD: 1, AppliedConf: 1}
	}

	// risk_pct path
	if in.Equity <= 0 || in.EntryPrice <= 0 || in.StopLossPercent <= 0 {
		return Output{Mode: mode, SkipReason: fmt.Sprintf("invalid input: equity=%v price=%v sl=%v", in.Equity, in.EntryPrice, in.StopLossPercent)}
	}

	riskPct := s.cfg.RiskPerTradePct
	if riskPct <= 0 {
		riskPct = 1.0
	}
	riskJPY := in.Equity * (riskPct / 100.0)
	slDistanceJPY := in.EntryPrice * (in.StopLossPercent / 100.0)
	base := riskJPY / slDistanceJPY

	out := Output{Mode: mode, BaseAmount: base, AppliedATR: 1, AppliedDD: 1, AppliedConf: 1}
	amount := base

	// ATR adjustment (optional).
	if s.cfg.ATRAdjust && in.CurrentATR > 0 && in.EntryPrice > 0 {
		target := s.cfg.TargetATRPct
		if target <= 0 {
			target = s.defaults.TargetATRPct
		}
		currentATRPct := (in.CurrentATR / in.EntryPrice) * 100.0
		if currentATRPct > 0 && target > 0 {
			scale := target / currentATRPct
			lo := s.cfg.ATRScaleMin
			if lo <= 0 {
				lo = s.defaults.ATRScaleMin
			}
			hi := s.cfg.ATRScaleMax
			if hi <= 0 {
				hi = s.defaults.ATRScaleMax
			}
			if scale < lo {
				scale = lo
			}
			if scale > hi {
				scale = hi
			}
			amount *= scale
			out.AppliedATR = scale
		}
	}

	// Confidence scaling: when min_conf is set, map [min, 1] → [0.5, 1.0].
	if in.MinConfidence > 0 && in.MinConfidence < 1 {
		conf := in.Confidence
		if conf < in.MinConfidence {
			conf = in.MinConfidence
		}
		if conf > 1 {
			conf = 1
		}
		confScale := 0.5 + 0.5*(conf-in.MinConfidence)/(1.0-in.MinConfidence)
		amount *= confScale
		out.AppliedConf = confScale
	}

	// Drawdown tier-based scaling.
	if dd := in.CurrentDrawdownPct; dd > 0 {
		d := s.cfg.DDScaleDown
		switch {
		case d.TierBPct > 0 && dd >= d.TierBPct && d.TierBScale > 0:
			amount *= d.TierBScale
			out.AppliedDD = d.TierBScale
		case d.TierAPct > 0 && dd >= d.TierAPct && d.TierAScale > 0:
			amount *= d.TierAScale
			out.AppliedDD = d.TierAScale
		}
	}

	// Max-position-pct cap.
	if cap := s.cfg.MaxPositionPctOfEquity; cap > 0 {
		maxNotional := in.Equity * (cap / 100.0)
		maxLot := maxNotional / in.EntryPrice
		if amount > maxLot {
			amount = maxLot
			out.Capped = true
		}
	}

	// Lot-step rounding.
	step := s.cfg.LotStep
	if step <= 0 {
		step = s.defaults.LotStep
	}
	if step > 0 {
		amount = math.Floor(amount/step) * step
	}

	// Min-lot gate.
	minLot := s.cfg.MinLot
	if minLot <= 0 {
		minLot = s.defaults.MinLot
	}
	if minLot > 0 && amount < minLot {
		return Output{
			Mode:        mode,
			BaseAmount:  base,
			AppliedATR:  out.AppliedATR,
			AppliedDD:   out.AppliedDD,
			AppliedConf: out.AppliedConf,
			SkipReason:  fmt.Sprintf("computed lot %.4f below min_lot %.4f", amount, minLot),
		}
	}

	out.Amount = amount
	return out
}
