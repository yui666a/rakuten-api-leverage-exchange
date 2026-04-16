// Package strategyprofile loads StrategyProfile JSON files from a configurable
// base directory (typically "profiles/"). It provides safe path resolution
// that refuses traversal attempts before touching the filesystem.
package strategyprofile

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// profileNamePattern is the allow-list for profile names. Only ASCII letters,
// digits, underscore and hyphen are permitted — no dots, no slashes, no
// whitespace. This is deliberately strict so callers cannot smuggle in
// path-traversal fragments like ".." or absolute paths like "/etc/passwd".
var profileNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ErrInvalidProfileName is returned when the caller supplies a profile name
// that fails the allow-list check.
var ErrInvalidProfileName = errors.New("invalid profile name")

// ErrPathEscape is a defence-in-depth sentinel retained for future changes;
// the regex above (profileNamePattern) currently makes it unreachable in
// practice because every name that could cause the cleaned path to escape
// baseDir would be rejected earlier as ErrInvalidProfileName. It is kept
// (and returned) so that if the regex is ever relaxed, callers still have
// a stable error type to match against.
var ErrPathEscape = errors.New("resolved path escapes base directory")

// ResolveProfilePath returns the cleaned *absolute* path to
// `<baseDir>/<name>.json`, or an error if `name` is unsafe.
//
// Contract:
//   - `name` must be non-empty and match ^[a-zA-Z0-9_-]+$.
//   - The returned path is absolute (filepath.Abs of
//     filepath.Clean(filepath.Join(baseDir, name+".json"))). Returning an
//     absolute path eliminates a TOCTOU class of bug where a caller
//     changes the process working directory between ResolveProfilePath
//     and os.Open, causing the "relative" path to resolve to a different
//     file than intended.
//   - The absolute path is also checked against the absolute baseDir as a
//     defence-in-depth guard against directory escape (see ErrPathEscape).
func ResolveProfilePath(baseDir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%w: name must not be empty", ErrInvalidProfileName)
	}
	if !profileNamePattern.MatchString(name) {
		return "", fmt.Errorf("%w: %q (only a-zA-Z0-9_- allowed)", ErrInvalidProfileName, name)
	}

	joined := filepath.Join(baseDir, name+".json")
	cleaned := filepath.Clean(joined)

	// Defence in depth: even though the regex rejects traversal fragments,
	// verify the cleaned absolute path is still rooted at baseDir.
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve base dir: %w", err)
	}
	absCleaned, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve profile path: %w", err)
	}
	absBase = filepath.Clean(absBase)
	// Use absBase + separator so that "/etc/passwordless" is not accepted as
	// inside "/etc/pass". The cleaned path is an exact match only if it
	// equals absBase (unlikely for a file) or starts with absBase + sep.
	if absCleaned != absBase && !strings.HasPrefix(absCleaned, absBase+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrPathEscape, cleaned)
	}

	return absCleaned, nil
}
