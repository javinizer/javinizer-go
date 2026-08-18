package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestReplacementSweepCoverage_JournalInstalledPredicate(t *testing.T) {
	backup := "/out/COV/p.jpg.dlbak." + p3HexA
	row := &models.BatchFileOperation{GeneratedFiles: `{"replacements":[{"destination":"/out/COV/p.jpg","backup":"/out/COV/p.jpg.dlbak.0123456789abcdef","installed":true}]}`}

	require.True(t, journalEntryInstalled(row, sweepSlash(backup)))
	require.False(t, journalEntryInstalled(row, sweepSlash("/out/COV/other.jpg.dlbak."+p3HexA)))
	require.False(t, journalEntryInstalled(&models.BatchFileOperation{GeneratedFiles: `{"replacements":broken`}, sweepSlash(backup)))
}

func TestReplacementSweepCoverage_TargetedPrefixSplit(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/COV-TGT"
	dest := dir + "/poster.jpg"
	targetBackup := dest + ".dlbak." + p3HexA
	siblingBackup := dir + "/fanart.jpg.dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, targetBackup, "target", time.Hour)
	writeSweepFile(t, fs, siblingBackup, "sibling", time.Hour)
	covCreateRootRow(t, repo, dir)

	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{dest})
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "target", string(mustRead2(t, fs, dest)))
	exists, err := afero.Exists(fs, targetBackup)
	require.NoError(t, err)
	require.True(t, exists, "restored unjournaled marker remains available for manual ownership review")
	exists, err = afero.Exists(fs, siblingBackup)
	require.NoError(t, err)
	require.True(t, exists, "a marker backup for a different named destination is skipped")
}

func TestReplacementSweepCoverage_OrphanOwnershipAnswerError(t *testing.T) {
	fs := afero.NewMemMapFs()
	base := newP3OpRepo()
	repo := &covDestinationErrorRepo{p3OpRepo: base, err: errors.New("ownership query failed")}
	ctx := context.Background()
	dir := "/out/COV-OWN"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, dest, "current", time.Hour)
	writeSweepFile(t, fs, backup, "old", time.Hour)
	covCreateRootRow(t, base, dir)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	exists, err := afero.Exists(fs, backup)
	require.NoError(t, err)
	require.True(t, exists, "an unreadable ownership answer keeps the orphan")
}

func TestReplacementSweepCoverage_OrphanFreshLedgerParseAndSuccess(t *testing.T) {
	fs := afero.NewMemMapFs()
	base := newP3OpRepo()
	repo := &covFreshRowsRepo{
		p3OpRepo: base,
		fresh: []models.BatchFileOperation{
			{GeneratedFiles: `{"replacements":broken`},
			{GeneratedFiles: `{"roots":["/out/not-this-dir"]}`},
		},
	}
	ctx := context.Background()
	dir := "/out/COV-FRESH"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, dest, "current", time.Hour)
	writeSweepFile(t, fs, backup, "old", time.Hour)
	covCreateRootRow(t, base, dir)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	require.Equal(t, "current", string(mustRead2(t, fs, dest)))
	exists, err := afero.Exists(fs, backup)
	require.NoError(t, err)
	require.True(t, exists, "an unjournaled marker-shaped file is retained")
}

func TestReplacementSweepCoverage_OrphanRestoreCopyError(t *testing.T) {
	inner := afero.NewMemMapFs()
	fs := &stagedWedgeHistoryFs{Fs: inner}
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/COV-ORPHAN-ERR"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	covCreateRootRow(t, repo, dir)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	exists, err := afero.Exists(fs, backup)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestReplacementSweepCoverage_RevertedOwnerIsRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/COV-REVERTED/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll("/out/COV-REVERTED", 0o755))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	journalRow(t, repo, "job-1", "COV-REVERTED", dest, backup, 1, models.RevertStatusReverted)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	exists, err := afero.Exists(fs, backup)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestReplacementSweepCoverage_JournalRestoreCopyError(t *testing.T) {
	inner := afero.NewMemMapFs()
	fs := &stagedWedgeHistoryFs{Fs: inner}
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/COV-JOURNAL-ERR/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll("/out/COV-JOURNAL-ERR", 0o755))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	journalRow(t, repo, "job-1", "COV-JOURNAL-ERR", dest, backup, 1, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	exists, err := afero.Exists(fs, backup)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestReplacementSweepCoverage_LiveLedgerParseErrorUndoesRestore(t *testing.T) {
	fs := afero.NewMemMapFs()
	base := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/COV-LIVE-PARSE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	op := journalRow(t, base, "job-1", "COV-LIVE-PARSE", dest, backup, 1, models.RevertStatusApplied)
	malformed := *op
	malformed.GeneratedFiles = `{"replacements":broken`
	repo := &covFindByIDRepo{
		p3OpRepo: base,
		results:  []covFindByIDResult{{row: op}, {row: &malformed}},
	}
	idx := &replacementLedgerIndex{journaled: map[string]*models.BatchFileOperation{sweepSlash(backup): op}}
	info, err := fs.Stat(backup)
	require.NoError(t, err)

	got := NewReplacementSweeper(fs, repo).sweepOne(ctx, idx, dir, info)
	require.Equal(t, 0, got)
	exists, err := afero.Exists(fs, dest)
	require.NoError(t, err)
	require.False(t, exists, "a malformed live ledger undoes the staged restore")
	exists, err = afero.Exists(fs, backup)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestReplacementSweepCoverage_LiveRowGoneUndoRemoveError(t *testing.T) {
	inner := afero.NewMemMapFs()
	ctx := context.Background()
	dir := "/out/COV-LIVE-GONE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	fs := &removeFailFs{Fs: inner, victim: dest}
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	base := newP3OpRepo()
	op := journalRow(t, base, "job-1", "COV-LIVE-GONE", dest, backup, 1, models.RevertStatusApplied)
	repo := &covFindByIDRepo{
		p3OpRepo: base,
		results:  []covFindByIDResult{{row: op}, {err: errors.New("live row disappeared")}},
	}
	idx := &replacementLedgerIndex{journaled: map[string]*models.BatchFileOperation{sweepSlash(backup): op}}
	info, err := fs.Stat(backup)
	require.NoError(t, err)

	got := NewReplacementSweeper(fs, repo).sweepOne(ctx, idx, dir, info)
	require.Equal(t, 0, got)
	exists, err := afero.Exists(fs, dest)
	require.NoError(t, err)
	require.True(t, exists, "the injected undo failure leaves the restored destination")
	exists, err = afero.Exists(fs, backup)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestReplacementSweepCoverage_AlreadyConsumedEntry(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/COV-CONSUMED"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	raw, err := json.Marshal(models.GeneratedFilesJSON{Roots: []string{dir}})
	require.NoError(t, err)
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "COV-CONSUMED", OriginalPath: "/src/cov-consumed.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw), RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))
	idx := &replacementLedgerIndex{journaled: map[string]*models.BatchFileOperation{sweepSlash(backup): &models.BatchFileOperation{ID: op.ID}}}
	info, err := fs.Stat(backup)
	require.NoError(t, err)

	got := NewReplacementSweeper(fs, repo).sweepOne(ctx, idx, dir, info)
	require.Equal(t, 1, got)
	require.Equal(t, "old", string(mustRead2(t, fs, dest)))
	exists, err := afero.Exists(fs, backup)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestReplacementSweepCoverage_ConsumptionUndoRemoveError(t *testing.T) {
	inner := afero.NewMemMapFs()
	ctx := context.Background()
	dir := "/out/COV-CONSUME-ERR"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	fs := &removeFailFs{Fs: inner, victim: dest}
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	base := newP3OpRepo()
	journalRow(t, base, "job-1", "COV-CONSUME-ERR", dest, backup, 1, models.RevertStatusApplied)
	flaky := &flakySweepRepo{p3OpRepo: base, fail: true}

	healed, err := NewReplacementSweeper(fs, flaky).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	exists, err := afero.Exists(fs, dest)
	require.NoError(t, err)
	require.True(t, exists)
	exists, err = afero.Exists(fs, backup)
	require.NoError(t, err)
	require.True(t, exists)
}

func covCreateRootRow(t *testing.T, repo *p3OpRepo, dir string) {
	t.Helper()
	raw, err := json.Marshal(models.GeneratedFilesJSON{Roots: []string{dir}})
	require.NoError(t, err)
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "COV-ROOT-" + dir,
		OriginalPath: "/src/cov-root.mkv", OperationType: models.OperationTypeUpdate,
		GeneratedFiles: string(raw), RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
}

type covDestinationErrorRepo struct {
	*p3OpRepo
	err error
}

func (r *covDestinationErrorRepo) FindOperationsByDestination(context.Context, string) ([]models.BatchFileOperation, error) {
	return nil, r.err
}

type covFreshRowsRepo struct {
	*p3OpRepo
	fresh []models.BatchFileOperation
}

func (r *covFreshRowsRepo) FindOperationsByDestination(context.Context, string) ([]models.BatchFileOperation, error) {
	return r.fresh, nil
}

type covFindByIDResult struct {
	row *models.BatchFileOperation
	err error
}

type covFindByIDRepo struct {
	*p3OpRepo
	results []covFindByIDResult
	calls   int
}

func (r *covFindByIDRepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	if r.calls < len(r.results) {
		result := r.results[r.calls]
		r.calls++
		if result.row == nil {
			return nil, result.err
		}
		copy := *result.row
		return &copy, result.err
	}
	r.calls++
	return r.p3OpRepo.FindByID(ctx, id)
}

// UpdateJournalInTx continues the SAME sequenced injection at the journal
// transaction seam (review 4960250562): the sweeper's live-row re-read under
// the journal lock now rides UpdateJournalInTx, so the next scripted result
// plays the role the second FindByID call used to play.
func (r *covFindByIDRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	if r.calls < len(r.results) {
		result := r.results[r.calls]
		r.calls++
		if result.row == nil {
			if result.err != nil {
				return result.err
			}
			return fmt.Errorf("update journal tx row %d: %w", id, database.ErrNotFound)
		}
		copy := *result.row
		next, persist, err := fn(&copy)
		if err != nil {
			return err
		}
		if persist {
			copy.GeneratedFiles = models.MarshalLedgerJSON(next)
			return r.p3OpRepo.Update(ctx, &copy)
		}
		return nil
	}
	r.calls++
	return r.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}
