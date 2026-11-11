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

	assertCleanupAfterExecutableRename(t, exe, resolved)
}

// An updater can rename a running Windows executable while GetModuleFileName
// still returns its original pathname. Exercise the real missing-path fallback
// without replacing os.Executable or filesystem calls. This is deliberately
// sequential: the generated test executable is restored before other tests run.
func assertCleanupAfterExecutableRename(t *testing.T, exe, resolved string) {
	t.Helper()
	parked, err := os.CreateTemp(filepath.Dir(resolved), ".cleanup-running-*.exe")
	if err != nil {
		t.Fatalf("reserve parked executable name: %v", err)
	}
	parkedPath := parked.Name()
	if err := parked.Close(); err != nil {
		t.Fatalf("close parked executable reservation: %v", err)
	}
	if err := os.Remove(parkedPath); err != nil {
		t.Fatalf("release parked executable reservation: %v", err)
	}
	if err := os.Rename(resolved, parkedPath); err != nil {
		t.Fatalf("rename running test executable: %v", err)
	}
	restored := false
	t.Cleanup(func() {
		if !restored {
			if err := os.Rename(parkedPath, resolved); err != nil {
				t.Errorf("restore running test executable: %v", err)
			}
		}
	})
	if current, err := os.Executable(); err != nil || current != exe {
		t.Fatalf("startup executable pathname changed: got %q, want %q, err = %v", current, exe, err)
	}
	if _, err := filepath.EvalSymlinks(exe); !os.IsNotExist(err) {
		t.Fatalf("renamed executable pathname should be absent, err = %v", err)
	}
	fallbackOldPath := exe + ".old"
	stale, err := os.OpenFile(fallbackOldPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create fallback .old sidecar: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(fallbackOldPath) })
	if _, err := stale.WriteString("stale after concurrent executable rename"); err != nil {
		_ = stale.Close()
		t.Fatalf("write fallback .old sidecar: %v", err)
	}
	if err := stale.Close(); err != nil {
		t.Fatalf("close fallback .old sidecar: %v", err)
	}

	cleanupStaleWindowsOldExe()

	if _, err := os.Stat(fallbackOldPath); !os.IsNotExist(err) {
		t.Errorf("fallback .old should be removed, stat err = %v", err)
	}
	if info, err := os.Stat(parkedPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("parked executable must remain intact, stat err = %v", err)
	}
	if err := os.Rename(parkedPath, resolved); err != nil {
		t.Fatalf("restore running test executable: %v", err)
	}
	restored = true
	if _, err := filepath.EvalSymlinks(exe); err != nil {
		t.Fatalf("restored executable pathname must resolve: %v", err)
	}
}

// TestCleanupStaleWindowsOldExe_NoOpWhenAbsent verifies the function does not
// error when there is no .old to remove (the common case on a clean launch).
func TestCleanupStaleWindowsOldExe_NoOpWhenAbsent(t *testing.T) {
	// No .old created; the function must not panic or error.
	cleanupStaleWindowsOldExe()
}
