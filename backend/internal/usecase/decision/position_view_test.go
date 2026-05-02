package decision

import (
	"context"
	"testing"
)

func TestFlatPositionView_AlwaysFlat(t *testing.T) {
	v := FlatPositionView{}
	for _, sym := range []int64{0, 7, 10, 99} {
		if got := v.CurrentSide(context.Background(), sym); got != "" {
			t.Errorf("symbol %d: got %q, want empty (flat)", sym, got)
		}
	}
}
