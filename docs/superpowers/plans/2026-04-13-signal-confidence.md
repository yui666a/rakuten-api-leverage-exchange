# Signal Confidence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Confidence score (0.0–1.0) to every Signal so the pipeline can scale order size by conviction and skip low-confidence trades.

**Architecture:** Signal.Confidence is computed by the StrategyEngine as a weighted sum of indicator agreement factors. The pipeline uses a configurable `MinConfidence` threshold to filter signals and scales `tradeAmount` linearly between `MinConfidence` and 1.0.

**Tech Stack:** Go, existing indicator infrastructure, no new dependencies.

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `internal/domain/entity/signal.go` | Add `Confidence float64` field |
| Modify | `internal/usecase/strategy.go` | Compute confidence score in evaluateTrendFollow / evaluateContrarian |
| Modify | `cmd/pipeline.go` | Filter by MinConfidence, scale order amount |
| Modify | `config/config.go` | Add `TRADE_MIN_CONFIDENCE` env var (default 0.3) |
| Modify | `internal/usecase/strategy_test.go` | Add confidence assertions to existing + new tests |
| Modify | `cmd/pipeline_test.go` | Add confidence filtering test |

---

### Task 1: Add Confidence field to Signal entity

**Files:**
- Modify: `internal/domain/entity/signal.go`

- [ ] **Step 1: Add Confidence field**

```go
type Signal struct {
	SymbolID   int64        `json:"symbolId"`
	Action     SignalAction `json:"action"`
	Confidence float64      `json:"confidence"` // 0.0–1.0
	Reason     string       `json:"reason"`
	Timestamp  int64        `json:"timestamp"`
}
```

- [ ] **Step 2: Run existing tests to confirm no breakage**

Run: `cd backend && go test ./...`
Expected: All PASS (Confidence defaults to 0.0 which is harmless for existing logic)

- [ ] **Step 3: Commit**

```
feat(entity): add Confidence field to Signal
```

---

### Task 2: Add MinConfidence config

**Files:**
- Modify: `config/config.go`

- [ ] **Step 1: Add MinConfidence to Config**

Add field `MinConfidence float64` to the config struct and load from env `TRADE_MIN_CONFIDENCE` with default `0.3`.

- [ ] **Step 2: Run tests**

Run: `cd backend && go test ./...`
Expected: PASS

- [ ] **Step 3: Commit**

```
feat(config): add TRADE_MIN_CONFIDENCE env var
```

---

### Task 3: Compute confidence in StrategyEngine

**Files:**
- Modify: `internal/usecase/strategy.go`
- Test: `internal/usecase/strategy_test.go`

- [ ] **Step 1: Write failing tests for confidence scoring**

Add tests that assert:
- TrendFollow BUY with SMA divergence 2%, RSI 55, MACD histogram +5 → Confidence ~0.8
- TrendFollow BUY with SMA barely crossing, RSI 68, no histogram → Confidence ~0.35
- Contrarian BUY with RSI 20, histogram not against → Confidence ~0.7
- HOLD signals always have Confidence 0.0

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/usecase/ -run TestStrategyEngine_Confidence -v`
Expected: FAIL (Confidence is always 0.0)

- [ ] **Step 3: Implement confidence scoring**

TrendFollow confidence factors (each 0.0–1.0):
- `smaDivergence`: `min(abs(sma20-sma50)/sma50 * 100, 2.0) / 2.0` — how strong the cross is
- `rsiRoom`: for BUY `(70-rsi)/40`, for SELL `(rsi-30)/40` — distance from overbought/oversold
- `macdConfirm`: if histogram present and agrees, `min(abs(histogram)/10, 1.0)`, else 0.5

Final: `(smaDivergence*0.4 + rsiRoom*0.3 + macdConfirm*0.3)`

Contrarian confidence factors:
- `rsiExtreme`: for BUY `(30-rsi)/30`, for SELL `(rsi-70)/30` — how deep into extreme
- `macdNotAgainst`: if histogram present and not strongly against, `1.0 - min(abs(histogram)/20, 1.0)`, else 0.5

Final: `(rsiExtreme*0.6 + macdNotAgainst*0.4)`

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/usecase/ -run TestStrategyEngine -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```
feat(strategy): compute signal confidence from indicator agreement
```

---

### Task 4: Pipeline filters by confidence and scales amount

**Files:**
- Modify: `cmd/pipeline.go`
- Test: `cmd/pipeline_test.go`

- [ ] **Step 1: Add minConfidence to TradingPipeline and snapshot**

Add `minConfidence float64` field, loaded from config in NewTradingPipeline.

- [ ] **Step 2: Filter low-confidence signals in evaluate()**

After strategy evaluation, if `signal.Confidence < snap.minConfidence`, log and return (treat as HOLD).

- [ ] **Step 3: Scale order amount by confidence**

Replace `amount := snap.tradeAmount / price` with:
```go
scaledAmount := snap.tradeAmount * scaleByConfidence(signal.Confidence, snap.minConfidence)
amount := scaledAmount / price
```

Where `scaleByConfidence` linearly maps [minConfidence, 1.0] → [0.5, 1.0]:
```go
func scaleByConfidence(confidence, minConfidence float64) float64 {
    if confidence >= 1.0 {
        return 1.0
    }
    return 0.5 + 0.5*(confidence-minConfidence)/(1.0-minConfidence)
}
```

- [ ] **Step 4: Add test for confidence filtering**

- [ ] **Step 5: Run all tests**

Run: `cd backend && go test ./...`
Expected: All PASS

- [ ] **Step 6: Commit**

```
feat(pipeline): filter low-confidence signals and scale order amount
```

---

### Task 5: Add confidence to pipeline log output

**Files:**
- Modify: `cmd/pipeline.go`

- [ ] **Step 1: Add confidence to slog output**

Update the signal log line to include confidence:
```go
slog.Info("pipeline: signal evaluated", "action", signal.Action, "confidence", signal.Confidence, "reason", signal.Reason, "price", latestTicker.Last)
```

- [ ] **Step 2: Run tests**

Run: `cd backend && go test ./...`
Expected: All PASS

- [ ] **Step 3: Commit**

```
feat(pipeline): log signal confidence for observability
```
