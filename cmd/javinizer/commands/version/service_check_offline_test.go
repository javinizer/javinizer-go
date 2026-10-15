package version

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/update"
	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type offlineVersionChecker struct {
	result *update.VersionInfo
	err    error
	calls  int
}

func (c *offlineVersionChecker) CheckLatestVersion(context.Context) (*update.VersionInfo, error) {
	c.calls++
	return c.result, c.err
}

// newOfflineVersionCheck uses the existing checker seam while preserving real
// config loading, ForceCheck, version comparison, state persistence and output.
// Each test gets an empty cache; no existing release state can mask a failure.
func newOfflineVersionCheck(t *testing.T, checker update.Checker) (*cobra.Command, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`config_version: 3
system:
  version_check_enabled: true
  version_check_interval_hours: 24
`), 0o600))
	t.Setenv("JAVINIZER_CONFIG", configPath)

	previousChecker, previousPath := updateChecker, updateStatePath
	previousVersion := appversion.Version
	t.Cleanup(func() {
		updateChecker, updateStatePath = previousChecker, previousPath
		appversion.Version = previousVersion
	})
	updateChecker = checker
	updateStatePath = filepath.Join(dir, "update-state.json")
	appversion.Version = "v1.0.0"

	cmd := NewCommand()
	cmd.SetArgs([]string{"--check"})
	return cmd, updateStatePath
}

func TestNewCommand_CheckForceCheckError(t *testing.T) {
	checker := &offlineVersionChecker{err: errors.New("synthetic update failure")}
	cmd, statePath := newOfflineVersionCheck(t, checker)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute(), "the service translates checker errors into stderr output")
	assert.Equal(t, 1, checker.calls)
	assert.Empty(t, stdout.String())
	assert.Equal(t, "Error checking for updates: synthetic update failure\n", stderr.String())
	assert.NoFileExists(t, statePath, "a failed first check must not persist a successful release")
}

func TestNewCommand_CheckUpdateAvailable_ThroughService(t *testing.T) {
	checker := &offlineVersionChecker{result: &update.VersionInfo{Version: "v2.0.0", TagName: "v2.0.0"}}
	cmd, statePath := newOfflineVersionCheck(t, checker)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())
	assert.Equal(t, 1, checker.calls)
	assert.Equal(t, "Update available: v2.0.0 (current: v1.0.0)\n", stdout.String())
	assert.Equal(t, "Update available: v2.0.0 (current: v1.0.0)\nRun 'javinizer upgrade' to update.\n", stderr.String())
	data, err := os.ReadFile(statePath)
	require.NoError(t, err)
	var state struct {
		Version   string `json:"version"`
		Available bool   `json:"available"`
		Source    string `json:"source"`
	}
	require.NoError(t, json.Unmarshal(data, &state))
	assert.Equal(t, "v2.0.0", state.Version)
	assert.True(t, state.Available)
	assert.Equal(t, string(update.UpdateSourceFresh), state.Source)
}

func TestVersionCommand_CheckReturnsStderrWriteError(t *testing.T) {
	checker := &offlineVersionChecker{err: errors.New("synthetic update failure")}
	cmd, _ := newOfflineVersionCheck(t, checker)
	cmd.SetOut(io.Discard)
	cmd.SetErr(missFailingWriter{})

	require.EqualError(t, cmd.Execute(), "write failed")
	assert.Equal(t, 1, checker.calls)
}
