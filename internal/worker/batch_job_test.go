package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// movieIDForResult extracts the primary movieID for a MovieResult.
// Prefers result.Movie.ID when available, falls back to FileMatchInfo.MovieID.
// Returns ("", false) if neither is set.
func movieIDForResult(r *MovieResult) (string, bool) {
	if r == nil {
		return "", false
	}
	if r.Movie != nil && r.Movie.ID != "" {
		return r.Movie.ID, true
	}
	if r.FileMatchInfo.MovieID != "" {
		return r.FileMatchInfo.MovieID, true
	}
	return "", false
}

// SetResultDirect sets a file result directly in the results map and rebuilds
// the movieID index. Use this in test helpers instead of directly assigning
// to job.results.Results, which bypasses index maintenance.
func (job *BatchJob) SetResultDirect(filePath string, result *MovieResult) {
	job.results.mu.Lock()
	defer job.results.mu.Unlock()

	job.results.Results[filePath] = result
	job.results.rebuildMovieIDIndexLocked()
}

func TestNewBatchJobDeps(t *testing.T) {
	t.Run("constructs deps with core fields", func(t *testing.T) {
		wf := &stubWorkflow{}
		m := &stubMatcher{}
		cfg := BatchJobConfig{
			MaxWorkers:      4,
			WorkerTimeout:   30 * time.Second,
			ScraperPriority: []string{"r18dev"},
			NFOEnabled:      true,
		}

		deps := NewBatchJobDeps(wf, m, nil, cfg)

		assert.Equal(t, wf, deps.WF)
		assert.Equal(t, m, deps.Matcher)
		assert.Nil(t, deps.PosterGen)
		assert.Equal(t, 4, deps.BatchCfg.MaxWorkers)
		assert.Equal(t, 30*time.Second, deps.BatchCfg.WorkerTimeout)
		assert.Equal(t, []string{"r18dev"}, deps.BatchCfg.ScraperPriority)
		assert.True(t, deps.BatchCfg.NFOEnabled)
	})

	t.Run("optional fields start nil and can be set after construction", func(t *testing.T) {
		deps := NewBatchJobDeps(nil, nil, nil, BatchJobConfig{})

		assert.Nil(t, deps.BatchFileOpRepo)
		assert.Nil(t, deps.MovieRepo)
		assert.Nil(t, deps.HistoryRepo)
		assert.Nil(t, deps.Emitter)
		assert.Nil(t, deps.PersistFn)
		assert.Nil(t, deps.Logger)

		// Verify optional fields can be set after construction
		persistCalled := false
		deps.PersistFn = func() { persistCalled = true }
		deps.PersistFn()
		assert.True(t, persistCalled)
	})

	t.Run("all three construction paths produce equivalent BatchJobDeps", func(t *testing.T) {
		wf := &stubWorkflow{}
		m := &stubMatcher{}
		cfg := BatchJobConfig{
			MaxWorkers:    2,
			WorkerTimeout: 5 * time.Second,
			NFOEnabled:    true,
		}

		// Path 1: NewBatchJobDeps (the unified constructor)
		deps := NewBatchJobDeps(wf, m, nil, cfg)

		// Path 2: via JobConfig embedding (how CLI/TUI/API consume it)
		jobCfg := &JobConfig{
			BatchJobDeps: NewBatchJobDeps(wf, m, nil, cfg),
		}

		assert.Equal(t, deps.WF, jobCfg.WF)
		assert.Equal(t, deps.Matcher, jobCfg.Matcher)
		assert.Equal(t, deps.BatchCfg, jobCfg.BatchCfg)
	})
}

func TestBatchJob_CompleteRescrape(t *testing.T) {
	t.Run("active job with matching revision returns success and updates Results", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, t.TempDir(), nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.SetResultDirect("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "OLD-001"},
			Status:        models.JobStatusCompleted,
			Revision:      1,
			Movie:         &models.Movie{ID: "OLD-001"},
		})
		job.results.Completed = 1
		job.results.Progress = 100

		newResult := &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "NEW-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "NEW-001"},
		}

		outcome, err := job.rescrapePhase.CompleteRescrape(rescrapePhaseInputs{ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "file1.mp4", newResult, 1, "NEW-001", "OLD-001")
		require.NoError(t, err)
		assert.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
		assert.Equal(t, uint64(2), job.results.Results["file1.mp4"].Revision)
		assert.Equal(t, "NEW-001", job.results.Results["file1.mp4"].FileMatchInfo.MovieID)
	})

	t.Run("deleted job returns IsGone", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.lifecycle.deleted = true

		newResult := &MovieResult{FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "NEW-001"}, Status: models.JobStatusCompleted}
		outcome, err := job.rescrapePhase.CompleteRescrape(rescrapePhaseInputs{ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "file1.mp4", newResult, 0, "NEW-001", "")
		require.NoError(t, err)
		assert.Equal(t, models.RescrapeStatusGone, outcome.Status)
	})

	t.Run("transitioned job (Running) returns IsGone", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.lifecycle.Status = models.JobStatusRunning

		newResult := &MovieResult{FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "NEW-001"}, Status: models.JobStatusCompleted}
		outcome, err := job.rescrapePhase.CompleteRescrape(rescrapePhaseInputs{ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "file1.mp4", newResult, 0, "NEW-001", "")
		require.NoError(t, err)
		assert.Equal(t, models.RescrapeStatusGone, outcome.Status)
	})

	t.Run("mismatched revision returns Conflict", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.SetResultDirect("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "OLD-001"},
			Status:        models.JobStatusCompleted,
			Revision:      5,
		})

		newResult := &MovieResult{FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "NEW-001"}, Status: models.JobStatusCompleted}
		outcome, err := job.rescrapePhase.CompleteRescrape(rescrapePhaseInputs{ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "file1.mp4", newResult, 3, "NEW-001", "")
		require.NoError(t, err)
		assert.Equal(t, models.RescrapeStatusConflict, outcome.Status)
	})

	t.Run("applies multipart metadata from models.FileMatchInfo", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.results.FileMatchInfo["file1.mp4"] = models.FileMatchInfo{
			MovieID:     "ABC-001",
			IsMultiPart: true,
			PartNumber:  2,
			PartSuffix:  "CD2",
		}
		job.SetResultDirect("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Revision:      1,
		})

		newResult := &MovieResult{FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"}, Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "ABC-001"}}
		_, err := job.rescrapePhase.CompleteRescrape(rescrapePhaseInputs{ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "file1.mp4", newResult, 1, "ABC-001", "ABC-001")
		require.NoError(t, err)
		assert.Equal(t, uint64(2), job.results.Results["file1.mp4"].Revision)
	})

	t.Run("skips poster cleanup when another result still uses old movie ID", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, t.TempDir(), nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4"})
		job.SetResultDirect("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "SHARED-001"},
			Status:        models.JobStatusCompleted,
			Revision:      1,
			Movie:         &models.Movie{ID: "SHARED-001"},
		})
		job.SetResultDirect("file2.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file2.mp4", MovieID: "SHARED-001"},
			Status:        models.JobStatusCompleted,
			Revision:      1,
			Movie:         &models.Movie{ID: "SHARED-001"},
		})
		job.ID = "test-job-456"

		newResult := &MovieResult{FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "NEW-001"}, Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "NEW-001"}}
		outcome, err := job.rescrapePhase.CompleteRescrape(rescrapePhaseInputs{ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "file1.mp4", newResult, 1, "NEW-001", "SHARED-001")
		require.NoError(t, err)
		assert.NotContains(t, outcome.OrphanedMovieIDs, "SHARED-001")
	})

	t.Run("recalculates progress after update", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4"})
		job.SetResultDirect("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Revision:      1,
		})
		job.results.Completed = 1
		job.results.Failed = 0
		job.results.Progress = 50.0

		newResult := &MovieResult{FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"}, Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "ABC-001"}}
		_, err := job.rescrapePhase.CompleteRescrape(rescrapePhaseInputs{ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "file1.mp4", newResult, 1, "ABC-001", "ABC-001")
		require.NoError(t, err)
		assert.Equal(t, 1, job.results.Completed)
		assert.Equal(t, 50.0, job.results.Progress)
	})
}

func TestBatchJob_IsJobActive(t *testing.T) {
	t.Run("returns true for Pending job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		assert.True(t, job.lifecycle.IsJobActive())
	})

	t.Run("returns true for Completed job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.lifecycle.MarkCompleted()
		assert.True(t, job.lifecycle.IsJobActive())
	})

	t.Run("returns false for deleted job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.lifecycle.deleted = true
		assert.False(t, job.lifecycle.IsJobActive())
	})

	t.Run("returns false for Running job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.lifecycle.Status = models.JobStatusRunning
		assert.False(t, job.lifecycle.IsJobActive())
	})

	t.Run("returns false for Organized job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.lifecycle.Status = models.JobStatusOrganized
		assert.False(t, job.lifecycle.IsJobActive())
	})

	t.Run("returns false for Failed job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.lifecycle.Status = models.JobStatusFailed
		assert.False(t, job.lifecycle.IsJobActive())
	})

	t.Run("returns false for Cancelled job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.lifecycle.Status = models.JobStatusCancelled
		assert.False(t, job.lifecycle.IsJobActive())
	})

	t.Run("returns false for Reverted job", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.lifecycle.Status = models.JobStatusReverted
		assert.False(t, job.lifecycle.IsJobActive())
	})
}

func TestBatchJob_GetMovieResult(t *testing.T) {
	t.Run("returns deep-copied result for existing filePath", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.SetResultDirect("file1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Revision:      3,
			Movie:         &models.Movie{ID: "ABC-001", Title: "Test Movie"},
		})

		result, err := job.results.GetMovieResult("file1.mp4")
		require.NoError(t, err)
		assert.Equal(t, "ABC-001", result.FileMatchInfo.MovieID)
		assert.Equal(t, uint64(3), result.Revision)
		assert.NotSame(t, job.results.Results["file1.mp4"], result, "should return a deep copy")

		result.FileMatchInfo.MovieID = "MODIFIED"
		assert.Equal(t, "ABC-001", job.results.Results["file1.mp4"].FileMatchInfo.MovieID, "mutation of copy should not affect original")
	})

	t.Run("returns error for non-existent filePath", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})

		_, err := job.results.GetMovieResult("nonexistent.mp4")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("returns error for nil result entry", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.SetResultDirect("file1.mp4", nil)

		_, err := job.results.GetMovieResult("file1.mp4")
		assert.Error(t, err)
	})
}

func TestBuildScrapeInputs(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF: wf,
			BatchCfg: BatchJobConfig{
				MaxWorkers:    3,
				WorkerTimeout: 10 * time.Second,
			},
		},
	})

	inputs := job.controller.buildScrapeInputs(wf, job.deps.BatchCfg, nil)

	assert.Equal(t, job.ID, inputs.JobID)
	assert.Equal(t, 3, inputs.Concurrency.MaxWorkers)
	assert.Equal(t, 10*time.Second, inputs.Concurrency.WorkerTimeout)
	assert.Equal(t, wf, inputs.WF)
	assert.Equal(t, job.results, inputs.Updater)
	assert.Equal(t, job.lifecycle, inputs.Lifecycle)
	assert.NotNil(t, inputs.Broadcaster)
	assert.Nil(t, inputs.Matcher, "Matcher should be nil when job.deps.Matcher is nil")
}

func TestBuildScrapeInputs_WithMatcher(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       wf,
			BatchCfg: BatchJobConfig{MaxWorkers: 1},
		},
	})
	// Set match to a real *matcher.Matcher to verify the construction method
	// conditionally sets inputs.Matcher when job.deps.Matcher is non-nil.
	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	job.deps.Matcher = m

	inputs := job.controller.buildScrapeInputs(wf, job.deps.BatchCfg, nil)
	assert.NotNil(t, inputs.Matcher, "Matcher should be set when job.deps.Matcher is non-nil")
}

func TestBuildApplyInputs(t *testing.T) {
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "TEST-001"}}}
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF: wf,
			BatchCfg: BatchJobConfig{
				MaxWorkers: 2,
				NFOEnabled: true,
			},
		},
	})
	job.cfg.destination = "/output"
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001"},
	})

	inputs := job.controller.buildApplyInputs(wf, job.deps.BatchCfg, ApplyPhaseConfig{Destination: "/output"}, nil)

	assert.Equal(t, job.ID, inputs.JobID)
	assert.Equal(t, 2, inputs.Concurrency.MaxWorkers, "Apply phase uses configured MaxWorkers when > 0")
	assert.True(t, inputs.NFOEnabled)
	assert.Equal(t, wf, inputs.WF)
	assert.Equal(t, "/output", inputs.Destination)
	assert.Equal(t, job.results, inputs.Updater)
	assert.Equal(t, job.lifecycle, inputs.Lifecycle)
	assert.NotNil(t, inputs.Results)
	assert.NotNil(t, inputs.Broadcaster)
}

func TestBuildRescrapeInputs(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       wf,
			BatchCfg: BatchJobConfig{MaxWorkers: 1},
		},
	})

	inputs := job.controller.buildRescrapeInputs(wf, job.deps.BatchCfg)

	assert.Equal(t, job.ID, inputs.JobID)
	assert.Equal(t, wf, inputs.WF)
	assert.Equal(t, job.resultIndex, inputs.ResultMap)
	assert.Equal(t, job.lifecycle, inputs.Lifecycle)
}

func TestBuildScrapeInputs_DefaultsApplied(t *testing.T) {
	wf := &stubWorkflow{scrapeResult: makeScrapeResult("TEST-001")}
	job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       wf,
			BatchCfg: BatchJobConfig{}, // All zeros
		},
	})

	inputs := job.controller.buildScrapeInputs(wf, BatchJobConfig{}, nil)

	assert.Equal(t, defaultMaxWorkers, inputs.Concurrency.MaxWorkers, "Should apply default MaxWorkers")
	assert.Equal(t, defaultWorkerTimeout, inputs.Concurrency.WorkerTimeout, "Should apply default WorkerTimeout")
}

func TestSetOperationModeOverride_InvalidReturnsError(t *testing.T) {
	t.Run("returns error on invalid mode", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		err := job.controller.SetOperationModeOverride(operationmode.OperationMode("invalid-mode"))
		assert.Error(t, err)
	})

	t.Run("returns nil on valid modes", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		for _, mode := range []operationmode.OperationMode{operationmode.OperationModeOrganize, operationmode.OperationModeInPlace} {
			err := job.controller.SetOperationModeOverride(mode)
			assert.NoError(t, err, "mode %q should be valid", mode)
		}
	})

	t.Run("empty string defaults to organize", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		err := job.controller.SetOperationModeOverride(operationmode.OperationMode(""))
		assert.NoError(t, err)
		assert.Equal(t, operationmode.OperationModeOrganize, job.GetOperationModeOverride())
	})
}

func TestSetOperationModeFromDB_InvalidResets(t *testing.T) {
	t.Run("invalid DB mode leaves operation mode empty and does not panic", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		assert.NotPanics(t, func() {
			job.controller.SetOperationModeOverride(operationmode.OperationMode("corrupted-value"))
		})
		assert.Equal(t, operationmode.OperationMode(""), job.GetOperationModeOverride())
	})

	t.Run("valid DB mode is preserved", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetOperationModeOverride(operationmode.OperationModeOrganize)
		assert.Equal(t, operationmode.OperationModeOrganize, job.GetOperationModeOverride())
	})

	t.Run("in-place DB mode is preserved", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetOperationModeOverride(operationmode.OperationModeInPlace)
		assert.Equal(t, operationmode.OperationModeInPlace, job.GetOperationModeOverride())
	})

	t.Run("empty DB mode defaults to organize", func(t *testing.T) {
		job := newBatchJob([]string{"file1.mp4"})
		job.controller.SetOperationModeOverride(operationmode.OperationMode(""))
		assert.Equal(t, operationmode.OperationModeOrganize, job.GetOperationModeOverride())
	})
}

func TestBatchJob_StartApply_UpdateNilPreservesExisting(t *testing.T) {
	t.Run("Update nil preserves existing true value", func(t *testing.T) {
		wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "TEST-001"}}}
		job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
			BatchJobDeps: BatchJobDeps{
				WF:       wf,
				BatchCfg: BatchJobConfig{MaxWorkers: 1},
			},
		})
		job.cfg.update = true

		// StartApply requires Completed lifecycle status (API-1+2: CAS fix)
		job.controller.markStarted(models.JobStatusPending)
		job.lifecycle.MarkCompleted()

		// Call StartApply with Update: nil — should preserve job.cfg.update = true
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := job.Controller().StartApply(ctx, ApplyPhaseConfig{Destination: "/out"})
		require.NoError(t, err)
		assert.True(t, job.cfg.update, "update should be preserved when cfg.Update is nil")
	})

	t.Run("Update nil preserves existing false value", func(t *testing.T) {
		wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "TEST-001"}}}
		job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
			BatchJobDeps: BatchJobDeps{
				WF:       wf,
				BatchCfg: BatchJobConfig{MaxWorkers: 1},
			},
		})
		job.cfg.update = false

		// StartApply requires Completed lifecycle status (API-1+2: CAS fix)
		job.controller.markStarted(models.JobStatusPending)
		job.lifecycle.MarkCompleted()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := job.Controller().StartApply(ctx, ApplyPhaseConfig{Destination: "/out"})
		require.NoError(t, err)
		assert.False(t, job.cfg.update, "update should remain false when cfg.Update is nil")
	})

	t.Run("Update true overrides existing false", func(t *testing.T) {
		wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "TEST-001"}}}
		job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
			BatchJobDeps: BatchJobDeps{
				WF:       wf,
				BatchCfg: BatchJobConfig{MaxWorkers: 1},
			},
		})
		job.cfg.update = false

		// StartApply requires Completed lifecycle status (API-1+2: CAS fix)
		job.controller.markStarted(models.JobStatusPending)
		job.lifecycle.MarkCompleted()

		updateTrue := true
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := job.Controller().StartApply(ctx, ApplyPhaseConfig{Destination: "/out", Update: &updateTrue})
		require.NoError(t, err)
		assert.True(t, job.cfg.update, "update should be set to true when cfg.Update is explicitly true")
	})

	t.Run("Update false overrides existing true", func(t *testing.T) {
		wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "TEST-001"}}}
		job := newBatchJob([]string{"file1.mp4"}, &JobConfig{
			BatchJobDeps: BatchJobDeps{
				WF:       wf,
				BatchCfg: BatchJobConfig{MaxWorkers: 1},
			},
		})
		job.cfg.update = true

		// StartApply requires Completed lifecycle status (API-1+2: CAS fix)
		job.controller.markStarted(models.JobStatusPending)
		job.lifecycle.MarkCompleted()

		updateFalse := false
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := job.Controller().StartApply(ctx, ApplyPhaseConfig{Destination: "/out", Update: &updateFalse})
		require.NoError(t, err)
		assert.False(t, job.cfg.update, "update should be set to false when cfg.Update is explicitly false")
	})
}

func TestBatchJob_UpdatePosterCrop(t *testing.T) {
	t.Run("backs up original poster values on first crop", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, t.TempDir(), nil, nil)
		job := jq.CreateJobBatch([]string{"/tmp/ABC-001.mp4"})
		job.SetResultDirect("/tmp/ABC-001.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/ABC-001.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001", Poster: models.PosterState{PosterURL: "original.jpg", CroppedPosterURL: "original-crop.jpg", ShouldCropPoster: true}},
		})

		err := job.posterEditor.UpdatePosterCrop("ABC-001", "new-crop.jpg")
		require.NoError(t, err)

		result := job.results.Results["/tmp/ABC-001.mp4"]
		assert.Equal(t, "original.jpg", result.Movie.Poster.OriginalPosterURL)
		assert.Equal(t, "original-crop.jpg", result.Movie.Poster.OriginalCroppedPosterURL)
		require.NotNil(t, result.Movie.Poster.OriginalShouldCropPoster)
		assert.True(t, *result.Movie.Poster.OriginalShouldCropPoster)
		assert.Equal(t, "new-crop.jpg", result.Movie.Poster.CroppedPosterURL)
		assert.False(t, result.Movie.Poster.ShouldCropPoster)
	})

	t.Run("does not overwrite backup on second crop", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, t.TempDir(), nil, nil)
		job := jq.CreateJobBatch([]string{"/tmp/ABC-001.mp4"})
		job.SetResultDirect("/tmp/ABC-001.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/ABC-001.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001", Poster: models.PosterState{PosterURL: "original.jpg", ShouldCropPoster: true}},
		})

		err := job.posterEditor.UpdatePosterCrop("ABC-001", "crop1.jpg")
		require.NoError(t, err)
		err = job.posterEditor.UpdatePosterCrop("ABC-001", "crop2.jpg")
		require.NoError(t, err)

		result := job.results.Results["/tmp/ABC-001.mp4"]
		assert.Equal(t, "original.jpg", result.Movie.Poster.OriginalPosterURL, "backup should remain from first crop")
		assert.Equal(t, "crop2.jpg", result.Movie.Poster.CroppedPosterURL)
	})

	t.Run("updates all file parts for multipart movie", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, t.TempDir(), nil, nil)
		job := jq.CreateJobBatch([]string{"/tmp/ABC-001-cd1.mp4", "/tmp/ABC-001-cd2.mp4"})
		job.SetResultDirect("/tmp/ABC-001-cd1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/ABC-001-cd1.mp4", MovieID: "ABC-001"},
			Movie:         &models.Movie{ID: "ABC-001", Poster: models.PosterState{PosterURL: "poster.jpg"}},
		})
		job.SetResultDirect("/tmp/ABC-001-cd2.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/ABC-001-cd2.mp4", MovieID: "ABC-001"},
			Movie:         &models.Movie{ID: "ABC-001", Poster: models.PosterState{PosterURL: "poster.jpg"}},
		})

		err := job.posterEditor.UpdatePosterCrop("ABC-001", "new-crop.jpg")
		require.NoError(t, err)

		assert.Equal(t, "new-crop.jpg", job.results.Results["/tmp/ABC-001-cd1.mp4"].Movie.Poster.CroppedPosterURL)
		assert.Equal(t, "new-crop.jpg", job.results.Results["/tmp/ABC-001-cd2.mp4"].Movie.Poster.CroppedPosterURL)
	})

	t.Run("skips nil movie without error", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, t.TempDir(), nil, nil)
		job := jq.CreateJobBatch([]string{"/tmp/ABC-001.mp4"})
		job.SetResultDirect("/tmp/ABC-001.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/ABC-001.mp4", MovieID: "ABC-001"},
			Movie:         nil,
		})

		err := job.posterEditor.UpdatePosterCrop("ABC-001", "crop.jpg")
		require.NoError(t, err)
	})

	t.Run("preserves original ShouldCropPoster value despite subsequent false assignment", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, t.TempDir(), nil, nil)
		job := jq.CreateJobBatch([]string{"/tmp/ABC-001.mp4"})
		job.SetResultDirect("/tmp/ABC-001.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/ABC-001.mp4", MovieID: "ABC-001"},
			Movie:         &models.Movie{ID: "ABC-001", Poster: models.PosterState{PosterURL: "poster.jpg", ShouldCropPoster: true}},
		})

		err := job.posterEditor.UpdatePosterCrop("ABC-001", "crop.jpg")
		require.NoError(t, err)

		result := job.results.Results["/tmp/ABC-001.mp4"]
		assert.False(t, result.Movie.Poster.ShouldCropPoster, "ShouldCropPoster should be false after crop")
		require.NotNil(t, result.Movie.Poster.OriginalShouldCropPoster, "OriginalShouldCropPoster should be set")
		assert.True(t, *result.Movie.Poster.OriginalShouldCropPoster, "OriginalShouldCropPoster should preserve pre-mutation value (true), not post-mutation (false)")
	})
}

func TestBatchJob_UpdatePosterFromURL(t *testing.T) {
	t.Run("backs up and updates poster URL and cropped URL", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, t.TempDir(), nil, nil)
		job := jq.CreateJobBatch([]string{"/tmp/ABC-001.mp4"})
		job.SetResultDirect("/tmp/ABC-001.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/ABC-001.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001", Poster: models.PosterState{PosterURL: "old-poster.jpg", CroppedPosterURL: "old-crop.jpg", ShouldCropPoster: true}},
		})

		err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "ABC-001", "new-poster.jpg", "new-crop.jpg")
		require.NoError(t, err)

		result := job.results.Results["/tmp/ABC-001.mp4"]
		assert.Equal(t, "old-poster.jpg", result.Movie.Poster.OriginalPosterURL)
		assert.Equal(t, "new-poster.jpg", result.Movie.Poster.PosterURL)
		assert.Equal(t, "new-crop.jpg", result.Movie.Poster.CroppedPosterURL)
		assert.False(t, result.Movie.Poster.ShouldCropPoster)
		require.NotNil(t, result.Movie.Poster.OriginalShouldCropPoster)
		assert.True(t, *result.Movie.Poster.OriginalShouldCropPoster)
	})

	t.Run("does not overwrite backup on second update", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, t.TempDir(), nil, nil)
		job := jq.CreateJobBatch([]string{"/tmp/ABC-001.mp4"})
		job.SetResultDirect("/tmp/ABC-001.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/ABC-001.mp4", MovieID: "ABC-001"},
			Movie:         &models.Movie{ID: "ABC-001", Poster: models.PosterState{PosterURL: "original.jpg", ShouldCropPoster: true}},
		})

		err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "ABC-001", "poster1.jpg", "crop1.jpg")
		require.NoError(t, err)
		err = job.posterEditor.UpdatePosterFromURL(context.TODO(), "ABC-001", "poster2.jpg", "crop2.jpg")
		require.NoError(t, err)

		result := job.results.Results["/tmp/ABC-001.mp4"]
		assert.Equal(t, "original.jpg", result.Movie.Poster.OriginalPosterURL, "backup should remain from first update")
		assert.Equal(t, "poster2.jpg", result.Movie.Poster.PosterURL)
	})

	t.Run("updates all file parts for multipart movie", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, t.TempDir(), nil, nil)
		job := jq.CreateJobBatch([]string{"/tmp/ABC-001-cd1.mp4", "/tmp/ABC-001-cd2.mp4"})
		job.SetResultDirect("/tmp/ABC-001-cd1.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/ABC-001-cd1.mp4", MovieID: "ABC-001"},
			Movie:         &models.Movie{ID: "ABC-001", Poster: models.PosterState{PosterURL: "poster.jpg"}},
		})
		job.SetResultDirect("/tmp/ABC-001-cd2.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/ABC-001-cd2.mp4", MovieID: "ABC-001"},
			Movie:         &models.Movie{ID: "ABC-001", Poster: models.PosterState{PosterURL: "poster.jpg"}},
		})

		err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "ABC-001", "new-poster.jpg", "new-crop.jpg")
		require.NoError(t, err)

		assert.Equal(t, "new-poster.jpg", job.results.Results["/tmp/ABC-001-cd1.mp4"].Movie.Poster.PosterURL)
		assert.Equal(t, "new-poster.jpg", job.results.Results["/tmp/ABC-001-cd2.mp4"].Movie.Poster.PosterURL)
	})

	t.Run("skips nil movie without error", func(t *testing.T) {
		jq := NewJobStore(nil, nil, nil, t.TempDir(), nil, nil)
		job := jq.CreateJobBatch([]string{"/tmp/ABC-001.mp4"})
		job.SetResultDirect("/tmp/ABC-001.mp4", &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/ABC-001.mp4", MovieID: "ABC-001"},
			Movie:         nil,
		})

		err := job.posterEditor.UpdatePosterFromURL(context.TODO(), "ABC-001", "poster.jpg", "crop.jpg")
		require.NoError(t, err)
	})
}

// TestBatchJobInterface_ViaCreateJob verifies that JobStore.CreateJob returns
// a valid BatchJobInterface that satisfies the full lifecycle seam (DEEP-1).
func TestBatchJobInterface_ViaCreateJob(t *testing.T) {
	store := NewJobStore(nil, nil, nil, "", nil, nil)
	job := store.CreateJob([]string{"file1.mp4", "file2.mp4"})

	// Verify the interface methods are accessible
	assert.NotEmpty(t, job.GetID())
	assert.Equal(t, models.JobStatusPending, job.GetJobStatus())

	status := job.GetStatus()
	assert.NotNil(t, status)
	assert.Equal(t, 2, status.TotalFiles)

	results := job.GetResults()
	assert.Empty(t, results)

	// Verify lookup methods
	paths := job.FindFilePathsForMovieID("nonexistent")
	assert.Empty(t, paths)

	_, _, found := job.GetFileResultByResultID("nonexistent")
	assert.False(t, found)

	// Verify the interface can be used for cancellation
	job.Cancel()
	assert.Equal(t, models.JobStatusCancelled, job.GetJobStatus())
}

// TestBatchJobInterface_ViaGetBatchJob verifies that JobStore.GetBatchJob
// returns a valid BatchJobInterface for an existing job (DEEP-1).
func TestBatchJobInterface_ViaGetBatchJob(t *testing.T) {
	store := NewJobStore(nil, nil, nil, "", nil, nil)
	created := store.CreateJob([]string{"file1.mp4"})
	jobID := created.GetID()

	retrieved, ok := store.GetBatchJob(jobID)
	require.True(t, ok)
	assert.Equal(t, jobID, retrieved.GetID())

	// Verify edit methods are accessible on the same interface
	status := retrieved.GetStatus()
	assert.NotNil(t, status)

	// Verify that GetJobForControl and GetJobForEdit still work
	controlled, ok := store.GetJobForControl(jobID)
	require.True(t, ok)
	assert.Equal(t, jobID, controlled.GetID())

	editable, ok := store.GetJobForEdit(jobID)
	require.True(t, ok)
	assert.Equal(t, jobID, editable.GetID())
}
