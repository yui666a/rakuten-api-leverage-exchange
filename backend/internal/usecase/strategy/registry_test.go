package strategy

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/port"
)

// stubStrategy is a minimal port.Strategy implementation for registry tests.
// It does not touch any indicator math – the registry is a pure lookup.
type stubStrategy struct {
	name string
}

func (s *stubStrategy) Evaluate(
	ctx context.Context,
	indicators *entity.IndicatorSet,
	higherTF *entity.IndicatorSet,
	lastPrice float64,
	now time.Time,
) (*entity.Signal, error) {
	return &entity.Signal{Action: entity.SignalActionHold}, nil
}

func (s *stubStrategy) Name() string { return s.name }

func TestStrategyRegistry_RegisterGetList_HappyPath(t *testing.T) {
	r := NewStrategyRegistry()

	// Registered intentionally out of lexicographic order to verify List sorts.
	entries := []*stubStrategy{
		{name: "default"},
		{name: "aggressive"},
		{name: "passive"},
	}
	for _, s := range entries {
		if err := r.Register(s.name, s); err != nil {
			t.Fatalf("Register(%q) returned unexpected error: %v", s.name, err)
		}
	}

	// Get each one back by name.
	for _, s := range entries {
		got, err := r.Get(s.name)
		if err != nil {
			t.Fatalf("Get(%q) returned error: %v", s.name, err)
		}
		if got.Name() != s.name {
			t.Errorf("Get(%q) returned strategy with Name()=%q", s.name, got.Name())
		}
	}

	want := []string{"aggressive", "default", "passive"}
	if got := r.List(); !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %v, want sorted %v", got, want)
	}
}

func TestStrategyRegistry_Register_DuplicateName(t *testing.T) {
	r := NewStrategyRegistry()
	if err := r.Register("default", &stubStrategy{name: "default"}); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	err := r.Register("default", &stubStrategy{name: "default"})
	if err == nil {
		t.Fatal("expected error registering duplicate name, got nil")
	}
	if !errors.Is(err, ErrStrategyAlreadyRegistered) {
		t.Errorf("expected ErrStrategyAlreadyRegistered, got %v", err)
	}
}

func TestStrategyRegistry_Register_EmptyName(t *testing.T) {
	r := NewStrategyRegistry()
	err := r.Register("", &stubStrategy{name: ""})
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
	if !errors.Is(err, ErrStrategyNameEmpty) {
		t.Errorf("expected ErrStrategyNameEmpty, got %v", err)
	}
}

func TestStrategyRegistry_Register_NilStrategy(t *testing.T) {
	r := NewStrategyRegistry()
	err := r.Register("default", nil)
	if err == nil {
		t.Fatal("expected error for nil strategy, got nil")
	}
	if !errors.Is(err, ErrStrategyNil) {
		t.Errorf("expected ErrStrategyNil, got %v", err)
	}
}

func TestStrategyRegistry_Get_Unknown(t *testing.T) {
	r := NewStrategyRegistry()
	_, err := r.Get("does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown name, got nil")
	}
	if !errors.Is(err, ErrStrategyNotFound) {
		t.Errorf("expected ErrStrategyNotFound, got %v", err)
	}
}

func TestStrategyRegistry_List_Empty(t *testing.T) {
	r := NewStrategyRegistry()
	if got := r.List(); len(got) != 0 {
		t.Errorf("expected empty slice from empty registry, got %v", got)
	}
}

// TestStrategyRegistry_ConcurrentAccess exercises Register / Get / List under
// concurrent goroutines. Combined with `go test -race`, this verifies that
// the sync.RWMutex protects the internal map.
func TestStrategyRegistry_ConcurrentAccess(t *testing.T) {
	r := NewStrategyRegistry()

	const numWorkers = 32
	const perWorker = 20

	var wg sync.WaitGroup

	// Writers: each goroutine registers its own unique names.
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				name := stratName(worker, i)
				_ = r.Register(name, &stubStrategy{name: name})
			}
		}(w)
	}

	// Readers: concurrently call Get / List while writers are registering.
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				_, _ = r.Get("default")
				_ = r.List()
			}
		}()
	}

	wg.Wait()

	// Post-condition: every written name should be retrievable exactly once.
	got := r.List()
	wantCount := numWorkers * perWorker
	if len(got) != wantCount {
		t.Fatalf("expected %d registered strategies, got %d", wantCount, len(got))
	}

	var (
		lookupErrs int
		s          port.Strategy
		err        error
	)
	for _, name := range got {
		s, err = r.Get(name)
		if err != nil || s == nil {
			lookupErrs++
		}
	}
	if lookupErrs != 0 {
		t.Errorf("%d concurrent lookups failed", lookupErrs)
	}
}

func stratName(worker, idx int) string {
	// Compact deterministic names that sort stably.
	return fmt.Sprintf("w%02d-%02d", worker, idx)
}
