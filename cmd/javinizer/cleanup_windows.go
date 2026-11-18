//go:build windows

package main

import (
	"os"
	"path/filepath"
)

// cleanupStaleWindowsOldExeWithResolver removes a leftover <exe>.old from a prior
// interrupted desktop self-upgrade. Windows can't overwrite a running exe, so
// the upgrade renames it to .old and the detached helper deletes it after the
// swap; if the helper was killed before cleanup, the .old survives and is
// cleared here on the next launch.
func cleanupStaleWindowsOldExeWithResolver(executable func() (string, error)) {
	exe, err := executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		_ = os.Remove(resolved + ".old")
	} else {
		_ = os.Remove(exe + ".old")
	}
}

// Resolve the running executable at the production boundary; the helper keeps
// resolution failures fail-closed without a mutable process-wide hook.
func cleanupStaleWindowsOldExe() {
	cleanupStaleWindowsOldExeWithResolver(os.Executable)
}
