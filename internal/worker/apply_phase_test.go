package worker

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubApplyWorkflow implements workflow.WorkflowInterface for ApplyPhase tests.
// Only Apply is functional; other methods return nil/zero.
type stubApplyWorkflow struct {
	applyResult *workflow.ApplyResult
	applyErr    error
	applyCalled int
	mu          sync.Mutex
}

func (s *stubApplyWorkflow) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return nil, nil, nil
}

func (s *stubApplyWorkflow) Apply(_ context.Context, cmd workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	s.mu.Lock()
	s.applyCalled++
	s.mu.Unlock()
	return s.applyResult, s.applyErr
}

func (s *stubApplyWorkflow) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}

func (s *stubApplyWorkflow) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}

func (s *stubApplyWorkflow) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

func (s *stubApplyWorkflow) getApplyCalled() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyCalled
}

// makeApplyInputs creates standard applyPhaseInputs for testing.
func makeApplyInputs(wf workflow.WorkflowInterface) applyPhaseInputs {
	return applyPhaseInputs{
		JobID:       "test-apply-001",
		Concurrency: concurrencyConfig{MaxWorkers: 1, WorkerTimeout: 0},
		NFOEnabled:  true,
		WF:          wf,
		Results:     make(map[string]*MovieResult),
		Excluded:    make(map[string]bool),
		Destination: "/output",
		Broadcaster: &stubBroadcaster{},
		Updater:     newStubUpdater(),
		Lifecycle:   &stubLifecycle{},
		persister:   nil,
	}
}

func TestApplyPhase_Run_Success(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{
			Movie: &models.Movie{ID: "IPX-777"},
		},
	}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777", Title: "Test Movie"},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.organized, "Lifecycle.MarkOrganized should be called on success")
	assert.False(t, lc.completed, "MarkCompleted should NOT be called when organized")
	assert.Equal(t, 1, wf.getApplyCalled(), "Workflow.Apply should be called once")
}

func TestApplyPhase_Run_OnPhaseCompleteInvokedWithCounts(t *testing.T) {
	// OnPhaseComplete is the API-layer hook that broadcasts the
	// {status:"organization_completed", progress:100} WebSocket progress
	// message at end of the apply phase. Without it, no WS message is sent,
	// the frontend falls back to HTTP polling, and (because organize sets
	// status='organized') polls forever — exactly the user-reported symptom.
	// Asserts the hook is invoked exactly once with the correct
	// organized/failed counts derived from trackApplyResults.
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	var gotOrganized, gotFailed int
	var callCount int
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		OnPhaseComplete: func(organized, failed int) {
			gotOrganized = organized
			gotFailed = failed
			callCount++
		},
	})

	assert.Equal(t, 1, callCount, "OnPhaseComplete must be invoked exactly once")
	assert.Equal(t, 1, gotOrganized, "organized count must reflect the single successful apply")
	assert.Equal(t, 0, gotFailed, "failed count must be zero for an all-success apply")
}

func TestApplyPhase_Run_FailedApply(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyErr: fmt.Errorf("disk full"),
	}
	inputs := makeApplyInputs(wf)
	inputMovie := &models.Movie{ID: "IPX-777", Title: "Test Movie"}
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         inputMovie,
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.False(t, lc.organized, "MarkOrganized should NOT be called on failure")
	assert.True(t, lc.completed, "MarkCompleted should be called on failure")

	updater := inputs.Updater.(*stubUpdater)
	r := updater.getResult("/source/IPX-777.mp4")
	require.NotNil(t, r, "Updater should have a failed result")
	assert.Equal(t, models.JobStatusFailed, r.Status)
	// Preserve the prior scrape-phase Movie on the apply-failure path. Main's
	// process_organize.go returned early on organizeErr WITHOUT mutating the
	// per-file FileResult, so the Movie survived for /review/[jobId] rendering
	// of failed-apply rows. UpdateFileResult replaces the whole struct
	// (preserving only ResultID + Revision), so without Movie: movie set here
	// the API response (convert.go:movieResultToResponse) loses its movie
	// payload. Same dropped-on-failure-path pattern fixed for FileMatchInfo
	// in commit 6249de64.
	require.NotNil(t, r.Movie, "failed-apply MovieResult must preserve the prior scrape-phase Movie pointer")
	assert.Equal(t, "IPX-777", r.Movie.ID, "Movie must be the same scrape-phase movie, not nil / replaced")
}

func TestApplyPhase_Run_SkipsExcluded(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}
	inputs.Excluded["/source/IPX-777.mp4"] = true

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	assert.Equal(t, 0, wf.getApplyCalled(), "Workflow.Apply should NOT be called for excluded files")
}

func TestApplyPhase_Run_SkipsNilMovie(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         nil, // nil movie — should be skipped
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	assert.Equal(t, 0, wf.getApplyCalled(), "Workflow.Apply should NOT be called for nil movie")
}

func TestApplyPhase_Run_SkipsFailedResult(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusFailed, // failed — should be skipped
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	assert.Equal(t, 0, wf.getApplyCalled(), "Workflow.Apply should NOT be called for failed results")
}

func TestApplyPhase_Run_EmptyResults(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	// No results at all

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.completed, "MarkCompleted should be called when no results to organize")
	assert.False(t, lc.organized, "MarkOrganized should NOT be called when no results")
}

func TestApplyPhase_Run_SkipOrganize(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{Skip: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.False(t, lc.organized, "MarkOrganized should NOT be called when OrganizeOptions.Skip=true")
	assert.True(t, lc.completed, "MarkCompleted should be called when OrganizeOptions.Skip=true")
}

// Per DEEP-6: TestApplyPhase_Run_IgnoresConfigWF removed — WF override field
// deleted from ApplyPhaseConfig. WF is now resolved at the factory/job level via
// SetWorkflow, not passed through phase config overrides.

func TestApplyPhase_Run_PersistFnCalled(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}
	persisted := false
	inputs.persister = persistFunc(func() { persisted = true })

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	assert.True(t, persisted, "PersistFn should be called after Run completes")
}

func TestApplyPhase_Run_NFODisabled(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.NFOEnabled = false // NFO disabled
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.organized, "Should still succeed even with NFO disabled")
}

func TestApplyPhase_Run_MultipleFiles(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency.MaxWorkers = 2
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}
	inputs.Results["/source/IPX-778.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-778.mp4", MovieID: "IPX-778"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-778"},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.organized, "MarkOrganized should be called when all files succeed")
	assert.Equal(t, 2, wf.getApplyCalled(), "Workflow.Apply should be called for each file")
}
