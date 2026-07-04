package app

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/desktop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommand_Properties(t *testing.T) {
	cmd := NewCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "app", cmd.Use)
	assert.Equal(t, "Launch the Javinizer desktop app", cmd.Short)
	assert.NotEmpty(t, cmd.Long, "Long description should be set")
	// NoArgs rejects positional arguments.
	require.NotNil(t, cmd.Args)
	assert.NoError(t, cmd.Args(cmd, nil))
	assert.Error(t, cmd.Args(cmd, []string{"unexpected"}))
}

func TestNewCommand_RunE_CLIBuildReturnsDesktopStubError(t *testing.T) {
	// In a normal CLI build (no `desktop` tag), desktop.Run is the stub in
	// app_stub.go and returns an error. The command is a thin wrapper, so
	// RunE must surface that error rather than silently succeeding.
	orig := desktop.BuildDesktop
	desktop.BuildDesktop = "0"
	defer func() { desktop.BuildDesktop = orig }()

	cmd := NewCommand()
	require.NotNil(t, cmd.RunE)
	// The --config flag is a root persistent flag, not defined on this
	// subcommand in isolation; GetString returns ("", error) which the
	// command ignores, so ConfigFile is "" — fine for the stub path.
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "desktop mode is not built")
}
