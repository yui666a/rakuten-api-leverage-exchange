package strategyprofile

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// Loader reads StrategyProfile JSON files from a configurable base directory.
// In production, baseDir is typically "profiles"; tests pass t.TempDir().
type Loader struct {
	baseDir string
}

// NewLoader returns a Loader rooted at baseDir.
func NewLoader(baseDir string) *Loader {
	return &Loader{baseDir: baseDir}
}

// Load resolves `<baseDir>/<name>.json`, decodes it, and validates the
// resulting StrategyProfile. Unknown JSON fields cause a decode error so
// typos in the on-disk config are surfaced at load time rather than silently
// ignored.
//
// Note: the profile's `name` field is NOT required to equal the on-disk
// filename — callers may have reasons to diverge (e.g. snapshotting). See
// the loader test for the specific scenario.
func (l *Loader) Load(name string) (*entity.StrategyProfile, error) {
	path, err := ResolveProfilePath(l.baseDir, name)
	if err != nil {
		return nil, fmt.Errorf("strategyprofile: load %q: %w", name, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("strategyprofile: load %q: %w", name, err)
	}
	defer f.Close()

	profile, err := ParseProfile(f)
	if err != nil {
		return nil, fmt.Errorf("strategyprofile: load %q: %w", name, err)
	}
	return profile, nil
}

// ParseProfile decodes a StrategyProfile from r and validates it. It is
// exported so tests (and future callers) can drive decoding without disk I/O.
// Unknown fields are rejected via json.Decoder.DisallowUnknownFields.
func ParseProfile(r io.Reader) (*entity.StrategyProfile, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	var profile entity.StrategyProfile
	if err := dec.Decode(&profile); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := profile.Validate(); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	return &profile, nil
}
