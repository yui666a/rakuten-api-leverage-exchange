package indicator

import "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"

// Microprice returns the volume-weighted touch:
//
//	microprice = (BestBid * askDepth + BestAsk * bidDepth) / (bidDepth + askDepth)
//
// Intuition: when askDepth >> bidDepth (sellers dominate the touch), the
// microprice tilts down toward BestBid — that's where transactions are most
// likely to happen next. Mirror image for bidDepth >> askDepth.
//
// Returns (0, false) when the book has no usable touch (zero volumes on both
// sides, or unset BestBid/BestAsk). Callers must check ok before using.
//
// We sum the *whole* visible side rather than just top-1 depth so a thin top
// followed by a thick second-level still produces a sensible weight. This
// matches the way OFI normalises against top-N depth, keeping the two
// signals comparable in scale.
func Microprice(ob entity.Orderbook) (float64, bool) {
	if ob.BestBid <= 0 || ob.BestAsk <= 0 {
		return 0, false
	}
	bidDepth := sumDepth(ob.Bids)
	askDepth := sumDepth(ob.Asks)
	total := bidDepth + askDepth
	if total <= 0 {
		return 0, false
	}
	return (ob.BestBid*askDepth + ob.BestAsk*bidDepth) / total, true
}

func sumDepth(levels []entity.OrderbookEntry) float64 {
	s := 0.0
	for _, lvl := range levels {
		if lvl.Amount > 0 {
			s += lvl.Amount
		}
	}
	return s
}

// TopNDepth returns the cumulative amount across the first n levels of one
// side of the book. Used both by Microprice consumers (to inspect the
// "weight" used) and by OFI for normalisation.
func TopNDepth(levels []entity.OrderbookEntry, n int) float64 {
	if n <= 0 {
		return 0
	}
	if n > len(levels) {
		n = len(levels)
	}
	s := 0.0
	for i := 0; i < n; i++ {
		if levels[i].Amount > 0 {
			s += levels[i].Amount
		}
	}
	return s
}
