package downloader

// POSTER-WRITE-HARDENING codex PR#215 w12 (P1): the wave-7 backup claim
// reserves its name with a 0-byte O_EXCL placeholder, so the dest→backup
// set-aside must either REPLACE that placeholder or (wave-38, finding F2)
// take it aside and move dest onto the freed name NO-REPLACE. Windows OsFs
// rename (MoveFileW) refuses an existing destination, which failed EVERY
// ledger-armed overwrite of an existing file on Windows pre-w12; the w12 fix
// routed the set-aside through the platform-aware replacement primitive, and
// wave-38's conditional handoff keeps the full claim → handoff → journal →
// install flow succeeding with the reservation in place on every platform.
// Rollback renames intentionally stay plain renames: their target is the
// just-vacated destination, absent by construction, and keeping fail-closed
// Windows behavior there is the safer posture.

import (
	"context"
	"os"
	"testing"

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
