package history

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING wave-11: two codex findings around paths the sweep
// and the re-arm hand to OS calls.
//
// (A — Windows CI follow-up) slash-normalized journal/sweep spellings must
// reach OS metadata calls in native separator form: afero's MemMapFs indexes
// filepath.Clean'd names while its Chmod performs a RAW lookup, so the
// re-armed backup "missed" itself on the Windows runner
// ("chmod ...: file does not exist", w10 CI). restoreOSPath is the single
// conversion seam (Destination-key posture, fsutil.PathBackslashesAreSeparators);
// rearmReplacementBackup and copyRestoreBytes route their chmod/chtimes/
// ReplaceFile arguments through it.
//
// (B — codex P2, replacement_sweep_p3.go:505) destination classification runs
// Lstat-FIRST in both sweep classifiers (restoreAndConsume and the
// unjournaled-orphan leg): Stat reads a DANGLING SYMLINK as ENOENT, and the
// absent-dest restore would then copyRestoreBytes over the link object
// itself, destroying an unjournaled directory entry. Only a genuine
// Lstat-ENOENT may flow into the absent-dest restore.

// TestW11RestoreOSPath_SeamLegs pins the single separator seam: under the
// Windows posture the conversion is filepath.FromSlash's expansion (decidable
// on any host — FromSlash is a compile-time no-op on POSIX builds); under the
// POSIX posture the helper is byte-exact so literal-backslash filenames are
// never rewritten. Seam flip mirrors the destKey style
// (fsutil/keyed_lock_p3_test.go).
func TestW11RestoreOSPath_SeamLegs(t *testing.T) {
	previous := fsutil.PathBackslashesAreSeparators
	t.Cleanup(func() { fsutil.PathBackslashesAreSeparators = previous })

	// Windows posture: slash-normalized journal spellings land on OS calls in
	// native form ("D:\out\W9-RETRYMAL\..." style as seen in the CI logs).
	fsutil.PathBackslashesAreSeparators = true
	require.Equal(t, `D:\pool\W11\dest.jpg.dlbak.0123456789abcdef`,
		restoreOSPath("D:/pool/W11/dest.jpg.dlbak.0123456789abcdef"))
	require.Equal(t, `D:\pool\W11\dest.jpg`, restoreOSPath("D:/pool/W11/dest.jpg"))
	require.Equal(t, `D:\already\native\dest.jpg`, restoreOSPath(`D:\already\native\dest.jpg`),
		"already-native spellings are idempotent")
	require.Equal(t, `\\pool\share\W11\dest.jpg`, restoreOSPath("//pool/share/W11/dest.jpg"),
		"UNC slash spellings convert to the native UNC form")

	// POSIX posture (the production default on POSIX): byte-exact — a
	// backslash is a filename character, never a separator.
	fsutil.PathBackslashesAreSeparators = false
	require.Equal(t, "/out/W11/dest.jpg.dlbak.0123456789abcdef",
		restoreOSPath("/out/W11/dest.jpg.dlbak.0123456789abcdef"))
	require.Equal(t, `/out/W11/lite\ral.jpg`, restoreOSPath(`/out/W11/lite\ral.jpg`),
		"POSIX literal-backslash names are never rewritten")
}

// TestW11SweepDanglingSymlinkDest_RetainsBackupAndJournal is finding (B-1):
// a journaled (armed) backup whose destination is a DANGLING symlink must be
// classified dest-PRESENT: the sweep leaves the link object AND the backup in
// place (no restore, no consumption, no re-arm).
func TestW11SweepDanglingSymlinkDest_RetainsBackupAndJournal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privilege on Windows")
	}
	root := t.TempDir()
	fs := afero.NewOsFs()
	repo := newP3OpRepo()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA

	target := filepath.Join(root, "purged-target.bin") // never created — the link dangles
	require.NoError(t, os.Symlink(target, dest))
	require.NoError(t, os.WriteFile(backup, []byte("pre-crash-old-bytes"), 0o640))
	op := journalRow(t, repo, "job-1", "W11-DANGLING", dest, backup, 1, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, healed, "a dangling symlink is a PRESENT directory entry, never the absent-dest restore")

	linkInfo, lerr := os.Lstat(dest)
	require.NoError(t, lerr, "the link object itself must survive the sweep")
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink, "dest stays the symlink object — restore never renamed over it")
	gotTarget, rlErr := os.Readlink(dest)
	require.NoError(t, rlErr)
	require.Equal(t, target, gotTarget)
	_, statErr := os.Stat(dest)
	require.ErrorIs(t, statErr, os.ErrNotExist, "nothing materialized behind the link")
	require.Equal(t, "pre-crash-old-bytes", string(mustRead2(t, fs, backup)),
		"the journaled backup was neither consumed nor overwritten")

	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "journal entry stays armed — no consumption on a present dest")
}

// TestW11SweepSymlinkDestToExistingFile_RetainsBackupAndJournal is finding
// (B-2): a symlink whose target EXISTS gets the same conservative present-dest
// outcome as a regular destination (armed entry retained; nothing restored).
func TestW11SweepSymlinkDestToExistingFile_RetainsBackupAndJournal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privilege on Windows")
	}
	root := t.TempDir()
	fs := afero.NewOsFs()
	repo := newP3OpRepo()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA
	victim := filepath.Join(root, "victim.bin")

	require.NoError(t, os.WriteFile(victim, []byte("user-bytes"), 0o600))
	require.NoError(t, os.Symlink(victim, dest))
	require.NoError(t, os.WriteFile(backup, []byte("pre-crash-old-bytes"), 0o640))
	op := journalRow(t, repo, "job-1", "W11-LINKPRESENT", dest, backup, 1, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, healed, "present-dest posture: armed entries are retained, never auto-restored")

	linkInfo, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink, "the link object is untouched")
	require.Equal(t, "user-bytes", string(mustRead2(t, fs, victim)), "the link target is untouched")
	require.Equal(t, "pre-crash-old-bytes", string(mustRead2(t, fs, backup)), "backup retained")

	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "journal entry stays armed")
}

// TestW11SweepAbsentDestOsFs_RestoresAndConsumes is finding (B-3): a genuine
// Lstat-ENOENT destination keeps the pre-fix crash-window behavior on a real
// filesystem — the journaled backup restores and the entry is consumed.
func TestW11SweepAbsentDestOsFs_RestoresAndConsumes(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewOsFs()
	repo := newP3OpRepo()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA

	require.NoError(t, os.WriteFile(backup, []byte("pre-crash-old-bytes"), 0o640))
	op := journalRow(t, repo, "job-1", "W11-ABSENT", dest, backup, 1, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, healed, "genuinely absent destinations still restore from their journaled backup")

	destInfo, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	require.True(t, destInfo.Mode().IsRegular(), "the restore landed a REGULAR file (never a link object)")
	require.Equal(t, "pre-crash-old-bytes", string(mustRead2(t, fs, dest)), "old bytes restored to the destination")
	_, lerr = os.Lstat(backup)
	require.ErrorIs(t, lerr, os.ErrNotExist, "consumed backup removed after the restore")

	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements, "journal entry consumed")
}

// TestW11SweepUnjournaledOrphan_DanglingSymlinkDestRetained is finding (B-4):
// the unjournaled-orphan leg (sweepOne) applies the same Lstat classification —
// a marker-shaped file journaled NOWHERE next to a dangling-symlink destination
// is retained for inspection, and the link object is not consumed by a restore.
func TestW11SweepUnjournaledOrphan_DanglingSymlinkDestRetained(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privilege on Windows")
	}
	root := t.TempDir()
	fs := afero.NewOsFs()
	repo := newP3OpRepo()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA

	target := filepath.Join(root, "gone.bin") // never created — the link dangles
	require.NoError(t, os.Symlink(target, dest))
	require.NoError(t, os.WriteFile(backup, []byte("last-copy"), 0o640))
	// A journaled destination in the same directory puts the dir in sweep
	// scope; ITS backup stays off-disk so only the orphan is arbitrated.
	other := filepath.Join(root, "other.jpg")
	journalRow(t, repo, "job-1", "W11-ORPHAN-SIBLING", other, other+".dlbak."+p3HexC, 1, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, healed,
		"orphan leg: the dangling symlink is a present destination — entry retained, never restored over")

	linkInfo, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink,
		"the link object survives — copyRestoreBytes never replaced an unjournaled directory entry")
	_, statErr := os.Stat(dest)
	require.ErrorIs(t, statErr, os.ErrNotExist, "no restore materialized anything behind the link")
	require.Equal(t, "last-copy", string(mustRead2(t, fs, backup)), "orphan marker file retained for inspection")
}

// TestW11SweepUnjournaledOrphan_AbsentDestOsFs_Restores controls B-4's other
// side on a real filesystem: with a genuinely absent destination the orphan
// leg's prior behavior is unchanged — restore, retain the source, report the
// heal.
func TestW11SweepUnjournaledOrphan_AbsentDestOsFs_Restores(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewOsFs()
	repo := newP3OpRepo()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA

	require.NoError(t, os.WriteFile(backup, []byte("last-copy"), 0o640))
	other := filepath.Join(root, "other.jpg")
	journalRow(t, repo, "job-1", "W11-ORPHAN-SIBLING", other, other+".dlbak."+p3HexC, 1, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, healed, "truly absent destination: the orphan stays the last-copy restore of record")
	require.Equal(t, "last-copy", string(mustRead2(t, fs, dest)), "orphan backup restored onto the destination")
	require.Equal(t, "last-copy", string(mustRead2(t, fs, backup)),
		"marker shape is not ownership proof — the source is retained for inspection")
}
