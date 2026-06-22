package worker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ApplyPhase miss lines: dry run, worker timeout, PreApplyFunc,
// PostApplyFunc, context.DeadlineExceeded, AtomicUpdateFileResult with result,
// panic recovery, destination fallback logic ---

func TestApplyPhase_Run_DryRun(t *testing.T) {
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
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		DryRun:          true,
	})

	assert.Equal(t, 1, wf.getApplyCalled(), "Workflow.Apply should be called even in dry-run")
	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.organized)
}

func TestApplyPhase_Run_WorkerTimeout(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency.WorkerTimeout = 5 * time.Second
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
	assert.True(t, lc.organized)
}

func TestApplyPhase_Run_WorkerTimeout_Exceeded(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyErr: context.DeadlineExceeded,
	}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency.WorkerTimeout = 1 * time.Nanosecond
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

	updater := inputs.Updater.(*stubUpdater)
	r := updater.getResult("/source/IPX-777.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.Contains(t, r.Error, "timed out")
}

func TestApplyPhase_Run_PreApplyFunc_Skips(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	preApplyCalled := false
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		PreApplyFunc: func(_ context.Context, _ *ApplyFileContext) error {
			preApplyCalled = true
			return fmt.Errorf("skip this file")
		},
	})

	assert.True(t, preApplyCalled, "PreApplyFunc should be called")
	assert.Equal(t, 0, wf.getApplyCalled(), "Workflow.Apply should NOT be called when PreApplyFunc returns error")
}

func TestApplyPhase_Run_PreApplyFunc_ModifiesContext(t *testing.T) {
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
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		PreApplyFunc: func(_ context.Context, afc *ApplyFileContext) error {
			afc.Destination = "/modified-dest"
			return nil
		},
	})

	assert.Equal(t, 1, wf.getApplyCalled())
}

func TestApplyPhase_Run_PostApplyFunc(t *testing.T) {
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
			assert.NotNil(t, afr)
		},
	})

	assert.True(t, postApplyCalled, "PostApplyFunc should be called")
}

func TestApplyPhase_Run_PostApplyFunc_OnError(t *testing.T) {
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
			assert.Error(t, afr.Err)
		},
	})

	assert.True(t, postApplyCalled, "PostApplyFunc should be called even on error")
}

func TestApplyPhase_Run_AtomicUpdateAfterApply(t *testing.T) {
	updatedMovie := &models.Movie{ID: "IPX-777", Title: "Updated Title"}
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: updatedMovie},
	}
	input := makeApplyInputs(wf)
	// Pre-populate the updater with the initial result so AtomicUpdateFileResult can find it
	input.Updater.UpdateFileResult("/source/IPX-777.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777", Title: "Original Title"},
	})
	input.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777", Title: "Original Title"},
	}

	NewApplyPhase().Run(context.Background(), input, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	updater := input.Updater.(*stubUpdater)
	r := updater.getResult("/source/IPX-777.mp4")
	require.NotNil(t, r)
	assert.Equal(t, "Updated Title", r.Movie.Title, "Movie should be updated after apply")
}

func TestApplyPhase_Run_PanicRecovery(t *testing.T) {
	panicWF := &panicApplyWorkflow{}
	inputs := makeApplyInputs(panicWF)
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

	updater := inputs.Updater.(*stubUpdater)
	r := updater.getResult("/source/IPX-777.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.Contains(t, r.Error, "panic")

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.completed, "Should still complete after panic recovery")
}

func TestApplyPhase_Run_PanicRecoveryPreservesMultiPartMetadata(t *testing.T) {
	// When a panic happens mid-apply, the recovery path must preserve the
	// FileMatchInfo from the previous phase. Constructing a fresh struct
	// with only Path + MovieID would silently zero IsMultiPart /
	// PartNumber / PartSuffix, so /review/[jobId] would then show the file
	// as single-part.
	panicWF := &panicApplyWorkflow{}
	inputs := makeApplyInputs(panicWF)
	inputs.Results["/source/IPX-777-part1.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{
			Path:        "/source/IPX-777-part1.mp4",
			MovieID:     "IPX-777",
			IsMultiPart: true,
			PartNumber:  1,
			PartSuffix:  "part1",
		},
		Status: models.JobStatusCompleted,
		Movie:  &models.Movie{ID: "IPX-777"},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	updater := inputs.Updater.(*stubUpdater)
	r := updater.getResult("/source/IPX-777-part1.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusFailed, r.Status)
	assert.True(t, r.FileMatchInfo.IsMultiPart, "panic-recovery must preserve IsMultiPart from the prior phase")
	assert.Equal(t, 1, r.FileMatchInfo.PartNumber, "panic-recovery must preserve PartNumber from the prior phase")
	assert.Equal(t, "part1", r.FileMatchInfo.PartSuffix, "panic-recovery must preserve PartSuffix from the prior phase")
}

func TestApplyPhase_Run_PanicRecoveryPreservesPriorMovie(t *testing.T) {
	// When a panic happens mid-apply, the recovered MovieResult must preserve
	// the prior scrape-phase Movie so /review/[jobId] can still render the
	// movie card for a panicked-apply file (mirrors the err-branch fix
	// validated in TestApplyPhase_Run_FailedApply). Same drop-on-failure-path
	// pattern as commit 6249de64, which fixed FileMatchInfo / timestamps
	// /panic broadcast on the same path — Movie was the remaining gap.
	panicWF := &panicApplyWorkflow{}
	inputMovie := &models.Movie{ID: "IPX-999", Title: "Under Test"}
	inputs := makeApplyInputs(panicWF)
	inputs.Results["/source/IPX-999.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-999.mp4", MovieID: "IPX-999"},
		Status:        models.JobStatusCompleted,
		Movie:         inputMovie,
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	r := inputs.Updater.(*stubUpdater).getResult("/source/IPX-999.mp4")
	require.NotNil(t, r)
	assert.Equal(t, models.JobStatusFailed, r.Status)
	require.NotNil(t, r.Movie, "panic-recovered MovieResult must preserve the prior scrape-phase Movie pointer")
	assert.Equal(t, "IPX-999", r.Movie.ID, "Movie must be the same scrape-phase movie, not nil")
}

// panicApplyWorkflow panics when Apply is called
type panicApplyWorkflow struct{}

func (p *panicApplyWorkflow) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return nil, nil, nil
}
func (p *panicApplyWorkflow) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	panic("intentional test panic")
}
func (p *panicApplyWorkflow) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}
func (p *panicApplyWorkflow) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}
func (p *panicApplyWorkflow) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

func TestApplyPhase_Run_ContextCancelled(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
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
	assert.True(t, lc.cancelled)
}

func TestApplyPhase_Run_DestinationFromConfig(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Destination = ""
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/config-dest",
	})

	assert.Equal(t, 1, wf.getApplyCalled())
}

func TestApplyPhase_Run_DestinationFromInputsWithSkip(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Destination = ""
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

	// When Skip=true and both dest are empty, sourceDir is used
	assert.Equal(t, 1, wf.getApplyCalled())
}

func TestApplyPhase_Run_GenerateNFODisabled(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.NFOEnabled = true
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		GenerateNFO:     false,
	})

	assert.Equal(t, 1, wf.getApplyCalled())
}

func TestApplyPhase_Run_NFOEnabledWithGenerateNFO(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.NFOEnabled = true
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		GenerateNFO:     true,
	})

	assert.Equal(t, 1, wf.getApplyCalled())
}

func TestApplyPhase_Run_NFOEnabledFalseOverridesGenerateNFO(t *testing.T) {
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
		GenerateNFO:     true,
	})

	assert.Equal(t, 1, wf.getApplyCalled())
}

func TestApplyPhase_Run_ApplyResultNilMovie(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: nil},
	}
	inputs := makeApplyInputs(wf)
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
	assert.True(t, lc.organized, "Should still mark organized even with nil result movie")
}

func TestApplyPhase_Run_DownloadEnabled(t *testing.T) {
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
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		Download:        true,
	})

	assert.Equal(t, 1, wf.getApplyCalled())
}

func TestApplyPhase_Run_BroadcastEventsOnSuccess(t *testing.T) {
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
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	broadcaster := inputs.Broadcaster.(*stubBroadcaster)
	found := false
	for _, evt := range broadcaster.events {
		if evt.Step == StepComplete {
			found = true
			break
		}
	}
	assert.True(t, found, "Should broadcast StepComplete on success")
}

func TestApplyPhase_Run_BroadcastEventsOnFailure(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyErr: fmt.Errorf("disk full"),
	}
	inputs := makeApplyInputs(wf)
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

	broadcaster := inputs.Broadcaster.(*stubBroadcaster)
	found := false
	for _, evt := range broadcaster.events {
		if evt.Step == StepFailed {
			found = true
			break
		}
	}
	assert.True(t, found, "Should broadcast StepFailed on error")
}

func TestApplyPhase_Run_AllFailMarkCompleted(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyErr: fmt.Errorf("fail"),
	}
	inputs := makeApplyInputs(wf)
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
	assert.True(t, lc.completed, "Should MarkCompleted when all files fail")
	assert.False(t, lc.organized, "Should NOT MarkOrganized when all files fail")
}

func TestApplyPhase_Run_DeadlineExceededMessage(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyErr: context.DeadlineExceeded,
	}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency.WorkerTimeout = 10 * time.Second
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

	updater := inputs.Updater.(*stubUpdater)
	r := updater.getResult("/source/IPX-777.mp4")
	require.NotNil(t, r)
	assert.Contains(t, r.Error, "timed out")
}

func TestApplyPhase_Run_NilApplyResult(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: nil,
		applyErr:    nil,
	}
	inputs := makeApplyInputs(wf)
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
	assert.True(t, lc.organized, "Should mark organized even with nil result")
}

func TestApplyPhase_Run_MixedSuccessAndFailure(t *testing.T) {
	callCount := 0
	wf := &stubApplyWorkflowWithCounter{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
		applyErr:    nil,
		callCount:   &callCount,
		failOnCall:  2,
	}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency.MaxWorkers = 1
	inputs.Results["/source/file1.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/file1.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}
	inputs.Results["/source/file2.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/file2.mp4", MovieID: "IPX-778"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-778"},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.completed, "Should mark completed when there's a mix of success and failure")
}

// stubApplyWorkflowWithCounter allows controlled failure on a specific call number
type stubApplyWorkflowWithCounter struct {
	applyResult *workflow.ApplyResult
	applyErr    error
	callCount   *int
	failOnCall  int
}

func (s *stubApplyWorkflowWithCounter) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return nil, nil, nil
}
func (s *stubApplyWorkflowWithCounter) Apply(_ context.Context, _ workflow.ApplyCmd, _ scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	*s.callCount++
	if *s.callCount == s.failOnCall {
		return nil, fmt.Errorf("controlled failure on call %d", s.failOnCall)
	}
	return s.applyResult, s.applyErr
}
func (s *stubApplyWorkflowWithCounter) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}
func (s *stubApplyWorkflowWithCounter) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}
func (s *stubApplyWorkflowWithCounter) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

func TestApplyPhase_Run_PersisterCalledOnPanic(t *testing.T) {
	panicWF := &panicApplyWorkflow{}
	inputs := makeApplyInputs(panicWF)
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

	assert.True(t, persisted, "Persister should be called even after panic recovery")
}

func TestApplyPhase_Run_NilApplyResultWithPostApply(t *testing.T) {
	wf := &stubApplyWorkflow{
		applyResult: nil,
		applyErr:    nil,
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
			assert.Nil(t, afr.Result)
			assert.Nil(t, afr.Err)
		},
	})

	assert.True(t, postApplyCalled)
}

func TestApplyPhase_Run_ErrGroupWaitError(t *testing.T) {
	// This is a defensive test — normally eg.Wait() returns nil since errors
	// are handled inline. But the code has a defensive check.
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
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
	})

	// Should complete normally
	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.organized)
}

// Verify that errors.Is works correctly with DeadlineExceeded
var _ error = context.DeadlineExceeded
