package history

// POSTER-WRITE-HARDENING wave-36 (codex local review round 6, PR#215 finding
// F3) — propagate a FAILED quarantine move-back to the journal callers.
//
// When the destination re-gate diverges after the verified backup was
// quarantined and the NO-REPLACE move-back then fails (a foreign claimant
// holds the journaled name), the verified bytes stay recoverable at the
// .dlq. quarantine name — but the pre-wave-36 callers left the journal entry
// armed (or clean-pending) against the now-UNOWNED name, so a later leg
// would stat/copy/remove the foreign occupant. The sweep's armed +
// durable-pending legs and the reverter's compensation now persist the
// rearm-refused (journal-only) pending kind; the consumed/legacy legs (no
// live entry to mark) just surface the failure.
//
// Test matrix:
//   - sweep armed leg + reverter compensation: claimed-name rollback failure
//     → entry pending (rearm-refused), quarantine retained, error surfaces;
//     marker-persist failure keeps the armed/clean durable posture and only
//     warns;
//   - already-consumed sweep leg + legacy pending leg: no live entry —
//     surface only, everything byte-intact;
//   - durable pending leg with the marker persist wedged: durable kind stays
//     clean while the in-process memory upgrades and dominates the retry.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// w36QuarMovePlantFs plants foreign bytes at a target name the moment the
// quarantining rename lands — the foreign claimant that makes the wedge
// move-back fail (no-replace) and the journaled name unowned.
type w36QuarMovePlantFs struct {
	afero.Fs
	plant string
	bytes []byte
	armed bool
}

// Wave-42: the conditional handoff issues two suffix renames — the
// take-aside (suffix→suffix) and the publish (src→suffix); the plant lands
// when the PUBLISH (the verified object's move) lands, never the take.
func (f *w36QuarMovePlantFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && !f.armed && strings.Contains(newname, backupQuarantineSuffix) && !strings.Contains(oldname, backupQuarantineSuffix) {
		f.armed = true
		if werr := afero.WriteFile(f.Fs, f.plant, f.bytes, 0o644); werr != nil {
			return werr
		}
	}
	return err
}

// w36DestGatePlantFs additionally wedges the destination no-follow lookup
// once the quarantine move armed — the pending-retry presence re-gate's
// divergence instant with the failed move-back composed.
type w36DestGatePlantFs struct {
	afero.Fs
	dest    string
	destErr error
	plant   string
	bytes   []byte
	armed   bool
}

// Wave-42: arms on the PUBLISH rename (oldname lacks the suffix) — never
// the take-aside — so the destination gate wedges only once the verified
// object provably sits at the quarantine name.
func (f *w36DestGatePlantFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && strings.Contains(newname, backupQuarantineSuffix) && !strings.Contains(oldname, backupQuarantineSuffix) {
		f.armed = true
		if werr := afero.WriteFile(f.Fs, f.plant, f.bytes, 0o644); werr != nil {
			return werr
		}
	}
	return err
}

func (f *w36DestGatePlantFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.armed && name == f.dest {
		return nil, false, f.destErr
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		info, _, err := ls.LstatIfPossible(name)
		return info, false, err
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// Sweep ARMED leg: the destination diverges after the quarantine move AND
// the move-back fails on the claimed name — the durable entry is disarmed
// onto the rearm-refused pending kind, the foreign claimant keeps the name,
// and the verified bytes stay recoverable at the quarantine name.
func TestSweepW36_ArmedLegFailedMoveBackMarksRearmRefused(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	plantFs := &w36QuarMovePlantFs{Fs: base}
	op, dest, backup := w25SweepCrashOp(t, plantFs, repo, "W36A", []byte("original bytes"), "stamped")
	plantFs.plant, plantFs.bytes = backup, []byte("foreign claimant at the journaled name")

	w32ScriptRestoredDestSeam(t, true, false)

	healed, err := NewReplacementSweeper(plantFs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1, "the live entry is retained, never consumed")
	require.True(t, entries[0].RestorePending)
	require.Equal(t, models.RestorePendingKindRearmRefused, entries[0].PendingKind(),
		"the unowned journaled name routes the durable marker journal-only")
	require.Equal(t, "foreign claimant at the journaled name", string(mustRead2(t, base, backup)),
		"the claimant keeps the journaled name byte-intact")
	names := w26DirQuarNames(t, base, filepath.Dir(backup))
	require.Len(t, names, 1, "the verified bytes stay recoverable at the quarantine name")
	require.Equal(t, "original bytes", string(mustRead2(t, base, filepath.Join(filepath.Dir(backup), names[0]))))
	require.Equal(t, "original bytes", string(mustRead2(t, base, dest)),
		"the restored destination is never removed")
}

// Same wedge with the marker persist failing: the durable entry keeps its
// ARMED posture (nothing claims the name against a live arm), the
// in-process kind memory already routes this process's retries
// journal-only, and the failure only surfaces.
func TestSweepW36_ArmedLegFailedMoveBackMarkerPersistFailureKeepsArmed(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := &w32FailNthJournalTxRepo{p3OpRepo: newP3OpRepo(), failAt: 2 /* 1 = the entry-presence read tx */, err: errors.New("w36 marker persist wedged")}
	plantFs := &w36QuarMovePlantFs{Fs: base}
	op, dest, backup := w25SweepCrashOp(t, plantFs, repo.p3OpRepo, "W36B", []byte("original bytes"), "stamped")
	plantFs.plant, plantFs.bytes = backup, []byte("foreign claimant at the journaled name")

	w32ScriptRestoredDestSeam(t, true, false)

	sweeper := NewReplacementSweeper(plantFs, repo)
	healed, err := sweeper.Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	entries := w25JournalEntries(t, repo.p3OpRepo, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending,
		"the durable entry stays ARMED — the disarm never committed")
	kind, ok := sweeper.pendingRemovalKind(sweepSlash(backup))
	require.True(t, ok)
	require.Equal(t, models.RestorePendingKindRearmRefused, kind,
		"the in-process memory still routes retries journal-only")
	require.Equal(t, "foreign claimant at the journaled name", string(mustRead2(t, base, backup)))
	require.Len(t, w26DirQuarNames(t, base, filepath.Dir(backup)), 1)
	require.Equal(t, "original bytes", string(mustRead2(t, base, dest)))
}

// Already-consumed sweep leg: NO live entry exists to mark — the failed
// move-back only surfaces; the foreign claimant and the verified bytes both
// stay byte-intact, the restored destination untouched.
func TestSweepW36_AlreadyConsumedLegFailedMoveBackSurfacesOnly(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W36C/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll("/out/W36C", config.DirPerm))
	writeSweepFile(t, base, backup, "old", 1)
	row := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "W36C", OriginalPath: "/src/w36c.mkv",
		OperationType:  models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{"/out/W36C"}}),
		RevertStatus:   models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, row))
	info, err := base.Stat(backup)
	require.NoError(t, err)
	idx := &replacementLedgerIndex{journaled: map[string]*models.BatchFileOperation{sweepSlash(backup): row}}

	plantFs := &w36QuarMovePlantFs{Fs: base, plant: backup, bytes: []byte("foreign claimant at the journaled name")}
	w32ScriptRestoredDestSeam(t, true, false)

	got := NewReplacementSweeper(plantFs, repo).sweepOne(ctx, idx, "/out/W36C", info)
	require.Equal(t, 0, got, "nothing consumed, nothing healed")
	require.Equal(t, "foreign claimant at the journaled name", string(mustRead2(t, base, backup)),
		"the claimant keeps the name — no live entry authorizes anything else")
	names := w26DirQuarNames(t, base, "/out/W36C")
	require.Len(t, names, 1)
	require.Equal(t, "old", string(mustRead2(t, base, "/out/W36C/"+names[0])),
		"the verified bytes stay recoverable at the quarantine name")
	require.Equal(t, "old", string(mustRead2(t, base, dest)), "the restored destination stays")
}

// Legacy pending leg (no durable entry): the presence re-gate fails after
// the quarantine move and the move-back fails on the claimed name — surface
// only, everything byte-intact.
func TestSweepW36_LegacyPendingFailedMoveBackSurfacesOnly(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W36L/dest.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll("/out/W36L", config.DirPerm))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), config.FilePerm))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), config.FilePerm))
	row := &models.BatchFileOperation{
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{"/out/W36L"}}),
		RevertStatus:   models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, row))

	fs := &w36DestGatePlantFs{Fs: base, dest: dest, destErr: errors.New("dest lookup wedged post-quarantine"),
		plant: backup, bytes: []byte("foreign claimant at the journaled name")}
	got := NewReplacementSweeper(fs, repo).retryPendingRemoval(ctx, row.ID, backup, dest, sweepSlash(backup))
	require.False(t, got)
	require.Equal(t, "foreign claimant at the journaled name", string(mustRead2(t, base, backup)))
	names := w26DirQuarNames(t, base, "/out/W36L")
	require.Len(t, names, 1)
	require.Equal(t, "old", string(mustRead2(t, base, "/out/W36L/"+names[0])))
	require.Equal(t, "old", string(mustRead2(t, base, dest)), "the destination is untouched")
}

// Durable pending leg: the presence re-gate fails after the quarantine move
// and the move-back fails on the claimed name — the durable marker upgrades
// to the rearm-refused kind and the in-process memory agrees.
func TestSweepW36_DurablePendingFailedMoveBackUpgradesKind(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W36D/dest.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll("/out/W36D", config.DirPerm))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), config.FilePerm))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), config.FilePerm))
	op := &models.BatchFileOperation{
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{Destination: dest, Backup: backup, RestorePending: true}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	fs := &w36DestGatePlantFs{Fs: base, dest: dest, destErr: errors.New("dest lookup wedged post-quarantine"),
		plant: backup, bytes: []byte("foreign claimant at the journaled name")}
	sweeper := NewReplacementSweeper(fs, repo)
	got := sweeper.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup))
	require.False(t, got)
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.True(t, entries[0].RestorePending)
	require.Equal(t, models.RestorePendingKindRearmRefused, entries[0].PendingKind(),
		"the durable marker upgrades to the journal-only kind")
	kind, ok := sweeper.pendingRemovalKind(sweepSlash(backup))
	require.True(t, ok)
	require.Equal(t, models.RestorePendingKindRearmRefused, kind)
	require.Equal(t, "foreign claimant at the journaled name", string(mustRead2(t, base, backup)))
	require.Len(t, w26DirQuarNames(t, base, "/out/W36D"), 1)
}

// Durable pending leg with the marker persist wedged: the durable entry
// keeps its clean kind (the upgrade never committed) while the in-process
// memory still upgrades and dominates the next retry.
func TestSweepW36_DurablePendingFailedMoveBackPersistFailureKeepsCleanDurable(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := &w32FailNthJournalTxRepo{p3OpRepo: newP3OpRepo(), failAt: 2 /* 1 = the read tx */, err: errors.New("w36 marker persist wedged")}
	ctx := context.Background()
	dest := "/out/W36F/dest.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll("/out/W36F", config.DirPerm))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), config.FilePerm))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), config.FilePerm))
	op := &models.BatchFileOperation{
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{Destination: dest, Backup: backup, RestorePending: true}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	fs := &w36DestGatePlantFs{Fs: base, dest: dest, destErr: errors.New("dest lookup wedged post-quarantine"),
		plant: backup, bytes: []byte("foreign claimant at the journaled name")}
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
	require.Equal(t, "foreign claimant at the journaled name", string(mustRead2(t, base, backup)))
	require.Len(t, w26DirQuarNames(t, base, "/out/W36F"), 1)
}

// Reverter compensation leg: the destination diverges after the quarantine
// move and the move-back fails on the claimed name — the error surfaces with
// both causes, the durable entry is disarmed onto the rearm-refused pending
// kind, and the verified bytes stay recoverable at the quarantine name.
func TestRestoreReplacementJournalW36_FailedMoveBackMarksRearmRefused(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	plantFs := &w36QuarMovePlantFs{Fs: base}
	op, dest, backup := w25ArmedOp(t, plantFs, repo, "W36R", []byte("new poster"), []byte("original poster"), "stamped")
	plantFs.plant, plantFs.bytes = backup, []byte("foreign claimant at the journaled name")

	w32ScriptRestoredDestSeam(t, true, false)

	restored, err := NewReverter(plantFs, repo).restoreReplacementJournal(context.Background(), op)
	require.Error(t, err)
	require.Contains(t, err.Error(), "verified move-back failed")
	require.True(t, restored[dest], "the restored path stays protected from the delete-list cleanup")
	entries := w25JournalEntries(t, repo, op.ID)
	require.Len(t, entries, 1, "the entry is retained, never consumed")
	require.True(t, entries[0].RestorePending)
	require.Equal(t, models.RestorePendingKindRearmRefused, entries[0].PendingKind(),
		"the unowned journaled name routes the durable marker journal-only")
	require.Equal(t, "foreign claimant at the journaled name", string(mustRead2(t, base, backup)))
	names := w26DirQuarNames(t, base, filepath.Dir(backup))
	require.Len(t, names, 1)
	require.Equal(t, "original poster", string(mustRead2(t, base, filepath.Join(filepath.Dir(backup), names[0]))))
	require.Equal(t, "original poster", string(mustRead2(t, base, dest)),
		"the restored destination is never removed")
}

// Same wedge with the marker persist failing: the error still surfaces with
// both causes, but the durable entry keeps its ARMED posture (last-resort
// logged) — never marked against the unproven name.
func TestRestoreReplacementJournalW36_FailedMoveBackMarkerPersistFailureKeepsArmed(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := &w36FailFirstJournalTxRepo{p3OpRepo: newP3OpRepo(), err: errors.New("w36 marker persist wedged")}
	plantFs := &w36QuarMovePlantFs{Fs: base}
	op, dest, backup := w25ArmedOp(t, plantFs, repo.p3OpRepo, "W36P", []byte("new poster"), []byte("original poster"), "stamped")
	plantFs.plant, plantFs.bytes = backup, []byte("foreign claimant at the journaled name")

	w32ScriptRestoredDestSeam(t, true, false)

	_, err := NewReverter(plantFs, repo).restoreReplacementJournal(context.Background(), op)
	require.Error(t, err)
	require.Contains(t, err.Error(), "verified move-back failed")
	entries := w25JournalEntries(t, repo.p3OpRepo, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending,
		"the durable entry stays ARMED — the disarm never committed")
	require.Equal(t, "foreign claimant at the journaled name", string(mustRead2(t, base, backup)))
	require.Len(t, w26DirQuarNames(t, base, filepath.Dir(backup)), 1)
	require.Equal(t, "original poster", string(mustRead2(t, base, dest)))
}

// w36FailFirstJournalTxRepo fails the FIRST UpdateJournalInTx — the
// reverter's rearm-refused marker persist after the failed move-back.
type w36FailFirstJournalTxRepo struct {
	*p3OpRepo
	calls int
	err   error
}

func (r *w36FailFirstJournalTxRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	r.calls++
	if r.calls == 1 {
		return r.err
	}
	return r.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}
