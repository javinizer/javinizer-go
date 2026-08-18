//go:build !windows

package fsutil

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReplacementProbePIDAliveAwareW19_SignalOutcomes(t *testing.T) {
	originalFindProcess := replacementFindProcess
	originalSignalZero := replacementSignalZero
	t.Cleanup(func() {
		replacementFindProcess = originalFindProcess
		replacementSignalZero = originalSignalZero
	})

	for _, tc := range []struct {
		name string
		err  error
		want replacementPIDLiveness
	}{
		{name: "signal succeeds", err: nil, want: replacementPIDAlive},
		{name: "permission denied", err: syscall.EPERM, want: replacementPIDAlive},
		{name: "signal is undecidable", err: errors.New("signal wedged"), want: replacementPIDUnprobeable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replacementSignalZero = func(*os.Process) error { return tc.err }
			require.Equal(t, tc.want, replacementProbePIDAliveAwarePlatform(os.Getpid()))
		})
	}

	t.Run("find process fails", func(t *testing.T) {
		replacementFindProcess = func(int) (*os.Process, error) {
			return nil, errors.New("find process wedged")
		}
		require.Equal(t, replacementPIDUnprobeable, replacementProbePIDAliveAwarePlatform(os.Getpid()))
	})
}

func TestReplacementProbePIDAliveAwareW23_POSIXClassifiesProbeFailures(t *testing.T) {
	originalFindProcess := replacementFindProcess
	originalSignalZero := replacementSignalZero
	t.Cleanup(func() {
		replacementFindProcess = originalFindProcess
		replacementSignalZero = originalSignalZero
	})

	replacementFindProcess = func(int) (*os.Process, error) {
		return nil, errors.New("find process wedged")
	}
	require.Equal(t, replacementPIDUnprobeable, replacementProbePIDAliveAwarePlatform(os.Getpid()))

	replacementFindProcess = func(int) (*os.Process, error) {
		return &os.Process{}, nil
	}
	for _, tc := range []struct {
		name string
		err  error
		want replacementPIDLiveness
	}{
		{name: "missing process", err: syscall.ESRCH, want: replacementPIDDead},
		{name: "process already finished", err: os.ErrProcessDone, want: replacementPIDDead},
		{name: "signal undecidable", err: errors.New("signal wedged"), want: replacementPIDUnprobeable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replacementSignalZero = func(*os.Process) error { return tc.err }
			require.Equal(t, tc.want, replacementProbePIDAliveAwarePlatform(os.Getpid()))
		})
	}
}
