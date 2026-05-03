package exitplan

import (
	"context"
	"math"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func TestATRSource_consumesIndicatorEvent(t *testing.T) {
	src := NewATRSource()
	atr := 12.5
	ev := entity.IndicatorEvent{
		Primary: entity.IndicatorSet{
			ATR: &atr,
		},
	}
	if _, err := src.Handle(context.Background(), ev); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := src.Current(); got != 12.5 {
		t.Errorf("Current = %v, want 12.5", got)
	}
}

func TestATRSource_NaNIgnored(t *testing.T) {
	src := NewATRSource()
	atr := math.NaN()
	ev := entity.IndicatorEvent{Primary: entity.IndicatorSet{ATR: &atr}}
	if _, err := src.Handle(context.Background(), ev); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := src.Current(); got != 0 {
		t.Errorf("NaN should be ignored, got %v", got)
	}
}

func TestATRSource_NegativeIgnored(t *testing.T) {
	src := NewATRSource()
	atr := -1.0
	src.Handle(context.Background(), entity.IndicatorEvent{Primary: entity.IndicatorSet{ATR: &atr}})
	if got := src.Current(); got != 0 {
		t.Errorf("negative should be ignored, got %v", got)
	}
}

func TestATRSource_ZeroAccepted(t *testing.T) {
	src := NewATRSource()
	atr1 := 10.0
	src.Handle(context.Background(), entity.IndicatorEvent{Primary: entity.IndicatorSet{ATR: &atr1}})
	atr2 := 0.0
	src.Handle(context.Background(), entity.IndicatorEvent{Primary: entity.IndicatorSet{ATR: &atr2}})
	if got := src.Current(); got != 0 {
		t.Errorf("zero should be accepted (replace stale positive), got %v", got)
	}
}

func TestATRSource_NilATR_noOp(t *testing.T) {
	src := NewATRSource()
	atr := 10.0
	src.Handle(context.Background(), entity.IndicatorEvent{Primary: entity.IndicatorSet{ATR: &atr}})
	src.Handle(context.Background(), entity.IndicatorEvent{Primary: entity.IndicatorSet{ATR: nil}})
	if got := src.Current(); got != 10 {
		t.Errorf("nil ATR should not overwrite, got %v", got)
	}
}

func TestATRSource_NonIndicatorEvent_noOp(t *testing.T) {
	src := NewATRSource()
	if _, err := src.Handle(context.Background(), entity.TickEvent{}); err != nil {
		t.Fatalf("non-indicator should not error: %v", err)
	}
	if src.Current() != 0 {
		t.Errorf("Current should remain 0")
	}
}
