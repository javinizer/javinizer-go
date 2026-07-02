package upgrade_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	upgradecmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/upgrade"
	"github.com/javinizer/javinizer-go/internal/update"
	"github.com/javinizer/javinizer-go/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubUpgrade replaces upgradecmd.runUpgrade for deterministic command tests.
type stubUpgrade struct {
	result *update.UpgradeResult
	err    error
	called bool
}

func (s *stubUpgrade) run(ctx context.Context, opts update.UpgradeOptions) (*update.UpgradeResult, error) {
	s.called = true
	return s.result, s.err
}

func TestUpgradeCommand_Flags(t *testing.T) {
	cmd := upgradecmd.NewCommand()
	assert.Equal(t, "upgrade", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("check"))
	assert.NotNil(t, cmd.Flags().Lookup("force"))
}

func TestUpgradeCommand_Success(t *testing.T) {
	stub := &stubUpgrade{result: &update.UpgradeResult{Upgraded: true, LatestVersion: "v1.0.0"}}
	restore := upgradecmd.SetRunUpgrade(stub.run)
	defer upgradecmd.SetRunUpgrade(restore)

	var out bytes.Buffer
	cmd := upgradecmd.NewCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())
	assert.True(t, stub.called)
}

func TestUpgradeCommand_ErrorPrintsToStderrAndReturnsErr(t *testing.T) {
	stub := &stubUpgrade{result: &update.UpgradeResult{}, err: errors.New("network down")}
	restore := upgradecmd.SetRunUpgrade(stub.run)
	defer upgradecmd.SetRunUpgrade(restore)

	var stderr bytes.Buffer
	cmd := upgradecmd.NewCommand()
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, stderr.String(), "Upgrade failed: network down")
}

func TestUpgradeCommand_HandoffIsNotAnError(t *testing.T) {
	// A package-manager handoff returns no error from RunE even though the
	// upgrade did not happen — the user was told to run brew/scoop instead.
	stub := &stubUpgrade{result: &update.UpgradeResult{Handoff: true}, err: errors.New("would clobber brew")}
	restore := upgradecmd.SetRunUpgrade(stub.run)
	defer upgradecmd.SetRunUpgrade(restore)

	var stderr bytes.Buffer
	cmd := upgradecmd.NewCommand()
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.NoError(t, err, "handoff should not surface as a CLI error")
	assert.Contains(t, stderr.String(), "Upgrade failed")
}

func TestUpgradeCommand_PassesCurrentVersion(t *testing.T) {
	orig := version.Version
	defer func() { version.Version = orig }()
	version.Version = "v1.2.3"

	var captured update.UpgradeOptions
	restore := upgradecmd.SetRunUpgrade(func(ctx context.Context, opts update.UpgradeOptions) (*update.UpgradeResult, error) {
		captured = opts
		return &update.UpgradeResult{UpToDate: true}, nil
	})
	defer upgradecmd.SetRunUpgrade(restore)

	cmd := upgradecmd.NewCommand()
	cmd.SetArgs([]string{"--check"})
	require.NoError(t, cmd.Execute())
	assert.Equal(t, "v1.2.3", captured.CurrentVersion)
	assert.True(t, captured.CheckOnly)
}
