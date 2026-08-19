package history

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestReplacementOwnershipCovW8_ReverterRemoveFailureRetainsAndRetries(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, err: errors.New("backup remove wedged"), fail: true}
	repo := newP3OpRepo()
	ctx := context.Background()
	f := &p3Fixture{fs: fs, repo: repo}
	op, dest := f.addAppliedOp(t, "job-1", "W8-REV", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"
	fs.victim = backup

	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Installed = true
	row.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(ctx, row))
	op.GeneratedFiles = row.GeneratedFiles

	var logs bytes.Buffer
	restoreLogOutput := logging.SetOutput(&logs)
	defer restoreLogOutput()

	r := NewReverter(fs, repo)
	restored, err := r.restoreReplacementJournal(ctx, op)
	require.Error(t, err)
	require.True(t, restored[dest])
	require.Equal(t, "old", string(mustRead2(t, fs, dest)))
	require.Equal(t, "old", string(mustRead2(t, fs, backup)))
	row, err = repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1)
	require.True(t, gf.Replacements[0].Installed, "remove failure must not weaken install confirmation")
	require.True(t, gf.Replacements[0].RestorePending)

	absoluteBackup, err := filepath.Abs(backup)
	require.NoError(t, err)
	requireLogPathContains(t, logs.String(), absoluteBackup)
	require.Contains(t, logs.String(), "backup remove wedged")

	fs.fail = false
	restored, err = r.restoreReplacementJournal(ctx, op)
	require.NoError(t, err)
	require.True(t, restored[dest])
	_, err = fs.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
	row, err = repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

func TestReplacementOwnershipCovW8_SweepRemoveFailureRetriesCleanup(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, err: errors.New("sweep remove wedged"), fail: true}
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W8-SWEEP/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	writeSweepFile(t, fs, backup, "old", 1)
	fs.victim = backup
	op := journalRow(t, repo, "job-1", "W8-SWEEP", dest, backup, 1, models.RevertStatusApplied)

	var logs bytes.Buffer
	restoreLogOutput := logging.SetOutput(&logs)
	defer restoreLogOutput()

	s := NewReplacementSweeper(fs, repo)
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	require.Equal(t, "old", string(mustRead2(t, fs, dest)))
	require.Equal(t, "old", string(mustRead2(t, fs, backup)))
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1)
	require.True(t, gf.Replacements[0].RestorePending)

	absoluteBackup, err := filepath.Abs(backup)
	require.NoError(t, err)
	requireLogPathContains(t, logs.String(), absoluteBackup)
	require.Contains(t, logs.String(), "sweep remove wedged")

	fs.fail = false
	healed, err = NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	_, err = fs.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
	row, err = repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

// TestReplacementOwnershipCovW8_RemoveNotExistRetainsThenConverges pins the
// wave-32 (codex local review round 2, PR#215 finding R4) contract for an
// ENOENT landing at the quarantine unlink itself: the bytes vanished
// unownably, so NOTHING is consumed in that round — the sweep keeps the
// entry (healed 0) and marks it restore-pending; the convergent retry then
// finds the journaled name genuinely absent and consumes on the next sweep.
func TestReplacementOwnershipCovW8_RemoveNotExistRetainsThenConverges(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, notExist: true, fail: true}
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W8-ENOENT/poster.jpg"
	backup := dest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	writeSweepFile(t, fs, backup, "old", 1)
	fs.victim = backup
	op := journalRow(t, repo, "job-1", "W8-ENOENT", dest, backup, 1, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed,
		"the vanished quarantine is indeterminate retention, not a consumed removal")
	require.Equal(t, "old", string(mustRead2(t, base, dest)), "the restore itself landed")
	_, err = base.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist, "the verified object was moved aside, then vanished")
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "the entry stays live")
	require.True(t, gf.Replacements[0].RestorePending)

	// Convergent retry: the pending leg's absent-name posture consumes.
	healed, err = NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	row, err = repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

func TestReplacementOwnershipCovW8_AlreadyConsumedRemoveFailureIsRetained(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, err: errors.New("already-consumed remove wedged"), fail: true}
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W8-RACE/poster.jpg"
	backup := dest + ".dlbak." + p3HexC
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	writeSweepFile(t, fs, backup, "old", 1)
	fs.victim = backup
	row := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "W8-RACE", OriginalPath: "/src/w8-race.mkv",
		OperationType:  models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{filepath.Dir(dest)}}),
		RevertStatus:   models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, row))
	info, err := fs.Stat(backup)
	require.NoError(t, err)
	idx := &replacementLedgerIndex{journaled: map[string]*models.BatchFileOperation{sweepSlash(backup): row}}

	got := NewReplacementSweeper(fs, repo).sweepOne(ctx, idx, filepath.Dir(dest), info)
	require.Equal(t, 0, got)
	require.Equal(t, "old", string(mustRead2(t, fs, backup)))
	_, err = fs.Stat(dest)
	require.NoError(t, err)
}

func TestReplacementOwnershipCovW8_PendingMarkerUpdateFailureStillRetries(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, err: errors.New("pending marker remove wedged"), fail: true}
	baseRepo := newP3OpRepo()
	repo := &flakySweepRepo{p3OpRepo: baseRepo, fail: true}
	ctx := context.Background()
	dest := "/out/W8-MARKER/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	writeSweepFile(t, fs, backup, "old", 1)
	fs.victim = backup
	journalRow(t, baseRepo, "job-1", "W8-MARKER", dest, backup, 1, models.RevertStatusApplied)

	s := NewReplacementSweeper(fs, repo)
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	exists, err := afero.Exists(fs, dest)
	require.NoError(t, err)
	require.False(t, exists, "failed marker persistence must undo the restore")
	require.Equal(t, "old", string(mustRead2(t, fs, backup)))

	fs.fail = false
	repo.fail = false
	healed, err = s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	_, err = fs.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestReplacementOwnershipCovW8_PendingHelperLegs(t *testing.T) {
	backup := "/out/W8-HELP/p.jpg.dlbak." + p3HexA
	row := &models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{Backup: backup, RestorePending: true}},
	})}
	require.True(t, journalEntryRestorePending(row, sweepSlash(backup)))
	require.False(t, journalEntryRestorePending(row, sweepSlash("/out/W8-HELP/other.dlbak."+p3HexA)))
	require.False(t, journalEntryRestorePending(&models.BatchFileOperation{GeneratedFiles: `{"replacements":broken`}, sweepSlash(backup)))

	gf := models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Backup: "/out/W8-HELP/other.dlbak." + p3HexB},
		{Backup: backup},
	}}
	require.True(t, markReplacementRestorePendingKind(&gf, sweepSlash(backup), models.RestorePendingKindClean))
	require.False(t, markReplacementRestorePendingKind(&gf, sweepSlash(backup), models.RestorePendingKindClean), "identical re-mark is a no-op")
	require.False(t, markReplacementRestorePendingKind(&gf, sweepSlash("/out/W8-HELP/missing.dlbak."+p3HexA), models.RestorePendingKindClean))

	repo := newP3OpRepo()
	live := &models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{Backup: backup}},
	})}
	require.NoError(t, repo.Create(context.Background(), live))
	require.NoError(t, markReplacementEntryRestorePendingKind(context.Background(), repo, live.ID, sweepSlash(backup), models.RestorePendingKindClean))
	require.NoError(t, markReplacementEntryRestorePendingKind(context.Background(), repo, live.ID, sweepSlash(backup), models.RestorePendingKindClean))
	gone := &rowGoneRepo2{p3OpRepo: repo, goneID: live.ID}
	require.Error(t, markReplacementEntryRestorePendingKind(context.Background(), gone, live.ID, sweepSlash(backup), models.RestorePendingKindClean))
	nilRepo := &w8NilRowRepo{p3OpRepo: repo}
	require.Error(t, markReplacementEntryRestorePendingKind(context.Background(), nilRepo, live.ID, sweepSlash(backup), models.RestorePendingKindClean))
	broken := *live
	broken.GeneratedFiles = `{"replacements":broken`
	require.NoError(t, repo.Update(context.Background(), &broken))
	require.Error(t, markReplacementEntryRestorePendingKind(context.Background(), repo, live.ID, sweepSlash(backup), models.RestorePendingKindClean))

	updateRepo := &failingUpdateRepo{p3OpRepo: newP3OpRepo(), updateErr: errors.New("marker update wedged")}
	updateRow := &models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{Backup: backup}},
	})}
	require.NoError(t, updateRepo.Create(context.Background(), updateRow))
	require.Error(t, markReplacementEntryRestorePendingKind(context.Background(), updateRepo, updateRow.ID, sweepSlash(backup), models.RestorePendingKindClean))

	zero := &ReplacementSweeper{}
	zero.rememberPendingRemoval("w8-zero")
	require.True(t, zero.hasPendingRemoval("w8-zero"))
}

func TestReplacementOwnershipCovW8_RearmHelperLegs(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &pathNormalizingChmodFs{Fs: base}
	dest := "/out/W8-REARM/dest.jpg"
	backup := "/out/W8-REARM/dest.jpg.dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), config.FilePerm))
	info, err := fs.Stat(dest)
	require.NoError(t, err)
	// Wave-15: each call targets a FRESH backup name — the re-arm publish is a
	// no-replace install (fsutil.PublishNoReplace), so re-arming onto an
	// already-created backup collides by design instead of clobbering it.
	require.NoError(t, rearmReplacementBackup(fs, dest, backup, nil))
	require.NoError(t, rearmReplacementBackup(fs, dest, backup+".b", info))
	require.Error(t, rearmReplacementBackup(afero.NewReadOnlyFs(base), dest, backup+".c", info))
}

func TestReplacementOwnershipCovW8_ReverterWarningLegs(t *testing.T) {
	t.Run("cleanup marker update failure", func(t *testing.T) {
		base := afero.NewMemMapFs()
		fs := &w8RemoveFs{Fs: base, err: errors.New("remove first"), fail: true}
		repo := &failingUpdateRepo{p3OpRepo: newP3OpRepo(), updateErr: errors.New("marker update failed")}
		f := &p3Fixture{fs: fs, repo: repo.p3OpRepo}
		op, dest := f.addAppliedOp(t, "job-1", "W8-MARK-ERR", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
		fs.victim = dest + ".dlbak.a"
		_, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
		require.Error(t, err)
	})

	t.Run("backup re-arm failure", func(t *testing.T) {
		base := afero.NewMemMapFs()
		fs := &w8RearmFailFs{Fs: base}
		repo := &failingUpdateRepo{p3OpRepo: newP3OpRepo(), updateErr: errors.New("consume update failed")}
		f := &p3Fixture{fs: fs, repo: repo.p3OpRepo}
		op, _ := f.addAppliedOp(t, "job-1", "W8-REARM-ERR", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
		_, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
		require.Error(t, err)
	})
}

func TestReplacementOwnershipCovW8_RetryPendingRemovalLegs(t *testing.T) {
	ctx := context.Background()
	makePending := func(t *testing.T, repo *p3OpRepo, dest, backup string, status models.RevertStatusEnum) *models.BatchFileOperation {
		t.Helper()
		raw := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{Destination: dest, Backup: backup, RestorePending: true}}})
		op := &models.BatchFileOperation{BatchJobID: "job-1", MovieID: "W8-RETRY", GeneratedFiles: raw, RevertStatus: status}
		require.NoError(t, repo.Create(ctx, op))
		return op
	}

	t.Run("owner unreadable", func(t *testing.T) {
		repo := newP3OpRepo()
		dest := "/out/W8-R1/dest.jpg"
		backup := dest + ".dlbak." + p3HexA
		op := makePending(t, repo, dest, backup, models.RevertStatusApplied)
		gone := &rowGoneRepo{p3OpRepo: repo, goneID: op.ID}
		require.False(t, NewReplacementSweeper(afero.NewMemMapFs(), gone).retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	})

	t.Run("reverted owner", func(t *testing.T) {
		repo := newP3OpRepo()
		op := makePending(t, repo, "/out/W8-R2/dest.jpg", "/out/W8-R2/dest.jpg.dlbak."+p3HexA, models.RevertStatusReverted)
		require.False(t, NewReplacementSweeper(afero.NewMemMapFs(), repo).retryPendingRemoval(ctx, op.ID, "backup", "dest", "backup"))
	})

	t.Run("malformed journal", func(t *testing.T) {
		repo := newP3OpRepo()
		op := &models.BatchFileOperation{GeneratedFiles: `{"replacements":broken`, RevertStatus: models.RevertStatusApplied}
		require.NoError(t, repo.Create(ctx, op))
		require.False(t, NewReplacementSweeper(afero.NewMemMapFs(), repo).retryPendingRemoval(ctx, op.ID, "backup", "dest", "backup"))
	})

	t.Run("already consumed removes backup", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		dest, backup := "/out/W8-R4/dest.jpg", "/out/W8-R4/dest.jpg.dlbak."+p3HexA
		require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
		require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), config.FilePerm))
		require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
		op := &models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{filepath.Dir(dest)}}), RevertStatus: models.RevertStatusApplied}
		require.NoError(t, repo.Create(ctx, op))
		require.True(t, NewReplacementSweeper(fs, repo).retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	})

	t.Run("already consumed remove failure", func(t *testing.T) {
		fs := &w8RemoveFs{Fs: afero.NewMemMapFs(), err: errors.New("retry remove failed"), fail: true}
		repo := newP3OpRepo()
		dest, backup := "/out/W8-R5/dest.jpg", "/out/W8-R5/dest.jpg.dlbak."+p3HexA
		require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
		require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), config.FilePerm))
		require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
		fs.victim = backup
		op := &models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{filepath.Dir(dest)}}), RevertStatus: models.RevertStatusApplied}
		require.NoError(t, repo.Create(ctx, op))
		require.False(t, NewReplacementSweeper(fs, repo).retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	})

	t.Run("non-pending entry is retained", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		dest, backup := "/out/W8-R6/dest.jpg", "/out/W8-R6/dest.jpg.dlbak."+p3HexA
		require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
		require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), config.FilePerm))
		require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
		op := &models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{Destination: dest, Backup: backup}}}), RevertStatus: models.RevertStatusApplied}
		require.NoError(t, repo.Create(ctx, op))
		require.False(t, NewReplacementSweeper(fs, repo).retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	})

	t.Run("retry remove failure", func(t *testing.T) {
		fs := &w8RemoveFs{Fs: afero.NewMemMapFs(), err: errors.New("retry remove wedged"), fail: true}
		repo := newP3OpRepo()
		dest, backup := "/out/W8-R7/dest.jpg", "/out/W8-R7/dest.jpg.dlbak."+p3HexA
		require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
		require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), config.FilePerm))
		require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
		fs.victim = backup
		op := makePending(t, repo, dest, backup, models.RevertStatusApplied)
		require.False(t, NewReplacementSweeper(fs, repo).retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	})

	t.Run("consumption update failure rearms", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		base := newP3OpRepo()
		repo := &flakySweepRepo{p3OpRepo: base, fail: true}
		dest, backup := "/out/W8-R8/dest.jpg", "/out/W8-R8/dest.jpg.dlbak."+p3HexA
		require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
		require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), config.FilePerm))
		require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
		op := makePending(t, base, dest, backup, models.RevertStatusApplied)
		require.False(t, NewReplacementSweeper(fs, repo).retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
		_, err := fs.Stat(backup)
		require.NoError(t, err)
	})

	t.Run("pending cleanup re-arm failure", func(t *testing.T) {
		fs := &w8RearmFailFs{Fs: afero.NewMemMapFs()}
		base := newP3OpRepo()
		repo := &flakySweepRepo{p3OpRepo: base, fail: true}
		dest, backup := "/out/W8-R9/dest.jpg", "/out/W8-R9/dest.jpg.dlbak."+p3HexA
		require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
		require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), config.FilePerm))
		require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
		op := makePending(t, base, dest, backup, models.RevertStatusApplied)
		require.False(t, NewReplacementSweeper(fs, repo).retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	})

	t.Run("initial cleanup re-arm failure", func(t *testing.T) {
		fs := &w8RearmFailFs{Fs: afero.NewMemMapFs()}
		base := newP3OpRepo()
		repo := &flakySweepRepo{p3OpRepo: base, fail: true}
		dest, backup := "/out/W8-R10/dest.jpg", "/out/W8-R10/dest.jpg.dlbak."+p3HexA
		require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
		require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
		backdate(t, fs, backup)
		journalRow(t, base, "job-1", "W8-R10", dest, backup, 1, models.RevertStatusApplied)
		healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
		require.NoError(t, err)
		require.Equal(t, 0, healed)
	})

	t.Run("keeps concurrent entries during retry", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		dest, other, backup := "/out/W8-R11/dest.jpg", "/out/W8-R11/other.jpg", "/out/W8-R11/dest.jpg.dlbak."+p3HexA
		require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
		require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), config.FilePerm))
		require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
		op := &models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
			{Destination: other, Backup: other + ".dlbak." + p3HexB},
			{Destination: dest, Backup: backup, RestorePending: true},
		}}), RevertStatus: models.RevertStatusApplied}
		require.NoError(t, repo.Create(ctx, op))
		require.True(t, NewReplacementSweeper(fs, repo).retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	})
}

type w8NilRowRepo struct{ *p3OpRepo }

// The owner-row-missing leg moved from FindByID to UpdateJournalInTx (review
// 4960250562): a missing row surfaces as ErrNotFound before fn runs.
func (r *w8NilRowRepo) UpdateJournalInTx(context.Context, uint, database.JournalUpdateFn) error {
	return fmt.Errorf("owner row missing: %w", database.ErrNotFound)
}

// pathNormalizingChmodFs compensates for afero.MemMapFs.Chmod looking up
// the unnormalized path on Windows, unlike its other path operations.
type pathNormalizingChmodFs struct{ afero.Fs }

func (f *pathNormalizingChmodFs) Chmod(name string, mode os.FileMode) error {
	return f.Fs.Chmod(filepath.FromSlash(name), mode)
}

func requireLogPathContains(t *testing.T, logs, path string) {
	t.Helper()
	normalizedLogs := filepath.ToSlash(strings.ReplaceAll(logs, `\\`, `\`))
	require.Contains(t, normalizedLogs, filepath.ToSlash(path))
}

type w8RearmFailFs struct{ afero.Fs }

// The wedge targets the re-arm's exclusively-staged copy name (wave-21:
// `<backup>.dlrarm.<hex>`), so the failure lands BEFORE any publish attempt
// — the re-arm's pre-publish failure class.
func (f *w8RearmFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.Contains(name, rearmStagingSuffix+".") {
		return nil, errors.New("re-arm temp open wedged")
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type w8RemoveFs struct {
	afero.Fs
	victim   string
	err      error
	fail     bool
	notExist bool
}

func (f *w8RemoveFs) Remove(name string) error {
	normName := strings.ReplaceAll(name, "\\", "/")
	// Wave-26: the removal gate unlinks the backup's QUARANTINE sibling
	// (victim + ".dlq." + token) rather than the journaled pathname; the
	// wedge covers both spellings. The compensation moves the quarantined
	// verified object back onto the journaled name, so the fixtures'
	// assertions (bytes still at the backup name, entry armed) hold.
	if f.fail && (normName == f.victim || strings.HasPrefix(normName, f.victim+backupQuarantineSuffix)) {
		if f.notExist {
			_ = f.Fs.Remove(name)
			return os.ErrNotExist
		}
		return f.err
	}
	return f.Fs.Remove(name)
}
