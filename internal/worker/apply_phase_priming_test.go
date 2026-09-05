package worker

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// primingStubWorkflow extends stubApplyWorkflow with the optional
// workflow.DuplicatePrimingPlanner seam, recording priming attempts in call
// order and any Apply dispatched before every priming completed.
type primingStubWorkflow struct {
	stubApplyWorkflow
	planErrOn map[string]bool
	planPanic map[string]bool

	mu             sync.Mutex
	primeCalls     []string
	applyCmds      []workflow.ApplyCmd
	earlyApply     []string
	applyDeadlines map[string]time.Time
	total          int
}

func (s *primingStubWorkflow) PlanDuplicatePriming(_ context.Context, cmd workflow.ApplyCmd) (organizer.DuplicatePriming, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src := cmd.Match.Path
	s.primeCalls = append(s.primeCalls, src)
	if s.planPanic[src] {
		panic("priming plan boom for " + src)
	}
	if s.planErrOn[src] {
		return organizer.DuplicatePriming{}, fmt.Errorf("plan boom for %s", src)
	}
	return organizer.DuplicatePriming{
		SourcePath: src,
		TargetPath: filepath.Join(cmd.DestPath, strings.ToLower(cmd.Match.MovieID)+".mkv"),
		WillMove:   true,
	}, nil
}

func (s *primingStubWorkflow) Apply(ctx context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	s.mu.Lock()
	if len(s.primeCalls) < s.total {
		s.earlyApply = append(s.earlyApply, cmd.Match.Path)
	}
	s.applyCmds = append(s.applyCmds, cmd)
	if deadline, ok := ctx.Deadline(); ok {
		if s.applyDeadlines == nil {
			s.applyDeadlines = make(map[string]time.Time)
		}
		s.applyDeadlines[cmd.Match.Path] = deadline
	}
	s.mu.Unlock()
	return s.stubApplyWorkflow.Apply(ctx, cmd)
}

func (s *primingStubWorkflow) applyDeadlineFor(path string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deadline, ok := s.applyDeadlines[path]
	return deadline, ok
}

func (s *primingStubWorkflow) snapshot() (primeCalls []string, applyCmds []workflow.ApplyCmd, early []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.primeCalls...), append([]workflow.ApplyCmd(nil), s.applyCmds...), append([]string(nil), s.earlyApply...)
}

// TestApplyPhase_Run_PrimesDuplicateOwnersInSortedOrder pins #240 finding A:
// the apply phase plans every sorted item exactly once BEFORE any worker
// starts and primes the batch's shared tracker in that sorted order.
func TestApplyPhase_Run_PrimesDuplicateOwnersInSortedOrder(t *testing.T) {
	wf := &primingStubWorkflow{total: 3}
	wf.applyResult = &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 4, WorkerTimeout: 0}
	for _, p := range []string{"/source/m-3.mp4", "/source/a-1.mp4", "/source/z-2.mp4"} {
		inputs.Results[p] = &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: p, MovieID: "M-100"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "M-100", Title: "Shared Destination"},
		}
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		Destination:     "/output",
	})

	primeCalls, applyCmds, early := wf.snapshot()
	assert.Equal(t, []string{"/source/a-1.mp4", "/source/m-3.mp4", "/source/z-2.mp4"}, primeCalls,
		"every item is planned exactly once per run, before fan-out, in sorted order")
	assert.Empty(t, early, "no worker Apply may start before every claim is primed")
	require.Len(t, applyCmds, 3)
	for _, cmd := range applyCmds {
		require.NotNil(t, cmd.Organize.DuplicateTracker)
		assert.Same(t, applyCmds[0].Organize.DuplicateTracker, cmd.Organize.DuplicateTracker,
			"primed tracker is shared by every file")
	}
}

// TestApplyPhase_Run_PrimingSkipsUnexecutableAndUnplannableItems pins the two
// priming skip legs: a PreApply-declined item registers no claim (and never
// applies), while a planning failure only forfeits priming — the item still
// applies and fails through its own identical plan error.
func TestApplyPhase_Run_PrimingSkipsUnexecutableAndUnplannableItems(t *testing.T) {
	wf := &primingStubWorkflow{total: 2, planErrOn: map[string]bool{"/source/bad.mp4": true}}
	wf.applyResult = &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 2, WorkerTimeout: 0}
	for _, p := range []string{"/source/skip.mp4", "/source/bad.mp4", "/source/good.mp4"} {
		inputs.Results[p] = &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: p, MovieID: "M-100"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "M-100"},
		}
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		Destination:     "/output",
		PreApplyFunc: func(_ context.Context, afc *ApplyFileContext) error {
			if afc.FilePath == "/source/skip.mp4" {
				return fmt.Errorf("skip this file")
			}
			return nil
		},
	})

	primeCalls, applyCmds, _ := wf.snapshot()
	assert.Equal(t, []string{"/source/bad.mp4", "/source/good.mp4"}, primeCalls,
		"sorted priming attempts skip hook-declined items and continue past plan failures")
	appliedPaths := make([]string, 0, len(applyCmds))
	for _, cmd := range applyCmds {
		appliedPaths = append(appliedPaths, cmd.Match.Path)
	}
	assert.ElementsMatch(t, []string{"/source/bad.mp4", "/source/good.mp4"}, appliedPaths,
		"a plan failure forfeits priming but never drops the item from apply")
}

// TestApplyPhase_Run_DuplicatePreflightProbePolicy pins #240 finding B at the
// batch boundary: dry runs construct the non-probing tracker (zero probes,
// zero probe artifacts), while live runs probe exactly once per root, exactly
// as before.
func TestApplyPhase_Run_DuplicatePreflightProbePolicy(t *testing.T) {
	snapshotTree := func(t *testing.T, root string) map[string]bool {
		t.Helper()
		entries := map[string]bool{}
		require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			entries[path] = d.IsDir()
			return nil
		}))
		return entries
	}

	runBatch := func(t *testing.T, dryRun bool) (dest string, caseCalls, normCalls int) {
		t.Helper()
		fsutil.ResetCaseSensitivityCache()
		fsutil.ResetNormalizationCache()
		cc, nc := 0, 0
		prevCase, prevNorm := fsutil.CaseSensitiveProbe, fsutil.NormalizationProbe
		fsutil.CaseSensitiveProbe = func(string) (bool, error) { cc++; return false, nil }
		fsutil.NormalizationProbe = func(string) (bool, error) { nc++; return true, nil }
		defer func() {
			fsutil.CaseSensitiveProbe = prevCase
			fsutil.NormalizationProbe = prevNorm
			fsutil.ResetCaseSensitivityCache()
			fsutil.ResetNormalizationCache()
		}()

		dest = t.TempDir()
		before := snapshotTree(t, dest)

		wf := &primingStubWorkflow{total: 2}
		wf.applyResult = &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}
		inputs := makeApplyInputs(wf)
		for _, p := range []string{"/source/a-1.mp4", "/source/z-2.mp4"} {
			inputs.Results[p] = &resultstore.MovieResult{
				FileMatchInfo: models.FileMatchInfo{Path: p, MovieID: "M-100"},
				Status:        models.JobStatusCompleted,
				Movie:         &models.Movie{ID: "M-100"},
			}
		}
		NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
			DryRun:          dryRun,
			OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
			Destination:     dest,
		})

		assert.Equal(t, before, snapshotTree(t, dest),
			"no probe artifacts may survive the run (the duplicate preflight adds no disk writes)")
		return dest, cc, nc
	}

	t.Run("dry run performs zero probe writes", func(t *testing.T) {
		_, cc, nc := runBatch(t, true)
		assert.Equal(t, 0, cc, "dry runs never run the writing case probe")
		assert.Equal(t, 0, nc, "dry runs never run the writing normalization probe")
	})

	t.Run("live run probing is unchanged", func(t *testing.T) {
		_, cc, nc := runBatch(t, false)
		assert.Equal(t, 1, cc, "live runs keep one case probe per root per process")
		assert.Equal(t, 1, nc, "live runs keep one normalization probe per root per process")
	})
}

// TestApplyPhase_Run_PrimingHookRespectsPerFileTimeout pins codex r2 P2: the
// priming-time PreApply invocation runs under the SAME per-file execution
// boundary as the worker path — a hook blocking on ctx deadline burns only
// its own WorkerTimeout budget; the rest of the batch prepares and applies
// normally instead of stalling behind the raw batch context.
func TestApplyPhase_Run_PrimingHookRespectsPerFileTimeout(t *testing.T) {
	const blocked, good = "/source/a-blocked.mp4", "/source/b-good.mp4"
	const perFile = 150 * time.Millisecond

	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 2, WorkerTimeout: perFile}
	for _, p := range []string{blocked, good} {
		inputs.Results[p] = &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: p, MovieID: "M-100"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "M-100"},
		}
	}

	start := time.Now()
	var hookDeadline time.Time
	var hookHasDeadline bool
	var organized, failed int
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		Destination:     "/output",
		PreApplyFunc: func(ctx context.Context, afc *ApplyFileContext) error {
			if afc.FilePath != blocked {
				return nil
			}
			hookDeadline, hookHasDeadline = ctx.Deadline()
			<-ctx.Done() // blocks ONLY until the per-file budget expires
			return ctx.Err()
		},
		OnPhaseComplete: func(o, f int) { organized, failed = o, f },
	})
	elapsed := time.Since(start)

	require.True(t, hookHasDeadline, "the priming hook receives a per-file WorkerTimeout context, not the raw batch context")
	assert.WithinDuration(t, start.Add(perFile), hookDeadline, 100*time.Millisecond,
		"the hook context deadline is the per-file budget, measured from file preparation")
	assert.Less(t, elapsed, 5*time.Second,
		"the whole batch is bounded by the blocked hook's per-file budget — the batch context never leaks in")
	require.Equal(t, 1, wf.getApplyCalled(), "other files apply unaffected")
	assert.Equal(t, good, wf.getLastCmd().Match.Path)
	assert.Equal(t, 1, organized)
	assert.Equal(t, 0, failed, "a hook declining execution (here: past its budget) skips the file, mirroring the hook-error contract")
	assert.True(t, inputs.Lifecycle.(*stubLifecycle).organized)
}

// TestApplyPhase_Run_PrimingHookPanicRecordsPerFileFailure pins codex r2 P2:
// a panicking priming-time PreApply hook is recovered under withFileRecovery
// semantics — the failure is written back and broadcast, the batch
// continues, and the file counts as exactly one failed outcome instead of
// aborting every remaining file.
func TestApplyPhase_Run_PrimingHookPanicRecordsPerFileFailure(t *testing.T) {
	const panics, good = "/source/a-panics.mp4", "/source/b-good.mp4"

	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 2, WorkerTimeout: 0}
	for _, p := range []string{panics, good} {
		inputs.Results[p] = &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: p, MovieID: "M-100"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "M-100"},
		}
	}

	hookCalls := 0
	var organized, failed int
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		Destination:     "/output",
		PreApplyFunc: func(_ context.Context, afc *ApplyFileContext) error {
			hookCalls++
			if afc.FilePath == panics {
				panic("priming hook boom")
			}
			return nil
		},
		OnPhaseComplete: func(o, f int) { organized, failed = o, f },
	})

	assert.Equal(t, 2, hookCalls, "every hook still runs exactly once per file — the panic skips only its own file")
	require.Equal(t, 1, wf.getApplyCalled(), "the batch continues and the healthy file applies")
	assert.Equal(t, good, wf.getLastCmd().Match.Path)
	assert.Equal(t, 1, organized, "the healthy file's success is unaffected by the panicked sibling")
	assert.Equal(t, 1, failed, "the panicking hook records exactly one per-file failure")

	row := inputs.Updater.(*stubUpdater).getResult(panics)
	require.NotNil(t, row, "recovery writes the failure back, mirroring the worker panic path")
	assert.Equal(t, models.JobStatusFailed, row.Status)
	assert.Contains(t, row.Error, "priming hook boom")
	assert.False(t, inputs.Lifecycle.(*stubLifecycle).organized, "a recorded failure keeps the job out of Organized")
	assert.True(t, inputs.Lifecycle.(*stubLifecycle).completed)
}

// TestApplyPhase_Run_PrimingPlanPanicRecordsPerFileFailure pins codex P2 (PR
// #241, F3): a panic INSIDE PlanDuplicatePriming escapes on the phase's main
// goroutine, where no worker-side withFileRecovery exists — without a
// per-file boundary it would sink the whole job. The planning call runs under
// the same recovery semantics as preparation/execution: the panicking file
// records exactly one failure with the panic message and the batch applies
// every other file.
func TestApplyPhase_Run_PrimingPlanPanicRecordsPerFileFailure(t *testing.T) {
	const panics, goodB, goodC = "/source/a-panics.mp4", "/source/b-good.mp4", "/source/c-good.mp4"

	wf := &primingStubWorkflow{total: 3, planPanic: map[string]bool{panics: true}}
	wf.applyResult = &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 2, WorkerTimeout: 0}
	for _, p := range []string{panics, goodB, goodC} {
		inputs.Results[p] = &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: p, MovieID: "M-100"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "M-100"},
		}
	}

	var organized, failed int
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		Destination:     "/output",
		OnPhaseComplete: func(o, f int) { organized, failed = o, f },
	})

	primeCalls, applyCmds, _ := wf.snapshot()
	assert.Equal(t, []string{panics, goodB, goodC}, primeCalls,
		"planning keeps going past the panic — every item is attempted exactly once")
	appliedPaths := make([]string, 0, len(applyCmds))
	for _, cmd := range applyCmds {
		appliedPaths = append(appliedPaths, cmd.Match.Path)
	}
	assert.ElementsMatch(t, []string{goodB, goodC}, appliedPaths,
		"the panicking file is never applied; the batch applies every other file")
	assert.Equal(t, 2, organized)
	assert.Equal(t, 1, failed, "the planning panic records exactly one per-file failure")

	row := inputs.Updater.(*stubUpdater).getResult(panics)
	require.NotNil(t, row, "recovery writes the planning panic back, mirroring the worker panic path")
	assert.Equal(t, models.JobStatusFailed, row.Status)
	assert.Contains(t, row.Error, "priming plan boom")
	assert.NotContains(t, row.Error, "hook", "the panic is attributed to planning, not to the hook")

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.False(t, lc.failed, "the phase main body survives: no job-sinking panic")
	assert.False(t, lc.organized, "a recorded failure keeps the job out of Organized")
	assert.True(t, lc.completed)
}

// TestApplyPhase_Run_SingleDeadlineAcrossPrepareAndApply pins codex P2 (PR
// #241, F4): one absolute per-file deadline spans the PreApply hook during
// preparation AND the worker's apply execution — the worker derives its task
// context from the SAME timestamp instead of re-granting a fresh full
// WorkerTimeout (~2x the configured limit).
func TestApplyPhase_Run_SingleDeadlineAcrossPrepareAndApply(t *testing.T) {
	const perFile = 30 * time.Second
	files := []string{"/source/a-one.mp4", "/source/b-two.mp4"}

	wf := &primingStubWorkflow{total: len(files)}
	wf.applyResult = &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: len(files), WorkerTimeout: perFile}
	for _, p := range files {
		inputs.Results[p] = &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: p, MovieID: "M-100"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "M-100"},
		}
	}

	hookDeadlines := make(map[string]time.Time, len(files))
	runStart := time.Now()
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		Destination:     "/output",
		PreApplyFunc: func(ctx context.Context, afc *ApplyFileContext) error {
			deadline, ok := ctx.Deadline()
			require.True(t, ok, "the priming hook receives the per-file deadline context")
			hookDeadlines[afc.FilePath] = deadline
			return nil
		},
	})

	require.Len(t, hookDeadlines, len(files))
	for _, p := range files {
		hookDeadline := hookDeadlines[p]
		applyDeadline, ok := wf.applyDeadlineFor(p)
		require.True(t, ok, "the worker's apply context carries a deadline")
		assert.Equal(t, hookDeadline, applyDeadline,
			"ONE absolute deadline spans hook + apply — no fresh double-grant for %s", p)
		assert.WithinDuration(t, runStart.Add(perFile), hookDeadline, time.Second,
			"the single deadline is measured once, from the file's preparation")
		assert.Less(t, time.Until(applyDeadline), perFile,
			"by apply time the hook has already spent part of the shared budget")
	}
}
