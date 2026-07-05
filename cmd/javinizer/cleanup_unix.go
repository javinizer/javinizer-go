//go:build !windows

package main

// cleanupStaleWindowsOldExe is a no-op on non-Windows: only Windows can't
// overwrite a running exe, so the .old cleanup is Windows-specific. The real
// implementation lives in cleanup_windows.go.
func cleanupStaleWindowsOldExe() {}
