//go:build unix

package config

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigLockProcessProbe(t *testing.T) {
	require.False(t, isProcessAlive(0))
	require.False(t, isProcessAlive(-1))
	require.True(t, isProcessAlive(os.Getpid()))
	require.True(t, isProcessAlive(os.Getpid()), "releasing the probe must not affect the process")

	// Re-execute this test binary without selecting any tests, then wait so
	// evaluation cannot mistake a still-running or zombie child for a dead PID.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	require.NoError(t, cmd.Run())
	require.False(t, isProcessAlive(cmd.Process.Pid))
}
