# confidence sizing の一元化と意味論の再定義 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** confidence scaling を `ConfidenceScale` 公開関数 1 箇所に集約し、`confidence_floor` を設定化、`risk_pct` の意味論を「ベース予算」に正し、live/backtest で confidence scaling を真に同値にする。

**Architecture:** sizer 内のインライン confidence 式を純粋公開関数 `ConfidenceScale` に抽出し、`Sizer.Compute` と legacy `cmd/pipeline.go` の両方をそこへ集約。`entity.PositionSizingConfig` に `ConfidenceFloor`（`(0,1]`、`0=unset→0.5`）を追加。backtest の 5 入口に `MinConfidence`（環境変数 `TRADE_MIN_CONFIDENCE` 既定 0.3 由来）を配線し、live と同値にする。

**Tech Stack:** Go 1.25 / 既存 `positionsize` パッケージ / table-driven test / `go test ./... -race -count=1`

**設計書:** `docs/superpowers/specs/2026-06-10-confidence-sizing-unification-design.md`

---

## File Structure

- `internal/usecase/positionsize/confidence.go` — **新規**。`ConfidenceScale` 公開純粋関数（single source of truth）。
- `internal/usecase/positionsize/confidence_test.go` — **新規**。`ConfidenceScale` の table-driven test。
- `internal/usecase/positionsize/sizer.go` — **修正**。インライン confidence 式（183-195 行）を `ConfidenceScale` 呼び出しへ置換。パッケージコメントの意味論更新。
- `internal/usecase/positionsize/sizer_test.go` — **修正**。`ConfidenceFloor` 関連テスト追加。
- `internal/domain/entity/strategy_config.go` — **修正**。`ConfidenceFloor` フィールド追加 + `Validate()` 範囲チェック。
- `internal/domain/entity/strategy_config_test.go` — **修正/確認**。`Validate()` テスト追加。
- `cmd/pipeline.go` — **修正**。重複 `scaleByConfidence`（709 行）削除、`positionsize.ConfidenceScale` へ置換。コメント（60, 114 行）更新。
- `internal/interfaces/api/handler/backtest.go` — **修正**。`WithMinConfidence` option 追加、単発 API の `RunInput` へ配線。
- `internal/interfaces/api/handler/backtest_multi.go` — **修正**。複数期間 API の `RunInput` へ配線。
- `internal/interfaces/api/handler/backtest_walkforward.go` — **修正**。WFO API の `RunInput` へ配線。
- `internal/interfaces/api/router.go` — **修正**。composition root で `WithMinConfidence(cfg.Trading.MinConfidence)` を注入。
- `cmd/backtest/main.go` — **修正**。`buildRunInput` に `cfg` を渡し CLI 単発/optimize/refine の `RunInput` へ配線。
- `cmd/backtest/walkforward.go` — **修正**。CLI WFO の `RunInput` へ配線（クロージャ内の `cfg` を利用）。

---

## Task 1: `ConfidenceScale` 公開関数の新設（TDD）

設計書「設計 #2」。confidence → ロット倍率の写像を単一の純粋関数に集約する。

**Files:**
- Create: `backend/internal/usecase/positionsize/confidence.go`
- Test: `backend/internal/usecase/positionsize/confidence_test.go`

- [ ] **Step 1: Write the failing test**

`backend/internal/usecase/positionsize/confidence_test.go`:

```go
package positionsize

import (
	"math"
	"testing"
)

func TestConfidenceScale(t *testing.T) {
	t.Parallel()
	// legacyScale reproduces the old inline formula (floor fixed at 0.5)
	// so we can assert ConfidenceScale(.., 0.5) is bit-equivalent.
	legacyScale := func(conf, min float64) float64 {
		if conf < min {
			conf = min
		}
		if conf > 1 {
			conf = 1
		}
		return 0.5 + 0.5*(conf-min)/(1.0-min)
	}

	cases := []struct {
		name              string
		conf, min, floor  float64
		want              float64
	}{
		{"legacy equiv mid", 0.6, 0.3, 0.5, legacyScale(0.6, 0.3)},
		{"legacy equiv high", 0.9, 0.3, 0.5, legacyScale(0.9, 0.3)},
		{"legacy equiv at min", 0.3, 0.3, 0.5, legacyScale(0.3, 0.3)},
		{"floor unset (0) falls back to 0.5", 0.6, 0.3, 0.0, legacyScale(0.6, 0.3)},
		{"floor 1.0 disables scaling", 0.3, 0.3, 1.0, 1.0},
		{"floor 0.7 maps min->0.7", 0.3, 0.3, 0.7, 0.7},
		{"conf>=1 full size", 1.0, 0.3, 0.5, 1.0},
		{"conf<min clamps to floor", 0.1, 0.3, 0.5, 0.5},
		{"min<=0 disables (no anchor)", 0.6, 0.0, 0.5, 1.0},
		{"min>=1 disables (no range)", 0.6, 1.0, 0.5, 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ConfidenceScale(tc.conf, tc.min, tc.floor)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("ConfidenceScale(%v,%v,%v) = %v, want %v", tc.conf, tc.min, tc.floor, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/usecase/positionsize/ -run TestConfidenceScale -v`
Expected: FAIL — `undefined: ConfidenceScale`

- [ ] **Step 3: Write minimal implementation**

`backend/internal/usecase/positionsize/confidence.go`:

```go
package positionsize

// ConfidenceScale maps confidence in [minConfidence, 1.0] to a lot multiplier
// in [floor, 1.0] linearly. It is the single source of truth for how signal
// confidence shrinks the per-trade lot; both the risk_pct path (Sizer.Compute)
// and the legacy fixed-amount caller (cmd/pipeline.go) route through here so
// the formula can never drift between sites.
//
//   - confidence >= 1.0           → 1.0  (full size)
//   - minConfidence not in (0,1)  → 1.0  (scaling disabled; nothing to map)
//   - floor <= 0                  → 0.5  (legacy default; 0 is the "unset" sentinel)
//   - floor >= 1.0                → 1.0  (scaling disabled by config)
//   - otherwise → floor + (1-floor) * (confidence-minConfidence)/(1-minConfidence)
func ConfidenceScale(confidence, minConfidence, floor float64) float64 {
	// No usable anchor range → scaling disabled.
	if minConfidence <= 0 || minConfidence >= 1 {
		return 1.0
	}
	// Floor sentinel / bounds.
	if floor <= 0 {
		floor = 0.5 // legacy default
	}
	if floor >= 1.0 {
		return 1.0 // scaling disabled by config
	}
	conf := confidence
	if conf >= 1.0 {
		return 1.0
	}
	if conf < minConfidence {
		conf = minConfidence
	}
	return floor + (1.0-floor)*(conf-minConfidence)/(1.0-minConfidence)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/usecase/positionsize/ -run TestConfidenceScale -v`
Expected: PASS (all 10 sub-cases)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/usecase/positionsize/confidence.go backend/internal/usecase/positionsize/confidence_test.go
git commit -m "feat(sizing): add ConfidenceScale pure function (single source of truth)"
```

---

## Task 2: `entity.PositionSizingConfig` に `ConfidenceFloor` を追加（TDD）

設計書「設計 #1」。floor を設定化。`(0,1]` 有効、`0=unset`。

**Files:**
- Modify: `backend/internal/domain/entity/strategy_config.go:393-427`（struct）, `:635-650`（Validate）
- Test: `backend/internal/domain/entity/strategy_config_test.go`

- [ ] **Step 1: Write the failing test**

`backend/internal/domain/entity/strategy_config_test.go` に追記（既存 package `entity` のテストファイル。無ければ新規作成し `package entity`）:

```go
func TestValidate_ConfidenceFloorRange(t *testing.T) {
	base := func(floor float64) StrategyProfile {
		return StrategyProfile{
			Name: "t",
			Risk: StrategyRiskConfig{
				PositionSizing: &PositionSizingConfig{
					Mode:            "risk_pct",
					RiskPerTradePct: 2.0,
					ConfidenceFloor: floor,
				},
			},
		}
	}
	// 0 is the unset sentinel → allowed.
	if err := base(0).Validate(); err != nil {
		t.Fatalf("floor=0 (unset) should be valid, got %v", err)
	}
	// in-range.
	if err := base(0.5).Validate(); err != nil {
		t.Fatalf("floor=0.5 should be valid, got %v", err)
	}
	if err := base(1.0).Validate(); err != nil {
		t.Fatalf("floor=1.0 should be valid, got %v", err)
	}
	// out of range.
	if err := base(-0.1).Validate(); err == nil {
		t.Fatalf("floor=-0.1 should be rejected")
	}
	if err := base(1.5).Validate(); err == nil {
		t.Fatalf("floor=1.5 should be rejected")
	}
}
```

> 注: `StrategyProfile.Validate()` が他フィールド必須で落ちる場合、既存の通過するテストフィクスチャ（`strategy_config_test.go` 内の最小有効 profile）に倣って `base` を補完すること。最小有効形は既存テストを `grep -n "Validate()" strategy_config_test.go` で確認。

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/entity/ -run TestValidate_ConfidenceFloorRange -v`
Expected: FAIL — `ConfidenceFloor` フィールド未定義でコンパイルエラー

- [ ] **Step 3: フィールド追加**

`strategy_config.go` の `PositionSizingConfig` struct（`MinLot`/`LotStep` の前あたり、`:426` 付近）に追加:

```go
	// ConfidenceFloor is the lower bound of the confidence-based lot scale.
	// Effective range is (0, 1]; the scale maps [min_confidence, 1.0] →
	// [ConfidenceFloor, 1.0] linearly. 1.0 disables scaling entirely. 0 is
	// the "unset" sentinel — because omitempty cannot tell an explicit 0 from
	// a missing field, 0 always falls back to the legacy default 0.5 so
	// existing profiles stay bit-identical. To minimise the floor explicitly,
	// set a small positive value (e.g. 0.01), not 0.
	ConfidenceFloor float64 `json:"confidence_floor,omitempty"`
```

- [ ] **Step 4: Validate に範囲チェック追加**

`strategy_config.go` の `Validate()` 内、`RiskPerTradePct` チェック（`:641-643`）の直後に追加:

```go
		if ps.ConfidenceFloor < 0 || ps.ConfidenceFloor > 1 {
			errs = append(errs, fmt.Errorf("strategy_risk.position_sizing.confidence_floor must be in [0, 1] (0 = unset) (got %v)", ps.ConfidenceFloor))
		}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/entity/ -run TestValidate_ConfidenceFloorRange -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add backend/internal/domain/entity/strategy_config.go backend/internal/domain/entity/strategy_config_test.go
git commit -m "feat(sizing): add confidence_floor to PositionSizingConfig with (0,1] validation"
```

---

## Task 3: `Sizer.Compute` を `ConfidenceScale` へ置換（TDD）

設計書「設計 #3」。インライン式を関数呼び出しに置換し、floor を config から渡す。挙動は floor 未設定で従来等価。

**Files:**
- Modify: `backend/internal/usecase/positionsize/sizer.go:183-195`
- Test: `backend/internal/usecase/positionsize/sizer_test.go`

- [ ] **Step 1: Write the failing test**

`sizer_test.go` に追記（既存 `TestSizer_ConfidenceScales` の隣）:

```go
func TestSizer_ConfidenceFloorDisablesScaling(t *testing.T) {
	t.Parallel()
	// floor=1.0 → low-confidence signal still gets full base lot.
	cfg := &entity.PositionSizingConfig{
		Mode:            "risk_pct",
		RiskPerTradePct: 1.0,
		ConfidenceFloor: 1.0,
	}
	s := New(cfg, fineDefaults())
	in := Input{EntryPrice: 12000, StopLossPercent: 14, Equity: 100000, Confidence: 0.3, MinConfidence: 0.3}
	got := s.Compute(in)
	if !almostEqual(got.AppliedConf, 1.0, 1e-9) {
		t.Fatalf("floor=1.0 should yield AppliedConf=1.0, got %v", got.AppliedConf)
	}
	if !almostEqual(got.Amount, got.BaseAmount, 1e-9) {
		t.Fatalf("floor=1.0 should leave base lot unscaled; base=%v amount=%v", got.BaseAmount, got.Amount)
	}
}

func TestSizer_ConfidenceFloorUnsetIsLegacy(t *testing.T) {
	t.Parallel()
	// floor unset (0) must match the legacy 0.5-floored behaviour exactly.
	cfg := &entity.PositionSizingConfig{Mode: "risk_pct", RiskPerTradePct: 1.0}
	s := New(cfg, fineDefaults())
	in := Input{EntryPrice: 12000, StopLossPercent: 14, Equity: 100000, Confidence: 0.6, MinConfidence: 0.3}
	got := s.Compute(in)
	wantConf := 0.5 + 0.5*(0.6-0.3)/(1.0-0.3) // legacy formula
	if !almostEqual(got.AppliedConf, wantConf, 1e-9) {
		t.Fatalf("unset floor should reproduce legacy 0.5 floor; got AppliedConf=%v want %v", got.AppliedConf, wantConf)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/usecase/positionsize/ -run 'TestSizer_ConfidenceFloor' -v`
Expected: FAIL — `floor=1.0` でも旧式が走り AppliedConf≠1.0（置換前なので）

- [ ] **Step 3: Compute のインライン式を置換**

`sizer.go:183-195` の以下のブロック:

```go
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
```

を、次に置換:

```go
	// Confidence scaling: shrink the lot toward ConfidenceFloor as confidence
	// approaches MinConfidence. ConfidenceScale is the single source of truth
	// (shared with cmd/pipeline.go). floor=0 falls back to the legacy 0.5.
	confScale := ConfidenceScale(in.Confidence, in.MinConfidence, s.cfg.ConfidenceFloor)
	amount *= confScale
	out.AppliedConf = confScale
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/usecase/positionsize/ -v`
Expected: PASS（新規2件 + 既存 `TestSizer_ConfidenceScales` 含む全件）

- [ ] **Step 5: パッケージコメントの意味論を更新**

`sizer.go` 冒頭のパッケージコメント（`Design goals:` の confidence 行、`:12-13` 付近）を更新。`risk_pct` を「ベース予算」とし「上限/ceiling」の語を使わない:

```go
//   - risk_pct mode = equity * risk_pct / (entry_price * stop_loss_percent),
//     which is the per-trade BASE risk budget (not a hard ceiling — ATR and
//     drawdown multipliers compose on top and can push realised risk above it).
//   - Signal confidence (via ConfidenceScale), current ATR, and running
//     drawdown all compose multiplicatively on top of the base lot.
```

- [ ] **Step 6: Commit**

```bash
git add backend/internal/usecase/positionsize/sizer.go backend/internal/usecase/positionsize/sizer_test.go
git commit -m "refactor(sizing): route Sizer.Compute confidence through ConfidenceScale; fix risk_pct semantics to base-budget"
```

---

## Task 4: `cmd/pipeline.go` の重複 `scaleByConfidence` を削除（TDD）

設計書「設計 #4」。legacy pipeline の重複式を `positionsize.ConfidenceScale` に統合。挙動不変。

**Files:**
- Modify: `backend/cmd/pipeline.go:415, 428, 707-717`（関数定義）, `:60, :114`（コメント）

- [ ] **Step 1: 既存 pipeline テストが緑であることを記録（ベースライン）**

Run: `cd backend && go test ./cmd/ -run Pipeline -v`
Expected: PASS（変更前の緑を確認。テストが無ければ「no tests to run」を記録し次へ）

- [ ] **Step 2: line 415 の死んだ scaleByConfidence 掛けを除去**

`pipeline.go:415`:

```go
		baseline := (snap.tradeAmount * scaleByConfidence(signal.Confidence, snap.minConfidence)) / price
```

を（risk_pct モードでは baseline は sizer に無視されるため scaling は無意味）:

```go
		baseline := snap.tradeAmount / price
```

- [ ] **Step 3: line 428 の fixed フォールバックを ConfidenceScale 経由に置換**

`pipeline.go:428`（`else` ブランチ、`snap.sizer == nil`）:

```go
		scaledTradeAmount := snap.tradeAmount * scaleByConfidence(signal.Confidence, snap.minConfidence)
```

を（legacy floor 0.5 を明示）:

```go
		scaledTradeAmount := snap.tradeAmount * positionsize.ConfidenceScale(signal.Confidence, snap.minConfidence, 0.5)
```

`cmd/pipeline.go` の import に `positionsize` が無ければ追加:

```go
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/positionsize"
```

- [ ] **Step 4: `func scaleByConfidence` を削除**

`pipeline.go:707-717` の関数定義ブロック全体（コメント `// scaleByConfidence は信頼度に基づいて...` 含む）を削除。

- [ ] **Step 5: 言及コメントを更新**

`pipeline.go:60` と `:114` の `scaleByConfidence` への言及を `positionsize.ConfidenceScale` に書き換える（文意は維持。例: 「restores the legacy scaleByConfidence behaviour」→「restores the legacy positionsize.ConfidenceScale behaviour (floor 0.5)」）。

- [ ] **Step 6: ビルド & テストで挙動不変を確認**

Run: `cd backend && go build ./... && go test ./cmd/ -v`
Expected: ビルド成功、PASS（`scaleByConfidence` 未定義エラーが残らないこと、既存 pipeline テストが緑のまま）

- [ ] **Step 7: 式の集約を確認**

Run: `cd backend && grep -rn "0.5 + 0.5\*\|0.5\*(conf" --include='*.go' . | grep -v _test.go`
Expected: ヒット 0 件（confidence 式が ConfidenceScale 1 箇所のみ。テストの legacyScale ヘルパは除外）

- [ ] **Step 8: Commit**

```bash
git add backend/cmd/pipeline.go
git commit -m "refactor(sizing): remove duplicate scaleByConfidence; route legacy pipeline through positionsize.ConfidenceScale"
```

---

## Task 5: backtest 5 入口に `MinConfidence` を配線（TDD）

設計書「設計 #5」。case (a)（環境変数 `TRADE_MIN_CONFIDENCE` 既定 0.3）。これで live/backtest の confidence scaling が同値になる。

**Files:**
- Modify: `backend/internal/interfaces/api/handler/backtest.go`（`WithMinConfidence` option + 単発 RunInput）
- Modify: `backend/internal/interfaces/api/handler/backtest_multi.go:228`（複数期間 RunInput）
- Modify: `backend/internal/interfaces/api/handler/backtest_walkforward.go:262`（WFO RunInput）
- Modify: `backend/internal/interfaces/api/router.go:192`（composition root で注入）
- Modify: `backend/cmd/backtest/main.go`（`buildRunInput` に cfg、CLI 単発/optimize/refine）
- Modify: `backend/cmd/backtest/walkforward.go:168`（CLI WFO）

- [ ] **Step 1: API handler に MinConfidence フィールド + option を追加**

`backtest.go` の `BacktestHandler` struct（`:66` の `decisionLogRepo` の後）に追加:

```go
	// minConfidence mirrors cfg.Trading.MinConfidence (TRADE_MIN_CONFIDENCE,
	// default 0.3) so the sizer's confidence scaling in backtest matches the
	// live pipeline. 0 leaves scaling disabled (legacy backtest behaviour).
	minConfidence float64
```

option を `WithDecisionLogRepo` の後に追加:

```go
// WithMinConfidence wires the live min-confidence threshold so backtest runs
// apply the same confidence scaling as the live pipeline. Without it, backtest
// runs leave MinConfidence at 0 and confidence scaling is disabled.
func WithMinConfidence(min float64) BacktestHandlerOption {
	return func(h *BacktestHandler) {
		h.minConfidence = min
	}
}
```

- [ ] **Step 2: 単発 API の RunInput に配線**

`backtest.go:369` の `runner.Run(context.Background(), bt.RunInput{` リテラル内、`ExitOnSignal:` の隣に追加:

```go
			MinConfidence:     h.minConfidence,
```

- [ ] **Step 3: 複数期間 API の RunInput に配線**

`backtest_multi.go:228` の `input := bt.RunInput{` リテラル内、`ExitOnSignal:` の隣に追加。`buildSharedRunInput` 等のレシーバが handler なら `h.minConfidence`、そうでなければ呼び出し元から引き回す:

```go
				MinConfidence:     h.minConfidence,
```

> 注: `backtest_multi.go` の当該関数が `*BacktestHandler` のメソッドか確認（`grep -n "func (h \*BacktestHandler)" backtest_multi.go`）。メソッドでない場合は引数で `minConfidence float64` を受け取り、呼び出し元（handler メソッド）から `h.minConfidence` を渡す。

- [ ] **Step 4: WFO API の RunInput に配線**

`backtest_walkforward.go:262` の `windowRunner.Run(ctx, bt.RunInput{` リテラル内に追加（Step 3 と同じレシーバ確認の注意が適用）:

```go
				MinConfidence:     h.minConfidence,
```

- [ ] **Step 5: composition root で注入**

`Dependencies`（`router.go:24`）に `MinConfidence` は無く、router は cfg に直接アクセスしない。よって `Dependencies` 構造体にフィールドを追加し、`main.go` から値を流す。

(5a) `router.go:24` の `Dependencies` 構造体に追加:

```go
	// BacktestMinConfidence mirrors cfg.Trading.MinConfidence so backtest
	// runs apply the same confidence scaling as the live pipeline.
	BacktestMinConfidence float64
```

(5b) `router.go:192` 付近の `opts` 組み立て箇所に追加（他の `opts = append(...)` に倣う）:

```go
		opts = append(opts, handler.WithMinConfidence(deps.BacktestMinConfidence))
```

> 注: `opts` がスライスで条件付き append している既存形を確認（`grep -n "opts = append\|opts :=" router.go`）。`WithMinConfidence(0)` でも安全（min=0 は scaling 無効＝従来の backtest 挙動）なので無条件 append でよい。

(5c) `main.go:313` の `api.Dependencies{` リテラル（`BacktestResultRepo:` の隣、`:325` 付近）に追加。`cfg` は同スコープ（`cfg.Server.Port` を `:342` で使用済み）:

```go
		BacktestMinConfidence: cfg.Trading.MinConfidence,
```

- [ ] **Step 6: API 配線のテスト**

`backtest` handler のテストに追加（既存の handler テストファイルに倣う。無ければ既存 `RunInput` を検証している箇所を流用）:

```go
func TestBacktestHandler_MinConfidenceWired(t *testing.T) {
	h := NewBacktestHandler(nil, nil, WithMinConfidence(0.3))
	if h.minConfidence != 0.3 {
		t.Fatalf("WithMinConfidence should set minConfidence=0.3, got %v", h.minConfidence)
	}
}
```

Run: `cd backend && go test ./internal/interfaces/api/handler/ -run MinConfidence -v`
Expected: PASS

- [ ] **Step 7: CLI 単発（buildRunInput）に配線**

`cmd/backtest/main.go`:
- `buildRunInput` のシグネチャに `cfg config.Config`（または min 値）を追加: `func buildRunInput(f runFlags, profile *entity.StrategyProfile, minConfidence float64) (bt.RunInput, error)`
- `:726` の `in := bt.RunInput{` リテラルに `MinConfidence: minConfidence,` を追加
- 3 つの呼び出し元（`:145, :281, :376`）に、既に `:484` で `config.Load()` した `cfg.Trading.MinConfidence` を渡す。`cfg` がスコープ外なら呼び出し関数に引き回す。

- [ ] **Step 8: CLI WFO に配線**

`cmd/backtest/walkforward.go:168` の `windowRunner.Run(ctx, bt.RunInput{` リテラルに追加（クロージャは `cfg` を既にキャプチャしている）:

```go
				MinConfidence:     cfg.Trading.MinConfidence,
```

- [ ] **Step 9: ビルド & 全 backtest テスト**

Run: `cd backend && go build ./... && go test ./cmd/backtest/ ./internal/interfaces/api/handler/ -v`
Expected: ビルド成功、PASS

- [ ] **Step 10: 5 入口すべてに配線されたことを確認**

Run: `cd backend && grep -rn "MinConfidence:" internal/interfaces/api/handler/ cmd/backtest/ | grep -v _test.go`
Expected: 5 件（単発API / 複数期間API / WFO API / CLI buildRunInput / CLI WFO）

- [ ] **Step 11: Commit**

```bash
git add backend/internal/interfaces/api/handler/ backend/internal/interfaces/api/router.go backend/cmd/backtest/
git commit -m "feat(backtest): wire MinConfidence (TRADE_MIN_CONFIDENCE) into all 5 backtest entry points"
```

---

## Task 6: 配線前後の backtest 結果差分の記録 + 全体回帰

設計書「テスト戦略 6,7」。MinConfidence 配線で backtest の振る舞いが変わるため、production プロファイルでの差分を実測し promote 実績の再検証要否を判断する。

**Files:**
- Create/Modify: `docs/pdca/` 配下の記録ノート（または設計書末尾に追記）

- [ ] **Step 1: 全体回帰テスト**

Run: `cd backend && go test ./... -race -count=1`
Expected: PASS（全パッケージ）

- [ ] **Step 2: production プロファイルで backtest を実行し結果を取得**

Docker で backend が起動している前提（`docker compose up --build -d`）。production.json で複数期間 backtest を実行:

Run:
```bash
curl -s -X POST http://localhost:38080/api/v1/backtest/run-multi \
  -H 'Content-Type: application/json' \
  -d '{"profileName":"production","periods":["1m","3m","6m","1y","2y"],"tradeAmount":1000,"initialBalance":60000}' \
  | python3 -m json.tool | head -60
```
Expected: 各期間の return / maxDD / trades が返る。エラー時は `docker compose logs backend` で確認。

- [ ] **Step 3: 結果を記録**

得られた数値を `docs/pdca/` の新規ノート（例 `2026-06-10_minconfidence_wiring_impact.md`）に記録:
- 配線後の 5 期間 return / MaxDD / trades
- production.json コメントの旧実績（1m +0.2%, 3m +8.9%, 6m +20.4%, 1y +26.0%, 2y +67.7%）との比較
- 乖離が大きい場合（例 MaxDD が 25% を超える、return 符号が反転）は「promote 実績の再キャリブレーションが必要」と明記し、別タスク（PDCA サイクル）として起票を推奨

> 注: これは「配線で挙動が変わったか」を可視化する観測ステップ。数値を変えること自体は本タスクの目的ではない。floor の調整（0.5→0.7 等）は設計書の非目的どおり別タスク。

- [ ] **Step 4: Commit**

```bash
git add docs/pdca/2026-06-10_minconfidence_wiring_impact.md
git commit -m "docs(pdca): record backtest result delta from MinConfidence wiring"
```

---

## Task 7: ドキュメント更新と最終確認

設計書「ドキュメント更新」+「受け入れ基準」。

**Files:**
- Modify: `backend/.env.example`（`confidence_floor` の言及、該当すれば）
- Modify: 設計書 Status を `Implemented` に

- [ ] **Step 1: .env.example / profile スキーマ説明の確認**

Run: `cd backend && grep -n "TRADE_MIN_CONFIDENCE\|confidence" .env.example`
`confidence_floor` は profile JSON のフィールドなので `.env` ではなく profile スキーマ説明（docs があれば）に追記。`.env.example` の `TRADE_MIN_CONFIDENCE` のコメントに「backtest にも適用される」旨を追記。

- [ ] **Step 2: 受け入れ基準の全項目を検証**

設計書「受け入れ基準」を1つずつ確認:
- [ ] `confidence_floor` 未設定の production.json で従来 bit-identical（Task 3 のテストで担保）
- [ ] `confidence_floor=1.0` で low-conf でも base どおり（Task 3 のテストで担保）
- [ ] 式が `ConfidenceScale` 1 箇所のみ（Task 4 Step 7 で確認済み）
- [ ] backtest で MinConfidence 配線済み・live と同値（Task 5 Step 10 で確認済み）
- [ ] 配線前後 backtest 差分を記録（Task 6 で実施）
- [ ] `go test ./... -race -count=1` 緑（Task 6 Step 1）
- [ ] `reverse_signal` / `ExitOnSignal` 無変更:

Run: `cd backend && git diff main --stat | grep -E "real_executor|simulator" || echo "両建てガード無変更 OK"`
Expected: `reverse_signal` を含むファイルに変更が無いこと

- [ ] **Step 3: 設計書 Status 更新 + Commit**

設計書の `Status:` を `Implemented (2026-06-10)` に更新。

```bash
git add docs/superpowers/specs/2026-06-10-confidence-sizing-unification-design.md backend/.env.example
git commit -m "docs: mark confidence sizing unification spec as implemented"
```

- [ ] **Step 4: PR 作成（feature ブランチ運用）**

AGENTS.md の GitHub Flow に従い、feature ブランチで作業 → PR。

```bash
git push -u origin feat/confidence-sizing-unification
gh pr create --title "feat(sizing): confidence sizing の一元化と意味論の再定義" --body "設計書: docs/superpowers/specs/2026-06-10-confidence-sizing-unification-design.md

- ConfidenceScale 公開関数に式を集約 (重複削除)
- confidence_floor を設定化 ((0,1], 0=unset→0.5、現状等価)
- risk_pct の意味論を「ベース予算」に訂正
- backtest 5入口に MinConfidence を配線 (live/backtest 同値)

Codex 3巡レビュー済み。"
```
