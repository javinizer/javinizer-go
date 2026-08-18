package downloader

// POSTER-WRITE-HARDENING codex PR#215 w12 (P1): the wave-7 backup claim
// reserves its name with a 0-byte O_EXCL placeholder, so the dest→backup
// set-aside renames INTO an occupied path. Windows OsFs rename (MoveFileW)
// refuses an existing destination, which failed EVERY ledger-armed overwrite
// of an existing file on Windows. moveIntoReservedBackup now routes that leg
// through the platform-aware replacement primitive (fsutil.ReplaceFile —
// MoveFileExW with MOVEFILE_REPLACE_EXISTING for OsFs) behind the
// fsutil.PathBackslashesAreSeparators Windows-posture seam (the same seam
// style as keyed_lock_p3 and history's restoreOSPath), so both legs run in
// host tests. Rollback renames intentionally stay plain renames: their target
// is the just-vacated destination, absent by construction, and keeping
// fail-closed Windows behavior there is the safer posture.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// Host end-to-end: an armed overwrite of an existing file claims and reserves
// the backup name, and the set-aside REPLACES that reservation with the real
// destination bytes — never leaving a 0-byte placeholder journaled as the
// backup.
func TestInstallOverwritingW12_ArmedOverwriteReplacesReservedBackup(t *testing.T) {
	resetBackupOrdinalW22(t)
	base, dest, staged, recorder := w7rSetupFixture(t, "/out/W12-REPLACE")
	first := backupCandidateW22(dest, "w12-replace", 1)

	d := NewDownloader(nil, base, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w12-replace", recorder: recorder})
	require.NoError(t, err)
	require.False(t, skipped)
	require.True(t, replaced)

	require.Equal(t, "new", string(mustReadDownloaderW7(t, base, dest)), "staged bytes installed")
	require.Len(t, recorder.get(), 1, "the replace journaled exactly once")
	require.Equal(t, first, recorder.get()[0].backupPath, "the reserved first candidate is the journaled backup")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, first)),
		"the set-aside handoff replaced the 0-byte reservation with the pre-existing destination bytes")
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist, "the busy marker is released")
}

// Seam-leg unit coverage for the handoff itself: with EITHER platform leg
// selected, a 0-byte reservation at the backup name is replaced by the
// destination bytes. On a POSIX host fsutil.ReplaceFile is itself a rename,
// so both legs land behaviorally identical here; the Windows CI run exercises
// this same dispatch against the MemMapFs rename, and fsutil's own
// TestReplaceFileWindows_AtomicReplace covers the native OsFs MoveFileEx leg.
func TestMoveIntoReservedBackupW12_BothLegsReplaceTheReservation(t *testing.T) {
	previous := fsutil.PathBackslashesAreSeparators
	t.Cleanup(func() { fsutil.PathBackslashesAreSeparators = previous })

	for _, windowsPosture := range []bool{false, true} {
		fsutil.PathBackslashesAreSeparators = windowsPosture
		fs := afero.NewMemMapFs()
		src := "/out/W12-LEG/poster.jpg"
		dst := backupCandidateW22(src, "w12-leg", 1)
		require.NoError(t, fs.MkdirAll(filepath.Dir(src), 0o755))
		require.NoError(t, afero.WriteFile(fs, src, []byte("set-aside-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(fs, dst, nil, 0o600), "the claim's 0-byte reservation")

		require.NoError(t, moveIntoReservedBackup(fs, src, dst), "windowsPosture=%v", windowsPosture)
		require.Equal(t, "set-aside-bytes", string(mustReadDownloaderW7(t, fs, dst)),
			"the reservation placeholder is replaced (windowsPosture=%v)", windowsPosture)
		_, statErr := fs.Stat(src)
		require.ErrorIs(t, statErr, os.ErrNotExist, "the source moved (windowsPosture=%v)", windowsPosture)
	}
}

// The armed-overwrite path THROUGH the ReplaceFile leg: under the Windows
// posture seam the set-aside dispatches fsutil.ReplaceFile, and the full
// claim → handoff → journal → install flow must succeed with the reservation
// in place. This is the regression shape from the finding: before the fix
// this leg failed on Windows (MoveFileW over an occupied reservation).
func TestInstallOverwritingW12_WindowsPostureReplaceLegSucceedsWithReservation(t *testing.T) {
	previous := fsutil.PathBackslashesAreSeparators
	fsutil.PathBackslashesAreSeparators = true
	t.Cleanup(func() { fsutil.PathBackslashesAreSeparators = previous })

	resetBackupOrdinalW22(t)
	base, dest, staged, recorder := w7rSetupFixture(t, "/out/W12-WINPOS")
	first := backupCandidateW22(dest, "w12-winpos", 1)

	d := NewDownloader(nil, base, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w12-winpos", recorder: recorder})
	require.NoError(t, err, "the ReplaceFile-dispatching leg must replace the occupied reservation")
	require.False(t, skipped)
	require.True(t, replaced)

	require.Equal(t, "new", string(mustReadDownloaderW7(t, base, dest)))
	require.Len(t, recorder.get(), 1, "journal armed")
	require.Equal(t, first, recorder.get()[0].backupPath)
	require.Equal(t, "current", string(mustReadDownloaderW7(t, base, first)),
		"the reservation was replaced by the set-aside bytes, not left as a placeholder")
	_, markerErr := base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist)
}
