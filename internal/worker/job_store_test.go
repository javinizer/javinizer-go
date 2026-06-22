package worker

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestJobStore_CreateGetDeleteList(t *testing.T) {
	t.Run("Create and get job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4", "file2.mkv", "file3.avi"}

		job := jq.CreateJobBatch(files)

		// Verify job creation
		assert.NotEmpty(t, job.ID)
		assert.Equal(t, models.JobStatusPending, job.lifecycle.Status)
		assert.Equal(t, 3, job.results.TotalFiles)
		assert.Equal(t, files, job.results.Files)
		assert.NotNil(t, job.results.Results)
		assert.Empty(t, job.results.Results)
		assert.Equal(t, 0, job.results.Completed)
		assert.Equal(t, 0, job.results.Failed)
		assert.Equal(t, 0.0, job.results.Progress)
		assert.False(t, job.StartedAt.IsZero())
		assert.Nil(t, job.lifecycle.CompletedAt)

		// Retrieve the job
		retrieved, ok := jq.GetJob(job.ID.String())
		require.True(t, ok, "Job should exist")
		assert.Equal(t, job.ID, retrieved.ID)
		assert.Equal(t, job.results.TotalFiles, retrieved.TotalFiles)
	})

	t.Run("Get non-existent job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)

		retrieved, ok := jq.GetJob("non-existent-id")
		assert.False(t, ok, "Job should not exist")
		assert.Nil(t, retrieved)
	})

	t.Run("Delete job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4"}

		job := jq.CreateJobBatch(files)
		jobID := job.ID

		// Verify job exists
		_, ok := jq.GetJob(jobID.String())
		require.True(t, ok, "Job should exist before deletion")

		// Delete job
		jq.DeleteJob(jobID.String())

		// Verify job is deleted
		_, ok = jq.GetJob(jobID.String())
		assert.False(t, ok, "Job should not exist after deletion")
	})

	t.Run("List jobs", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)

		// Initially empty
		jobs := jq.ListJobs()
		assert.Empty(t, jobs)

		// Create multiple jobs
		job1 := jq.CreateJobBatch([]string{"file1.mp4"})
		job2 := jq.CreateJobBatch([]string{"file2.mkv", "file3.avi"})
		job3 := jq.CreateJobBatch([]string{"file4.mp4"})

		// List should contain all jobs
		jobs = jq.ListJobs()
		assert.Len(t, jobs, 3)

		// Verify all job IDs are present
		jobIDs := make(map[string]bool)
		for _, job := range jobs {
			jobIDs[job.ID.String()] = true
		}
		assert.True(t, jobIDs[job1.ID.String()], "Job1 should be in list")
		assert.True(t, jobIDs[job2.ID.String()], "Job2 should be in list")
		assert.True(t, jobIDs[job3.ID.String()], "Job3 should be in list")

		// Delete one job
		jq.DeleteJob(job2.ID.String())

		// List should have 2 jobs
		jobs = jq.ListJobs()
		assert.Len(t, jobs, 2)
	})

	t.Run("Empty files list", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{})

		assert.Equal(t, 0, job.results.TotalFiles)
		assert.Empty(t, job.results.Files)
	})
}

func TestBatchJob_UpdateFileResult(t *testing.T) {
	t.Run("Update single file result", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4", "file2.mkv", "file3.avi"}
		job := jq.CreateJobBatch(files)

		now := time.Now()
		result := &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "IPX-123"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
			EndedAt:       &now,
		}

		job.results.UpdateFileResult("file1.mp4", result)

		// Verify result is stored
		assert.Len(t, job.results.Results, 1)
		assert.Equal(t, result, job.results.Results["file1.mp4"])

		// Verify counters
		assert.Equal(t, 1, job.results.Completed)
		assert.Equal(t, 0, job.results.Failed)
		assert.InDelta(t, 33.33, job.results.Progress, 0.1) // 1/3 * 100
	})

	t.Run("Update multiple file results with mixed status", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4", "file2.mkv", "file3.avi", "file4.mp4"}
		job := jq.CreateJobBatch(files)

		now := time.Now()

		// Complete first file
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		})
		assert.Equal(t, 1, job.results.Completed)
		assert.Equal(t, 0, job.results.Failed)
		assert.Equal(t, 25.0, job.results.Progress)

		// Complete second file
		job.results.UpdateFileResult("file2.mkv", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file2.mkv"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		})
		assert.Equal(t, 2, job.results.Completed)
		assert.Equal(t, 0, job.results.Failed)
		assert.Equal(t, 50.0, job.results.Progress)

		// Fail third file
		job.results.UpdateFileResult("file3.avi", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file3.avi"},
			Status:        models.JobStatusFailed,
			Error:         "scraper error",
			StartedAt:     now,
		})
		assert.Equal(t, 2, job.results.Completed)
		assert.Equal(t, 1, job.results.Failed)
		assert.Equal(t, 75.0, job.results.Progress) // (2+1)/4 * 100

		// Complete fourth file
		job.results.UpdateFileResult("file4.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file4.mp4"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		})
		assert.Equal(t, 3, job.results.Completed)
		assert.Equal(t, 1, job.results.Failed)
		assert.Equal(t, 100.0, job.results.Progress)
	})

	t.Run("Update same file result multiple times", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4"}
		job := jq.CreateJobBatch(files)

		now := time.Now()

		// Initially running
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
			Status:        models.JobStatusRunning,
			StartedAt:     now,
		})
		assert.Equal(t, 0, job.results.Completed)
		assert.Equal(t, 0, job.results.Failed)
		assert.Equal(t, 0.0, job.results.Progress)

		// Then completed
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "IPX-123"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		})
		assert.Equal(t, 1, job.results.Completed)
		assert.Equal(t, 0, job.results.Failed)
		assert.Equal(t, 100.0, job.results.Progress)

		// Verify only one result exists
		assert.Len(t, job.results.Results, 1)
	})

	t.Run("Progress calculation with pending files", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4", "file2.mkv", "file3.avi"}
		job := jq.CreateJobBatch(files)

		now := time.Now()

		// Only update 1 out of 3 files
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		})

		// Progress should be 33.33% (1/3), not 100%
		assert.InDelta(t, 33.33, job.results.Progress, 0.1)
		assert.Equal(t, 1, job.results.Completed)
		assert.Equal(t, 2, job.results.TotalFiles-job.results.Completed-job.results.Failed) // 2 pending
	})
}

func TestBatchJob_StatusTransitions(t *testing.T) {
	t.Run("MarkStarted", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		assert.Equal(t, models.JobStatusPending, job.lifecycle.Status)
		initialStartTime := job.StartedAt

		time.Sleep(10 * time.Millisecond) // Ensure time difference

		job.controller.markStarted(models.JobStatusPending)

		assert.Equal(t, models.JobStatusRunning, job.lifecycle.Status)
		assert.True(t, job.StartedAt.After(initialStartTime), "StartedAt should be updated")
		assert.Nil(t, job.lifecycle.CompletedAt)
	})

	t.Run("MarkCompleted", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.controller.markStarted(models.JobStatusPending)

		beforeCompletion := time.Now()
		job.lifecycle.MarkCompleted()
		afterCompletion := time.Now()

		assert.Equal(t, models.JobStatusCompleted, job.lifecycle.Status)
		assert.Equal(t, 100.0, job.results.Progress)
		require.NotNil(t, job.lifecycle.CompletedAt)
		assert.True(t, job.lifecycle.CompletedAt.After(beforeCompletion) || job.lifecycle.CompletedAt.Equal(beforeCompletion))
		assert.True(t, job.lifecycle.CompletedAt.Before(afterCompletion) || job.lifecycle.CompletedAt.Equal(afterCompletion))
	})

	t.Run("MarkFailed", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.controller.markStarted(models.JobStatusPending)

		beforeFailure := time.Now()
		job.lifecycle.MarkFailed()
		afterFailure := time.Now()

		assert.Equal(t, models.JobStatusFailed, job.lifecycle.Status)
		require.NotNil(t, job.lifecycle.CompletedAt)
		assert.True(t, job.lifecycle.CompletedAt.After(beforeFailure) || job.lifecycle.CompletedAt.Equal(beforeFailure))
		assert.True(t, job.lifecycle.CompletedAt.Before(afterFailure) || job.lifecycle.CompletedAt.Equal(afterFailure))
	})

	t.Run("MarkCancelled", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.controller.markStarted(models.JobStatusPending)

		beforeCancellation := time.Now()
		job.lifecycle.MarkCancelled()
		afterCancellation := time.Now()

		assert.Equal(t, models.JobStatusCancelled, job.lifecycle.Status)
		require.NotNil(t, job.lifecycle.CompletedAt)
		assert.True(t, job.lifecycle.CompletedAt.After(beforeCancellation) || job.lifecycle.CompletedAt.Equal(beforeCancellation))
		assert.True(t, job.lifecycle.CompletedAt.Before(afterCancellation) || job.lifecycle.CompletedAt.Equal(afterCancellation))
	})

	t.Run("MarkReverted", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.controller.markStarted(models.JobStatusPending)
		job.lifecycle.MarkCompleted()
		job.lifecycle.MarkOrganized()

		beforeReverted := time.Now()
		job.lifecycle.MarkReverted()
		afterReverted := time.Now()

		assert.Equal(t, models.JobStatusReverted, job.lifecycle.Status)
		require.NotNil(t, job.lifecycle.RevertedAt)
		assert.True(t, job.lifecycle.RevertedAt.After(beforeReverted) || job.lifecycle.RevertedAt.Equal(beforeReverted))
		assert.True(t, job.lifecycle.RevertedAt.Before(afterReverted) || job.lifecycle.RevertedAt.Equal(afterReverted))
	})

	t.Run("Full workflow: pending -> running -> completed", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4", "file2.mkv"}
		job := jq.CreateJobBatch(files)

		// Start as pending
		assert.Equal(t, models.JobStatusPending, job.lifecycle.Status)

		// Mark as running
		job.controller.markStarted(models.JobStatusPending)
		assert.Equal(t, models.JobStatusRunning, job.lifecycle.Status)
		assert.Nil(t, job.lifecycle.CompletedAt)

		// Process files
		now := time.Now()
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		})
		job.results.UpdateFileResult("file2.mkv", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file2.mkv"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		})

		// Mark as completed
		job.lifecycle.MarkCompleted()
		assert.Equal(t, models.JobStatusCompleted, job.lifecycle.Status)
		assert.NotNil(t, job.lifecycle.CompletedAt)
		assert.Equal(t, 100.0, job.results.Progress)
	})

	t.Run("Revert workflow: organized -> reverted", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.controller.markStarted(models.JobStatusPending)
		job.lifecycle.MarkCompleted()
		job.lifecycle.MarkOrganized()

		assert.Equal(t, models.JobStatusOrganized, job.lifecycle.Status)

		// Done channel should be closed after MarkOrganized
		select {
		case <-job.lifecycle.done:
		default:
			t.Fatal("Done channel should be closed after MarkOrganized")
		}

		job.lifecycle.MarkReverted()
		assert.Equal(t, models.JobStatusReverted, job.lifecycle.Status)
		require.NotNil(t, job.lifecycle.RevertedAt)
	})
}

func TestBatchJob_GetStatus(t *testing.T) {
	t.Run("Returns copy with all fields", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4", "file2.mkv"}
		job := jq.CreateJobBatch(files)
		job.controller.markStarted(models.JobStatusPending)

		now := time.Now()
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		})

		status := job.GetStatus()

		// Verify all fields are copied
		assert.Equal(t, job.ID, status.ID)
		assert.Equal(t, job.lifecycle.Status, status.Status)
		assert.Equal(t, job.results.TotalFiles, status.TotalFiles)
		assert.Equal(t, job.results.Completed, status.Completed)
		assert.Equal(t, job.results.Failed, status.Failed)
		assert.Equal(t, job.results.Files, status.Files)
		assert.Equal(t, job.results.Progress, status.Progress)
		assert.Equal(t, job.StartedAt, status.StartedAt)
		assert.Len(t, status.Results, 1)
	})

	t.Run("Deep copy of MovieResults - map and MovieResults are independent", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4", "file2.mkv"}
		job := jq.CreateJobBatch(files)

		now := time.Now()
		result1 := &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "IPX-123"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		}
		job.results.UpdateFileResult("file1.mp4", result1)

		// Get status copy
		status := job.GetStatus()

		// Verify MovieResult objects are NOT shared (deep copy)
		// MovieResults should be independent to prevent concurrent mutations
		assert.NotSame(t, job.results.Results["file1.mp4"], status.Results["file1.mp4"],
			"MovieResult pointers should be different (deep copy)")

		// Verify fields are equal but independent
		assert.Equal(t, job.results.Results["file1.mp4"].FileMatchInfo.MovieID, status.Results["file1.mp4"].FileMatchInfo.MovieID,
			"MovieResult fields should be equal")

		// Modifying a MovieResult in the copy should NOT affect original
		status.Results["file1.mp4"].FileMatchInfo.MovieID = "MODIFIED-999"
		assert.Equal(t, "IPX-123", job.results.Results["file1.mp4"].FileMatchInfo.MovieID,
			"Original MovieResult should remain unchanged (deep copy)")
		assert.Equal(t, "MODIFIED-999", status.Results["file1.mp4"].FileMatchInfo.MovieID,
			"Copy MovieResult should be modified")

		// Adding new entries to the copy's map doesn't affect original
		status.Results["file2.mkv"] = &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file2.mkv"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		}
		assert.Len(t, status.Results, 2, "Copy should have 2 results")
		assert.Len(t, job.results.Results, 1, "Original should still have 1 result (map is independent)")
	})

	t.Run("Copies CompletedAt correctly when nil", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		status := job.GetStatus()

		assert.Nil(t, job.lifecycle.CompletedAt)
		assert.Nil(t, status.CompletedAt)
	})

	t.Run("Copies CompletedAt correctly when set", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.lifecycle.MarkCompleted()

		status := job.GetStatus()

		require.NotNil(t, job.lifecycle.CompletedAt)
		require.NotNil(t, status.CompletedAt)
		assert.Equal(t, *job.lifecycle.CompletedAt, *status.CompletedAt)

		// Verify they're separate pointers
		assert.NotSame(t, job.lifecycle.CompletedAt, status.CompletedAt, "CompletedAt should be copied, not shared")
	})

	t.Run("Empty results map is copied correctly", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		status := job.GetStatus()

		assert.Empty(t, status.Results)
		assert.NotNil(t, status.Results)
	})
}

// TestConcurrent_GetStatusAndUpdateFileResult validates thread-safe snapshot access
// This test catches race conditions where handlers read job state while workers update it
func TestConcurrent_GetStatusAndUpdateFileResult(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mkv", "file3.avi"})

	now := time.Now()
	// Initialize with a file result
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "IPX-100"},
		Status:        models.JobStatusRunning,
		StartedAt:     now,
	})

	// Simulate worker updating job results concurrently
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			// Rapidly update multiple file results
			job.results.UpdateFileResult("file1.mp4", &MovieResult{
				FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "IPX-" + fmt.Sprintf("%d", i)},
				Status:        models.JobStatusRunning,
				StartedAt:     now,
			})
			job.results.UpdateFileResult("file2.mkv", &MovieResult{
				FileMatchInfo: models.FileMatchInfo{Path: "file2.mkv", MovieID: "IPX-" + fmt.Sprintf("%d", i+1000)},
				Status:        models.JobStatusCompleted,
				StartedAt:     now,
			})
		}
		close(done)
	}()

	// Simulate handler reading job state concurrently (the safe pattern)
	for i := 0; i < 1000; i++ {
		// GetStatus() returns a thread-safe snapshot
		status := job.GetStatus()

		// Iterate over results (safe because it's a copy)
		for filePath, result := range status.Results {
			// Verify basic invariants
			assert.NotEmpty(t, filePath)
			if result != nil {
				assert.NotEmpty(t, result.FileMatchInfo.Path)
			}
		}
	}

	<-done
}

// TestConcurrent_DirectMapAccessIsUnsafe demonstrates the race condition
// This test would fail with -race if we directly accessed job.results.Results without GetStatus()
// Run with: go test -race -run TestConcurrent_DirectMapAccessIsUnsafe
func TestConcurrent_DirectMapAccessIsUnsafe(t *testing.T) {
	t.Skip("This test demonstrates unsafe pattern - skip to avoid race detector failures")

	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	now := time.Now()
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "IPX-1"},
		Status:        models.JobStatusRunning,
		StartedAt:     now,
	})

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			job.results.UpdateFileResult("file1.mp4", &MovieResult{
				FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: fmt.Sprintf("IPX-%d", i)},
				Status:        models.JobStatusRunning,
				StartedAt:     now,
			})
		}
		close(done)
	}()

	// UNSAFE: Direct map access without GetStatus() - WOULD FAIL WITH -race
	for i := 0; i < 1000; i++ {
		// This would cause: fatal error: concurrent map iteration and map write
		for filePath := range job.results.Results {
			_ = filePath
		}
	}

	<-done
}

// TestBatchJob_PointerFieldIndependence validates that pointer fields are deep copied
// This ensures modifying pointer fields in the snapshot doesn't affect the live job
func TestBatchJob_PointerFieldIndependence(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	// Create MovieResult with pointer fields
	originalTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	originalError := "poster download failed"
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "IPX-100"},
		Status:        models.JobStatusCompleted,
		StartedAt:     time.Now(),
		EndedAt:       &originalTime,
		OrchestrationState: models.OrchestrationState{
			PosterError: &originalError,
		},
	})

	// Get snapshot
	snapshot := job.GetStatus()

	// Verify initial values in snapshot
	assert.NotNil(t, snapshot.Results["file1.mp4"])
	assert.NotNil(t, snapshot.Results["file1.mp4"].EndedAt)
	assert.NotNil(t, snapshot.Results["file1.mp4"].PosterError)
	assert.Equal(t, originalTime, *snapshot.Results["file1.mp4"].EndedAt)
	assert.Equal(t, originalError, *snapshot.Results["file1.mp4"].PosterError)

	// Modify pointer fields in the snapshot
	newTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	newError := "different error"
	snapshot.Results["file1.mp4"].EndedAt = &newTime
	snapshot.Results["file1.mp4"].PosterError = &newError

	// Get fresh snapshot to verify original is unchanged
	freshSnapshot := job.GetStatus()
	assert.NotNil(t, freshSnapshot.Results["file1.mp4"])
	assert.NotNil(t, freshSnapshot.Results["file1.mp4"].EndedAt)
	assert.NotNil(t, freshSnapshot.Results["file1.mp4"].PosterError)

	// Verify original values are preserved (not affected by first snapshot modifications)
	assert.Equal(t, originalTime, *freshSnapshot.Results["file1.mp4"].EndedAt, "EndedAt should not be affected by snapshot modification")
	assert.Equal(t, originalError, *freshSnapshot.Results["file1.mp4"].PosterError, "PosterError should not be affected by snapshot modification")

	// Verify modified snapshot has new values
	assert.Equal(t, newTime, *snapshot.Results["file1.mp4"].EndedAt)
	assert.Equal(t, newError, *snapshot.Results["file1.mp4"].PosterError)
}

func TestJobStore_GetJobForEdit_returns_existing(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	editable, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)
	require.NotNil(t, editable)

	assert.Equal(t, job.ID.String(), editable.GetID())
	// IsJobActive is concrete-type-only; verify via status snapshot
	status := editable.GetStatus()
	assert.True(t, status.Status == models.JobStatusPending || status.Status == models.JobStatusCompleted)
}

func TestJobStore_GetJobForEdit_returns_nil_for_nonexistent(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)

	editable, ok := jq.GetJobForEdit("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, editable)
}

func TestJobStore_GetJobForEdit(t *testing.T) {
	t.Run("returns EditableJob for existing job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		editable, ok := jq.GetJobForEdit(job.ID.String())
		require.True(t, ok)
		require.NotNil(t, editable)

		// Verify EditableJob has read + edit + persist + lookup methods
		assert.Equal(t, job.ID.String(), editable.GetID())
		status := editable.GetStatus()
		assert.True(t, status.Status == models.JobStatusPending || status.Status == models.JobStatusCompleted)

		// Verify lookup methods are available
		paths := editable.FindFilePathsForMovieID("ABC-001")
		_ = paths // empty slice, just verifying no panic
	})

	t.Run("returns nil and false for nonexistent job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)

		editable, ok := jq.GetJobForEdit("nonexistent")
		assert.False(t, ok)
		assert.Nil(t, editable)
	})
}

func TestJobStore_GetJobForControl(t *testing.T) {
	t.Run("returns ControlledJob for existing job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		controlled, ok := jq.GetJobForControl(job.ID.String())
		require.True(t, ok)
		require.NotNil(t, controlled)

		// Verify ControlledJob has read + phase control + cancel methods
		assert.Equal(t, job.ID.String(), controlled.GetID())
		status := controlled.GetStatus()
		assert.True(t, status.Status == models.JobStatusPending || status.Status == models.JobStatusCompleted)

		// Verify cancel is available (from JobCanceller)
		controlled.Cancel()
	})

	t.Run("returns nil and false for nonexistent job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)

		controlled, ok := jq.GetJobForControl("nonexistent")
		assert.False(t, ok)
		assert.Nil(t, controlled)
	})
}

// TestBatchJob_AtomicUpdateFileResult tests atomic file result updates
func TestBatchJob_AtomicUpdateFileResult(t *testing.T) {
	t.Run("atomic update with update function", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		now := time.Now()
		initial := &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "IPX-100"},
			Status:        models.JobStatusRunning,
			StartedAt:     now,
		}
		job.results.UpdateFileResult("file1.mp4", initial)

		// Atomic update function
		err := job.results.AtomicUpdateFileResult("file1.mp4", func(current *MovieResult) (*MovieResult, error) {
			// Create updated result
			updated := *current
			updated.FileMatchInfo.MovieID = "IPX-200"
			updated.Status = models.JobStatusCompleted
			return &updated, nil
		})

		assert.NoError(t, err)
		assert.Equal(t, "IPX-200", job.results.Results["file1.mp4"].FileMatchInfo.MovieID)
		assert.Equal(t, models.JobStatusCompleted, job.results.Results["file1.mp4"].Status)
	})

	t.Run("atomic update with error", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		now := time.Now()
		initial := &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "IPX-100"},
			Status:        models.JobStatusRunning,
			StartedAt:     now,
		}
		job.results.UpdateFileResult("file1.mp4", initial)

		// Atomic update that returns error
		err := job.results.AtomicUpdateFileResult("file1.mp4", func(current *MovieResult) (*MovieResult, error) {
			return nil, fmt.Errorf("update failed")
		})

		assert.Error(t, err)
		assert.Equal(t, "update failed", err.Error())
		// Original should be unchanged
		assert.Equal(t, "IPX-100", job.results.Results["file1.mp4"].FileMatchInfo.MovieID)
	})

	t.Run("atomic update on non-existent file", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		// Try to update without initial result
		err := job.results.AtomicUpdateFileResult("file1.mp4", func(current *MovieResult) (*MovieResult, error) {
			updated := *current
			updated.FileMatchInfo.MovieID = "IPX-999"
			return &updated, nil
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "file result not found")
	})
}

// TestBatchJob_SetCancelFunc tests setting cancellation function
func TestBatchJob_SetCancelFunc(t *testing.T) {
	t.Run("set and trigger cancel func", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		cancelled := false
		cancelFunc := func() {
			cancelled = true
		}

		// Set cancel function
		job.lifecycle.setCancelFunc(cancelFunc)

		// Trigger cancellation
		job.lifecycle.Cancel()

		// Verify cancel function was called
		assert.True(t, cancelled, "Cancel function should have been called")
		assert.Equal(t, models.JobStatusCancelled, job.lifecycle.Status)
	})

	t.Run("cancel without cancel func", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		// Don't set cancel func, just call Cancel
		job.lifecycle.Cancel()

		// Should still mark as cancelled
		assert.Equal(t, models.JobStatusCancelled, job.lifecycle.Status)
	})
}

// TestBatchJob_GetProgress tests progress retrieval
func TestBatchJob_GetProgress(t *testing.T) {
	t.Run("get progress at different stages", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4", "file2.mkv", "file3.avi", "file4.mp4"}
		job := jq.CreateJobBatch(files)

		// Initial progress
		progress := job.results.Progress
		assert.Equal(t, 0.0, progress)

		// Complete one file
		now := time.Now()
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		})
		progress = job.results.Progress
		assert.Equal(t, 25.0, progress)

		// Complete two more files
		job.results.UpdateFileResult("file2.mkv", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file2.mkv"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		})
		job.results.UpdateFileResult("file3.avi", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file3.avi"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		})
		progress = job.results.Progress
		assert.Equal(t, 75.0, progress)

		// Complete last file
		job.results.UpdateFileResult("file4.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file4.mp4"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		})
		progress = job.results.Progress
		assert.Equal(t, 100.0, progress)
	})
}

// TestBatchJob_ExcludeFile tests file exclusion
func TestBatchJob_ExcludeFile(t *testing.T) {
	t.Run("exclude single file", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4", "file2.mkv", "file3.avi"}
		job := jq.CreateJobBatch(files)

		// Exclude file
		excludeFile(job, "file1.mp4")

		// Verify exclusion
		assert.True(t, job.results.Excluded["file1.mp4"])
		assert.False(t, job.results.Excluded["file2.mkv"])
		assert.False(t, job.results.Excluded["file3.avi"])
	})

	t.Run("exclude multiple files", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4", "file2.mkv", "file3.avi"}
		job := jq.CreateJobBatch(files)

		// Exclude multiple files
		excludeFile(job, "file1.mp4")
		excludeFile(job, "file3.avi")

		// Verify exclusions
		assert.True(t, job.results.Excluded["file1.mp4"])
		assert.False(t, job.results.Excluded["file2.mkv"])
		assert.True(t, job.results.Excluded["file3.avi"])
	})

	t.Run("exclude same file multiple times", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4"}
		job := jq.CreateJobBatch(files)

		excludeFile(job, "file1.mp4")
		excludeFile(job, "file1.mp4")
		excludeFile(job, "file1.mp4")

		assert.True(t, job.results.Excluded["file1.mp4"])
	})

}

// TestBatchJob_IsExcluded tests exclusion checking
func TestBatchJob_IsExcluded(t *testing.T) {
	t.Run("check non-excluded file", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4", "file2.mkv"}
		job := jq.CreateJobBatch(files)

		// Files should not be excluded initially
		assert.False(t, job.results.Excluded["file1.mp4"])
		assert.False(t, job.results.Excluded["file2.mkv"])
	})

	t.Run("check excluded file", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4", "file2.mkv"}
		job := jq.CreateJobBatch(files)

		excludeFile(job, "file1.mp4")

		assert.True(t, job.results.Excluded["file1.mp4"])
		assert.False(t, job.results.Excluded["file2.mkv"])
	})

	t.Run("check non-existent file", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		files := []string{"file1.mp4"}
		job := jq.CreateJobBatch(files)

		// Non-existent file should not be excluded
		assert.False(t, job.results.Excluded["non-existent.mp4"])
	})
}

// TestMarkReverted_StatusAndTimestamp verifies MarkReverted sets status and timestamp
func TestMarkReverted_StatusAndTimestamp(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()
	job.lifecycle.MarkOrganized()

	beforeReverted := time.Now()
	job.lifecycle.MarkReverted()

	assert.Equal(t, models.JobStatusReverted, job.lifecycle.Status)
	require.NotNil(t, job.lifecycle.RevertedAt)
	assert.True(t, job.lifecycle.RevertedAt.After(beforeReverted) || job.lifecycle.RevertedAt.Equal(beforeReverted))
}

// TestMarkReverted_DoneChannelClosed verifies Done channel is closed after MarkReverted
func TestMarkReverted_DoneChannelClosed(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()
	job.lifecycle.MarkOrganized()

	// Done channel should be closed after MarkOrganized
	select {
	case <-job.lifecycle.done:
	default:
		t.Fatal("Done channel should be closed after MarkOrganized")
	}

	// MarkReverted should still work (idempotent close)
	job.lifecycle.MarkReverted()
	assert.Equal(t, models.JobStatusReverted, job.lifecycle.Status)

	// Done channel should still be closed
	select {
	case <-job.lifecycle.done:
	default:
		t.Fatal("Done channel should be closed after MarkReverted")
	}
}

// TestCleanupOldOrganizedJobs_DoesNotDeleteReverted verifies that the cleanup
// goroutine does NOT delete any jobs (it is now a no-op per D-05/HIST-11).

// TestBatchJob_PersistError tests PersistError field getter/setter and GetStatus
func TestBatchJob_PersistError(t *testing.T) {
	t.Run("GetPersistError and SetPersistError", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		assert.Empty(t, job.persistError)

		job.controller.SetPersistError("create failed: disk full")
		assert.Equal(t, "create failed: disk full", job.persistError)

		job.controller.SetPersistError("")
		assert.Empty(t, job.persistError)
	})

	t.Run("GetStatus snapshot includes PersistError", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		job.controller.SetPersistError("update failed: connection refused")
		snapshot := job.GetStatus()

		assert.Equal(t, "update failed: connection refused", snapshot.PersistError)
	})

	t.Run("PersistError in snapshot is independent copy", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		job.controller.SetPersistError("some error")
		snapshot := job.GetStatus()

		job.controller.SetPersistError("different error")
		assert.Equal(t, "some error", snapshot.PersistError, "snapshot should not be affected by later mutation")
	})

	t.Run("concurrent read/write PersistError", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := 0; i < 100; i++ {
				job.controller.SetPersistError(fmt.Sprintf("error %d", i))
			}
		}()

		for i := 0; i < 100; i++ {
			_ = job.GetPersistError()
		}

		<-done
	})
}

// TestJobStore_PersistToDatabase_SetsPersistError tests that persistToDatabase stores errors
func TestJobStore_PersistToDatabase_SetsPersistError(t *testing.T) {
	t.Run("upsert failure sets PersistError", func(t *testing.T) {
		mockRepo := mocks.NewMockJobRepositoryInterface(t)
		mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()
		mockRepo.On("Upsert", mock.Anything, mock.AnythingOfType("*models.Job")).Return(fmt.Errorf("disk full")).Once()

		jq := NewJobStore(mockRepo, nil, nil, "", nil, nil)
		job := &BatchJob{
			ID: "test-job-1",
			lifecycle: &JobLifecycle{
				Status: models.JobStatusPending,
				done:   make(chan struct{}),
			},
			results: newResultTrackerFromState(&resultTrackerState{
				TotalFiles:    1,
				Files:         []string{"file1.mp4"},
				Results:       make(map[string]*MovieResult),
				Excluded:      make(map[string]bool),
				FileMatchInfo: make(map[string]models.FileMatchInfo),
			}),
		}
		job.controller = newJobController(job)
		jq.mu.Lock()
		jq.jobs["test-job-1"] = job
		jq.mu.Unlock()

		jq.persistence.PersistJob(job)
		assert.Contains(t, job.persistError, "upsert failed")

		mockRepo.AssertExpectations(t)
	})

	t.Run("upsert failure sets PersistError (connection refused)", func(t *testing.T) {
		mockRepo := mocks.NewMockJobRepositoryInterface(t)
		mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()
		mockRepo.On("Upsert", mock.Anything, mock.AnythingOfType("*models.Job")).Return(fmt.Errorf("connection refused")).Once()

		jq := NewJobStore(mockRepo, nil, nil, "", nil, nil)
		job := &BatchJob{
			ID: "test-job-2",
			lifecycle: &JobLifecycle{
				Status: models.JobStatusRunning,
				done:   make(chan struct{}),
			},
			results: newResultTrackerFromState(&resultTrackerState{
				TotalFiles:    1,
				Files:         []string{"file1.mp4"},
				Results:       make(map[string]*MovieResult),
				Excluded:      make(map[string]bool),
				FileMatchInfo: make(map[string]models.FileMatchInfo),
			}),
		}
		job.controller = newJobController(job)
		jq.mu.Lock()
		jq.jobs["test-job-2"] = job
		jq.mu.Unlock()

		jq.persistence.PersistJob(job)
		assert.Contains(t, job.persistError, "upsert failed")

		mockRepo.AssertExpectations(t)
	})

	t.Run("success clears PersistError", func(t *testing.T) {
		mockRepo := mocks.NewMockJobRepositoryInterface(t)
		mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()
		mockRepo.On("Upsert", mock.Anything, mock.AnythingOfType("*models.Job")).Return(nil).Once()

		jq := NewJobStore(mockRepo, nil, nil, "", nil, nil)
		job := &BatchJob{
			ID: "test-job-3",
			lifecycle: &JobLifecycle{
				Status: models.JobStatusRunning,
				done:   make(chan struct{}),
			},
			results: newResultTrackerFromState(&resultTrackerState{
				TotalFiles:    1,
				Files:         []string{"file1.mp4"},
				Results:       make(map[string]*MovieResult),
				Excluded:      make(map[string]bool),
				FileMatchInfo: make(map[string]models.FileMatchInfo),
			}),
		}
		job.controller = newJobController(job)
		jq.mu.Lock()
		jq.jobs["test-job-3"] = job
		jq.mu.Unlock()

		job.controller.SetPersistError("previous error")
		jq.persistence.PersistJob(job)
		assert.Empty(t, job.persistError)

		mockRepo.AssertExpectations(t)
	})
}

// TestJobStore_PersistToDatabase_AtomicSnapshot verifies that persistToDatabase
// produces consistent snapshots even when concurrent MarkCompleted calls
// transition the job state during persistence. Before the simultaneous 3-lock
// fix, a concurrent MarkCompleted could set lifecycle.Status="completed" while
// results still showed partial progress (Progress<100), producing an
// inconsistent DB row. With the fix, all 3 sub-managers are read under
// simultaneous RLocks, so the snapshot is always from a single point in time.
func TestJobStore_PersistToDatabase_AtomicSnapshot(t *testing.T) {
	t.Run("no Status=completed with Progress<100 in persisted snapshots", func(t *testing.T) {
		// Create a job with 5 files, 3 completed, 2 pending.
		// Progress = 60%, lifecycle.Status = Running.
		job := &BatchJob{
			ID: "atomic-test-job",
			lifecycle: &JobLifecycle{
				Status: models.JobStatusRunning,
				done:   make(chan struct{}),
			},
			results: newResultTrackerFromState(&resultTrackerState{
				TotalFiles:    5,
				Completed:     3,
				Failed:        0,
				Progress:      60.0,
				Files:         []string{"f1.mp4", "f2.mp4", "f3.mp4", "f4.mp4", "f5.mp4"},
				Results:       make(map[string]*MovieResult),
				Excluded:      make(map[string]bool),
				FileMatchInfo: make(map[string]models.FileMatchInfo),
			}),
		}
		job.controller = newJobController(job)
		// Per ADR-0042: use consolidated attachLifecycleCallback
		job.attachLifecycleCallback()
		// Populate 3 completed + 2 pending results
		for i := 1; i <= 3; i++ {
			key := fmt.Sprintf("f%d.mp4", i)
			job.results.Results[key] = &MovieResult{
				FileMatchInfo: models.FileMatchInfo{Path: key, MovieID: fmt.Sprintf("ID-%d", i)},
				Status:        models.JobStatusCompleted,
				StartedAt:     time.Now(),
			}
		}
		for i := 4; i <= 5; i++ {
			key := fmt.Sprintf("f%d.mp4", i)
			job.results.Results[key] = &MovieResult{
				FileMatchInfo: models.FileMatchInfo{Path: key},
				Status:        models.JobStatusPending,
				StartedAt:     time.Now(),
			}
		}

		// Capture all persisted DB rows via mock Upsert.
		var capturedSnapshots []models.Job
		var snapshotsMu sync.Mutex

		mockRepo := mocks.NewMockJobRepositoryInterface(t)
		mockRepo.On("List", mock.Anything).Return([]models.Job{}, nil).Once()
		mockRepo.On("Upsert", mock.Anything, mock.AnythingOfType("*models.Job")).
			Run(func(args mock.Arguments) {
				dbJob := args[1].(*models.Job)
				snapshotsMu.Lock()
				// Deep copy to avoid later mutation
				snapshot := *dbJob
				capturedSnapshots = append(capturedSnapshots, snapshot)
				snapshotsMu.Unlock()
			}).
			Return(nil).
			Times(100)

		jq := NewJobStore(mockRepo, nil, nil, "", nil, nil)
		jq.mu.Lock()
		jq.jobs["atomic-test-job"] = job
		jq.mu.Unlock()

		var wg sync.WaitGroup
		wg.Add(2)

		// Goroutine 1: MarkCompleted after yielding to scheduler for better interleaving
		go func() {
			defer wg.Done()
			runtime.Gosched()
			job.lifecycle.MarkCompleted()
		}()

		// Goroutine 2: persistToDatabase 100 times, yielding each iteration
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				jq.persistence.PersistJob(job)
				runtime.Gosched()
			}
		}()

		wg.Wait()

		// Assert: every captured row with Status=completed must have Progress=100.
		// MarkCompleted forces Progress to 100 when setting Status=completed, so
		// a consistent snapshot must never show Status=completed with Progress<100.
		// MarkCompleted calls recalculateProgress() which updates Completed/Failed,
		// then forces Progress=100 if it was <100. We only assert Progress==100
		// because the forced-100 override may not match Completed/TotalFiles * 100
		// when some results are still Pending.
		snapshotsMu.Lock()
		defer snapshotsMu.Unlock()
		for i, snapshot := range capturedSnapshots {
			if snapshot.Status == models.JobStatusCompleted {
				assert.Equal(t, 100.0, snapshot.Progress,
					"captured snapshot %d: Status=completed but Progress=%.1f (should be 100)",
					i, snapshot.Progress)
			}

			// The specific bug this fix prevents: Status=completed AND Progress<100
			if snapshot.Status == models.JobStatusCompleted {
				assert.False(t, snapshot.Progress < 100.0,
					"captured snapshot %d: BUG — Status=completed with Progress=%.1f < 100 (inconsistent snapshot)",
					i, snapshot.Progress)
			}
		}

		mockRepo.AssertExpectations(t)
	})
}

func TestBatchJob_GettersSetters(t *testing.T) {
	t.Run("GetOperationModeOverride and SetOperationModeOverride", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		assert.Equal(t, operationmode.OperationMode(""), job.GetOperationModeOverride())

		job.controller.SetOperationModeOverride(operationmode.OperationModeOrganize)
		assert.Equal(t, operationmode.OperationModeOrganize, job.GetOperationModeOverride())

		job.controller.SetOperationModeOverride(operationmode.OperationMode(""))
		assert.Equal(t, operationmode.OperationModeOrganize, job.GetOperationModeOverride())
	})

	t.Run("GetDestination and SetDestination", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		assert.Empty(t, job.GetDestination())

		job.cfg.destination = "/output/dir"
		assert.Equal(t, "/output/dir", job.GetDestination())
	})

	t.Run("GetFiles returns a copy", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mkv"})

		files := job.results.GetFiles()
		assert.Equal(t, []string{"file1.mp4", "file2.mkv"}, files)

		files[0] = "modified"
		originalFiles := job.results.GetFiles()
		assert.Equal(t, "file1.mp4", originalFiles[0], "mutation of returned slice should not affect job")
	})

	t.Run("GetCompleted, GetFailed, GetTotalFiles", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mkv", "file3.avi"})

		assert.Equal(t, 0, job.results.Completed)
		assert.Equal(t, 0, job.results.Failed)
		assert.Equal(t, 3, job.results.TotalFiles)

		now := time.Now()
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
			Status:        models.JobStatusCompleted,
			StartedAt:     now,
		})
		job.results.UpdateFileResult("file2.mkv", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file2.mkv"},
			Status:        models.JobStatusFailed,
			Error:         "test error",
			StartedAt:     now,
		})

		assert.Equal(t, 1, job.results.Completed)
		assert.Equal(t, 1, job.results.Failed)
		assert.Equal(t, 3, job.results.TotalFiles)
	})

	t.Run("concurrent access without race", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mkv", "file3.avi"})

		done := make(chan struct{})

		go func() {
			defer close(done)
			for i := 0; i < 100; i++ {
				job.mu.Lock()
				job.cfg.operationMode = "organize"
				job.cfg.destination = "/test/dir"
				job.mu.Unlock()
				_ = job.GetOperationModeOverride()
				_ = job.GetDestination()
				_ = job.results.GetFiles()
				_ = job.results.Completed
				_ = job.results.Failed
				_ = job.results.TotalFiles
			}
		}()

		for i := 0; i < 100; i++ {
			_ = job.GetOperationModeOverride()
			_ = job.GetDestination()
			_ = job.results.GetFiles()
			_ = job.results.Completed
			_ = job.results.Failed
			_ = job.results.TotalFiles
		}

		<-done
	})

	t.Run("GetFileMatchInfo", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		info := models.FileMatchInfo{MovieID: "ABC-123", IsMultiPart: true, PartNumber: 1}
		job.results.FileMatchInfo["file1.mp4"] = info

		retrieved, ok := job.results.GetFileMatchInfo("file1.mp4")
		assert.True(t, ok)
		assert.Equal(t, "ABC-123", retrieved.MovieID)
		assert.True(t, retrieved.IsMultiPart)

		_, ok = job.results.GetFileMatchInfo("nonexistent.mp4")
		assert.False(t, ok)
	})
}

func TestBatchJob_GetStatusSlim(t *testing.T) {
	t.Skip("GetStatusSlim removed per ADR-0041")
	t.Run("slim snapshot has correct status fields", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mkv"})

		now := time.Now()
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-123"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-123", Title: "Test Movie"},
			StartedAt:     now,
		})

		slim := job.GetStatus()

		assert.Equal(t, job.ID, slim.ID)
		assert.Equal(t, models.JobStatusPending, slim.Status)
		assert.Equal(t, 2, slim.TotalFiles)
		assert.Equal(t, 1, slim.Completed)
		assert.Equal(t, 0, slim.Failed)
		assert.InDelta(t, 50.0, slim.Progress, 0.01)
	})

	t.Run("slim snapshot excludes Data", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		now := time.Now()
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-123"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-123", Title: "Test Movie"},
			StartedAt:     now,
		})

		slim := job.GetStatus()

		result, ok := slim.Results["file1.mp4"]
		require.True(t, ok)
		assert.Equal(t, "ABC-123", result.FileMatchInfo.MovieID)
		assert.Equal(t, models.JobStatusCompleted, result.Status)
	})

	t.Run("GetStatus still includes Data", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		now := time.Now()
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-123"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-123", Title: "Test Movie"},
			StartedAt:     now,
		})

		full := job.GetStatus()

		result, ok := full.Results["file1.mp4"]
		require.True(t, ok)
		assert.Equal(t, "ABC-123", result.FileMatchInfo.MovieID)
		assert.NotNil(t, result.Movie, "Full result should contain Movie")
		require.NotNil(t, result.Movie)
		assert.Equal(t, "ABC-123", result.Movie.ID)
		assert.Equal(t, "Test Movie", result.Movie.Title)
	})

	t.Run("TranslationWarning deep-copied in GetStatus", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		warning := "Translation failed: rate limited"
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
			Status:        models.JobStatusCompleted,
			StartedAt:     time.Now(),
			OrchestrationState: models.OrchestrationState{
				TranslationWarning: &warning,
			},
		})

		status := job.GetStatus()
		result, ok := status.Results["file1.mp4"]
		require.True(t, ok)
		require.NotNil(t, result.TranslationWarning)
		assert.Equal(t, warning, *result.TranslationWarning)

		*result.TranslationWarning = "modified"
		status2 := job.GetStatus()
		result2, ok := status2.Results["file1.mp4"]
		require.True(t, ok)
		require.NotNil(t, result2.TranslationWarning)
		assert.Equal(t, warning, *result2.TranslationWarning, "mutation of snapshot should not affect source")
	})

	t.Run("TranslationWarning deep-copied in GetStatusSlim", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		warning := "Translation failed: empty result"
		job.results.UpdateFileResult("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
			Status:        models.JobStatusCompleted,
			StartedAt:     time.Now(),
			OrchestrationState: models.OrchestrationState{
				TranslationWarning: &warning,
			},
		})

		slim := job.GetStatus()
		result, ok := slim.Results["file1.mp4"]
		require.True(t, ok)
		require.NotNil(t, result.TranslationWarning)
		assert.Equal(t, warning, *result.TranslationWarning)

		*result.TranslationWarning = "modified"
		slim2 := job.GetStatus()
		result2, ok := slim2.Results["file1.mp4"]
		require.True(t, ok)
		require.NotNil(t, result2.TranslationWarning)
		assert.Equal(t, warning, *result2.TranslationWarning, "mutation of slim snapshot should not affect source")
	})

	t.Run("GetID and GetJobStatus", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		assert.Equal(t, job.ID.String(), job.GetID())
		assert.Equal(t, models.JobStatusPending, job.lifecycle.GetJobStatus())

		job.controller.markStarted(models.JobStatusPending)
		assert.Equal(t, models.JobStatusRunning, job.lifecycle.GetJobStatus())
	})
}

func TestBatchJob_RLockRUnlock(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	assert.NotPanics(t, func() {
		job.mu.RLock()
		job.mu.RUnlock()
	})
}

func TestJobStore_LoadFromDatabase_NilRepo(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	assert.NotPanics(t, func() {
		jq.loadFromDatabase()
	})
}

func TestJobStore_PersistToDatabase_NilRepo(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	assert.NotPanics(t, func() {
		jq.persistence.PersistJob(job)
	})
}

func TestJobStore_PersistToDatabase_DeletedJob(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.lifecycle.deleted = true
	assert.NotPanics(t, func() {
		jq.persistence.PersistJob(job)
	})
}

func TestGetStatusSlim_NilResultEntry(t *testing.T) {
	t.Skip("GetStatusSlim removed per ADR-0041")
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.Results["file1.mp4"] = nil

	slim := job.GetStatus()
	assert.NotNil(t, slim)
	_, exists := slim.Results["file1.mp4"]
	assert.False(t, exists, "nil result entries should be skipped")
}

func TestGetStatusSlim_ProvenanceData(t *testing.T) {
	t.Skip("GetStatusSlim removed per ADR-0041")
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.Results["file1.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
		Status:        models.JobStatusCompleted,
	}
	job.results.SetProvenance("file1.mp4", &ProvenanceData{
		FieldSources: map[string]string{
			"title": "r18dev",
		},
		ActressSources: map[string]string{
			"actress_0": "dmm",
		},
	})

	slim := job.GetStatus()
	assert.NotNil(t, slim)
	slimResult := slim.Results["file1.mp4"]
	require.NotNil(t, slimResult)
}

func TestGetStatusSlim_DeepCopyTimestamps(t *testing.T) {
	t.Skip("GetStatusSlim removed per ADR-0041")
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	now := time.Now()
	job.lifecycle.CompletedAt = &now
	job.lifecycle.OrganizedAt = &now
	job.lifecycle.RevertedAt = &now

	slim := job.GetStatus()
	require.NotNil(t, slim)
	require.NotNil(t, slim.CompletedAt)
	require.NotNil(t, slim.OrganizedAt)
	require.NotNil(t, slim.RevertedAt)

	*slim.CompletedAt = time.Time{}
	assert.NotEqual(t, time.Time{}, *job.lifecycle.CompletedAt, "deep copy should isolate mutations")
}

func TestAtomicUpdateFileResult_NotFound(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	err := job.results.AtomicUpdateFileResult("nonexistent.mp4", func(fr *MovieResult) (*MovieResult, error) {
		return fr, nil
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAtomicUpdateFileResult_NilResult(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.Results["file1.mp4"] = nil

	err := job.results.AtomicUpdateFileResult("file1.mp4", func(fr *MovieResult) (*MovieResult, error) {
		return fr, nil
	})
	assert.Error(t, err)
}

func TestAtomicUpdateFileResult_UpdateFnError(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.Results["file1.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
		Status:        models.JobStatusPending,
	}

	err := job.results.AtomicUpdateFileResult("file1.mp4", func(fr *MovieResult) (*MovieResult, error) {
		return nil, fmt.Errorf("update rejected")
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "update rejected")
}

func TestAtomicUpdateFileResult_StatusTransition(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.Results["file1.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
		Status:        models.JobStatusPending,
	}

	err := job.results.AtomicUpdateFileResult("file1.mp4", func(fr *MovieResult) (*MovieResult, error) {
		fr.Status = models.JobStatusCompleted
		return fr, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, job.results.Completed)
	assert.Equal(t, 0, job.results.Failed)
}
