package testkit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDbFileReleased_ReturnsTrueWhenFileAbsent(t *testing.T) {
	// No file exists at this path → there is nothing to unlock, so
	// dbFileReleased reports released so waitForFileRelease can return
	// immediately instead of spinning until the deadline.
	tmp := t.TempDir()
	absent := filepath.Join(tmp, "does-not-exist.db")
	assert.True(t, dbFileReleased(absent), "expected dbFileReleased=true when the DB file is absent")
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

func TestDbFileReleased_ReturnsFalseWhenRenameFails(t *testing.T) {
	// A read-only parent directory blocks the rename of dbPath to the probe,
	// so dbFileReleased reports not-released (the caller keeps polling). This
	// covers the rename-failure branch on platforms that enforce write perms
	// on the parent for rename. Skip where the OS/root allows it regardless.
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write-permission checks")
	}
	tmp := t.TempDir()
	roDir := filepath.Join(tmp, "ro")
	require.NoError(t, os.Mkdir(roDir, 0o755)) // writable first so we can place the file
	dbPath := filepath.Join(roDir, "test.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("x"), 0o644))
	require.NoError(t, os.Chmod(roDir, 0o555)) // now read-only: rename cannot create the probe
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })

	assert.False(t, dbFileReleased(dbPath), "expected false when rename is blocked by a read-only parent")
}

func TestDbFileReleased_ReturnsFalseWhenRestoreFails(t *testing.T) {
	// The rename-back (restore) inside dbFileReleased is impossible to fail
	// deterministically in a single-threaded test without a seam: both renames
	// happen inside the function and nothing can obstruct the restore between
	// them. Inject a renameFunc whose second call (the restore) fails, so the
	// restore-failure branch — clean up the stranded probe + report not-released
	// — is exercised rather than left as an uncovered defensive guard.
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("x"), 0o644))

	origRename := renameFunc
	t.Cleanup(func() { renameFunc = origRename })
	call := 0
	renameFunc = func(oldpath, newpath string) error {
		call++
		if call == 1 {
			return origRename(oldpath, newpath) // probe rename succeeds
		}
		return os.ErrPermission // restore fails
	}

	assert.False(t, dbFileReleased(dbPath), "expected false when the rename-back restore fails")
	assert.NoFileExists(t, dbPath+".release-probe", "stranded probe should be cleaned up after restore failure")
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
	// file was created), dbFileReleased reports released (nothing to unlock),
	// so waitForFileRelease should return immediately rather than spinning to
	// the 2s deadline.
	tmp := t.TempDir()
	absent := filepath.Join(tmp, "never-created.db")

	start := time.Now()
	waitForFileRelease(t, absent)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 500*time.Millisecond, "should return immediately when the DB file is absent, took %v", elapsed)
}
