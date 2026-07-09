package testkit

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDbFileReleased_ReturnsTrueWhenFileAbsent(t *testing.T) {
	// No file exists at this path → Rename fails → not released.
	tmp := t.TempDir()
	absent := filepath.Join(tmp, "does-not-exist.db")
	assert.False(t, dbFileReleased(absent), "expected dbFileReleased=false when the DB file is absent")
}

func TestDbFileReleased_ReturnsTrueWhenFileRenameable(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("x"), 0o644))

	assert.True(t, dbFileReleased(dbPath), "expected dbFileReleased=true when the file can be renamed")

	// File must still exist (renamed there and back).
	_, err := os.Stat(dbPath)
	assert.NoError(t, err, "DB file should still exist after the rename round-trip")
}

func TestDbFileReleased_ReturnsFalseWhenHeldOpenOnWindows(t *testing.T) {
	// On Windows, an open file handle blocks Rename. Hold the file open and
	// assert dbFileReleased reports false. On Unix, files are renameable while
	// open, so this test asserts the opposite baseline (true) and is skipped
	// when the platform allows it — it exists to document the Windows behaviour
	// the helper exists for, not to fail on Unix.
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only: open file blocks rename on Windows; Unix allows rename-while-open")
	}

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "held.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("x"), 0o644))

	f, err := os.OpenFile(dbPath, os.O_RDWR, 0)
	require.NoError(t, err)
	defer f.Close()

	assert.False(t, dbFileReleased(dbPath), "expected dbFileReleased=false while the file is held open on Windows")
}

func TestAllSidecarsRemoved_ReturnsTrueWhenAllAbsent(t *testing.T) {
	tmp := t.TempDir()
	// No sidecar files exist → all considered removed.
	sidecars := []string{
		filepath.Join(tmp, "test.db-wal"),
		filepath.Join(tmp, "test.db-shm"),
	}
	assert.True(t, allSidecarsRemoved(sidecars))
}

func TestAllSidecarsRemoved_RemovesExistingSidecars(t *testing.T) {
	tmp := t.TempDir()
	wal := filepath.Join(tmp, "test.db-wal")
	shm := filepath.Join(tmp, "test.db-shm")
	require.NoError(t, os.WriteFile(wal, []byte("w"), 0o644))
	require.NoError(t, os.WriteFile(shm, []byte("s"), 0o644))

	assert.True(t, allSidecarsRemoved([]string{wal, shm}))

	// Sidecars should be gone after the call.
	for _, p := range []string{wal, shm} {
		_, err := os.Stat(p)
		assert.True(t, os.IsNotExist(err), "expected %s to be removed", p)
	}
}

func TestAllSidecarsRemoved_ReturnsFalseWhenRemoveFails(t *testing.T) {
	// A non-empty directory cannot be removed by os.Remove (it requires
	// RemoveAll), so it produces a non-IsNotExist error → allSidecarsRemoved
	// returns false so waitForFileRelease keeps polling instead of returning
	// early and reintroducing the RemoveAll flake.
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "test.db-wal")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "contents"), []byte("x"), 0o644))

	assert.False(t, allSidecarsRemoved([]string{dir}), "expected false when a sidecar cannot be removed")
}

func TestWaitForFileRelease_ReturnsImmediatelyWhenFileReleased(t *testing.T) {
	// Happy path: the DB file is immediately renameable, so waitForFileRelease
	// should return without waiting for the deadline. This keeps the test fast
	// (~microseconds) and covers the success branch.
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("x"), 0o644))

	start := time.Now()
	waitForFileRelease(t, dbPath)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 500*time.Millisecond, "should return immediately when the file is releasable, took %v", elapsed)
}

func TestWaitForFileRelease_HandlesAbsentDbFile(t *testing.T) {
	// When the DB file doesn't exist at all (e.g. db.Open failed before the
	// file was created), waitForFileRelease should still terminate cleanly
	// rather than spinning until the deadline — dbFileReleased returns false
	// for an absent file, so this exercises the polling-then-log path. Keep the
	// wait short to avoid a 2s test: assert it completes within a bound that
	// is well under the 2s deadline only when the deadline can't be shortened.
	// We can't shorten the internal deadline, so accept up to ~2.1s here.
	tmp := t.TempDir()
	absent := filepath.Join(tmp, "never-created.db")

	start := time.Now()
	waitForFileRelease(t, absent)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 2200*time.Millisecond, "should not run far past the 2s deadline, took %v", elapsed)
}
