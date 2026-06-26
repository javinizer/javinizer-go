package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// job_store.go: CreateJob, PersistJob, CleanupStaleTempDirs,
// isPastActiveStatus, latestInactiveTime, StartStaleTempCleanup,
// setDepsFromConfig, loadFromDatabase, DeleteJob
// ---------------------------------------------------------------------------

func TestJobStore_CreateJob(t *testing.T) {
	t.Run("returns BatchJobInterface composite", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		controlled := jq.CreateJob([]string{"file1.mp4"})
		require.NotNil(t, controlled)
		assert.NotEmpty(t, controlled.GetID())
		assert.Equal(t, models.JobStatusPending, controlled.GetJobStatus())
	})

	t.Run("with JobConfig sets deps", func(t *testing.T) {
		mockRepo := mocks.NewMockMovieRepositoryInterface(t)
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		controlled := jq.CreateJob([]string{"file1.mp4"}, &JobConfig{
			BatchJobDeps: BatchJobDeps{
				MovieRepo: mockRepo,
			},
		})
		require.NotNil(t, controlled)
	})
}

func TestJobStore_PersistJob(t *testing.T) {
	t.Run("delegates to persistToDatabase", func(t *testing.T) {
		mockRepo := mocks.NewMockJobRepositoryInterface(t)
		mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()
		mockRepo.On("Upsert", mock.Anything, mock.AnythingOfType("*models.Job")).Return(nil).Once()

		jq := NewJobStore(mockRepo, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		// Reset call count — CreateJob already called Upsert once
		mockRepo.AssertExpectations(t)
		mockRepo.On("Upsert", mock.Anything, mock.AnythingOfType("*models.Job")).Return(nil).Once()

		jq.PersistJob(job)
		mockRepo.AssertExpectations(t)
	})
}

func TestIsTerminalStatus(t *testing.T) {
	tests := []struct {
		status   models.JobStatus
		expected bool
	}{
		{models.JobStatusOrganized, true},
		{models.JobStatusFailed, true},
		{models.JobStatusCancelled, true},
		{models.JobStatusReverted, true},
		{models.JobStatusCompleted, true},
		{models.JobStatusPending, false},
		{models.JobStatusRunning, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.expected, isPastActiveStatus(tt.status))
		})
	}
}

func TestLatestTerminalTime(t *testing.T) {
	t.Run("returns nil when no timestamps set", func(t *testing.T) {
		job := &models.Job{}
		assert.Nil(t, latestInactiveTime(job))
	})

	t.Run("returns OrganizedAt when only set", func(t *testing.T) {
		now := time.Now()
		job := &models.Job{OrganizedAt: &now}
		result := latestInactiveTime(job)
		require.NotNil(t, result)
		assert.Equal(t, now, *result)
	})

	t.Run("returns CompletedAt when only set", func(t *testing.T) {
		now := time.Now()
		job := &models.Job{CompletedAt: &now}
		result := latestInactiveTime(job)
		require.NotNil(t, result)
		assert.Equal(t, now, *result)
	})

	t.Run("returns RevertedAt when only set", func(t *testing.T) {
		now := time.Now()
		job := &models.Job{RevertedAt: &now}
		result := latestInactiveTime(job)
		require.NotNil(t, result)
		assert.Equal(t, now, *result)
	})

	t.Run("returns latest of all timestamps", func(t *testing.T) {
		early := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		mid := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
		late := time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)

		job := &models.Job{
			OrganizedAt: &mid,
			CompletedAt: &early,
			RevertedAt:  &late,
		}
		result := latestInactiveTime(job)
		require.NotNil(t, result)
		assert.Equal(t, late, *result)
	})
}

func TestJobStore_CleanupStaleTempDirs(t *testing.T) {
	t.Run("returns 0 when fs is nil", func(t *testing.T) {
		jq := &JobStore{fs: nil}
		count, err := jq.CleanupStaleTempDirs(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("returns 0 when posters dir does not exist", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		jq := &JobStore{fs: fs, tempDir: "/tmp"}
		count, err := jq.CleanupStaleTempDirs(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("returns 0 when posters dir is empty", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		fs.MkdirAll("/tmp/posters", 0755)
		jq := &JobStore{fs: fs, tempDir: "/tmp"}
		count, err := jq.CleanupStaleTempDirs(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("removes orphaned directories when job not in database", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		fs.MkdirAll("/tmp/posters/orphaned-job", 0755)
		// Create a file in the orphaned dir to make it non-empty
		afero.WriteFile(fs, "/tmp/posters/orphaned-job/poster.jpg", []byte("data"), 0644)

		mockRepo := mocks.NewMockJobRepositoryInterface(t)
		mockRepo.On("FindByID", mock.Anything, "orphaned-job").Return(nil, nil).Once()

		jq := &JobStore{
			fs:      fs,
			tempDir: "/tmp",
			jobRepo: mockRepo,
		}
		count, err := jq.CleanupStaleTempDirs(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		exists, _ := afero.Exists(fs, "/tmp/posters/orphaned-job")
		assert.False(t, exists, "orphaned directory should be removed")
		mockRepo.AssertExpectations(t)
	})

	t.Run("removes terminal directories older than 24h", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		fs.MkdirAll("/tmp/posters/old-job", 0755)
		afero.WriteFile(fs, "/tmp/posters/old-job/poster.jpg", []byte("data"), 0644)

		oldTime := time.Now().Add(-48 * time.Hour)
		mockRepo := mocks.NewMockJobRepositoryInterface(t)
		mockRepo.On("FindByID", mock.Anything, "old-job").Return(&models.Job{
			ID:          "old-job",
			Status:      models.JobStatusOrganized,
			OrganizedAt: &oldTime,
		}, nil).Once()

		jq := &JobStore{
			fs:      fs,
			tempDir: "/tmp",
			jobRepo: mockRepo,
		}
		count, err := jq.CleanupStaleTempDirs(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, count)
		mockRepo.AssertExpectations(t)
	})

	t.Run("keeps recent terminal directories", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		fs.MkdirAll("/tmp/posters/recent-job", 0755)
		afero.WriteFile(fs, "/tmp/posters/recent-job/poster.jpg", []byte("data"), 0644)

		recentTime := time.Now().Add(-1 * time.Hour)
		mockRepo := mocks.NewMockJobRepositoryInterface(t)
		mockRepo.On("FindByID", mock.Anything, "recent-job").Return(&models.Job{
			ID:          "recent-job",
			Status:      models.JobStatusOrganized,
			OrganizedAt: &recentTime,
		}, nil).Once()

		jq := &JobStore{
			fs:      fs,
			tempDir: "/tmp",
			jobRepo: mockRepo,
		}
		count, err := jq.CleanupStaleTempDirs(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 0, count)
		mockRepo.AssertExpectations(t)
	})

	t.Run("keeps non-terminal directories", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		fs.MkdirAll("/tmp/posters/running-job", 0755)

		mockRepo := mocks.NewMockJobRepositoryInterface(t)
		mockRepo.On("FindByID", mock.Anything, "running-job").Return(&models.Job{
			ID:     "running-job",
			Status: models.JobStatusRunning,
		}, nil).Once()

		jq := &JobStore{
			fs:      fs,
			tempDir: "/tmp",
			jobRepo: mockRepo,
		}
		count, err := jq.CleanupStaleTempDirs(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 0, count)
		mockRepo.AssertExpectations(t)
	})

	t.Run("skips non-directory entries", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		fs.MkdirAll("/tmp/posters", 0755)
		afero.WriteFile(fs, "/tmp/posters/somefile.txt", []byte("data"), 0644)

		jq := &JobStore{fs: fs, tempDir: "/tmp"}
		count, err := jq.CleanupStaleTempDirs(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("no job repo: removes old directories as heuristic", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		fs.MkdirAll("/tmp/posters/old-dir", 0755)
		afero.WriteFile(fs, "/tmp/posters/old-dir/poster.jpg", []byte("data"), 0644)
		// Set mod time to > 24h ago
		fs.Chtimes("/tmp/posters/old-dir", time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour))

		jq := &JobStore{
			fs:      fs,
			tempDir: "/tmp",
		}
		count, err := jq.CleanupStaleTempDirs(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})
}

func TestJobStore_StartStaleTempCleanup(t *testing.T) {
	t.Run("returns stop channel and stops when closed", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		jq := &JobStore{
			fs:      fs,
			tempDir: "/tmp",
		}
		stop := jq.StartStaleTempCleanup()
		require.NotNil(t, stop)
		close(stop)
		// Give the goroutine time to exit
		time.Sleep(100 * time.Millisecond)
	})
}

func TestJobStore_setDepsFromConfig(t *testing.T) {
	t.Run("nil config does nothing", func(t *testing.T) {
		job := &BatchJob{deps: BatchJobDeps{}}
		job.controller = newJobController(job)
		job.controller.setDepsFromConfig(nil)
		assert.Nil(t, job.deps.WF)
	})

	t.Run("sets all deps from config", func(t *testing.T) {
		job := &BatchJob{deps: BatchJobDeps{}}
		job.controller = newJobController(job)
		wf := &stubWorkflow{}
		matcher := &stubMatcher{result: "ABC-001"}
		mockEmitter := mocks.NewMockEventEmitter(t)
		persistFn := func() {}

		cfg := &JobConfig{
			BatchJobDeps: BatchJobDeps{
				WF:        wf,
				Matcher:   matcher,
				BatchCfg:  BatchJobConfig{MaxWorkers: 3},
				Emitter:   mockEmitter,
				PersistFn: persistFn,
			},
		}
		job.controller.setDepsFromConfig(cfg)
		assert.Equal(t, wf, job.deps.WF)
		assert.Equal(t, matcher, job.deps.Matcher)
		assert.Equal(t, 3, job.deps.BatchCfg.MaxWorkers)
		assert.Equal(t, mockEmitter, job.deps.Emitter)
		assert.NotNil(t, job.deps.PersistFn)
	})

	t.Run("replaces BatchCfg when new config has fields", func(t *testing.T) {
		job := &BatchJob{deps: BatchJobDeps{
			BatchCfg: BatchJobConfig{MaxWorkers: 5},
		}}
		job.controller = newJobController(job)
		cfg := &JobConfig{
			BatchJobDeps: BatchJobDeps{
				BatchCfg: BatchJobConfig{WorkerTimeout: 10 * time.Second},
			},
		}
		// setDepsFromConfig replaces entire BatchCfg when any new field is non-zero
		job.controller.setDepsFromConfig(cfg)
		assert.Equal(t, 10*time.Second, job.deps.BatchCfg.WorkerTimeout, "new WorkerTimeout should be set")
	})
}

func TestJobStore_loadFromDatabase(t *testing.T) {
	t.Run("loads jobs on NewJobStore", func(t *testing.T) {
		mockRepo := mocks.NewMockJobRepositoryInterface(t)
		now := time.Now()
		jobs := []models.Job{
			{
				ID:         "loaded-job-1",
				Status:     models.JobStatusCompleted,
				TotalFiles: 2,
				Completed:  2,
				Progress:   100,
				StartedAt:  now,
			},
		}
		mockRepo.On("List", mock.Anything).Return(jobs, nil).Once()

		jq := NewJobStore(mockRepo, nil, nil, "", nil, nil)

		// Verify the loaded job is accessible
		status, ok := jq.GetJob("loaded-job-1")
		require.True(t, ok)
		assert.Equal(t, models.JobStatusCompleted, status.Status)
		assert.Equal(t, 2, status.TotalFiles)
		mockRepo.AssertExpectations(t)
	})

	t.Run("handles List error gracefully", func(t *testing.T) {
		mockRepo := mocks.NewMockJobRepositoryInterface(t)
		mockRepo.On("List", mock.Anything).Return(nil, fmt.Errorf("db error")).Once()

		jq := NewJobStore(mockRepo, nil, nil, "", nil, nil)
		// Should not panic — jobs map should be empty
		jobs := jq.ListJobs()
		assert.Empty(t, jobs)
		mockRepo.AssertExpectations(t)
	})
}

func TestJobStore_DeleteJob_ErrorPaths(t *testing.T) {
	t.Run("returns error for already deleted job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.lifecycle.deleted = true

		err := jq.DeleteJob(job.ID.String())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already deleted")
	})

	t.Run("returns error for running job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.lifecycle.Status = models.JobStatusRunning

		err := jq.DeleteJob(job.ID.String())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot delete running job")
	})

	t.Run("deletes pending job by cancelling it first", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		err := jq.DeleteJob(job.ID.String())
		require.NoError(t, err)

		_, ok := jq.GetJob(job.ID.String())
		assert.False(t, ok, "job should be removed from store")
	})

	t.Run("removes temp poster directory", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		tempDir := t.TempDir()
		posterDir := filepath.Join(tempDir, "posters")
		os.MkdirAll(filepath.Join(posterDir, "test-id"), 0755)

		jq := NewJobStore(nil, nil, nil, tempDir, nil, fs)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		jobID := job.ID

		// Create the poster directory for this job
		fs.MkdirAll(filepath.Join(posterDir, jobID.String()), 0755)

		err := jq.DeleteJob(jobID.String())
		require.NoError(t, err)

		exists, _ := afero.Exists(fs, filepath.Join(posterDir, jobID.String()))
		assert.False(t, exists, "poster directory should be removed")
	})

	t.Run("handles database deletion failure", func(t *testing.T) {
		mockRepo := mocks.NewMockJobRepositoryInterface(t)
		mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()
		mockRepo.On("Upsert", mock.Anything, mock.AnythingOfType("*models.Job")).Return(nil).Once() // CreateJob calls Upsert
		mockRepo.On("Delete", mock.Anything, mock.AnythingOfType("string")).Return(fmt.Errorf("db error")).Once()

		jq := NewJobStore(mockRepo, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		err := jq.DeleteJob(job.ID.String())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database deletion failed")
		mockRepo.AssertExpectations(t)
	})
}

// ---------------------------------------------------------------------------
// batch_job.go: GetTempDir, IsDeleted, GetProvenance, SetJobStatus,
// GetResultsMap, SetFileMatchInfo, SetDeleted, SetFileResultRevision,
// TemplateEngine, GetResults, Subscribe, SendJobEvent,
// CloseEventBroadcaster, UpdateMovie, FindMovieResultForMovieID,
// GetMovieResultsForMovieID, GetFileMatchInfosForMovieID, findFileForRescrape,
// SetRunOptions, SetRunOptions, Run, Wait
// ---------------------------------------------------------------------------

func TestBatchJob_GetTempDir(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "/custom/temp", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	assert.Equal(t, "/custom/temp", job.GetTempDir())
}

func TestBatchJob_IsDeleted(t *testing.T) {
	t.Run("returns false for active job", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		assert.False(t, job.lifecycle.IsDeleted())
	})

	t.Run("returns true after SetDeleted", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.lifecycle.SetDeleted(true)
		assert.True(t, job.lifecycle.IsDeleted())
	})
}

func TestBatchJob_GetProvenance(t *testing.T) {
	t.Run("returns nil for unset provenance", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		assert.Nil(t, job.results.GetProvenance("file1.mp4"))
	})

	t.Run("returns cloned provenance", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		prov := &ProvenanceData{
			FieldSources:   map[string]string{"title": "r18dev"},
			ActressSources: map[string]string{"actress_0": "dmm"},
		}
		job.results.SetProvenance("file1.mp4", prov)

		retrieved := job.results.GetProvenance("file1.mp4")
		require.NotNil(t, retrieved)
		assert.Equal(t, "r18dev", retrieved.FieldSources["title"])

		// Mutation of returned provenance should not affect original
		retrieved.FieldSources["title"] = "modified"
		assert.Equal(t, "r18dev", job.results.GetProvenance("file1.mp4").FieldSources["title"])
	})
}

func TestBatchJob_SetJobStatus(t *testing.T) {
	t.Run("sets Running and clears timestamps", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetJobStatus(models.JobStatusRunning)
		assert.Equal(t, models.JobStatusRunning, job.lifecycle.GetJobStatus())
		assert.Nil(t, job.lifecycle.OrganizedAt)
		assert.Nil(t, job.lifecycle.RevertedAt)
	})

	t.Run("sets Completed and sets CompletedAt", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetJobStatus(models.JobStatusCompleted)
		assert.Equal(t, models.JobStatusCompleted, job.lifecycle.GetJobStatus())
		assert.NotNil(t, job.lifecycle.CompletedAt)
	})

	t.Run("sets Organized and sets OrganizedAt", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetJobStatus(models.JobStatusOrganized)
		assert.Equal(t, models.JobStatusOrganized, job.lifecycle.GetJobStatus())
		assert.NotNil(t, job.lifecycle.OrganizedAt)
	})

	t.Run("sets Reverted and sets RevertedAt", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetJobStatus(models.JobStatusReverted)
		assert.Equal(t, models.JobStatusReverted, job.lifecycle.GetJobStatus())
		assert.NotNil(t, job.lifecycle.RevertedAt)
	})
}

func TestBatchJob_GetResultsMap(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
	})

	results := job.results.SnapshotData().Results
	assert.Len(t, results, 1)
	assert.Contains(t, results, "file1.mp4")
}

func TestBatchJob_SetFileMatchInfo(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	info := models.FileMatchInfo{MovieID: "ABC-123", IsMultiPart: true, PartNumber: 1}
	job.results.setFileMatchInfo(map[string]models.FileMatchInfo{"file1.mp4": info})

	retrieved, ok := job.results.GetFileMatchInfo("file1.mp4")
	assert.True(t, ok)
	assert.Equal(t, "ABC-123", retrieved.MovieID)
	assert.True(t, retrieved.IsMultiPart)
}

func TestBatchJob_SetDeleted(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	assert.False(t, job.lifecycle.deleted)
	job.lifecycle.SetDeleted(true)
	assert.True(t, job.lifecycle.deleted)
	job.lifecycle.SetDeleted(false)
	assert.False(t, job.lifecycle.deleted)
}

func TestBatchJob_SetFileResultRevision(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
		Status:        models.JobStatusCompleted,
	})

	assert.Equal(t, uint64(1), job.results.Results["file1.mp4"].Revision)
	err := job.results.SetFileResultRevision("file1.mp4", 42)
	assert.NoError(t, err)
	assert.Equal(t, uint64(42), job.results.Results["file1.mp4"].Revision)
}

func TestBatchJob_SetFileResultRevision_ReturnsErrorOnMissing(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	err := job.results.SetFileResultRevision("nonexistent.mp4", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "file result not found")
}

func TestBatchJob_TemplateEngine(t *testing.T) {
	t.Run("returns injected engine when set", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		eng := job.TemplateEngine()
		assert.NotNil(t, eng)
	})

	t.Run("creates default engine when nil", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.templateEngine = nil
		eng := job.TemplateEngine()
		assert.NotNil(t, eng)
	})
}

func TestBatchJob_GetResults(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
	})

	results := job.results.GetResults()
	assert.Len(t, results, 1)
	assert.Equal(t, "ABC-001", results[0].FileMatchInfo.MovieID)
}

func TestBatchJob_Subscribe(t *testing.T) {
	t.Run("receives events from broadcaster", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		sub := job.Subscribe()
		defer sub.Close()

		job.SendJobEvent(JobEvent{JobID: job.ID, Message: "test event"})

		select {
		case evt := <-sub.Events():
			assert.Equal(t, job.ID, evt.JobID)
			assert.Equal(t, "test event", evt.Message)
		case <-time.After(time.Second):
			t.Fatal("should have received event")
		}
	})

	t.Run("returns closed channel when broadcaster is nil", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.batchJobEventSource.eventBroadcaster = nil
		sub := job.Subscribe()
		_, ok := <-sub.Events()
		assert.False(t, ok, "channel should be closed when broadcaster is nil")
	})
}

func TestBatchJob_SendJobEvent(t *testing.T) {
	t.Run("sends event to broadcaster", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		sub := job.Subscribe()
		defer sub.Close()

		job.SendJobEvent(JobEvent{JobID: job.ID, Step: StepScrape})

		select {
		case evt := <-sub.Events():
			assert.Equal(t, StepScrape, evt.Step)
		case <-time.After(time.Second):
			t.Fatal("should have received event")
		}
	})

	t.Run("no-op when broadcaster is nil", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.batchJobEventSource.eventBroadcaster = nil
		assert.NotPanics(t, func() {
			job.SendJobEvent(JobEvent{JobID: job.ID})
		})
	})
}

func TestBatchJob_CloseEventBroadcaster(t *testing.T) {
	t.Run("closes broadcaster and subscribers", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		sub := job.Subscribe()

		job.CloseEventBroadcaster()

		_, ok := <-sub.Events()
		assert.False(t, ok, "subscriber channel should be closed after broadcaster close")
	})

	t.Run("no-op when broadcaster is nil", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.batchJobEventSource.eventBroadcaster = nil
		assert.NotPanics(t, func() {
			job.CloseEventBroadcaster()
		})
	})
}

func TestBatchJob_UpdateMovie(t *testing.T) {
	t.Run("updates movie for existing result", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "OLD-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "OLD-001", Title: "Old Title"},
		})

		newMovie := &models.Movie{ID: "NEW-001", Title: "New Title"}
		err := job.results.UpdateMovie("file1.mp4", newMovie)
		require.NoError(t, err)

		result, _ := job.results.GetMovieResult("file1.mp4")
		assert.Equal(t, "NEW-001", result.Movie.ID)
		assert.Equal(t, "New Title", result.Movie.Title)
	})
}

func TestBatchJob_FindMovieResultForMovieID(t *testing.T) {
	t.Run("returns movie result for existing movieID", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.SetResultDirect("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001", Title: "Test Movie"},
		})

		result, err := job.resultIndex.FindMovieResultForMovieID("ABC-001")
		require.NoError(t, err)
		assert.Equal(t, "ABC-001", result.Movie.ID)
	})

	t.Run("returns error for nonexistent movieID", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		_, err := job.resultIndex.FindMovieResultForMovieID("NONEXISTENT-001")
		assert.Error(t, err)
	})

	t.Run("case-insensitive lookup", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.SetResultDirect("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		})

		result, err := job.resultIndex.FindMovieResultForMovieID("abc-001")
		require.NoError(t, err)
		assert.Equal(t, "ABC-001", result.Movie.ID)
	})
}

func TestBatchJob_GetMovieResultsForMovieID(t *testing.T) {
	t.Run("returns results for multipart movie", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4", "file2.mp4"})
		job.SetResultDirect("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		})
		job.SetResultDirect("file2.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file2.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		})

		results := job.resultIndex.GetMovieResultsForMovieID("ABC-001")
		assert.Len(t, results, 2)
	})

	t.Run("returns empty for nonexistent movieID", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		results := job.resultIndex.GetMovieResultsForMovieID("NONEXISTENT-001")
		assert.Empty(t, results)
	})
}

func TestBatchJob_GetFileMatchInfosForMovieID(t *testing.T) {
	t.Run("returns infos for existing movieID", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.SetResultDirect("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001", IsMultiPart: true},
			Status:        models.JobStatusCompleted,
		})

		infos := job.resultIndex.GetFileMatchInfosForMovieID("ABC-001")
		assert.Len(t, infos, 1)
		assert.True(t, infos[0].IsMultiPart)
	})

	t.Run("returns empty for nonexistent movieID", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		infos := job.resultIndex.GetFileMatchInfosForMovieID("NONEXISTENT-001")
		assert.Empty(t, infos)
	})
}

// TestResultTracker_FindFileForMovieID tests the MovieLookup method moved from
// BatchJob per ADR-0041 Decision 1. Pre-resolved FilePath handling moved to
// RescrapePhase.Rescrape per Decision 3.
func TestResultTracker_FindFileForMovieID(t *testing.T) {
	t.Run("finds file by movie ID", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.SetResultDirect("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Revision:      3,
			Movie:         &models.Movie{ID: "ABC-001"},
		})

		result, err := job.resultIndex.FindFileForMovieID("ABC-001")
		require.NoError(t, err)
		assert.Equal(t, "file1.mp4", result.FilePath)
		assert.Equal(t, uint64(3), result.CapturedRevision)
		assert.Equal(t, "ABC-001", result.OldMovieID)
	})

	t.Run("returns error for missing movie ID", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		_, err := job.resultIndex.FindFileForMovieID("NONEXISTENT-001")
		assert.Error(t, err)
	})
}

func TestBatchJob_SetRunOptions(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	scrapeCfg := ScrapePhaseConfig{Force: true}
	applyCfg := ApplyPhaseConfig{Destination: "/out"}

	// Per DEEP-1: SetRunOptions moved to StandaloneJob/JobRunner.
	sj := newStandaloneJobFromBatchJob(job)
	sj.SetRunOptions(scrapeCfg, applyCfg)
	// Verify the runner stored the options by checking the runner's fields
	runner := sj.(*standaloneJobAdapter).runner
	assert.NotNil(t, runner.scrapeCfg)
	assert.NotNil(t, runner.applyCfg)
	assert.True(t, runner.scrapeCfg.Force)
	assert.Equal(t, "/out", runner.applyCfg.Destination)
}

func TestBatchJob_Wait(t *testing.T) {
	t.Run("returns nil for completed job", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		go func() {
			time.Sleep(10 * time.Millisecond)
			job.lifecycle.MarkCompleted()
		}()
		err := job.Controller().Wait()
		assert.NoError(t, err)
	})

	t.Run("returns error for failed job", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		go func() {
			time.Sleep(10 * time.Millisecond)
			job.lifecycle.MarkFailed()
		}()
		err := job.Controller().Wait()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed")
	})

	t.Run("returns error for cancelled job", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		go func() {
			time.Sleep(10 * time.Millisecond)
			job.lifecycle.MarkCancelled()
		}()
		err := job.Controller().Wait()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cancelled")
	})
}

func TestBatchJob_MarkGuardedTransitions(t *testing.T) {
	t.Run("MarkCompleted is no-op when already Organized", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetJobStatus(models.JobStatusOrganized)
		job.lifecycle.MarkCompleted()
		assert.Equal(t, models.JobStatusOrganized, job.lifecycle.GetJobStatus())
	})

	t.Run("MarkCompleted is no-op when already Reverted", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetJobStatus(models.JobStatusReverted)
		job.lifecycle.MarkCompleted()
		assert.Equal(t, models.JobStatusReverted, job.lifecycle.GetJobStatus())
	})

	t.Run("MarkFailed is no-op when already Completed", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetJobStatus(models.JobStatusCompleted)
		job.lifecycle.MarkFailed()
		assert.Equal(t, models.JobStatusCompleted, job.lifecycle.GetJobStatus())
	})

	t.Run("MarkCancelled is no-op when already Organized", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetJobStatus(models.JobStatusOrganized)
		job.lifecycle.MarkCancelled()
		assert.Equal(t, models.JobStatusOrganized, job.lifecycle.GetJobStatus())
	})

	t.Run("MarkOrganized is no-op when already Organized", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetJobStatus(models.JobStatusOrganized)
		job.lifecycle.MarkOrganized()
		assert.Equal(t, models.JobStatusOrganized, job.lifecycle.GetJobStatus())
	})

	t.Run("MarkOrganized is no-op when already Reverted", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetJobStatus(models.JobStatusReverted)
		job.lifecycle.MarkOrganized()
		assert.Equal(t, models.JobStatusReverted, job.lifecycle.GetJobStatus())
	})

	t.Run("MarkReverted is no-op when already Reverted", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetJobStatus(models.JobStatusReverted)
		job.lifecycle.MarkReverted()
		assert.Equal(t, models.JobStatusReverted, job.lifecycle.GetJobStatus())
	})
}

// ---------------------------------------------------------------------------
// result_tracker.go: GetProvenance, UpdateMovie, GetResults, setFileMatchInfo,
// cloneResults, updateProgressFromCounters, recalculateProgress, MarkExcluded,

func TestResultTracker_UpdateMovie(t *testing.T) {
	t.Run("updates movie and movieID", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "OLD-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "OLD-001"},
		})

		newMovie := &models.Movie{ID: "NEW-001", Title: "New"}
		err := job.results.UpdateMovie("file1.mp4", newMovie)
		require.NoError(t, err)

		result := job.results.Results["file1.mp4"]
		assert.Equal(t, "NEW-001", result.Movie.ID)
		assert.Equal(t, "NEW-001", result.FileMatchInfo.MovieID)
	})
}

func TestResultTracker_GetResults(t *testing.T) {
	t.Run("returns value copies not pointers", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
		})

		results := job.results.GetResults()
		assert.Len(t, results, 1)
		// GetResults returns []MovieResult (value), not []*MovieResult
		assert.Equal(t, "ABC-001", results[0].FileMatchInfo.MovieID)
	})

	t.Run("skips nil results", func(t *testing.T) {
		rt := newResultTrackerFromState(&resultTrackerState{
			Results: map[string]*MovieResult{
				"file1.mp4": {Status: models.JobStatusCompleted},
				"file2.mp4": nil,
			},
		})
		results := rt.GetResults()
		assert.Len(t, results, 1)
	})
}

func TestResultTracker_setFileMatchInfo(t *testing.T) {
	t.Run("merges file match info", func(t *testing.T) {
		rt := newResultTrackerFromState(&resultTrackerState{
			FileMatchInfo: map[string]models.FileMatchInfo{
				"file1.mp4": {MovieID: "OLD-001"},
			},
		})

		rt.setFileMatchInfo(map[string]models.FileMatchInfo{
			"file2.mp4": {MovieID: "NEW-002"},
			"file1.mp4": {MovieID: "UPDATED-001"},
		})

		assert.Equal(t, "UPDATED-001", rt.FileMatchInfo["file1.mp4"].MovieID)
		assert.Equal(t, "NEW-002", rt.FileMatchInfo["file2.mp4"].MovieID)
	})
}

func TestResultTracker_recalculateProgress(t *testing.T) {
	t.Run("counts completed and failed from results", func(t *testing.T) {
		rt := newResultTrackerFromState(&resultTrackerState{
			TotalFiles: 3,
			Results: map[string]*MovieResult{
				"f1": {Status: models.JobStatusCompleted},
				"f2": {Status: models.JobStatusFailed},
				"f3": {Status: models.JobStatusRunning},
			},
		})
		rt.recalculateProgress()
		assert.Equal(t, 1, rt.Completed)
		assert.Equal(t, 1, rt.Failed)
		assert.InDelta(t, 66.67, rt.Progress, 0.1) // (1+1)/3 * 100
	})

	t.Run("sets 100% when TotalFiles is 0", func(t *testing.T) {
		rt := newResultTrackerFromState(&resultTrackerState{TotalFiles: 0})
		rt.recalculateProgress()
		assert.Equal(t, 100.0, rt.Progress)
	})
}

func TestResultTracker_recalculateProgress_SkipsExcluded(t *testing.T) {
	// BUG-1 regression: stateRecalculateProgress must skip excluded files.
	// Without the fix, recalculate would re-count excluded results by Status,
	// causing Completed/Failed counters to be over-counted.
	t.Run("skips excluded files when recounting", func(t *testing.T) {
		rt := newResultTrackerFromState(&resultTrackerState{
			TotalFiles: 5,
			Excluded:   map[string]bool{"f2": true, "f5": true},
			Results: map[string]*MovieResult{
				"f1": {Status: models.JobStatusCompleted},
				"f2": {Status: models.JobStatusCompleted}, // excluded
				"f3": {Status: models.JobStatusFailed},
				"f4": {Status: models.JobStatusCompleted},
				"f5": {Status: models.JobStatusFailed}, // excluded
			},
		})
		rt.recalculateProgress()
		assert.Equal(t, 2, rt.Completed, "Completed should exclude excluded files")
		assert.Equal(t, 1, rt.Failed, "Failed should exclude excluded files")
		// Excluded files are removed from the denominator too: 3 active files
		// (f1,f3,f4) all resolved → (2 completed + 1 failed) / 3 active * 100 = 100%.
		// Using TotalFiles (5) here would falsely report 60% with everything done.
		assert.InDelta(t, 100.0, rt.Progress, 0.1)
	})
}

func TestResultTracker_MarkExcluded(t *testing.T) {
	t.Run("decrements counters when excluding completed result", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4"})
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			Status: models.JobStatusCompleted,
		})
		assert.Equal(t, 1, job.results.Completed)

		job.results.MarkExcluded("file1.mp4")
		assert.Equal(t, 0, job.results.Completed, "Completed should be decremented when excluding a completed result")
		assert.True(t, job.results.Excluded["file1.mp4"])
	})

	t.Run("decrements counters when excluding failed result", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4"})
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			Status: models.JobStatusFailed,
		})
		assert.Equal(t, 1, job.results.Failed)

		job.results.MarkExcluded("file1.mp4")
		assert.Equal(t, 0, job.results.Failed, "Failed should be decremented when excluding a failed result")
	})

	t.Run("sets Running status to Cancelled when excluding (BUG-1)", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4"})
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			Status: models.JobStatusRunning,
		})
		assert.Equal(t, 0, job.results.Completed)
		assert.Equal(t, 0, job.results.Failed)

		job.results.MarkExcluded("file1.mp4")
		assert.True(t, job.results.Excluded["file1.mp4"])
		// After exclusion, the result status must be Cancelled, not still Running
		assert.Equal(t, models.JobStatusCancelled, job.results.Results["file1.mp4"].Status,
			"BUG-1: excluded Running file must have status set to Cancelled, not left as Running")

		// Verify snapshot also reports Cancelled, not Running
		snap := job.results.SnapshotData()
		assert.Equal(t, models.JobStatusCancelled, snap.Results["file1.mp4"].Status,
			"snapshot must show excluded Running file as Cancelled")
	})

	t.Run("does not double-decrement on second exclude", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4"})
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			Status: models.JobStatusCompleted,
		})

		job.results.MarkExcluded("file1.mp4")
		job.results.MarkExcluded("file1.mp4") // second call
		assert.Equal(t, 0, job.results.Completed, "should not double-decrement")
	})
}

func TestResultTracker_cloneResults(t *testing.T) {
	t.Run("returns deep-copied results map", func(t *testing.T) {
		rt := newResultTrackerFromState(&resultTrackerState{
			Results: map[string]*MovieResult{
				"f1": {Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{MovieID: "ABC-001"}},
			},
		})
		cloned := rt.SnapshotData().Results
		assert.Len(t, cloned, 1)
		assert.NotSame(t, rt.Results["f1"], cloned["f1"])
	})
}

// ---------------------------------------------------------------------------
// phase_interfaces.go: GetRevision, GetCurrentMovieID
// ---------------------------------------------------------------------------

func TestBatchJob_GetRevision(t *testing.T) {
	t.Run("returns revision for existing result", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			Status: models.JobStatusCompleted,
		})
		assert.Equal(t, uint64(1), job.resultIndex.GetRevision("file1.mp4"))
	})

	t.Run("returns 0 for nonexistent result", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		assert.Equal(t, uint64(0), job.resultIndex.GetRevision("nonexistent.mp4"))
	})
}

func TestBatchJob_GetCurrentMovieID(t *testing.T) {
	t.Run("returns Movie.ID when available", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "FMI-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "MOVIE-001"},
		})
		assert.Equal(t, "MOVIE-001", job.resultIndex.GetCurrentMovieID("file1.mp4"))
	})

	t.Run("falls back to FileMatchInfo.MovieID when Movie is nil", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "FMI-001"},
			Status:        models.JobStatusCompleted,
		})
		assert.Equal(t, "FMI-001", job.resultIndex.GetCurrentMovieID("file1.mp4"))
	})

	t.Run("returns empty string for nonexistent result", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		assert.Equal(t, "", job.resultIndex.GetCurrentMovieID("nonexistent.mp4"))
	})
}

// ---------------------------------------------------------------------------
// job_event.go: Send with full buffer (drop-oldest), Close idempotent,
// unsubscribe with closed broadcaster
// ---------------------------------------------------------------------------

func TestJobEventBroadcaster_SendWithFullBuffer(t *testing.T) {
	t.Run("drops oldest event when buffer is full", func(t *testing.T) {
		b := newJobEventBroadcaster()
		defer b.Close()

		// Create subscriber with small buffer
		ch := make(chan JobEvent, 2)
		b.mu.Lock()
		b.subscribers = append(b.subscribers, ch)
		b.mu.Unlock()

		// Fill buffer beyond capacity
		for i := 0; i < 5; i++ {
			b.Send(JobEvent{JobID: models.MustJobID(fmt.Sprintf("j%d", i))})
		}

		// Should be able to read at least some events
		eventCount := 0
		for {
			select {
			case <-ch:
				eventCount++
			default:
				goto done
			}
		}
	done:
		assert.Greater(t, eventCount, 0, "should receive some events even when buffer overflows")
	})
}

func TestJobEventBroadcaster_CloseIdempotent(t *testing.T) {
	b := newJobEventBroadcaster()

	// Close twice — should not panic
	b.Close()
	b.Close()

	assert.True(t, b.closed)
}

func TestJobEventBroadcaster_UnsubscribeAfterBroadcasterClose(t *testing.T) {
	b := newJobEventBroadcaster()
	sub := b.Subscribe()

	b.Close()

	// Unsubscribe after broadcaster close — should return false
	ch := sub.(*channelSubscriber).ch
	result := b.unsubscribe(ch)
	assert.False(t, result, "unsubscribe should return false after broadcaster is closed")
}

// ---------------------------------------------------------------------------
// job_store_persist.go: snapshotForPersist, reconstructBatchJob
// ---------------------------------------------------------------------------

func TestJobStore_SnapshotForPersist_DeletedJob(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.lifecycle.deleted = true

	dbJob, ok := snapshotForPersist(job)
	assert.False(t, ok)
	assert.Nil(t, dbJob)
}

func TestJobStore_SnapshotForPersist_Success(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
	})
	job.cfg.destination = "/output"
	job.cfg.update = true
	job.cfg.operationMode = operationmode.OperationModeOrganize

	dbJob, ok := snapshotForPersist(job)
	require.True(t, ok)
	require.NotNil(t, dbJob)
	assert.Equal(t, job.ID.String(), dbJob.ID)
	assert.Equal(t, models.JobStatusPending, dbJob.Status)
	assert.Equal(t, "/output", dbJob.Destination)
	assert.True(t, dbJob.Update)
	assert.Equal(t, operationmode.OperationModeOrganize, dbJob.OperationModeOverride)

	// Verify JSON fields are non-empty
	assert.NotEmpty(t, dbJob.Files)
	assert.NotEmpty(t, dbJob.Results)
	assert.NotEmpty(t, dbJob.Excluded)
	assert.NotEmpty(t, dbJob.FileMatchInfo)

	// Verify Results is valid envelope JSON
	var envelope JobResultsEnvelope
	err := json.Unmarshal([]byte(dbJob.Results), &envelope)
	require.NoError(t, err)
	assert.Contains(t, envelope.Domain, "file1.mp4")
}

func TestJobStore_ReconstructBatchJob_EnvelopeFormat(t *testing.T) {
	t.Run("parses envelope format with domain key", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		envelope := JobResultsEnvelope{
			Domain: map[string]*MovieResult{
				"/path/file1.mp4": {
					FileMatchInfo: models.FileMatchInfo{Path: "/path/file1.mp4", MovieID: "ABC-001"},
					Status:        models.JobStatusCompleted,
					Movie:         &models.Movie{ID: "ABC-001"},
				},
			},
			Provenance: map[string]*ProvenanceData{
				"/path/file1.mp4": {FieldSources: map[string]string{"title": "r18dev"}},
			},
		}
		resultsJSON, _ := json.Marshal(envelope)

		dbJob := &models.Job{
			ID:      "test-envelope",
			Status:  models.JobStatusCompleted,
			Results: string(resultsJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		require.NotNil(t, result)
		assert.Contains(t, result.results.Results, "/path/file1.mp4")
		assert.Equal(t, "ABC-001", result.results.Results["/path/file1.mp4"].FileMatchInfo.MovieID)

		// Provenance should be loaded
		assert.NotNil(t, result.results.Provenance["/path/file1.mp4"])
	})
}

func TestJobStore_ReconstructBatchJob_InvalidExcludedJSON(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

	dbJob := &models.Job{
		ID:       "test-invalid-excluded",
		Status:   models.JobStatusPending,
		Excluded: "not valid json",
	}

	result := jq.reconstructBatchJob(dbJob)
	assert.NotNil(t, result)
	// Should not panic, excluded should be empty map
	assert.Empty(t, result.results.Excluded)
}

func TestJobStore_ReconstructBatchJob_InvalidFileMatchInfoJSON(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

	dbJob := &models.Job{
		ID:            "test-invalid-fmi",
		Status:        models.JobStatusPending,
		FileMatchInfo: "not valid json",
	}

	result := jq.reconstructBatchJob(dbJob)
	assert.NotNil(t, result)
	assert.Empty(t, result.results.FileMatchInfo)
}

func TestJobStore_PersistJobByID_NotFound(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	// Should not panic for nonexistent job
	jq.PersistJobByID("nonexistent-id")
}

// ---------------------------------------------------------------------------
// fs_case.go: isCaseInsensitiveFS with MemMapFs
// ---------------------------------------------------------------------------

func TestFSCaseCache_IsCaseInsensitiveFS_MemMapFs(t *testing.T) {
	t.Run("MemMapFs is case-sensitive (returns false)", func(t *testing.T) {
		// MemMapFs is case-sensitive by default
		cache := NewFSCaseCache(afero.NewMemMapFs())
		tmpDir := t.TempDir()

		// Create the temp dir in the MemMapFs
		cache.fs.MkdirAll(tmpDir, 0755)

		result := cache.isCaseInsensitiveFS(tmpDir)
		assert.False(t, result, "MemMapFs should be case-sensitive")
	})
}

func TestFSCaseCache_IsCaseInsensitive_NilFS(t *testing.T) {
	cache := NewFSCaseCache(nil)
	tmpDir := t.TempDir()

	// This will use the OS filesystem
	result := cache.IsCaseInsensitive(tmpDir)
	_ = result // just verifying it doesn't panic
}

// ---------------------------------------------------------------------------
// BatchJob Run method
// ---------------------------------------------------------------------------

func TestBatchJob_Run_MissingWorkflow(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	err := newStandaloneJobFromBatchJob(job).Run(context.Background())
	assert.Error(t, err)
	// Per N-7: validateWFFn removed — WF validation now happens in jobController.StartScrape.
	// When both WF and BatchCfg are missing, BatchCfg validation fires first.
	assert.Contains(t, err.Error(), "cannot run")
}

func TestBatchJob_Run_MissingBatchConfig(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{WF: &stubWorkflow{}},
	})
	err := newStandaloneJobFromBatchJob(job).Run(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "batch config not configured")
}

// ---------------------------------------------------------------------------
// createJob with JobConfig.ID
// ---------------------------------------------------------------------------

func TestJobStore_CreateJob_WithPreGeneratedID(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"}, &JobConfig{
		ID: "custom-job-id",
	})
	assert.Equal(t, models.JobID("custom-job-id"), job.ID)
}

// TestUpdateFileResult_SkipsExcluded verifies BUG-4 fix:
// When a file is excluded via MarkExcluded while Running, a subsequent
// UpdateFileResult with Completed/Failed must NOT increment the counter.
// Without the fix, Completed would be incremented even though the file is
// excluded, inflating the progress percentage.
func TestUpdateFileResult_SkipsExcluded(t *testing.T) {
	t.Run("UpdateFileResult does not increment Completed for excluded file", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4", "file3.mp4"})

		// file1 completes normally
		job.results.UpdateFileResult("file1.mp4", &MovieResult{Status: models.JobStatusCompleted})
		assert.Equal(t, 1, job.results.Completed)

		// file2 is Running, then gets excluded
		job.results.UpdateFileResult("file2.mp4", &MovieResult{Status: models.JobStatusRunning})
		job.results.MarkExcluded("file2.mp4")
		assert.Equal(t, 1, job.results.Completed, "MarkExcluded should not change Completed (Running is not counted)")
		assert.True(t, job.results.Excluded["file2.mp4"])

		// BUG-4 scenario: scrape goroutine completes file2 after exclusion
		job.results.UpdateFileResult("file2.mp4", &MovieResult{Status: models.JobStatusCompleted})
		assert.Equal(t, 1, job.results.Completed, "UpdateFileResult must NOT increment Completed for excluded file")
	})

	t.Run("UpdateFileResult does not increment Failed for excluded file", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4"})

		// file2 is Running, then gets excluded
		job.results.UpdateFileResult("file2.mp4", &MovieResult{Status: models.JobStatusRunning})
		job.results.MarkExcluded("file2.mp4")

		// scrape goroutine reports failure after exclusion
		job.results.UpdateFileResult("file2.mp4", &MovieResult{Status: models.JobStatusFailed})
		assert.Equal(t, 0, job.results.Failed, "UpdateFileResult must NOT increment Failed for excluded file")
	})

	t.Run("AtomicUpdateFileResult does not change counters for excluded file", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4"})

		// file1 completes normally
		job.results.UpdateFileResult("file1.mp4", &MovieResult{Status: models.JobStatusCompleted})
		assert.Equal(t, 1, job.results.Completed)

		// file2 completes then gets excluded (counter goes back down)
		job.results.UpdateFileResult("file2.mp4", &MovieResult{Status: models.JobStatusCompleted})
		assert.Equal(t, 2, job.results.Completed)
		job.results.MarkExcluded("file2.mp4")
		assert.Equal(t, 1, job.results.Completed)

		// AtomicUpdateFileResult on excluded file must not change counters
		err := job.results.AtomicUpdateFileResult("file2.mp4", func(r *MovieResult) (*MovieResult, error) {
			r.Status = models.JobStatusFailed
			return r, nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 1, job.results.Completed, "AtomicUpdateFileResult must NOT change Completed for excluded file")
		assert.Equal(t, 0, job.results.Failed, "AtomicUpdateFileResult must NOT increment Failed for excluded file")
	})
}

// ---------------------------------------------------------------------------
// concurrent MarkExcluded
// ---------------------------------------------------------------------------

func TestResultTracker_ConcurrentMarkExcluded(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	files := make([]string, 100)
	for i := 0; i < 100; i++ {
		files[i] = fmt.Sprintf("file%d.mp4", i)
	}
	job := jq.CreateJobBatch(files)

	// Set all results as completed
	for _, f := range files {
		job.results.UpdateFileResult(f, &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: f},
			Status:        models.JobStatusCompleted,
		})
	}

	// Concurrently exclude files
	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		go func(fp string) {
			defer wg.Done()
			job.results.MarkExcluded(fp)
		}(f)
	}
	wg.Wait()

	// All files should be excluded
	for _, f := range files {
		assert.True(t, job.results.Excluded[f], "file %s should be excluded", f)
	}
}

// ---------------------------------------------------------------------------
// NewJobStore with custom filesystem
// ---------------------------------------------------------------------------

func TestNewJobStore_WithCustomFilesystem(t *testing.T) {
	fs := afero.NewMemMapFs()
	jq := NewJobStore(nil, nil, nil, "", nil, fs)
	assert.Equal(t, fs, jq.fs)
}

func TestNewJobStore_WithNilFilesystem(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	// Should use OS fs — just verify no panic
	assert.NotNil(t, jq.fs)
}

// TestMarkExcluded_RecalculateProgress_NoInconsistency verifies BUG-1 fix:
// After MarkExcluded, a subsequent stateRecalculateProgress (triggered by
// CommitResult or recalculateProgress) must not re-count excluded files.
func TestMarkExcluded_RecalculateProgress_NoInconsistency(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4", "file3.mp4"})

	// All 3 files complete → Completed=3, Progress=100%
	job.results.UpdateFileResult("file1.mp4", &MovieResult{Status: models.JobStatusCompleted})
	job.results.UpdateFileResult("file2.mp4", &MovieResult{Status: models.JobStatusCompleted})
	job.results.UpdateFileResult("file3.mp4", &MovieResult{Status: models.JobStatusCompleted})
	assert.Equal(t, 3, job.results.Completed)
	assert.InDelta(t, 100.0, job.results.Progress, 0.01)

	// Exclude file2 → Completed=2, Progress=66.7%
	job.results.MarkExcluded("file2.mp4")
	assert.Equal(t, 2, job.results.Completed, "MarkExcluded should decrement Completed")
	assert.True(t, job.results.Excluded["file2.mp4"])

	// Recalculate progress (simulates CommitResult or recalculateProgress call)
	job.results.recalculateProgress()
	assert.Equal(t, 2, job.results.Completed, "stateRecalculateProgress must NOT re-count excluded files")
	assert.Equal(t, 0, job.results.Failed, "stateRecalculateProgress must NOT count excluded files as failed")
	// Excluded files drop out of the denominator: 2 active files, both completed → 100%.
	expectedProgress := float64(2+0) / float64(3-1) * 100
	assert.InDelta(t, expectedProgress, job.results.Progress, 0.01, "Progress must reflect only non-excluded results")

	// CommitResult on another file triggers stateRecalculateProgress internally
	err := job.results.CommitResult("file3.mp4", &MovieResult{
		Status:   models.JobStatusFailed,
		Revision: 2,
	}, 1)
	assert.NoError(t, err)
	assert.Equal(t, 1, job.results.Completed, "after CommitResult changing file3 to failed, Completed should be 1 (file1 only)")
	assert.Equal(t, 1, job.results.Failed, "Failed should be 1 (file3), not counting excluded file2")
}
