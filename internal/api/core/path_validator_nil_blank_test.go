package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dirUnderCWD creates a unique directory directly beneath the process working
// directory and returns its absolute path. This is required to reliably
// reproduce the blank-deny-entry bug: filepath.Clean("") == "." resolves to CWD
// via filepath.Abs, so only a path genuinely under CWD would be denied by the
// buggy (pre-fix) deny loop. t.TempDir() creates dirs under the OS temp dir
// (e.g. /var/folders/...), which is NOT under CWD, so it would mask the bug.
func dirUnderCWD(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	dir, err := os.MkdirTemp(wd, "javinizer-blank-deny-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	abs, err := filepath.Abs(dir)
	require.NoError(t, err)
	return abs
}

// TestPathValidator_BlankDenyEntryDoesNotBlockCWD verifies that empty denylist
// entries are skipped instead of being normalized to "." (the process working
// directory) by filepath.Clean, which would unintentionally deny the CWD and all
// its children. The allowlist already skips blanks; this asserts the IsDirAllowed
// deny loop (path_validator.go ~line 277) does too.
//
// The tested dir is created under CWD so that, with the fix reverted, the blank
// deny entry resolves to CWD and isPathWithinCanonical(dir, CWD) returns true,
// genuinely denying it — making this a real regression catcher.
func TestPathValidator_BlankDenyEntryDoesNotBlockCWD(t *testing.T) {
	dir := dirUnderCWD(t)
	// deny list contains a blank entry alongside a real one.
	v := NewPathValidator(afero.NewOsFs(), []string{dir}, []string{"", "/nonexistent"})

	// dir must remain allowed despite the blank deny entry resolving to CWD.
	assert.True(t, v.IsDirAllowed(dir),
		"blank deny entry must not resolve to CWD and deny the allowed dir")
}

// TestValidateScanPath_BlankDenyEntryDoesNotBlock exercises the unexported
// isPathDenied deny loop (path_validator.go ~line 223) reached via the
// ValidateScanPath -> ValidateDir path. Same blank-entry regression as above,
// but through the validation pipeline that returns an error (not a bool).
func TestValidateScanPath_BlankDenyEntryDoesNotBlock(t *testing.T) {
	dir := dirUnderCWD(t)
	cfg := &SecurityNarrowConfig{
		AllowedDirectories: []string{dir},
		DeniedDirectories:  []string{"", "/nonexistent"},
	}

	// A blank deny entry must not resolve to CWD and reject the allowed dir.
	_, err := ValidateScanPath(dir, cfg)
	require.NoError(t, err,
		"blank deny entry must not resolve to CWD and reject the allowed dir")
}

// TestValidateScanPath_NilConfigReturnsError verifies a nil security config
// yields a controlled error rather than a nil-pointer panic.
func TestValidateScanPath_NilConfigReturnsError(t *testing.T) {
	_, err := ValidateScanPath("/some/path", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "security config is required")
}

// TestValidateAndOpenPath_NilConfigReturnsError covers the TOCTOU-safe variant.
func TestValidateAndOpenPath_NilConfigReturnsError(t *testing.T) {
	_, _, err := ValidateAndOpenPath("/some/path", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "security config is required")
}

// TestAPIRuntime_ReloadConfig_NilConfigReturnsError verifies a nil config yields
// a controlled error rather than panicking on cfg.Scrapers.Finalize.
func TestAPIRuntime_ReloadConfig_NilConfigReturnsError(t *testing.T) {
	rt := NewAPIRuntime(&APIDeps{})
	err := rt.ReloadConfig(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config is nil")
}
