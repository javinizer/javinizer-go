package worker

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestFencedSkipRetryFailureSurvivesRestartWithoutRevisingDomain(t *testing.T) {
	f := newSkippedFenceFixture(t)
	completed := f.store.CloneResults()[skippedFencePath]
	completed.Status = models.JobStatusCompleted
	completed.Error = ""
	completed.ErrorCode = ""
	completed.VideoCollision = nil
	f.store.UpdateFileResult(skippedFencePath, completed)
	admitted, err := f.store.GetMovieResult(skippedFencePath)
	require.NoError(t, err)
	f.afc.MovieResult = admitted
	job := newBatchJob([]string{skippedFencePath})
	job.results = f.store
	f.inputs.RecordApplyRetryFailure = job.recordApplyRetryFailure
	fs, dir := witnessFixture(t)
	witness := promoteWitness{PosterID: f.movie.ID, URL: f.movie.Poster.PosterURL, ResultID: admitted.ResultID, PrevRevision: admitted.Revision, OldSHA: map[string]string{"crop": shaContentHex([]byte("old-bytes"))}}
	payload, err := json.Marshal(witness)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-"+f.movie.ID+".json"), payload, 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, f.movie.ID+".jpg"), []byte("uncommitted-refresh"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, f.movie.ID+".jpg.bak"), []byte("old-bytes"), 0o644))
	editor := newEditorForStore(f.store)
	editor.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-W1"})
	f.inputs.PromoteWitnessFn = editor.hasUnresolvedPromoteWitness
	f.inputs.EditLockFn = func(ids ...string) func() { return editor.lockRegistry().AcquireMany(ids) }
	before := f.store.CloneResults()
	provenance := f.store.GetProvenance(skippedFencePath)
	outcome := f.run(t)
	f.assertFenced(t, outcome, before, provenance)
	require.Equal(t, models.JobStatusFailed, job.GetStatus().Results[skippedFencePath].Status)
	require.Equal(t, before, job.results.CloneResults())
	durableRepo := &retryLedgerRepo{mockJobRepoForPersist: &mockJobRepoForPersist{}}
	require.NoError(t, persistToDatabase(durableRepo, job))
	row := durableRepo.saved
	require.NotNil(t, row)
	decoded, errs := jobpersist.Decode(row)
	require.Empty(t, errs)
	require.Equal(t, before, decoded.Results, "persist must retain raw witness revision and domain")
	require.Contains(t, decoded.ApplyRetryFailures, skippedFencePath)
	store := &JobStore{fs: afero.NewMemMapFs()}
	restored := store.reconstructBatchJob(row)
	status := restored.GetStatus()
	require.Equal(t, models.JobStatusFailed, status.Results[skippedFencePath].Status)
	require.Equal(t, "APPLY_RECEIPT_PERSIST_FAILED", status.Results[skippedFencePath].ErrorCode)
	inputs := restored.controller.buildApplyInputs(nil, BatchJobConfig{}, ApplyPhaseConfig{}, nil)
	require.Equal(t, models.JobStatusFailed, inputs.Results[skippedFencePath].Status)
	require.EqualValues(t, 1, countRemainingApplyFailures(inputs, nil), "subset retries retain unselected receipt failures")
	require.False(t, retryWritebackClearedFailure(inputs, applyFileOutcome{FilePath: skippedFencePath, Success: true}))
	// Recovery must inspect raw domain, not the projected failure, and restore the backup.
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(row, nil)
	cleaner := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cleaner.ReconcileRekeyWitnesses(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	content, err := afero.ReadFile(fs, filepath.Join(dir, f.movie.ID+".jpg"))
	require.NoError(t, err)
	require.Equal(t, "old-bytes", string(content))
	require.False(t, editor.hasUnresolvedPromoteWitness(f.movie.ID))
	require.Equal(t, models.JobStatusFailed, restored.GetStatus().Results[skippedFencePath].Status)
	// After witness reconciliation, only an actual committed receipt supersedes the ledger.
	f.inputs = inputs
	retry := f.run(t)
	require.True(t, retry.Success)
	require.False(t, retry.Failed)
	require.Equal(t, "skipped", restored.GetStatus().Results[skippedFencePath].VideoOutcome)
	require.True(t, retryWritebackClearedFailure(inputs, applyFileOutcome{FilePath: skippedFencePath, Success: true}))
	require.EqualValues(t, 0, countRemainingApplyFailures(inputs, []applyFileOutcome{{FilePath: skippedFencePath, Success: true}}))
}

type retryLedgerRepo struct {
	*mockJobRepoForPersist
	saved   *models.Job
	failure error
}

func (r *retryLedgerRepo) CommitEnvelope(_ context.Context, row *models.Job, generation uint64) (uint64, error) {
	if r.failure != nil {
		return generation, r.failure
	}
	copy := *row
	r.saved = &copy
	return generation + 1, nil
}

func TestApplyRetryFailurePersistenceErrorsAndConcurrentSnapshots(t *testing.T) {
	job := newBatchJob([]string{"a"})
	job.recordApplyRetryFailure("a", nil, "ignored")
	job.recordApplyRetryFailure("a", &resultstore.MovieResult{}, "ignored")
	require.Empty(t, job.applyRetryFailures)
	job.results.UpdateFileResult("a", &resultstore.MovieResult{ResultID: "retry-r1", Movie: &models.Movie{ID: "ABC-123"}, Status: models.JobStatusCompleted})
	admitted, err := job.results.GetMovieResult("a")
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 30 {
				job.recordApplyRetryFailure("a", admitted, "fenced")
				_ = job.GetStatus()
				_ = job.applyRetryFailurePending("a", admitted)
				_, _ = snapshotForPersist(job)
			}
		}()
	}
	wg.Wait()
	repo := &retryLedgerRepo{mockJobRepoForPersist: &mockJobRepoForPersist{}}
	require.NoError(t, persistToDatabase(repo, job))
	require.NotNil(t, repo.saved)
	loaded, errs := jobpersist.Decode(repo.saved)
	require.Empty(t, errs)
	require.Contains(t, loaded.ApplyRetryFailures, "a")
	require.Equal(t, models.JobStatusCompleted, loaded.Results["a"].Status)
	repo.failure = errors.New("disk full")
	require.ErrorContains(t, persistToDatabase(repo, job), "disk full")
	require.Contains(t, job.GetStatus().PersistError, "disk full")
	require.Equal(t, models.JobStatusFailed, job.GetStatus().Results["a"].Status)
}
