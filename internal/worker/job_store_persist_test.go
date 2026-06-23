package worker

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockJobRepoForPersist tracks Upsert calls for testing persistFn.
// Both upsertCalled and lastJobID are concurrency-safe.
type mockJobRepoForPersist struct {
	upsertCalled atomic.Int32
	mu           sync.Mutex
	lastJobID    string
}

func (m *mockJobRepoForPersist) Upsert(_ context.Context, job *models.Job) error {
	m.upsertCalled.Add(1)
	m.mu.Lock()
	m.lastJobID = job.ID
	m.mu.Unlock()
	return nil
}

func (m *mockJobRepoForPersist) getLastJobID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastJobID
}

func (m *mockJobRepoForPersist) Create(_ context.Context, job *models.Job) error {
	return nil
}

func (m *mockJobRepoForPersist) FindByID(_ context.Context, id string) (*models.Job, error) {
	return nil, nil
}

func (m *mockJobRepoForPersist) List(_ context.Context) ([]models.Job, error) {
	return nil, nil
}

func (m *mockJobRepoForPersist) Delete(_ context.Context, id string) error {
	return nil
}

func (m *mockJobRepoForPersist) DeleteOrganizedOlderThan(_ context.Context, _ time.Time) error {
	return nil
}

func (m *mockJobRepoForPersist) Update(_ context.Context, job *models.Job) error {
	return nil
}

// TestReconstructBatchJob_PersistFn tests that reconstructBatchJob re-attaches
// persistFn so that PersistJobByID on reconstructed jobs persists results
// instead of silently discarding them.
func TestReconstructBatchJob_PersistFn(t *testing.T) {
	t.Parallel()

	mockRepo := &mockJobRepoForPersist{}
	jq := NewJobStore(mockRepo, nil, nil, t.TempDir(), nil, nil)

	// Create and store a job via the JobStore so it goes through the normal path
	jobCfg := &JobConfig{
		ID: "test-persist-job",
		BatchJobDeps: BatchJobDeps{
			WF: &stubRescrapeWF{},
			BatchCfg: BatchJobConfig{
				MaxWorkers:    2,
				WorkerTimeout: 30 * time.Second,
			},
		},
	}
	job := jq.CreateJobBatch([]string{"/path/file1.mp4"}, jobCfg)
	require.NotNil(t, job)
	require.NotNil(t, job.deps.PersistFn, "freshly created job should have persistFn")

	// Reset counter (CreateJob already called Upsert once)
	mockRepo.upsertCalled.Store(0)

	// Now reconstruct the job from DB — simulates server restart
	dbJob := &models.Job{
		ID:          job.ID.String(),
		Status:      models.JobStatusCompleted,
		TotalFiles:  1,
		Completed:   1,
		Progress:    100,
		Destination: "/dest/path",
		TempDir:     t.TempDir(),
		StartedAt:   time.Now(),
	}
	filesJSON, _ := json.Marshal([]string{"/path/file1.mp4"})
	dbJob.Files = string(filesJSON)
	resultsJSON, _ := json.Marshal(map[string]*MovieResult{
		"/path/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		},
	})
	dbJob.Results = string(resultsJSON)

	reconstructed := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, reconstructed)

	// The critical assertion: persistFn must be non-nil on reconstructed jobs
	assert.NotNil(t, reconstructed.deps.PersistFn, "reconstructed job must have persistFn — without it, PersistJobByID silently discards results")

	// Call persistFn and verify it triggers persistToDatabase (which calls jobRepo.Upsert)
	if reconstructed.deps.PersistFn != nil {
		reconstructed.deps.PersistFn()
		assert.Equal(t, int32(1), mockRepo.upsertCalled.Load(), "persistFn should call persistToDatabase which calls jobRepo.Upsert")
		assert.Equal(t, job.ID.String(), mockRepo.getLastJobID())
	}
}

// TestReconstructBatchJob_PersistJobByIDPersistsResults tests that PersistJobByID
// on a reconstructed job persists results to the database.
func TestReconstructBatchJob_PersistJobByIDPersistsResults(t *testing.T) {
	t.Parallel()

	mockRepo := &mockJobRepoForPersist{}
	jq := NewJobStore(mockRepo, nil, nil, t.TempDir(), nil, nil)

	// Create a job with results so we can reconstruct it
	jobCfg := &JobConfig{
		ID: "test-persist-managed-job",
		BatchJobDeps: BatchJobDeps{
			WF: &stubRescrapeWF{},
			BatchCfg: BatchJobConfig{
				MaxWorkers:    2,
				WorkerTimeout: 30 * time.Second,
			},
		},
	}
	job := jq.CreateJobBatch([]string{"/path/file1.mp4"}, jobCfg)

	// Reset counter (CreateJob already called Upsert once)
	mockRepo.upsertCalled.Store(0)

	// Reconstruct the same job from DB
	dbJob := &models.Job{
		ID:         job.ID.String(),
		Status:     models.JobStatusCompleted,
		TotalFiles: 1,
		Completed:  1,
		Progress:   100,
		TempDir:    t.TempDir(),
		StartedAt:  time.Now(),
	}
	filesJSON, _ := json.Marshal([]string{"/path/file1.mp4"})
	dbJob.Files = string(filesJSON)
	resultsJSON, _ := json.Marshal(map[string]*MovieResult{
		"/path/file1.mp4": {
			FileMatchInfo: models.FileMatchInfo{Path: "/path/file1.mp4", MovieID: "ABC-001"},
			Revision:      1,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		},
	})
	dbJob.Results = string(resultsJSON)

	reconstructed := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, reconstructed)

	// Simulate what the API handler does after Rescrape: call PersistJobByID
	jq.PersistJobByID(reconstructed.ID.String())

	// Verify the underlying DB repo was called
	assert.Equal(t, int32(1), mockRepo.upsertCalled.Load(), "PersistJobByID on reconstructed job should trigger DB upsert")
}
