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

// ErrPathEscape is returned when the resolved path, after cleaning, lies
// outside baseDir. This should not happen given the regex check, but is kept
// as a defence-in-depth guard.
var ErrPathEscape = errors.New("resolved path escapes base directory")

// ResolveProfilePath returns the cleaned relative path to
// `<baseDir>/<name>.json`, or an error if `name` is unsafe.
//
// Contract:
//   - `name` must be non-empty and match ^[a-zA-Z0-9_-]+$.
//   - The returned path is the result of filepath.Clean(filepath.Join(baseDir, name+".json"))
//     — it is intentionally *relative* (not absolute) so logs and test
//     expectations remain portable. The caller may pass the result straight
//     to os.Open.
//   - The cleaned path is double-checked against the absolute baseDir to
//     guarantee it has not escaped (defence in depth).
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

	return cleaned, nil
}
