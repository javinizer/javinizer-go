package history

// POSTER-WRITE-HARDENING wave-31 (codex local round 1, PR#215 finding L1) —
// the restore legs revalidate that the destination still names the object
// they just PUBLISHED before removing the backup or consuming the journal
// entry. A foreign writer swapping or deleting the destination inside the
// publish→remove window used to get the last recoverable copy of the
// pre-replacement bytes unlinked and the restore record consumed. Both
// funnels (reverter restore, sweeper crash-window restore-and-consume) now
// refuse into their conservative postures: backup retained, entry left
// ARMED (never marked restore-pending — that marker certifies destination
// bytes that are no longer provably in place), destination untouched.
//
// Test matrix:
//   - destStillNamesRestoredObject unit legs (unknown-identity skip,
//     absent/indeterminate/symlink/non-regular occupant, dev/inode +
//     metadata mismatches, match) — the real-OsFs detection legs ride an
//     actual copyRestoreBytesIdentity publish;
//   - reverter end-to-end refusal through the restoredDestStillOurs seam
//     replay (the publish→recheck instant is unreachable for a Filesystem
//     double on the real OsFs — the wave-30 gate requires the native
//     descriptor), including the armed retry healing on the next run;
//   - sweeper end-to-end refusal through the same seam.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w31SwapRestoredDestSeam replays the foreign writer landing inside the
// publish→recheck window: dest no longer names the published restore object.
func w31SwapRestoredDestSeam(t *testing.T, answer bool) {
	t.Helper()
	prev := restoredDestStillOurs
	restoredDestStillOurs = func(afero.Fs, string, restoredDestIdentity) bool { return answer }
	t.Cleanup(func() { restoredDestStillOurs = prev })
}

func TestRestoredDestIdentityW31_UnitLegs(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/w31", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/w31/poster.jpg", []byte("restored bytes"), 0o644))
	info, err := lstatRestoreSource(fs, "/w31/poster.jpg")
	require.NoError(t, err)
	id := restoredDestIdentityFrom(info)

	require.False(t, restoredDestIdentityFrom(nil).known, "a nil publish answer carries no provable identity")
	require.True(t, id.known)
	require.True(t, destStillNamesRestoredObject(fs, "/w31/poster.jpg", restoredDestIdentity{}),
		"an unknown identity skips the check (documented virtual-leg residual)")
	require.True(t, destStillNamesRestoredObject(fs, "/w31/poster.jpg", id),
		"the untouched destination still names the published object")

	// Swap semantics: a fresh object under the destination name (remove +
	// recreate) — the foreign-substitution shape this gate exists for.
	require.NoError(t, fs.Remove("/w31/poster.jpg"))
	require.NoError(t, afero.WriteFile(fs, "/w31/poster.jpg", []byte("restored bytes LONGER foreign plant"), 0o644))
	require.False(t, destStillNamesRestoredObject(fs, "/w31/poster.jpg", id),
		"a swapped (resized) destination object never matches")

	require.NoError(t, fs.Remove("/w31/poster.jpg"))
	require.False(t, destStillNamesRestoredObject(fs, "/w31/poster.jpg", id), "a deleted destination never matches")

	require.NoError(t, fs.Mkdir("/w31/poster.jpg", 0o755))
	require.False(t, destStillNamesRestoredObject(fs, "/w31/poster.jpg", id),
		"a directory occupant never names the published object")
}

// Indeterminate and nil stat answers both fail closed.
func TestRestoredDestIdentityW31_StatLegs(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/w31b/poster.jpg", []byte("bytes"), 0o644))
	info, err := lstatRestoreSource(base, "/w31b/poster.jpg")
	require.NoError(t, err)
	id := restoredDestIdentityFrom(info)

	require.False(t, destStillNamesRestoredObject(&w31StatFailFs{Fs: base, err: errors.New("w31 stat wedged")}, "/w31b/poster.jpg", id),
		"an indeterminate destination answer is never 'still ours'")
	require.False(t, destStillNamesRestoredObject(&w31NilStatFs{Fs: base}, "/w31b/poster.jpg", id),
		"a nil stat answer is never 'still ours'")
}

type w31StatFailFs struct {
	afero.Fs
	err error
}

func (f *w31StatFailFs) Stat(string) (os.FileInfo, error) { return nil, f.err }

type w31NilStatFs struct{ afero.Fs }

func (f *w31NilStatFs) Stat(string) (os.FileInfo, error) { return nil, nil }

// Real-OsFs legs riding an actual publish: the returned identity pins the
// landed destination; the dev/inode leg catches a same-size, re-mtimed
// foreign inode renamed over the destination (the CI-stable substitution
// shape — a pre-created file renamed over dest instead of remove+create).
func TestRestoredDestIdentityW31_OSIdentityLegs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inode identity assertions are POSIX-shaped; Windows coverage runs through the size/mtime legs")
	}
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, os.WriteFile(backup, []byte("original poster bytes"), 0o644))
	require.NoError(t, os.Chtimes(backup, fixedW31Time(), fixedW31Time()))

	id, err := copyRestoreBytesIdentity(fs, backup, dest)
	require.NoError(t, err)
	require.True(t, id.known, "the OsFs publish reports a proven destination identity")
	require.True(t, restoredDestStillOurs(fs, dest, id), "the untouched destination verifies")

	// Foreign swap: a pre-created distinct inode of the SAME byte length,
	// re-mtimed to match — only dev/inode can tell it apart (remove+create
	// at one path routinely reuses the freed inode on CI filesystems).
	foreign := filepath.Join(dir, "foreign-plant.jpg")
	require.NoError(t, os.WriteFile(foreign, []byte("foreign poster bytes!!"), 0o644))
	require.NoError(t, os.Rename(foreign, dest))
	destInfo, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	require.NoError(t, os.Chtimes(dest, destInfo.ModTime(), id.info.ModTime()))
	require.False(t, restoredDestStillOurs(fs, dest, id),
		"a same-size re-mtimed foreign inode mismatches via dev/inode")

	// Absent destination.
	id2, err := copyRestoreBytesIdentity(fs, backup, dest)
	require.NoError(t, err)
	require.NoError(t, os.Remove(dest))
	require.False(t, restoredDestStillOurs(fs, dest, id2), "a vanished destination never matches")

	// Symlink occupant: the no-follow classifier refuses it outright.
	id3, err := copyRestoreBytesIdentity(fs, backup, dest)
	require.NoError(t, err)
	require.NoError(t, os.Remove(dest))
	require.NoError(t, os.Symlink(filepath.Join(dir, "nowhere"), dest))
	require.False(t, restoredDestStillOurs(fs, dest, id3), "a symlink occupant never matches")
}

func fixedW31Time() (t time.Time) {
	return time.Date(2021, 2, 3, 4, 5, 6, 0, time.UTC)
}

// The reverter's armed restore leg: a destination that no longer names the
// just-published restore object refuses the backup removal AND the journal
// consumption — backup retained, entry left armed (NOT marked pending),
// destination untouched. The next explicit retry heals from the armed state.
func TestRestoreReplacementJournalW31_DestSwapRefusesRemovalAndConsumption(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25ArmedOp(t, fs, repo, "W31A", []byte("new poster"), []byte("original poster"), "stamped")

	w31SwapRestoredDestSeam(t, false)

	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no longer names the restored object")
	require.True(t, restored[dest], "the restored path stays protected from the delete-list cleanup")

	require.Equal(t, "original poster", string(mustRead2(t, fs, backup)),
		"the backup — the only remaining copy of the pre-replacement bytes — is retained")
	require.Equal(t, "original poster", string(mustRead2(t, fs, dest)), "the published restore is never removed")

	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1, "the journal entry was not consumed")
	require.False(t, entries[0].RestorePending,
		"no restore-pending marker: that marker certifies destination bytes that are unproven now")

	// The armed posture is retryable: the next run restores and consumes.
	restoredDestStillOurs = destStillNamesRestoredObject
	restored2, err2 := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err2)
	require.True(t, restored2[dest])
	exists, _ := afero.Exists(fs, backup)
	require.False(t, exists, "the retry consumed the backup with the entry")
	require.Empty(t, w25JournalEntries(t, repo, op.ID))
}

// The sweeper's crash-window restore-and-consume takes the same refusal:
// backup retained, destination untouched, entry left armed — and the
// in-process/durable restore-pending markers stay clear, so a later sweep
// arbitrates the armed entry instead of consuming it on a false
// certification.
func TestSweepW31_DestSwapRefusesRemovalAndConsumption(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25SweepCrashOp(t, fs, repo, "W31B", []byte("original bytes"), "stamped")

	w31SwapRestoredDestSeam(t, false)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, healed, "the refusal heals nothing")

	require.Equal(t, "original bytes", string(mustRead2(t, fs, backup)), "the backup is retained")
	require.Equal(t, "original bytes", string(mustRead2(t, fs, dest)),
		"the published restore stays (nothing deletes the destination either)")

	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1, "the journal entry was not consumed")
	require.False(t, entries[0].RestorePending, "no durable restore-pending marker was persisted")

	// A follow-up sweep with the destination PRESENT and the entry merely
	// armed keeps everything retained (never restores over a present
	// destination, never consumes): the conservative steady state.
	restoredDestStillOurs = destStillNamesRestoredObject
	healed2, err2 := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err2)
	require.Equal(t, 0, healed2)
	require.Equal(t, "original bytes", string(mustRead2(t, fs, backup)))
	require.Len(t, w25JournalEntries(t, repo, op.ID), 1)
}
