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
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// primingStubWorkflow extends stubApplyWorkflow with the optional
// workflow.DuplicatePrimingPlanner seam, recording priming attempts in call
// order and any Apply dispatched before every priming completed. planBlock
// models a ctx-AWARE slow planner (unblocks only when its ctx is done) and
// planDelay a ctx-IGNORANT one (burns wall clock past any deadline); both
// record the priming ctx's deadline and their own elapsed spend so tests can
// pin the per-file priming budget boundary and its charging (codex P2, PR
// #241 F1).
type primingStubWorkflow struct {
	stubApplyWorkflow
	planErrOn map[string]bool
	planPanic map[string]bool
	planBlock map[string]bool
	planDelay map[string]time.Duration

	mu             sync.Mutex
	primeCalls     []string
	primeDeadline  map[string]time.Time
	primeStart     map[string]time.Time
	primeElapsed   map[string]time.Duration
	applyCmds      []workflow.ApplyCmd
	earlyApply     []string
	applyDeadlines map[string]time.Time
	applyStarts    map[string]time.Time
	applyEntryErrs map[string]error
	total          int
}

func (s *primingStubWorkflow) PlanDuplicatePriming(ctx context.Context, cmd workflow.ApplyCmd) (organizer.DuplicatePriming, error) {
	start := time.Now()
	deadline, _ := ctx.Deadline()
	s.mu.Lock()
	src := cmd.Match.Path
	s.primeCalls = append(s.primeCalls, src)
	if s.primeStart == nil {
		s.primeStart = make(map[string]time.Time)
		s.primeDeadline = make(map[string]time.Time)
		s.primeElapsed = make(map[string]time.Duration)
	}
	s.primeStart[src] = start
	s.primeDeadline[src] = deadline
	block := s.planBlock[src]
	delay := s.planDelay[src]
	s.mu.Unlock()

	if block {
		// A ctx-aware slow planner: unblocks exactly when ITS priming ctx is
		// done (the per-file budget firing, or the batch ctx canceling).
		<-ctx.Done()
		s.mu.Lock()
		s.primeElapsed[src] = time.Since(start)
		s.mu.Unlock()
		return organizer.DuplicatePriming{}, ctx.Err()
	}
	if delay > 0 {
		// A ctx-IGNORANT slow planner: burns wall clock past its deadline.
		time.Sleep(delay)
	}

	s.mu.Lock()
	s.primeElapsed[src] = time.Since(start)
	panics := s.planPanic[src]
	errs := s.planErrOn[src]
	s.mu.Unlock()
	if panics {
		panic("priming plan boom for " + src)
	}
	if errs {
		return organizer.DuplicatePriming{}, fmt.Errorf("plan boom for %s", src)
	}
	return organizer.DuplicatePriming{
		SourcePath: src,
		TargetPath: filepath.Join(cmd.DestPath, strings.ToLower(cmd.Match.MovieID)+".mkv"),
		WillMove:   true,
	}, nil
}

// primingObservation returns the priming-ctx state one plan call observed:
// its own start, the ctx deadline it was handed, and its measured spend.
func (s *primingStubWorkflow) primingObservation(path string) (start time.Time, deadline time.Time, elapsed time.Duration, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	start, ok = s.primeStart[path]
	return start, s.primeDeadline[path], s.primeElapsed[path], ok
}

func (s *primingStubWorkflow) Apply(ctx context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	s.mu.Lock()
	if len(s.primeCalls) < s.total {
		s.earlyApply = append(s.earlyApply, cmd.Match.Path)
	}
	s.applyCmds = append(s.applyCmds, cmd)
	if s.applyStarts == nil {
		s.applyStarts = make(map[string]time.Time)
	}
	s.applyStarts[cmd.Match.Path] = time.Now()
	if s.applyEntryErrs == nil {
		s.applyEntryErrs = make(map[string]error)
	}
	s.applyEntryErrs[cmd.Match.Path] = ctx.Err()
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

// applyObservationsFor returns the task-context state the Apply entry
// observed for one file: dispatch start, ctx error at entry (nil while the
// budget is live), and the ctx deadline.
func (s *primingStubWorkflow) applyObservationsFor(path string) (start time.Time, entryErr error, deadline time.Time, hasDeadline bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deadline, hasDeadline = s.applyDeadlines[path]
	return s.applyStarts[path], s.applyEntryErrs[path], deadline, hasDeadline
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

// TestApplyPhase_Run_PerFileBudgetChargedToOwnHookOnly pins codex P2 (PR
// #241, F5) — the fix for the F4 single-absolute-deadline overcharge: the
// per-file WorkerTimeout clock starts when the file's OWN worker task
// starts, debited ONLY by that file's own PreApply-hook spend. The
// sequential pre-fan-out preparation loop (sibling hooks, the priming pass,
// queue time) is invisible to every file's budget, so an early healthy item
// can never reach wf.Apply with a context expired by its siblings; and the
// file's own hook still counts against its own total (F4 — no fresh full
// re-grant for apply).
func TestApplyPhase_Run_PerFileBudgetChargedToOwnHookOnly(t *testing.T) {
	const perFile = 1200 * time.Millisecond
	const early, slowB, slowC = "/source/a-early.mp4", "/source/b-slow.mp4", "/source/c-slower.mp4"
	files := []string{early, slowB, slowC}
	// Prepared sequentially AFTER the early item, the two slow sibling hooks
	// burn 2x700ms > perFile of WALL CLOCK before fan-out: a prepare-time
	// absolute deadline (the F4 form) would hand the early file an already
	// expired apply context although its own work (instant hook + apply)
	// fits WorkerTimeout with room to spare. Each slow hook stays under its
	// OWN perFile budget, so every file still executes.
	hookDelay := map[string]time.Duration{
		slowB: 700 * time.Millisecond,
		slowC: 700 * time.Millisecond,
	}

	wf := &primingStubWorkflow{total: len(files)}
	wf.applyResult = &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: len(files), WorkerTimeout: perFile}
	for _, p := range files {
		inputs.Results[p] = &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: p, MovieID: "M-100"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "M-100", Title: "Shared Destination"},
		}
	}

	type hookSpan struct {
		start    time.Time
		deadline time.Time
		elapsed  time.Duration
	}
	// The preparation loop is sequential on the phase goroutine, so these
	// writes strictly precede fan-out (and the reads after Run returns).
	hookSpans := make(map[string]*hookSpan, len(files))
	var organized, failed int
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		Destination:     "/output",
		PreApplyFunc: func(ctx context.Context, afc *ApplyFileContext) error {
			span := &hookSpan{}
			span.start = time.Now()
			span.deadline, _ = ctx.Deadline()
			hookSpans[afc.FilePath] = span
			if d := hookDelay[afc.FilePath]; d > 0 {
				time.Sleep(d) // a genuinely slow hook, well inside its OWN budget
			}
			span.elapsed = time.Since(span.start)
			return nil
		},
		OnPhaseComplete: func(o, f int) { organized, failed = o, f },
	})

	require.Equal(t, len(files), wf.getApplyCalled(), "no item is dropped by budget bookkeeping")
	assert.Equal(t, len(files), organized)
	assert.Equal(t, 0, failed)
	const tol = 350 * time.Millisecond // interval slack >> Windows ~15.6ms tick
	for _, p := range files {
		span := hookSpans[p]
		require.NotNil(t, span, "the hook ran exactly once for %s", p)
		// The hook side is unchanged: full WorkerTimeout measured from THIS
		// item's own preparation start (never the batch start, never a
		// sibling's clock).
		assert.WithinDuration(t, span.start.Add(perFile), span.deadline, tol,
			"%s: the hook keeps the full per-file budget from its own preparation", p)

		applyStart, entryErr, applyDeadline, hasDeadline := wf.applyObservationsFor(p)
		require.True(t, hasDeadline, "%s: the worker's apply context carries a deadline", p)
		// (a) Sibling preprocessing is invisible: the EARLY item — prepared
		// before its siblings burned 1.4s of wall clock, ahead of fan-out —
		// reaches Apply with a LIVE context (the F4 absolute deadline would
		// have handed it context.DeadlineExceeded at entry).
		assert.NoError(t, entryErr,
			"%s: sibling preparation must not expire this file's own budget", p)
		// (c) Exact arithmetic: applyDeadline − taskStart == WorkerTimeout −
		// hookElapsed. The test's span.elapsed and applyStart observe the
		// same hook call and (within microseconds) the same task start the
		// worker measured, so the pin is an interval check, not a strict
		// clock equality (Windows-safe).
		expectedDeadline := applyStart.Add(perFile - span.elapsed)
		assert.WithinDuration(t, expectedDeadline, applyDeadline, tol,
			"%s: apply deadline = own task start + WorkerTimeout − own hook elapsed", p)
	}
}

// floorRecordingWorkflow routes Apply through a REAL organizer on an afero
// filesystem (same wiring as the #241 dup-waiter tests) while recording the
// task context's entry state, so the F5 floor test can pin the observable
// contract: an essentially-expired context at Apply entry and zero
// filesystem mutation behind it. Single-file fixtures only: no mutex.
type floorRecordingWorkflow struct {
	organizerBackedWorkflow
	calls       int
	startedAt   time.Time
	deadline    time.Time
	hasDeadline bool
	entryErr    error
}

func (w *floorRecordingWorkflow) Apply(ctx context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	w.calls++
	w.startedAt = time.Now()
	w.entryErr = ctx.Err()
	w.deadline, w.hasDeadline = ctx.Deadline()
	return w.organizerBackedWorkflow.Apply(ctx, cmd)
}

// TestApplyPhase_Run_HookBurningFullBudgetYieldsFloorApplyContext pins the
// F5 floor (codex P2, PR #241): a file whose own PreApply hook already
// consumed — here OVERSHOT by pointedly ignoring its context — the whole
// WorkerTimeout gets an apply budget clamped at applyBudgetFloor. The floor
// keeps the derived duration non-negative (never a deadline before the
// task's own start), so WithDeadline cancels synchronously at construction:
// the workflow observes context.DeadlineExceeded from its first instruction,
// the real organizer returns before ANY filesystem mutation, and — honoring
// F4 — no second full timeout materializes for apply.
func TestApplyPhase_Run_HookBurningFullBudgetYieldsFloorApplyContext(t *testing.T) {
	const perFile = 200 * time.Millisecond
	const src = "/in/A.mkv"

	fsys := afero.NewMemMapFs()
	require.NoError(t, fsys.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(fsys, src, []byte("a-bytes"), 0o644))
	org := organizer.NewOrganizer(fsys, &organizer.Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)
	wf := &floorRecordingWorkflow{organizerBackedWorkflow: organizerBackedWorkflow{fs: fsys, org: org}}

	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 1, WorkerTimeout: perFile}
	inputs.Results[src] = &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: src, Name: "A.mkv", Extension: ".mkv", MovieID: "ABC-123"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ABC-123", Title: "Floor Case"},
	}

	var organized, failed int
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		Destination:     "/dest",
		PreApplyFunc: func(_ context.Context, _ *ApplyFileContext) error {
			// Deliberately NOT ctx-aware: the hook overshoots its own budget
			// (perFile) by ~2.75x (>> Windows ~15.6ms tick either side) —
			// the concrete shape of "hook consumed most of the timeout" (F4).
			time.Sleep(550 * time.Millisecond)
			return nil
		},
		OnPhaseComplete: func(o, f int) { organized, failed = o, f },
	})

	require.Equal(t, 1, wf.calls, "apply still runs once — the budget clamps, it never vetoes")
	// Expired at start BY CONSTRUCTION: the zero-remaining WithDeadline
	// cancels synchronously, so the ctx error predates any workflow work.
	assert.ErrorIs(t, wf.entryErr, context.DeadlineExceeded,
		"a hook burning the full budget leaves an essentially-expired apply context")
	require.True(t, wf.hasDeadline)
	// The floor IS the task start: deadline ≈ startedAt + applyBudgetFloor —
	// never earlier (no time travel), never a fresh full grant.
	assert.WithinDuration(t, wf.startedAt.Add(applyBudgetFloor), wf.deadline, 100*time.Millisecond,
		"the floored deadline is exactly the task start — non-negative duration, no second grant")

	// The apply failed THROUGH the deadline, and no filesystem mutation
	// happened behind it: the organizer's entry ctx recheck returned before
	// planning or moving anything.
	row := inputs.Updater.(*stubUpdater).getResult(src)
	require.NotNil(t, row, "the deadline failure is written back")
	assert.Equal(t, models.JobStatusFailed, row.Status)
	assert.Contains(t, row.Error, "timed out")
	content, err := afero.ReadFile(fsys, src)
	require.NoError(t, err, "the source file is untouched")
	assert.Equal(t, []byte("a-bytes"), content)
	exists, existsErr := afero.Exists(fsys, "/dest/ABC-123/ABC-123.mkv")
	require.NoError(t, existsErr)
	assert.False(t, exists, "no organized output was produced behind the floored context")

	assert.Equal(t, 0, organized)
	assert.Equal(t, 1, failed)
	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.False(t, lc.organized)
	assert.True(t, lc.completed)
}

// TestApplyPhase_Run_PrimingTimeoutRecordsRecoverableFailure pins codex P2
// (PR #241, F1): the priming call runs under the file's OWN remaining budget
// on a per-file context — never the raw batch context — so a ctx-aware slow
// planner unblocks when ITS priming deadline fires, leaving the batch alive.
// The file skips priming, records exactly ONE recoverable failure (mirroring
// the panic-recovery outcome shape minus the panic flag: written back as
// Failed with the timeout message, broadcast as StepFailed, replayed by its
// worker instead of re-executing), and the batch primes/applies the rest.
func TestApplyPhase_Run_PrimingTimeoutRecordsRecoverableFailure(t *testing.T) {
	const slow, good = "/source/a-slow-prime.mp4", "/source/b-good.mp4"
	const perFile = 300 * time.Millisecond

	wf := &primingStubWorkflow{total: 2, planBlock: map[string]bool{slow: true}}
	wf.applyResult = &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 2, WorkerTimeout: perFile}
	for _, p := range []string{slow, good} {
		inputs.Results[p] = &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: p, MovieID: "M-100"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "M-100"},
		}
	}

	var organized, failed int
	start := time.Now()
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		Destination:     "/output",
		OnPhaseComplete: func(o, f int) { organized, failed = o, f },
	})
	elapsed := time.Since(start)

	primeCalls, applyCmds, _ := wf.snapshot()
	assert.Equal(t, []string{slow, good}, primeCalls,
		"every item's priming is still attempted exactly once, in sorted order")
	require.Len(t, applyCmds, 1)
	assert.Equal(t, good, applyCmds[0].Match.Path,
		"the timed-out file is never executed; the healthy file applies normally")
	assert.Less(t, elapsed, 5*time.Second,
		"the priming ctx canceled at its own deadline — the batch never stalled behind the raw batch context")

	// The priming deadline is the file's OWN remaining budget (its hook was
	// instant, so ≈ the full WorkerTimeout) measured from ITS priming start —
	// an interval pin, not a strict clock Less (Windows-safe).
	primeStart, primeDeadline, _, ok := wf.primingObservation(slow)
	require.True(t, ok, "the slow file's priming ran")
	assert.WithinDuration(t, primeStart.Add(perFile), primeDeadline, 250*time.Millisecond,
		"priming ran under a per-file ctx bounded by the file's own WorkerTimeout share")

	assert.Equal(t, 1, organized, "the healthy file's success is unaffected by the priming-timeout sibling")
	assert.Equal(t, 1, failed, "the priming timeout records exactly one per-file failure")

	row := inputs.Updater.(*stubUpdater).getResult(slow)
	require.NotNil(t, row, "the recoverable failure is written back, mirroring the planning-panic path")
	assert.Equal(t, models.JobStatusFailed, row.Status)
	assert.Contains(t, row.Error, "duplicate preflight planning timed out",
		"the failure is a recoverable priming timeout, not a panic")
	assert.NotContains(t, row.Error, "panic")

	broadcaster := inputs.Broadcaster.(*stubBroadcaster)
	foundFailureBroadcast := false
	for _, evt := range broadcaster.events {
		if evt.Step == StepFailed && evt.MovieID == "M-100" && strings.Contains(evt.Message, "planning timed out") {
			foundFailureBroadcast = true
		}
	}
	assert.True(t, foundFailureBroadcast,
		"the timeout is broadcast exactly like a recovered planning panic")

	lc := inputs.Lifecycle.(*stubLifecycle)
	assert.False(t, lc.failed, "the phase main body survives: no job-sinking failure")
	assert.False(t, lc.organized, "a recorded failure keeps the job out of Organized")
	assert.True(t, lc.completed)
}

// TestApplyPhase_Run_PrimingSpendChargedToOwnBudget pins codex P2 (PR #241,
// F1) charging: the file's OWN priming spend is measured alongside
// hookElapsed and debited from the budget the worker starts at ITS task
// start — applyDeadline == taskStart + WorkerTimeout − hookElapsed −
// primingElapsed — so hook + priming + apply stay bounded by ONE
// WorkerTimeout with no silent re-grant for whatever priming burned.
func TestApplyPhase_Run_PrimingSpendChargedToOwnBudget(t *testing.T) {
	const solo = "/source/solo.mp4"
	const perFile = 1500 * time.Millisecond

	wf := &primingStubWorkflow{total: 1, planDelay: map[string]time.Duration{solo: 350 * time.Millisecond}}
	wf.applyResult = &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 1, WorkerTimeout: perFile}
	inputs.Results[solo] = &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: solo, MovieID: "M-100"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "M-100"},
	}

	// The preparation loop is sequential on the phase goroutine, so this
	// write strictly precedes fan-out (and the reads after Run returns).
	var hookStart time.Time
	var hookElapsed time.Duration
	var organized, failed int
	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		Destination:     "/output",
		PreApplyFunc: func(_ context.Context, afc *ApplyFileContext) error {
			hookStart = time.Now()
			time.Sleep(250 * time.Millisecond) // a genuinely slow hook, well inside its own budget
			hookElapsed = time.Since(hookStart)
			return nil
		},
		OnPhaseComplete: func(o, f int) { organized, failed = o, f },
	})

	require.NotZero(t, hookStart, "the hook ran exactly once")
	_, _, primeElapsed, ok := wf.primingObservation(solo)
	require.True(t, ok, "the file's priming ran")
	assert.GreaterOrEqual(t, primeElapsed, 300*time.Millisecond,
		"the priming spend is measured, not dropped (the planner burned 350ms)")

	applyStart, entryErr, applyDeadline, hasDeadline := wf.applyObservationsFor(solo)
	require.True(t, hasDeadline, "the worker's apply context carries a deadline")
	assert.NoError(t, entryErr,
		"hook+priming (~600ms) fit the budget with room — apply enters with a live context")
	// Exact arithmetic pin, interval-style: applyDeadline ≈ taskStart +
	// WorkerTimeout − hookElapsed − primingElapsed. Stub spans observe the
	// same hook/priming calls (the worker's own measurements additionally
	// include microseconds of command-build overhead, absorbed by the
	// tolerance — no strict-Less clock pins, Windows-safe).
	expectedDeadline := applyStart.Add(perFile - hookElapsed - primeElapsed)
	assert.WithinDuration(t, expectedDeadline, applyDeadline, 350*time.Millisecond,
		"apply deadline = own task start + WorkerTimeout − own hook elapsed − own priming elapsed")
	assert.Equal(t, 1, organized)
	assert.Equal(t, 0, failed)
}

// TestApplyPhase_Run_CtxIgnoringPrimingOvershootClampsAtFloor pins the F1
// endpoint of the charging contract: a ctx-IGNORANT planner cannot be
// preempted — but its overshoot is still measured and charged, so the
// remainder clamps at applyBudgetFloor and the apply context is
// essentially-expired from construction (the SAME shape F5 pins for a hook
// overshoot). The priming itself was lawful and still registers; only the
// apply budget is exhausted.
func TestApplyPhase_Run_CtxIgnoringPrimingOvershootClampsAtFloor(t *testing.T) {
	const solo = "/source/solo.mp4"
	const perFile = 250 * time.Millisecond

	wf := &primingStubWorkflow{total: 1, planDelay: map[string]time.Duration{solo: 550 * time.Millisecond}}
	wf.applyResult = &workflow.ApplyResult{Movie: &models.Movie{ID: "M-100"}}
	inputs := makeApplyInputs(wf)
	inputs.Concurrency = concurrencyConfig{MaxWorkers: 1, WorkerTimeout: perFile}
	inputs.Results[solo] = &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: solo, MovieID: "M-100"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "M-100"},
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
		Destination:     "/output",
	})

	primeCalls, _, _ := wf.snapshot()
	assert.Equal(t, []string{solo}, primeCalls, "the lawful priming still completes and registers")
	applyStart, entryErr, applyDeadline, hasDeadline := wf.applyObservationsFor(solo)
	require.True(t, hasDeadline)
	assert.ErrorIs(t, entryErr, context.DeadlineExceeded,
		"hook+priming overshoot the whole budget — the floored apply context is expired from construction")
	assert.WithinDuration(t, applyStart.Add(applyBudgetFloor), applyDeadline, 250*time.Millisecond,
		"the floored deadline is exactly the task start — clamped, never time-travel")
}
