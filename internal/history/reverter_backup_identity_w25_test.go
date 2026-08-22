package history

// POSTER-WRITE-HARDENING codex PR#215 wave-25 (finding 2) —
// removeReplacementBackup used to unlink the journaled backup by PATHNAME
// alone: a directory writer renaming our set-aside away and planting a
// foreign file at the same name had it deleted AND the journal record
// consumed — foreign bytes destroyed, restore history erased. The gate now
// binds the removal to the OWNED object: the downloader-stamped arm-time
// facts (BackupSize/BackupModUnix) and, when a restore just streamed the
// backup, the identity of the object actually read (dev/inode + size +
// mtime). Mismatch → refuse unlink, entry retained live, warn-only. Legacy
// entries without stamped facts keep the pathname posture.
//
// Test matrix:
//   - matching stamped facts → unlink + consume (reverter AND sweeper)
//   - facts mismatch (the foreign-substitution shape) → NO unlink, entry
//     retained (armed→pending-clean / pending-clean stays pending)
//   - legacy unstamped entries → legacy removal path (pinned)
//   - every backup-removal refusal/error leg at the unit level (wave-32:
//     exercised through the successor chain quarantineReplacementBackupForRemoval
//     + hold.removeVerified)

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// w25StampBackupFacts returns the entry facts binding the on-disk backup
// object, mirroring the downloader's arm-time stamp.
func w25StampBackupFacts(t *testing.T, fs afero.Fs, backup string) (int64, int64) {
	t.Helper()
	info, err := lstatRestoreSource(fs, backup)
	require.NoError(t, err)
	require.NotNil(t, info)
	return info.Size(), info.ModTime().Unix()
}

// w25ArmedOp builds one applied operation whose applied install journaled
// dest→backup. facts=false models a legacy pre-wave-25 entry; facts=true
// stamps the current on-disk backup's size + mtime; a non-nil override
// stamps deliberately WRONG facts (the foreign-substitution shape).
func w25ArmedOp(t *testing.T, fs afero.Fs, repo *p3OpRepo, movieID string, destBytes, backupBytes []byte, stampMode string) (*models.BatchFileOperation, string, string) {
	t.Helper()
	dir := "/dst/" + movieID
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak.aaaaaaaaaaaaaaaa"
	require.NoError(t, fs.MkdirAll(dir, config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, dest, destBytes, config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, backup, backupBytes, config.FilePerm))

	entry := models.ReplacementEntry{Destination: dest, Backup: backup, DestSeq: 1, Installed: true}
	switch stampMode {
	case "stamped":
		entry.BackupSize, entry.BackupModUnix = w25StampBackupFacts(t, fs, backup)
	case "legacy":
		// zero facts — pre-wave-25 blob shape
	case "wrong":
		size, modUnix := w25StampBackupFacts(t, fs, backup)
		entry.BackupSize = size + 4096
		entry.BackupModUnix = modUnix + 86400
	default:
		t.Fatalf("unknown stamp mode %q", stampMode)
	}
	op := &models.BatchFileOperation{
		BatchJobID:    "job-" + movieID,
		MovieID:       movieID,
		OriginalPath:  "/src/" + movieID + ".mkv",
		NewPath:       dir + "/" + movieID + ".mkv",
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{entry},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op, dest, backup
}

func w25JournalEntries(t *testing.T, repo *p3OpRepo, id uint) []models.ReplacementEntry {
	t.Helper()
	row, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, row)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	return gf.Replacements
}

// Matching stamped identity: the restore unlinks the backup and consumes the
// entry exactly like the pre-facts flow.
func TestRestoreReplacementJournalW25_MatchingFactsUnlinkAndConsume(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25ArmedOp(t, fs, repo, "W25A", []byte("new poster"), []byte("original poster"), "stamped")

	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err)
	require.True(t, restored[dest])
	require.Equal(t, "original poster", string(mustRead2(t, fs, dest)), "backup bytes restored over the destination")
	exists, _ := afero.Exists(fs, backup)
	require.False(t, exists, "the verified-owned backup was unlinked")
	require.Empty(t, w25JournalEntries(t, repo, op.ID), "the journaled entry was consumed")
}

// The finding's central case: the occupant does not match the journaled
// arm-time identity (a directory writer swapped a foreign file onto the
// backup name after the journal write). Wave-62 (finding F1) refuses the
// copy BEFORE any bytes reach dest — the substituted backup never lands at
// dest, the foreign occupant at the backup name is preserved byte-intact,
// and the journal entry stays live (NOT restore-pending: the copy published
// nothing, so no cleanup marker is warranted).
func TestRestoreReplacementJournalW25_FactsMismatchRefusesUnlinkRetainsEntry(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25ArmedOp(t, fs, repo, "W25B", []byte("new poster"), []byte("foreign occupant bytes"), "wrong")

	_, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRestoreSourceRefused, "the pre-copy gate refuses the substituted backup")
	require.Contains(t, err.Error(), "failed to restore")
	require.Contains(t, err.Error(), "occupant identity mismatch")

	require.Equal(t, "new poster", string(mustRead2(t, fs, dest)),
		"dest stays untouched — the substituted backup's bytes never landed")
	require.Equal(t, "foreign occupant bytes", string(mustRead2(t, fs, backup)),
		"the foreign occupant at the backup name was NOT deleted or copied")
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1, "the journal entry stays live")
	require.False(t, entries[0].RestorePending, "no copy ran — no cleanup marker is warranted")
}

// Legacy unstamped entries keep the pre-wave-25 pathname removal posture.
func TestRestoreReplacementJournalW25_LegacyUnstampedConsumes(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25ArmedOp(t, fs, repo, "W25C", []byte("new"), []byte("old"), "legacy")

	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err)
	require.True(t, restored[dest])
	exists, _ := afero.Exists(fs, backup)
	require.False(t, exists, "legacy entries unlink without stamped facts (pinned posture)")
	require.Empty(t, w25JournalEntries(t, repo, op.ID))
}

// w25PendingOp arms an entry already marked restore-pending (clean kind):
// the destination carries the certified restored bytes; only the backup
// cleanup + journal consumption remain. NO copy runs for pending entries.
func w25PendingOp(t *testing.T, fs afero.Fs, repo *p3OpRepo, movieID string, backupBytes []byte, stampMode string) (*models.BatchFileOperation, string, string) {
	t.Helper()
	op, dest, backup := w25ArmedOp(t, fs, repo, movieID, []byte("restored bytes in place"), backupBytes, stampMode)
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1)
	entries[0].RestorePending = true // clean kind (empty RestorePendingKind)
	row, _ := repo.FindByID(context.Background(), op.ID)
	gf, _ := models.ParseGeneratedFiles(row.GeneratedFiles)
	gf.Replacements[0].RestorePending = true
	row.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), row))
	// The caller-visible op snapshot carries the pending marker too.
	snap := *op
	gf0, _ := models.ParseGeneratedFiles(snap.GeneratedFiles)
	gf0.Replacements[0].RestorePending = true
	snap.GeneratedFiles = models.MarshalLedgerJSON(gf0)
	*op = snap
	return op, dest, backup
}

// Pending-clean retry WITH matching facts: no copy, unlink, consume.
func TestRestoreReplacementJournalW25_PendingCleanMatchingFactsConsumes(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25PendingOp(t, fs, repo, "W25D", []byte("owned set-aside"), "stamped")

	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err)
	require.True(t, restored[dest])
	require.Equal(t, "restored bytes in place", string(mustRead2(t, fs, dest)),
		"pending retries never copy (the destination is certified)")
	exists, _ := afero.Exists(fs, backup)
	require.False(t, exists, "verified-owned pending backup unlinked")
	require.Empty(t, w25JournalEntries(t, repo, op.ID))
}

// Pending-clean retry against a mismatched occupant: NO copy (as ever) and
// now NO unlink either — the foreign file and the pending entry both stay.
func TestRestoreReplacementJournalW25_PendingCleanMismatchRetainsForeign(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25PendingOp(t, fs, repo, "W25E", []byte("foreign occupant"), "wrong")

	_, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.Error(t, err)
	require.Contains(t, err.Error(), "backup cleanup failed")
	require.Equal(t, "foreign occupant", string(mustRead2(t, fs, backup)),
		"the foreign occupant at the pending name was NOT deleted")
	require.Equal(t, "restored bytes in place", string(mustRead2(t, fs, dest)),
		"no copy ran against the unowned name")
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1, "entry retained live")
	require.True(t, entries[0].RestorePending)
}

// w25SweepCrashFixture builds the crash-window state the sweep heals: an
// armed (never-installed) journal entry, the backup present, the destination
// MISSING.
func w25SweepCrashOp(t *testing.T, fs afero.Fs, repo *p3OpRepo, movieID string, backupBytes []byte, stampMode string) (*models.BatchFileOperation, string, string) {
	t.Helper()
	op, dest, backup := w25ArmedOp(t, fs, repo, movieID, []byte("discard-me"), backupBytes, stampMode)
	require.NoError(t, fs.Remove(dest)) // the install never landed (crash window)
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1)
	row, _ := repo.FindByID(context.Background(), op.ID)
	gf, _ := models.ParseGeneratedFiles(row.GeneratedFiles)
	gf.Replacements[0].Installed = false
	row.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), row))
	return op, dest, backup
}

// Sweeper crash-window restore with matching stamped facts: restore, unlink,
// consume — healed.
func TestSweepW25_CrashWindowRestoreMatchingFactsHeals(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25SweepCrashOp(t, fs, repo, "W25F", []byte("original bytes"), "stamped")

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, healed, "the crash-window backup restored + consumed")
	require.Equal(t, "original bytes", string(mustRead2(t, fs, dest)))
	exists, _ := afero.Exists(fs, backup)
	require.False(t, exists)
	require.Empty(t, w25JournalEntries(t, repo, op.ID))
}

// Sweeper crash-window restore against a mismatched occupant: wave-62
// (finding F1) refuses the copy BEFORE any bytes reach the (missing)
// destination — the substituted backup's bytes never land at dest, the
// foreign occupant at the backup name is preserved, the entry stays armed
// (no copy ran, so no restore-pending marker), and a later sweep retries
// identically.
func TestSweepW25_CrashWindowRestoreMismatchKeepsForeignBackup(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25SweepCrashOp(t, fs, repo, "W25G", []byte("foreign occupant"), "wrong")

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Zero(t, healed, "the mismatched occupant is never consumed")
	exists, _ := afero.Exists(fs, dest)
	require.False(t, exists, "dest stays missing — the substituted backup's bytes never landed")
	require.Equal(t, "foreign occupant", string(mustRead2(t, fs, backup)),
		"the foreign occupant at the backup name was NOT deleted or copied")
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1, "the entry stays live")
	require.False(t, entries[0].RestorePending, "no copy ran — the entry stays armed, not pending")

	// A later sweep re-attempts the restore and refuses the same substituted
	// occupant up front; the entry stays armed for sweep/revert arbitration.
	healed, err = NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Zero(t, healed, "the retry also refuses the foreign occupant before any copy")
	require.Equal(t, "foreign occupant", string(mustRead2(t, fs, backup)))
	require.Len(t, w25JournalEntries(t, repo, op.ID), 1)
}

// Sweeper crash-window restore for a legacy unstamped entry keeps the
// pre-wave-25 behavior (pinned).
func TestSweepW25_CrashWindowRestoreLegacyUnstampedHeals(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25SweepCrashOp(t, fs, repo, "W25H", []byte("old bytes"), "legacy")

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "old bytes", string(mustRead2(t, fs, dest)))
	exists, _ := afero.Exists(fs, backup)
	require.False(t, exists, "legacy removal path is unchanged")
	require.Empty(t, w25JournalEntries(t, repo, op.ID))
}

// --- backup removal unit legs (the wave-32 successor chain) ---

type w25LstatFailFs struct {
	afero.Fs
	victim string
	err    error
}

func (f *w25LstatFailFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == f.victim {
		return nil, false, f.err
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

type w25LstatNilFs struct{ afero.Fs }

func (f *w25LstatNilFs) LstatIfPossible(string) (os.FileInfo, bool, error) { return nil, false, nil }

type w25OpenFailFs struct {
	afero.Fs
	victim string
	err    error
}

func (f *w25OpenFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if name == f.victim {
		return nil, f.err
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type w25StatBrokenFile struct{ afero.File }

func (f w25StatBrokenFile) Stat() (os.FileInfo, error) { return nil, errors.New("w25 fstat wedged") }

type w25StatNilFile struct{ afero.File }

func (f w25StatNilFile) Stat() (os.FileInfo, error) { return nil, nil }

// w25FakeStatFile substitutes the opened handle's Stat result.
func w25FakeStatFile(inner afero.File, info os.FileInfo) afero.File {
	return &w25StatSubstFile{File: inner, info: info}
}

type w25StatSubstFile struct {
	afero.File
	info os.FileInfo
}

func (f *w25StatSubstFile) Stat() (os.FileInfo, error) { return f.info, nil }

func TestRemoveReplacementBackupW25_InspectionLegs(t *testing.T) {
	ctxBackup := "/w25u/backup.bin"

	t.Run("missing backup is already removed", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, quarantineAndRemoveVerifiedReplacementBackup(fs, ctxBackup, "w25 unit", nil, nil))
	})

	t.Run("lstat failure retains the entry", func(t *testing.T) {
		sentinel := errors.New("w25 lstat wedged")
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, ctxBackup, []byte("x"), 0o644))
		fs := &w25LstatFailFs{Fs: base, victim: ctxBackup, err: sentinel}
		err := quarantineAndRemoveVerifiedReplacementBackup(fs, ctxBackup, "w25 unit", nil, nil)
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "x", string(mustRead2(t, base, ctxBackup)), "nothing was removed")
	})

	t.Run("lstat nil answer retains the entry", func(t *testing.T) {
		fs := &w25LstatNilFs{Fs: afero.NewMemMapFs()}
		err := quarantineAndRemoveVerifiedReplacementBackup(fs, ctxBackup, "w25 unit", nil, nil)
		require.Error(t, err)
		var refused *BackupRemovalRefusedError
		require.False(t, errors.As(err, &refused), "an indeterminate read is a keep-error, not a foreign-occupant refusal")
	})

	t.Run("directory occupant refused", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(ctxBackup, 0o755))
		err := quarantineAndRemoveVerifiedReplacementBackup(fs, ctxBackup, "w25 unit", nil, nil)
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, err, &refused)
	})

	t.Run("symlink occupant refused", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation needs elevated privilege on Windows")
		}
		tmp := t.TempDir()
		base := afero.NewOsFs()
		target := filepath.Join(tmp, "foreign.txt")
		link := filepath.Join(tmp, "poster.jpg.dlbak.aaaaaaaaaaaaaaaa")
		require.NoError(t, os.WriteFile(target, []byte("foreign"), 0o644))
		require.NoError(t, os.Symlink(target, link))
		err := quarantineAndRemoveVerifiedReplacementBackup(base, link, "w25 unit", nil, nil)
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, err, &refused, "a symlink at the backup name is never unlinked by this gate")
		st, lerr := os.Lstat(link)
		require.NoError(t, lerr)
		require.NotZero(t, st.Mode()&os.ModeSymlink, "the link object survived")
	})

	t.Run("journaled facts size mismatch refused", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, ctxBackup, []byte("12345"), 0o644))
		size, modUnix := w25StampBackupFacts(t, fs, ctxBackup)
		entry := &models.ReplacementEntry{BackupSize: size + 1, BackupModUnix: modUnix}
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, quarantineAndRemoveVerifiedReplacementBackup(fs, ctxBackup, "w25 unit", entry, nil), &refused)
		require.Equal(t, "12345", string(mustRead2(t, fs, ctxBackup)))
	})

	t.Run("journaled facts mtime mismatch refused", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, ctxBackup, []byte("12345"), 0o644))
		size, modUnix := w25StampBackupFacts(t, fs, ctxBackup)
		entry := &models.ReplacementEntry{BackupSize: size, BackupModUnix: modUnix + 60}
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, quarantineAndRemoveVerifiedReplacementBackup(fs, ctxBackup, "w25 unit", entry, nil), &refused)
	})

	t.Run("stale re-arm substitute mismatches the restore-read identity", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("dev/inode identity requires POSIX restoreSourceIdentity")
		}
		tmp := t.TempDir()
		base := afero.NewOsFs()
		read := filepath.Join(tmp, "read.txt")
		other := filepath.Join(tmp, "other.txt")
		require.NoError(t, os.WriteFile(read, []byte("same-size"), 0o644))
		require.NoError(t, os.WriteFile(other, []byte("same-size"), 0o644))
		readInfo, err := lstatRestoreSource(base, read)
		require.NoError(t, err)
		// copiedFrom binds a DIFFERENT object than the removal target.
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, quarantineAndRemoveVerifiedReplacementBackup(base, other, "w25 unit", nil, readInfo), &refused,
			"dev/inode binding catches a same-size same-mtime substitute")
		require.Equal(t, "same-size", string(mustRead2(t, base, other)))
	})

	t.Run("copiedFrom metadata mismatch refused", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, ctxBackup, []byte("12345"), 0o644))
		require.NoError(t, afero.WriteFile(fs, "/w25u/other", []byte("123456789"), 0o644))
		otherInfo, err := lstatRestoreSource(fs, "/w25u/other")
		require.NoError(t, err)
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, quarantineAndRemoveVerifiedReplacementBackup(fs, ctxBackup, "w25 unit", nil, otherInfo), &refused,
			"size/mtime binding independent of dev/inode availability")
	})

	t.Run("reopen failure retains the entry", func(t *testing.T) {
		sentinel := errors.New("w25 open wedged")
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, ctxBackup, []byte("x"), 0o644))
		fs := &w25OpenFailFs{Fs: base, victim: ctxBackup, err: sentinel}
		err := quarantineAndRemoveVerifiedReplacementBackup(fs, ctxBackup, "w25 unit", nil, nil)
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "x", string(mustRead2(t, base, ctxBackup)))
	})

	t.Run("reopen vanished is removed", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, ctxBackup, []byte("x"), 0o644))
		fs := &w25OpenFailFs{Fs: base, victim: ctxBackup, err: os.ErrNotExist}
		require.NoError(t, quarantineAndRemoveVerifiedReplacementBackup(fs, ctxBackup, "w25 unit", nil, nil))
	})

	t.Run("opened fstat failure retains the entry", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, ctxBackup, []byte("x"), 0o644))
		prev := restoreOpenReplacementSource
		restoreOpenReplacementSource = func(fsys afero.Fs, name string) (afero.File, error) {
			inner, err := fsys.Open(name)
			if err != nil {
				return nil, err
			}
			return w25StatBrokenFile{File: inner}, nil
		}
		defer func() { restoreOpenReplacementSource = prev }()
		err := quarantineAndRemoveVerifiedReplacementBackup(base, ctxBackup, "w25 unit", nil, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "fstat wedged")
		require.Equal(t, "x", string(mustRead2(t, base, ctxBackup)))
	})

	t.Run("opened nil stat refused", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, ctxBackup, []byte("x"), 0o644))
		prev := restoreOpenReplacementSource
		restoreOpenReplacementSource = func(fsys afero.Fs, name string) (afero.File, error) {
			inner, err := fsys.Open(name)
			if err != nil {
				return nil, err
			}
			return w25StatNilFile{File: inner}, nil
		}
		defer func() { restoreOpenReplacementSource = prev }()
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, quarantineAndRemoveVerifiedReplacementBackup(base, ctxBackup, "w25 unit", nil, nil), &refused)
	})

	t.Run("opened non-regular refused", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(base, ctxBackup, []byte("x"), 0o644))
		dirInfo, err := base.Stat("/w25u")
		require.NoError(t, err)
		prev := restoreOpenReplacementSource
		restoreOpenReplacementSource = func(fsys afero.Fs, name string) (afero.File, error) {
			inner, err := fsys.Open(name)
			if err != nil {
				return nil, err
			}
			return w25FakeStatFile(inner, dirInfo), nil
		}
		defer func() { restoreOpenReplacementSource = prev }()
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, quarantineAndRemoveVerifiedReplacementBackup(base, ctxBackup, "w25 unit", nil, nil), &refused)
	})

	t.Run("opened object differs from the Lstat object", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("dev/inode identity requires POSIX restoreSourceIdentity")
		}
		tmp := t.TempDir()
		base := afero.NewOsFs()
		backup := filepath.Join(tmp, "poster.jpg.dlbak.aaaaaaaaaaaaaaaa")
		other := filepath.Join(tmp, "other.txt")
		require.NoError(t, os.WriteFile(backup, []byte("owned"), 0o644))
		require.NoError(t, os.WriteFile(other, []byte("other"), 0o644))
		prev := restoreOpenReplacementSource
		restoreOpenReplacementSource = func(fsys afero.Fs, _ string) (afero.File, error) {
			// The name was swapped between the Lstat and the open: the open
			// landed on a DIFFERENT object.
			return fsys.Open(other)
		}
		defer func() { restoreOpenReplacementSource = prev }()
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, quarantineAndRemoveVerifiedReplacementBackup(base, backup, "w25 unit", nil, nil), &refused)
		require.Equal(t, "owned", string(mustRead2(t, base, backup)), "the verified object survived")
	})

	t.Run("fully verified owned object unlinks", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("dev/inode identity requires POSIX restoreSourceIdentity")
		}
		tmp := t.TempDir()
		base := afero.NewOsFs()
		backup := filepath.Join(tmp, "poster.jpg.dlbak.aaaaaaaaaaaaaaaa")
		require.NoError(t, os.WriteFile(backup, []byte("owned bytes"), 0o644))
		stamp, modUnix := w25StampBackupFacts(t, base, backup)
		entry := &models.ReplacementEntry{BackupSize: stamp, BackupModUnix: modUnix}
		copied, err := lstatRestoreSource(base, backup)
		require.NoError(t, err)
		require.NoError(t, quarantineAndRemoveVerifiedReplacementBackup(base, backup, "w25 unit", entry, copied))
		_, statErr := os.Lstat(backup)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("rm failure retains the entry", func(t *testing.T) {
		sentinel := errors.New("w25 remove wedged")
		fs := &w25RemoveFailFs{Fs: afero.NewMemMapFs(), victim: ctxBackup, err: sentinel}
		require.NoError(t, afero.WriteFile(fs, ctxBackup, []byte("x"), 0o644))
		err := quarantineAndRemoveVerifiedReplacementBackup(fs, ctxBackup, "w25 unit", nil, nil)
		require.ErrorIs(t, err, sentinel)
	})
}

// w25RemoveFailFs fails the final Remove for the victim name. Wave-26: the
// removal gate unlinks the QUARANTINE sibling (victim + ".dlq." + token),
// so the wedge covers both spellings; the wedge compensation then moves the
// quarantined object back, keeping the pre-wave-26 assertions intact.
type w25RemoveFailFs struct {
	afero.Fs
	victim string
	err    error
}

func (f *w25RemoveFailFs) Remove(name string) error {
	if name == f.victim || strings.HasPrefix(name, f.victim+backupQuarantineSuffix) {
		return f.err
	}
	return f.Fs.Remove(name)
}

// journaledEntryFacts + journalEntryPendingKind tolerate unreadable ledgers.
func TestW25_JournalFactHelpers_TolerateBrokenLedger(t *testing.T) {
	broken := &models.BatchFileOperation{GeneratedFiles: `{"replacements": broken`}
	require.Nil(t, journaledEntryFacts(broken, "any"),
		"an unparseable ledger yields no stamped facts")
	require.Equal(t, "", journalEntryPendingKind(broken, "any"),
		"an unparseable ledger yields no pending kind (armed posture)")

	// Lookup keys are pre-normalized DestKeys — on Windows that uppercases +
	// backslashes, so derive the expected keys through sweepSlash instead of
	// hardcoding the posix spellings.
	row := &models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{Destination: "/d", Backup: "/b", BackupSize: 3, BackupModUnix: 9}},
	})}
	require.Nil(t, journaledEntryFacts(row, sweepSlash("other")))
	keyB := sweepSlash("/b")
	entry := journaledEntryFacts(row, keyB)
	require.NotNil(t, entry)
	require.Equal(t, int64(3), entry.BackupSize)
	require.Equal(t, int64(9), entry.BackupModUnix)
	entry.BackupSize = 99
	require.Equal(t, int64(3), journaledEntryFacts(row, keyB).BackupSize,
		"the returned facts are a copy — mutating them never edits the ledger snapshot")
}
