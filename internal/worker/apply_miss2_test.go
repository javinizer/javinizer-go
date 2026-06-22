package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
)

// --- ApplyPhase Run: PostApplyFunc with error result ---

func TestMiss2_ApplyPhase_Run_PostApplyFuncWithError(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyErr: fmt.Errorf("apply failed"),
	}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	postApplyCalled := false
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		PostApplyFunc: func(_ context.Context, _ *ApplyFileContext, afr *ApplyFileResult) {
			postApplyCalled = true
			assert.NotNil(t, afr.Err)
		},
	})

	assert.True(t, postApplyCalled, "PostApplyFunc should be called even on error")
}

// --- ApplyPhase Run: PostApplyFunc with success result ---

func TestMiss2_ApplyPhase_Run_PostApplyFuncWithSuccess(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	postApplyCalled := false
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		PostApplyFunc: func(_ context.Context, _ *ApplyFileContext, afr *ApplyFileResult) {
			postApplyCalled = true
			assert.Nil(t, afr.Err)
			assert.NotNil(t, afr.Result)
		},
	})

	assert.True(t, postApplyCalled)
}

// --- ApplyPhase Run: OrganizeOptions.Skip → MarkCompleted instead of MarkOrganized ---

func TestMiss2_ApplyPhase_Run_OrganizeSkip(t *testing.T) {
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
		Destination:     "",
	})

	lc := inputs.Lifecycle.(*stubLifecycle)
	// When Skip=true, should call MarkCompleted (not MarkOrganized)
	assert.True(t, lc.completed, "MarkCompleted should be called when organize is skipped")
	assert.False(t, lc.organized, "MarkOrganized should NOT be called when organize is skipped")
}

// --- ApplyPhase Run: with NFO disabled ---

func TestMiss2_ApplyPhase_Run_NFODisabled(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.NFOEnabled = false
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		GenerateNFO:     true, // Config says generate, but NFOEnabled=false
	})

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.organized)
}

// --- ApplyPhase Run: multiple files ---

func TestMiss2_ApplyPhase_Run_MultipleFiles(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 2}
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

	assert.Equal(t, 2, wf.getApplyCalled())
	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.organized)
}

// --- ApplyPhase Run: with persister ---

func TestMiss2_ApplyPhase_Run_WithPersister(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	persistCalled := false
	inputs.persister = &stubPersister{called: &persistCalled}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	assert.True(t, persistCalled, "Persister should be called on completion")
}

type stubPersister struct {
	called *bool
}

func (s *stubPersister) Persist() {
	*s.called = true
}

// --- ApplyPhase Run: context cancellation before processing ---

func TestMiss2_ApplyPhase_Run_ContextCancellation(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 1}
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	NewApplyPhase().Run(ctx, inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.cancelled, "Lifecycle should be marked as cancelled")
}

// --- ApplyPhase Run: destination fallback when cfg.Destination is empty and inputs.Destination is set ---

func TestMiss2_ApplyPhase_Run_DestinationFallback(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Destination = "/fallback-output"
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "", // empty → fallback to inputs.Destination
	})

	assert.Equal(t, 1, wf.getApplyCalled())
}

// --- ApplyPhase Run: panic recovery in goroutine ---

func TestMiss2_ApplyPhase_Run_PanicRecovery(t *testing.T) {
	wf := &panicApplyWorkflow2{}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	// Should not panic — the apply phase recovers from panics in goroutines
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.completed, "Should still complete after panic recovery")
}

type panicApplyWorkflow2 struct{}

func (p *panicApplyWorkflow2) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return nil, nil, nil
}

func (p *panicApplyWorkflow2) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	panic("test panic in apply 2")
}

func (p *panicApplyWorkflow2) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}

func (p *panicApplyWorkflow2) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}

func (p *panicApplyWorkflow2) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}
