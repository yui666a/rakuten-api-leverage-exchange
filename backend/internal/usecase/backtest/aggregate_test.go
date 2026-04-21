package backtest

import (
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// makeResult is a tiny helper so tests read like "N periods with these
// Return and MaxDrawdown values" without the BacktestResult boilerplate.
func makeResult(label string, ret, dd float64) entity.LabeledBacktestResult {
	return entity.LabeledBacktestResult{
		Label: label,
		Result: entity.BacktestResult{
			Summary: entity.BacktestSummary{TotalReturn: ret, MaxDrawdown: dd},
		},
	}
}

func TestComputeAggregate_ThreePositive(t *testing.T) {
	// +10%, +5%, +3% all winning.
	items := []entity.LabeledBacktestResult{
		makeResult("a", 0.10, 0.03),
		makeResult("b", 0.05, 0.04),
		makeResult("c", 0.03, 0.02),
	}
	got := ComputeAggregate(items)

	// Geometric mean of 1.10, 1.05, 1.03 - 1
	wantGeom := math.Cbrt(1.10*1.05*1.03) - 1
	if math.Abs(got.GeomMeanReturn-wantGeom) > 1e-9 {
		t.Fatalf("GeomMeanReturn = %v, want %v", got.GeomMeanReturn, wantGeom)
	}
	if !got.AllPositive {
		t.Fatalf("AllPositive should be true")
	}
	if got.WorstReturn != 0.03 {
		t.Fatalf("WorstReturn = %v, want 0.03", got.WorstReturn)
	}
	if got.BestReturn != 0.10 {
		t.Fatalf("BestReturn = %v, want 0.10", got.BestReturn)
	}
	if got.WorstDrawdown != 0.04 {
		t.Fatalf("WorstDrawdown = %v, want 0.04", got.WorstDrawdown)
	}
	// ReturnStdDev: population std of [0.10, 0.05, 0.03]
	mean := (0.10 + 0.05 + 0.03) / 3
	v := ((0.10-mean)*(0.10-mean) + (0.05-mean)*(0.05-mean) + (0.03-mean)*(0.03-mean)) / 3
	wantStd := math.Sqrt(v)
	if math.Abs(got.ReturnStdDev-wantStd) > 1e-9 {
		t.Fatalf("ReturnStdDev = %v, want %v", got.ReturnStdDev, wantStd)
	}
	// RobustnessScore = geomMean - stdDev
	if math.Abs(got.RobustnessScore-(wantGeom-wantStd)) > 1e-9 {
		t.Fatalf("RobustnessScore = %v, want %v", got.RobustnessScore, wantGeom-wantStd)
	}
}

func TestComputeAggregate_MixedReturns(t *testing.T) {
	// +10%, -5%, +3% -> not all positive.
	items := []entity.LabeledBacktestResult{
		makeResult("a", 0.10, 0.08),
		makeResult("b", -0.05, 0.12),
		makeResult("c", 0.03, 0.05),
	}
	got := ComputeAggregate(items)

	if got.AllPositive {
		t.Fatalf("AllPositive should be false when any return <= 0")
	}
	if got.WorstReturn != -0.05 {
		t.Fatalf("WorstReturn = %v, want -0.05", got.WorstReturn)
	}
	if got.WorstDrawdown != 0.12 {
		t.Fatalf("WorstDrawdown = %v, want 0.12", got.WorstDrawdown)
	}
	// geomMean = (1.10 * 0.95 * 1.03)^(1/3) - 1
	wantGeom := math.Cbrt(1.10*0.95*1.03) - 1
	if math.Abs(got.GeomMeanReturn-wantGeom) > 1e-9 {
		t.Fatalf("GeomMeanReturn = %v, want %v", got.GeomMeanReturn, wantGeom)
	}
}

func TestComputeAggregate_RuinReturnsNaN(t *testing.T) {
	// A period with TotalReturn <= -1.0 means bankruptcy. GeomMean is not
	// defined (log(0) or log(negative)) so we return NaN and clamp
	// AllPositive to false.
	items := []entity.LabeledBacktestResult{
		makeResult("a", 0.10, 0.05),
		makeResult("b", -1.0, 0.50), // ruin
		makeResult("c", 0.03, 0.02),
	}
	got := ComputeAggregate(items)

	if !math.IsNaN(got.GeomMeanReturn) {
		t.Fatalf("GeomMeanReturn should be NaN on ruin, got %v", got.GeomMeanReturn)
	}
	if got.AllPositive {
		t.Fatalf("AllPositive should be false on ruin")
	}
	if got.WorstReturn != -1.0 {
		t.Fatalf("WorstReturn = %v, want -1.0", got.WorstReturn)
	}
	if !math.IsNaN(got.RobustnessScore) {
		t.Fatalf("RobustnessScore should be NaN when GeomMeanReturn is NaN, got %v", got.RobustnessScore)
	}
	// WorstDrawdown remains populated even under ruin so operators can see
	// the full picture.
	if got.WorstDrawdown != 0.50 {
		t.Fatalf("WorstDrawdown = %v, want 0.50", got.WorstDrawdown)
	}
}

func TestComputeAggregate_ReturnBelowMinusOneAlsoRuin(t *testing.T) {
	// r <= -1.0 (e.g. -1.2 would be impossible but guard anyway)
	items := []entity.LabeledBacktestResult{
		makeResult("a", -1.2, 0.30),
		makeResult("b", 0.05, 0.02),
	}
	got := ComputeAggregate(items)
	if !math.IsNaN(got.GeomMeanReturn) {
		t.Fatalf("ruin guard should trigger for r <= -1")
	}
}

func TestComputeAggregate_Empty(t *testing.T) {
	got := ComputeAggregate(nil)
	// Empty input: all zero, AllPositive vacuously true? We pick false so
	// downstream code cannot confuse "no data" with "all positive".
	if got.AllPositive {
		t.Fatalf("AllPositive should be false on empty input (no evidence)")
	}
	if got.GeomMeanReturn != 0 || got.ReturnStdDev != 0 {
		t.Fatalf("empty input should be all zero, got %+v", got)
	}
}

func TestComputeAggregate_Single(t *testing.T) {
	// One period: stdDev = 0, RobustnessScore = Return.
	items := []entity.LabeledBacktestResult{makeResult("a", 0.08, 0.04)}
	got := ComputeAggregate(items)
	if got.ReturnStdDev != 0 {
		t.Fatalf("single-period stdDev should be 0, got %v", got.ReturnStdDev)
	}
	if math.Abs(got.GeomMeanReturn-0.08) > 1e-9 {
		t.Fatalf("single-period geomMean should equal the return, got %v", got.GeomMeanReturn)
	}
	if math.Abs(got.RobustnessScore-0.08) > 1e-9 {
		t.Fatalf("single-period robustness should equal the return, got %v", got.RobustnessScore)
	}
	if !got.AllPositive {
		t.Fatalf("single positive period should be AllPositive")
	}
}

func TestComputeAggregate_WorstDrawdownIsMaxNotMin(t *testing.T) {
	// Quick regression guard: WorstDrawdown must be the MAX of the individual
	// DD values (largest drawdown = worst outcome), not the minimum.
	items := []entity.LabeledBacktestResult{
		makeResult("a", 0.05, 0.03),
		makeResult("b", 0.02, 0.15), // worst
		makeResult("c", 0.04, 0.08),
	}
	got := ComputeAggregate(items)
	if got.WorstDrawdown != 0.15 {
		t.Fatalf("WorstDrawdown = %v, want 0.15", got.WorstDrawdown)
	}
}
