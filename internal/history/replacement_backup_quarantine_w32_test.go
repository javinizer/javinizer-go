package history

// POSTER-WRITE-HARDENING wave-32 (codex local review round 2, PR#215 findings
// R1+R4):
//
//   - R4: the quarantine unlink re-binds the name at Remove time (the
//     re-verify→Remove window is a watcher's), and an ENOENT at Remove time
//     or at the post-move re-verify NO LONGER consumes ownership — the owned
//     bytes vanished unownably, so the journal entry stays live
//     (errReplacementBackupQuarantineVanished). Callers whose pending marker
//     persists route the vanished class onto the REARM-REFUSED kind (the
//     journaled name is absent by construction; only the journal-only retry
//     converges).
//   - R1: every "delete backup after verified restore" leg runs the split
//     quarantine — verified move, THEN the destination re-gate, THEN the
//     quarantined unlink — so a foreign swap/deletion between the wave-31
//     check and the removal cannot destroy the recoverable bytes with
//     consumption going through. Divergence moves the verified object back
//     onto the journaled name and keeps the entry live.
//
// Test matrix: hold-level unlink legs through a scripted quarantine fs, the
// hold lifecycle guards, the vanished-kind routing unit, and caller-level
// divergence flows for the reverter + sweeper (armed, already-consumed,
// pending-clean, and durable pending legs).

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// w32QuarFs scripts the quarantine fs surface: armed latches on the
// quarantining PUBLISH rename (wave-42: the conditional handoff issues two
// suffix renames — the take-aside (suffix→suffix) and the publish (src→
// suffix) — the scripted surface keys on the publish, when the verified
// object lands at the quarantine name), post-arm quarantine-name lookups
// route through lstat by call number (call 1 = the phase-1 post-move
// re-verify, call 2 = the unlink-time re-verify), and Remove wedges the
// quarantine unlink.
type w32QuarFs struct {
	afero.Fs
	armed    bool
	quarName string // the publish target, learned at the publish rename (wave-42)
	moves    int
	lookups  int
	lstat    func(call int, name string) (os.FileInfo, error)
	remove   func(name string) error
}

func (f *w32QuarFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && strings.Contains(newname, backupQuarantineSuffix) {
		f.moves++
		if !strings.Contains(oldname, backupQuarantineSuffix) {
			f.armed = true
			f.quarName = newname
		}
	}
	return err
}

func (f *w32QuarFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.armed && name == f.quarName {
		f.lookups++
		if f.lstat != nil {
			info, err := f.lstat(f.lookups, name)
			return info, false, err
		}
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func (f *w32QuarFs) Remove(name string) error {
	// Wave-42: the take-aside placeholder unlink (a 0-byte scratch whose
	// wedge posture is warn-only) is never the scripted victim.
	if f.armed && f.remove != nil && name == f.quarName {
		return f.remove(name)
	}
	return f.Fs.Remove(name)
}

// w32RestoreReadsReal passes the phase-1 re-verify through and scripts only
// the unlink-time lookup.
func w32RestoreReadsReal(fs *w32QuarFs) func(call int, name string) (os.FileInfo, error) {
	return func(call int, name string) (os.FileInfo, error) {
		if ls, ok := fs.Fs.(afero.Lstater); ok {
			info, _, err := ls.LstatIfPossible(name)
			return info, err
		}
		info, err := fs.Fs.Stat(name)
		return info, err
	}
}

func TestRemoveReplacementBackupW32_UnlinkTimeLegs(t *testing.T) {
	const backup = "/w32u/poster.jpg.dlbak." + p3HexA

	t.Run("unlink-time ENOENT is typed indeterminate retention", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w32QuarFs{Fs: base}
		fs.lstat = func(call int, name string) (os.FileInfo, error) {
			if call == 2 {
				return nil, afero.ErrFileNotFound
			}
			return w32RestoreReadsReal(fs)(call, name)
		}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w32 unit", nil, nil)
		require.ErrorIs(t, err, errReplacementBackupQuarantineVanished)
		_, statErr := base.Stat(backup)
		require.ErrorIs(t, statErr, os.ErrNotExist, "the verified object stays at the quarantine name — move was never compensated")
		require.Len(t, w26DirQuarNames(t, base, "/w32u"), 1,
			"the wedge only REPLAYED the vanish — the bytes are recoverable for manual inspection")
	})

	t.Run("unlink-time indeterminate lookup restores the journaled name", func(t *testing.T) {
		sentinel := errors.New("unlink-time lstat wedged")
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w32QuarFs{Fs: base}
		fs.lstat = func(call int, name string) (os.FileInfo, error) {
			if call == 2 {
				return nil, sentinel
			}
			return w32RestoreReadsReal(fs)(call, name)
		}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w32 unit", nil, nil)
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "old", string(mustRead2(t, base, backup)), "the compensation moved the object back")
		require.Empty(t, w26DirQuarNames(t, base, "/w32u"))
	})

	t.Run("unlink-time nil answer refuses typed and restores", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		fs := &w32QuarFs{Fs: base}
		fs.lstat = func(call int, name string) (os.FileInfo, error) {
			if call == 2 {
				return nil, nil
			}
			return w32RestoreReadsReal(fs)(call, name)
		}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w32 unit", nil, nil)
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, err, &refused)
		require.Contains(t, refused.Reason, "no longer names the verified regular file at the unlink")
		require.Equal(t, "old", string(mustRead2(t, base, backup)))
		require.Empty(t, w26DirQuarNames(t, base, "/w32u"))
	})

	t.Run("unlink-time symlink-mode answer refuses typed and restores", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		linkRoot := t.TempDir()
		require.NoError(t, os.Symlink("nowhere", linkRoot+"/link"))
		linkInfo, lerr := os.Lstat(linkRoot + "/link")
		require.NoError(t, lerr)
		fs := &w32QuarFs{Fs: base}
		fs.lstat = func(call int, name string) (os.FileInfo, error) {
			if call == 2 {
				return linkInfo, nil
			}
			return w32RestoreReadsReal(fs)(call, name)
		}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w32 unit", nil, nil)
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, err, &refused)
		require.Contains(t, refused.Reason, "no longer names the verified regular file at the unlink")
		require.Equal(t, "old", string(mustRead2(t, base, backup)))
	})

	t.Run("unlink-time metadata change refuses typed and restores", func(t *testing.T) {
		base := afero.NewMemMapFs()
		w26WriteBackup(t, base, backup, "old")
		require.NoError(t, afero.WriteFile(base, "/w32u/foreign", []byte("a much longer foreign payload"), 0o644))
		foreignInfo, ferr := base.Stat("/w32u/foreign")
		require.NoError(t, ferr)
		fs := &w32QuarFs{Fs: base}
		fs.lstat = func(call int, name string) (os.FileInfo, error) {
			if call == 2 {
				return foreignInfo, nil
			}
			return w32RestoreReadsReal(fs)(call, name)
		}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w32 unit", nil, nil)
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, err, &refused)
		require.Contains(t, refused.Reason, "metadata changed between the re-verify and the unlink")
		require.Equal(t, "old", string(mustRead2(t, base, backup)))
		require.Empty(t, w26DirQuarNames(t, base, "/w32u"))
	})

	t.Run("unlink-time dev/inode mismatch on OsFs refuses typed and restores", func(t *testing.T) {
		base := afero.NewOsFs()
		dir := t.TempDir()
		backupOS := dir + "/poster.jpg.dlbak." + p3HexA
		require.NoError(t, os.WriteFile(backupOS, []byte("old"), 0o644))
		require.NoError(t, os.WriteFile(dir+"/foreign", []byte("foreign-even-if-same-size"), 0o644))
		foreignInfo, ferr := os.Lstat(dir + "/foreign")
		require.NoError(t, ferr)
		fs := &w32QuarFs{Fs: base}
		fs.lstat = func(call int, name string) (os.FileInfo, error) {
			if call == 2 {
				return foreignInfo, nil
			}
			return w32RestoreReadsReal(fs)(call, name)
		}

		err := quarantineAndRemoveVerifiedReplacementBackup(fs, backupOS, "w32 unit", nil, nil)
		var refused *BackupRemovalRefusedError
		require.ErrorAs(t, err, &refused,
			"both platform legs are the SAME refusal class — a proven-foreign answer at the unlink")
		if runtime.GOOS == "windows" {
			// The Windows Stat_t identity is unavailable (restoreSourceIdentity
			// reports ok=false), so the dev/inode comparison never runs there;
			// the scripted same-window substitution lands on the metadata leg.
			require.Contains(t, refused.Reason, "metadata changed between the re-verify and the unlink")
		} else {
			require.Contains(t, refused.Reason, "dev/inode mismatch")
		}
		require.Equal(t, "old", string(mustRead2(t, base, backupOS)))
		entries, derr := os.ReadDir(dir)
		require.NoError(t, derr)
		for _, e := range entries {
			require.NotContains(t, e.Name(), backupQuarantineSuffix, "the compensation consumed the quarantine name")
		}
	})
}

// Hold lifecycle: the re-gate compensation is idempotent, a completed hold
// unlinks nothing twice, and an absent-at-gate hold is an inert success.
func TestBackupQuarantineHoldW32_Lifecycle(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w32h/poster.jpg.dlbak." + p3HexA
	w26WriteBackup(t, base, backup, "old")

	t.Run("restore then restore again moves the bytes back exactly once", func(t *testing.T) {
		hold, err := quarantineReplacementBackupForRemoval(base, backup, "w32 unit", nil, nil)
		require.NoError(t, err)
		require.True(t, hold.moved)
		hold.restore()
		hold.restore()
		require.Equal(t, "old", string(mustRead2(t, base, backup)))
		require.Empty(t, w26DirQuarNames(t, base, "/w32h"))
	})

	t.Run("a completed hold never acts again", func(t *testing.T) {
		w26WriteBackup(t, base, backup, "old")
		hold, err := quarantineReplacementBackupForRemoval(base, backup, "w32 unit", nil, nil)
		require.NoError(t, err)
		require.NoError(t, hold.removeVerified())
		exists, _ := afero.Exists(base, backup)
		require.False(t, exists)
		hold.restore()
		require.False(t, hold.moved)
	})

	t.Run("absent-at-gate hold is an inert success", func(t *testing.T) {
		hold, err := quarantineReplacementBackupForRemoval(base, "/w32h/never-existed", "w32 unit", nil, nil)
		require.NoError(t, err)
		require.NoError(t, hold.removeVerified())
		hold.restore()
	})
}

// pendingKindForRemovalError: the vanished class routes to the journal-only
// (rearm-refused) kind; every other failure keeps the clean marker.
func TestPendingKindForRemovalErrorW32(t *testing.T) {
	require.Equal(t, models.RestorePendingKindRearmRefused,
		pendingKindForRemovalError(errReplacementBackupQuarantineVanished))
	require.Equal(t, models.RestorePendingKindClean,
		pendingKindForRemovalError(errors.New("plain removal failure")))
	require.Equal(t, models.RestorePendingKindClean,
		pendingKindForRemovalError(refuseReplacementBackupRemoval("b", "p", "r")))
}

// quarantineAndRemoveVerifiedReplacementBackup composes the wave-32
// successors the way the removed one-shot removeReplacementBackup did for
// callers WITHOUT a destination re-gate: bind the occupant + verified
// quarantine move, then the quarantine unlink. Unit legs whose scenario
// never touches a destination exercise the successor chain through it.
// Callers WITH a destination gate (reverter/sweeper legs) hold the split
// explicitly in the production code under test.
func quarantineAndRemoveVerifiedReplacementBackup(fs afero.Fs, backup, phase string, entry *models.ReplacementEntry, copiedFrom os.FileInfo) error {
	hold, err := quarantineReplacementBackupForRemoval(fs, backup, phase, entry, copiedFrom)
	if err != nil {
		return err
	}
	return hold.removeVerified()
}

// w32ScriptRestoredDestSeam answers the wave-31 destination recheck from a
// fixed script, then falls back to the production wiring.
func w32ScriptRestoredDestSeam(t *testing.T, answers ...bool) {
	t.Helper()
	prev := restoredDestStillOurs
	calls := 0
	restoredDestStillOurs = func(fs afero.Fs, dest string, id restoredDestIdentity) bool {
		if calls < len(answers) {
			answer := answers[calls]
			calls++
			return answer
		}
		calls++
		return prev(fs, dest, id)
	}
	t.Cleanup(func() { restoredDestStillOurs = prev })
}

// w32DestPresenceGateFs fails the destination's no-follow lookup once the
// quarantining rename has armed — the post-quarantine presence re-gate's
// divergence instant.
type w32DestPresenceGateFs struct {
	afero.Fs
	dest  string
	armed bool
	err   error
}

func (f *w32DestPresenceGateFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && strings.Contains(newname, backupQuarantineSuffix) {
		f.armed = true
	}
	return err
}

func (f *w32DestPresenceGateFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.armed && name == f.dest {
		return nil, false, f.err
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// R1 — reverter ARMED leg: the destination no longer names the just-restored
// object after the backup was quarantined ⇒ the verified object moves back
// onto the journaled name, the entry stays ARMED (never restore-pending),
// nothing is unlinked, and the next explicit retry heals from the armed
// state.
func TestRestoreReplacementJournalW32_PostQuarantineDivergenceRetainsArmed(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25ArmedOp(t, fs, repo, "W32A", []byte("new poster"), []byte("original poster"), "stamped")

	w32ScriptRestoredDestSeam(t, true, false)

	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.Error(t, err)
	require.Contains(t, err.Error(), "diverged after the backup was quarantined")
	require.True(t, restored[dest])
	require.Equal(t, "original poster", string(mustRead2(t, fs, backup)),
		"the verified object moved BACK onto the journaled name")
	require.Equal(t, "original poster", string(mustRead2(t, fs, dest)),
		"the restored destination is never removed")
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending, "the entry stays ARMED — no marker certifies against an unproven destination")
	require.Empty(t, w26DirQuarNames(t, fs, "/dst/W32A"))

	// The armed posture converges on retry.
	restoredDestStillOurs = destStillNamesRestoredObject
	restored2, err2 := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err2)
	require.True(t, restored2[dest])
	exists, _ := afero.Exists(fs, backup)
	require.False(t, exists)
	require.Empty(t, w25JournalEntries(t, repo, op.ID))
}

// R1 — reverter PENDING-CLEAN leg: the destination-presence re-gate fires
// after the quarantine move; a missing/indeterminate destination puts the
// verified object back and keeps the pending marker live.
func TestRestoreReplacementJournalW32_PendingCleanPresenceDivergenceRestoresBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest := "/dst/W32P/poster.jpg"
	backup := dest + ".dlbak.aaaaaaaaaaaaaaaa"
	require.NoError(t, base.MkdirAll("/dst/W32P", config.DirPerm))
	require.NoError(t, afero.WriteFile(base, dest, []byte("certified restored bytes"), config.FilePerm))
	require.NoError(t, afero.WriteFile(base, backup, []byte("original poster"), config.FilePerm))
	op := &models.BatchFileOperation{
		BatchJobID: "job-W32P", MovieID: "W32P",
		OriginalPath:  "/src/W32P.mkv",
		NewPath:       "/dst/W32P/W32P.mkv",
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{
				Destination: dest, Backup: backup, DestSeq: 1,
				Installed: true, RestorePending: true,
			}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))

	fs := &w32DestPresenceGateFs{Fs: base, dest: dest, err: errors.New("dest lookup wedged post-quarantine")}
	_, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.Error(t, err)
	require.Contains(t, err.Error(), "diverged after the backup was quarantined")
	require.Equal(t, "original poster", string(mustRead2(t, base, backup)),
		"the verified object moved back onto the journaled name")
	require.Equal(t, "certified restored bytes", string(mustRead2(t, base, dest)), "the destination is untouched")
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.True(t, entries[0].RestorePending, "the pending marker stays live for the retry")
	require.Empty(t, w26DirQuarNames(t, base, "/dst/W32P"))
}

// R1+R4 — reverter ARMED leg with the quarantine unlink answering ENOENT:
// the bytes vanished unownably, so nothing is consumed AND the durable
// marker upgrades to the journal-only (rearm-refused) kind — the journaled
// name is absent by construction. The explicit revert then converges
// journal-only.
func TestRestoreReplacementJournalW32_VanishedUnlinkUpgradesToJournalOnlyKind(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, notExist: true, fail: true}
	repo := newP3OpRepo()
	op, dest, backup := w25ArmedOp(t, fs, repo, "W32V", []byte("new poster"), []byte("original poster"), "stamped")
	fs.victim = backup

	_, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.Error(t, err)
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1, "the vanished unlink never consumes")
	require.True(t, entries[0].RestorePending)
	require.Equal(t, models.RestorePendingKindRearmRefused, entries[0].PendingKind(),
		"the vanished class routes to the journal-only retry kind")
	require.Equal(t, "original poster", string(mustRead2(t, base, dest)))
	_, statErr := base.Stat(backup)
	require.ErrorIs(t, statErr, os.ErrNotExist)

	// The journal-only pending leg converges on the explicit retry.
	freshRow, frErr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, frErr)
	restored, err2 := NewReverter(base, repo).restoreReplacementJournal(context.Background(), freshRow)
	require.NoError(t, err2)
	require.True(t, restored[dest])
	require.Empty(t, w25JournalEntries(t, repo, op.ID))
}

// R1 — sweeper ARMED leg (entryPresent): post-quarantine divergence keeps
// the entry ARMED (never restore-pending), the backup restored to its
// journaled name, the restored destination untouched.
func TestSweepW32_ArmedLegPostQuarantineDivergenceRetainsArmed(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w25SweepCrashOp(t, fs, repo, "W32B", []byte("original bytes"), "stamped")

	w32ScriptRestoredDestSeam(t, true, false)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	require.Equal(t, "original bytes", string(mustRead2(t, fs, backup)),
		"the verified object moved back onto the journaled name")
	require.Equal(t, "original bytes", string(mustRead2(t, fs, dest)),
		"the restored destination is never removed")
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending, "the entry stays ARMED — no marker against an unproven destination")
	require.Empty(t, w26DirQuarNames(t, fs, "/dst/W32B"))
}

// R1 — sweeper ALREADY-CONSUMED leg: post-quarantine divergence restores the
// backup and reports not-healed; nothing is consumed or unlinked.
func TestSweepW32_AlreadyConsumedLegPostQuarantineDivergenceRetains(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W32R/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll("/out/W32R", config.DirPerm))
	writeSweepFile(t, base, backup, "old", 1)
	row := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "W32R", OriginalPath: "/src/w32r.mkv",
		OperationType:  models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{"/out/W32R"}}),
		RevertStatus:   models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, row))
	info, err := base.Stat(backup)
	require.NoError(t, err)
	idx := &replacementLedgerIndex{journaled: map[string]*models.BatchFileOperation{sweepSlash(backup): row}}

	w32ScriptRestoredDestSeam(t, true, false)

	got := NewReplacementSweeper(base, repo).sweepOne(ctx, idx, "/out/W32R", info)
	require.Equal(t, 0, got, "nothing consumed, nothing healed")
	require.Equal(t, "old", string(mustRead2(t, base, backup)),
		"the verified object moved back onto the journaled name")
	require.Equal(t, "old", string(mustRead2(t, base, dest)),
		"the restored destination is never removed")
	require.Empty(t, w26DirQuarNames(t, base, "/out/W32R"))
}

// R1 — retryPendingRemoval LEGACY leg (no durable entry facts): the
// destination-presence re-gate fires after the quarantine move; divergence
// restores the bytes and reports not-healed.
func TestSweepW32_LegacyPendingPresenceDivergenceRestoresBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W32L/dest.jpg"
	backup := "/out/W32L/dest.jpg.dlbak." + p3HexA
	require.NoError(t, base.MkdirAll("/out/W32L", config.DirPerm))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), config.FilePerm))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), config.FilePerm))
	row := &models.BatchFileOperation{
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{"/out/W32L"}}),
		RevertStatus:   models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, row))

	fs := &w32DestPresenceGateFs{Fs: base, dest: dest, err: errors.New("dest lookup wedged post-quarantine")}
	got := NewReplacementSweeper(fs, repo).retryPendingRemoval(ctx, row.ID, backup, dest, sweepSlash(backup))
	require.False(t, got)
	require.Equal(t, "old", string(mustRead2(t, base, backup)),
		"the verified object moved back onto the journaled name")
	require.Equal(t, "old", string(mustRead2(t, base, dest)), "the destination is untouched")
	require.Empty(t, w26DirQuarNames(t, base, "/out/W32L"))
}

// R1 — retryPendingRemoval DURABLE leg (clean kind): the presence re-gate
// divergence restores the backup, keeps the in-process fallback armed, and
// leaves the durable marker live.
func TestSweepW32_DurablePendingPresenceDivergenceRestoresBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W32D/dest.jpg"
	backup := "/out/W32D/dest.jpg.dlbak." + p3HexA
	require.NoError(t, base.MkdirAll("/out/W32D", config.DirPerm))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), config.FilePerm))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), config.FilePerm))
	op := &models.BatchFileOperation{
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{Destination: dest, Backup: backup, RestorePending: true}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	fs := &w32DestPresenceGateFs{Fs: base, dest: dest, err: errors.New("dest lookup wedged post-quarantine")}
	got := NewReplacementSweeper(fs, repo).retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup))
	require.False(t, got)
	require.Equal(t, "old", string(mustRead2(t, base, backup)),
		"the verified object moved back onto the journaled name")
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.True(t, entries[0].RestorePending, "the durable marker stays live")
	require.Empty(t, w26DirQuarNames(t, base, "/out/W32D"))
}

// R4 — retryPendingRemoval DURABLE leg with the quarantine unlink answering
// ENOENT: the durable marker upgrades to the rearm-refused kind (the name is
// absent by construction; only the journal-only retry converges).
func TestSweepW32_DurablePendingVanishedUnlinkUpgradesKind(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, notExist: true, fail: true}
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W32X/dest.jpg"
	backup := "/out/W32X/dest.jpg.dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll("/out/W32X", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
	fs.victim = backup
	op := &models.BatchFileOperation{
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{Destination: dest, Backup: backup, RestorePending: true}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	got := NewReplacementSweeper(fs, repo).retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup))
	require.False(t, got, "the vanished unlink never consumes")
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.Equal(t, models.RestorePendingKindRearmRefused, entries[0].PendingKind(),
		"the vanished class routed the durable marker to the journal-only kind")

	// The upgraded durable kind converges journal-only on retry.
	got = NewReplacementSweeper(base, repo).retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup))
	require.True(t, got)
	require.Empty(t, w25JournalEntries(t, repo, op.ID))
}

// w32FailNthJournalTxRepo fails exactly the Nth UpdateJournalInTx call — the
// pending retry's read transaction must succeed while the vanished-class
// marker persist (a LATER transaction) wedges.
type w32FailNthJournalTxRepo struct {
	*p3OpRepo
	failAt int
	calls  int
	err    error
}

func (r *w32FailNthJournalTxRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	r.calls++
	if r.calls == r.failAt {
		return r.err
	}
	return r.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}

// R4 — retryPendingRemoval DURABLE leg, vanished class with the marker
// persist wedged: the upgrade warn stays best-effort — nothing is consumed
// (not healed), the durable entry keeps its CLEAN pending kind (the upgrade
// never committed), while the in-process memory still upgrades to
// rearm-refused and DOMINATES the durable kind on the next retry,
// converging journal-only once the wedge lifts.
func TestSweepW32_DurablePendingVanishedUnlinkPersistFailureKeepsCleanDurable(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, notExist: true, fail: true}
	repo := &w32FailNthJournalTxRepo{p3OpRepo: newP3OpRepo(), failAt: 2 /* 1 = the read tx */, err: errors.New("w32 marker persist wedged")}
	ctx := context.Background()
	dest := "/out/W32F/dest.jpg"
	backup := "/out/W32F/dest.jpg.dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll("/out/W32F", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
	fs.victim = backup
	op := &models.BatchFileOperation{
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{Destination: dest, Backup: backup, RestorePending: true}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	sweeper := NewReplacementSweeper(fs, repo)
	got := sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup))
	require.False(t, got, "nothing is consumed while the upgrade cannot be persisted")
	entries := w25JournalEntries(t, repo.p3OpRepo, op.ID)
	require.Len(t, entries, 1)
	require.True(t, entries[0].RestorePending)
	require.Equal(t, models.RestorePendingKindClean, entries[0].PendingKind(),
		"the durable marker keeps the clean kind — the upgrade never committed")
	kind, ok := sweeper.pendingRemovalKind(sweepSlash(backup))
	require.True(t, ok)
	require.Equal(t, models.RestorePendingKindRearmRefused, kind,
		"the in-process memory still upgrades and routes the retry journal-only")
	_, statErr := base.Stat(backup)
	require.ErrorIs(t, statErr, os.ErrNotExist, "the verified object moved aside and vanished (replayed)")

	// Wedge lifted: the in-process rearm-refused memory dominates the durable
	// clean kind, so the retry consumes journal-only.
	repo.failAt = -1
	got = sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup))
	require.True(t, got, "the in-process rearm-refused routing converges journal-only")
	require.Empty(t, w25JournalEntries(t, repo.p3OpRepo, op.ID))
}
