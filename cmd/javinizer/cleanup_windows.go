//go:build windows

package main

import (
	"os"
	"path/filepath"
)

// cleanupStaleWindowsOldExe removes a leftover <exe>.old from a prior
// interrupted desktop self-upgrade. Windows can't overwrite a running exe, so
// the upgrade renames it to .old and the detached helper deletes it after the
// swap; if the helper was killed before cleanup, the .old survives and is
// cleared here on the next launch.
func cleanupStaleWindowsOldExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		_ = os.Remove(resolved + ".old")
	} else {
		_ = os.Remove(exe + ".old")
	}
}
