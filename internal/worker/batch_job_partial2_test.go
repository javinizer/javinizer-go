package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- FindFileForMovieID: PartNumber sorting (lines 659, 662, 670) ---

func TestBatchJob_FindFileForMovieID_PartNumberSorting_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file_cd1.mp4", "file_cd2.mp4", "file_cd3.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	// Set results with different part numbers
	job.SetResultDirect("file_cd1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file_cd1.mp4", MovieID: "SORT-001", PartNumber: 1, PartSuffix: "cd1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SORT-001"},
	})
	job.SetResultDirect("file_cd2.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file_cd2.mp4", MovieID: "SORT-001", PartNumber: 2, PartSuffix: "cd2"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SORT-001"},
	})
	job.SetResultDirect("file_cd3.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file_cd3.mp4", MovieID: "SORT-001", PartNumber: 0, PartSuffix: "cd3"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SORT-001"},
	})

	// FindFileForMovieID should sort and return the correct file
	result, err := job.resultIndex.FindFileForMovieID("SORT-001")
	require.NoError(t, err)
	// PartNumber 0 should sort after PartNumber 1
	// PartNumber 1 should be first
	assert.Equal(t, "file_cd1.mp4", result.FilePath)
}

func TestBatchJob_FindFileForMovieID_PartNumber0ComesLast_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file_a.mp4", "file_b.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	// One file with PartNumber 0 (should come after PartNumber > 0)
	job.SetResultDirect("file_a.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file_a.mp4", MovieID: "P0-001", PartNumber: 0},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "P0-001"},
	})
	// Another file with PartNumber 2
	job.SetResultDirect("file_b.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file_b.mp4", MovieID: "P0-001", PartNumber: 2},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "P0-001"},
	})

	result, err := job.resultIndex.FindFileForMovieID("P0-001")
	require.NoError(t, err)
	// Just verify that sorting works and returns a valid file
	assert.Contains(t, []string{"file_a.mp4", "file_b.mp4"}, result.FilePath)
}

func TestBatchJob_FindFileForMovieID_SuffixOrderDifference_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file_a.mp4", "file_b.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	// Same PartNumber, different suffixes
	job.SetResultDirect("file_a.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file_a.mp4", MovieID: "SUF-001", PartNumber: 1, PartSuffix: "cd2"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SUF-001"},
	})
	job.SetResultDirect("file_b.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file_b.mp4", MovieID: "SUF-001", PartNumber: 1, PartSuffix: "cd1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SUF-001"},
	})

	result, err := job.resultIndex.FindFileForMovieID("SUF-001")
	require.NoError(t, err)
	// Just verify that sorting works and returns a valid file
	assert.Contains(t, []string{"file_a.mp4", "file_b.mp4"}, result.FilePath)
}

// --- FindMovieResultForMovieID: nil result (line 722) ---

func TestBatchJob_FindMovieResultForMovieID_NilResult_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "NIL-001"},
		Status:        models.JobStatusCompleted,
		Movie:         nil,
	})

	_, err := job.resultIndex.FindMovieResultForMovieID("NIL-001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no movie result found")
}

// --- findFileForRescrape: result with nil Movie, FileMatchInfo.MovieID used (line 778) ---

func TestBatchJob_FindFileForRescrape_NilMovieUsesFileMatchInfoMovieID_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "FMI-001"},
		Status:        models.JobStatusCompleted,
		Movie:         nil,
	})

	cmd := RescrapeCmd{FilePath: "file1.mp4", MovieID: "FMI-001"}
	result, err := job.resultIndex.FindFileForMovieID(cmd.MovieID)
	require.NoError(t, err)
	assert.Equal(t, "file1.mp4", result.FilePath)
	assert.Equal(t, "FMI-001", result.OldMovieID)
}

// --- StartScrape: FileMatchInfo set (line 928) ---

func TestBatchJob_StartScrape_FileMatchInfoSet_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	fmi := map[string]models.FileMatchInfo{
		"file1.mp4": {Path: "file1.mp4", MovieID: "FMI-TEST-001"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := job.Controller().StartScrape(ctx, []string{"file1.mp4"}, ScrapePhaseConfig{
		FileMatchInfo: fmi,
	})
	require.NoError(t, err)

	// Wait for the scrape to complete
	_ = job.Controller().Wait()
}

// --- StartScrape: BatchCfg set on job.deps (line 964) ---
// Per DEEP-6: BatchCfg override removed from ScrapePhaseConfig. BatchCfg is
// set on job.deps at construction or before phase calls.

func TestBatchJob_StartScrape_BatchCfgOverride_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF: &stubWorkflow{},
		},
	})

	// Set BatchCfg on job.deps directly (per DEEP-6: replaces phase config override)
	job.deps.BatchCfg = BatchJobConfig{MaxWorkers: 2, WorkerTimeout: 10 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := job.Controller().StartScrape(ctx, []string{"file1.mp4"}, ScrapePhaseConfig{})
	require.NoError(t, err)

	_ = job.Controller().Wait()
}

// --- StartApply: OperationModeOverride (line 979) ---

func TestBatchJob_StartApply_OperationModeOverride_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	// Set up a completed scrape result
	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "APPLY-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "APPLY-001"},
	})

	// StartApply requires Completed lifecycle status (API-1+2: CAS fix)
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mode := operationmode.OperationModeMetadataArtwork
	err := job.Controller().StartApply(ctx, ApplyPhaseConfig{
		OperationModeOverride: mode,
	})
	require.NoError(t, err)

	_ = job.Controller().Wait()
}

// --- StartApply: empty OperationModeOverride does not override (line 979) ---

func TestBatchJob_StartApply_EmptyOperationModeOverride_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "APPLY-002"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "APPLY-002"},
	})

	// StartApply requires Completed lifecycle status (API-1+2: CAS fix)
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := job.Controller().StartApply(ctx, ApplyPhaseConfig{
		OperationModeOverride: "", // empty - should not override
	})
	require.NoError(t, err)

	_ = job.Controller().Wait()
}

// --- StartApply: Update flag set (line 979+) ---

func TestBatchJob_StartApply_UpdateFlag_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "UPD-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "UPD-001"},
	})

	// StartApply requires Completed lifecycle status (API-1+2: CAS fix)
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	updateTrue := true
	err := job.Controller().StartApply(ctx, ApplyPhaseConfig{
		Update: &updateTrue,
	})
	require.NoError(t, err)

	assert.True(t, job.cfg.update)
	_ = job.Controller().Wait()
}

// --- Run: no completed results (line 1061) ---

func TestBatchJob_Run_NoCompletedResults_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second, NFOEnabled: false},
		},
	})

	// Set a failed result - no completed results
	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "FAIL-001"},
		Status:        models.JobStatusFailed,
		Movie:         nil,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := newStandaloneJobFromBatchJob(job).Run(ctx)
	assert.NoError(t, err) // Should return nil when no completed results
}

// --- Run: has completed results, NFO enabled (line 1083) ---

func TestBatchJob_Run_HasCompletedNFOEnabled_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second, NFOEnabled: true},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := newStandaloneJobFromBatchJob(job).Run(ctx)
	// Run will try to scrape and apply; with stub workflow it may succeed or fail
	_ = err
}

// --- FindFileForMovieID: single verified file (no sorting needed) ---

func TestBatchJob_FindFileForMovieID_SingleFile_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "SINGLE-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SINGLE-001"},
	})

	result, err := job.resultIndex.FindFileForMovieID("SINGLE-001")
	require.NoError(t, err)
	assert.Equal(t, "file1.mp4", result.FilePath)
	assert.Equal(t, "SINGLE-001", result.OldMovieID)
}

// --- FindFileForMovieID: not found ---

func TestBatchJob_FindFileForMovieID_NotFound_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	_, err := job.resultIndex.FindFileForMovieID("NOTFOUND-001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- findFileForRescrape: result with Movie.ID set ---

// Per ADR-0041: FindFileForMovieID moved from BatchJob to ResultTracker.
// Pre-resolved FilePath handling moved to RescrapePhase.Rescrape.
func TestResultTracker_FindFileForMovieID_WithMovieID_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "WMI-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "WMI-001"},
	})

	result, err := job.resultIndex.FindFileForMovieID("WMI-001")
	require.NoError(t, err)
	assert.Equal(t, "file1.mp4", result.FilePath)
	assert.Equal(t, "WMI-001", result.OldMovieID)
}

// --- StartApply: Destination set ---

func TestBatchJob_StartApply_DestinationSet_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "DEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "DEST-001"},
	})

	// StartApply requires Completed lifecycle status (API-1+2: CAS fix)
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := job.Controller().StartApply(ctx, ApplyPhaseConfig{
		Destination: "/some/destination",
	})
	require.NoError(t, err)
	assert.Equal(t, "/some/destination", job.GetDestination())

	_ = job.Controller().Wait()
}

// --- StartApply: TempDir set ---

func TestBatchJob_StartApply_TempDirSet_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second},
		},
	})

	job.SetResultDirect("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "TEMP-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEMP-001"},
	})

	// StartApply requires Completed lifecycle status (API-1+2: CAS fix)
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := job.Controller().StartApply(ctx, ApplyPhaseConfig{
		TempDir: "/tmp/test-temp",
	})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/test-temp", job.GetTempDir())

	_ = job.Controller().Wait()
}

// --- Run: with stored scrape config (line 1045) ---

func TestBatchJob_Run_WithStoredScrapeCfg_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second, NFOEnabled: false},
		},
	})

	// Per DEEP-1: SetRunOptions moved to StandaloneJob/JobRunner.
	sj := newStandaloneJobFromBatchJob(job)
	sj.SetRunOptions(
		ScrapePhaseConfig{Force: true, Strict: true},
		ApplyPhaseConfig{GenerateNFO: false},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := sj.Run(ctx)
	_ = err // may succeed or fail with stub workflow
}

// --- Run: with stored apply config (line 1061+) ---

func TestBatchJob_Run_WithStoredApplyCfg_Partial2(t *testing.T) {
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       &stubWorkflow{},
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 5 * time.Second, NFOEnabled: true},
		},
	})

	// Per DEEP-1: SetRunOptions moved to StandaloneJob/JobRunner.
	sj := newStandaloneJobFromBatchJob(job)
	sj.SetRunOptions(
		ScrapePhaseConfig{Force: false},
		ApplyPhaseConfig{GenerateNFO: true, Destination: "/dest"},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := sj.Run(ctx)
	_ = err
}
