package version

import (
	"bytes"
	"context"
	"errors"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- NewCommand: default output (full version info) ---

func TestMiss_NewCommand_DefaultOutput(t *testing.T) {
	origVersion := appversion.Version
	origCommit := appversion.Commit
	origBuildDate := appversion.BuildDate
	defer func() {
		appversion.Version = origVersion
		appversion.Commit = origCommit
		appversion.BuildDate = origBuildDate
	}()

	appversion.Version = "v9.9.9"
	appversion.Commit = "cafe1234"
	appversion.BuildDate = "2026-06-01T12:00:00Z"

	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := strings.TrimSpace(out.String())
	assert.Contains(t, output, "javinizer v9.9.9")
	assert.Contains(t, output, "cafe1234")
	assert.Contains(t, output, "2026-06-01T12:00:00Z")
}

// --- NewCommand: --short flag ---

func TestMiss_NewCommand_ShortFlag(t *testing.T) {
	origVersion := appversion.Version
	defer func() { appversion.Version = origVersion }()

	appversion.Version = "v7.7.7"

	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"-s"})

	err := cmd.Execute()
	require.NoError(t, err)

	assert.Equal(t, appversion.Short(), strings.TrimSpace(out.String()))
}

// --- NewCommand: --check when update checks are disabled ---

func TestMiss_NewCommand_CheckDisabled(t *testing.T) {
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
	var errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.NoError(t, err)

	assert.Contains(t, strings.TrimSpace(out.String()), "Update checks are disabled in configuration")
}

// --- NewCommand: --check with bad config file ---

func TestMiss_NewCommand_CheckBadConfig(t *testing.T) {
	configPath := testutil.UnreachableConfigPath(t)
	t.Setenv("JAVINIZER_CONFIG", configPath)

	cmd := NewCommand()
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// --- loadConfigForCheck: env override ---

func TestMiss_LoadConfigForCheck_EnvOverride(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "env-override.yaml")
	configText := `config_version: 3
system:
  version_check_enabled: false
  version_check_interval_hours: 24
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(configText), 0644))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	cfg, err := loadConfigForCheck("")
	require.NoError(t, err)
	assert.False(t, cfg.System.VersionCheckEnabled)
}

// --- loadConfigForCheck: invalid config ---

func TestMiss_LoadConfigForCheck_InvalidConfig(t *testing.T) {
	configPath := testutil.UnreachableConfigPath(t)
	_, err := loadConfigForCheck(configPath)
	require.Error(t, err)
}

// --- loadConfigForCheck: JAVINIZER_CONFIG overrides configFile parameter ---

func TestMiss_LoadConfigForCheck_EnvOverridesFile(t *testing.T) {
	// When JAVINIZER_CONFIG is set, it should take precedence over the configFile param
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "env-priority.yaml")
	configText := `config_version: 3
system:
  version_check_enabled: false
  version_check_interval_hours: 24
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(configText), 0644))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	configPath := testutil.UnreachableConfigPath(t)
	cfg, err := loadConfigForCheck(configPath)
	require.NoError(t, err)
	assert.False(t, cfg.System.VersionCheckEnabled)
}

// --- NewCommand: --check with network error (context cancelled) ---

// TestMiss_NewCommand_CheckNetworkError removed: flaky because network may succeed before context cancellation

// --- NewCommand: --check with write error on stderr ---

type missFailingWriter struct{}

func (missFailingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestMiss_NewCommand_CheckWriteError(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	configText := `config_version: 3
system:
  version_check_enabled: true
  version_check_interval_hours: 24
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(configText), 0644))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := NewCommand()
	cmd.SetContext(ctx)
	cmd.SetOut(ioDiscard{})
	cmd.SetErr(missFailingWriter{})
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.Error(t, err)
}

// ioDiscard is a writer that discards all output.
type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

// --- NewCommand: --check with stdout write error on disabled message ---

func TestMiss_NewCommand_CheckDisabledWriteError(t *testing.T) {
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
	cmd.SetOut(missFailingWriter{})
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.Error(t, err)
}

// --- NewCommand: default output write error ---

func TestMiss_NewCommand_DefaultWriteError(t *testing.T) {
	origVersion := appversion.Version
	defer func() { appversion.Version = origVersion }()
	appversion.Version = "v1.0.0"

	cmd := NewCommand()
	cmd.SetOut(missFailingWriter{})
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
}

// --- NewCommand: short output write error ---

func TestMiss_NewCommand_ShortWriteError(t *testing.T) {
	origVersion := appversion.Version
	defer func() { appversion.Version = origVersion }()
	appversion.Version = "v1.0.0"

	cmd := NewCommand()
	cmd.SetOut(missFailingWriter{})
	cmd.SetArgs([]string{"-s"})

	err := cmd.Execute()
	require.Error(t, err)
}
