//go:build windows

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// This separate native control uses the unchanged production wrappers and real
// Windows operations. It does not simulate API failures or use synthetic handles.
func TestWindowsProcessNativeSelfEvidence(t *testing.T) {
	pid := os.Getpid()
	require.Equal(t, replacementPIDAlive, replacementProbePIDAliveAwarePlatform(pid))
	start := replacementProcessStartTimePlatform(pid)
	require.NotNil(t, start)
	require.Positive(t, start.UnixNano(), "the current process must provide usable creation evidence")
}

// These are simulated adapter contracts, not reproductions of kernel access
// denial. Every operation is local: the synthetic handle never reaches an OS API.
func windowsProcessContractOps(t *testing.T, pid int, creation windows.Filetime, openErr, timesErr, closeErr error) (replacementWindowsProcessOps, *[]string) {
	t.Helper()
	const handle = windows.Handle(123)
	var calls []string
	return replacementWindowsProcessOps{
		openProcess: func(gotPID int) (windows.Handle, error) {
			calls = append(calls, "open")
			require.Equal(t, pid, gotPID)
			if openErr != nil {
				return 0, openErr
			}
			return handle, nil
		},
		getProcessTimes: func(got windows.Handle, created, exit, kernel, user *windows.Filetime) error {
			calls = append(calls, "times")
			require.Equal(t, handle, got)
			require.NotNil(t, created)
			require.NotNil(t, exit)
			require.NotNil(t, kernel)
			require.NotNil(t, user)
			if timesErr != nil {
				return timesErr
			}
			*created = creation
			return nil
		},
		closeHandle: func(got windows.Handle) error {
			calls = append(calls, "close")
			require.Equal(t, handle, got)
			return closeErr
		},
	}, &calls
}

func TestWindowsProcessStartTimeLocalAdapterContract(t *testing.T) {
	validNanos := time.Date(2024, 5, 6, 7, 8, 9, 123456700, time.UTC).UnixNano()
	closeFailure := errors.New("simulated close failure")
	for _, tc := range []struct {
		name          string
		pid           int
		creationNanos int64
		openErr       error
		timesErr      error
		closeErr      error
		wantNanos     int64
		wantCalls     []string
	}{
		{name: "zero PID", pid: 0},
		{name: "negative PID", pid: -1},
		{name: "open denied", pid: 42, openErr: windows.ERROR_ACCESS_DENIED, wantCalls: []string{"open"}},
		{name: "query denied", pid: 42, timesErr: windows.ERROR_ACCESS_DENIED, wantCalls: []string{"open", "times", "close"}},
		{name: "query and close fail", pid: 42, timesErr: windows.ERROR_ACCESS_DENIED, closeErr: closeFailure, wantCalls: []string{"open", "times", "close"}},
		{name: "valid creation", pid: 42, creationNanos: validNanos, wantNanos: validNanos, wantCalls: []string{"open", "times", "close"}},
		{name: "close failure preserves evidence", pid: 42, creationNanos: validNanos, closeErr: closeFailure, wantNanos: validNanos, wantCalls: []string{"open", "times", "close"}},
		// Use representable Unix-epoch boundaries, not raw zero FILETIME:
		// Filetime.Nanoseconds can overflow for dates far before the Unix epoch.
		{name: "Unix epoch is not evidence", pid: 42, creationNanos: 0, wantCalls: []string{"open", "times", "close"}},
		{name: "before Unix epoch is not evidence", pid: 42, creationNanos: -100, wantCalls: []string{"open", "times", "close"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops, calls := windowsProcessContractOps(t, tc.pid, windows.NsecToFiletime(tc.creationNanos), tc.openErr, tc.timesErr, tc.closeErr)
			got := replacementProcessStartTimeWithOps(tc.pid, ops)
			if tc.wantNanos == 0 {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
				require.True(t, got.Equal(time.Unix(0, tc.wantNanos)), "creation time must survive unchanged: %v", got)
			}
			require.Equal(t, tc.wantCalls, *calls)
		})
	}
}

func TestWindowsProcessLivenessLocalAdapterContract(t *testing.T) {
	for _, tc := range []struct {
		name      string
		pid       int
		openErr   error
		closeErr  error
		want      replacementPIDLiveness
		wantCalls []string
	}{
		{name: "zero PID", pid: 0, want: replacementPIDDead},
		{name: "negative PID", pid: -1, want: replacementPIDDead},
		{name: "open proves alive", pid: 42, want: replacementPIDAlive, wantCalls: []string{"open", "close"}},
		{name: "close failure retains liveness", pid: 42, closeErr: errors.New("simulated close failure"), want: replacementPIDAlive, wantCalls: []string{"open", "close"}},
		{name: "invalid PID is dead", pid: 42, openErr: windows.ERROR_INVALID_PARAMETER, want: replacementPIDDead, wantCalls: []string{"open"}},
		{name: "wrapped invalid PID is dead", pid: 42, openErr: fmt.Errorf("open: %w", windows.ERROR_INVALID_PARAMETER), want: replacementPIDDead, wantCalls: []string{"open"}},
		{name: "access denied is undecidable", pid: 42, openErr: windows.ERROR_ACCESS_DENIED, want: replacementPIDUnprobeable, wantCalls: []string{"open"}},
		{name: "wrapped access denied is undecidable", pid: 42, openErr: fmt.Errorf("open: %w", windows.ERROR_ACCESS_DENIED), want: replacementPIDUnprobeable, wantCalls: []string{"open"}},
		{name: "other failure is undecidable", pid: 42, openErr: errors.New("simulated open failure"), want: replacementPIDUnprobeable, wantCalls: []string{"open"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops, calls := windowsProcessContractOps(t, tc.pid, windows.Filetime{}, tc.openErr, nil, tc.closeErr)
			require.Equal(t, tc.want, replacementProbePIDAliveAwareWithOps(tc.pid, ops))
			require.Equal(t, tc.wantCalls, *calls, "liveness must not query process times")
		})
	}
}
