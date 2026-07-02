package version

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/update"
	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCommand_VersionOutput tests the default version output path.
func TestNewCommand_VersionOutput(t *testing.T) {
	origVersion := appversion.Version
	origCommit := appversion.Commit
	origBuildDate := appversion.BuildDate
	defer func() {
		appversion.Version = origVersion
		appversion.Commit = origCommit
		appversion.BuildDate = origBuildDate
	}()

	appversion.Version = "v2.0.0"
	appversion.Commit = "deadbeef"
	appversion.BuildDate = "2026-01-01T00:00:00Z"

	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, out.String(), "javinizer v2.0.0")
}

// TestNewCommand_ShortOutput tests the --short flag output.
func TestNewCommand_ShortOutput(t *testing.T) {
	origVersion := appversion.Version
	defer func() { appversion.Version = origVersion }()

	appversion.Version = "v3.0.0-test"

	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"-s"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, out.String(), appversion.Short())
}

// TestNewCommand_CheckDisabled tests --check when update checks are disabled.
func TestNewCommand_CheckDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	configText := `config_version: 3
system:
  version_check_enabled: false
  version_check_interval_hours: 24
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(configText), 0644))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Update checks are disabled")
}

// TestNewCommand_NoArgs tests that version command accepts no args.
func TestNewCommand_NoArgs(t *testing.T) {
	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestNewCommand_LoadConfigForCheck tests loadConfigForCheck with env override.
func TestNewCommand_LoadConfigForCheck_EnvOverride(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "custom.yaml")
	configText := `config_version: 3
system:
  version_check_enabled: false
  version_check_interval_hours: 24
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(configText), 0644))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Update checks are disabled")
}

var errConfigFail = configLoadErr{}

type configLoadErr struct{}

func (configLoadErr) Error() string { return "failed to load config: missing" }

// TestNewCommand_CheckUpdateAvailable covers the --check output branch where an
// update IS available — including the "Run 'javinizer upgrade'" hint. Uses a
// stubbed runVersionCheck so no network is needed.
func TestNewCommand_CheckUpdateAvailable(t *testing.T) {
	restore := SetRunVersionCheck(func(_ context.Context, _ string) (*checkOutcome, error) {
		return &checkOutcome{available: true, version: "v9.9.9"}, nil
	})
	defer SetRunVersionCheck(restore)

	cmd := NewCommand()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"--check"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Update available: v9.9.9")
	assert.Contains(t, errBuf.String(), "Run 'javinizer upgrade' to update.")
}

// TestNewCommand_CheckUpToDate covers the --check output branch where the user
// is already on the latest version.
func TestNewCommand_CheckUpToDate(t *testing.T) {
	restore := SetRunVersionCheck(func(_ context.Context, _ string) (*checkOutcome, error) {
		return &checkOutcome{available: false, version: "v1.0.0"}, nil
	})
	defer SetRunVersionCheck(restore)

	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--check"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "You are running the latest version")
}

// TestNewCommand_CheckError covers the --check error-output branch.
func TestNewCommand_CheckError(t *testing.T) {
	restore := SetRunVersionCheck(func(_ context.Context, _ string) (*checkOutcome, error) {
		return &checkOutcome{errMsg: "network unreachable"}, nil
	})
	defer SetRunVersionCheck(restore)

	cmd := NewCommand()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"--check"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, errBuf.String(), "Error checking for updates: network unreachable")
}

// TestNewCommand_CheckConfigError covers the --check path where config loading
// itself fails — the error must propagate from RunE.
func TestNewCommand_CheckConfigError(t *testing.T) {
	restore := SetRunVersionCheck(func(_ context.Context, _ string) (*checkOutcome, error) {
		return nil, errConfigFail
	})
	defer SetRunVersionCheck(restore)

	cmd := NewCommand()
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// failWriter is a writer that always returns an error, for covering the
// write-error sub-branches in the --check output paths.
type failWriter struct{}

func (failWriter) Write(_ []byte) (int, error) { return 0, errors.New("write failed") }

// failAfterN fails on the (n+1)th write, succeeding the first n.
type failAfterN struct{ n int }

func (w *failAfterN) Write(p []byte) (int, error) {
	if w.n <= 0 {
		return 0, errors.New("write failed")
	}
	w.n--
	return len(p), nil
}

// TestNewCommand_CheckAvailable_StdoutWriteError covers the write-error branch
// after writing "Update available" to stdout.
func TestNewCommand_CheckAvailable_StdoutWriteError(t *testing.T) {
	restore := SetRunVersionCheck(func(_ context.Context, _ string) (*checkOutcome, error) {
		return &checkOutcome{available: true, version: "v9.9.9"}, nil
	})
	defer SetRunVersionCheck(restore)

	cmd := NewCommand()
	cmd.SetOut(failWriter{})
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

// TestNewCommand_CheckAvailable_StderrWriteError covers the write-error branch
// after writing "Update available" to stderr (stdout succeeds first).
func TestNewCommand_CheckAvailable_StderrWriteError(t *testing.T) {
	restore := SetRunVersionCheck(func(_ context.Context, _ string) (*checkOutcome, error) {
		return &checkOutcome{available: true, version: "v9.9.9"}, nil
	})
	defer SetRunVersionCheck(restore)

	cmd := NewCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(failWriter{})
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

// TestNewCommand_CheckAvailable_RunHintWriteError covers the write-error branch
// after writing "Run 'javinizer upgrade'" to stderr (the second stderr write).
func TestNewCommand_CheckAvailable_RunHintWriteError(t *testing.T) {
	restore := SetRunVersionCheck(func(_ context.Context, _ string) (*checkOutcome, error) {
		return &checkOutcome{available: true, version: "v9.9.9"}, nil
	})
	defer SetRunVersionCheck(restore)

	cmd := NewCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&failAfterN{n: 1}) // first stderr write ok, second fails
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

// TestNewCommand_CheckDisabled_StdoutWriteError covers the write-error branch
// in the disabled path.
func TestNewCommand_CheckDisabled_StdoutWriteError(t *testing.T) {
	restore := SetRunVersionCheck(func(_ context.Context, _ string) (*checkOutcome, error) {
		return &checkOutcome{disabled: true}, nil
	})
	defer SetRunVersionCheck(restore)

	cmd := NewCommand()
	cmd.SetOut(failWriter{})
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

// TestNewCommand_CheckUpToDate_StdoutWriteError covers the write-error branch
// in the up-to-date path.
func TestNewCommand_CheckUpToDate_StdoutWriteError(t *testing.T) {
	restore := SetRunVersionCheck(func(_ context.Context, _ string) (*checkOutcome, error) {
		return &checkOutcome{available: false, version: "v1.0.0"}, nil
	})
	defer SetRunVersionCheck(restore)

	cmd := NewCommand()
	cmd.SetOut(failWriter{})
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

// TestNewCommand_CheckError_StderrWriteError covers the write-error branch in
// the error-message path (writing "Error checking for updates" to stderr).
func TestNewCommand_CheckError_StderrWriteError(t *testing.T) {
	restore := SetRunVersionCheck(func(_ context.Context, _ string) (*checkOutcome, error) {
		return &checkOutcome{errMsg: "network unreachable"}, nil
	})
	defer SetRunVersionCheck(restore)

	cmd := NewCommand()
	cmd.SetErr(failWriter{})
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

// TestLoadConfigForCheck_PrepareError covers the config.Prepare failure path in
// loadConfigForCheck (an unsupported config version makes Prepare error).
func TestLoadConfigForCheck_PrepareError(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	// A config_version far in the future triggers Prepare's "newer than
	// supported" error.
	configText := `config_version: 999
system:
  version_check_enabled: true
  version_check_interval_hours: 24
scrapers:
  priority:
    - r18
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(configText), 0644))

	_, err := loadConfigForCheck(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer than supported")
}

// failingChecker is a Checker stub whose CheckLatestVersion always errors, used
// to drive the real service's ForceCheck into the UpdateSourceError branch.
type failingChecker struct{ err error }

func (c *failingChecker) CheckLatestVersion(_ context.Context) (*update.VersionInfo, error) {
	return nil, c.err
}

// emptyErrorChecker returns a VersionInfo whose processing yields an
// UpdateSourceError state with an empty Error string, covering the
// "Unknown error" fallthrough branch.
type emptyErrorChecker struct{}

func (c *emptyErrorChecker) CheckLatestVersion(_ context.Context) (*update.VersionInfo, error) {
	return &update.VersionInfo{Version: ""}, nil
}

// TestRunVersionCheck_RealErrorBranch covers the runVersionCheck error-translation
// branch (ForceCheck returns an UpdateSourceError state) by injecting a failing
// checker into the service — no network, no shared on-disk cache, deterministic.
func TestRunVersionCheck_RealErrorBranch(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	configText := `config_version: 3
system:
  version_check_enabled: true
  version_check_interval_hours: 24
scrapers:
  priority:
    - r18
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(configText), 0644))

	prev := updateChecker
	updateChecker = &failingChecker{err: errors.New("github unreachable")}
	t.Cleanup(func() { updateChecker = prev })

	prevPath := updateStatePath
	updateStatePath = filepath.Join(t.TempDir(), "update-state.json")
	t.Cleanup(func() { updateStatePath = prevPath })

	outcome, err := runVersionCheck(context.Background(), cfgPath)
	require.NoError(t, err, "runVersionCheck translates the error into an outcome, not a Go error")
	require.NotNil(t, outcome)
	assert.NotEmpty(t, outcome.errMsg, "the ForceCheck failure must surface as an error message")
	assert.Contains(t, outcome.errMsg, "github unreachable")
}

// TestRunVersionCheck_UnknownErrorFallthrough covers the "Unknown error"
// branch: an error state with an empty Error string surfaces the generic
// message rather than an empty one.
func TestRunVersionCheck_UnknownErrorFallthrough(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	configText := `config_version: 3
system:
  version_check_enabled: true
  version_check_interval_hours: 24
scrapers:
  priority:
    - r18
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(configText), 0644))

	prev := updateChecker
	updateChecker = &emptyErrorChecker{}
	t.Cleanup(func() { updateChecker = prev })

	prevPath := updateStatePath
	updateStatePath = filepath.Join(t.TempDir(), "update-state.json")
	t.Cleanup(func() { updateStatePath = prevPath })

	outcome, err := runVersionCheck(context.Background(), cfgPath)
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Contains(t, outcome.errMsg, "Unknown error occurred while checking for updates")
}
