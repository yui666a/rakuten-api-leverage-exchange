package strategyprofile

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

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

// ProfileSummary is the shape returned by Loader.List. It intentionally
// carries only the metadata the FE picker needs — the full StrategyProfile
// is fetched lazily via Load(name) when the user actually selects one.
type ProfileSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// IsRouter is true when the profile carries a regime_routing block
	// (StrategyProfile.HasRouting). Surfaced to the FE so the picker can
	// disable the "edit-and-run" path for router profiles, which cannot
	// be run standalone without resolving children.
	IsRouter bool `json:"isRouter"`
}

// List enumerates every `<baseDir>/*.json` profile and returns a summary
// for each one. Files that fail to decode or validate are skipped (with
// a best-effort inclusion using the filename as Name) so a single bad
// profile does not hide the rest of the directory from the UI. The
// result is sorted by Name for deterministic FE ordering.
func (l *Loader) List() ([]ProfileSummary, error) {
	entries, err := os.ReadDir(l.baseDir)
	if err != nil {
		return nil, fmt.Errorf("strategyprofile: list %q: %w", l.baseDir, err)
	}
	out := make([]ProfileSummary, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		base := strings.TrimSuffix(name, ".json")
		profile, err := l.Load(base)
		if err != nil {
			// Still surface the entry so the UI can signal a bad profile;
			// the description field carries the error text.
			out = append(out, ProfileSummary{
				Name:        base,
				Description: fmt.Sprintf("(load error: %v)", err),
			})
			continue
		}
		out = append(out, ProfileSummary{
			Name:        profile.Name,
			Description: profile.Description,
			IsRouter:    profile.HasRouting(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
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
	// Call Validate on the value (Validate has a value receiver, so this
	// avoids any nil-pointer question entirely).
	if err := profile.Validate(); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	return &profile, nil
}
