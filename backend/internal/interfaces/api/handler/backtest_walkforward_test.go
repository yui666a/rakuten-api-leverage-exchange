package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	csvinfra "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/csv"
	bt "github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/backtest"
)

// readBaselineProfileJSON loads backend/profiles/baseline.json via path
// walk-up; mirrors readProductionProfileJSON in backtest_test.go.
func readBaselineProfileJSON(t *testing.T) []byte {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			data, err := os.ReadFile(filepath.Join(dir, "profiles", "baseline.json"))
			if err != nil {
				t.Fatalf("read baseline.json: %v", err)
			}
			return data
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// makeCSVForWalkForwardTests generates ~10mo of synthetic 15m candles with
// enough cyclic variance that the strategy engine actually fires trades
// (otherwise both TP-override branches produce identical zero-trade
// summaries and the regression test can't distinguish them).
func makeCSVForWalkForwardTests(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	n := 30000
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := make([]entity.Candle, 0, n)
	// Cyclic price path with amplitude ~4% over ~200-bar cycles — rich
	// enough that RSI / MACD / BB produce genuine crossovers.
	price := 100.0
	for i := 0; i < n; i++ {
		// Two overlapping sines at different periods to avoid strict
		// periodicity and give the indicators varied shape.
		phase1 := float64(i) / 40.0
		phase2 := float64(i) / 137.0
		target := 100.0 + 4.0*sinFast(phase1) + 1.5*sinFast(phase2)
		// First-order low-pass toward target so prices don't jump.
		price += (target - price) * 0.15
		ts := start.Add(time.Duration(i) * 15 * time.Minute)
		candles = append(candles, entity.Candle{
			Open: price - 0.1, High: price + 0.3, Low: price - 0.3, Close: price,
			Volume: 1.0, Time: ts.UnixMilli(),
		})
	}
	return writeTempCSV(t, tmpDir, csvinfra.CandleFile{
		Symbol: "LTC_JPY", SymbolID: 10, Interval: "PT15M", Candles: candles,
	})
}

// sinFast is a small tabular sine — avoids importing math just for the
// fixture generator. Accurate enough for test data.
func sinFast(x float64) float64 {
	// Reduce to [-π, π] roughly.
	for x > 3.14159 {
		x -= 6.2832
	}
	for x < -3.14159 {
		x += 6.2832
	}
	// Bhaskara I approximation (good to ~1e-3 on [-π, π]).
	return (16 * x * (3.14159 - x)) / (49.348 - 4*x*(3.14159-x))
}

// newWalkForwardRouter wires a BacktestHandler for POST
// /backtest/walk-forward tests. No repo needed: the walk-forward MVP
// doesn't persist.
func newWalkForwardRouter(t *testing.T, profilesDir string) (*gin.Engine, *BacktestHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var opts []BacktestHandlerOption
	if profilesDir != "" {
		opts = append(opts, WithProfilesBaseDir(profilesDir))
	}
	h := NewBacktestHandler(bt.NewBacktestRunner(), &mockBacktestResultRepo{}, opts...)
	router := gin.New()
	router.POST("/backtest/walk-forward", h.RunWalkForward)
	return router, h
}

// The handler validations below do NOT require a real backtest run — they
// should reject the request before ExpandGrid/ComputeWindows/runner are
// reached. A minimal BacktestHandler is enough.

func newWalkForwardHandler(t *testing.T) *BacktestHandler {
	t.Helper()
	return NewBacktestHandler(
		bt.NewBacktestRunner(),
		&mockBacktestResultRepo{},
	)
}

// TestWalkForward_RejectsInvalidObjective locks in the Codex PR-13
// follow-up: a typo in the objective name must surface as 400, not
// silently fall back to TotalReturn inside SelectByObjective.
func TestWalkForward_RejectsInvalidObjective(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newWalkForwardHandler(t)
	r := gin.New()
	r.POST("/backtest/walk-forward", h.RunWalkForward)

	body := `{
		"data": "x.csv",
		"from": "2024-01-01",
		"to":   "2024-12-01",
		"baseProfile": "production",
		"objective":   "returns"
	}`
	req := httptest.NewRequest(http.MethodPost, "/backtest/walk-forward", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid objective") {
		t.Fatalf("body should mention invalid objective, got: %s", w.Body.String())
	}
}

// TestWalkForward_RiskOverrideReachesRunner is the Codex PR-117 regression
// lock: POST /backtest/walk-forward with a grid entry on
// strategy_risk.take_profit_percent must actually change the IS result.
//
// The bug that triggered this test shipped the handler building RiskConfig
// from the request-level `shared` struct instead of the per-combo profile,
// which meant every TP axis entry was silently ignored. Here we run a
// tiny 1-window sweep on two very different TP values and assert that the
// two IS summaries are NOT identical. If the regression comes back this
// test will fail immediately.
func TestWalkForward_RiskOverrideReachesRunner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profilesDir := setupProfilesDir(t, map[string][]byte{"baseline": readBaselineProfileJSON(t)})
	csvPath := makeCSVForWalkForwardTests(t)

	router, _ := newWalkForwardRouter(t, profilesDir)

	// 1 year of synthetic 15-minute candles is enough to slice into one
	// IS(6mo) + OOS(3mo) window. The grid has exactly two TP values; if
	// either produces the same IS summary, the override isn't reaching
	// the runner.
	body := `{
		"data": "` + csvPath + `",
		"from": "2024-01-01",
		"to":   "2024-10-01",
		"inSampleMonths": 6,
		"outOfSampleMonths": 3,
		"stepMonths": 3,
		"baseProfile": "baseline",
		"objective": "return",
		"parameterGrid": [
			{"path":"strategy_risk.take_profit_percent","values":[1, 20]},
			{"path":"strategy_risk.stop_loss_percent","values":[1, 20]}
		],
		"initialBalance": 100000,
		"tradeAmount": 0.01
	}`
	req := httptest.NewRequest(http.MethodPost, "/backtest/walk-forward", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Windows []struct {
			ISResults []struct {
				Parameters map[string]float64 `json:"parameters"`
				Summary    struct {
					TotalReturn float64 `json:"totalReturn"`
					TotalTrades int     `json:"totalTrades"`
				} `json:"summary"`
			} `json:"isResults"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Windows) == 0 {
		t.Fatalf("no windows returned")
	}
	// 2x2 grid -> 4 combos. Before the fix every combo used the same shared
	// TP/SL and all four summaries were byte-identical; after the fix, each
	// distinct (TP, SL) pair yields a distinct summary. We just assert that
	// the total number of distinct (TotalTrades, TotalReturn) pairs is > 1,
	// which is the strongest "some override reached the loop" signal that
	// doesn't depend on knowing which combo wins.
	results := resp.Windows[0].ISResults
	if len(results) != 4 {
		t.Fatalf("IS results len = %d, want 4", len(results))
	}
	distinct := map[[2]float64]bool{}
	for _, r := range results {
		key := [2]float64{r.Summary.TotalReturn, float64(r.Summary.TotalTrades)}
		distinct[key] = true
	}
	if len(distinct) < 2 {
		t.Fatalf("all 4 IS combos produced identical summaries — risk override not reaching the runner. results=%+v", results)
	}
}

// TestWalkForward_RejectsUnknownOverridePath locks in the follow-up: an
// unknown override path must be a 400 (caller error), not a 500 from
// ApplyOverrides failing mid-run.
func TestWalkForward_RejectsUnknownOverridePath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newWalkForwardHandler(t)
	r := gin.New()
	r.POST("/backtest/walk-forward", h.RunWalkForward)

	body := `{
		"data": "x.csv",
		"from": "2024-01-01",
		"to":   "2024-12-01",
		"baseProfile": "production",
		"objective":   "return",
		"parameterGrid": [{"path": "not.a.real.path", "values": [1, 2]}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/backtest/walk-forward", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unsupported override path") {
		t.Fatalf("body should mention unsupported path, got: %s", w.Body.String())
	}
}
