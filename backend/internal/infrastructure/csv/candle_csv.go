package csv

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

const (
	csvHeaderSymbol   = "symbol"
	csvHeaderSymbolID = "symbol_id"
	csvHeaderInterval = "interval"
	csvHeaderTime     = "time"
	csvHeaderOpen     = "open"
	csvHeaderHigh     = "high"
	csvHeaderLow      = "low"
	csvHeaderClose    = "close"
	csvHeaderVolume   = "volume"
)

type CandleFile struct {
	Symbol   string
	SymbolID int64
	Interval string
	Candles  []entity.Candle
}

func LoadCandles(path string) (*CandleFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = 9

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	expected := []string{
		csvHeaderSymbol, csvHeaderSymbolID, csvHeaderInterval, csvHeaderTime,
		csvHeaderOpen, csvHeaderHigh, csvHeaderLow, csvHeaderClose, csvHeaderVolume,
	}
	for i := range expected {
		if header[i] != expected[i] {
			return nil, fmt.Errorf("invalid header at %d: got=%s want=%s", i, header[i], expected[i])
		}
	}

	out := &CandleFile{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}

		symbolID, err := strconv.ParseInt(row[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse symbol_id: %w", err)
		}
		ts, err := strconv.ParseInt(row[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse time: %w", err)
		}
		open, err := strconv.ParseFloat(row[4], 64)
		if err != nil {
			return nil, fmt.Errorf("parse open: %w", err)
		}
		high, err := strconv.ParseFloat(row[5], 64)
		if err != nil {
			return nil, fmt.Errorf("parse high: %w", err)
		}
		low, err := strconv.ParseFloat(row[6], 64)
		if err != nil {
			return nil, fmt.Errorf("parse low: %w", err)
		}
		closePrice, err := strconv.ParseFloat(row[7], 64)
		if err != nil {
			return nil, fmt.Errorf("parse close: %w", err)
		}
		volume, err := strconv.ParseFloat(row[8], 64)
		if err != nil {
			return nil, fmt.Errorf("parse volume: %w", err)
		}

		if out.Symbol == "" {
			out.Symbol = row[0]
			out.SymbolID = symbolID
			out.Interval = row[2]
		}

		out.Candles = append(out.Candles, entity.Candle{
			Open:   open,
			High:   high,
			Low:    low,
			Close:  closePrice,
			Volume: volume,
			Time:   ts,
		})
	}

	sort.Slice(out.Candles, func(i, j int) bool {
		return out.Candles[i].Time < out.Candles[j].Time
	})
	return out, nil
}

func SaveCandles(path string, file CandleFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	sort.Slice(file.Candles, func(i, j int) bool {
		return file.Candles[i].Time < file.Candles[j].Time
	})

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create csv: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{
		csvHeaderSymbol, csvHeaderSymbolID, csvHeaderInterval, csvHeaderTime,
		csvHeaderOpen, csvHeaderHigh, csvHeaderLow, csvHeaderClose, csvHeaderVolume,
	}); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	for _, c := range file.Candles {
		if err := w.Write([]string{
			file.Symbol,
			strconv.FormatInt(file.SymbolID, 10),
			file.Interval,
			strconv.FormatInt(c.Time, 10),
			strconv.FormatFloat(c.Open, 'f', -1, 64),
			strconv.FormatFloat(c.High, 'f', -1, 64),
			strconv.FormatFloat(c.Low, 'f', -1, 64),
			strconv.FormatFloat(c.Close, 'f', -1, 64),
			strconv.FormatFloat(c.Volume, 'f', -1, 64),
		}); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
	}
	if err := w.Error(); err != nil {
		return fmt.Errorf("flush writer: %w", err)
	}
	return nil
}

func LatestTimestamp(path string) (int64, error) {
	file, err := LoadCandles(path)
	if err != nil {
		return 0, err
	}
	if len(file.Candles) == 0 {
		return 0, nil
	}
	return file.Candles[len(file.Candles)-1].Time, nil
}
