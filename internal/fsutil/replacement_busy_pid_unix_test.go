//go:build !windows

package fsutil

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReplacementProcessAliveW19_SignalOutcomes(t *testing.T) {
	originalFindProcess := replacementFindProcess
	originalSignalZero := replacementSignalZero
	t.Cleanup(func() {
		replacementFindProcess = originalFindProcess
		replacementSignalZero = originalSignalZero
	})

	for _, tc := range []struct {
		name      string
		signalErr error
		wantAlive bool
	}{
		{name: "signal succeeds", signalErr: nil, wantAlive: true},
		{name: "permission denied", signalErr: syscall.EPERM, wantAlive: true},
		{name: "signal fails", signalErr: errors.New("signal wedged"), wantAlive: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replacementSignalZero = func(*os.Process) error { return tc.signalErr }
			require.Equal(t, tc.wantAlive, replacementProcessAlive(os.Getpid()))
		})
	}

	t.Run("find process fails", func(t *testing.T) {
		replacementFindProcess = func(int) (*os.Process, error) {
			return nil, errors.New("find process wedged")
		}
		require.False(t, replacementProcessAlive(os.Getpid()))
	})
}
