package version_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	versioncmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/version"
	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingWriter struct{}

func (w failingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestVersionCommand_Default(t *testing.T) {
	origVersion := appversion.Version
	origCommit := appversion.Commit
	origBuildDate := appversion.BuildDate
	defer func() {
		appversion.Version = origVersion
		appversion.Commit = origCommit
		appversion.BuildDate = origBuildDate
	}()

	appversion.Version = "v1.2.3"
	appversion.Commit = "abcdef123456"
	appversion.BuildDate = "2026-02-23T00:00:00Z"

	cmd := versioncmd.NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := strings.TrimSpace(out.String())
	assert.Contains(t, output, "javinizer v1.2.3")
	assert.Contains(t, output, "commit: abcdef123456")
	assert.Contains(t, output, "built: 2026-02-23T00:00:00Z")
}

func TestVersionCommand_Short(t *testing.T) {
	origVersion := appversion.Version
	origCommit := appversion.Commit
	defer func() {
		appversion.Version = origVersion
		appversion.Commit = origCommit
	}()

	appversion.Version = "dev"
	appversion.Commit = "abcdef123456"

	cmd := versioncmd.NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--short"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := strings.TrimSpace(out.String())
	assert.Equal(t, appversion.Short(), output)
}

func TestVersionCommand_CheckHonorsJAVINIZER_CONFIG(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	configText := `config_version: 3
system:
  update_enabled: false
  update_check_interval_hours: 24
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(configText), 0644))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	cmd := versioncmd.NewCommand()
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.NoError(t, err)

	assert.Contains(t, strings.TrimSpace(out.String()), "Update checks are disabled in configuration")
	assert.Equal(t, "", strings.TrimSpace(errBuf.String()))
}

func TestVersionCommand_CheckReturnsStderrWriteError(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	configText := `config_version: 3
system:
  update_enabled: true
  update_check_interval_hours: 24
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(configText), 0644))
	t.Setenv("JAVINIZER_CONFIG", cfgPath)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := versioncmd.NewCommand()
	cmd.SetContext(ctx)
	cmd.SetOut(io.Discard)
	cmd.SetErr(failingWriter{})
	cmd.SetArgs([]string{"--check"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}
