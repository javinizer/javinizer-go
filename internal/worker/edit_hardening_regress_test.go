package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// TestReconstructedJob_EditsUseCompositeTx — regression for the restart
// blind spot (D13): a job reconstructed from the DB must have the edit env
// attached (composite tx + envelope persist), not silently fall back to the
// legacy publish-then-persist path with no envelope write at all.
func TestReconstructedJob_EditsUseCompositeTx(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	ctx := context.Background()

	storeA := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil, WithEditTransactor(db))
	job := seedOneMovie(t, storeA, "/v/a.mp4", "RECON-001")
	require.NoError(t, storeA.PersistJobByID(job.ID.String()))

	// Restart boundary: a fresh store reconstructs jobs from the DB.
	storeB := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil, WithEditTransactor(db))
	jobB, ok := storeB.GetBatchJob(job.ID.String())
	require.True(t, ok, "restarted store must reconstruct the job")

	require.NoError(t, jobB.UpdateMovieFamily(ctx, "RECON-001", "", &models.Movie{ID: "RECON-001", Title: "post-restart edit"}, FamilySaveOptions{}))

	// Envelope + movie row both committed (composite tx path was live;
	// the legacy fallback would have silently skipped the movie row write is
	// still possible — but the ENVELOPE must carry the edit, which is what
	// the handler-level persist removal relies on).
	row, err := repos.JobRepo.FindByID(ctx, job.ID.String())
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Contains(t, row.Results, "post-restart edit")

	// Rollback exercise on the reconstructed job: encode failure mid-tx rolls
	// back BOTH legs and publishes nothing.
	orig := jobpersist.MarshalFn
	jobpersist.MarshalFn = func(v any) ([]byte, error) { return nil, errors.New("forced") }
	err = jobB.UpdateMovieFamily(ctx, "RECON-001", "", &models.Movie{ID: "RECON-001", Title: "never lands"}, FamilySaveOptions{})
	jobpersist.MarshalFn = orig
	require.Error(t, err)
	row2, err := repos.JobRepo.FindByID(ctx, job.ID.String())
	require.NoError(t, err)
	assert.NotContains(t, row2.Results, "never lands")
}

// TestDeleteJob_RunningJobFastFails — regression for the exclusive-drain
// blocking issue: DeleteJob on a Running (phase-leased) job must return
// "cannot delete running job" nearly immediately, not drain-wait the whole
// phase. Uses NO phase lease (Status set directly) — the slow predecessor
// shape is covered by TestDeleteJob_WaitsForActiveEdits_DrainBarrier.
func TestDeleteJob_RunningJobFastFails(t *testing.T) {
	store := NewInMemoryJobStore()
	job := seedOneMovie(t, store, "/v/a.mp4", "RUN-001")
	job.lifecycle.mu.Lock()
	job.lifecycle.Status = models.JobStatusRunning
	job.lifecycle.currentPhase = string(JobPhaseScrape)
	job.lifecycle.mu.Unlock()

	start := time.Now()
	err := store.DeleteJob(job.ID.String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot delete running job")
	assert.Less(t, time.Since(start), 500*time.Millisecond, "delete on Running must fail fast, not drain the phase")
}

// TestExcludeAll_AutoCancelStatusPersists — regression for the stale-status
// envelope defect: excluding the last movie auto-cancels the job; the
// durable row must carry the cancelled status (otherwise a restart flips
// pending→failed via recoverOrphanedJobs).
func TestExcludeAll_AutoCancelStatusPersists(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	store := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil, WithEditTransactor(db))
	job := store.CreateJobBatch([]string{"/v/only.mp4"})
	job.results.UpdateFileResult("/v/only.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/v/only.mp4", MovieID: "X-1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "X-1"},
	})
	// Pending job: exclusion auto-cancel applies.
	require.NoError(t, job.posterEditor.ExcludeMovieFamily(context.Background(), "X-1"))
	assert.Equal(t, models.JobStatusCancelled, job.Lifecycle().GetJobStatus())

	row, err := repos.JobRepo.FindByID(context.Background(), job.ID.String())
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, models.JobStatusCancelled, row.Status, "durable row must carry the auto-cancel status")
}

// TestRenameLeg_RollbackKeepsOriginalName — the actres rename leg rolls back
// WITH the tx: a forced envelope failure after the renames must leave the
// actress row name unchanged.
func TestRenameLeg_RollbackKeepsOriginalName(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	seeded := seedNamedActress(t, repos.ActressRepo, "Yui", "", "波多野結衣")

	store := NewJobStore(repos.JobRepo, nil, repos.MovieRepo, "", nil, nil,
		WithActressRepo(repos.ActressRepo), WithEditTransactor(db))
	job := seedOneMovie(t, store, "/v/a.mp4", "RN-001")

	orig := jobpersist.MarshalFn
	jobpersist.MarshalFn = func(v any) ([]byte, error) { return nil, errors.New("forced envelope failure") }
	err := job.posterEditor.UpdateMovieFamily(context.Background(), "RN-001", "", &models.Movie{
		ID:        "RN-001",
		Title:     "X",
		Actresses: []models.Actress{{ID: seeded.ID, FirstName: "Yui-Edited", JapaneseName: "波多野結衣"}},
	}, FamilySaveOptions{})
	jobpersist.MarshalFn = orig
	require.Error(t, err)

	got, err := repos.ActressRepo.FindByID(context.Background(), seeded.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Yui", got.FirstName, "rename must roll back with the composite tx")
}
