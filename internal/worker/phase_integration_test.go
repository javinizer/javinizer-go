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

// Integration tests that exercise the full path from BatchJob through PhaseInputs.
// These verify the wiring from BatchJob → PhaseInputs → Phase → callbacks → back to BatchJob.

// integrationScrapeWF is a stub WorkflowInterface for integration tests.
// Scrape returns a successful result; Apply is a no-op.
type integrationScrapeWF struct {
	scrapeResult *scrape.ScrapeResult
	scrapeErr    error
	mu           sync.Mutex
	scrapeCalled int
}

func (w *integrationScrapeWF) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	w.mu.Lock()
	w.scrapeCalled++
	w.mu.Unlock()
	return w.scrapeResult, nil, w.scrapeErr
}

func (w *integrationScrapeWF) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return nil, nil
}

func (w *integrationScrapeWF) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}

func (w *integrationScrapeWF) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}

func (w *integrationScrapeWF) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

func (w *integrationScrapeWF) getScrapeCalled() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.scrapeCalled
}

// integrationApplyWF is a stub WorkflowInterface for integration tests.
// Apply returns a successful result; Scrape is a no-op.
type integrationApplyWF struct {
	applyResult *workflow.ApplyResult
	applyErr    error
	mu          sync.Mutex
	applyCalled int
}

func (w *integrationApplyWF) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return nil, nil, nil
}

func (w *integrationApplyWF) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	w.mu.Lock()
	w.applyCalled++
	w.mu.Unlock()
	return w.applyResult, w.applyErr
}

func (w *integrationApplyWF) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}

func (w *integrationApplyWF) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}

func (w *integrationApplyWF) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

func (w *integrationApplyWF) getApplyCalled() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.applyCalled
}

// TestIntegration_ScrapePhase_ThroughBatchJob verifies that BatchJob.StartScrape
// correctly constructs scrapePhaseInputs and that the scrape phase runs end-to-end,
// updating results back on the BatchJob via the ResultUpdater callback.
func TestIntegration_ScrapePhase_ThroughBatchJob(t *testing.T) {
	wf := &integrationScrapeWF{
		scrapeResult: &scrape.ScrapeResult{
			Movie: &models.Movie{ID: "INT-001"},
		},
	}

	job := newBatchJob([]string{"/source/INT-001.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       wf,
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 0, NFOEnabled: true},
		},
	})

	err := job.Controller().StartScrape(context.Background(), []string{"/source/INT-001.mp4"}, ScrapePhaseConfig{})
	require.NoError(t, err, "StartScrape should not return an error")

	// Wait for the job to finish
	err = job.Controller().Wait()
	require.NoError(t, err, "Wait should not return an error for successful scrape")

	// Verify the result was written back to the BatchJob via ResultUpdater
	result, err := job.results.GetMovieResult("/source/INT-001.mp4")
	require.NoError(t, err, "Should find result for scraped file")
	assert.Equal(t, "INT-001", result.FileMatchInfo.MovieID, "MovieID should match scraped result")
	assert.Equal(t, models.JobStatusCompleted, result.Status, "Result should be completed")

	// Verify the job was marked as completed
	assert.Equal(t, models.JobStatusCompleted, job.lifecycle.GetJobStatus(), "Job status should be Completed")

	// Verify the WF was actually called
	assert.Equal(t, 1, wf.getScrapeCalled(), "Scrape should be called once")
}

// TestIntegration_ApplyPhase_ThroughBatchJob verifies that BatchJob.StartApply
// correctly constructs applyPhaseInputs and that the apply phase runs end-to-end,
// updating results back on the BatchJob via the ResultUpdater callback.
func TestIntegration_ApplyPhase_ThroughBatchJob(t *testing.T) {
	wf := &integrationApplyWF{
		applyResult: &workflow.ApplyResult{
			Movie: &models.Movie{ID: "INT-002"},
		},
	}

	job := newBatchJob([]string{"/source/INT-002.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       wf,
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 0, NFOEnabled: true},
		},
	})

	// Pre-populate results as if scrape completed
	job.results.UpdateFileResult("/source/INT-002.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-002.mp4", MovieID: "INT-002"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-002", Title: "Test Movie"},
	})
	job.cfg.destination = "/output"

	// StartApply requires Completed lifecycle status (API-1+2: CAS fix)
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()

	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})
	require.NoError(t, err, "StartApply should not return an error")

	// Wait for the job to finish
	err = job.Controller().Wait()
	require.NoError(t, err, "Wait should not return an error for successful apply")

	// Verify the result was updated via the Updater callback
	result, err := job.results.GetMovieResult("/source/INT-002.mp4")
	require.NoError(t, err, "Should find result for applied file")
	assert.Equal(t, models.JobStatusCompleted, result.Status, "Result should still be completed after apply")

	// Verify the job was marked as organized (all files organized successfully)
	assert.Equal(t, models.JobStatusOrganized, job.lifecycle.GetJobStatus(), "Job status should be Organized")

	// Verify the WF was actually called
	assert.Equal(t, 1, wf.getApplyCalled(), "Apply should be called once")
}

// TestIntegration_RescrapePhase_ThroughBatchJob verifies that BatchJob.ScrapeSingle
// and BatchJob.CompleteRescrape correctly construct rescrapePhaseInputs and that
// the rescrape phase works end-to-end.
func TestIntegration_RescrapePhase_ThroughBatchJob(t *testing.T) {
	wf := &integrationScrapeWF{
		scrapeResult: &scrape.ScrapeResult{
			Movie: &models.Movie{ID: "INT-003"},
		},
	}

	job := newBatchJob([]string{"/source/INT-003.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       wf,
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 0, NFOEnabled: true},
		},
	})

	// Pre-populate results as if scrape completed
	job.results.UpdateFileResult("/source/INT-003.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-003.mp4", MovieID: "INT-003"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-003"},
	})

	// Get the current revision before rescrape
	currentResult, err := job.results.GetMovieResult("/source/INT-003.mp4")
	require.NoError(t, err)
	capturedRevision := currentResult.Revision

	// ScrapeSingle should return a result
	scrapeResult, _, err := job.rescrapePhase.ScrapeSingle(context.Background(), rescrapePhaseInputs{JobID: job.ID, WF: job.deps.WF, ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "/source/INT-003.mp4", scrape.ScrapeCmd{MovieID: "INT-003"})
	require.NoError(t, err, "ScrapeSingle should not return an error")
	require.NotNil(t, scrapeResult, "ScrapeSingle should return a result")
	assert.Equal(t, "INT-003", scrapeResult.Movie.ID)

	// CompleteRescrape should commit the new result
	newResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-003.mp4", MovieID: "INT-003"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-003"},
	}
	outcome, err := job.rescrapePhase.CompleteRescrape(rescrapePhaseInputs{ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "/source/INT-003.mp4", newResult, capturedRevision, "INT-003", "INT-003")
	require.NoError(t, err, "CompleteRescrape should not return an error")
	require.NotNil(t, outcome)
	assert.Equal(t, models.RescrapeStatusSuccess, outcome.Status, "Status should be success")
	assert.Empty(t, outcome.OrphanedMovieIDs, "No orphaned IDs when movie ID unchanged")

	// Verify the result was committed to the BatchJob
	committedResult, err := job.results.GetMovieResult("/source/INT-003.mp4")
	require.NoError(t, err)
	assert.Equal(t, capturedRevision+1, committedResult.Revision, "Revision should be incremented after commit")
}

// TestIntegration_CallbackInterfaces_Satisfied verifies at compile time
// that *BatchJob satisfies all the callback interfaces used by phase inputs.
// The actual assertions are in phase_interfaces.go; this test provides
// an additional runtime check for defense in depth.
func TestIntegration_CallbackInterfaces_Satisfied(t *testing.T) {
	// These assertions mirror the compile-time checks in phase_interfaces.go.
	// They verify the interface satisfaction at runtime for clarity.
	var _ ResultUpdater = (*ResultTracker)(nil)
	var _ PhaseLifecycle = (*JobLifecycle)(nil)
	var _ ResultMapAccessor = (*ResultTracker)(nil)

	// Verify that BatchJob methods used by the phases exist and are callable
	job := newBatchJob(nil)

	// ResultUpdater methods (moved to ResultTracker per ADR-0041)
	assert.NotNil(t, job.results.UpdateFileResult, "ResultTracker should have UpdateFileResult")
	assert.NotNil(t, job.results.AtomicUpdateFileResult, "ResultTracker should have AtomicUpdateFileResult")

	// PhaseLifecycle methods (moved to JobLifecycle per ADR-0041)
	assert.NotNil(t, job.lifecycle.MarkCompleted, "JobLifecycle should have MarkCompleted")
	assert.NotNil(t, job.lifecycle.MarkFailed, "JobLifecycle should have MarkFailed")
	assert.NotNil(t, job.lifecycle.MarkCancelled, "JobLifecycle should have MarkCancelled")
	assert.NotNil(t, job.lifecycle.MarkOrganized, "JobLifecycle should have MarkOrganized")

	// ResultMapAccessor methods
	assert.NotNil(t, job.resultIndex.IsGone, "ResultTracker should have IsGone")
	assert.NotNil(t, job.results.GetFileMatchInfo, "BatchJob should have GetFileMatchInfo")
	assert.NotNil(t, job.resultIndex.GetCurrentMovieID, "ResultTracker should have GetCurrentMovieID")
	assert.NotNil(t, job.resultIndex.GetRevision, "ResultTracker should have GetRevision")
	assert.NotNil(t, job.resultIndex.CommitResult, "ResultTracker should have CommitResult")
	assert.NotNil(t, job.resultIndex.OtherResultUsesMovieID, "ResultTracker should have OtherResultUsesMovieID")
}

// TestIntegration_ScrapePhase_WFOverride_ThroughBatchJob verifies that
// the cfg.WF override in StartScrape is correctly propagated to the
// scrapePhaseInputs without mutating job.wf.
func TestIntegration_ScrapePhase_WFOverride_ThroughBatchJob(t *testing.T) {
	// Create a job without WF — the override WF should be used
	originalWF := &integrationScrapeWF{
		scrapeResult: &scrape.ScrapeResult{
			Movie: &models.Movie{ID: "ORIG-001"},
		},
	}
	overrideWF := &integrationScrapeWF{
		scrapeResult: &scrape.ScrapeResult{
			Movie: &models.Movie{ID: "OVERRIDE-001"},
		},
	}

	job := newBatchJob([]string{"/source/test.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       originalWF,
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 0, NFOEnabled: true},
		},
	})

	// Per DEEP-6: set WF on job.deps via SetWorkflow instead of ScrapePhaseConfig.WF override
	job.controller.SetWorkflow(overrideWF)

	err := job.Controller().StartScrape(context.Background(), []string{"/source/test.mp4"}, ScrapePhaseConfig{})
	require.NoError(t, err)

	err = job.Controller().Wait()
	require.NoError(t, err)

	// The SetWorkflow WF should have been used, not the original
	assert.Equal(t, 0, originalWF.getScrapeCalled(), "Original WF should NOT be called when SetWorkflow provides a different WF")
	assert.Equal(t, 1, overrideWF.getScrapeCalled(), "SetWorkflow WF should be called")

	// Verify the result uses the override WF's output
	result, err := job.results.GetMovieResult("/source/test.mp4")
	require.NoError(t, err)
	assert.Equal(t, "OVERRIDE-001", result.FileMatchInfo.MovieID, "Result should come from the override WF")
}

// TestIntegration_ApplyPhase_WFOverride_ThroughBatchJob verifies that
// the cfg.WF override in StartApply is correctly handled.
func TestIntegration_ApplyPhase_WFOverride_ThroughBatchJob(t *testing.T) {
	originalWF := &integrationApplyWF{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "ORIG-002"}},
	}
	overrideWF := &integrationApplyWF{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "OVERRIDE-002"}},
	}

	job := newBatchJob([]string{"/source/INT-002.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       originalWF,
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 0, NFOEnabled: true},
		},
	})

	// Pre-populate results
	job.results.UpdateFileResult("/source/INT-002.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-002.mp4", MovieID: "INT-002"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-002"},
	})

	// Per DEEP-6: set WF on job.deps via SetWorkflow instead of ApplyPhaseConfig.WF override
	job.controller.SetWorkflow(overrideWF)

	// StartApply requires Completed lifecycle status (API-1+2: CAS fix)
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()

	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})
	require.NoError(t, err)

	err = job.Controller().Wait()
	require.NoError(t, err)

	// The SetWorkflow WF should have been used, not the original
	assert.Equal(t, 0, originalWF.getApplyCalled(), "Original WF should NOT be called when SetWorkflow provides a different WF")
	assert.Equal(t, 1, overrideWF.getApplyCalled(), "SetWorkflow WF should be called")
}

// TestIntegration_ApplyPhase_NilWF_OverrideSucceeds verifies that StartApply
// succeeds when WF is provided via the cfg.WF override even though the job
// was created without one.
func TestIntegration_ApplyPhase_NilWF_OverrideSucceeds(t *testing.T) {
	overrideWF := &integrationApplyWF{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "OVERRIDE-003"}},
	}

	job := newBatchJob([]string{"/source/test.mp4"}) // No JobConfig

	// Pre-populate results (required for apply phase to have work)
	job.results.UpdateFileResult("/source/test.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/test.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001"},
	})

	// Per DEEP-6: set WF on job.deps via SetWorkflow instead of ApplyPhaseConfig.WF override
	job.controller.SetWorkflow(overrideWF)
	job.deps.BatchCfg = BatchJobConfig{MaxWorkers: 1}

	// StartApply requires Completed lifecycle status (API-1+2: CAS fix)
	job.controller.markStarted(models.JobStatusPending)
	job.lifecycle.MarkCompleted()

	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})
	require.NoError(t, err, "StartApply should succeed when WF provided via SetWorkflow")

	err = job.Controller().Wait()
	require.NoError(t, err)

	assert.Equal(t, 1, overrideWF.getApplyCalled(), "Override WF should be called")
}

// TestIntegration_BatchJobConfig_SetOnDeps_ThroughStartScrape verifies that
// BatchCfg set on job.deps is used by StartScrape. Per DEEP-6: BatchCfg override
// removed from ScrapePhaseConfig. BatchCfg is set on job.deps at construction
// or before phase calls.
func TestIntegration_BatchJobConfig_SetOnDeps_ThroughStartScrape(t *testing.T) {
	wf := &integrationScrapeWF{
		scrapeResult: &scrape.ScrapeResult{
			Movie: &models.Movie{ID: "CFG-001"},
		},
	}

	// Create job with minimal batch config
	job := newBatchJob([]string{"/source/CFG-001.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF: wf,
		},
	})

	// Per DEEP-6: set BatchCfg on job.deps directly instead of ScrapePhaseConfig.BatchCfg override
	job.deps.BatchCfg = BatchJobConfig{
		MaxWorkers:      2,
		WorkerTimeout:   30 * 1e9, // 30 seconds in nanoseconds
		ScraperPriority: []string{"r18dev"},
		NFOEnabled:      true,
	}

	err := job.Controller().StartScrape(context.Background(), []string{"/source/CFG-001.mp4"}, ScrapePhaseConfig{})
	require.NoError(t, err)

	err = job.Controller().Wait()
	require.NoError(t, err)

	// Verify the scrape ran with the override config
	result, err := job.results.GetMovieResult("/source/CFG-001.mp4")
	require.NoError(t, err)
	assert.Equal(t, "CFG-001", result.FileMatchInfo.MovieID)
	assert.Equal(t, models.JobStatusCompleted, result.Status)
}

// Compile-time assertions that our stub types satisfy the required interfaces.
var (
	_ workflow.WorkflowInterface = (*integrationScrapeWF)(nil)
	_ workflow.WorkflowInterface = (*integrationApplyWF)(nil)
)

// Helper to create a simple integration scrape WF
func newIntegrationScrapeWF(movieID string) *integrationScrapeWF {
	return &integrationScrapeWF{
		scrapeResult: &scrape.ScrapeResult{
			Movie: &models.Movie{ID: movieID},
		},
	}
}

// TestIntegration_CompleteRescrape_Conflict verifies that a concurrent
// modification between ScrapeSingle and CompleteRescrape is detected.
func TestIntegration_CompleteRescrape_Conflict(t *testing.T) {
	wf := newIntegrationScrapeWF("INT-004")

	job := newBatchJob([]string{"/source/INT-004.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       wf,
			BatchCfg: BatchJobConfig{MaxWorkers: 1, WorkerTimeout: 0, NFOEnabled: true},
		},
	})

	// Pre-populate with a result at revision 1
	job.results.UpdateFileResult("/source/INT-004.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-004.mp4", MovieID: "INT-004"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-004"},
	})

	// Simulate capturing revision before scrape
	result, _ := job.results.GetMovieResult("/source/INT-004.mp4")
	capturedRevision := result.Revision // Should be 1

	// Simulate a concurrent modification that increments the revision
	job.results.UpdateFileResult("/source/INT-004.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-004.mp4", MovieID: "INT-004"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-004"},
	})
	// Now revision is 2, but we captured 1

	newResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-004.mp4", MovieID: "INT-005"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-005"},
	}

	outcome, err := job.rescrapePhase.CompleteRescrape(rescrapePhaseInputs{ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "/source/INT-004.mp4", newResult, capturedRevision, "INT-005", "INT-004")
	// Conflict should be detected — the revision no longer matches
	require.NoError(t, err) // CompleteRescrape returns conflict in outcome, not as error
	require.NotNil(t, outcome)
	assert.Equal(t, models.RescrapeStatusConflict, outcome.Status, "Should detect conflict when revision changed between capture and commit")
}

// TestIntegration_ScrapePhase_NilWF_ReturnsError verifies that StartScrape
// returns an error when no workflow is configured.
func TestIntegration_ScrapePhase_NilWF_ReturnsError(t *testing.T) {
	job := newBatchJob([]string{"/source/test.mp4"}) // No JobConfig

	err := job.Controller().StartScrape(context.Background(), []string{"/source/test.mp4"}, ScrapePhaseConfig{})
	require.Error(t, err, "StartScrape should return error when no WF configured")
	assert.Contains(t, err.Error(), "workflow not configured")
}

// TestIntegration_ApplyPhase_NilWF_ReturnsError verifies that StartApply
// returns an error when no workflow is configured.
func TestIntegration_ApplyPhase_NilWF_ReturnsError(t *testing.T) {
	job := newBatchJob([]string{"/source/test.mp4"}) // No JobConfig

	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{})
	require.Error(t, err, "StartApply should return error when no WF configured")
	assert.Contains(t, err.Error(), "workflow not configured")
}

// TestIntegration_ScrapePhase_NilWF_SetWorkflowSucceeds verifies that StartScrape
// succeeds when WF is set on job.deps via SetWorkflow even though the job
// was created without one. Per DEEP-6: replaces the old ScrapePhaseConfig.WF override.
func TestIntegration_ScrapePhase_NilWF_SetWorkflowSucceeds(t *testing.T) {
	wf := newIntegrationScrapeWF("OVERRIDE-003")

	job := newBatchJob([]string{"/source/test.mp4"}) // No JobConfig

	// Per DEEP-6: set WF on job.deps via SetWorkflow instead of ScrapePhaseConfig.WF override
	job.controller.SetWorkflow(wf)
	job.deps.BatchCfg = BatchJobConfig{MaxWorkers: 1}

	err := job.Controller().StartScrape(context.Background(), []string{"/source/test.mp4"}, ScrapePhaseConfig{})
	require.NoError(t, err, "StartScrape should succeed when WF provided via SetWorkflow")

	err = job.Controller().Wait()
	require.NoError(t, err)

	result, err := job.results.GetMovieResult("/source/test.mp4")
	require.NoError(t, err)
	assert.Equal(t, "OVERRIDE-003", result.FileMatchInfo.MovieID)
}

// TestIntegration_RescrapePhase_NilWF_Error verifies that ScrapeSingle
// returns an error when no workflow is configured.
func TestIntegration_RescrapePhase_NilWF_Error(t *testing.T) {
	job := newBatchJob([]string{"/source/test.mp4"}) // No JobConfig

	_, _, err := job.rescrapePhase.ScrapeSingle(context.Background(), rescrapePhaseInputs{JobID: job.ID, WF: job.deps.WF, ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "/source/test.mp4", scrape.ScrapeCmd{MovieID: "TEST"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow not configured")
}

// TestIntegration_OrphanDetection verifies that CompleteRescrape detects
// orphaned movie IDs when the movie ID changes and no other result uses the old ID.
func TestIntegration_OrphanDetection(t *testing.T) {
	job := newBatchJob([]string{"/source/INT-005.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       newIntegrationScrapeWF("INT-005"),
			BatchCfg: BatchJobConfig{MaxWorkers: 1, NFOEnabled: true},
		},
	})

	// Pre-populate with a result
	job.results.UpdateFileResult("/source/INT-005.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-005.mp4", MovieID: "INT-005"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-005"},
	})

	result, _ := job.results.GetMovieResult("/source/INT-005.mp4")
	capturedRevision := result.Revision

	newResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-005.mp4", MovieID: "INT-006"}, // Different movie ID
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-006"},
	}

	outcome, err := job.rescrapePhase.CompleteRescrape(rescrapePhaseInputs{ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "/source/INT-005.mp4", newResult, capturedRevision, "INT-006", "INT-005")
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Contains(t, outcome.OrphanedMovieIDs, "INT-005", "Old movie ID should be orphaned when no other result uses it")
}

// TestIntegration_NoOrphanWhenSharedMovieID verifies that no orphan is detected
// when another result still references the old movie ID.
func TestIntegration_NoOrphanWhenSharedMovieID(t *testing.T) {
	job := newBatchJob([]string{"/source/INT-007A.mp4", "/source/INT-007B.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       newIntegrationScrapeWF("INT-007"),
			BatchCfg: BatchJobConfig{MaxWorkers: 1, NFOEnabled: true},
		},
	})

	// Pre-populate with two results sharing the same movie ID
	job.results.UpdateFileResult("/source/INT-007A.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-007A.mp4", MovieID: "INT-007"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-007"},
	})
	job.results.UpdateFileResult("/source/INT-007B.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-007B.mp4", MovieID: "INT-007"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-007"},
	})

	// Rescrape the first file to a different movie ID
	result, _ := job.results.GetMovieResult("/source/INT-007A.mp4")
	capturedRevision := result.Revision

	newResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-007A.mp4", MovieID: "INT-008"}, // Changed movie ID
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-008"},
	}

	outcome, err := job.rescrapePhase.CompleteRescrape(rescrapePhaseInputs{ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "/source/INT-007A.mp4", newResult, capturedRevision, "INT-008", "INT-007")
	require.NoError(t, err)
	require.NotNil(t, outcome)
	// INT-007 should NOT be orphaned because INT-007B still uses it
	assert.NotContains(t, outcome.OrphanedMovieIDs, "INT-007", "Should not be orphaned when another result still uses the movie ID")
}

// TestIntegration_MultipartMetadata_AppliedOnRescrape verifies that models.FileMatchInfo
// multipart metadata is applied when committing a rescrape result.
func TestIntegration_MultipartMetadata_AppliedOnRescrape(t *testing.T) {
	job := newBatchJob([]string{"/source/INT-009-pt1.mp4"}, &JobConfig{
		BatchJobDeps: BatchJobDeps{
			WF:       newIntegrationScrapeWF("INT-009"),
			BatchCfg: BatchJobConfig{MaxWorkers: 1, NFOEnabled: true},
		},
	})

	// Pre-populate with models.FileMatchInfo
	job.results.UpdateFileResult("/source/INT-009-pt1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-009-pt1.mp4", MovieID: "INT-009"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-009"},
	})
	job.mu.Lock()
	job.results.FileMatchInfo["/source/INT-009-pt1.mp4"] = models.FileMatchInfo{
		MovieID:     "INT-009",
		IsMultiPart: true,
		PartNumber:  1,
		PartSuffix:  "pt1",
	}
	job.mu.Unlock()

	result, _ := job.results.GetMovieResult("/source/INT-009-pt1.mp4")
	capturedRevision := result.Revision

	newResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/INT-009-pt1.mp4", MovieID: "INT-009"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "INT-009"},
	}

	outcome, err := job.rescrapePhase.CompleteRescrape(rescrapePhaseInputs{ResultMap: job.resultIndex, Lifecycle: job.lifecycle}, "/source/INT-009-pt1.mp4", newResult, capturedRevision, "INT-009", "INT-009")
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Empty(t, outcome.OrphanedMovieIDs)

	// Verify multipart metadata was applied
	committed, err := job.results.GetMovieResult("/source/INT-009-pt1.mp4")
	require.NoError(t, err)
	assert.True(t, committed.FileMatchInfo.IsMultiPart, "IsMultiPart should be set from models.FileMatchInfo")
	assert.Equal(t, 1, committed.FileMatchInfo.PartNumber, "PartNumber should be set from models.FileMatchInfo")
	assert.Equal(t, "pt1", committed.FileMatchInfo.PartSuffix, "PartSuffix should be set from models.FileMatchInfo")
}

// Compile-time assertions for integration test stubs
var _ ResultUpdater = (*ResultTracker)(nil)
var _ PhaseLifecycle = (*JobLifecycle)(nil)
var _ ResultMapAccessor = (*ResultTracker)(nil)
