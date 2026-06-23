package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- FindFileForMovieID: stale index entry (result nil) ---

func TestFindFileForMovieID_Miss_StaleIndexNilResult(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	// Set a result then nil it out — simulates stale index
	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "STALE-001"},
		Status:        models.JobStatusCompleted,
	})
	// Clear it but the index entry may persist
	job.results.mu.Lock()
	job.results.Results["file1.mp4"] = nil
	job.results.mu.Unlock()

	_, err := job.resultIndex.FindFileForMovieID("STALE-001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- FindFileForMovieID: multiple files with different part numbers ---

func TestFindFileForMovieID_Miss_MultipleFilesPartNumberSorting(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4"})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "MULTI-001", PartNumber: 2, PartSuffix: "b"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MULTI-001"},
	})
	job.SetResultDirect("file2.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file2.mp4", MovieID: "MULTI-001", PartNumber: 1, PartSuffix: "a"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MULTI-001"},
	})

	result, err := job.resultIndex.FindFileForMovieID("MULTI-001")
	require.NoError(t, err)
	assert.Contains(t, []string{"file1.mp4", "file2.mp4"}, result.FilePath)
}

// --- FindFileForMovieID: multiple files with zero part number ---

func TestFindFileForMovieID_Miss_ZeroPartNumber(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4"})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ZERO-001", PartNumber: 0},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ZERO-001"},
	})
	job.SetResultDirect("file2.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file2.mp4", MovieID: "ZERO-001", PartNumber: 1},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ZERO-001"},
	})

	result, err := job.resultIndex.FindFileForMovieID("ZERO-001")
	require.NoError(t, err)
	assert.Contains(t, []string{"file1.mp4", "file2.mp4"}, result.FilePath)
}

// --- FindFileForMovieID: multiple files with same part number, different suffix ---

func TestFindFileForMovieID_Miss_SuffixOrder(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4"})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "SUFF-001", PartNumber: 1, PartSuffix: "b"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SUFF-001"},
	})
	job.SetResultDirect("file2.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file2.mp4", MovieID: "SUFF-001", PartNumber: 1, PartSuffix: "a"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SUFF-001"},
	})

	result, err := job.resultIndex.FindFileForMovieID("SUFF-001")
	require.NoError(t, err)
	// Just verify it returns one of the files (sorting behavior is covered by suffixOrder unit tests)
	assert.Contains(t, []string{"file1.mp4", "file2.mp4"}, result.FilePath)
}

// --- FindFileForMovieID: captured revision and old movie ID ---

func TestFindFileForMovieID_Miss_CapturedRevisionAndOldMovieID(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ORIG-001"},
		Status:        models.JobStatusCompleted,
		Revision:      42,
		Movie:         &models.Movie{ID: "CHANGED-001"},
	})

	result, err := job.resultIndex.FindFileForMovieID("CHANGED-001")
	require.NoError(t, err)
	assert.Equal(t, "file1.mp4", result.FilePath)
	assert.Equal(t, uint64(42), result.CapturedRevision)
	assert.Equal(t, "CHANGED-001", result.OldMovieID)
}

// --- FindFileForMovieID: old movie ID from FileMatchInfo when Movie is nil ---

func TestFindFileForMovieID_Miss_OldMovieIDFromFileMatchInfo(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "FMID-001"},
		Status:        models.JobStatusCompleted,
		Revision:      1,
		Movie:         nil,
	})

	result, err := job.resultIndex.FindFileForMovieID("FMID-001")
	require.NoError(t, err)
	assert.Equal(t, "file1.mp4", result.FilePath)
	assert.Equal(t, "FMID-001", result.OldMovieID)
}

// --- FindMovieResultForMovieID: no movie in result ---

func TestFindMovieResultForMovieID_Miss_NoMovie(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "NOMOV-001"},
		Status:        models.JobStatusCompleted,
		Movie:         nil,
	})

	_, err := job.resultIndex.FindMovieResultForMovieID("NOMOV-001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no movie result found")
}

// --- UpdatePosterFromURL: no file paths for movie ID ---

func TestUpdatePosterFromURL_Miss_NoFilePaths(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{})

	err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "NOFILE-001", "https://example.com/poster.jpg", "https://example.com/cropped.jpg")
	assert.NoError(t, err)
}

// --- UpdatePosterCrop: no file paths for movie ID ---

func TestUpdatePosterCrop_Miss_NoFilePaths(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{})

	err := job.posterEditor.UpdatePosterCrop("NOFILE-001", "https://example.com/cropped.jpg")
	assert.NoError(t, err)
}

// --- FindFileForMovieID: movie not found (moved to ResultTracker per ADR-0041) ---

func TestFindFileForMovieID_Miss_NoResult(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	_, err := job.resultIndex.FindFileForMovieID("PRERES-001")
	assert.Error(t, err)
}

// --- Run: no workflow configured ---

func TestRun_Miss_NoWorkflow(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	err := newStandaloneJobFromBatchJob(job).Run(context.Background())
	require.Error(t, err)
	// Per N-7: validateWFFn removed — WF validation now happens in jobController.StartScrape.
	// When both WF and BatchCfg are missing, BatchCfg validation fires first.
	assert.Contains(t, err.Error(), "cannot run")
}

// --- Run: no batch config ---

func TestRun_Miss_NoBatchConfig(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.deps.WF = &noopWorkflowForMissTest{}

	err := newStandaloneJobFromBatchJob(job).Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "batch config not configured")
}

// --- StartApply: workflow nil and cfg.WF nil ---

func TestStartApply_Miss_NoWorkflow(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	// StartApply requires Completed status (API-1+2: CAS fix for double-start race)
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()

	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow not configured")
}

// --- StartScrape: workflow nil and cfg.WF nil ---

func TestStartScrape_Miss_NoWorkflow(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	err := job.Controller().StartScrape(context.Background(), []string{"file1.mp4"}, ScrapePhaseConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow not configured")
}

// --- Rescrape: workflow nil ---

func TestRescrape_Miss_NoWorkflow(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	result, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{MovieID: "NFW-001"})
	require.NoError(t, err)
	assert.Equal(t, models.RescrapeStatusFailed, result.Status)
}

// --- StartApply: with WF set on job.deps ---
// Per DEEP-6: WF override removed from ApplyPhaseConfig. WF is set on job.deps
// via SetWorkflow before calling phase methods.

func TestStartApply_Miss_CfgWFOverride(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.deps.BatchCfg = BatchJobConfig{MaxWorkers: 1}
	job.deps.WF = &noopWorkflowForMissTest{}

	// StartApply requires Completed status (API-1+2: CAS fix for double-start race)
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()

	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{
		Destination:           "/tmp/test",
		OperationModeOverride: "organize",
		TempDir:               t.TempDir(),
	})
	require.NoError(t, err)
}

// --- StartScrape: with WF set on job.deps ---
// Per DEEP-6: WF override removed from ScrapePhaseConfig. WF is set on job.deps
// via SetWorkflow before calling phase methods.

func TestStartScrape_Miss_CfgWFOverride(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.deps.BatchCfg = BatchJobConfig{MaxWorkers: 1}
	job.deps.WF = &noopWorkflowForMissTest{}

	err := job.Controller().StartScrape(context.Background(), []string{"file1.mp4"}, ScrapePhaseConfig{})
	require.NoError(t, err)
}

// --- noop workflow for testing ---

type noopWorkflowForMissTest struct{}

func (n *noopWorkflowForMissTest) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return &scrape.ScrapeResult{Status: scrape.StatusCompleted}, nil, nil
}
func (n *noopWorkflowForMissTest) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return &workflow.ApplyResult{}, nil
}
func (n *noopWorkflowForMissTest) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}
func (n *noopWorkflowForMissTest) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}
func (n *noopWorkflowForMissTest) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}
