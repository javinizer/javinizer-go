package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

// TestPathValidator_BrowseBypassesEmptyAllowlist is the catch-22 regression:
// a non-picker validator with an empty allowlist rejects every path, so the
// browse endpoint can never list a directory to add. Browse mode must accept
// the same path (denylist still applies — see the next test).
func TestPathValidator_BrowseBypassesEmptyAllowlist(t *testing.T) {
	dir := t.TempDir()
	cfg := &SecurityNarrowConfig{AllowedDirectories: nil, DeniedDirectories: nil}

	if _, err := ValidateScanPath(dir, cfg); err == nil {
		t.Fatal("non-picker ValidateScanPath with empty allowlist should reject, got nil error")
	}
	// macOS t.TempDir() lives under /var which symlinks to /private/var; the
	// validator canonicalizes, so compare against the resolved path.
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		canonical = dir
	}
	if got, err := ValidateBrowsePath(dir, cfg); err != nil {
		t.Fatalf("picker ValidateBrowsePath should accept with empty allowlist: %v", err)
	} else if got != canonical {
		t.Errorf("picker returned %q, want %q", got, canonical)
	}
}

// TestPathValidator_BrowseStillEnforcesDenylist asserts that picker mode keeps
// the denylist — even when browsing to configure the allowlist, sensitive
// system directories (/proc, /sys, /dev) and config-denied paths stay blocked.
func TestPathValidator_BrowseStillEnforcesDenylist(t *testing.T) {
	allowed := t.TempDir()
	denied := t.TempDir()
	cfg := &SecurityNarrowConfig{
		AllowedDirectories: []string{allowed},
		DeniedDirectories:  []string{denied},
	}

	if _, err := ValidateBrowsePath(denied, cfg); err == nil {
		t.Fatal("picker should still reject config-denied directories")
	}
}

// TestPathValidator_BrowseRejectsNonExistent confirms picker mode still runs
// the exists + is-dir checks (it is not an unconditional pass-through).
func TestPathValidator_BrowseRejectsNonExistent(t *testing.T) {
	cfg := &SecurityNarrowConfig{}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := ValidateBrowsePath(missing, cfg); err == nil {
		t.Fatal("picker should reject a non-existent path")
	}
}

// TestValidateBrowsePath_NilConfigErrors covers the nil-config guard: a nil
// security config must produce an explicit error rather than dereferencing it.
func TestValidateBrowsePath_NilConfigErrors(t *testing.T) {
	if _, err := ValidateBrowsePath(t.TempDir(), nil); err == nil {
		t.Fatal("ValidateBrowsePath with nil config must return an error")
	}
}

// TestValidateAndOpenBrowsePath_NilConfigErrors covers the nil-config guard on
// the TOCTOU-safe browse variant too.
func TestValidateAndOpenBrowsePath_NilConfigErrors(t *testing.T) {
	if _, _, err := ValidateAndOpenBrowsePath(t.TempDir(), nil); err == nil {
		t.Fatal("ValidateAndOpenBrowsePath with nil config must return an error")
	}
}

// TestValidateAndOpenBrowsePath_OpenHandle verifies the TOCTOU-safe picker
// variant returns an open file handle the caller must close.
func TestValidateAndOpenBrowsePath_OpenHandle(t *testing.T) {
	dir := t.TempDir()
	cfg := &SecurityNarrowConfig{}
	f, got, err := ValidateAndOpenBrowsePath(dir, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = f.Close() }()
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		canonical = dir
	}
	if got != canonical {
		t.Errorf("returned path = %q, want %q", got, canonical)
	}
	if _, err := f.Stat(); err != nil {
		t.Errorf("returned file is not usable: %v", err)
	}
}

// TestNewPathValidatorBrowse_Flags asserts the constructor wires picker mode
// without an allowlist but preserves the denylist + UNC settings.
func TestNewPathValidatorBrowse_Flags(t *testing.T) {
	v := NewPathValidatorBrowse(afero.NewOsFs(), []string{"/denied"}, true, []string{"srv"})
	if !!v.enforceAllowlist {
		t.Error("picker flag not set")
	}
	if len(v.allow) != 0 {
		t.Errorf("allowlist should be empty, got %v", v.allow)
	}
	if len(v.deny) != 1 || v.deny[0] != "/denied" {
		t.Errorf("denylist not preserved, got %v", v.deny)
	}
	if !v.allowUNC {
		t.Error("allowUNC not preserved")
	}
}

// TestValidateAndOpenBrowsePath_RealProcDenied is a smoke test that on Linux
// the built-in denylist blocks /proc even in picker mode. Skipped on non-Linux
// where /proc does not exist.
func TestValidateAndOpenBrowsePath_RealProcDenied(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc not present on this platform")
	}
	cfg := &SecurityNarrowConfig{}
	if _, err := ValidateBrowsePath("/proc", cfg); err == nil {
		t.Fatal("picker should reject the built-in denied directory /proc")
	}
}
