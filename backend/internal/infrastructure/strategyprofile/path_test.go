package strategyprofile

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProfilePath(t *testing.T) {
	// A fixed baseDir is fine for these tests — nothing touches the
	// filesystem, we only check path math. The expected paths are computed
	// via filepath.Abs because ResolveProfilePath now returns absolute paths
	// to avoid a CWD-change TOCTOU class of bug.
	const baseDir = "profiles"

	mustAbs := func(t *testing.T, p string) string {
		t.Helper()
		abs, err := filepath.Abs(p)
		if err != nil {
			t.Fatalf("filepath.Abs(%q): %v", p, err)
		}
		return abs
	}

	tests := []struct {
		name       string
		profile    string
		wantPath   string // expected absolute cleaned path; "" when an error is expected
		wantErr    error  // expected sentinel via errors.Is; nil when no error expected
		wantErrSub string // substring check when wantErr is nil but an error is expected
	}{
		{
			name:     "valid simple name",
			profile:  "production",
			wantPath: mustAbs(t, filepath.Join("profiles", "production.json")),
		},
		{
			name:     "valid with hyphen and underscore",
			profile:  "experiment_2026-04-16_01",
			wantPath: mustAbs(t, filepath.Join("profiles", "experiment_2026-04-16_01.json")),
		},
		{
			name:     "valid alphanumeric mix",
			profile:  "ltc_aggressive_v3",
			wantPath: mustAbs(t, filepath.Join("profiles", "ltc_aggressive_v3.json")),
		},
		{
			name:    "empty name rejected",
			profile: "",
			wantErr: ErrInvalidProfileName,
		},
		{
			name:    "dotdot rejected by regex",
			profile: "..",
			wantErr: ErrInvalidProfileName,
		},
		{
			name:    "absolute path rejected by regex",
			profile: "/etc/passwd",
			wantErr: ErrInvalidProfileName,
		},
		{
			name:    "slash rejected by regex",
			profile: "a/b",
			wantErr: ErrInvalidProfileName,
		},
		{
			name:    "backslash rejected by regex",
			profile: `a\b`,
			wantErr: ErrInvalidProfileName,
		},
		{
			name:    "trailing space rejected",
			profile: "production ",
			wantErr: ErrInvalidProfileName,
		},
		{
			name:    "leading space rejected",
			profile: " production",
			wantErr: ErrInvalidProfileName,
		},
		{
			name:    "dot in name rejected",
			profile: "foo.bar",
			wantErr: ErrInvalidProfileName,
		},
		{
			name:    "parent traversal embedded rejected",
			profile: "../../etc/passwd",
			wantErr: ErrInvalidProfileName,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveProfilePath(baseDir, tc.profile)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected errors.Is %v, got %v", tc.wantErr, err)
				}
				if got != "" {
					t.Fatalf("expected empty path on error, got %q", got)
				}
				return
			}
			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrSub)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Fatalf("expected error substring %q, got %v", tc.wantErrSub, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantPath {
				t.Fatalf("path = %q, want %q", got, tc.wantPath)
			}
		})
	}
}

// TestResolveProfilePath_CrossPlatformSeparator ensures the joined path uses
// the platform's native separator (i.e. filepath.Join is doing the work, not
// a hard-coded "/"). This is defensive — if someone refactors ResolveProfilePath
// to use path.Join or a literal separator, the test catches it on Windows.
func TestResolveProfilePath_CrossPlatformSeparator(t *testing.T) {
	t.Parallel()
	got, err := ResolveProfilePath("profiles", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantSep := string(filepath.Separator)
	if !strings.Contains(got, wantSep) {
		t.Fatalf("expected path to contain platform separator %q, got %q", wantSep, got)
	}
}

// TestResolveProfilePath_AbsoluteBaseDir exercises the absolute-baseDir
// branch (the defence-in-depth check). A valid profile name under an
// absolute baseDir should still resolve cleanly.
func TestResolveProfilePath_AbsoluteBaseDir(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	got, err := ResolveProfilePath(baseDir, "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(baseDir, "production.json")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}
