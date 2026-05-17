package live

import (
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestLiveSource_HandleTick_EmitsTickEvent(t *testing.T) {
	src := NewLiveSource(7, "PT15M")

	ticker := entity.Ticker{
		SymbolID:  7,
		BestAsk:   50100,
		BestBid:   49900,
		Last:      50000,
		High:      50500,
		Low:       49500,
		Volume:    100,
		Timestamp: time.Date(2026, 4, 15, 10, 3, 0, 0, time.UTC).UnixMilli(),
	}

	events := src.HandleTick(ticker)
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}

	// First event should always be a TickEvent.
	tickEv, ok := events[0].(entity.TickEvent)
	if !ok {
		t.Fatalf("expected TickEvent, got %T", events[0])
	}
	if tickEv.EventType() != entity.EventTypeTick {
		t.Fatalf("expected event type %s, got %s", entity.EventTypeTick, tickEv.EventType())
	}
	if tickEv.Price != 50000 {
		t.Fatalf("expected price=50000, got %f", tickEv.Price)
	}
	if tickEv.SymbolID != 7 {
		t.Fatalf("expected symbolID=7, got %d", tickEv.SymbolID)
	}
	if tickEv.TickType != "live" {
		t.Fatalf("expected tickType=live, got %s", tickEv.TickType)
	}
}

func TestLiveSource_HandleTick_NoCandleForSamePeriod(t *testing.T) {
	src := NewLiveSource(7, "PT15M")

	base := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)

	// All ticks within the same 15-minute period.
	for i := 0; i < 5; i++ {
		ticker := entity.Ticker{
			SymbolID:  7,
			Last:      50000 + float64(i*100),
			High:      50500,
			Low:       49500,
			Volume:    float64(100 + i),
			Timestamp: base.Add(time.Duration(i) * time.Minute).UnixMilli(),
		}
		events := src.HandleTick(ticker)
		// Each tick should produce only a TickEvent (no CandleEvent within same period).
		for _, ev := range events {
			if ev.EventType() == entity.EventTypeCandle {
				t.Fatalf("unexpected CandleEvent within same period at tick %d", i)
			}
		}
	}
}

func TestLiveSource_HandleTick_EmitsCandleOnPeriodBoundary(t *testing.T) {
	src := NewLiveSource(7, "PT15M")

	// Tick 1: within first 15-min period (10:00 - 10:15).
	t1 := entity.Ticker{
		SymbolID:  7,
		Last:      50000,
		High:      50500,
		Low:       49500,
		Volume:    100,
		Timestamp: time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC).UnixMilli(),
	}
	events := src.HandleTick(t1)
	if len(events) != 1 {
		t.Fatalf("expected 1 event (TickEvent), got %d", len(events))
	}

	// Tick 2: still in first period.
	t2 := entity.Ticker{
		SymbolID:  7,
		Last:      50200,
		High:      50700,
		Low:       49800,
		Volume:    110,
		Timestamp: time.Date(2026, 4, 15, 10, 5, 0, 0, time.UTC).UnixMilli(),
	}
	events = src.HandleTick(t2)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Tick 3: higher price, still first period.
	t3 := entity.Ticker{
		SymbolID:  7,
		Last:      50500,
		High:      51000,
		Low:       49900,
		Volume:    120,
		Timestamp: time.Date(2026, 4, 15, 10, 14, 0, 0, time.UTC).UnixMilli(),
	}
	events = src.HandleTick(t3)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Tick 4: crosses into second period (10:15). Should emit CandleEvent for first period.
	t4 := entity.Ticker{
		SymbolID:  7,
		Last:      50300,
		High:      50800,
		Low:       50000,
		Volume:    130,
		Timestamp: time.Date(2026, 4, 15, 10, 15, 0, 0, time.UTC).UnixMilli(),
	}
	events = src.HandleTick(t4)

	var candleEvent *entity.CandleEvent
	tickCount := 0
	for _, ev := range events {
		switch e := ev.(type) {
		case entity.TickEvent:
			tickCount++
		case entity.CandleEvent:
			candleEvent = &e
		}
	}

	if tickCount != 1 {
		t.Fatalf("expected 1 TickEvent, got %d", tickCount)
	}
	if candleEvent == nil {
		t.Fatal("expected CandleEvent on period boundary")
	}

	// Verify candle OHLCV from ticks t1, t2, t3.
	if candleEvent.Candle.Open != 50000 {
		t.Fatalf("expected open=50000, got %f", candleEvent.Candle.Open)
	}
	if candleEvent.Candle.High != 50500 {
		t.Fatalf("expected high=50500, got %f", candleEvent.Candle.High)
	}
	if candleEvent.Candle.Low != 50000 {
		t.Fatalf("expected low=50000, got %f", candleEvent.Candle.Low)
	}
	if candleEvent.Candle.Close != 50500 {
		t.Fatalf("expected close=50500, got %f", candleEvent.Candle.Close)
	}

	// Verify interval is set.
	if candleEvent.Interval != "PT15M" {
		t.Fatalf("expected interval=PT15M, got %s", candleEvent.Interval)
	}

	// Verify candle timestamp is the end of the first period.
	expectedTS := time.Date(2026, 4, 15, 10, 15, 0, 0, time.UTC).UnixMilli()
	if candleEvent.Timestamp != expectedTS {
		t.Fatalf("expected timestamp=%d, got %d", expectedTS, candleEvent.Timestamp)
	}
}

func TestCandleBuilder_MultiplePeriodsEmitMultipleCandles(t *testing.T) {
	builder := NewCandleBuilder(7, 15*time.Minute)

	base := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)

	// Period 1: one tick.
	ticker1 := entity.Ticker{
		SymbolID:  7,
		Last:      100,
		Volume:    10,
		Timestamp: base.UnixMilli(),
	}
	ev := builder.AddTick(ticker1)
	if ev != nil {
		t.Fatal("should not emit on first tick")
	}

	// Period 2: first tick triggers candle for period 1.
	ticker2 := entity.Ticker{
		SymbolID:  7,
		Last:      110,
		Volume:    20,
		Timestamp: base.Add(15 * time.Minute).UnixMilli(),
	}
	ev = builder.AddTick(ticker2)
	if ev == nil {
		t.Fatal("expected CandleEvent for period 1")
	}
	if ev.Candle.Open != 100 || ev.Candle.Close != 100 {
		t.Fatalf("expected single-tick candle O=C=100, got O=%f C=%f", ev.Candle.Open, ev.Candle.Close)
	}

	// Period 3: first tick triggers candle for period 2.
	ticker3 := entity.Ticker{
		SymbolID:  7,
		Last:      120,
		Volume:    30,
		Timestamp: base.Add(30 * time.Minute).UnixMilli(),
	}
	ev = builder.AddTick(ticker3)
	if ev == nil {
		t.Fatal("expected CandleEvent for period 2")
	}
	if ev.Candle.Open != 110 || ev.Candle.Close != 110 {
		t.Fatalf("expected single-tick candle O=C=110, got O=%f C=%f", ev.Candle.Open, ev.Candle.Close)
	}
}

func TestCandleBuilder_OHLCV_AccumulatesCorrectly(t *testing.T) {
	builder := NewCandleBuilder(7, 5*time.Minute)

	base := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)

	tickers := []entity.Ticker{
		{SymbolID: 7, Last: 100, Volume: 10, Timestamp: base.UnixMilli()},                         // open
		{SymbolID: 7, Last: 105, Volume: 15, Timestamp: base.Add(1 * time.Minute).UnixMilli()},    // high
		{SymbolID: 7, Last: 95, Volume: 20, Timestamp: base.Add(2 * time.Minute).UnixMilli()},     // low
		{SymbolID: 7, Last: 102, Volume: 25, Timestamp: base.Add(4 * time.Minute).UnixMilli()},    // close
	}

	for _, tk := range tickers {
		ev := builder.AddTick(tk)
		if ev != nil {
			t.Fatal("should not emit during same period")
		}
	}

	// Next period tick triggers the candle.
	nextTick := entity.Ticker{
		SymbolID:  7,
		Last:      108,
		Volume:    30,
		Timestamp: base.Add(5 * time.Minute).UnixMilli(),
	}
	ev := builder.AddTick(nextTick)
	if ev == nil {
		t.Fatal("expected CandleEvent")
	}

	if ev.Candle.Open != 100 {
		t.Fatalf("expected open=100, got %f", ev.Candle.Open)
	}
	if ev.Candle.High != 105 {
		t.Fatalf("expected high=105, got %f", ev.Candle.High)
	}
	if ev.Candle.Low != 95 {
		t.Fatalf("expected low=95, got %f", ev.Candle.Low)
	}
	if ev.Candle.Close != 102 {
		t.Fatalf("expected close=102, got %f", ev.Candle.Close)
	}
}

func TestParseInterval(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"PT1M", time.Minute},
		{"PT5M", 5 * time.Minute},
		{"PT15M", 15 * time.Minute},
		{"PT30M", 30 * time.Minute},
		{"PT1H", time.Hour},
		{"PT4H", 4 * time.Hour},
		{"P1D", 24 * time.Hour},
		{"pt15m", 15 * time.Minute}, // lowercase
		{"UNKNOWN", 15 * time.Minute},  // default
	}

	for _, tt := range tests {
		got := parseInterval(tt.input)
		if got != tt.expected {
			t.Errorf("parseInterval(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestLiveSource_SeedFromMinuteCandles_FoldsCurrentPeriodOnly(t *testing.T) {
	src := NewLiveSource(7, "PT15M")
	// "now" sits inside the 10:15-10:30 window.
	now := time.Date(2026, 4, 27, 10, 23, 0, 0, time.UTC)
	periodStart := time.Date(2026, 4, 27, 10, 15, 0, 0, time.UTC)

	minutes := []entity.Candle{
		// Earlier period (10:00-10:15) — must be ignored.
		{Open: 100, High: 110, Low: 95, Close: 105, Volume: 1, Time: time.Date(2026, 4, 27, 10, 14, 0, 0, time.UTC).UnixMilli()},
		// Current period — three minute bars at 10:15, 10:16, 10:17.
		{Open: 200, High: 210, Low: 195, Close: 205, Volume: 2, Time: time.Date(2026, 4, 27, 10, 15, 0, 0, time.UTC).UnixMilli()},
		{Open: 205, High: 220, Low: 200, Close: 215, Volume: 3, Time: time.Date(2026, 4, 27, 10, 16, 0, 0, time.UTC).UnixMilli()},
		{Open: 215, High: 218, Low: 190, Close: 192, Volume: 4, Time: time.Date(2026, 4, 27, 10, 17, 0, 0, time.UTC).UnixMilli()},
	}

	folded := src.SeedFromMinuteCandles(now, minutes)
	if folded != 3 {
		t.Fatalf("folded count = %d, want 3", folded)
	}

	// Drive an in-period tick so the seeded candle becomes observable via
	// the same path live ticks travel through.
	tick := entity.Ticker{
		SymbolID:  7,
		Last:      193,
		Volume:    5,
		Timestamp: time.Date(2026, 4, 27, 10, 24, 0, 0, time.UTC).UnixMilli(),
	}
	events := src.HandleTick(tick)
	if len(events) != 1 {
		t.Fatalf("in-period tick should not emit a CandleEvent, got %d events", len(events))
	}

	// Cross the boundary (10:30) — the seeded + updated bar should now emit.
	closeTick := entity.Ticker{
		SymbolID:  7,
		Last:      230,
		Volume:    1,
		Timestamp: time.Date(2026, 4, 27, 10, 30, 1, 0, time.UTC).UnixMilli(),
	}
	events = src.HandleTick(closeTick)
	if len(events) != 2 {
		t.Fatalf("expected 2 events (Tick + Candle), got %d", len(events))
	}
	candleEv, ok := events[1].(entity.CandleEvent)
	if !ok {
		t.Fatalf("second event = %T, want CandleEvent", events[1])
	}
	c := candleEv.Candle
	if c.Time != periodStart.UnixMilli() {
		t.Errorf("candle.Time = %d, want %d (period start)", c.Time, periodStart.UnixMilli())
	}
	if c.Open != 200 {
		t.Errorf("Open = %f, want 200 (first seeded minute open)", c.Open)
	}
	if c.High != 220 {
		t.Errorf("High = %f, want 220 (max across seeded minutes)", c.High)
	}
	if c.Low != 190 {
		t.Errorf("Low = %f, want 190 (min across seeded minutes including the third)", c.Low)
	}
	// Close = last live tick that landed inside the bar (193), since the
	// boundary-crossing tick at 230 starts the *next* bar.
	if c.Close != 193 {
		t.Errorf("Close = %f, want 193 (last in-period live tick)", c.Close)
	}
}

// TestLiveSource_HandleTick_BarRangeFromCurrentCandle_NotTicker24h は
// Issue #266 の回帰テスト。楽天 ticker.High/Low は 24h ローリングレンジで、
// これを TickEvent.BarHigh/BarLow に渡すと SL/TP 即発火事故 (2026-05-12,
// 2026-05-17) を招いた。BarHigh/BarLow は現在進行中バー (CandleBuilder の
// currentCandle) 由来でなければならない。
func TestLiveSource_HandleTick_BarRangeFromCurrentCandle_NotTicker24h(t *testing.T) {
	src := NewLiveSource(7, "PT15M")
	base := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)

	// 24h ticker high/low は意図的に Last から大きく離す。バグがあると
	// この値が BarHigh/BarLow に漏れて、即 TP/SL を踏む。
	const ticker24hHigh = 99999.0
	const ticker24hLow = 1.0

	// Tick 1: 単独 tick → BarHigh == BarLow == Last (8884.5).
	t1 := entity.Ticker{
		SymbolID:  7,
		Last:      8884.5,
		High:      ticker24hHigh,
		Low:       ticker24hLow,
		Volume:    100,
		Timestamp: base.UnixMilli(),
	}
	events := src.HandleTick(t1)
	tickEv := events[0].(entity.TickEvent)
	if tickEv.BarHigh != 8884.5 || tickEv.BarLow != 8884.5 {
		t.Fatalf("tick1: expected BarHigh=BarLow=8884.5 (single-tick bar), got high=%f low=%f", tickEv.BarHigh, tickEv.BarLow)
	}
	if tickEv.BarHigh == ticker24hHigh || tickEv.BarLow == ticker24hLow {
		t.Fatalf("tick1: BarHigh/BarLow leaked from 24h ticker (high=%f low=%f) — regression of Issue #266", tickEv.BarHigh, tickEv.BarLow)
	}

	// Tick 2: 同じバー内で上昇 → BarHigh 更新、BarLow は据え置き。
	t2 := entity.Ticker{
		SymbolID:  7,
		Last:      8890.0,
		High:      ticker24hHigh,
		Low:       ticker24hLow,
		Volume:    100,
		Timestamp: base.Add(1 * time.Minute).UnixMilli(),
	}
	events = src.HandleTick(t2)
	tickEv = events[0].(entity.TickEvent)
	if tickEv.BarHigh != 8890.0 {
		t.Fatalf("tick2: expected BarHigh=8890.0 (updated by this tick), got %f", tickEv.BarHigh)
	}
	if tickEv.BarLow != 8884.5 {
		t.Fatalf("tick2: expected BarLow=8884.5 (unchanged from tick1), got %f", tickEv.BarLow)
	}

	// Tick 3: 下落 → BarLow 更新、BarHigh は据え置き。
	t3 := entity.Ticker{
		SymbolID:  7,
		Last:      8830.0,
		High:      ticker24hHigh,
		Low:       ticker24hLow,
		Volume:    100,
		Timestamp: base.Add(2 * time.Minute).UnixMilli(),
	}
	events = src.HandleTick(t3)
	tickEv = events[0].(entity.TickEvent)
	if tickEv.BarHigh != 8890.0 {
		t.Fatalf("tick3: expected BarHigh=8890.0 (unchanged), got %f", tickEv.BarHigh)
	}
	if tickEv.BarLow != 8830.0 {
		t.Fatalf("tick3: expected BarLow=8830.0 (updated by this tick), got %f", tickEv.BarLow)
	}

	// 重要: TP=Last*1.022=9079.95 を 24h high=99999 が常に超えるが、
	// BarHigh は 8890 で止まっており TP は発火しない、というのが本修正の要点。
	tpPrice := 8884.5 * 1.022
	if tickEv.BarHigh >= tpPrice {
		t.Fatalf("tick3 BarHigh=%f would falsely trigger TP at %f — Issue #266 regression", tickEv.BarHigh, tpPrice)
	}
}

// TestLiveSource_HandleTick_BarRangeResetOnPeriodBoundary はバー跨ぎで
// BarHigh/BarLow が新バーから取り直されることを確認する。前バーの High/Low
// が次バー初回 tick に持ち越されると、新規エントリ直後の SL/TP 判定が
// 歪むため、このリセット挙動は契約として固定する。
func TestLiveSource_HandleTick_BarRangeResetOnPeriodBoundary(t *testing.T) {
	src := NewLiveSource(7, "PT15M")
	base := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)

	// Bar 1: 100 → 200 まで触る。
	_ = src.HandleTick(entity.Ticker{SymbolID: 7, Last: 100, Timestamp: base.UnixMilli()})
	_ = src.HandleTick(entity.Ticker{SymbolID: 7, Last: 200, Timestamp: base.Add(1 * time.Minute).UnixMilli()})

	// Bar 2 の最初の tick (バー跨ぎ) → BarHigh=BarLow=Last (50)。
	events := src.HandleTick(entity.Ticker{SymbolID: 7, Last: 50, Timestamp: base.Add(15 * time.Minute).UnixMilli()})
	tickEv := events[0].(entity.TickEvent)
	if tickEv.BarHigh != 50 || tickEv.BarLow != 50 {
		t.Fatalf("first tick of new bar: expected BarHigh=BarLow=50, got high=%f low=%f (prev bar leak?)", tickEv.BarHigh, tickEv.BarLow)
	}
}

// TestLiveSource_HandleTick_BarRangeAfterSeed は SeedFromMinuteCandles で
// 前 14 分の OHLC を流し込んだ後、最初の live tick が seed 由来の High/Low
// を引き継ぎ、24h ticker.High/Low に汚染されないことを固定する。再起動直後の
// SL/TP 判定が seed 範囲のみで行われ、24h レンジが混入しないという契約。
// Codex review (PR #267) で seed 経路に専用テストがないと指摘された。
func TestLiveSource_HandleTick_BarRangeAfterSeed(t *testing.T) {
	src := NewLiveSource(7, "PT15M")
	now := time.Date(2026, 4, 27, 10, 23, 0, 0, time.UTC)

	// Seed: 10:15-10:30 の現在進行中バーに 3 本の minute candle を畳む。
	// 期待される seeded current bar: Open=200, High=220, Low=190, Close=192.
	minutes := []entity.Candle{
		{Open: 200, High: 210, Low: 195, Close: 205, Volume: 2, Time: time.Date(2026, 4, 27, 10, 15, 0, 0, time.UTC).UnixMilli()},
		{Open: 205, High: 220, Low: 200, Close: 215, Volume: 3, Time: time.Date(2026, 4, 27, 10, 16, 0, 0, time.UTC).UnixMilli()},
		{Open: 215, High: 218, Low: 190, Close: 192, Volume: 4, Time: time.Date(2026, 4, 27, 10, 17, 0, 0, time.UTC).UnixMilli()},
	}
	if folded := src.SeedFromMinuteCandles(now, minutes); folded != 3 {
		t.Fatalf("folded count = %d, want 3", folded)
	}

	// 最初の live tick は seed 範囲の内側 (193)。BarHigh は seed の 220、
	// BarLow は seed の 190 を引き継ぐべき。24h ticker.High/Low (99999/1) は
	// 一切混入してはならない。
	const ticker24hHigh = 99999.0
	const ticker24hLow = 1.0
	tick := entity.Ticker{
		SymbolID:  7,
		Last:      193,
		High:      ticker24hHigh,
		Low:       ticker24hLow,
		Volume:    5,
		Timestamp: time.Date(2026, 4, 27, 10, 24, 0, 0, time.UTC).UnixMilli(),
	}
	events := src.HandleTick(tick)
	tickEv := events[0].(entity.TickEvent)

	if tickEv.BarHigh != 220 {
		t.Fatalf("post-seed first tick: BarHigh=%f, want 220 (seeded high)", tickEv.BarHigh)
	}
	if tickEv.BarLow != 190 {
		t.Fatalf("post-seed first tick: BarLow=%f, want 190 (seeded low)", tickEv.BarLow)
	}
	if tickEv.BarHigh == ticker24hHigh || tickEv.BarLow == ticker24hLow {
		t.Fatalf("post-seed first tick: 24h ticker range leaked (high=%f low=%f) — Issue #266 regression", tickEv.BarHigh, tickEv.BarLow)
	}

	// 続く live tick が seed 範囲外 (上抜け 225) → BarHigh のみ更新。
	tick2 := entity.Ticker{
		SymbolID:  7,
		Last:      225,
		High:      ticker24hHigh,
		Low:       ticker24hLow,
		Volume:    6,
		Timestamp: time.Date(2026, 4, 27, 10, 25, 0, 0, time.UTC).UnixMilli(),
	}
	events = src.HandleTick(tick2)
	tickEv = events[0].(entity.TickEvent)
	if tickEv.BarHigh != 225 {
		t.Fatalf("second post-seed tick: BarHigh=%f, want 225 (updated by this tick above seeded 220)", tickEv.BarHigh)
	}
	if tickEv.BarLow != 190 {
		t.Fatalf("second post-seed tick: BarLow=%f, want 190 (seeded low unchanged)", tickEv.BarLow)
	}
}

func TestLiveSource_SeedFromMinuteCandles_NoCandlesIsNoOp(t *testing.T) {
	src := NewLiveSource(7, "PT15M")
	now := time.Date(2026, 4, 27, 10, 23, 0, 0, time.UTC)
	folded := src.SeedFromMinuteCandles(now, nil)
	if folded != 0 {
		t.Errorf("nil minute slice should fold 0, got %d", folded)
	}
	// Builder must still behave normally on the next live tick (i.e.
	// initialise from the tick's price as before).
	events := src.HandleTick(entity.Ticker{
		SymbolID:  7,
		Last:      999,
		Timestamp: now.UnixMilli(),
	})
	if len(events) != 1 {
		t.Errorf("expected just TickEvent (no seed -> no candle), got %d", len(events))
	}
}
