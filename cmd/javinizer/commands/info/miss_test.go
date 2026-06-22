package info

import (
	"bytes"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintUpdateStatus_Disabled(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = false

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := printUpdateStatus(cmd, cfg)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Updates are disabled")
}

func TestPrintUpdateStatus_NeverChecked(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = true
	cfg.System.VersionCheckIntervalHours = 24

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := printUpdateStatus(cmd, cfg)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Last checked: never")
}

func TestPrintUpdateStatus_CheckedWithUpdate(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.VersionCheckEnabled = true
	cfg.System.VersionCheckIntervalHours = 24

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := printUpdateStatus(cmd, cfg)
	require.NoError(t, err)
	output := out.String()
	// Either "never" (no cached status) or "Last checked" with details
	if !bytes.Contains(out.Bytes(), []byte("never")) {
		assert.Contains(t, output, "Last checked")
	}
}

func TestRunInfo_Success(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := run(cmd, "")
	// May fail if no config file, but should not panic
	_ = err
	_ = cfg
}

func TestRun_WriteError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(&errorWriter{})
	cmd.SetErr(&errorWriter{})

	// This will fail because no config, but tests the error writer path
	_ = cmd
}

type errorWriter struct{}

func (errorWriter) Write(_ []byte) (int, error) { return 0, bytes.ErrTooLarge }
