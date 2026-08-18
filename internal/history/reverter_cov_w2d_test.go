package history

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestReverterCoverageW2D_MalformedReplacementJournal(t *testing.T) {
	r := &Reverter{fs: afero.NewMemMapFs(), batchFileOpRepo: newP3OpRepo()}
	op := &models.BatchFileOperation{ID: 101, GeneratedFiles: `{"replacements":broken`}

	restored, err := r.restoreReplacementJournal(context.Background(), op)
	require.Error(t, err)
	require.Empty(t, restored)
	require.Contains(t, err.Error(), "failed to parse replacement journal")
}

func TestReverterCoverageW2D_PhaseTwoBlocking(t *testing.T) {
	ctx := context.Background()
	dest := "/out/W2D-PHASE/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"

	op := &models.BatchFileOperation{
		ID:             201,
		GeneratedFiles: covW2DJournal(t, dest, backup, 1),
	}
	blocker := models.BatchFileOperation{
		ID:           202,
		MovieID:      "W2D-BLOCKER",
		RevertStatus: models.RevertStatusApplied,
		GeneratedFiles: covW2DJournal(t, dest,
			dest+".dlbak.fedcba9876543210", 2),
	}
	repo := &covW2DPhaseTwoRepo{p3OpRepo: newP3OpRepo(), blocker: blocker}
	r := &Reverter{fs: afero.NewMemMapFs(), batchFileOpRepo: repo}

	restored, err := r.restoreReplacementJournal(ctx, op)
	require.Error(t, err)
	var newer *NewerAppliedDestError
	require.ErrorAs(t, err, &newer)
	require.Empty(t, restored)
	require.Equal(t, 2, repo.calls, "preflight and lock-gated checks must both run")
}

func TestReverterCoverageW2D_CopyRestoreOpenError(t *testing.T) {
	inner := afero.NewMemMapFs()
	dest := "/out/W2D-OPEN/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, inner.MkdirAll(filepath.Dir(dest), config.DirPerm))
	require.NoError(t, afero.WriteFile(inner, backup, []byte("old"), config.FilePerm))

	op := &models.BatchFileOperation{
		ID:             301,
		GeneratedFiles: covW2DJournal(t, dest, backup, 1),
	}
	repo := newP3OpRepo()
	require.NoError(t, repo.Create(context.Background(), op))
	r := &Reverter{
		fs:              &stagedWedgeHistoryFs{Fs: inner},
		batchFileOpRepo: repo,
	}

	restored, err := r.restoreReplacementJournal(context.Background(), op)
	require.Error(t, err)
	require.Empty(t, restored)
	require.Contains(t, err.Error(), "failed to restore")
}

func TestReverterCoverageW2D_CopyRestoreReadError(t *testing.T) {
	inner := afero.NewMemMapFs()
	dest := "/out/W2D-READ/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, inner.MkdirAll(filepath.Dir(dest), config.DirPerm))
	require.NoError(t, afero.WriteFile(inner, dest, []byte("current"), config.FilePerm))
	require.NoError(t, afero.WriteFile(inner, backup, []byte("old"), config.FilePerm))

	fs := &covW2DReadErrorFs{Fs: inner, failPath: backup}
	err := copyRestoreBytes(fs, backup, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stage restore copy")
	require.Equal(t, "current", string(mustRead2(t, inner, dest)))
}

func TestReverterCoverageW2D_CopyRestoreCloseError(t *testing.T) {
	inner := afero.NewMemMapFs()
	dest := "/out/W2D-CLOSE/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, inner.MkdirAll(filepath.Dir(dest), config.DirPerm))
	require.NoError(t, afero.WriteFile(inner, dest, []byte("current"), config.FilePerm))
	require.NoError(t, afero.WriteFile(inner, backup, []byte("old"), config.FilePerm))

	fs := &covW2DCloseErrorFs{Fs: inner}
	err := copyRestoreBytes(fs, backup, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stage restore close")
	require.Equal(t, "current", string(mustRead2(t, inner, dest)))
}

func TestReverterCoverageW2D_SweepJournaledDestinations(t *testing.T) {
	ctx := context.Background()

	t.Run("nil sweeper", func(t *testing.T) {
		(&Reverter{}).sweepJournaledDestinations(ctx, nil)
	})

	t.Run("malformed ledger is skipped", func(t *testing.T) {
		r := &Reverter{sweeper: &ReplacementSweeper{}}
		r.sweepJournaledDestinations(ctx, []models.BatchFileOperation{
			{GeneratedFiles: `{"replacements":broken`},
		})
	})

	t.Run("sweeper error is best effort", func(t *testing.T) {
		broken := &covW2DErrorIndexRepo{p3OpRepo: newP3OpRepo(), err: errors.New("index unavailable")}
		r := &Reverter{
			sweeper: NewReplacementSweeper(afero.NewMemMapFs(), broken),
		}
		r.sweepJournaledDestinations(ctx, []models.BatchFileOperation{{
			GeneratedFiles: covW2DJournal(t,
				"/out/W2D-SWEEP/poster.jpg",
				"/out/W2D-SWEEP/poster.jpg.dlbak.0123456789abcdef", 1),
		}})
	})
}

func TestReverterCoverageW2D_CheckDestBlockingMalformedRow(t *testing.T) {
	ctx := context.Background()
	dest := "/out/W2D-MALFORMED/poster.jpg"
	repo := &covW2DMalformedDestinationRepo{
		p3OpRepo: newP3OpRepo(),
		rows: []models.BatchFileOperation{{
			ID:             402,
			MovieID:        "W2D-MALFORMED",
			RevertStatus:   models.RevertStatusApplied,
			GeneratedFiles: `{"replacements":broken`,
		}},
	}
	r := &Reverter{batchFileOpRepo: repo}
	self := &models.BatchFileOperation{ID: 401, RevertStatus: models.RevertStatusApplied}

	require.NoError(t, r.checkDestBlocking(ctx, self, dest, 1))
}

func TestReverterCoverageW2D_MaxJournalSeqMalformed(t *testing.T) {
	op := &models.BatchFileOperation{GeneratedFiles: `{"replacements":broken`}
	require.Zero(t, maxJournalSeq(op))
}

func TestReverterCoverageW2D_OperationsRetrySystemError(t *testing.T) {
	ctx := context.Background()
	op := models.BatchFileOperation{ID: 501, MovieID: "W2D-RETRY"}
	calls := 0

	outcomes := (&Reverter{}).revertOperations(ctx, []models.BatchFileOperation{op},
		func(_ context.Context, got *models.BatchFileOperation) (*RevertFileResult, error) {
			calls++
			if calls == 1 {
				return (&RevertFileResult{OperationID: got.ID}).withRetryable(&NewerAppliedDestError{}), nil
			}
			return nil, errors.New("retry system failure")
		})

	require.Equal(t, 2, calls)
	require.Len(t, outcomes, 1)
	require.Equal(t, models.RevertOutcomeFailed, outcomes[0].Outcome)
	require.Equal(t, "retry system failure", outcomes[0].Error)
}

func TestReverterCoverageW2D_StartupSweepError(t *testing.T) {
	broken := &covW2DErrorIndexRepo{p3OpRepo: newP3OpRepo(), err: errors.New("startup index unavailable")}
	SweepOnStartup(afero.NewMemMapFs(), broken)
}

func covW2DJournal(t *testing.T, dest, backup string, seq int64) string {
	t.Helper()
	raw, err := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
		Destination: dest,
		Backup:      backup,
		DestSeq:     seq,
	}}})
	require.NoError(t, err)
	return string(raw)
}

type covW2DPhaseTwoRepo struct {
	*p3OpRepo
	blocker models.BatchFileOperation
	calls   int
}

func (r *covW2DPhaseTwoRepo) FindOperationsByDestination(context.Context, string) ([]models.BatchFileOperation, error) {
	r.calls++
	if r.calls == 1 {
		return nil, nil
	}
	return []models.BatchFileOperation{r.blocker}, nil
}

type covW2DErrorIndexRepo struct {
	*p3OpRepo
	err error
}

func (r *covW2DErrorIndexRepo) FindOperationsWithReplacements(context.Context) ([]models.BatchFileOperation, error) {
	return nil, r.err
}

type covW2DMalformedDestinationRepo struct {
	*p3OpRepo
	rows []models.BatchFileOperation
}

func (r *covW2DMalformedDestinationRepo) FindOperationsByDestination(context.Context, string) ([]models.BatchFileOperation, error) {
	return r.rows, nil
}

type covW2DReadErrorFs struct {
	afero.Fs
	failPath string
}

func (f *covW2DReadErrorFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil || name != f.failPath {
		return file, err
	}
	return &covW2DReadErrorFile{File: file}, nil
}

type covW2DReadErrorFile struct{ afero.File }

func (f *covW2DReadErrorFile) Read([]byte) (int, error) {
	return 0, errors.New("synthetic source read failure")
}

type covW2DCloseErrorFs struct{ afero.Fs }

func (f *covW2DCloseErrorFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil || !strings.Contains(name, ".rstr.") {
		return file, err
	}
	return &covW2DCloseErrorFile{File: file}, nil
}

type covW2DCloseErrorFile struct{ afero.File }

func (f *covW2DCloseErrorFile) Close() error {
	_ = f.File.Close()
	return errors.New("synthetic staged close failure")
}
