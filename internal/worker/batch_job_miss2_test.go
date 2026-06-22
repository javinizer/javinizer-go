package worker

import (
	"context"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- UpdatePosterCrop: with movie result having movie ---

func TestUpdatePosterCrop_Miss2_WithMovieResult(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	job.results.UpdateFileResult("/test/file.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie: &models.Movie{
			ID: "TEST-001",
			Poster: models.PosterState{
				PosterURL:        "https://example.com/poster.jpg",
				ShouldCropPoster: true,
			},
		},
		FileMatchInfo: models.FileMatchInfo{
			Path:    "/test/file.mp4",
			MovieID: "TEST-001",
		},
	})

	err := job.posterEditor.UpdatePosterCrop("TEST-001", "https://example.com/cropped.jpg")
	require.NoError(t, err)

	result, err := job.results.GetMovieResult("/test/file.mp4")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/cropped.jpg", result.Movie.Poster.CroppedPosterURL)
	assert.False(t, result.Movie.Poster.ShouldCropPoster)
}

// --- UpdatePosterFromURL: with movie result ---

func TestUpdatePosterFromURL_Miss2_WithMovieResult(t *testing.T) {
	job := newBatchJob([]string{"/test/file2.mp4"})
	job.results.UpdateFileResult("/test/file2.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie: &models.Movie{
			ID: "TEST-002",
			Poster: models.PosterState{
				PosterURL:        "https://example.com/old-poster.jpg",
				CroppedPosterURL: "https://example.com/old-cropped.jpg",
				ShouldCropPoster: true,
			},
		},
		FileMatchInfo: models.FileMatchInfo{
			Path:    "/test/file2.mp4",
			MovieID: "TEST-002",
		},
	})

	err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "TEST-002", "https://example.com/new-poster.jpg", "https://example.com/new-cropped.jpg")
	require.NoError(t, err)

	result, err := job.results.GetMovieResult("/test/file2.mp4")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/new-poster.jpg", result.Movie.Poster.PosterURL)
	assert.Equal(t, "https://example.com/new-cropped.jpg", result.Movie.Poster.CroppedPosterURL)
	assert.False(t, result.Movie.Poster.ShouldCropPoster)
}

// --- backupPosterOriginals: already backed up ---

func TestBackupPosterOriginals_Miss2_AlreadyBacked(t *testing.T) {
	movie := &models.Movie{
		Poster: models.PosterState{
			OriginalPosterURL: "https://example.com/original.jpg",
			PosterURL:         "https://example.com/new.jpg",
		},
	}
	backupPosterOriginals(movie)
	// Should NOT overwrite original since it's already set
	assert.Equal(t, "https://example.com/original.jpg", movie.Poster.OriginalPosterURL)
}

// --- FindFileForMovieID: multiple verified files with different part numbers ---

func TestFindFileForMovieID_Miss2_MultipleFilesWithParts(t *testing.T) {
	job := newBatchJob([]string{"/test/partA.mp4", "/test/partB.mp4"})
	job.results.UpdateFileResult("/test/partA.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie:  &models.Movie{ID: "MULTI-001"},
		FileMatchInfo: models.FileMatchInfo{
			Path:       "/test/partA.mp4",
			MovieID:    "MULTI-001",
			PartNumber: 2,
			PartSuffix: "pt2",
		},
	})
	job.results.UpdateFileResult("/test/partB.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie:  &models.Movie{ID: "MULTI-001"},
		FileMatchInfo: models.FileMatchInfo{
			Path:       "/test/partB.mp4",
			MovieID:    "MULTI-001",
			PartNumber: 1,
			PartSuffix: "pt1",
		},
	})

	result, err := job.resultIndex.FindFileForMovieID("MULTI-001")
	require.NoError(t, err)
	assert.NotEmpty(t, result.FilePath)
	assert.NotEmpty(t, result.OldMovieID)
}

// --- FindMovieResultForMovieID: movie not found ---

func TestFindMovieResultForMovieID_Miss2_NotFound(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	_, err := job.resultIndex.FindMovieResultForMovieID("NONEXISTENT-001")
	require.Error(t, err)
}

// --- FindMovieResultForMovieID: found with movie ---

func TestFindMovieResultForMovieID_Miss2_Found(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	job.results.UpdateFileResult("/test/file.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie:  &models.Movie{ID: "FOUND-001"},
		FileMatchInfo: models.FileMatchInfo{
			Path:    "/test/file.mp4",
			MovieID: "FOUND-001",
		},
	})

	result, err := job.resultIndex.FindMovieResultForMovieID("FOUND-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "FOUND-001", result.Movie.ID)
}

// --- GetMovieResultsForMovieID ---

func TestGetMovieResultsForMovieID_Miss2(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	job.results.UpdateFileResult("/test/file.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie:  &models.Movie{ID: "MRES-001"},
		FileMatchInfo: models.FileMatchInfo{
			Path:    "/test/file.mp4",
			MovieID: "MRES-001",
		},
	})

	results := job.resultIndex.GetMovieResultsForMovieID("MRES-001")
	assert.Len(t, results, 1)
}

// --- GetFileMatchInfosForMovieID ---

func TestGetFileMatchInfosForMovieID_Miss2(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	job.results.UpdateFileResult("/test/file.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie:  &models.Movie{ID: "FMI-001"},
		FileMatchInfo: models.FileMatchInfo{
			Path:    "/test/file.mp4",
			MovieID: "FMI-001",
		},
	})

	infos := job.resultIndex.GetFileMatchInfosForMovieID("FMI-001")
	assert.Len(t, infos, 1)
	assert.Equal(t, "FMI-001", infos[0].MovieID)
}

// --- FindFilePathsForMovieID: no paths found ---

func TestFindFilePathsForMovieID_Miss2_NoPaths(t *testing.T) {
	job := newBatchJob([]string{})
	result := job.resultIndex.FindFilePathsForMovieID("NONEXISTENT-001")
	assert.Nil(t, result)
}

// --- FindFilePathsForMovieID: paths found ---

func TestFindFilePathsForMovieID_Miss2_PathsFound(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	job.results.UpdateFileResult("/test/file.mp4", &MovieResult{
		Status: models.JobStatusCompleted,
		Movie:  &models.Movie{ID: "FP-001"},
		FileMatchInfo: models.FileMatchInfo{
			Path:    "/test/file.mp4",
			MovieID: "FP-001",
		},
	})

	result := job.resultIndex.FindFilePathsForMovieID("FP-001")
	assert.NotEmpty(t, result)
	assert.Equal(t, "/test/file.mp4", result[0])
}

// --- SetOperationModeOverride: invalid mode ---

func TestSetOperationModeOverride_Miss2_InvalidMode(t *testing.T) {
	job := newBatchJob([]string{})
	err := job.controller.SetOperationModeOverride("invalid-mode")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid operation mode")
}

// --- SetOperationModeOverride: empty mode defaults to organize ---

func TestSetOperationModeOverride_Miss2_EmptyMode(t *testing.T) {
	job := newBatchJob([]string{})
	err := job.controller.SetOperationModeOverride("")
	require.NoError(t, err)
	assert.Equal(t, "organize", string(job.GetOperationModeOverride()))
}

// --- setOperationModeFromDB: invalid mode logged as warning ---

func TestSetOperationModeFromDB_Miss2_InvalidMode(t *testing.T) {
	job := newBatchJob([]string{})
	job.controller.SetOperationModeOverride("invalid-db-mode")
}

// --- Run: no workflow configured ---

func TestRun_Miss2_NoWorkflow(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	err := newStandaloneJobFromBatchJob(job).Run(context.Background())
	require.Error(t, err)
	// Per N-7: validateWFFn removed — WF validation now happens in jobController.StartScrape.
	// When both WF and BatchCfg are missing, BatchCfg validation fires first.
	assert.Contains(t, err.Error(), "cannot run")
}

// --- Run: no batch config configured ---

func TestRun_Miss2_NoBatchConfig(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF: &stubMiss2WF{},
		},
	})
	err := newStandaloneJobFromBatchJob(job).Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "batch config not configured")
}

// --- StartApply: no workflow configured ---

func TestStartApply_Miss2_NoWorkflow(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow not configured")
}

// --- StartScrape: no workflow configured ---

func TestStartScrape_Miss2_NoWorkflow(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	err := job.Controller().StartScrape(context.Background(), []string{"/test/file.mp4"}, ScrapePhaseConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow not configured")
}

// --- Wait: failed job returns error ---

func TestWait_Miss2_FailedJob(t *testing.T) {
	job := newBatchJob([]string{})
	job.lifecycle.MarkFailed()
	err := job.Controller().Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
}

// --- Wait: cancelled job returns error ---

func TestWait_Miss2_CancelledJob(t *testing.T) {
	job := newBatchJob([]string{})
	job.lifecycle.MarkCancelled()
	err := job.Controller().Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

// --- Wait: completed job returns nil ---

func TestWait_Miss2_CompletedJob(t *testing.T) {
	job := newBatchJob([]string{})
	job.lifecycle.MarkCompleted()
	err := job.Controller().Wait()
	require.NoError(t, err)
}

// --- ExcludeFile: all excluded triggers cancel ---

func TestExcludeFile_Miss2_AllExcluded(t *testing.T) {
	job := newBatchJob([]string{"/test/file.mp4"})
	excludeFile(job, "/test/file.mp4")
	assert.Equal(t, models.JobStatusCancelled, job.lifecycle.GetJobStatus())
}

// --- TemplateEngine: lazy initialization ---

func TestTemplateEngine_Miss2_LazyInit(t *testing.T) {
	job := newBatchJob([]string{})
	eng := job.TemplateEngine()
	require.NotNil(t, eng)
}

// --- stubMiss2WF for testing ---

type stubMiss2WF struct {
	mu sync.Mutex
}

func (s *stubMiss2WF) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return nil, nil, nil
}

func (s *stubMiss2WF) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return nil, nil
}

func (s *stubMiss2WF) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}

func (s *stubMiss2WF) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}

func (s *stubMiss2WF) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}
