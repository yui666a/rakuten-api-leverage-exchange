package live

import (
	"strings"
	"sync"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// LiveSource bridges real-time ticker data to EventEngine events.
// It accumulates ticks into candles and emits CandleEvent when periods close.
type LiveSource struct {
	symbolID        int64
	primaryInterval string // e.g. "PT15M"
	candleBuilder   *CandleBuilder
}

func NewLiveSource(symbolID int64, primaryInterval string) *LiveSource {
	interval := parseInterval(primaryInterval)
	return &LiveSource{
		symbolID:        symbolID,
		primaryInterval: primaryInterval,
		candleBuilder:   NewCandleBuilder(symbolID, interval),
	}
}

// HandleTick processes a real-time ticker and returns events to feed into EventEngine.
// Every ticker produces a TickEvent. When a candle period closes, a CandleEvent is also emitted.
func (s *LiveSource) HandleTick(ticker entity.Ticker) []entity.Event {
	var events []entity.Event

	// Always emit a TickEvent for SL/TP checking.
	tickEvent := entity.TickEvent{
		SymbolID:  ticker.SymbolID,
		Interval:  s.primaryInterval,
		Price:     ticker.Last,
		Timestamp: ticker.Timestamp,
		TickType:  "live",
		BarLow:    ticker.Low,
		BarHigh:   ticker.High,
	}
	events = append(events, tickEvent)

	// Feed into candle builder; may produce a CandleEvent on period boundary.
	if candleEvent := s.candleBuilder.AddTick(ticker); candleEvent != nil {
		candleEvent.Interval = s.primaryInterval
		events = append(events, *candleEvent)
	}

	return events
}

// CandleBuilder accumulates ticks and emits a CandleEvent when a candle period closes.
type CandleBuilder struct {
	mu            sync.Mutex
	interval      time.Duration
	symbolID      int64
	currentCandle *entity.Candle
	currentStart  time.Time
}

func NewCandleBuilder(symbolID int64, interval time.Duration) *CandleBuilder {
	return &CandleBuilder{
		symbolID: symbolID,
		interval: interval,
	}
}

// AddTick ingests a ticker. Returns a CandleEvent if the current period has closed, nil otherwise.
func (b *CandleBuilder) AddTick(ticker entity.Ticker) *entity.CandleEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	tickTime := time.UnixMilli(ticker.Timestamp)
	periodStart := b.periodStart(tickTime)

	price := ticker.Last

	// If no candle yet, or this tick belongs to the same period, accumulate.
	if b.currentCandle == nil {
		b.currentStart = periodStart
		b.currentCandle = &entity.Candle{
			Open:   price,
			High:   price,
			Low:    price,
			Close:  price,
			Volume: ticker.Volume,
			Time:   periodStart.UnixMilli(),
		}
		return nil
	}

	// If the tick is in the same period, update OHLCV.
	if periodStart.Equal(b.currentStart) {
		b.updateCandle(price, ticker.Volume)
		return nil
	}

	// Period boundary crossed: finalize current candle and emit event.
	completed := *b.currentCandle
	completedStart := b.currentStart
	closedTimestamp := completedStart.Add(b.interval).UnixMilli()

	// Start a new candle for the current period.
	b.currentStart = periodStart
	b.currentCandle = &entity.Candle{
		Open:   price,
		High:   price,
		Low:    price,
		Close:  price,
		Volume: ticker.Volume,
		Time:   periodStart.UnixMilli(),
	}

	return &entity.CandleEvent{
		SymbolID:  b.symbolID,
		Interval:  "", // filled by LiveSource
		Candle:    completed,
		Timestamp: closedTimestamp,
	}
}

// periodStart returns the start of the period that contains the given time.
func (b *CandleBuilder) periodStart(t time.Time) time.Time {
	if b.interval <= 0 {
		return t
	}
	return t.Truncate(b.interval)
}

// updateCandle updates the current candle with a new tick price and volume.
func (b *CandleBuilder) updateCandle(price, volume float64) {
	if price > b.currentCandle.High {
		b.currentCandle.High = price
	}
	if price < b.currentCandle.Low {
		b.currentCandle.Low = price
	}
	b.currentCandle.Close = price
	b.currentCandle.Volume = volume
}

// parseInterval converts an ISO 8601 duration string to time.Duration.
// Supports common intervals: PT1M, PT5M, PT15M, PT30M, PT1H, PT4H, P1D.
func parseInterval(s string) time.Duration {
	s = strings.ToUpper(s)
	switch s {
	case "PT1M":
		return time.Minute
	case "PT5M":
		return 5 * time.Minute
	case "PT15M":
		return 15 * time.Minute
	case "PT30M":
		return 30 * time.Minute
	case "PT1H":
		return time.Hour
	case "PT4H":
		return 4 * time.Hour
	case "P1D":
		return 24 * time.Hour
	default:
		return 15 * time.Minute // default to 15 minutes
	}
}
