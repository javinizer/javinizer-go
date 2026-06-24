package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- StartStaleTempCleanup (lines 460-485) ---

func TestJobStore_StartStaleTempCleanup_StopsOnClose(t *testing.T) {
	mockRepo := mocks.NewMockJobRepositoryInterface(t)
	mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()

	jq := NewJobStore(mockRepo, nil, nil, t.TempDir(), nil, nil)
	stop := jq.StartStaleTempCleanup()

	// Close the stop channel immediately — the goroutine should exit
	close(stop)

	// Give the goroutine a moment to finish
	time.Sleep(100 * time.Millisecond)

	// No panic or deadlock — the test passes
}

func TestJobStore_StartStaleTempCleanup_RunsOnStartup(t *testing.T) {
	memFS := afero.NewMemMapFs()
	tmpDir := t.TempDir()
	postersDir := filepath.Join(tmpDir, "posters")

	// Create an orphaned directory (job doesn't exist in DB)
	orphanDir := filepath.Join(postersDir, "orphan-job-id")
	require.NoError(t, memFS.MkdirAll(orphanDir, 0755))

	mockRepo := mocks.NewMockJobRepositoryInterface(t)
	mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()
	// The startup cleanup will call FindByID for "orphan-job-id"
	mockRepo.On("FindByID", mock.Anything, "orphan-job-id").Return(nil, nil).Once()

	jq := NewJobStore(mockRepo, nil, nil, tmpDir, nil, memFS)
	stop := jq.StartStaleTempCleanup()

	// Give the startup cleanup a moment to run
	time.Sleep(200 * time.Millisecond)

	// Close stop
	close(stop)
	time.Sleep(100 * time.Millisecond)

	// The orphaned directory should be removed
	exists, _ := afero.Exists(memFS, orphanDir)
	assert.False(t, exists, "orphaned temp directory should be cleaned up on startup")

	mockRepo.AssertExpectations(t)
}

// --- CleanupStaleTempDirs (lines 400-458) ---

func TestJobStore_CleanupStaleTempDirs_NilFS(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	jq.fs = nil

	removed, err := jq.CleanupStaleTempDirs(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 0, removed)
}

func TestJobStore_CleanupStaleTempDirs_NonExistentDir(t *testing.T) {
	memFS := afero.NewMemMapFs()
	jq := NewJobStore(nil, nil, nil, "/nonexistent", nil, memFS)

	removed, err := jq.CleanupStaleTempDirs(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 0, removed)
}

func TestJobStore_CleanupStaleTempDirs_EmptyDir(t *testing.T) {
	memFS := afero.NewMemMapFs()
	tmpDir := t.TempDir()
	postersDir := filepath.Join(tmpDir, "posters")
	require.NoError(t, memFS.MkdirAll(postersDir, 0755))

	jq := NewJobStore(nil, nil, nil, tmpDir, nil, memFS)

	removed, err := jq.CleanupStaleTempDirs(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 0, removed)
}

func TestJobStore_CleanupStaleTempDirs_OrphanedDirectory(t *testing.T) {
	memFS := afero.NewMemMapFs()
	tmpDir := t.TempDir()
	postersDir := filepath.Join(tmpDir, "posters")
	require.NoError(t, memFS.MkdirAll(filepath.Join(postersDir, "orphan-job"), 0755))

	mockRepo := mocks.NewMockJobRepositoryInterface(t)
	mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()
	mockRepo.On("FindByID", mock.Anything, "orphan-job").Return(nil, nil).Once()

	jq := NewJobStore(mockRepo, nil, nil, tmpDir, nil, memFS)

	removed, err := jq.CleanupStaleTempDirs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, removed)

	// Verify directory was removed
	exists, _ := afero.Exists(memFS, filepath.Join(postersDir, "orphan-job"))
	assert.False(t, exists)
}

func TestJobStore_CleanupStaleTempDirs_TerminalJobOldEnough(t *testing.T) {
	memFS := afero.NewMemMapFs()
	tmpDir := t.TempDir()
	postersDir := filepath.Join(tmpDir, "posters")
	require.NoError(t, memFS.MkdirAll(filepath.Join(postersDir, "old-organized-job"), 0755))

	// Create a job that was organized more than 24 hours ago
	oldTime := time.Now().UTC().Add(-48 * time.Hour)
	oldJob := &models.Job{
		ID:          "old-organized-job",
		Status:      models.JobStatusOrganized,
		TotalFiles:  1,
		StartedAt:   oldTime,
		OrganizedAt: &oldTime,
	}

	mockRepo := mocks.NewMockJobRepositoryInterface(t)
	mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()
	mockRepo.On("FindByID", mock.Anything, "old-organized-job").Return(oldJob, nil).Once()

	jq := NewJobStore(mockRepo, nil, nil, tmpDir, nil, memFS)

	removed, err := jq.CleanupStaleTempDirs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
}

func TestJobStore_CleanupStaleTempDirs_TerminalJobTooRecent(t *testing.T) {
	memFS := afero.NewMemMapFs()
	tmpDir := t.TempDir()
	postersDir := filepath.Join(tmpDir, "posters")
	require.NoError(t, memFS.MkdirAll(filepath.Join(postersDir, "recent-organized-job"), 0755))

	// Create a job that was organized less than 24 hours ago
	recentTime := time.Now().UTC().Add(-1 * time.Hour)
	recentJob := &models.Job{
		ID:          "recent-organized-job",
		Status:      models.JobStatusOrganized,
		TotalFiles:  1,
		StartedAt:   recentTime,
		OrganizedAt: &recentTime,
	}

	mockRepo := mocks.NewMockJobRepositoryInterface(t)
	mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()
	mockRepo.On("FindByID", mock.Anything, "recent-organized-job").Return(recentJob, nil).Once()

	jq := NewJobStore(mockRepo, nil, nil, tmpDir, nil, memFS)

	removed, err := jq.CleanupStaleTempDirs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, removed, "recently organized job should not be cleaned up")
}

func TestJobStore_CleanupStaleTempDirs_NonTerminalJob(t *testing.T) {
	memFS := afero.NewMemMapFs()
	tmpDir := t.TempDir()
	postersDir := filepath.Join(tmpDir, "posters")
	require.NoError(t, memFS.MkdirAll(filepath.Join(postersDir, "running-job"), 0755))

	// Create a running job — should NOT be cleaned up
	runningJob := &models.Job{
		ID:         "running-job",
		Status:     models.JobStatusRunning,
		TotalFiles: 1,
		StartedAt:  time.Now().UTC().Add(-48 * time.Hour),
	}

	mockRepo := mocks.NewMockJobRepositoryInterface(t)
	mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()
	mockRepo.On("FindByID", mock.Anything, "running-job").Return(runningJob, nil).Once()

	jq := NewJobStore(mockRepo, nil, nil, tmpDir, nil, memFS)

	removed, err := jq.CleanupStaleTempDirs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, removed, "running job should not be cleaned up")
}

func TestJobStore_CleanupStaleTempDirs_FileNotDir(t *testing.T) {
	memFS := afero.NewMemMapFs()
	tmpDir := t.TempDir()
	postersDir := filepath.Join(tmpDir, "posters")
	require.NoError(t, memFS.MkdirAll(postersDir, 0755))

	// Create a regular file in the posters dir (not a directory)
	require.NoError(t, afero.WriteFile(memFS, filepath.Join(postersDir, "not-a-dir.txt"), []byte("test"), 0644))

	jq := NewJobStore(nil, nil, nil, tmpDir, nil, memFS)

	removed, err := jq.CleanupStaleTempDirs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, removed, "files should be skipped, not cleaned up")
}

func TestJobStore_CleanupStaleTempDirs_NoJobRepo_OldHeuristic(t *testing.T) {
	memFS := afero.NewMemMapFs()
	tmpDir := t.TempDir()
	postersDir := filepath.Join(tmpDir, "posters")

	// Create a directory
	oldDir := filepath.Join(postersDir, "heuristic-old")
	require.NoError(t, memFS.MkdirAll(oldDir, 0755))

	// When jobRepo is nil, the heuristic falls through.
	// The ModTime check will fail because the dir was just created.
	jq := NewJobStore(nil, nil, nil, tmpDir, nil, memFS)

	removed, err := jq.CleanupStaleTempDirs(context.Background())
	require.NoError(t, err)
	// Dir was just created so its modtime is recent — should not be removed
	assert.Equal(t, 0, removed)
}

// --- isPastActiveStatus (line 418) ---

func TestIsTerminalStatus_AllTerminalStates(t *testing.T) {
	assert.True(t, isPastActiveStatus(models.JobStatusOrganized))
	assert.True(t, isPastActiveStatus(models.JobStatusFailed))
	assert.True(t, isPastActiveStatus(models.JobStatusCancelled))
	assert.True(t, isPastActiveStatus(models.JobStatusReverted))
	assert.True(t, isPastActiveStatus(models.JobStatusCompleted))
}

func TestIsTerminalStatus_NonTerminalStates(t *testing.T) {
	assert.False(t, isPastActiveStatus(models.JobStatusPending))
	assert.False(t, isPastActiveStatus(models.JobStatusRunning))
}

// --- latestInactiveTime (line 425) ---

func TestLatestTerminalTime_AllNil(t *testing.T) {
	job := &models.Job{}
	result := latestInactiveTime(job)
	assert.Nil(t, result)
}

func TestLatestTerminalTime_OnlyOrganizedAt(t *testing.T) {
	ts := time.Now().UTC()
	job := &models.Job{OrganizedAt: &ts}
	result := latestInactiveTime(job)
	require.NotNil(t, result)
	assert.Equal(t, ts, *result)
}

func TestLatestTerminalTime_OnlyCompletedAt(t *testing.T) {
	ts := time.Now().UTC()
	job := &models.Job{CompletedAt: &ts}
	result := latestInactiveTime(job)
	require.NotNil(t, result)
	assert.Equal(t, ts, *result)
}

func TestLatestTerminalTime_OnlyRevertedAt(t *testing.T) {
	ts := time.Now().UTC()
	job := &models.Job{RevertedAt: &ts}
	result := latestInactiveTime(job)
	require.NotNil(t, result)
	assert.Equal(t, ts, *result)
}

func TestLatestTerminalTime_RevertedIsLatest(t *testing.T) {
	earlier := time.Now().UTC().Add(-2 * time.Hour)
	middle := time.Now().UTC().Add(-1 * time.Hour)
	latest := time.Now().UTC()

	job := &models.Job{
		OrganizedAt: &earlier,
		CompletedAt: &middle,
		RevertedAt:  &latest,
	}
	result := latestInactiveTime(job)
	require.NotNil(t, result)
	assert.Equal(t, latest, *result)
}

func TestLatestTerminalTime_CompletedIsLatest(t *testing.T) {
	earlier := time.Now().UTC().Add(-2 * time.Hour)
	latest := time.Now().UTC()

	job := &models.Job{
		OrganizedAt: &earlier,
		CompletedAt: &latest,
	}
	result := latestInactiveTime(job)
	require.NotNil(t, result)
	assert.Equal(t, latest, *result)
}

func TestJobStore_DeleteJob_AlreadyDeleted_Miss(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	// Mark as deleted manually
	job.lifecycle.mu.Lock()
	job.lifecycle.deleted = true
	job.lifecycle.mu.Unlock()

	err := jq.DeleteJob(job.ID.String())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already deleted")
}

// --- DeleteJob running job (line 292) ---

func TestJobStore_DeleteJob_RunningJob_Miss(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	// Set status to Running
	job.lifecycle.mu.Lock()
	job.lifecycle.Status = models.JobStatusRunning
	job.lifecycle.mu.Unlock()

	err := jq.DeleteJob(job.ID.String())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot delete running job")
}

// --- DeleteJob pending job cancels first (line 296) ---

func TestJobStore_DeleteJob_PendingJobCancels_Miss(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	err := jq.DeleteJob(job.ID.String())
	assert.NoError(t, err)

	// Job should be gone
	_, ok := jq.GetJob(job.ID.String())
	assert.False(t, ok)
}

// --- PersistJobByID not found (line 315) ---

func TestJobStore_PersistJobByID_NotFound_Miss(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)

	// Should not panic
	assert.NotPanics(t, func() {
		jq.PersistJobByID("nonexistent")
	})
}

// Suppress os import
var _ = os.ReadFile

// TestJobStore_CleanupStaleTempDirs_TransientDBErrorDoesNotDelete verifies that
// a transient DB error (not ErrNotFound) from FindByID does NOT cause the temp
// dir to be deleted — the cleaner should skip and retry on the next cycle.
func TestJobStore_CleanupStaleTempDirs_TransientDBErrorDoesNotDelete(t *testing.T) {
	memFS := afero.NewMemMapFs()
	tmpDir := t.TempDir()
	postersDir := filepath.Join(tmpDir, "posters")
	jobDir := filepath.Join(postersDir, "active-job")
	require.NoError(t, memFS.MkdirAll(jobDir, 0755))

	mockRepo := mocks.NewMockJobRepositoryInterface(t)
	mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()
	// Return a generic DB error (NOT ErrNotFound) — transient failure.
	mockRepo.On("FindByID", mock.Anything, "active-job").Return(nil, fmt.Errorf("connection refused")).Once()

	jq := NewJobStore(mockRepo, nil, nil, tmpDir, nil, memFS)

	removed, err := jq.CleanupStaleTempDirs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, removed, "transient DB error must not cause deletion")

	// Directory must still exist.
	exists, _ := afero.Exists(memFS, jobDir)
	assert.True(t, exists, "temp dir must survive a transient DB error")
}

// TestJobStore_CleanupStaleTempDirs_NotFoundStillRemovesOrphan verifies that
// ErrNotFound is still treated as orphaned and removed.
func TestJobStore_CleanupStaleTempDirs_NotFoundStillRemovesOrphan(t *testing.T) {
	memFS := afero.NewMemMapFs()
	tmpDir := t.TempDir()
	postersDir := filepath.Join(tmpDir, "posters")
	jobDir := filepath.Join(postersDir, "orphan-job")
	require.NoError(t, memFS.MkdirAll(jobDir, 0755))

	mockRepo := mocks.NewMockJobRepositoryInterface(t)
	mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()
	mockRepo.On("FindByID", mock.Anything, "orphan-job").Return(nil, database.ErrNotFound).Once()

	jq := NewJobStore(mockRepo, nil, nil, tmpDir, nil, memFS)

	removed, err := jq.CleanupStaleTempDirs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, removed, "ErrNotFound should still remove orphaned dir")

	exists, _ := afero.Exists(memFS, jobDir)
	assert.False(t, exists)
}
