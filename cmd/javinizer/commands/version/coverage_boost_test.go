package version

import (
	"bytes"
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
