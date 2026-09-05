package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"github.com/javinizer/javinizer-go/internal/progress"
	"github.com/javinizer/javinizer-go/internal/worker/fanout"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// ApplyPhase runs the apply (organize) step over a batch job's scraped files.
type ApplyPhase interface {
	Run(ctx context.Context, inputs applyPhaseInputs, cfg ApplyPhaseConfig)
}

// applyBudgetFloor is the minimum per-file apply budget granted at worker
// task start when the item's own PreApply hook already consumed (or, by
// ignoring its context, overshot) the whole WorkerTimeout (codex P2, PR
// #241 F5). Zero is the one duration that is never negative — the derived
// deadline lands exactly AT the task start instead of in its past (no time
// travel) — and context.WithDeadline against a non-future deadline cancels
// SYNCHRONOUSLY, so the workflow receives an essentially-expired-but-
// consistent context: DeadlineExceeded from the moment the task context
// exists, letting the organizer's entry recheck return before any
// filesystem work. The file's own hook having burned the grant satisfies
// F4 (no second full timeout) without a clock comparison at the worker.
const applyBudgetFloor = time.Duration(0)

type applyPhase struct{}

// NewApplyPhase returns the default ApplyPhase implementation.
func NewApplyPhase() ApplyPhase {
	return &applyPhase{}
}

// applyItem is one file selected for this apply iteration.
type applyItem struct {
	filePath   string
	fileResult *resultstore.MovieResult
	movie      *models.Movie
}

// preparedApplyFile is the per-item command material the apply phase builds
// ONCE per item, in sorted order, before worker fan-out (#240 finding A):
// the duplicate preflight's owner priming derives from the same commands the
// workers later execute, so PreApply-hook mutations reach both, and every
// hook still runs exactly once per file. baseline is the phase-entry movie
// clone (codex r51), frozen before the hook could mutate the live pointer.
type preparedApplyFile struct {
	cmd      workflow.ApplyCmd
	afc      *ApplyFileContext
	baseline *models.Movie
	execute  bool
	// hookElapsed is the item's OWN preparation spend — the PreApply hook
	// plus command build, measured on the phase goroutine (codex P2, PR
	// #241 F5). The preparation loop is SEQUENTIAL over the batch and
	// fan-out starts only after every item is prepared+primed, so an
	// absolute deadline stamped here would silently charge every SIBLING's
	// hook (and the priming pass, and fan-out queue time) against THIS
	// file's budget: in a large batch or behind a slow later hook, an
	// early healthy item could reach wf.Apply with an already-expired
	// context although its own work fits WorkerTimeout. The worker instead
	// starts the budget clock at ITS task start and debits only this
	// own-work spend (F4 still holds: a hook burning the whole timeout
	// never earns a second full grant — the remainder clamps to
	// applyBudgetFloor).
	hookElapsed time.Duration
	// primingElapsed is the item's OWN duplicate-priming spend — the
	// PlanDuplicatePriming call, measured on the phase goroutine during the
	// sequential priming pass (codex P2, PR #241 F1). The priming pass runs
	// BEFORE fan-out on the phase goroutine, so without this debit a slow
	// priming call consumed real per-file budget invisibly: hookElapsed
	// alone under-charged and apply silently re-granted whatever priming
	// burned. The worker debits hookElapsed+primingElapsed from the budget
	// it starts at ITS task start; sibling priming spends stay invisible
	// exactly like sibling preparation (only the file's OWN priming is
	// charged).
	primingElapsed time.Duration
	// hookOutcome is non-nil when the priming-time PreApply hook panicked
	// (codex r2 P2), the priming-time planning pass panicked (codex P2, PR
	// #241 F3), or planning exhausted the file's own priming budget (codex
	// P2, PR #241 F1): the prepare/priming pass recovered, wrote back, and
	// broadcast the failure already, so the item's worker replays this
	// outcome instead of re-executing — the once-per-file hook contract
	// holds and the batch counts exactly one recorded failure for the file.
	hookOutcome *applyFileOutcome
	// stationary marks a primed item whose plan moves nothing (WillMove=false
	// — a resident already sitting at its destination, codex P1, PR #241):
	// its priming PARKED the canonical key as a PENDING claim terminal-gated
	// on this item's own worker (codex P2, PR #241 F1), so observing movers
	// block until it validates. The apply phase schedules these items FIRST
	// in fan-out order (sorted order preserved within each class) so a
	// single-worker run validates the resident before any mover can wait on
	// its key instead of deadlocking behind a mover sorted ahead of it.
	stationary bool
}

// primeDuplicateClaims runs the batch's ONE planning pass when the workflow
// exposes the read-only planning seam: each prepared item's organize target
// registers against the run's duplicate tracker in sorted order,
// pre-assigning every canonical key's winner (and its ordered standbys,
// codex P2, PR #241 F1) before any apply worker starts (#240 finding A).
// Items whose PreApply hook declined execution (or panicked into a recorded
// failure), whose plan fails or exhausts the file's own priming budget
// (codex P2, PR #241 F1), or whose source already vanished register nothing
// (codex r2 P2 — extended to stationary residents by codex P2, PR #241 F1:
// an unverified resident would otherwise park a born-settled ghost claim) —
// their workers skip execution or fail with the identical plan error, so
// priming can never claim a file that cannot run. Without
// the seam (or on a nil workflow) the tracker stays unprimed, preserving
// first-come observation for single-file callers.
func primeDuplicateClaims(ctx context.Context, wf workflow.WorkflowInterface, tracker *organizer.DuplicateTracker, items []applyItem, prepared map[string]*preparedApplyFile, inputs applyPhaseInputs, cfg ApplyPhaseConfig) {
	planner, ok := wf.(workflow.DuplicatePrimingPlanner)
	if !ok {
		return
	}
	primings := make([]organizer.DuplicatePriming, 0, len(items))
	for _, item := range items {
		p := prepared[item.filePath]
		if !p.execute {
			continue
		}
		// codex P2 (PR #241 F1): the planning call is bounded by the file's
		// OWN remaining budget — WorkerTimeout debited by its own hook spend.
		// The priming pass is SEQUENTIAL on the phase goroutine, so an
		// unbounded call behind the raw batch context could stall every
		// later file's priming AND fan-out; the per-file context cancels at
		// its own deadline without touching the batch context. The measured
		// spend (primingElapsed) is then charged to the same budget at the
		// worker's task start. A file whose hook already burned the whole
		// grant primes under applyBudgetFloor: the zero-duration context is
		// already expired at construction, a ctx-aware planner returns
		// immediately, and the file records the timeout instead of priming.
		primeCtx := ctx
		var primeBudget time.Duration
		var primeCancel context.CancelFunc
		if timeout := inputs.Concurrency.WorkerTimeout; timeout > 0 {
			primeBudget = timeout - p.hookElapsed
			if primeBudget < applyBudgetFloor {
				primeBudget = applyBudgetFloor
			}
			primeCtx, primeCancel = context.WithTimeout(ctx, primeBudget)
		}
		primingStart := time.Now()
		priming, recorded, err := planPrimingRecovered(ctx, primeCtx, planner, item, p.cmd, inputs, cfg, primeBudget)
		p.primingElapsed = time.Since(primingStart)
		if primeCancel != nil {
			primeCancel()
		}
		if recorded != nil {
			// codex P2 (PR #241 F3 panics, F1 timeouts): the planning failure
			// was already recovered, written back, and broadcast per file —
			// the worker replays the recorded outcome (planning never runs
			// twice) while the batch continues with every other item.
			p.execute = false
			p.hookOutcome = recorded
			continue
		}
		if err != nil {
			logging.Warnf("[Apply] duplicate preflight planning skipped %s: %v", item.filePath, err)
			continue
		}
		// codex P2 (PR #241 F1): mirror PrimeBatch's park condition so the
		// fan-out's residents-first ordering covers exactly the primings that
		// left a pending parked claim behind.
		p.stationary = !priming.WillMove && priming.TargetPath != ""
		primings = append(primings, priming)
	}
	tracker.PrimeBatch(primings)
}

// planPrimingRecovered runs ONE item's priming-plan call under the apply
// phase's per-file recovery boundary (codex P2, PR #241 F3) and per-file
// budget boundary (codex P2, PR #241 F1).
// PlanDuplicatePriming runs the organizer's whole planning pipeline on the
// phase's MAIN goroutine, so an unrecovered panic would escape
// withFileRecovery (which exists only inside the fanned-out workers) and
// sink the whole job. A recovered panic is written back and broadcast with
// the identical semantics as a preparation or execution panic — that file
// fails with the recorded panic message and the batch continues — and the
// returned outcome is what the item's worker replays.
//
// F1: primeCtx carries the file's OWN remaining budget (primeBudget =
// WorkerTimeout − hookElapsed, clamped at applyBudgetFloor; zero primeBudget
// means unbounded when no WorkerTimeout is configured). Its deadline firing
// while the BATCH context is still alive is this file's priming budget
// exhausting: planning is abandoned, the file skips priming, and the SAME
// recorded-failure shape is published (write-back + broadcast + replayed
// outcome), minus the panic flag — a timeout is a recoverable per-file
// failure, never a panic. A batch-level cancellation or deadline surfaces
// through batchCtx instead and keeps the ordinary log-and-continue skip —
// it is never mislabeled as a per-file priming timeout.
func planPrimingRecovered(batchCtx, primeCtx context.Context, planner workflow.DuplicatePrimingPlanner, item applyItem, cmd workflow.ApplyCmd, inputs applyPhaseInputs, cfg ApplyPhaseConfig, primeBudget time.Duration) (priming organizer.DuplicatePriming, recorded *applyFileOutcome, err error) {
	outcome := applyFileOutcome{FilePath: item.filePath, MovieID: item.movie.ID, DryRun: cfg.DryRun}
	// Mirror prepareApplyItem's recoveryContext assembly field-for-field: the
	// recorded failure is indistinguishable from a worker-side panic.
	rc := recoveryContext{
		filePath:         item.filePath,
		fmi:              item.fileResult.FileMatchInfo,
		movie:            item.fileResult.Movie,
		provenance:       inputs.Provenance[item.filePath],
		updater:          inputs.Updater,
		broadcast:        broadcastFailure(inputs.Broadcaster, inputs.JobID, item.movie.ID, jobEventPhaseApply, "Apply"),
		startTime:        time.Now(),
		editLockFn:       inputs.EditLockFn,
		promoteWitnessFn: inputs.PromoteWitnessFn,
	}
	// Post-recovery bookkeeping is deferred FIRST so it runs AFTER the
	// recovery func below (LIFO) — recover() only bites when called directly
	// by a deferred function, exactly like prepareApplyItem's dual-defers.
	defer func() {
		if outcome.Panic || outcome.Failed {
			recorded = &outcome
		}
	}()
	defer withFileRecovery(rc, &outcome)()
	priming, err = planner.PlanDuplicatePriming(primeCtx, cmd)
	if err != nil && primeBudget > 0 && errors.Is(primeCtx.Err(), context.DeadlineExceeded) && batchCtx.Err() == nil {
		// The file's own priming budget fired mid-plan with the batch still
		// alive: skip priming and record the recoverable failure through the
		// same publication path a panic would take (minus the panic flag).
		recordPrimingTimeout(rc, &outcome, primeBudget)
	}
	return priming, nil, err
}

// recordPrimingTimeout publishes a per-file priming budget exhaustion through
// the IDENTICAL write-back + broadcast path a recovered planning panic takes
// (codex P2, PR #241 F1 — publishRecoveryFailure is the panic recovery's
// publication half), then marks the replayed outcome as a plain recoverable
// failure: Panic stays false (nothing panicked — no panic audit), Failed is
// set so the batch counts exactly one failure and the row stays retryable,
// and the batch continues with every other file.
func recordPrimingTimeout(rc recoveryContext, outcome *applyFileOutcome, primeBudget time.Duration) {
	msg := fmt.Sprintf("duplicate preflight planning timed out after %v", primeBudget)
	logging.Warnf("[Apply] %s (%s skips priming; the batch continues)", msg, rc.filePath)
	publishRecoveryFailure(rc, msg)
	outcome.Failed = true
	outcome.ErrorMsg = msg
}

// applyFileOutcome captures the result of applying a single file.
// Collected by the errgroup goroutine, then aggregated by trackApplyResults.
type applyFileOutcome struct {
	FilePath  string
	MovieID   string
	Success   bool
	Failed    bool // true if apply failed (not panic, not skip, not cancel)
	Cancelled bool // true if apply was interrupted by context cancellation
	Panic     bool // true if goroutine panicked
	PanicMsg  string
	ErrorMsg  string
	Movie     *models.Movie // updated movie after apply (nil if failed)
	DryRun    bool          // true if apply was a dry-run
}

// recoverRunPanic marks the job failed only when a panic recovered in Run's
// defer counciled the main body (never nil on normal exit).
func recoverRunPanic(inputs applyPhaseInputs, r any) {
	if r == nil {
		return
	}
	panicErr := panicutil.FormatRecover(r)
	logging.Errorf("BatchJob.StartApply %s %v", inputs.JobID.String(), panicErr)
	inputs.Lifecycle.MarkFailed()
}

// Run executes the apply phase: setup errgroup → iterate files → dispatch
// applyFile → collect outcomes → track results → report status.
func (p *applyPhase) Run(ctx context.Context, inputs applyPhaseInputs, cfg ApplyPhaseConfig) {
	wf := inputs.WF
	persister := inputs.persister

	defer func() {
		// Extraction keeps the recovery arm testable: fanout workers forward
		// panics, so ONLY main-body panics reach this defer — and a named fn is
		// the honest seam for exercising MarkFailed on phase panic (codex P2-E).
		recoverRunPanic(inputs, recover())
		if persister != nil {
			if err := persister.Persist(); err != nil {
				logging.Warnf("[Apply] envelope persist failed: %v", err)
			}
		}
	}()

	excludedSnapshot := make(map[string]bool, len(inputs.Results))
	for filePath := range inputs.Results {
		excludedSnapshot[filePath] = inputs.Excluded[filePath]
	}

	// Build the filtered list of files to apply. inputs.Results is a frozen
	// snapshot of result state at apply start time. This is intentional:
	// apply operates on scrape-time state, and mutations (e.g., rescrape,
	// exclusion) during apply go through the live tracker via inputs.Updater
	// but do not affect which files this apply iteration processes. This
	// prevents concurrent-modification bugs during iteration.
	retryPaths := make(map[string]struct{}, len(cfg.RetryFilePaths))
	for _, filePath := range cfg.RetryFilePaths {
		retryPaths[filePath] = struct{}{}
	}
	items := make([]applyItem, 0, len(inputs.Results))
	for filePath, fileResult := range inputs.Results {
		_, retryFailed := retryPaths[filePath]
		if fileResult.Movie == nil {
			continue
		}
		if len(retryPaths) > 0 {
			if !retryFailed || (fileResult.Status != models.JobStatusFailed && fileResult.Status != models.JobStatusCompleted) {
				continue
			}
		} else if fileResult.Status != models.JobStatusCompleted {
			continue
		}
		if excludedSnapshot[filePath] {
			logging.Infof("Skipping excluded file: %s", filePath)
			continue
		}
		items = append(items, applyItem{
			filePath:   filePath,
			fileResult: fileResult,
			movie:      fileResult.Movie,
		})
	}

	// Apply workers are intentionally concurrent, so slice order alone cannot
	// choose a shared-destination owner. Sort once and prime the downloader
	// claims before any goroutine starts; the first file path owns each folded
	// movie key, while failed owners still release the claim for retry.
	sort.Slice(items, func(i, j int) bool { return items[i].filePath < items[j].filePath })
	owners := make(map[string]string, len(items))
	for _, item := range items {
		key := strings.ToLower(strings.TrimSpace(item.movie.ID))
		if key != "" {
			if _, exists := owners[key]; !exists {
				owners[key] = item.filePath
			}
		}
	}
	total := len(items)
	var processed int64
	inputs.Dedup = &sync.Map{}
	downloader.PrimeDownloadOwners(inputs.Dedup, owners)
	// One intra-batch duplicate preflight registry per apply run (#224 phase
	// E): every file's plan registers its proven-equal canonical destination
	// key so same-batch target collisions surface as plan conflicts — or, when
	// overwrite is authorized, as persisted per-file warnings. Dry runs
	// construct the non-probing variant so key derivation never writes probe
	// artifacts (#240 finding B).
	cfg.OrganizeOptions.DuplicateTracker = organizer.NewDuplicateTracker(cfg.DryRun)
	// Prepare every item's ApplyCmd ONCE, in sorted order, before fan-out
	// (#240 finding A): priming and execution share identical commands, and
	// PreApply hooks keep their once-per-file contract. Each build runs under
	// its own per-file execution boundary (codex r2 P2) — a hook blocking on
	// the raw batch context could otherwise stall the entire priming pass,
	// and a panicking hook could abort it.
	prepared := make(map[string]*preparedApplyFile, len(items))
	for _, item := range items {
		prepared[item.filePath] = prepareApplyItem(ctx, item, inputs, cfg)
	}
	primeDuplicateClaims(ctx, wf, cfg.OrganizeOptions.DuplicateTracker, items, prepared, inputs, cfg)
	// codex P2 (PR #241 F1): primed stationary residents own PENDING parked
	// claims terminal-gated on their own workers, so an observing mover
	// blocks until its resident validates. Parallel workers absorb that
	// wait; a MaxWorkers=1 run with a mover sorted AHEAD of its resident
	// would deadlock on the still-pending key, so stationary items lead the
	// fan-out (stable within each class — mover-vs-mover sorted ownership
	// from priming is untouched, and multi-worker outcomes are unchanged:
	// residents simply finish validating sooner). Items without a primed
	// parked claim keep their sorted place.
	if len(items) > 1 {
		ordered := make([]applyItem, 0, len(items))
		for _, item := range items {
			if prepared[item.filePath].stationary {
				ordered = append(ordered, item)
			}
		}
		for _, item := range items {
			if !prepared[item.filePath].stationary {
				ordered = append(ordered, item)
			}
		}
		items = ordered
	}
	outcomes := fanout.BoundedFanOut(ctx, inputs.Concurrency.MaxWorkers, items,
		func(egCtx context.Context, item applyItem) applyFileOutcome {
			outcome := applyFile(egCtx, wf, item.filePath, item.fileResult, item.movie, prepared[item.filePath], inputs, cfg)
			// Report per-file progress so the frontend bar advances 0→100 across
			// files instead of jumping straight to 100 on OnPhaseComplete. A file
			// counts as processed whether it succeeded or failed — the bar tracks
			// throughput, not success rate (per-file success/failure is surfaced
			// separately via OnPhaseComplete + the controller's polling fallback).
			// This closure runs once per item, so total (= len(items)) is always > 0
			// here; the total<=0 case is handled at the broadcast boundary by
			// organizeProgressPercent and is unreachable from this call site.
			if cfg.OnFileProgress != nil {
				done := int(atomic.AddInt64(&processed, 1))
				cfg.OnFileProgress(done, total)
			}
			return outcome
		},
	)

	if err := ctx.Err(); err != nil {
		var org, fail int64
		trackApplyResults(inputs, outcomes, &org, &fail)
		inputs.Lifecycle.MarkCancelled()
		return
	}

	var organized int64
	var failed int64
	trackApplyResults(inputs, outcomes, &organized, &failed)

	orgCount := atomic.LoadInt64(&organized)
	// A normal apply intentionally skips failures produced by the scrape phase;
	// those rows have no eligible apply outcome and must not prevent successful
	// files from reaching Organized. A subset retry, however, must retain any
	// failed rows that were not selected, so it stays Completed until all
	// remaining failures are retried.
	failCount := atomic.LoadInt64(&failed)
	if len(cfg.RetryFilePaths) > 0 {
		failCount = countRemainingApplyFailures(inputs, outcomes)
	}

	// Broadcast the final organization_completed / update_completed WebSocket
	// message BEFORE MarkOrganized / MarkCompleted so frontend clients
	// watching for that status (organize-controller.handleWebSocketMessage)
	// can finalize the apply flow in real time. Mirrors main's
	// process_organize.go which called broadcastProgress inline at end of
	// organize. API layer wires the hook via ApplyPhaseConfig.OnPhaseComplete.
	if cfg.OnPhaseComplete != nil {
		cfg.OnPhaseComplete(int(orgCount), int(failCount))
	}

	if failCount == 0 && orgCount > 0 && !cfg.OrganizeOptions.Skip {
		inputs.Lifecycle.MarkOrganized()
	} else {
		inputs.Lifecycle.MarkCompleted()
	}
}

// retryWritebackClearedFailure confirms a successful retry actually cleared
// the live result's failed status. The frozen apply input cannot prove that:
// identity and promote-witness fences, or a write error, may skip write-back
// while interpretApplyResult still reports the workflow success.
func retryWritebackClearedFailure(inputs applyPhaseInputs, outcome applyFileOutcome) bool {
	reader, ok := inputs.Updater.(interface {
		GetMovieResult(string) (*resultstore.MovieResult, error)
	})
	if !ok {
		return false
	}
	current, err := reader.GetMovieResult(outcome.FilePath)
	return err == nil && current != nil && current.Status == models.JobStatusCompleted
}

// countRemainingApplyFailures returns the failures that remain after this apply
// attempt. The apply input is a frozen snapshot, so a subset retry must remove
// only paths that succeeded in this attempt while retaining eligible failures
// that were not selected. Scrape failures without a movie cannot produce an
// apply outcome and must not keep a successful retry from reaching Organized.
func countRemainingApplyFailures(inputs applyPhaseInputs, outcomes []applyFileOutcome) int64 {
	failedPaths := make(map[string]struct{})
	for filePath, result := range inputs.Results {
		if result != nil && result.Movie != nil && !inputs.Excluded[filePath] && result.Status == models.JobStatusFailed {
			failedPaths[filePath] = struct{}{}
		}
	}
	for _, outcome := range outcomes {
		if outcome.Success && retryWritebackClearedFailure(inputs, outcome) {
			delete(failedPaths, outcome.FilePath)
		}
		if outcome.Failed || outcome.Panic {
			failedPaths[outcome.FilePath] = struct{}{}
		}
	}
	return int64(len(failedPaths))
}

// prepareApplyItem builds one sorted item's ApplyCmd ahead of fan-out,
// wrapping the priming-time build — PreApply hook included — in the SAME
// per-file execution boundary applyFile enforces around its worker body
// (codex r2 P2): a per-file WorkerTimeout context, so a hook blocking past
// its budget unblocks instead of stalling the whole batch priming pass
// behind an untamed batch context; and withFileRecovery, so a panicking
// hook records THIS file's failure and the run continues instead of
// aborting every remaining file. A recovered panic yields execute=false
// plus a pre-recorded failure outcome the item's worker replays unchanged
// (the hook never runs twice).
//
// codex P2 (PR #241 F4+F5): the hook spends from the per-file budget here,
// and the item's OWN preparation spend is recorded as hookElapsed; applyFile
// later debits exactly that — plus primingElapsed, the file's own priming
// spend (codex P2, PR #241 F1) — from the budget it starts at ITS task
// start. Hook + priming + apply stay bounded by ONE WorkerTimeout — no
// fresh full re-grant — while sibling preparation and priming, and fan-out
// queue time never consume THIS file's budget anymore (the preparation and
// priming loops are sequential, fan-out starts only after ALL items are
// prepared+primed).
func prepareApplyItem(ctx context.Context, item applyItem, inputs applyPhaseInputs, cfg ApplyPhaseConfig) (prepared *preparedApplyFile) {
	prepared = &preparedApplyFile{baseline: item.movie.Clone()}
	if timeout := inputs.Concurrency.WorkerTimeout; timeout > 0 {
		// The hook's own budget is still the full WorkerTimeout measured from
		// THIS item's preparation start — only the accounting handed to the
		// worker changed (hookElapsed), not the hook-facing boundary.
		hookCtx, hookCancel := context.WithTimeout(ctx, timeout)
		defer hookCancel()
		ctx = hookCtx
	}

	outcome := applyFileOutcome{FilePath: item.filePath, MovieID: item.movie.ID, DryRun: cfg.DryRun}
	// Mirror applyFile's recoveryContext assembly field-for-field; the
	// poster-claim release stays at the worker boundary (fan-out per-item).
	rc := recoveryContext{
		filePath:         item.filePath,
		fmi:              item.fileResult.FileMatchInfo,
		movie:            item.fileResult.Movie,
		provenance:       inputs.Provenance[item.filePath],
		updater:          inputs.Updater,
		broadcast:        broadcastFailure(inputs.Broadcaster, inputs.JobID, item.movie.ID, jobEventPhaseApply, "Apply"),
		startTime:        time.Now(),
		editLockFn:       inputs.EditLockFn,
		promoteWitnessFn: inputs.PromoteWitnessFn,
	}
	// Post-recovery bookkeeping is deferred FIRST so it runs AFTER the
	// recovery func below (LIFO) — recover() only bites when called directly
	// by a deferred function, exactly like applyFile's defer site.
	defer func() {
		if outcome.Panic {
			prepared.execute = false
			prepared.hookOutcome = &outcome
		}
	}()
	defer withFileRecovery(rc, &outcome)()

	prepareStart := time.Now()
	prepared.cmd, prepared.afc, prepared.execute = buildApplyCmd(item.filePath, item.movie, item.fileResult, inputs, cfg, ctx)
	prepared.hookElapsed = time.Since(prepareStart)
	return prepared
}

// buildApplyCmd constructs the workflow.ApplyCmd for a single file apply.
// It resolves the destination path, builds the command, and runs the
// PreApply hook if configured (which may mutate the ApplyFileContext and
// thus the returned ApplyCmd fields).
// applyFamilyLock serializes an apply write-back against review edits on ALL
// identity keys (codex r37 P2): edits lock the MATCHER alias
// (FileMatchInfo.MovieID), while buildApplyCmd rewrote afc.Match.MovieID to
// the canonical Movie.ID — applying under the canonical key alone lets the
// atomic write-back run between an edit's candidate snapshot and its
// post-transaction publication on the alias key.
//
// codex r42 P2: the lock acquisition MUST route through the registry's ONE
// total order (AcquireMany folds keys uppercase then sorts). Any caller-side
// ordering rule reproduces a DIFFERENT comparison (e.g. lowercase sort
// orders "z" AFTER "_" while the fold orders "Z" BEFORE "_") — a concurrent
// multi-key review edit then deadlocks against this write-back.
func applyFamilyLock(inputs applyPhaseInputs, aliasID, canonicalID string) func() {
	if inputs.EditLockFn == nil {
		return func() {}
	}
	return inputs.EditLockFn(aliasID, canonicalID)
}

// applyFamilyKeyIDs extracts the matcher alias (edit-lock identity) and the
// canonical Movie.ID (apply-rewritten identity) for a write-back.
func applyFamilyKeyIDs(afc *ApplyFileContext) (aliasID, canonicalID string) {
	canonicalID = afc.Match.MovieID
	if afc.MovieResult != nil {
		aliasID = afc.MovieResult.FileMatchInfo.MovieID
	}
	return aliasID, canonicalID
}

func buildApplyCmd(
	filePath string,
	movie *models.Movie,
	fileResult *resultstore.MovieResult,
	inputs applyPhaseInputs,
	cfg ApplyPhaseConfig,
	ctx context.Context,
) (workflow.ApplyCmd, *ApplyFileContext, bool) {
	sourceDir := filepath.Dir(filePath)
	match := fileResult.FileMatchInfo
	match.MovieID = movie.ID

	destPath := cfg.Destination
	if destPath == "" {
		destPath = inputs.Destination
	}
	// In-place modes (organize runs but no destination is required) must fall
	// back to the source dir so downstream steps (download/NFO) resolve a real
	// directory. The previous gate on cfg.OrganizeOptions.Skip is stale: the
	// skip-gate fix (!RequiresOrganize()) means in-place modes now run organize
	// with Skip=false, so an empty destination would otherwise stay empty.
	// Gate on the effective override mode requiring organize instead of Skip.
	if destPath == "" {
		if mode := cfg.OperationModeOverride; mode != "" && mode.RequiresOrganize() {
			destPath = sourceDir
		} else if cfg.OrganizeOptions.Skip {
			destPath = sourceDir
		}
	}

	applyCmd := workflow.ApplyCmd{
		Movie:                  movie,
		Match:                  match,
		DestPath:               destPath,
		DryRun:                 cfg.DryRun,
		Organize:               cfg.OrganizeOptions,
		Merge:                  cfg.MergeOptions,
		Download:               cfg.Download,
		DisplayTitleSrc:        movie,
		DownloadExtrafanart:    cfg.DownloadExtrafanart,
		OverwriteExistingMedia: cfg.OverwriteExistingMedia,
		Dedup:                  inputs.Dedup,
		DedupOwnerKey:          filePath,
		DedupLogicalKey:        strings.ToLower(strings.TrimSpace(movie.ID)),
		OperationMode:          cfg.OperationModeOverride,
	}

	applyCmd.GenerateNFO = cfg.GenerateNFO && (inputs.NFOEnabled || cfg.ForceNFO)

	afc := &ApplyFileContext{
		FilePath:    filePath,
		Movie:       movie,
		MovieResult: fileResult,
		Match:       match,
		Destination: destPath,
	}

	if cfg.PreApplyFunc != nil {
		if err := cfg.PreApplyFunc(ctx, afc); err != nil {
			logging.Warnf("PreApply hook skipped %s: %v", filePath, err)
			return applyCmd, afc, false // false = skip execution
		}
		applyCmd.Movie = afc.Movie
		applyCmd.Match = afc.Match
		applyCmd.DestPath = afc.Destination
		applyCmd.DedupLogicalKey = strings.ToLower(strings.TrimSpace(afc.Movie.ID))
	}

	return applyCmd, afc, true
}

// interpretApplyResult processes the workflow.Apply result/error into an
// applyFileOutcome. It updates the job result tracker, broadcasts events,
// and runs the PostApply hook if configured.
func interpretApplyResult(
	filePath string,
	movie *models.Movie,
	startTime time.Time,
	applyTimeout time.Duration,
	inputs applyPhaseInputs,
	cfg ApplyPhaseConfig,
	taskCtx context.Context,
	afc *ApplyFileContext,
	result *workflow.ApplyResult,
	applyErr error,
) applyFileOutcome {
	outcome := applyFileOutcome{
		FilePath: filePath,
		MovieID:  movie.ID,
	}

	afr := &ApplyFileResult{
		Result: result,
		Err:    applyErr,
	}

	if cfg.PostApplyFunc != nil {
		cfg.PostApplyFunc(taskCtx, afc, afr)
	}

	if applyErr != nil {
		errMsg := applyErr.Error()
		if errors.Is(applyErr, context.DeadlineExceeded) {
			errMsg = fmt.Sprintf("apply timed out after %v", applyTimeout)
		}
		// A mid-apply cancellation is not an organize failure: the file was
		// scraped successfully, just not organized. Mirror scrape_phase.go and
		// preserve the Cancelled status (old OrganizeTask returned the error to
		// the pool without mutating the per-file FileResult, so cancelled-but-
		// scraped files stayed Completed). Main's process_organize.go likewise
		// did not relabel them Failed.
		fileStatus := models.JobStatusFailed
		isCancelled := errors.Is(applyErr, context.Canceled)
		if isCancelled {
			fileStatus = models.JobStatusCancelled
			errMsg = "organization canceled"
		}
		now := time.Now()
		// Preserve the prior scrape-phase Movie on the apply-failure path.
		// Main's process_organize.go returned early on organizeErr WITHOUT
		// mutating the per-file FileResult, so the Movie that the scrape
		// phase populated survived for inspection/retry on failed-apply rows.
		// UpdateFileResult replaces the whole struct (preserving only ResultID
		// + Revision), so without Movie set here, the API response for the
		// failed-apply row loses its movie payload and /review/[jobId] can't
		// render the movie card / poster preview. Same dropped-on-failure-path
		// pattern fixed for FileMatchInfo in commit 6249de64.
		// Live-session merge (not whole-struct replace): the review-editable
		// set keeps the LIVE value when the user edited it mid-phase
		// (codex P6-B); phase-computed fields still come from the frozen
		// movie. Falls back to whole-struct write when no result exists yet
		// (early-fail paths in phase tests create rows on demand).
		// Serialize with concurrent review edits (codex r11 P1): publication of
		// the merged write-back happens under the movie family key.
		// codex r37 P2: lock BOTH identity keys — edits hold the matcher
		// alias while afc.Match.MovieID was rewritten to the canonical ID.
		aliasID, canonicalID := applyFamilyKeyIDs(afc)
		unlock := applyFamilyLock(inputs, aliasID, canonicalID)
		defer unlock()

		// codex P2-C/D: settled-rekey skip runs UNDER the family key; the
		// callback's mismatch return is the net for rekeys landing mid-write.
		// codex P2: fence the FAILURE write-back (and panic-converted failures
		// land here too) behind outstanding promote witnesses — its revision
		// bump would make startup arbitrate the failed refresh as committed.
		if mid := strings.TrimSpace(movie.ID); mid != "" && inputs.PromoteWitnessFn != nil && inputs.PromoteWitnessFn(mid) {
			logging.Warnf("[Apply] skipping failure write-back for %s — promote witness for %s unresolved; restart reconciles", filePath, mid)
		} else if !writebackPreSkipped(inputs.Updater, movie, filePath, "Apply") {
			// R10-6: state+provenance publish under ONE acquisition — a persist
			// snapshot between the two never observes mismatched halves.
			errUp := inputs.Updater.AtomicUpdateFileResultWithProvenance(filePath, func(current *resultstore.MovieResult, prov *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
				if applyWritebackIdentityMismatch(movie, current) {
					logging.Warnf("[Apply] skipping write-back for %s — result rekeyed to %s mid-phase", filePath, current.FileMatchInfo.MovieID)
					return current, prov, nil
				}
				fm := applyMatchFollowedByLiveIdentity(afc.Match, current)
				current.FileMatchInfo = fm
				current.Movie = mergeLiveReviewEdits(movie, movie, current.Movie)
				current.Status = fileStatus
				current.Error = errMsg
				current.StartedAt = startTime
				current.EndedAt = &now
				return current, mergeWriteBackProvenance(inputs.Provenance[filePath], prov), nil
			})
			if errUp != nil {
				upsertWriteBackResultWithProvenance(inputs.Updater, filePath, &resultstore.MovieResult{
					FileMatchInfo: afc.Match,
					Movie:         movie,
					Status:        fileStatus,
					Error:         errMsg,
					StartedAt:     startTime,
					EndedAt:       &now,
				}, inputs.Provenance[filePath])
			}
		}

		if isCancelled {
			if result != nil && result.OrganizeResult != nil {
				auditOrganizeSuccess(inputs, movie, filePath, result, cfg)
			}
			// and do NOT invoke OnFileFailed, otherwise the review page records
			// the file as failed and offers a Retry path despite the persisted
			// result being Cancelled.
			inputs.Broadcaster.Send(JobEvent{
				JobID:     inputs.JobID,
				MovieID:   movie.ID,
				Phase:     jobEventPhaseApply,
				Step:      StepApply,
				Message:   errMsg,
				Timestamp: time.Now(),
			})
			outcome.Cancelled = true
			outcome.ErrorMsg = errMsg
			return outcome
		}
		inputs.Broadcaster.Send(JobEvent{
			JobID:     inputs.JobID,
			MovieID:   movie.ID,
			Phase:     jobEventPhaseApply,
			Step:      StepFailed,
			Message:   fmt.Sprintf("Apply failed: %v", applyErr),
			Timestamp: time.Now(),
		})
		// Broadcast per-file failure over WebSocket so the frontend's fileStatuses
		// map records the failure and OrganizeStatusCard can offer a Retry path.
		// Mirrors main's process_organize.go per-file 'failed' WS message.
		if cfg.OnFileFailed != nil {
			cfg.OnFileFailed(filePath, errMsg)
		}
		outcome.Failed = true
		outcome.ErrorMsg = errMsg
		if !cfg.OrganizeOptions.Skip || (result != nil && result.OrganizeResult != nil) {
			auditOrganizeFailure(inputs, movie, filePath, result, applyErr, cfg)
		}
		return outcome
	}

	if result != nil && result.Movie != nil {
		// codex r37 P2: lock BOTH identity keys — edits hold the matcher alias.
		aliasID, canonicalID := applyFamilyKeyIDs(afc)
		unlock := applyFamilyLock(inputs, aliasID, canonicalID)
		defer unlock()
		// codex P2-C/D: settled-rekey skip runs UNDER the family key.
		if !writebackPreSkipped(inputs.Updater, movie, filePath, "Apply") {
			if mid := result.Movie.ID; mid != "" && inputs.PromoteWitnessFn != nil && inputs.PromoteWitnessFn(mid) {
				// codex P2: an unresolved promote witness arbitrates at startup by
				// revision — this write-back bumps it, which would declare a
				// failed refresh committed and discard its recovery state.
				logging.Warnf("[Apply] skipping success write-back for %s — promote witness for %s unresolved; restart reconciles", filePath, mid)
			} else {
				err2 := inputs.Updater.AtomicUpdateFileResultWithProvenance(filePath, func(current *resultstore.MovieResult, prov *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
					if applyWritebackIdentityMismatch(movie, current) {
						logging.Warnf("[Apply] skipping success write-back for %s — result rekeyed to %s mid-phase", filePath, current.FileMatchInfo.MovieID)
						return current, prov, nil
					}
					current.Movie = mergeLiveReviewEdits(movie, result.Movie, current.Movie)
					// A successful explicit retry must clear the prior apply failure so
					// later retries and reloads do not keep treating this row as failed.
					if current.Status == models.JobStatusFailed {
						current.Status = models.JobStatusCompleted
						current.Error = ""
					}
					return current, mergeWriteBackProvenance(inputs.Provenance[filePath], prov), nil
				})
				if err2 != nil {
					logging.Warnf("Failed to update movie result for %s after apply: %v", filePath, err2)
				}
			}
		}
		outcome.Movie = result.Movie
	}

	inputs.Broadcaster.Send(JobEvent{
		JobID:     inputs.JobID,
		MovieID:   movie.ID,
		Phase:     jobEventPhaseApply,
		Step:      StepComplete,
		Progress:  1.0,
		Message:   fmt.Sprintf("Applied %s successfully", movie.ID),
		Timestamp: time.Now(),
	})
	// Broadcast per-file success over WebSocket so the frontend's fileStatuses
	// map populates per file and OrganizeStatusCard renders live per-file rows.
	// Mirrors main's process_organize.go per-file 'organized' WS message.
	if cfg.OnFileOrganized != nil {
		cfg.OnFileOrganized(filePath)
	}
	outcome.Success = true
	auditOrganizeSuccess(inputs, movie, filePath, result, cfg)
	return outcome
}

// applyFile handles the per-file apply logic: consume the prepared ApplyCmd,
// execute workflow.Apply, interpret result. Error handling, panic recovery,
// and result tracking are performed here. The command itself was built once
// per item, in sorted order, before fan-out (#240 finding A).
func applyFile(
	egCtx context.Context,
	wf workflow.WorkflowInterface,
	filePath string,
	fileResult *resultstore.MovieResult,
	movie *models.Movie,
	prepared *preparedApplyFile,
	inputs applyPhaseInputs,
	cfg ApplyPhaseConfig,
) (outcome applyFileOutcome) {
	startTime := time.Now()

	// Fire the per-file organize-start hook BEFORE any work begins on this file,
	// so the frontend's "Current Activity" card shows which file is being
	// organized (verbose organize progress). Nil-guarded; safe to call
	// concurrently from worker goroutines (the WS broadcaster is goroutine-safe).
	if cfg.OnFileOrganizeStart != nil {
		cfg.OnFileOrganizeStart(filePath)
	}

	// outcome is a NAMED return so the deferred withFileRecovery(rc, &outcome)
	// mutates the value the caller actually receives. With an unnamed return,
	// a recovered panic would leave the caller with the zero-value outcome
	// (Failed/Panic both false), so a panicking file would be counted as
	// neither organized nor failed by trackApplyResults — and the job would
	// wrongly MarkOrganized. Naming the return closes that hole: setPanic now
	// writes Failed/Panic onto the returned value.
	outcome = applyFileOutcome{
		FilePath: filePath,
		MovieID:  movie.ID,
		DryRun:   cfg.DryRun,
	}

	// The deterministic owner claim is primed before fan-out, but poster work
	// is not guaranteed to run for every item. Release it at the item boundary
	// as a final safety net; the downloader's reservation path remains the
	// fast-path release for normal poster completion.
	ownerLogicalKey := strings.ToLower(strings.TrimSpace(movie.ID))
	defer downloader.ReleaseDownloadOwnerClaim(inputs.Dedup, ownerLogicalKey, filePath)

	// codex P2 (PR #241) recover-path close-out: a primed duplicate owner
	// whose worker exits WITHOUT reaching the organizer's own settle/release
	// (panic in an earlier pipeline step, cancellation at Organize's entry,
	// or the recovered-panic replay below) must not hold its canonical key
	// open — waiting claimants promote instead of deadlocking behind a dead
	// owner. Settled claims are final and untouched, so every normally-
	// completed Organize is a no-op here. Runs during unwind (after the
	// recovery defer below) and on ordinary return alike.
	defer cfg.OrganizeOptions.DuplicateTracker.ReleaseAbandonedBy(prepared.cmd.Match.Path)

	rc := recoveryContext{
		filePath: filePath,
		// Preserve the existing FileMatchInfo (incl. IsMultiPart / PartNumber /
		// PartSuffix set by the earlier discovery/scrape phases) on the panic
		// path. Constructing a fresh struct here would silently zero multipart
		// metadata for any file that panicked mid-apply, so /review/[jobId]
		// would then show the file as single-part.
		fmi:              fileResult.FileMatchInfo,
		movie:            fileResult.Movie,
		provenance:       inputs.Provenance[filePath],
		updater:          inputs.Updater,
		broadcast:        broadcastFailure(inputs.Broadcaster, inputs.JobID, movie.ID, jobEventPhaseApply, "Apply"),
		startTime:        startTime,
		editLockFn:       inputs.EditLockFn,
		promoteWitnessFn: inputs.PromoteWitnessFn,
	}
	defer withFileRecovery(rc, &outcome)()

	applyTimeout := inputs.Concurrency.WorkerTimeout
	taskCtx := egCtx
	var taskCancel context.CancelFunc
	if applyTimeout > 0 {
		// codex P2 (PR #241 F5): the budget clock starts at THIS worker
		// task's start — sibling preparation and priming, and fan-out queue
		// time are all invisible — debited only by the file's OWN
		// preparation spend (F4: hook + apply stay inside ONE WorkerTimeout;
		// a hook that burned it buys no second full grant for apply).
		// codex P2 (PR #241 F1): the file's OWN priming spend joins the
		// debit — priming ran under the same per-file budget, so apply sees
		// hook + priming + apply bounded by ONE WorkerTimeout, with sibling
		// priming still invisible. Because an uncooperative hook (or a
		// ctx-ignoring planner) can overshoot its deadline, the raw
		// remainder can go negative — a deadline BEFORE task start, i.e.
		// time travel — so it clamps at applyBudgetFloor: never negative,
		// and the zero duration cancels the derived context synchronously
		// (see the constant's contract).
		remaining := applyTimeout - (prepared.hookElapsed + prepared.primingElapsed)
		if remaining < applyBudgetFloor {
			remaining = applyBudgetFloor
		}
		taskCtx, taskCancel = context.WithDeadline(egCtx, startTime.Add(remaining))
		defer taskCancel()
	}

	// Step 1: Consume the prepared ApplyCmd (built pre-fan-out with the
	// phase-entry baseline frozen before any hook mutation, codex r51 P2c).
	// codex r2 P2: a hook panic during priming was already recovered,
	// recorded, and broadcast there — replay the pre-recorded failure
	// outcome instead of re-executing (once-per-file hook contract).
	if prepared.hookOutcome != nil {
		return *prepared.hookOutcome
	}
	if !prepared.execute {
		return outcome
	}
	applyCmd, afc := prepared.cmd, prepared.afc

	// Step 2: Execute the workflow.Apply.
	reporter := makeProgressReporter(inputs.Broadcaster, inputs.JobID, movie.ID, jobEventPhaseApply)
	// Inject the reporter into taskCtx (which carries the worker timeout /
	// errgroup cancellation) so downstream emitters resolve it via
	// progress.FromContext. Use taskCtx, not the parent egCtx.
	taskCtx = progress.WithReporter(taskCtx, reporter)

	result, applyErr := wf.Apply(taskCtx, applyCmd)

	// Step 3: Interpret the result against the FROZEN baseline (workflow
	// permutations may have rewritten fields on the live pointer mid-apply).
	return interpretApplyResult(filePath, prepared.baseline, startTime, applyTimeout, inputs, cfg, taskCtx, afc, result, applyErr)
}

// trackApplyResults processes collected applyFileOutcomes: increments counters
// for organized/failed. The actual Updater/Broadcaster calls are already done
// inside applyFile; this function only handles the aggregate counters.
func trackApplyResults(inputs applyPhaseInputs, outcomes []applyFileOutcome, organized *int64, failed *int64) {
	for _, o := range outcomes {
		if o.Success {
			atomic.AddInt64(organized, 1)
		}
		if o.Failed || o.Panic {
			atomic.AddInt64(failed, 1)
		}
		if o.Panic && !o.Cancelled && !inputs.OrganizeSkipped {
			auditOrganizePanic(inputs, o)
		}
	}
}
