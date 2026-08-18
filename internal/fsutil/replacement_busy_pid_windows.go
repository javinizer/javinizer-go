//go:build windows

package fsutil

import (
	"errors"

	"golang.org/x/sys/windows"
)

func replacementProbePIDAliveAwarePlatform(pid int) replacementPIDLiveness {
	if pid <= 0 {
		return replacementPIDDead
	}

	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err == nil {
		// A successfully opened process is the owner-liveness proof. A close
		// failure does not change that proof and must not turn it into stale.
		_ = windows.CloseHandle(handle)
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
