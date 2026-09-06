package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression pins for the #244 CLI batch-runtime wiring: the factory's
// persistent standalone path must register the job on a real JobStore (jobs
// row written to the DB), while factories without a store keep the in-memory
// behavior TUI/tests rely on.
func TestBatchJobFactory_CreatePersistentStandaloneJob_PersistsJobsRow(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	tempDir := t.TempDir()

	store := NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo, tempDir, nil, nil,
		WithHistoryRepo(repos.HistoryRepo),
		WithSkipStartupRecovery(),
	)

	jobID := "cli-batch-" + models.NewJobID().String()
	factory := NewBatchJobFactory(store, nil, nil, nil, BatchJobConfig{}, nil)
	job := factory.CreatePersistentStandaloneJob([]string{"/src/GOOD-700.mp4"}, BatchJobOptions{
		ID:          jobID,
		Destination: "/dest",
	})
	require.NotNil(t, job)
	assert.Equal(t, jobID, job.GetID())

	// The jobs row is written at creation (the CLI batch becomes queryable via
	// `javinizer history list --batch <id>` and participates in
	// batch_file_operations FK scoping).
	row, err := repos.JobRepo.FindByID(context.Background(), jobID)
	require.NoError(t, err, "jobs row must persist at creation")
	require.NotNil(t, row)
	assert.Equal(t, jobID, row.ID)
	assert.Equal(t, "/dest", row.Destination)

	// HistoryRepo is wired through the store fallback so worker audit rows
	// persist for CLI batches too.
	store.mu.RLock()
	raw, ok := store.jobs[models.JobID(jobID)]
	store.mu.RUnlock()
	require.True(t, ok)
	raw.mu.RLock()
	hist := raw.deps.HistoryRepo
	raw.mu.RUnlock()
	assert.Equal(t, repos.HistoryRepo, hist, "store fallback wires HistoryRepo into the created job")
}

func TestBatchJobFactory_CreatePersistentStandaloneJob_NilStoreFallsBackToStandalone(t *testing.T) {
	factory := NewBatchJobFactory(nil, nil, nil, nil, BatchJobConfig{}, nil)
	job := factory.CreatePersistentStandaloneJob([]string{"/src/GOOD-700.mp4"}, BatchJobOptions{})
	require.NotNil(t, job, "nil store must fall back to the in-memory standalone path")
	assert.NotEmpty(t, job.GetID())
}

// TestNewJobStore_WithSkipStartupRecovery pins the one-shot CLI store
// behavior (#244): a CLI store sharing a database with a live server must
// neither reconstruct that server's jobs nor mark its in-flight jobs failed —
// while the default (server-boot) construction keeps recovering orphans.
func TestNewJobStore_WithSkipStartupRecovery(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	tempDir := t.TempDir()
	ctx := context.Background()

	// Seed a jobs row stuck in 'running' (a possibly-live server's in-flight
	// batch sharing the database).
	seed := NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo, tempDir, nil, nil, WithSkipStartupRecovery())
	seeded := seed.CreateJobBatch([]string{"/src/GOOD-700.mp4"})
	seeded.lifecycle.mu.Lock()
	seeded.lifecycle.Status = models.JobStatusRunning
	seeded.lifecycle.mu.Unlock()
	require.NoError(t, seed.PersistJob(seeded))

	// Skipped recovery: no reconstruction, no orphan marking.
	skipped := NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo, tempDir, nil, nil, WithSkipStartupRecovery())
	_, found := skipped.GetJob(seeded.ID.String())
	assert.False(t, found, "skipped recovery must not reconstruct prior jobs")
	row, err := repos.JobRepo.FindByID(ctx, seeded.ID.String())
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusRunning, row.Status, "skipped recovery must not mark a possibly-live job failed")

	// Default construction (parity control) keeps the server-boot semantics:
	// the orphaned running job is reconstructed and marked failed.
	full := NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo, tempDir, nil, nil)
	snap, found := full.GetJob(seeded.ID.String())
	require.True(t, found, "default construction must reconstruct prior jobs")
	require.NotNil(t, snap)
	assert.Equal(t, models.JobStatusFailed, snap.Status)
	row, err = repos.JobRepo.FindByID(ctx, seeded.ID.String())
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusFailed, row.Status)
}

func TestBatchJobFactory_CreatePersistentStandaloneJob_InMemoryStore(t *testing.T) {
	// A *JobStore with no-op persistence (the in-memory constructor) is still a
	// real store: the job registers on it and survives GetJob, while nothing
	// database-side exists to assert against.
	memStore := NewInMemoryJobStore()
	factory := NewBatchJobFactory(memStore, nil, nil, nil, BatchJobConfig{}, nil)
	job := factory.CreatePersistentStandaloneJob([]string{"/src/GOOD-700.mp4"}, BatchJobOptions{})
	require.NotNil(t, job)
	status, ok := memStore.GetJob(job.GetID())
	require.True(t, ok, "job must be registered on the in-memory store")
	require.NotNil(t, status)
}
