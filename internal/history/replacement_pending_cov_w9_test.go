package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestReplacementPendingCovW9_MarkerPersistFailureUndoesRestore(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, err: errors.New("backup remove wedged"), fail: true}
	baseRepo := newP3OpRepo()
	repo := &flakySweepRepo{p3OpRepo: baseRepo, fail: true}
	ctx := context.Background()
	dest := "/out/W9-MARKER/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	fs.victim = backup
	op := journalRow(t, baseRepo, "job-1", "W9-MARKER", dest, backup, 1, models.RevertStatusApplied)

	s := NewReplacementSweeper(fs, repo)
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	exists, err := afero.Exists(fs, dest)
	require.NoError(t, err)
	require.False(t, exists, "a failed marker update must undo the restore")
	require.Equal(t, "old", string(mustRead2(t, fs, backup)))
	row, err := baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1)
	require.False(t, gf.Replacements[0].RestorePending, "the failed marker update must leave the row armed")

	fs.fail = false
	repo.fail = false
	healed, err = s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "old", string(mustRead2(t, fs, dest)))
	_, err = fs.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
	row, err = baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

func TestReplacementPendingCovW9_ConcurrentSweepsSynchronizePendingState(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, err: errors.New("backup remove wedged"), fail: true}
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W9-RACE/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	fs.victim = backup
	op := journalRow(t, repo, "job-1", "W9-RACE", dest, backup, 1, models.RevertStatusApplied)
	s := NewReplacementSweeper(fs, repo)

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Sweep(ctx)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, "old", string(mustRead2(t, fs, dest)))
	require.Equal(t, "old", string(mustRead2(t, fs, backup)))
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.True(t, journalEntryRestorePending(row, sweepSlash(backup)))

	fs.fail = false
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	_, err = fs.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
	row, err = repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}

func TestReplacementPendingCovW9_RearmPreservesModeAndMtime(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &pathNormalizingChmodFs{Fs: base}
	repo := &flakySweepRepo{p3OpRepo: newP3OpRepo(), fail: true}
	ctx := context.Background()
	dest := "/out/W9-PERM/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), 0o600))
	originalMtime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, base.Chtimes(backup, originalMtime, originalMtime))
	originalInfo, err := base.Stat(backup)
	require.NoError(t, err)
	journalRow(t, repo.p3OpRepo, "job-1", "W9-PERM", dest, backup, 1, models.RevertStatusApplied)

	s := NewReplacementSweeper(fs, repo)
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "the injected consumption failure must re-arm the backup")
	_, err = base.Stat(dest)
	require.ErrorIs(t, err, os.ErrNotExist)
	rearmedInfo, err := base.Stat(backup)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), rearmedInfo.Mode().Perm())
	require.Equal(t, originalInfo.ModTime(), rearmedInfo.ModTime())

	repo.fail = false
	healed, err = s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	_, err = base.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Wave-21 (codex P1) re-pointed this wedge: the re-arm's mode application
// is the create-time Chmod on the exclusively staged `<backup>.dlrarm.<hex>`
// name inside fsutil.CreateExclusiveStagingFile — no Chmod ever targets the
// published backup path — so the failure is strictly PRE-publish and the
// backup never materializes.
func TestReplacementPendingCovW9_RearmChmodFailure(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w9ChmodFailFs{Fs: base}
	dest := "/out/W9-CHMOD/dest.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), config.DirPerm))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), config.FilePerm))
	info, err := base.Stat(dest)
	require.NoError(t, err)
	require.Error(t, rearmReplacementBackup(fs, dest, backup, info))
	require.True(t, fs.fired, "the staged-name chmod wedge fired")
	_, serr := base.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "a pre-publish mode failure publishes nothing")
}

type w9ChmodFailFs struct {
	afero.Fs
	fired bool
}

func (f *w9ChmodFailFs) Chmod(name string, mode os.FileMode) error {
	if strings.Contains(name, rearmStagingSuffix+".") {
		f.fired = true
		return errors.New("chmod wedged")
	}
	return f.Fs.Chmod(name, mode)
}

// w9MalformedSecondCallRepo serves a corrupted row on a chosen journal
// transaction call: with backup removal FAILING, call 2 is restoreAndConsume's
// marker merge; with removal succeeding, call 2 is the consumption merge.
// Covers the post-classification parse/undo legs (review 4960250562
// restructured the journal section into explicit transactions).
type w9MalformedSecondCallRepo struct {
	*p3OpRepo
	calls int
	at    int
}

func (r *w9MalformedSecondCallRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	r.calls++
	at := r.at
	if at == 0 {
		at = 2
	}
	if r.calls == at {
		_, _, err := fn(&models.BatchFileOperation{ID: id, GeneratedFiles: "{\"replacements\":broken"})
		return err
	}
	return r.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}

func TestReplacementPendingCovW9_MarkerMergeMalformedLedgerUndoesRestore(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, err: errors.New("backup remove wedged"), fail: true}
	baseRepo := newP3OpRepo()
	repo := &w9MalformedSecondCallRepo{p3OpRepo: baseRepo}
	ctx := context.Background()
	dest := "/out/W9-MALFORMED/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	fs.victim = backup
	op := journalRow(t, baseRepo, "job-1", "W9-MALFORMED", dest, backup, 1, models.RevertStatusApplied)

	s := NewReplacementSweeper(fs, repo)
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	exists, err := afero.Exists(fs, dest)
	require.NoError(t, err)
	require.False(t, exists, "an unparseable marker ledger undoes the restore")
	require.Equal(t, "old", string(mustRead2(t, fs, backup)), "backup retained for the retry")
	require.False(t, s.hasPendingRemoval(sweepSlash(backup)), "in-process fallback cleared after the undo")
	row, err := baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.False(t, gf.Replacements[0].RestorePending, "no marker persisted")
}

// The consumption merge re-reads with the same discipline: a ledger that turns
// unparseable between the presence probe and the consumption transaction
// re-arms the backup and undoes the restored destination for a clean retry.
func TestReplacementPendingCovW9_ConsumeMergeMalformedLedgerRearmsAndUndoes(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	repo := &w9MalformedSecondCallRepo{p3OpRepo: baseRepo}
	ctx := context.Background()
	dest := "/out/W9-MALCONSUME/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), config.DirPerm))
	writeSweepFile(t, base, backup, "old", time.Hour)
	journalRow(t, baseRepo, "job-1", "W9-MALCONSUME", dest, backup, 1, models.RevertStatusApplied)

	s := NewReplacementSweeper(base, repo)
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	exists, err := afero.Exists(base, dest)
	require.NoError(t, err)
	require.False(t, exists, "failed consumption undoes the restored destination")
	require.Equal(t, "old", string(mustRead2(t, base, backup)), "backup re-armed for the retry")
}

// retryPendingRemoval's consumption merge honours the same contract: the
// backup bytes come back onto the destination and the in-process pending
// fallback re-arms, so a healed row can complete the cleanup.
func TestReplacementPendingCovW9_RetryConsumeMalformedLedgerRearms(t *testing.T) {
	base := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	repo := &w9MalformedSecondCallRepo{p3OpRepo: baseRepo}
	ctx := context.Background()
	dest := "/out/W9-RETRYMAL/dest.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), config.DirPerm))
	require.NoError(t, afero.WriteFile(base, dest, []byte("new"), config.FilePerm))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), config.FilePerm))
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "W9-RETRYMAL", OriginalPath: "/src/w9-retrymal.mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
			Destination: dest, Backup: backup, DestSeq: 1, RestorePending: true,
		}}}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, baseRepo.Create(ctx, op))

	s := NewReplacementSweeper(base, repo)
	require.False(t, s.retryPendingRemoval(ctx, op.ID, backup, dest, sweepSlash(backup)))
	require.Equal(t, "new", string(mustRead2(t, base, backup)), "backup re-armed from the restored destination after the failed consume")
	require.True(t, s.hasPendingRemoval(sweepSlash(backup)), "pending fallback re-armed")
	row, err := baseRepo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.True(t, journalEntryRestorePending(row, sweepSlash(backup)), "base row untouched by the broken merge")
}

// A row already carrying the durable RestorePending marker needs no marker
// write at all when the backup removal fails again: the no-change merge keeps
// the restored destination in place (there is nothing left to undo for).
func TestReplacementPendingCovW9_AlreadyDurableMarkerNeedsNoRewrite(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := &w8RemoveFs{Fs: base, err: errors.New("backup remove wedged"), fail: true}
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W9-MARKED/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	fs.victim = backup
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "W9-MARKED", OriginalPath: "/src/w9-marked.mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
			Destination: dest, Backup: backup, DestSeq: 1, RestorePending: true,
		}}}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	s := NewReplacementSweeper(fs, repo)
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "backup removal still wedged — nothing completes")
	require.Equal(t, "old", string(mustRead2(t, fs, dest)),
		"the no-change marker merge does not undo a restore the durable marker already owns")
	require.Equal(t, "old", string(mustRead2(t, fs, backup)))
	require.True(t, s.hasPendingRemoval(sweepSlash(backup)), "in-process fallback armed for the retry")
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.True(t, journalEntryRestorePending(row, sweepSlash(backup)), "marker kept")
}
