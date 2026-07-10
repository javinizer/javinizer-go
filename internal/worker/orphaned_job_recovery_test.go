package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoverOrphanedJobs_RunningJobMarkedFailed(t *testing.T) {
	store := NewInMemoryJobStore()

	job := store.CreateJobBatch([]string{"/test/file.mp4"})
	require.NotNil(t, job)

	job.lifecycle.mu.Lock()
	job.lifecycle.Status = models.JobStatusRunning
	job.lifecycle.mu.Unlock()

	store.recoverOrphanedJobs()

	status, ok := store.GetJob(job.ID.String())
	require.True(t, ok)
	assert.Equal(t, models.JobStatusFailed, status.Status)
	assert.NotNil(t, status.CompletedAt)
}

func TestRecoverOrphanedJobs_PendingJobMarkedFailed(t *testing.T) {
	store := NewInMemoryJobStore()

	job := store.CreateJobBatch([]string{"/test/file.mp4"})
	require.NotNil(t, job)

	job.lifecycle.mu.Lock()
	job.lifecycle.Status = models.JobStatusPending
	job.lifecycle.mu.Unlock()

	store.recoverOrphanedJobs()

	status, ok := store.GetJob(job.ID.String())
	require.True(t, ok)
	assert.Equal(t, models.JobStatusFailed, status.Status)
	assert.NotNil(t, status.CompletedAt)
}

func TestRecoverOrphanedJobs_TerminalJobsUntouched(t *testing.T) {
	store := NewInMemoryJobStore()

	job := store.CreateJobBatch([]string{"/test/file.mp4"})
	require.NotNil(t, job)

	originalCompletedAt := time.Now()
	originalProgress := 42.0

	terminalStatuses := []models.JobStatus{
		models.JobStatusCompleted,
		models.JobStatusFailed,
		models.JobStatusCancelled,
		models.JobStatusOrganized,
		models.JobStatusReverted,
	}

	for _, status := range terminalStatuses {
		job.lifecycle.mu.Lock()
		job.lifecycle.Status = status
		job.lifecycle.CompletedAt = &originalCompletedAt
		job.lifecycle.mu.Unlock()

		job.results.mu.Lock()
		job.results.Progress = originalProgress
		job.results.mu.Unlock()

		store.recoverOrphanedJobs()

		result, ok := store.GetJob(job.ID.String())
		require.True(t, ok)
		assert.Equal(t, status, result.Status, "terminal job should not be recovered")
		assert.Equal(t, originalProgress, result.Progress, "progress should be preserved for terminal jobs")
		require.NotNil(t, result.CompletedAt)
		assert.Equal(t, originalCompletedAt.Unix(), result.CompletedAt.Unix(), "completed_at should be preserved")
	}
}

func TestRecoverOrphanedJobs_MultipleOrphanedJobs(t *testing.T) {
	store := NewInMemoryJobStore()

	job1 := store.CreateJobBatch([]string{"/test/file1.mp4"})
	job2 := store.CreateJobBatch([]string{"/test/file2.mp4"})
	job3 := store.CreateJobBatch([]string{"/test/file3.mp4"})

	require.NotNil(t, job1)
	require.NotNil(t, job2)
	require.NotNil(t, job3)

	job1.lifecycle.mu.Lock()
	job1.lifecycle.Status = models.JobStatusRunning
	job1.lifecycle.mu.Unlock()

	job2.lifecycle.mu.Lock()
	job2.lifecycle.Status = models.JobStatusPending
	job2.lifecycle.mu.Unlock()

	job3.lifecycle.mu.Lock()
	job3.lifecycle.Status = models.JobStatusCompleted
	completedAt := time.Now()
	job3.lifecycle.CompletedAt = &completedAt
	job3.lifecycle.mu.Unlock()

	store.recoverOrphanedJobs()

	s1, ok := store.GetJob(job1.ID.String())
	require.True(t, ok)
	s2, ok := store.GetJob(job2.ID.String())
	require.True(t, ok)
	s3, ok := store.GetJob(job3.ID.String())
	require.True(t, ok)

	assert.Equal(t, models.JobStatusFailed, s1.Status)
	assert.Equal(t, models.JobStatusFailed, s2.Status)
	assert.Equal(t, models.JobStatusCompleted, s3.Status)
}

func TestRecoverOrphanedJobs_EmptyStore(t *testing.T) {
	store := NewInMemoryJobStore()

	store.recoverOrphanedJobs()

	assert.Empty(t, store.ListJobs())
}

func TestRecoverOrphanedJobs_RunningJobCanBeDeletedAfter(t *testing.T) {
	store := NewInMemoryJobStore()

	job := store.CreateJobBatch([]string{"/test/file.mp4"})
	require.NotNil(t, job)

	job.lifecycle.mu.Lock()
	job.lifecycle.Status = models.JobStatusRunning
	job.lifecycle.mu.Unlock()

	err := store.DeleteJob(job.ID.String())
	assert.Error(t, err, "cannot delete running job before recovery")

	store.recoverOrphanedJobs()

	err = store.DeleteJob(job.ID.String())
	assert.NoError(t, err, "should be deletable after recovery marks it failed")

	_, ok := store.GetJob(job.ID.String())
	assert.False(t, ok, "job should be gone after delete")
}

func TestRecoverOrphanedJobs_PersistsRecoveredStatus(t *testing.T) {
	fakePersist := &fakePersistencer{}
	store := NewInMemoryJobStore(WithPersistence(fakePersist))

	job := store.CreateJobBatch([]string{"/test/file.mp4"})
	require.NotNil(t, job)

	job.lifecycle.mu.Lock()
	job.lifecycle.Status = models.JobStatusRunning
	job.lifecycle.mu.Unlock()

	fakePersist.calls = 0
	store.recoverOrphanedJobs()

	assert.Equal(t, 1, fakePersist.calls, "recovered job should be persisted")
	assert.Equal(t, models.JobStatusFailed, fakePersist.lastStatus, "persisted status should be failed")
	assert.NotNil(t, fakePersist.lastCompletedAt, "persisted completed_at should be set")
}

func TestRecoverOrphanedJobs_IntegrationWithLoadFromDatabase(t *testing.T) {
	runningJob := &models.Job{
		ID:         "loadtest-0001",
		Status:     models.JobStatusRunning,
		TotalFiles: 1,
		Completed:  0,
		Failed:     0,
		Progress:   0,
		Files:      `["/test/file.mp4"]`,
		Results:    `{"domain":{}}`,
		StartedAt:  time.Now().Add(-1 * time.Hour),
	}
	pendingJob := &models.Job{
		ID:         "loadtest-0002",
		Status:     models.JobStatusPending,
		TotalFiles: 1,
		Completed:  0,
		Failed:     0,
		Progress:   0,
		Files:      `["/test/file2.mp4"]`,
		Results:    `{"domain":{}}`,
		StartedAt:  time.Now().Add(-30 * time.Minute),
	}
	completedJob := &models.Job{
		ID:         "loadtest-0003",
		Status:     models.JobStatusCompleted,
		TotalFiles: 1,
		Completed:  1,
		Failed:     0,
		Progress:   100,
		Files:      `["/test/file3.mp4"]`,
		Results:    `{"domain":{}}`,
		StartedAt:  time.Now().Add(-2 * time.Hour),
	}

	fakePersist := &fakePersistencer{
		jobs: []*models.Job{runningJob, pendingJob, completedJob},
	}
	store := NewInMemoryJobStore(WithPersistence(fakePersist))
	store.loadFromDatabase()

	jobs := store.ListJobs()
	require.Len(t, jobs, 3)

	for _, j := range jobs {
		if j.ID.String() == "loadtest-0001" || j.ID.String() == "loadtest-0002" {
			assert.Equal(t, models.JobStatusFailed, j.Status, "orphaned job should be recovered to failed")
		}
		if j.ID.String() == "loadtest-0003" {
			assert.Equal(t, models.JobStatusCompleted, j.Status, "completed job should be untouched")
		}
	}

	assert.GreaterOrEqual(t, fakePersist.persistCount, 2, "at least 2 recovered jobs should be persisted")
}

type fakePersistencer struct {
	mu              sync.Mutex
	calls           int
	lastStatus      models.JobStatus
	lastCompletedAt *time.Time
	persistCount    int
	jobs            []*models.Job
}

func (f *fakePersistencer) PersistJob(job *BatchJob) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.persistCount++
	job.lifecycle.mu.RLock()
	f.lastStatus = job.lifecycle.Status
	f.lastCompletedAt = job.lifecycle.CompletedAt
	job.lifecycle.mu.RUnlock()
}

func (f *fakePersistencer) PersistJobByID(id string) {}

func (f *fakePersistencer) DeleteJobFromDB(id string) error { return nil }

func (f *fakePersistencer) LoadJobs(_ context.Context) ([]models.Job, error) {
	if f.jobs == nil {
		return nil, nil
	}
	result := make([]models.Job, len(f.jobs))
	for i, j := range f.jobs {
		result[i] = *j
	}
	return result, nil
}

func (f *fakePersistencer) UpsertJob(_ *models.Job) error { return nil }
