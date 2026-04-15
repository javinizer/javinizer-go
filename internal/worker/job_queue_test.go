package worker

import (
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestJobQueue_CreateGetDeleteList(t *testing.T) {
	t.Run("Create and get job", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4", "file2.mkv", "file3.avi"}

		job := jq.CreateJob(files)

		// Verify job creation
		assert.NotEmpty(t, job.ID)
		assert.Equal(t, JobStatusPending, job.Status)
		assert.Equal(t, 3, job.TotalFiles)
		assert.Equal(t, files, job.Files)
		assert.NotNil(t, job.Results)
		assert.Empty(t, job.Results)
		assert.Equal(t, 0, job.Completed)
		assert.Equal(t, 0, job.Failed)
		assert.Equal(t, 0.0, job.Progress)
		assert.False(t, job.StartedAt.IsZero())
		assert.Nil(t, job.CompletedAt)

		// Retrieve the job
		retrieved, ok := jq.GetJob(job.ID)
		require.True(t, ok, "Job should exist")
		assert.Equal(t, job.ID, retrieved.ID)
		assert.Equal(t, job.TotalFiles, retrieved.TotalFiles)
	})

	t.Run("Get non-existent job", func(t *testing.T) {
		jq := NewJobQueue(nil, "")

		retrieved, ok := jq.GetJob("non-existent-id")
		assert.False(t, ok, "Job should not exist")
		assert.Nil(t, retrieved)
	})

	t.Run("Delete job", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4"}

		job := jq.CreateJob(files)
		jobID := job.ID

		// Verify job exists
		_, ok := jq.GetJob(jobID)
		require.True(t, ok, "Job should exist before deletion")

		// Delete job
		jq.DeleteJob(jobID, "data/temp")

		// Verify job is deleted
		_, ok = jq.GetJob(jobID)
		assert.False(t, ok, "Job should not exist after deletion")
	})

	t.Run("List jobs", func(t *testing.T) {
		jq := NewJobQueue(nil, "")

		// Initially empty
		jobs := jq.ListJobs()
		assert.Empty(t, jobs)

		// Create multiple jobs
		job1 := jq.CreateJob([]string{"file1.mp4"})
		job2 := jq.CreateJob([]string{"file2.mkv", "file3.avi"})
		job3 := jq.CreateJob([]string{"file4.mp4"})

		// List should contain all jobs
		jobs = jq.ListJobs()
		assert.Len(t, jobs, 3)

		// Verify all job IDs are present
		jobIDs := make(map[string]bool)
		for _, job := range jobs {
			jobIDs[job.ID] = true
		}
		assert.True(t, jobIDs[job1.ID], "Job1 should be in list")
		assert.True(t, jobIDs[job2.ID], "Job2 should be in list")
		assert.True(t, jobIDs[job3.ID], "Job3 should be in list")

		// Delete one job
		jq.DeleteJob(job2.ID, "data/temp")

		// List should have 2 jobs
		jobs = jq.ListJobs()
		assert.Len(t, jobs, 2)
	})

	t.Run("Empty files list", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{})

		assert.Equal(t, 0, job.TotalFiles)
		assert.Empty(t, job.Files)
	})
}

func TestBatchJob_UpdateFileResult(t *testing.T) {
	t.Run("Update single file result", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4", "file2.mkv", "file3.avi"}
		job := jq.CreateJob(files)

		now := time.Now()
		result := &FileResult{
			FilePath:  "file1.mp4",
			MovieID:   "IPX-123",
			Status:    JobStatusCompleted,
			StartedAt: now,
			EndedAt:   &now,
		}

		job.UpdateFileResult("file1.mp4", result)

		// Verify result is stored
		assert.Len(t, job.Results, 1)
		assert.Equal(t, result, job.Results["file1.mp4"])

		// Verify counters
		assert.Equal(t, 1, job.Completed)
		assert.Equal(t, 0, job.Failed)
		assert.InDelta(t, 33.33, job.Progress, 0.1) // 1/3 * 100
	})

	t.Run("Update multiple file results with mixed status", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4", "file2.mkv", "file3.avi", "file4.mp4"}
		job := jq.CreateJob(files)

		now := time.Now()

		// Complete first file
		job.UpdateFileResult("file1.mp4", &FileResult{
			FilePath:  "file1.mp4",
			Status:    JobStatusCompleted,
			StartedAt: now,
		})
		assert.Equal(t, 1, job.Completed)
		assert.Equal(t, 0, job.Failed)
		assert.Equal(t, 25.0, job.Progress)

		// Complete second file
		job.UpdateFileResult("file2.mkv", &FileResult{
			FilePath:  "file2.mkv",
			Status:    JobStatusCompleted,
			StartedAt: now,
		})
		assert.Equal(t, 2, job.Completed)
		assert.Equal(t, 0, job.Failed)
		assert.Equal(t, 50.0, job.Progress)

		// Fail third file
		job.UpdateFileResult("file3.avi", &FileResult{
			FilePath:  "file3.avi",
			Status:    JobStatusFailed,
			Error:     "scraper error",
			StartedAt: now,
		})
		assert.Equal(t, 2, job.Completed)
		assert.Equal(t, 1, job.Failed)
		assert.Equal(t, 75.0, job.Progress) // (2+1)/4 * 100

		// Complete fourth file
		job.UpdateFileResult("file4.mp4", &FileResult{
			FilePath:  "file4.mp4",
			Status:    JobStatusCompleted,
			StartedAt: now,
		})
		assert.Equal(t, 3, job.Completed)
		assert.Equal(t, 1, job.Failed)
		assert.Equal(t, 100.0, job.Progress)
	})

	t.Run("Update same file result multiple times", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4"}
		job := jq.CreateJob(files)

		now := time.Now()

		// Initially running
		job.UpdateFileResult("file1.mp4", &FileResult{
			FilePath:  "file1.mp4",
			Status:    JobStatusRunning,
			StartedAt: now,
		})
		assert.Equal(t, 0, job.Completed)
		assert.Equal(t, 0, job.Failed)
		assert.Equal(t, 0.0, job.Progress)

		// Then completed
		job.UpdateFileResult("file1.mp4", &FileResult{
			FilePath:  "file1.mp4",
			Status:    JobStatusCompleted,
			MovieID:   "IPX-123",
			StartedAt: now,
		})
		assert.Equal(t, 1, job.Completed)
		assert.Equal(t, 0, job.Failed)
		assert.Equal(t, 100.0, job.Progress)

		// Verify only one result exists
		assert.Len(t, job.Results, 1)
	})

	t.Run("Progress calculation with pending files", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4", "file2.mkv", "file3.avi"}
		job := jq.CreateJob(files)

		now := time.Now()

		// Only update 1 out of 3 files
		job.UpdateFileResult("file1.mp4", &FileResult{
			FilePath:  "file1.mp4",
			Status:    JobStatusCompleted,
			StartedAt: now,
		})

		// Progress should be 33.33% (1/3), not 100%
		assert.InDelta(t, 33.33, job.Progress, 0.1)
		assert.Equal(t, 1, job.Completed)
		assert.Equal(t, 2, job.TotalFiles-job.Completed-job.Failed) // 2 pending
	})
}

func TestBatchJob_StatusTransitions(t *testing.T) {
	t.Run("MarkStarted", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})

		assert.Equal(t, JobStatusPending, job.Status)
		initialStartTime := job.StartedAt

		time.Sleep(10 * time.Millisecond) // Ensure time difference

		job.MarkStarted()

		assert.Equal(t, JobStatusRunning, job.Status)
		assert.True(t, job.StartedAt.After(initialStartTime), "StartedAt should be updated")
		assert.Nil(t, job.CompletedAt)
	})

	t.Run("MarkCompleted", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})
		job.MarkStarted()

		beforeCompletion := time.Now()
		job.MarkCompleted()
		afterCompletion := time.Now()

		assert.Equal(t, JobStatusCompleted, job.Status)
		assert.Equal(t, 100.0, job.Progress)
		require.NotNil(t, job.CompletedAt)
		assert.True(t, job.CompletedAt.After(beforeCompletion) || job.CompletedAt.Equal(beforeCompletion))
		assert.True(t, job.CompletedAt.Before(afterCompletion) || job.CompletedAt.Equal(afterCompletion))
	})

	t.Run("MarkFailed", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})
		job.MarkStarted()

		beforeFailure := time.Now()
		job.MarkFailed()
		afterFailure := time.Now()

		assert.Equal(t, JobStatusFailed, job.Status)
		require.NotNil(t, job.CompletedAt)
		assert.True(t, job.CompletedAt.After(beforeFailure) || job.CompletedAt.Equal(beforeFailure))
		assert.True(t, job.CompletedAt.Before(afterFailure) || job.CompletedAt.Equal(afterFailure))
	})

	t.Run("MarkCancelled", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})
		job.MarkStarted()

		beforeCancellation := time.Now()
		job.MarkCancelled()
		afterCancellation := time.Now()

		assert.Equal(t, JobStatusCancelled, job.Status)
		require.NotNil(t, job.CompletedAt)
		assert.True(t, job.CompletedAt.After(beforeCancellation) || job.CompletedAt.Equal(beforeCancellation))
		assert.True(t, job.CompletedAt.Before(afterCancellation) || job.CompletedAt.Equal(afterCancellation))
	})

	t.Run("MarkReverted", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})
		job.MarkStarted()
		job.MarkCompleted()
		job.MarkOrganized()

		beforeReverted := time.Now()
		job.MarkReverted()
		afterReverted := time.Now()

		assert.Equal(t, JobStatusReverted, job.Status)
		require.NotNil(t, job.RevertedAt)
		assert.True(t, job.RevertedAt.After(beforeReverted) || job.RevertedAt.Equal(beforeReverted))
		assert.True(t, job.RevertedAt.Before(afterReverted) || job.RevertedAt.Equal(afterReverted))
	})

	t.Run("Full workflow: pending -> running -> completed", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4", "file2.mkv"}
		job := jq.CreateJob(files)

		// Start as pending
		assert.Equal(t, JobStatusPending, job.Status)

		// Mark as running
		job.MarkStarted()
		assert.Equal(t, JobStatusRunning, job.Status)
		assert.Nil(t, job.CompletedAt)

		// Process files
		now := time.Now()
		job.UpdateFileResult("file1.mp4", &FileResult{
			FilePath:  "file1.mp4",
			Status:    JobStatusCompleted,
			StartedAt: now,
		})
		job.UpdateFileResult("file2.mkv", &FileResult{
			FilePath:  "file2.mkv",
			Status:    JobStatusCompleted,
			StartedAt: now,
		})

		// Mark as completed
		job.MarkCompleted()
		assert.Equal(t, JobStatusCompleted, job.Status)
		assert.NotNil(t, job.CompletedAt)
		assert.Equal(t, 100.0, job.Progress)
	})

	t.Run("Revert workflow: organized -> reverted", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})
		job.MarkStarted()
		job.MarkCompleted()
		job.MarkOrganized()

		assert.Equal(t, JobStatusOrganized, job.Status)

		// Done channel should be closed after MarkOrganized
		select {
		case <-job.Done:
		default:
			t.Fatal("Done channel should be closed after MarkOrganized")
		}

		job.MarkReverted()
		assert.Equal(t, JobStatusReverted, job.Status)
		require.NotNil(t, job.RevertedAt)
	})
}

func TestBatchJob_GetStatus(t *testing.T) {
	t.Run("Returns copy with all fields", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4", "file2.mkv"}
		job := jq.CreateJob(files)
		job.MarkStarted()

		now := time.Now()
		job.UpdateFileResult("file1.mp4", &FileResult{
			FilePath:  "file1.mp4",
			Status:    JobStatusCompleted,
			StartedAt: now,
		})

		status := job.GetStatus()

		// Verify all fields are copied
		assert.Equal(t, job.ID, status.ID)
		assert.Equal(t, job.Status, status.Status)
		assert.Equal(t, job.TotalFiles, status.TotalFiles)
		assert.Equal(t, job.Completed, status.Completed)
		assert.Equal(t, job.Failed, status.Failed)
		assert.Equal(t, job.Files, status.Files)
		assert.Equal(t, job.Progress, status.Progress)
		assert.Equal(t, job.StartedAt, status.StartedAt)
		assert.Len(t, status.Results, 1)
	})

	t.Run("Deep copy of FileResults - map and FileResults are independent", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4", "file2.mkv"}
		job := jq.CreateJob(files)

		now := time.Now()
		result1 := &FileResult{
			FilePath:  "file1.mp4",
			Status:    JobStatusCompleted,
			MovieID:   "IPX-123",
			StartedAt: now,
		}
		job.UpdateFileResult("file1.mp4", result1)

		// Get status copy
		status := job.GetStatus()

		// Verify FileResult objects are NOT shared (deep copy)
		// FileResults should be independent to prevent concurrent mutations
		assert.NotSame(t, job.Results["file1.mp4"], status.Results["file1.mp4"],
			"FileResult pointers should be different (deep copy)")

		// Verify fields are equal but independent
		assert.Equal(t, job.Results["file1.mp4"].MovieID, status.Results["file1.mp4"].MovieID,
			"FileResult fields should be equal")

		// Modifying a FileResult in the copy should NOT affect original
		status.Results["file1.mp4"].MovieID = "MODIFIED-999"
		assert.Equal(t, "IPX-123", job.Results["file1.mp4"].MovieID,
			"Original FileResult should remain unchanged (deep copy)")
		assert.Equal(t, "MODIFIED-999", status.Results["file1.mp4"].MovieID,
			"Copy FileResult should be modified")

		// Adding new entries to the copy's map doesn't affect original
		status.Results["file2.mkv"] = &FileResult{
			FilePath:  "file2.mkv",
			Status:    JobStatusCompleted,
			StartedAt: now,
		}
		assert.Len(t, status.Results, 2, "Copy should have 2 results")
		assert.Len(t, job.Results, 1, "Original should still have 1 result (map is independent)")
	})

	t.Run("Copies CompletedAt correctly when nil", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})

		status := job.GetStatus()

		assert.Nil(t, job.CompletedAt)
		assert.Nil(t, status.CompletedAt)
	})

	t.Run("Copies CompletedAt correctly when set", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})
		job.MarkCompleted()

		status := job.GetStatus()

		require.NotNil(t, job.CompletedAt)
		require.NotNil(t, status.CompletedAt)
		assert.Equal(t, *job.CompletedAt, *status.CompletedAt)

		// Verify they're separate pointers
		assert.NotSame(t, job.CompletedAt, status.CompletedAt, "CompletedAt should be copied, not shared")
	})

	t.Run("Empty results map is copied correctly", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})

		status := job.GetStatus()

		assert.Empty(t, status.Results)
		assert.NotNil(t, status.Results)
	})
}

// TestConcurrent_GetStatusAndUpdateFileResult validates thread-safe snapshot access
// This test catches race conditions where handlers read job state while workers update it
func TestConcurrent_GetStatusAndUpdateFileResult(t *testing.T) {
	jq := NewJobQueue(nil, "")
	job := jq.CreateJob([]string{"file1.mp4", "file2.mkv", "file3.avi"})

	now := time.Now()
	// Initialize with a file result
	job.UpdateFileResult("file1.mp4", &FileResult{
		FilePath:  "file1.mp4",
		MovieID:   "IPX-100",
		Status:    JobStatusRunning,
		StartedAt: now,
	})

	// Simulate worker updating job results concurrently
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			// Rapidly update multiple file results
			job.UpdateFileResult("file1.mp4", &FileResult{
				FilePath:  "file1.mp4",
				MovieID:   "IPX-" + fmt.Sprintf("%d", i),
				Status:    JobStatusRunning,
				StartedAt: now,
			})
			job.UpdateFileResult("file2.mkv", &FileResult{
				FilePath:  "file2.mkv",
				MovieID:   "IPX-" + fmt.Sprintf("%d", i+1000),
				Status:    JobStatusCompleted,
				StartedAt: now,
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
				assert.NotEmpty(t, result.FilePath)
			}
		}
	}

	<-done
}

// TestConcurrent_DirectMapAccessIsUnsafe demonstrates the race condition
// This test would fail with -race if we directly accessed job.Results without GetStatus()
// Run with: go test -race -run TestConcurrent_DirectMapAccessIsUnsafe
func TestConcurrent_DirectMapAccessIsUnsafe(t *testing.T) {
	t.Skip("This test demonstrates unsafe pattern - skip to avoid race detector failures")

	jq := NewJobQueue(nil, "")
	job := jq.CreateJob([]string{"file1.mp4"})

	now := time.Now()
	job.UpdateFileResult("file1.mp4", &FileResult{
		FilePath:  "file1.mp4",
		MovieID:   "IPX-1",
		Status:    JobStatusRunning,
		StartedAt: now,
	})

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			job.UpdateFileResult("file1.mp4", &FileResult{
				FilePath:  "file1.mp4",
				MovieID:   fmt.Sprintf("IPX-%d", i),
				Status:    JobStatusRunning,
				StartedAt: now,
			})
		}
		close(done)
	}()

	// UNSAFE: Direct map access without GetStatus() - WOULD FAIL WITH -race
	for i := 0; i < 1000; i++ {
		// This would cause: fatal error: concurrent map iteration and map write
		for filePath := range job.Results {
			_ = filePath
		}
	}

	<-done
}

// TestBatchJob_PointerFieldIndependence validates that pointer fields are deep copied
// This ensures modifying pointer fields in the snapshot doesn't affect the live job
func TestBatchJob_PointerFieldIndependence(t *testing.T) {
	jq := NewJobQueue(nil, "")
	job := jq.CreateJob([]string{"file1.mp4"})

	// Create FileResult with pointer fields
	originalTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	originalError := "poster download failed"
	job.UpdateFileResult("file1.mp4", &FileResult{
		FilePath:    "file1.mp4",
		MovieID:     "IPX-100",
		Status:      JobStatusCompleted,
		StartedAt:   time.Now(),
		EndedAt:     &originalTime,
		PosterError: &originalError,
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

// TestJobQueue_GetJobPointer tests the GetJobPointer method
func TestJobQueue_GetJobPointer(t *testing.T) {
	t.Run("get existing job pointer", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4", "file2.mkv"}
		job := jq.CreateJob(files)

		// Get pointer to the job
		jobPtr, ok := jq.GetJobPointer(job.ID)
		require.True(t, ok, "Should find existing job")
		require.NotNil(t, jobPtr)

		// Verify it's the same job
		assert.Equal(t, job.ID, jobPtr.ID)
		assert.Equal(t, job.TotalFiles, jobPtr.TotalFiles)

		// Modify through pointer should affect original
		jobPtr.MarkStarted()
		assert.Equal(t, JobStatusRunning, job.Status)
	})

	t.Run("get non-existent job pointer", func(t *testing.T) {
		jq := NewJobQueue(nil, "")

		jobPtr, ok := jq.GetJobPointer("non-existent-id")
		assert.False(t, ok, "Should not find non-existent job")
		assert.Nil(t, jobPtr)
	})
}

// TestBatchJob_AtomicUpdateFileResult tests atomic file result updates
func TestBatchJob_AtomicUpdateFileResult(t *testing.T) {
	t.Run("atomic update with update function", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})

		now := time.Now()
		initial := &FileResult{
			FilePath:  "file1.mp4",
			MovieID:   "IPX-100",
			Status:    JobStatusRunning,
			StartedAt: now,
		}
		job.UpdateFileResult("file1.mp4", initial)

		// Atomic update function
		err := job.AtomicUpdateFileResult("file1.mp4", func(current *FileResult) (*FileResult, error) {
			// Create updated result
			updated := *current
			updated.MovieID = "IPX-200"
			updated.Status = JobStatusCompleted
			return &updated, nil
		})

		assert.NoError(t, err)
		assert.Equal(t, "IPX-200", job.Results["file1.mp4"].MovieID)
		assert.Equal(t, JobStatusCompleted, job.Results["file1.mp4"].Status)
	})

	t.Run("atomic update with error", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})

		now := time.Now()
		initial := &FileResult{
			FilePath:  "file1.mp4",
			MovieID:   "IPX-100",
			Status:    JobStatusRunning,
			StartedAt: now,
		}
		job.UpdateFileResult("file1.mp4", initial)

		// Atomic update that returns error
		err := job.AtomicUpdateFileResult("file1.mp4", func(current *FileResult) (*FileResult, error) {
			return nil, fmt.Errorf("update failed")
		})

		assert.Error(t, err)
		assert.Equal(t, "update failed", err.Error())
		// Original should be unchanged
		assert.Equal(t, "IPX-100", job.Results["file1.mp4"].MovieID)
	})

	t.Run("atomic update on non-existent file", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})

		// Try to update without initial result
		err := job.AtomicUpdateFileResult("file1.mp4", func(current *FileResult) (*FileResult, error) {
			updated := *current
			updated.MovieID = "IPX-999"
			return &updated, nil
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "file result not found")
	})
}

// TestBatchJob_SetCancelFunc tests setting cancellation function
func TestBatchJob_SetCancelFunc(t *testing.T) {
	t.Run("set and trigger cancel func", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})

		cancelled := false
		cancelFunc := func() {
			cancelled = true
		}

		// Set cancel function
		job.SetCancelFunc(cancelFunc)

		// Trigger cancellation
		job.Cancel()

		// Verify cancel function was called
		assert.True(t, cancelled, "Cancel function should have been called")
		assert.Equal(t, JobStatusCancelled, job.Status)
	})

	t.Run("cancel without cancel func", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})

		// Don't set cancel func, just call Cancel
		job.Cancel()

		// Should still mark as cancelled
		assert.Equal(t, JobStatusCancelled, job.Status)
	})
}

// TestBatchJob_GetProgress tests progress retrieval
func TestBatchJob_GetProgress(t *testing.T) {
	t.Run("get progress at different stages", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4", "file2.mkv", "file3.avi", "file4.mp4"}
		job := jq.CreateJob(files)

		// Initial progress
		progress := job.GetProgress()
		assert.Equal(t, 0.0, progress)

		// Complete one file
		now := time.Now()
		job.UpdateFileResult("file1.mp4", &FileResult{
			FilePath:  "file1.mp4",
			Status:    JobStatusCompleted,
			StartedAt: now,
		})
		progress = job.GetProgress()
		assert.Equal(t, 25.0, progress)

		// Complete two more files
		job.UpdateFileResult("file2.mkv", &FileResult{
			FilePath:  "file2.mkv",
			Status:    JobStatusCompleted,
			StartedAt: now,
		})
		job.UpdateFileResult("file3.avi", &FileResult{
			FilePath:  "file3.avi",
			Status:    JobStatusCompleted,
			StartedAt: now,
		})
		progress = job.GetProgress()
		assert.Equal(t, 75.0, progress)

		// Complete last file
		job.UpdateFileResult("file4.mp4", &FileResult{
			FilePath:  "file4.mp4",
			Status:    JobStatusCompleted,
			StartedAt: now,
		})
		progress = job.GetProgress()
		assert.Equal(t, 100.0, progress)
	})
}

// TestBatchJob_ExcludeFile tests file exclusion
func TestBatchJob_ExcludeFile(t *testing.T) {
	t.Run("exclude single file", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4", "file2.mkv", "file3.avi"}
		job := jq.CreateJob(files)

		// Exclude file
		job.ExcludeFile("file1.mp4")

		// Verify exclusion
		assert.True(t, job.IsExcluded("file1.mp4"))
		assert.False(t, job.IsExcluded("file2.mkv"))
		assert.False(t, job.IsExcluded("file3.avi"))
	})

	t.Run("exclude multiple files", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4", "file2.mkv", "file3.avi"}
		job := jq.CreateJob(files)

		// Exclude multiple files
		job.ExcludeFile("file1.mp4")
		job.ExcludeFile("file3.avi")

		// Verify exclusions
		assert.True(t, job.IsExcluded("file1.mp4"))
		assert.False(t, job.IsExcluded("file2.mkv"))
		assert.True(t, job.IsExcluded("file3.avi"))
	})

	t.Run("exclude same file multiple times", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4"}
		job := jq.CreateJob(files)

		// Exclude same file multiple times
		job.ExcludeFile("file1.mp4")
		job.ExcludeFile("file1.mp4")
		job.ExcludeFile("file1.mp4")

		// Should still be excluded
		assert.True(t, job.IsExcluded("file1.mp4"))
	})
}

// TestBatchJob_IsExcluded tests exclusion checking
func TestBatchJob_IsExcluded(t *testing.T) {
	t.Run("check non-excluded file", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4", "file2.mkv"}
		job := jq.CreateJob(files)

		// Files should not be excluded initially
		assert.False(t, job.IsExcluded("file1.mp4"))
		assert.False(t, job.IsExcluded("file2.mkv"))
	})

	t.Run("check excluded file", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4", "file2.mkv"}
		job := jq.CreateJob(files)

		job.ExcludeFile("file1.mp4")

		assert.True(t, job.IsExcluded("file1.mp4"))
		assert.False(t, job.IsExcluded("file2.mkv"))
	})

	t.Run("check non-existent file", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		files := []string{"file1.mp4"}
		job := jq.CreateJob(files)

		// Non-existent file should not be excluded
		assert.False(t, job.IsExcluded("non-existent.mp4"))
	})
}

// TestMarkReverted_StatusAndTimestamp verifies MarkReverted sets status and timestamp
func TestMarkReverted_StatusAndTimestamp(t *testing.T) {
	jq := NewJobQueue(nil, "")
	job := jq.CreateJob([]string{"file1.mp4"})
	job.MarkStarted()
	job.MarkCompleted()
	job.MarkOrganized()

	beforeReverted := time.Now()
	job.MarkReverted()

	assert.Equal(t, JobStatusReverted, job.Status)
	require.NotNil(t, job.RevertedAt)
	assert.True(t, job.RevertedAt.After(beforeReverted) || job.RevertedAt.Equal(beforeReverted))
}

// TestMarkReverted_DoneChannelClosed verifies Done channel is closed after MarkReverted
func TestMarkReverted_DoneChannelClosed(t *testing.T) {
	jq := NewJobQueue(nil, "")
	job := jq.CreateJob([]string{"file1.mp4"})
	job.MarkStarted()
	job.MarkCompleted()
	job.MarkOrganized()

	// Done channel should be closed after MarkOrganized
	select {
	case <-job.Done:
	default:
		t.Fatal("Done channel should be closed after MarkOrganized")
	}

	// MarkReverted should still work (idempotent close)
	job.MarkReverted()
	assert.Equal(t, JobStatusReverted, job.Status)

	// Done channel should still be closed
	select {
	case <-job.Done:
	default:
		t.Fatal("Done channel should be closed after MarkReverted")
	}
}

// TestCleanupOldOrganizedJobs_DoesNotDeleteReverted verifies that the cleanup
// goroutine does NOT delete any jobs (it is now a no-op per D-05/HIST-11).
func TestCleanupOldOrganizedJobs_DoesNotDeleteReverted(t *testing.T) {
	mockRepo := mocks.NewMockJobRepositoryInterface(t)

	// NewJobQueue calls loadFromDatabase → List()
	mockRepo.On("List").Return([]models.Job{}, nil)

	jq := NewJobQueue(mockRepo, "")
	jq.cleanupOldOrganizedJobs()

	// Verify that DeleteOrganizedOlderThan was NOT called (cleanup is disabled)
	mockRepo.AssertNotCalled(t, "DeleteOrganizedOlderThan", mock.Anything)
	mockRepo.AssertExpectations(t)
}

func TestBatchJob_GettersSetters(t *testing.T) {
	t.Run("GetOperationModeOverride and SetOperationModeOverride", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})

		assert.Empty(t, job.GetOperationModeOverride())

		job.SetOperationModeOverride("organize")
		assert.Equal(t, "organize", job.GetOperationModeOverride())

		job.SetOperationModeOverride("")
		assert.Empty(t, job.GetOperationModeOverride())
	})

	t.Run("GetMoveToFolderOverride and SetMoveToFolderOverride", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})

		assert.Nil(t, job.GetMoveToFolderOverride())

		tTrue := true
		job.SetMoveToFolderOverride(&tTrue)
		result := job.GetMoveToFolderOverride()
		require.NotNil(t, result)
		assert.True(t, *result)

		tFalse := false
		job.SetMoveToFolderOverride(&tFalse)
		result = job.GetMoveToFolderOverride()
		require.NotNil(t, result)
		assert.False(t, *result)

		job.SetMoveToFolderOverride(nil)
		assert.Nil(t, job.GetMoveToFolderOverride())
	})

	t.Run("GetRenameFolderInPlaceOverride and SetRenameFolderInPlaceOverride", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})

		assert.Nil(t, job.GetRenameFolderInPlaceOverride())

		tTrue := true
		job.SetRenameFolderInPlaceOverride(&tTrue)
		result := job.GetRenameFolderInPlaceOverride()
		require.NotNil(t, result)
		assert.True(t, *result)

		job.SetRenameFolderInPlaceOverride(nil)
		assert.Nil(t, job.GetRenameFolderInPlaceOverride())
	})

	t.Run("GetDestination and SetDestination", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})

		assert.Empty(t, job.GetDestination())

		job.SetDestination("/output/dir")
		assert.Equal(t, "/output/dir", job.GetDestination())
	})

	t.Run("GetFiles returns a copy", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4", "file2.mkv"})

		files := job.GetFiles()
		assert.Equal(t, []string{"file1.mp4", "file2.mkv"}, files)

		files[0] = "modified"
		originalFiles := job.GetFiles()
		assert.Equal(t, "file1.mp4", originalFiles[0], "mutation of returned slice should not affect job")
	})

	t.Run("GetCompleted, GetFailed, GetTotalFiles", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4", "file2.mkv", "file3.avi"})

		assert.Equal(t, 0, job.GetCompleted())
		assert.Equal(t, 0, job.GetFailed())
		assert.Equal(t, 3, job.GetTotalFiles())

		now := time.Now()
		job.UpdateFileResult("file1.mp4", &FileResult{
			FilePath:  "file1.mp4",
			Status:    JobStatusCompleted,
			StartedAt: now,
		})
		job.UpdateFileResult("file2.mkv", &FileResult{
			FilePath:  "file2.mkv",
			Status:    JobStatusFailed,
			Error:     "test error",
			StartedAt: now,
		})

		assert.Equal(t, 1, job.GetCompleted())
		assert.Equal(t, 1, job.GetFailed())
		assert.Equal(t, 3, job.GetTotalFiles())
	})

	t.Run("concurrent access without race", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4", "file2.mkv", "file3.avi"})

		done := make(chan struct{})

		go func() {
			defer close(done)
			for i := 0; i < 100; i++ {
				job.SetOperationModeOverride("organize")
				_ = job.GetOperationModeOverride()
				job.SetDestination("/test/dir")
				_ = job.GetDestination()
				t := true
				job.SetMoveToFolderOverride(&t)
				_ = job.GetMoveToFolderOverride()
				job.SetRenameFolderInPlaceOverride(&t)
				_ = job.GetRenameFolderInPlaceOverride()
				_ = job.GetFiles()
				_ = job.GetCompleted()
				_ = job.GetFailed()
				_ = job.GetTotalFiles()
			}
		}()

		for i := 0; i < 100; i++ {
			_ = job.GetOperationModeOverride()
			_ = job.GetDestination()
			_ = job.GetMoveToFolderOverride()
			_ = job.GetRenameFolderInPlaceOverride()
			_ = job.GetFiles()
			_ = job.GetCompleted()
			_ = job.GetFailed()
			_ = job.GetTotalFiles()
		}

		<-done
	})

	t.Run("SetFileMatchInfo and GetFileMatchInfo", func(t *testing.T) {
		jq := NewJobQueue(nil, "")
		job := jq.CreateJob([]string{"file1.mp4"})

		info := FileMatchInfo{MovieID: "ABC-123", IsMultiPart: true, PartNumber: 1}
		job.SetFileMatchInfo("file1.mp4", info)

		retrieved, ok := job.GetFileMatchInfo("file1.mp4")
		assert.True(t, ok)
		assert.Equal(t, "ABC-123", retrieved.MovieID)
		assert.True(t, retrieved.IsMultiPart)

		_, ok = job.GetFileMatchInfo("nonexistent.mp4")
		assert.False(t, ok)
	})
}
