package csv

import (
	"path/filepath"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestSaveLoadCandles_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "candles.csv")

	err := SaveCandles(path, CandleFile{
		Symbol:   "BTC_JPY",
		SymbolID: 7,
		Interval: "PT15M",
		Candles: []entity.Candle{
			{Open: 2, High: 3, Low: 1, Close: 2.5, Volume: 10, Time: 2000},
			{Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 5, Time: 1000},
		},
	})
	if err != nil {
		t.Fatalf("save error: %v", err)
	}

	got, err := LoadCandles(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if got.Symbol != "BTC_JPY" || got.SymbolID != 7 || got.Interval != "PT15M" {
		t.Fatalf("metadata mismatch: %+v", got)
	}
	if len(got.Candles) != 2 {
		t.Fatalf("expected 2 candles, got %d", len(got.Candles))
	}
	if got.Candles[0].Time != 1000 || got.Candles[1].Time != 2000 {
		t.Fatalf("expected sorted candles, got %+v", got.Candles)
	}
}

func TestLatestTimestamp(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "candles.csv")

	err := SaveCandles(path, CandleFile{
		Symbol:   "BTC_JPY",
		SymbolID: 7,
		Interval: "PT1H",
		Candles: []entity.Candle{
			{Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 5, Time: 1000},
			{Open: 2, High: 3, Low: 1, Close: 2.5, Volume: 10, Time: 2000},
		},
	})
	if err != nil {
		t.Fatalf("save error: %v", err)
	}

	ts, err := LatestTimestamp(path)
	if err != nil {
		t.Fatalf("latest timestamp error: %v", err)
	}
	if ts != 2000 {
		t.Fatalf("expected 2000, got %d", ts)
	}
}
