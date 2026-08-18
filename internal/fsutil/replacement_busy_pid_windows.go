//go:build windows

package fsutil

import (
	"errors"
	"time"

	"golang.org/x/sys/windows"
)

// Windows process seams stay injectable so reuse-discrimination tests can feed
// start-time evidence without owning a recycled PID on a live host.
var replacementWindowsOpenProcess = func(pid int) (windows.Handle, error) {
	return windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
}
var replacementWindowsGetProcessTimes = windows.GetProcessTimes
var replacementWindowsCloseHandle = windows.CloseHandle

// Windows now answers the same owner-start-time question as the Linux /proc
// seam (K4): K32 GetProcessTimes exposes the process creation stamp, and the
// classifier's contract is identical — an owner whose start time is AFTER the
// marker timestamp proves the recorded PID was recycled, so the marker is
// stale. OpenProcess/PROCESS_QUERY_LIMITED_INFORMATION is enough for
// GetProcessTimes on ordinary processes, including ones owned by other
// sessions. When the creation time is unobtainable (handle refused,
// GetProcessTimes denied for a protected process, or a zero/unreadable
// FILETIME), the seam returns nil and classification retains the documented
// liveness-only fallback: a positive liveness proof keeps the marker busy.
func replacementProcessStartTimePlatform(pid int) *time.Time {
	if pid <= 0 {
		return nil
	}
	handle, err := replacementWindowsOpenProcess(pid)
	if err != nil {
		return nil // liveness-only fallback: no handle, no start-time proof
	}
	defer func() { _ = replacementWindowsCloseHandle(handle) }()
	var creation, exit, kernel, user windows.Filetime
	if err := replacementWindowsGetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return nil // liveness-only fallback: start time unobtainable
	}
	return replacementStartTimeFromUnixNano(creation.Nanoseconds())
}

func replacementProbePIDAliveAwarePlatform(pid int) replacementPIDLiveness {
	if pid <= 0 {
		return replacementPIDDead
	}

	handle, err := replacementWindowsOpenProcess(pid)
	if err == nil {
		// A successfully opened process is the owner-liveness proof. A close
		// failure does not change that proof and must not turn it into stale.
		_ = replacementWindowsCloseHandle(handle)
		return replacementPIDAlive
	}
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		// OpenProcess documents ERROR_INVALID_PARAMETER for a PID that no
		// longer exists, so a well-formed marker can be reclaimed.
		return replacementPIDDead
	}
	// Access-denied and other failures do not prove that the owner is gone.
	return replacementPIDUnprobeable
}
