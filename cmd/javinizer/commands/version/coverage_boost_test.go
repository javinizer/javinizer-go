package version

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

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
