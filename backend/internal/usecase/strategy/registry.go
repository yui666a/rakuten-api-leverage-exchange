package strategy

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/port"
)

// Registry errors.
var (
	// ErrStrategyNameEmpty is returned when Register is called with an empty
	// name.
	ErrStrategyNameEmpty = errors.New("strategy: name must not be empty")

	// ErrStrategyNil is returned when Register is called with a nil
	// port.Strategy.
	ErrStrategyNil = errors.New("strategy: strategy must not be nil")

	// ErrStrategyAlreadyRegistered is returned when Register is called with a
	// name that is already present in the registry.
	ErrStrategyAlreadyRegistered = errors.New("strategy: name already registered")

	// ErrStrategyNotFound is returned when Get cannot find a strategy by the
	// given name.
	ErrStrategyNotFound = errors.New("strategy: not found")
)

// StrategyRegistry is a goroutine-safe lookup for port.Strategy implementations
// keyed by stable name. Composition roots construct the registry once at
// startup and hand out a *StrategyRegistry to callers that need to resolve
// strategies dynamically (e.g. a future profile loader or CLI flag).
type StrategyRegistry struct {
	mu         sync.RWMutex
	strategies map[string]port.Strategy
}

// NewStrategyRegistry returns an empty, ready-to-use registry.
func NewStrategyRegistry() *StrategyRegistry {
	return &StrategyRegistry{
		strategies: make(map[string]port.Strategy),
	}
}

// Register stores s under name. Returns ErrStrategyNameEmpty for empty names,
// ErrStrategyNil if s is nil, and ErrStrategyAlreadyRegistered if name has
// already been registered.
func (r *StrategyRegistry) Register(name string, s port.Strategy) error {
	if name == "" {
		return ErrStrategyNameEmpty
	}
	if s == nil {
		return ErrStrategyNil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.strategies[name]; exists {
		return fmt.Errorf("%w: %q", ErrStrategyAlreadyRegistered, name)
	}
	r.strategies[name] = s
	return nil
}

// Get returns the strategy registered under name, or ErrStrategyNotFound.
func (r *StrategyRegistry) Get(name string) (port.Strategy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.strategies[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrStrategyNotFound, name)
	}
	return s, nil
}

// List returns all registered strategy names in lexicographic order. The
// returned slice is a copy safe for the caller to mutate.
func (r *StrategyRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.strategies))
	for name := range r.strategies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
