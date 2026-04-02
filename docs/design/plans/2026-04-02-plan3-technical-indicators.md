# Plan 3: テクニカル指標計算 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ローソク足データからRSI, MACD, 移動平均（SMA/EMA）を計算するモジュールを構築し、Indicator Calculatorユースケースとして統合する

**Architecture:** テクニカル指標の計算ロジックはインフラ層に純粋関数として実装する（外部依存なし、テストしやすい）。Indicator Calculatorはユースケース層に配置し、MarketDataServiceから価格データを取得して指標を計算する。計算結果はドメインエンティティとして定義する。

**Tech Stack:** Go 1.21, 標準ライブラリのみ（外部依存なし）

---

## ファイル構成

```
backend/internal/
├── domain/
│   └── entity/
│       └── indicator.go                    # 指標のエンティティ定義
├── usecase/
│   ├── indicator.go                        # Indicator Calculator
│   └── indicator_test.go
└── infrastructure/
    └── indicator/
        ├── sma.go                          # 単純移動平均
        ├── sma_test.go
        ├── ema.go                          # 指数移動平均
        ├── ema_test.go
        ├── rsi.go                          # RSI
        ├── rsi_test.go
        ├── macd.go                         # MACD
        └── macd_test.go
```

---

### Task 1: 指標エンティティの定義

**Files:**
- Create: `backend/internal/domain/entity/indicator.go`

- [ ] **Step 1: indicator.go を作成**

```go
package entity

// IndicatorSet はある時点の全テクニカル指標をまとめた構造体。
type IndicatorSet struct {
	SymbolID  int64   `json:"symbolId"`
	SMA20     float64 `json:"sma20"`
	SMA50     float64 `json:"sma50"`
	EMA12     float64 `json:"ema12"`
	EMA26     float64 `json:"ema26"`
	RSI14     float64 `json:"rsi14"`
	MACDLine  float64 `json:"macdLine"`
	SignalLine float64 `json:"signalLine"`
	Histogram float64 `json:"histogram"`
	Timestamp int64   `json:"timestamp"`
}
```

- [ ] **Step 2: ビルド確認**

```bash
cd backend
go build ./...
```

Expected: ビルド成功

- [ ] **Step 3: コミット**

```bash
git add -A
git commit -m "feat: add IndicatorSet entity for technical indicators"
```

---

### Task 2: SMA（単純移動平均）

**Files:**
- Create: `backend/internal/infrastructure/indicator/sma.go`
- Create: `backend/internal/infrastructure/indicator/sma_test.go`

- [ ] **Step 1: sma_test.go を書く**

```go
package indicator

import (
	"math"
	"testing"
)

func TestSMA_Basic(t *testing.T) {
	prices := []float64{10, 20, 30, 40, 50}
	result := SMA(prices, 5)
	if result != 30 {
		t.Fatalf("expected SMA=30, got %f", result)
	}
}

func TestSMA_Period3(t *testing.T) {
	prices := []float64{10, 20, 30, 40, 50}
	// SMA(3) uses last 3 values: 30, 40, 50
	result := SMA(prices, 3)
	expected := 40.0
	if result != expected {
		t.Fatalf("expected SMA=%f, got %f", expected, result)
	}
}

func TestSMA_InsufficientData(t *testing.T) {
	prices := []float64{10, 20}
	result := SMA(prices, 5)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for insufficient data, got %f", result)
	}
}

func TestSMA_EmptyInput(t *testing.T) {
	result := SMA([]float64{}, 5)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for empty input, got %f", result)
	}
}

func TestSMASeries(t *testing.T) {
	prices := []float64{10, 20, 30, 40, 50, 60}
	result := SMASeries(prices, 3)
	// Expects 4 values: SMA of [10,20,30], [20,30,40], [30,40,50], [40,50,60]
	expected := []float64{20, 30, 40, 50}
	if len(result) != len(expected) {
		t.Fatalf("expected %d values, got %d", len(expected), len(result))
	}
	for i, v := range result {
		if math.Abs(v-expected[i]) > 0.0001 {
			t.Fatalf("index %d: expected %f, got %f", i, expected[i], v)
		}
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd backend
go test ./internal/infrastructure/indicator/ -v -run TestSMA
```

Expected: コンパイルエラー

- [ ] **Step 3: sma.go を実装**

```go
package indicator

import "math"

// SMA は直近period件の終値から単純移動平均を計算する。
// データ不足の場合はNaNを返す。
func SMA(prices []float64, period int) float64 {
	if len(prices) < period {
		return math.NaN()
	}
	sum := 0.0
	for _, p := range prices[len(prices)-period:] {
		sum += p
	}
	return sum / float64(period)
}

// SMASeries は価格系列全体のSMA系列を計算する。
// 返り値の長さは len(prices) - period + 1。
func SMASeries(prices []float64, period int) []float64 {
	if len(prices) < period {
		return nil
	}
	result := make([]float64, 0, len(prices)-period+1)
	for i := period - 1; i < len(prices); i++ {
		sum := 0.0
		for j := i - period + 1; j <= i; j++ {
			sum += prices[j]
		}
		result = append(result, sum/float64(period))
	}
	return result
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd backend
go test ./internal/infrastructure/indicator/ -v -run TestSMA
```

Expected: 全テストPASS

- [ ] **Step 5: コミット**

```bash
git add -A
git commit -m "feat: add SMA (Simple Moving Average) calculation"
```

---

### Task 3: EMA（指数移動平均）

**Files:**
- Create: `backend/internal/infrastructure/indicator/ema.go`
- Create: `backend/internal/infrastructure/indicator/ema_test.go`

- [ ] **Step 1: ema_test.go を書く**

```go
package indicator

import (
	"math"
	"testing"
)

func TestEMA_Basic(t *testing.T) {
	prices := []float64{10, 20, 30, 40, 50}
	result := EMA(prices, 5)
	if math.IsNaN(result) {
		t.Fatal("expected valid EMA, got NaN")
	}
}

func TestEMA_InsufficientData(t *testing.T) {
	prices := []float64{10, 20}
	result := EMA(prices, 5)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for insufficient data, got %f", result)
	}
}

func TestEMA_FirstValueIsSMA(t *testing.T) {
	prices := []float64{10, 20, 30}
	// EMA(3) の初期値は SMA(3) = 20
	// 入力がperiodと同じ長さなら、EMA = SMA
	result := EMA(prices, 3)
	if result != 20 {
		t.Fatalf("expected EMA=20 (same as SMA for period-length input), got %f", result)
	}
}

func TestEMA_MoreWeightOnRecent(t *testing.T) {
	prices := []float64{10, 10, 10, 10, 50}
	emaVal := EMA(prices, 5)
	smaVal := SMA(prices, 5) // SMA = 18
	// EMA should be higher than SMA because recent price (50) is much higher
	if emaVal <= smaVal {
		t.Fatalf("expected EMA > SMA for rising prices, got EMA=%f SMA=%f", emaVal, smaVal)
	}
}

func TestEMASeries(t *testing.T) {
	prices := []float64{10, 20, 30, 40, 50, 60}
	result := EMASeries(prices, 3)
	// First value is SMA(3) of [10,20,30] = 20
	// Then EMA is applied to remaining values
	if len(result) != 4 {
		t.Fatalf("expected 4 values, got %d", len(result))
	}
	if result[0] != 20 {
		t.Fatalf("first EMA should be SMA=20, got %f", result[0])
	}
	// Each subsequent value should be larger (prices are rising)
	for i := 1; i < len(result); i++ {
		if result[i] <= result[i-1] {
			t.Fatalf("EMA should be rising at index %d: %f <= %f", i, result[i], result[i-1])
		}
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

- [ ] **Step 3: ema.go を実装**

```go
package indicator

import "math"

// EMA は直近のデータから指数移動平均を計算する。
// 初期値はSMAを使用する。データ不足の場合はNaNを返す。
func EMA(prices []float64, period int) float64 {
	series := EMASeries(prices, period)
	if len(series) == 0 {
		return math.NaN()
	}
	return series[len(series)-1]
}

// EMASeries は価格系列全体のEMA系列を計算する。
// 初期値はSMA、以降は multiplier = 2/(period+1) で指数的に重み付け。
func EMASeries(prices []float64, period int) []float64 {
	if len(prices) < period {
		return nil
	}

	multiplier := 2.0 / float64(period+1)
	result := make([]float64, 0, len(prices)-period+1)

	// 初期値はSMA
	sma := SMA(prices[:period], period)
	result = append(result, sma)

	// 以降はEMA計算
	for i := period; i < len(prices); i++ {
		prev := result[len(result)-1]
		ema := (prices[i]-prev)*multiplier + prev
		result = append(result, ema)
	}

	return result
}
```

- [ ] **Step 4: テストが通ることを確認**

- [ ] **Step 5: コミット**

```bash
git add -A
git commit -m "feat: add EMA (Exponential Moving Average) calculation"
```

---

### Task 4: RSI

**Files:**
- Create: `backend/internal/infrastructure/indicator/rsi.go`
- Create: `backend/internal/infrastructure/indicator/rsi_test.go`

- [ ] **Step 1: rsi_test.go を書く**

```go
package indicator

import (
	"math"
	"testing"
)

func TestRSI_AllGains(t *testing.T) {
	// Monotonically increasing prices -> RSI should be 100
	prices := make([]float64, 15)
	for i := range prices {
		prices[i] = float64(i + 1)
	}
	result := RSI(prices, 14)
	if result != 100 {
		t.Fatalf("expected RSI=100 for all gains, got %f", result)
	}
}

func TestRSI_AllLosses(t *testing.T) {
	// Monotonically decreasing prices -> RSI should be 0
	prices := make([]float64, 15)
	for i := range prices {
		prices[i] = float64(15 - i)
	}
	result := RSI(prices, 14)
	if result != 0 {
		t.Fatalf("expected RSI=0 for all losses, got %f", result)
	}
}

func TestRSI_InsufficientData(t *testing.T) {
	prices := []float64{10, 20, 30}
	result := RSI(prices, 14)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for insufficient data, got %f", result)
	}
}

func TestRSI_Range(t *testing.T) {
	prices := []float64{44, 44.34, 44.09, 43.61, 44.33, 44.83, 45.10, 45.42, 45.84, 46.08, 45.89, 46.03, 45.61, 46.28, 46.28}
	result := RSI(prices, 14)
	if result < 0 || result > 100 {
		t.Fatalf("RSI should be between 0 and 100, got %f", result)
	}
}

func TestRSI_MidRange(t *testing.T) {
	// Equal gains and losses -> RSI should be ~50
	prices := []float64{10, 11, 10, 11, 10, 11, 10, 11, 10, 11, 10, 11, 10, 11, 10}
	result := RSI(prices, 14)
	if math.Abs(result-50) > 5 {
		t.Fatalf("expected RSI near 50 for alternating prices, got %f", result)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

- [ ] **Step 3: rsi.go を実装**

```go
package indicator

import "math"

// RSI はRelative Strength Indexを計算する。
// Wilderの平滑化法を使用。prices は period+1 件以上必要。
// 結果は 0〜100 の範囲。データ不足の場合は NaN を返す。
func RSI(prices []float64, period int) float64 {
	if len(prices) < period+1 {
		return math.NaN()
	}

	gains := 0.0
	losses := 0.0

	// 最初のperiod期間の平均利益・平均損失を計算
	for i := 1; i <= period; i++ {
		change := prices[i] - prices[i-1]
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	// 残りのデータでWilderの平滑化
	for i := period + 1; i < len(prices); i++ {
		change := prices[i] - prices[i-1]
		if change > 0 {
			avgGain = (avgGain*float64(period-1) + change) / float64(period)
			avgLoss = (avgLoss * float64(period-1)) / float64(period)
		} else {
			avgGain = (avgGain * float64(period-1)) / float64(period)
			avgLoss = (avgLoss*float64(period-1) - change) / float64(period)
		}
	}

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}
```

- [ ] **Step 4: テストが通ることを確認**

- [ ] **Step 5: コミット**

```bash
git add -A
git commit -m "feat: add RSI (Relative Strength Index) calculation"
```

---

### Task 5: MACD

**Files:**
- Create: `backend/internal/infrastructure/indicator/macd.go`
- Create: `backend/internal/infrastructure/indicator/macd_test.go`

- [ ] **Step 1: macd_test.go を書く**

```go
package indicator

import (
	"math"
	"testing"
)

func TestMACD_Basic(t *testing.T) {
	// 35 data points minimum needed (26 for slow EMA + 9 for signal)
	prices := make([]float64, 35)
	for i := range prices {
		prices[i] = float64(100 + i)
	}

	macdLine, signalLine, histogram := MACD(prices, 12, 26, 9)

	if math.IsNaN(macdLine) {
		t.Fatal("expected valid MACD line, got NaN")
	}
	if math.IsNaN(signalLine) {
		t.Fatal("expected valid signal line, got NaN")
	}
	if math.IsNaN(histogram) {
		t.Fatal("expected valid histogram, got NaN")
	}

	// Histogram = MACD line - Signal line
	if math.Abs(histogram-(macdLine-signalLine)) > 0.0001 {
		t.Fatalf("histogram should be MACD-Signal, got %f", histogram)
	}
}

func TestMACD_InsufficientData(t *testing.T) {
	prices := make([]float64, 10)
	macdLine, signalLine, histogram := MACD(prices, 12, 26, 9)
	if !math.IsNaN(macdLine) || !math.IsNaN(signalLine) || !math.IsNaN(histogram) {
		t.Fatal("expected NaN for insufficient data")
	}
}

func TestMACD_RisingPrices(t *testing.T) {
	prices := make([]float64, 40)
	for i := range prices {
		prices[i] = float64(100 + i*2)
	}
	macdLine, _, _ := MACD(prices, 12, 26, 9)
	// For steadily rising prices, fast EMA > slow EMA, so MACD should be positive
	if macdLine <= 0 {
		t.Fatalf("expected positive MACD for rising prices, got %f", macdLine)
	}
}

func TestMACD_FallingPrices(t *testing.T) {
	prices := make([]float64, 40)
	for i := range prices {
		prices[i] = float64(200 - i*2)
	}
	macdLine, _, _ := MACD(prices, 12, 26, 9)
	// For steadily falling prices, fast EMA < slow EMA, so MACD should be negative
	if macdLine >= 0 {
		t.Fatalf("expected negative MACD for falling prices, got %f", macdLine)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

- [ ] **Step 3: macd.go を実装**

```go
package indicator

import "math"

// MACD はMACD Line、Signal Line、Histogramを計算する。
// 標準パラメータ: fast=12, slow=26, signal=9。
// データ不足の場合はすべてNaNを返す。
func MACD(prices []float64, fastPeriod, slowPeriod, signalPeriod int) (macdLine, signalLine, histogram float64) {
	fastEMA := EMASeries(prices, fastPeriod)
	slowEMA := EMASeries(prices, slowPeriod)

	if len(fastEMA) == 0 || len(slowEMA) == 0 {
		return math.NaN(), math.NaN(), math.NaN()
	}

	// MACD Line = Fast EMA - Slow EMA
	// fastEMAとslowEMAの長さが異なるため、末尾を揃える
	offset := len(fastEMA) - len(slowEMA)
	if offset < 0 {
		return math.NaN(), math.NaN(), math.NaN()
	}

	macdSeries := make([]float64, len(slowEMA))
	for i := range slowEMA {
		macdSeries[i] = fastEMA[i+offset] - slowEMA[i]
	}

	// Signal Line = MACD LineのEMA
	signalSeries := EMASeries(macdSeries, signalPeriod)
	if len(signalSeries) == 0 {
		return math.NaN(), math.NaN(), math.NaN()
	}

	macdLine = macdSeries[len(macdSeries)-1]
	signalLine = signalSeries[len(signalSeries)-1]
	histogram = macdLine - signalLine

	return macdLine, signalLine, histogram
}
```

- [ ] **Step 4: テストが通ることを確認**

- [ ] **Step 5: コミット**

```bash
git add -A
git commit -m "feat: add MACD (Moving Average Convergence Divergence) calculation"
```

---

### Task 6: Indicator Calculator（ユースケース層）

**Files:**
- Create: `backend/internal/usecase/indicator.go`
- Create: `backend/internal/usecase/indicator_test.go`

- [ ] **Step 1: indicator_test.go を書く**

```go
package usecase

import (
	"context"
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestIndicatorCalculator_Calculate(t *testing.T) {
	repo := newMockRepo()
	ctx := context.Background()

	// 50件のローソク足を生成（SMA50に必要）
	candles := make([]entity.Candle, 50)
	for i := range candles {
		candles[i] = entity.Candle{
			Close: float64(100 + i),
			Time:  int64(1700000000000 + i*60000),
		}
	}
	_ = repo.SaveCandles(ctx, 7, "PT1M", candles)

	calc := NewIndicatorCalculator(repo)

	result, err := calc.Calculate(ctx, 7, "PT1M")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.SymbolID != 7 {
		t.Fatalf("expected symbolID 7, got %d", result.SymbolID)
	}

	if math.IsNaN(result.SMA20) {
		t.Fatal("SMA20 should not be NaN with 50 data points")
	}

	if math.IsNaN(result.SMA50) {
		t.Fatal("SMA50 should not be NaN with 50 data points")
	}

	if math.IsNaN(result.RSI14) {
		t.Fatal("RSI14 should not be NaN with 50 data points")
	}

	if result.RSI14 < 0 || result.RSI14 > 100 {
		t.Fatalf("RSI should be 0-100, got %f", result.RSI14)
	}
}

func TestIndicatorCalculator_InsufficientData(t *testing.T) {
	repo := newMockRepo()
	ctx := context.Background()

	// Only 5 candles - not enough for any meaningful indicator
	candles := make([]entity.Candle, 5)
	for i := range candles {
		candles[i] = entity.Candle{
			Close: float64(100 + i),
			Time:  int64(1700000000000 + i*60000),
		}
	}
	_ = repo.SaveCandles(ctx, 7, "PT1M", candles)

	calc := NewIndicatorCalculator(repo)

	result, err := calc.Calculate(ctx, 7, "PT1M")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SMA20 requires 20 data points, so should be NaN
	if !math.IsNaN(result.SMA20) {
		t.Fatalf("SMA20 should be NaN with only 5 data points, got %f", result.SMA20)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

- [ ] **Step 3: indicator.go を実装**

```go
package usecase

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/indicator"
)

// IndicatorCalculator はローソク足データからテクニカル指標を計算する。
type IndicatorCalculator struct {
	repo repository.MarketDataRepository
}

func NewIndicatorCalculator(repo repository.MarketDataRepository) *IndicatorCalculator {
	return &IndicatorCalculator{repo: repo}
}

// Calculate は指定銘柄・時間足のテクニカル指標を計算する。
// 必要なローソク足をリポジトリから取得し、各指標を算出する。
func (c *IndicatorCalculator) Calculate(ctx context.Context, symbolID int64, interval string) (*entity.IndicatorSet, error) {
	// MACDのシグナルライン計算に最低 26+9=35 件、SMA50に50件必要
	// 余裕を持って100件取得
	candles, err := c.repo.GetCandles(ctx, symbolID, interval, 100)
	if err != nil {
		return nil, err
	}

	// GetCandlesは新しい順なので、古い順に反転
	prices := make([]float64, len(candles))
	for i, cd := range candles {
		prices[len(candles)-1-i] = cd.Close
	}

	var timestamp int64
	if len(candles) > 0 {
		timestamp = candles[0].Time // 最新のローソク足のタイムスタンプ
	}

	result := &entity.IndicatorSet{
		SymbolID:   symbolID,
		SMA20:      indicator.SMA(prices, 20),
		SMA50:      indicator.SMA(prices, 50),
		EMA12:      indicator.EMA(prices, 12),
		EMA26:      indicator.EMA(prices, 26),
		RSI14:      indicator.RSI(prices, 14),
		Timestamp:  timestamp,
	}

	macdLine, signalLine, histogram := indicator.MACD(prices, 12, 26, 9)
	result.MACDLine = macdLine
	result.SignalLine = signalLine
	result.Histogram = histogram

	return result, nil
}
```

- [ ] **Step 4: テストが通ることを確認**

- [ ] **Step 5: 全テストを実行して回帰がないことを確認**

```bash
cd backend
go test ./... -v
```

Expected: 全テストPASS

- [ ] **Step 6: コミット**

```bash
git add -A
git commit -m "feat: add IndicatorCalculator to compute technical indicators from candles"
```
