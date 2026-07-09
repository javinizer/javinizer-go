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
	// on the parent for rename. Some platforms (root, or filesystems that
	// ignore directory mode) allow the rename anyway — skip then so the test
	// stays portable (os.Geteuid is Unix-only and doesn't compile on Windows).
	tmp := t.TempDir()
	roDir := filepath.Join(tmp, "ro")
	require.NoError(t, os.Mkdir(roDir, 0o755)) // writable first so we can place the file
	dbPath := filepath.Join(roDir, "test.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("x"), 0o644))
	require.NoError(t, os.Chmod(roDir, 0o555)) // now read-only: rename cannot create the probe
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })

	// If the rename succeeds anyway (root / permissive fs), the branch this
	// test targets is unreachable here — skip rather than assert a false
	// negative. dbFileReleased is still exercised on the happy path elsewhere.
	probe := dbPath + ".release-probe"
	if err := os.Rename(dbPath, probe); err == nil {
		require.NoError(t, os.Rename(probe, dbPath)) // undo
		t.Skip("rename succeeds under read-only parent on this platform/root; rename-fail branch not reachable here")
	}

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

func TestDbFileReleased_CleansUpStrandedProbeFromPriorRestoreFailure(t *testing.T) {
	// If a prior dbFileReleased call's restore failed AND os.Remove(probe)
	// also failed, the probe file is stranded. The next poll must clean it up
	// before the absent-DB fast path returns true — otherwise waitForFileRelease
	// could return while a locked probe lingers and trip the RemoveAll this
	// helper exists to protect. Cover the stranded-probe cleanup branch.
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	probe := dbPath + ".release-probe"
	require.NoError(t, os.WriteFile(probe, []byte("stranded"), 0o644))
	// dbPath itself is absent; the stranded probe must be removed first.

	assert.True(t, dbFileReleased(dbPath), "expected true once the stranded probe is cleaned up and the DB is absent")
	assert.NoFileExists(t, probe, "stranded probe should be removed")
}

func TestDbFileReleased_ReturnsFalseWhenStrandedProbeCannotBeRemoved(t *testing.T) {
	// If the stranded probe cannot be removed (e.g. it's a non-empty dir),
	// dbFileReleased must report not-released so the caller keeps polling
	// rather than returning true while the probe lingers.
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	probe := dbPath + ".release-probe"
	require.NoError(t, os.Mkdir(probe, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(probe, "contents"), []byte("x"), 0o644))
	t.Cleanup(func() { _ = os.RemoveAll(probe) })

	assert.False(t, dbFileReleased(dbPath), "expected false when the stranded probe cannot be removed")
}

func TestDbFileReleased_ReturnsFalseWhenProbeStatErrors(t *testing.T) {
	// If os.Stat(probe) returns a non-IsNotExist error (e.g. EACCES when the
	// parent dir lacks execute permission), dbFileReleased must report
	// not-released so polling continues rather than falling through to the
	// absent-DB fast path and returning true while the probe may linger.
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	probe := dbPath + ".release-probe"
	require.NoError(t, os.WriteFile(probe, []byte("x"), 0o644))
	// Make the parent dir non-executable so stat of the probe returns EACCES.
	require.NoError(t, os.Chmod(tmp, 0o600))
	t.Cleanup(func() { _ = os.Chmod(tmp, 0o755) })

	// If the platform (e.g. root) still allows stat despite the missing x bit,
	// the branch this test targets is unreachable — skip rather than fail.
	if _, err := os.Stat(probe); err == nil {
		t.Skip("stat succeeds despite non-executable parent on this platform/root")
	}

	assert.False(t, dbFileReleased(dbPath), "expected false when the probe stat returns a non-IsNotExist error")
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

func TestWaitForFileRelease_PollsWhenLockedThenReleases(t *testing.T) {
	// Cover the polling loop body (sleep, remaining check, the cap branch)
	// without waiting the real 2s: shorten the deadline and make dbFileReleased
	// report not-released for the first two polls via the rename seam, then
	// release. This exercises the sleep/remaining/poll paths deterministically.
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("x"), 0o644))

	origDeadline := waitForFileReleaseDeadline
	origPoll := waitForFileReleasePollInterval
	origRename := renameFunc
	t.Cleanup(func() {
		waitForFileReleaseDeadline = origDeadline
		waitForFileReleasePollInterval = origPoll
		renameFunc = origRename
	})
	waitForFileReleaseDeadline = 100 * time.Millisecond
	waitForFileReleasePollInterval = 5 * time.Millisecond

	calls := 0
	renameFunc = func(oldpath, newpath string) error {
		calls++
		if calls <= 2 {
			return os.ErrPermission // locked for the first two polls
		}
		return origRename(oldpath, newpath) // release on the third
	}

	start := time.Now()
	waitForFileRelease(t, dbPath)
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, calls, 3, "should have polled at least 3 times (2 locked + 1 released)")
	assert.Less(t, elapsed, 500*time.Millisecond, "should return shortly after release, took %v", elapsed)
}

func TestWaitForFileRelease_LogsOnTimeoutWhenNeverReleased(t *testing.T) {
	// Cover the timeout/log path: dbFileReleased never returns true, so the
	// loop runs until the deadline and the final t.Logf fires. Shorten the
	// deadline so the test stays fast.
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("x"), 0o644))

	origDeadline := waitForFileReleaseDeadline
	origPoll := waitForFileReleasePollInterval
	origRename := renameFunc
	t.Cleanup(func() {
		waitForFileReleaseDeadline = origDeadline
		waitForFileReleasePollInterval = origPoll
		renameFunc = origRename
	})
	waitForFileReleaseDeadline = 30 * time.Millisecond
	waitForFileReleasePollInterval = 50 * time.Millisecond // > deadline so the cap branch (sleep > remaining) is hit on entry

	renameFunc = func(string, string) error { return os.ErrPermission } // always locked

	start := time.Now()
	waitForFileRelease(t, dbPath)
	elapsed := time.Since(start)

	// Should have polled past the deadline (covers the sleep + remaining cap
	// branches) and then logged. Allow a small buffer over the deadline.
	assert.GreaterOrEqual(t, elapsed, 30*time.Millisecond, "should have polled until the deadline, took %v", elapsed)
	assert.Less(t, elapsed, 500*time.Millisecond, "should not run far past the deadline, took %v", elapsed)
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
