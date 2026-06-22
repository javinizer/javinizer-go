package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- UpdatePosterCrop: no matching files returns nil ---

func TestBatchJob_UpdatePosterCrop_NoMatchingFiles_Partial(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})
	// No results set for any file — UpdatePosterCrop should return nil
	err := job.posterEditor.UpdatePosterCrop("NONEXISTENT-001", "cropped.jpg")
	assert.NoError(t, err, "should return nil when no matching files")
}

// --- UpdatePosterCrop: nil movie skips file ---

func TestBatchJob_UpdatePosterCrop_NilMovie_Partial(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})
	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie:         nil,
	})

	err := job.posterEditor.UpdatePosterCrop("ABC-001", "cropped.jpg")
	assert.NoError(t, err, "should skip files with nil Movie")
}

// --- UpdatePosterCrop: multiple files for same movieID ---

func TestBatchJob_UpdatePosterCrop_MultipleFiles_Miss(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4", "file2.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})
	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID: "ABC-001",
			Poster: models.PosterState{
				PosterURL:        "poster1.jpg",
				ShouldCropPoster: true,
			},
		},
	})
	job.SetResultDirect("file2.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file2.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID: "ABC-001",
			Poster: models.PosterState{
				PosterURL:        "poster2.jpg",
				ShouldCropPoster: true,
			},
		},
	})

	err := job.posterEditor.UpdatePosterCrop("ABC-001", "cropped.jpg")
	require.NoError(t, err)

	// Both files should be updated
	r1 := job.results.Results["file1.mp4"]
	r2 := job.results.Results["file2.mp4"]
	assert.Equal(t, "cropped.jpg", r1.Movie.Poster.CroppedPosterURL)
	assert.Equal(t, "cropped.jpg", r2.Movie.Poster.CroppedPosterURL)
	assert.False(t, r1.Movie.Poster.ShouldCropPoster)
	assert.False(t, r2.Movie.Poster.ShouldCropPoster)
}

// --- UpdatePosterFromURL: no matching files returns nil ---

func TestBatchJob_UpdatePosterFromURL_NoMatchingFiles_Partial(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})
	err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "NONEXISTENT-001", "poster.jpg", "crop.jpg")
	assert.NoError(t, err)
}

// --- UpdatePosterFromURL: nil movie skips file ---

func TestBatchJob_UpdatePosterFromURL_NilMovie_Partial(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})
	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie:         nil,
	})

	err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "ABC-001", "poster.jpg", "crop.jpg")
	assert.NoError(t, err)
}

// --- UpdatePosterFromURL: multiple files for same movieID ---

func TestBatchJob_UpdatePosterFromURL_MultipleFiles_Miss(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4", "file2.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})
	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID: "ABC-001",
			Poster: models.PosterState{
				PosterURL:        "old-poster1.jpg",
				ShouldCropPoster: true,
			},
		},
	})
	job.SetResultDirect("file2.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file2.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID: "ABC-001",
			Poster: models.PosterState{
				PosterURL:        "old-poster2.jpg",
				ShouldCropPoster: false,
			},
		},
	})

	err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "ABC-001", "new-poster.jpg", "new-cropped.jpg")
	require.NoError(t, err)

	// Both files should be updated
	r1 := job.results.Results["file1.mp4"]
	r2 := job.results.Results["file2.mp4"]
	assert.Equal(t, "new-poster.jpg", r1.Movie.Poster.PosterURL)
	assert.Equal(t, "new-cropped.jpg", r1.Movie.Poster.CroppedPosterURL)
	assert.Equal(t, "new-poster.jpg", r2.Movie.Poster.PosterURL)
	assert.Equal(t, "new-cropped.jpg", r2.Movie.Poster.CroppedPosterURL)
}

// --- ExcludeFile: all excluded cancels job ---

func TestBatchJob_ExcludeFile_CancelsWhenAllExcluded_Partial(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4", "file2.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
		Status:        models.JobStatusCompleted,
	})
	job.results.UpdateFileResult("file2.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file2.mp4"},
		Status:        models.JobStatusCompleted,
	})

	// Exclude first file — should not cancel yet
	excludeFile(job, "file1.mp4")
	assert.NotEqual(t, models.JobStatusCancelled, job.lifecycle.GetJobStatus())

	// Exclude second file — should cancel
	excludeFile(job, "file2.mp4")
	assert.Equal(t, models.JobStatusCancelled, job.lifecycle.GetJobStatus())
}

// --- ExcludeFile: already transitioned does not re-cancel ---

func TestBatchJob_ExcludeFile_AlreadyTransitioned_Partial(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
		Status:        models.JobStatusCompleted,
	})
	// Mark as completed first
	job.lifecycle.MarkCompleted()
	// Now exclude — should not change status because already transitioned
	excludeFile(job, "file1.mp4")
	assert.Equal(t, models.JobStatusCompleted, job.lifecycle.GetJobStatus())
}

// --- ExcludeFile: cancelFunc is called when all excluded ---

func TestBatchJob_ExcludeFile_CancelFuncCalled(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4"},
		Status:        models.JobStatusCompleted,
	})

	cancelCalled := false
	job.lifecycle.setCancelFunc(func() { cancelCalled = true })

	excludeFile(job, "file1.mp4")
	assert.Equal(t, models.JobStatusCancelled, job.lifecycle.GetJobStatus())
	assert.True(t, cancelCalled, "cancelFunc should be called when all files excluded")
}

// --- Run: scrape + apply lifecycle with stored configs ---

func TestBatchJob_Run_WithStoredConfigs_Partial(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF: wf,
			BatchCfg: BatchJobConfig{
				MaxWorkers:    1,
				WorkerTimeout: 5 * time.Second,
				NFOEnabled:    true,
			},
		},
	})

	// Per DEEP-1: Run/SetRunOptions moved to StandaloneJob/JobRunner.
	sj := newStandaloneJobFromBatchJob(job)
	sj.SetRunOptions(
		ScrapePhaseConfig{SelectedScrapers: []string{"r18dev"}},
		ApplyPhaseConfig{Destination: "/out"},
	)
	done := make(chan error, 1)
	go func() {
		done <- sj.Run(context.Background())
	}()

	select {
	case err := <-done:
		// The stub workflow returns a scrape result, so Run should succeed
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not complete within timeout")
	}
}

// --- Run: context cancellation ---

func TestBatchJob_Run_ContextCancelled_Partial(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF: wf,
			BatchCfg: BatchJobConfig{
				MaxWorkers:    1,
				WorkerTimeout: 5 * time.Second,
			},
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := newStandaloneJobFromBatchJob(job).Run(ctx)
	assert.Error(t, err)
}
