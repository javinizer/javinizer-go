package version_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	versioncmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/version"
	"github.com/javinizer/javinizer-go/internal/config"
	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
  version_check_enabled: false
  version_check_interval_hours: 24
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

func TestVersionCommand_CheckRejectsInvalidRuntimeOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = false
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "google"
	require.NoError(t, config.Save(cfg, path))
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	t.Setenv("JAVINIZER_CONFIG", path)
	t.Setenv("METADATA_TRANSLATION_PROVIDER", "unsupported-fixture-provider")

	cmd := versioncmd.NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--check"})
	err = cmd.Execute()
	require.ErrorContains(t, err, "failed to load config:")
	require.ErrorContains(t, err, "metadata.translation.provider")
	require.Empty(t, out.String())
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, after)
}
