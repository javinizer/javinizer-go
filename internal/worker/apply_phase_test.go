package worker

import (
	"context"
	"fmt"
	"strings"
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

func TestApplyPhase_Run_OnFileProgressAdvancesPerFile(t *testing.T) {
	// OnFileProgress is the API-layer hook that broadcasts an incremental
	// WebSocket ProgressMessage (0-100) after each file's apply completes.
	// Without it, the only WS progress the frontend receives during organize is
	// the terminal 100% from OnPhaseComplete, so the bar jumps 0→100 — exactly
	// the user-reported bug. Asserts the hook fires once per processed file,
	// every call carries the correct total, and the processed counts are a
	// gap-free permutation of 1..N (each invocation receives a unique atomic
	// increment in completion order — set membership, not delivery order, is the
	// correct invariant under MaxWorkers > 1). Files are processed concurrently
	// (MaxWorkers > 1), so the hook is invoked from multiple goroutines;
	// collection is mutex-guarded to catch data races under -race.
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 4, WorkerTimeout: 0}
	for i := 0; i < 5; i++ {
		path := fmt.Sprintf("/source/IPX-777-%d.mp4", i)
		inputs.Results[path] = &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: "IPX-777"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-777"},
		}
	}

	var mu sync.Mutex
	var calls []struct {
		processed int
		total     int
	}
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		OnFileProgress: func(processed, total int) {
			mu.Lock()
			calls = append(calls, struct {
				processed int
				total     int
			}{processed, total})
			mu.Unlock()
		},
	})

	require.Len(t, calls, 5, "OnFileProgress must fire once per file (5 files)")
	for _, c := range calls {
		assert.Equal(t, 5, c.total, "every call must carry the total file count")
		assert.GreaterOrEqual(t, c.processed, 1, "processed must be >= 1")
		assert.LessOrEqual(t, c.processed, 5, "processed must not exceed total")
	}
	// The processed values are a permutation of 1..5 (one per file, assigned
	// atomically in completion order) — verify the set is exactly {1,2,3,4,5}.
	// Set membership (not ordered sequence) is the correct check: under
	// MaxWorkers:4 completion order is nondeterministic, but each atomic
	// increment yields a unique gap-free value, so the set must be {1..5}.
	seen := make(map[int]bool, 5)
	for _, c := range calls {
		seen[c.processed] = true
	}
	assert.Equal(t, map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true}, seen,
		"processed counts must be exactly 1..5 with no duplicates or gaps")
}

func TestApplyPhase_Run_OnFileProgressNilIsNoOp(t *testing.T) {
	// Opt-out guard: OnFileProgress is optional (nil = no per-file progress).
	// Run must not panic when the hook is nil — the apply phase guards
	// `cfg.OnFileProgress != nil` before invoking it. Other tests in this file
	// already omit OnFileProgress and pass, but this pins the nil-safety
	// explicitly so a future change that drops the nil guard fails here.
	wf := &stubApplyWorkflow{
		applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}},
	}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}
	assert.NotPanics(t, func() {
		NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
			OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
			MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
			Destination:     "/output",
			// OnFileProgress intentionally nil
		})
	})
	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.organized, "run still succeeds with nil OnFileProgress")
}

func TestApplyPhase_Run_OnFileProgressNotCalledWhenNoEligibleItems(t *testing.T) {
	// total<=0 guard: when there are zero eligible items (all results are
	// non-completed or excluded), items is empty, boundedFanOut runs no work
	// closures, and OnFileProgress must never fire — the hook isn't invoked with
	// a bogus total=0. (The total<=0 case at the broadcast boundary is handled by
	// organizeProgressPercent; here we assert the call site never fires at all.)
	// Results present but not completed → filtered out of items.
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}}}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusFailed, // not completed → excluded from apply items
		Movie:         &models.Movie{ID: "IPX-777"},
	}
	var callCount int
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		OnFileProgress:  func(processed, total int) { callCount++ },
	})
	assert.Equal(t, 0, callCount, "OnFileProgress must not fire when there are no eligible items")
}

func TestApplyPhase_Run_OnFileProgressFiresOnFailedApply(t *testing.T) {
	// MAJOR-T3 guard: a file counts as processed whether its apply SUCCEEDED or
	// FAILED — the bar tracks throughput, not success rate. OnFileProgress is
	// invoked after applyFile returns regardless of outcome, so it must fire once
	// per eligible file even when every apply errors. A regression that moved the
	// call inside the success branch would stop advancing the bar on any failed
	// file and pass the rest of the suite — this test fails in that case.
	wf := &stubApplyWorkflow{applyErr: fmt.Errorf("disk full")}
	inputs := makeApplyInputs(wf)
	for i := 0; i < 3; i++ {
		path := fmt.Sprintf("/source/IPX-777-%d.mp4", i)
		inputs.Results[path] = &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: "IPX-777"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-777"},
		}
	}
	var (
		mu      sync.Mutex
		calls   []int
		maxSeen int
	)
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		OnFileProgress: func(processed, total int) {
			mu.Lock()
			calls = append(calls, processed)
			if processed > maxSeen {
				maxSeen = processed
			}
			mu.Unlock()
		},
	})
	require.Len(t, calls, 3, "OnFileProgress must fire once per file even when all applies fail")
	assert.Equal(t, 3, maxSeen, "final processed must reach the total despite all failures")
	seen := make(map[int]bool, 3)
	for _, p := range calls {
		seen[p] = true
	}
	assert.Equal(t, map[int]bool{1: true, 2: true, 3: true}, seen,
		"processed counts must be a gap-free permutation of 1..3 (throughput, not success)")
}

func TestApplyPhase_Run_OnFileProgressCancelledAtFanoutEndSkipsPhaseComplete(t *testing.T) {
	// Cancellation guard: when ctx is cancelled, Run's post-fanout ctx.Err()
	// branch calls MarkCancelled and returns BEFORE invoking OnPhaseComplete (no
	// terminal 100% broadcast on cancel). This tests the “ctx already cancelled
	// when fanout completes” path, NOT mid-flight cancellation: stubApplyWorkflow
	// ignores ctx and boundedFanOut does not short-circuit in-flight work, so all
	// items run to completion and OnFileProgress fires for each before the
	// cancellation check. (Mid-flight abort is a boundedFanOut property, not an
	// apply-phase one, and is out of scope here.) Asserts: all 3 files are
	// processed (fileCalls==3), processed never exceeds total, OnPhaseComplete is
	// skipped, and MarkCancelled is set. The frontend's poll loop detects the
	// 'cancelled' job status and finalizes failure, so no terminal WS progress is
	// wanted on cancel.
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}}}
	inputs := makeApplyInputs(wf)
	for i := 0; i < 3; i++ {
		path := fmt.Sprintf("/source/IPX-777-%d.mp4", i)
		inputs.Results[path] = &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: "IPX-777"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-777"},
		}
	}
	var (
		mu               sync.Mutex
		fileCalls        int
		phaseCompleteCnt int
		maxProcessed     int
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled: fanout still runs all items (boundedFanOut ignores ctx for dispatch)
	NewApplyPhase().Run(ctx, inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		OnFileProgress: func(processed, total int) {
			mu.Lock()
			fileCalls++
			if processed > maxProcessed {
				maxProcessed = processed
			}
			mu.Unlock()
		},
		OnPhaseComplete: func(organized, failed int) {
			mu.Lock()
			phaseCompleteCnt++
			mu.Unlock()
		},
	})
	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.cancelled, "cancelled ctx must MarkCancelled")
	assert.False(t, lc.organized, "must not mark organized on cancel")
	assert.False(t, lc.completed, "must not mark completed on cancel")
	assert.Equal(t, 3, fileCalls, "all items complete before the cancellation check, so OnFileProgress fires for each")
	assert.Equal(t, 0, phaseCompleteCnt, "OnPhaseComplete must NOT fire on cancellation (no terminal 100%)")
	assert.LessOrEqual(t, maxProcessed, 3, "processed must never exceed total even on cancel")
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

// TestApplyPhase_Run_CancellationMarksCancelled is a regression test for the
// apply-cancellation status. A mid-apply cancellation is not an organize
// failure: the file was scraped successfully, just not organized. The per-file
// FileResult must be JobStatusCancelled (mirroring scrape_phase.go), NOT
// JobStatusFailed. Old OrganizeTask returned the error to the pool without
// relabelling the row, so cancelled-but-scraped files stayed Completed.
func TestApplyPhase_Run_OnFileOrganizeStartFiresPerFile(t *testing.T) {
	// Part B guard: OnFileOrganizeStart fires at the TOP of applyFile, before
	// any work, so the frontend "Current Activity" card can show which file is
	// being organized (verbose organize progress: "Organizing <file>"). Asserts
	// the hook fires once per eligible file and carries the source file path.
	// The nil-guard is pinned separately in TestApplyPhase_Run_OnFileOrganizeStartNilIsNoOp.
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}}}
	inputs := makeApplyInputs(wf)
	for i := 0; i < 3; i++ {
		path := fmt.Sprintf("/source/IPX-777-%d.mp4", i)
		inputs.Results[path] = &MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: "IPX-777"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-777"},
		}
	}

	var mu sync.Mutex
	var starts []string
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
		Destination:     "/output",
		OnFileOrganizeStart: func(filePath string) {
			mu.Lock()
			starts = append(starts, filePath)
			mu.Unlock()
		},
	})

	require.Len(t, starts, 3, "OnFileOrganizeStart must fire once per eligible file")
	seen := make(map[string]bool, 3)
	for _, p := range starts {
		assert.True(t, strings.HasPrefix(p, "/source/IPX-777-"), "start must carry the source file path, got %q", p)
		assert.True(t, strings.HasSuffix(p, ".mp4"), "start must carry the file extension")
		seen[p] = true
	}
	assert.Len(t, seen, 3, "each file must produce a distinct start event")
}

func TestApplyPhase_Run_OnFileOrganizeStartNilIsNoOp(t *testing.T) {
	// nil-guard: OnFileOrganizeStart is optional; Run must not panic when the
	// hook is nil. applyFile guards `cfg.OnFileOrganizeStart != nil` before
	// invoking it. Pins the nil-safety so a future change that drops the guard
	// fails here.
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}}}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/IPX-777.mp4"] = &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/IPX-777.mp4", MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-777"},
	}
	assert.NotPanics(t, func() {
		NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
			OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
			MergeOptions:    workflow.MergeOptions{ForceOverwrite: true},
			Destination:     "/output",
			// OnFileOrganizeStart intentionally nil
		})
	})
	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.True(t, lc.organized, "run still succeeds with nil OnFileOrganizeStart")
}

func TestApplyPhase_Run_CancellationMarksCancelled(t *testing.T) {
	wf := &stubApplyWorkflow{applyErr: context.Canceled}
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

	updater := inputs.Updater.(*stubUpdater)
	r := updater.getResult("/source/IPX-777.mp4")
	require.NotNil(t, r, "Updater should have a result for the cancelled apply")
	assert.Equal(t, models.JobStatusCancelled, r.Status,
		"cancelled apply should mark the file Cancelled, not Failed")
	// The prior scrape-phase Movie is preserved on the cancel path too.
	require.NotNil(t, r.Movie)
	assert.Equal(t, "IPX-777", r.Movie.ID)
}
