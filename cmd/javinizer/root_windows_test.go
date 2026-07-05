//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCleanupStaleWindowsOldExe_RemovesStaleOld verifies that a leftover
// <exe>.old from a prior interrupted upgrade is removed on launch. Windows
// can't overwrite a running exe, so the upgrade renames it to .old and the
// detached helper deletes it after the swap; if the helper was killed before
// cleanup, the .old survives and cleanupStaleWindowsOldExe reclaims it.
func TestCleanupStaleWindowsOldExe_RemovesStaleOld(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	// Resolve symlinks the same way the function does, so the .old lands
	// exactly where it looks.
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	oldPath := resolved + ".old"
	if err := os.WriteFile(oldPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale .old: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(oldPath) })

	cleanupStaleWindowsOldExe()

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("stale .old should be removed, stat err = %v", err)
	}
}

// TestCleanupStaleWindowsOldExe_NoOpWhenAbsent verifies the function does not
// error when there is no .old to remove (the common case on a clean launch).
func TestCleanupStaleWindowsOldExe_NoOpWhenAbsent(t *testing.T) {
	// No .old created; the function must not panic or error.
	cleanupStaleWindowsOldExe()
}
