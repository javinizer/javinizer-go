//nolint:errcheck
package commandutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// noAuditIDLine is the single console sentence for runs with NO persisted
// jobs row (#248 codex P2, R2): dry runs (a preview persists no queryable
// batch identity) and live runs that exit during scan (zero files / zero
// matched IDs) before the runtime ever persists a jobs row. Both branches
// render the IDENTICAL line so `history list --batch <id>` can never be
// pointed at an id that would answer 'batch job not found'.
const noAuditIDLine = "Preview (not persisted; no audit ID)"

// ErrJobPersistenceFailed marks failure of the initial jobs-row persist that
// binds a live batch's audit identity (#248 codex P2). The store itself keeps
// best-effort swallow-and-log semantics for the API path (in-flight jobs must
// not die on a transient persist failure — a later persist self-heals and
// persist_error rides the job status payload), but a one-shot CLI batch turns
// the failure into a batch STARTUP error: RunBatchCommand returns non-nil,
// the console lands on the NOT-persisted audit line, and no unqueryable
// `history list --batch <id>` pointer is advertised. Match via errors.Is.
var ErrJobPersistenceFailed = errors.New("batch job audit persistence failed")

// BatchCommandPresenter abstracts CLI presentation for batch commands.
// Per W-5: extracted from RunBatchCommand so that the orchestration logic is
// decoupled from presentation. Default presenter prints to stdout;
// tests can use a silent presenter.
type BatchCommandPresenter interface {
	// OnHeader prints the command header (source, destination, mode, etc.).
	// The header renders NO audit identity: at header time (pre-scan) whether
	// a jobs row will exist is still unknown (#248 codex P2, R2) —
	// OnAuditID owns every audit-identity sentence.
	OnHeader(w io.Writer, opts BatchCommandOptions)
	// OnAuditID prints the run's audit-trail identity line, called only once
	// the persistence truth is known (#248 codex P2, R2): persisted=true
	// renders the queryable batch id for live runs, strictly AFTER the
	// runtime persisted the jobs row; persisted=false renders the 'no audit
	// trail' sentence for runs that exit before anything was persisted (live
	// early-exit during scan), identical to the dry-run header preview label.
	OnAuditID(w io.Writer, opts BatchCommandOptions, persisted bool)
	// OnScanStart prints the scan-started message.
	OnScanStart(w io.Writer)
	// OnNoFiles prints the no-files-found message.
	OnNoFiles(w io.Writer)
	// OnProcessingStart prints the processing-started message.
	OnProcessingStart(w io.Writer, actionVerb string)
	// OnSummary prints the final summary.
	OnSummary(w io.Writer, opts BatchCommandOptions, result BatchCommandResult)
}

// defaultBatchCommandPresenter prints CLI output to the given writer.
type defaultBatchCommandPresenter struct{}

func (p *defaultBatchCommandPresenter) OnHeader(w io.Writer, opts BatchCommandOptions) {
	fmt.Fprintf(w, "=== %s ===\n", opts.CommandLabel)
	fmt.Fprintf(w, "Source: %s\n", opts.SourcePath)
	fmt.Fprintf(w, "Destination: %s\n", opts.Destination)
	fmt.Fprintf(w, "Mode: %s\n", map[bool]string{true: "DRY RUN", false: "LIVE"}[opts.DryRun])
	if opts.BatchJobID != "" && opts.DryRun {
		// Truthful rendering (#248 codex P2): a dry run intentionally
		// persists no jobs row (NoopJobPersistence — #245's invariant
		// stands), so the pre-generated id is NOT queryable:
		// `history list --batch <id>` 404s with 'batch job not found'.
		// Print a preview label instead of a queryable-looking audit
		// ID; dry-run history rows stay reachable via plain
		// `history list` under its Dry Run marker.
		// A LIVE header prints no identity at all (#248 codex P2, R2):
		// whether a jobs row will exist is only known once the runtime is
		// constructed post-scan (the early-exit scan legs persist nothing),
		// so the identity line defers to OnAuditID.
		fmt.Fprintln(w, noAuditIDLine)
	}
	if opts.OperationLabel != "" {
		fmt.Fprintf(w, "Operation: %s\n", opts.OperationLabel)
	}
	if opts.GenerateNFO {
		fmt.Fprintf(w, "Generate NFO: %v\n", opts.GenerateNFO)
	}
	fmt.Fprintf(w, "Download Media: %v\n\n", opts.DownloadMedia)
}

// OnAuditID renders the audit identity once persistence is known (see the
// interface contract): live runs get the queryable id strictly post-persist;
// every unpersisted branch (live scan early-exit) gets the identical
// dry-run no-audit sentence, so no console output ever names a batch id
// whose jobs row does not exist.
func (p *defaultBatchCommandPresenter) OnAuditID(w io.Writer, opts BatchCommandOptions, persisted bool) {
	if persisted && opts.BatchJobID != "" {
		fmt.Fprintf(w, "Batch Job: %s\n", opts.BatchJobID)
		return
	}
	fmt.Fprintln(w, noAuditIDLine)
}

func (p *defaultBatchCommandPresenter) OnScanStart(w io.Writer) {
	fmt.Fprintln(w, "📂 Scanning for video files...")
}

func (p *defaultBatchCommandPresenter) OnNoFiles(w io.Writer) {
	fmt.Fprintln(w, "\n✅ No files to process")
}

func (p *defaultBatchCommandPresenter) OnProcessingStart(w io.Writer, actionVerb string) {
	fmt.Fprintf(w, "\n🌐 %s...\n", actionVerb)
}

func (p *defaultBatchCommandPresenter) OnSummary(w io.Writer, opts BatchCommandOptions, result BatchCommandResult) {
	defaultSummaryPrinter(w, opts, result)
}

// SilentBatchCommandPresenter is a no-op presenter for tests that don't
// want CLI output.
type SilentBatchCommandPresenter struct{}

// OnHeader implements BatchCommandPresenter.OnHeader as a no-op.
func (p *SilentBatchCommandPresenter) OnHeader(_ io.Writer, _ BatchCommandOptions) {}

// OnAuditID implements BatchCommandPresenter.OnAuditID as a no-op.
func (p *SilentBatchCommandPresenter) OnAuditID(_ io.Writer, _ BatchCommandOptions, _ bool) {}

// OnScanStart implements BatchCommandPresenter.OnScanStart as a no-op.
func (p *SilentBatchCommandPresenter) OnScanStart(_ io.Writer) {}

// OnNoFiles implements BatchCommandPresenter.OnNoFiles as a no-op.
func (p *SilentBatchCommandPresenter) OnNoFiles(_ io.Writer) {}

// OnProcessingStart implements BatchCommandPresenter.OnProcessingStart as a no-op.
func (p *SilentBatchCommandPresenter) OnProcessingStart(_ io.Writer, _ string) {}

// OnSummary implements BatchCommandPresenter.OnSummary as a no-op.
func (p *SilentBatchCommandPresenter) OnSummary(_ io.Writer, _ BatchCommandOptions, _ BatchCommandResult) {
}

// BatchCommandOptions holds all options for the shared batch command scaffold.
// Both sort and update commands construct this from their flags and pass it to
// RunBatchCommand, eliminating the duplicated config → bootstrap → scan →
// BatchJob → events → summary pipeline.
type BatchCommandOptions struct {
	// Config file path (empty string triggers default config creation)
	ConfigFile string

	// Source and destination
	SourcePath  string
	Destination string // Empty = same as source (sort resolves; update ignores)
	Recursive   bool

	// Operation modes
	DryRun              bool
	DownloadMedia       bool
	DownloadExtrafanart bool

	// Sort-specific
	MoveFiles   bool
	GenerateNFO bool
	ForceUpdate bool

	// Scrape options
	ScraperPriority []string
	ForceRefresh    bool

	// Update-specific merge options
	SkipOrganize           bool
	ForceOverwrite         bool
	PreserveNFO            bool
	OverwriteExistingMedia bool

	// Resolved seam strings (caller must resolve before calling)
	Resolved *workflow.ResolvedSeamStrings

	// BatchJobID is populated by RunBatchCommand before presentation. A
	// live run's id identifies the persisted batch (audit rows, jobs row)
	// so `javinizer history list --batch <id>` can drill into the run —
	// printed via OnAuditID strictly AFTER the runtime persisted the jobs
	// row; dry runs and live scan early-exits persist nothing, so the
	// presenter renders the non-queryable no-audit sentence instead of the
	// id (#248 codex P2, R2). Callers must leave it empty.
	BatchJobID string

	// Header label printed at the start (e.g., "Javinizer Sort" or "Javinizer Update")
	CommandLabel string
	// Operation label for the header (e.g., "COPY", "MOVE", "HARDLINK")
	OperationLabel string
	// Action verb for progress message (e.g., "Processing files" or "Updating metadata")
	ActionVerb string
	// Completion message suffix (e.g., "Sort complete!" or "Update complete!")
	CompletionMessage string
	// Mode line printed in summary (e.g., "Update (metadata & artwork, files remain in place)")
	ModeLine string

	// Optional: custom event handler. If nil, the default handler that prints
	// ❌ for failures and ✅ for completions is used.
	EventHandler func(w io.Writer, event worker.JobEvent)
	// Optional: custom summary printer. If nil, the default summary is printed.
	// Deprecated: use Presenter instead. Kept for backward compatibility; if both
	// are set, SummaryPrinter takes precedence for OnSummary.
	SummaryPrinter func(w io.Writer, opts BatchCommandOptions, result BatchCommandResult)
	// Presenter handles CLI presentation lifecycle. If nil, the default
	// presenter that prints to stdout is used. Per W-5.
	Presenter BatchCommandPresenter
}

// BatchCommandResult holds the results from a batch command run.
type BatchCommandResult struct {
	ScanResult   *workflow.ScanAndMatchResult
	FilePaths    []string
	MatchedCount int
	UniqueIDs    map[string]bool
	Movies       map[string]*models.Movie
	SuccessCount int
	FailedCount  int
	// SkippedDuplicates counts files whose apply succeeded as an authorized
	// intra-batch duplicate skip (no bytes moved). Reported separately so the
	// console summary tells the same truth as the persisted audit rows.
	SkippedDuplicates int
}

// RunBatchCommand executes the shared scaffold: config load → prepare → bootstrap →
// scan → BatchJob → events → summary. Both sort and update commands delegate to
// this after constructing a BatchCommandOptions from their CLI flags.
//
// Per W-5: CLI presentation is delegated to a BatchCommandPresenter, making the
// orchestration logic testable without stdout side effects.
func RunBatchCommand(ctx context.Context, w io.Writer, opts BatchCommandOptions) error {
	// Resolve presenter: use injected or default.
	presenter := opts.Presenter
	if presenter == nil {
		presenter = &defaultBatchCommandPresenter{}
	}

	// Load configuration
	cfg, err := config.LoadOrCreate(opts.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	config.ApplyEnvironmentOverrides(cfg)

	// Override config with flag if extrafanart is explicitly enabled
	if opts.DownloadExtrafanart {
		cfg.Output.Download.DownloadExtrafanart = true
	}

	if _, err := config.Prepare(cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Pre-generate the batch job ID up front (mirrors the API organize
	// usecase): it binds the per-job workflow's revert ledger, the persisted
	// jobs row, and every history/eventlog audit row to ONE identity.
	// Previously the CLI ran with no persisted identity, so
	// batch_file_operations rows landed under "" and no history/event rows
	// were written at all (#244).
	opts.BatchJobID = models.NewJobID().String()

	bs, err := Bootstrap(cfg)
	if err != nil {
		return fmt.Errorf("failed to bootstrap: %w", err)
	}
	defer func() { _ = bs.Close() }()

	// Print header via presenter
	presenter.OnHeader(w, opts)

	// Step 1 & 2: Scan and match via Workflow seam
	presenter.OnScanStart(w)
	scanResult, err := bs.Workflow.ScanAndMatch(ctx, workflow.ScanAndMatchCmd{
		Directory: opts.SourcePath,
		Recursive: opts.Recursive,
	})
	if err != nil {
		return err
	}

	if len(scanResult.Files) == 0 {
		presenter.OnNoFiles(w)
		// Live early-exit (#248 codex P2, R2): the runtime is never
		// constructed and NO jobs row exists, so offer no queryable batch id
		// (the pre-scan header printed none) — print the identical no-audit
		// sentence dry runs use. Dry runs already labelled the preview in
		// the header; do not repeat it.
		if !opts.DryRun {
			presenter.OnAuditID(w, opts, false)
		}
		return nil
	}

	// Extract file paths with matched IDs, keeping the per-file match metadata.
	// ScanAndMatch derives IsMultiPart / PartNumber / PartSuffix from the
	// directory listing; the scrape + apply phases plan part-suffixed targets
	// (<PARTSUFFIX> / multipart NFO + media layout) from FileMatchInfo, so the
	// map must reach the job below — discarding it collapses every part of a
	// multi-part file onto the same destination. Mirrors the API organize
	// usecase, which seeds ScrapePhaseConfig.FileMatchInfo from
	// discoverSiblingPartsWithMetadata (internal/api/batch/usecases.go).
	filePaths := make([]string, 0)
	uniqueIDs := make(map[string]bool)
	matchInfo := make(map[string]models.FileMatchInfo, len(scanResult.Files))
	for _, fmi := range scanResult.Files {
		if fmi.MovieID != "" {
			filePaths = append(filePaths, fmi.Path)
			uniqueIDs[fmi.MovieID] = true
			matchInfo[fmi.Path] = fmi
		}
	}
	if len(filePaths) == 0 {
		// Same live early-exit leg: scan found video files but no matched
		// IDs, nothing was persisted — no queryable audit id is offered
		// (#248 codex P2, R2).
		if !opts.DryRun {
			presenter.OnAuditID(w, opts, false)
		}
		return nil
	}

	matchedCount := len(filePaths)

	// Step 3+: Process via BatchJob
	presenter.OnProcessingStart(w, opts.ActionVerb)

	// Create BatchJob using the BatchJobFactory seam.
	// Per NEW-1: the factory owns infrastructure deps (WF, Matcher, PosterGen, BatchCfg)
	// so the CLI only provides per-call varying fields.
	batchCfg := BatchJobConfigFromAppConfig(cfg)
	rt, err := newCLIBatchRuntimeFn(bs, cfg, opts, batchCfg, filePaths)
	if err != nil {
		if errors.Is(err, ErrJobPersistenceFailed) {
			// #248 codex P2: the initial jobs-row persist failed, so the
			// pre-generated BatchJobID names NOTHING durable — land on the
			// NOT-persisted branch (never advertise the unqueryable id) and
			// say setup failed instead of printing a normal per-file summary.
			// Live runs only print the audit line: a dry run persists nothing
			// BY DESIGN (NoopJobPersistence) and the header already carried
			// the preview label.
			if !opts.DryRun {
				presenter.OnAuditID(w, opts, false)
			}
			fmt.Fprintf(w, "\n❌ Batch setup failed: %v\n", err)
		}
		return fmt.Errorf("failed to create batch job runtime: %w", err)
	}
	job := rt.job
	factory := rt.factory

	// Print the queryable batch identity ONLY post-persist (#248 codex P2,
	// R2): CreatePersistentStandaloneJob writes the jobs row at creation,
	// so from here on `history list --batch <id>` resolves — printing it
	// any earlier named an id the scan early-exit legs never persisted.
	// Dry runs persist nothing (NoopJobPersistence) and keep the header's
	// preview label instead.
	if !opts.DryRun {
		presenter.OnAuditID(w, opts, true)
	}

	// Validate the resolved seam strings before dereferencing them below; a
	// missing resolution step would otherwise panic when building applyOpts.
	if opts.Resolved == nil {
		return fmt.Errorf("batch command requires resolved seam strings: opts.Resolved is nil")
	}

	// Set run options using the shared CLIApplyOptions helper
	applyOpts := CLIApplyOptions{
		DryRun:                 opts.DryRun,
		MoveFiles:              opts.MoveFiles,
		LinkMode:               opts.Resolved.LinkMode,
		ForceUpdate:            opts.ForceUpdate,
		SkipOrganize:           opts.SkipOrganize,
		GenerateNFO:            opts.GenerateNFO,
		Download:               opts.DownloadMedia,
		OverwriteExistingMedia: opts.OverwriteExistingMedia,
		Destination:            opts.Destination,
		MergeOptions: workflow.MergeOptions{
			ForceOverwrite: opts.ForceOverwrite,
			PreserveNFO:    opts.PreserveNFO,
			ScalarStrategy: opts.Resolved.ScalarStrategy,
			ArrayStrategy:  opts.Resolved.ArrayStrategy,
		},
	}
	scrapeCfg := factory.NewScrapeConfig(opts.ScraperPriority, false, opts.ForceRefresh)
	// Carry the scan/match metadata into the job: StartScrape seeds the
	// result store from ScrapePhaseConfig.FileMatchInfo, and buildApplyCmd
	// reads PartSuffix/PartNumber/IsMultiPart back out of the per-file result
	// when planning organize destinations.
	scrapeCfg.FileMatchInfo = matchInfo
	applyCfg := applyOpts.ToApplyPhaseConfig()
	// Audit hook (#244): persist per-file organize/update events (incl.
	// authorized duplicate-skip warnings) to the eventlog and print skip
	// warnings to the console, keeping CLI output in sync with the persisted
	// audit truth. opts.SkipOrganize selects the update-mode event taxonomy
	// (#248 codex P2, F3).
	skipCount := &atomic.Int64{}
	printMu := &sync.Mutex{}
	applyCfg.PostApplyFunc = cliBatchPostApply(rt.emitter, w, opts.BatchJobID, opts.DryRun, opts.SkipOrganize, skipCount, printMu)
	job.SetRunOptions(scrapeCfg, applyCfg)

	// Subscribe to events for progress printing
	subscriber := job.Subscribe()
	defer subscriber.Close()

	// Start event reader goroutine
	doneReading := make(chan struct{})
	eventHandler := opts.EventHandler
	if eventHandler == nil {
		eventHandler = defaultEventHandler
	}
	go func() {
		defer close(doneReading)
		for event := range subscriber.Events() {
			if event.Message != "" {
				// Serialize with PostApplyFunc warning prints (worker goroutines)
				// so console lines never interleave mid-line.
				printMu.Lock()
				eventHandler(w, event)
				printMu.Unlock()
			}
		}
	}()

	// Run the batch job
	runErr := job.Run(ctx)

	// Wait for event reader to drain
	<-doneReading

	if runErr != nil {
		return runErr
	}

	// Process results
	movies := make(map[string]*models.Movie, len(uniqueIDs))
	results := job.GetResults()
	successCount := 0
	for _, r := range results {
		if r.Status == models.JobStatusCompleted && r.Movie != nil {
			movies[r.FileMatchInfo.MovieID] = r.Movie
			successCount++
		}
	}
	// Compute failures from the number of processed file results (not unique
	// IDs): duplicate files sharing a MovieID would otherwise make the count
	// inaccurate since SuccessCount is per-file.
	failedCount := len(results) - successCount
	if failedCount < 0 {
		failedCount = 0
	}

	batchResult := BatchCommandResult{
		ScanResult:        scanResult,
		FilePaths:         filePaths,
		MatchedCount:      matchedCount,
		UniqueIDs:         uniqueIDs,
		Movies:            movies,
		SuccessCount:      successCount,
		FailedCount:       failedCount,
		SkippedDuplicates: int(skipCount.Load()),
	}

	// Print summary — backward-compatible: if SummaryPrinter is set, use it;
	// otherwise delegate to the presenter.
	if opts.SummaryPrinter != nil {
		opts.SummaryPrinter(w, opts, batchResult)
	} else {
		presenter.OnSummary(w, opts, batchResult)
	}

	return nil
}

// cliBatchRuntime bundles the per-run batch infrastructure for sort/update:
// the DB-backed job store (jobs row + phase envelope persists), the eventlog
// emitter (audit entries), the shared BatchJobFactory seam, and the runnable
// job itself.
type cliBatchRuntime struct {
	job     worker.StandaloneJob
	factory worker.BatchJobFactoryInterface
	emitter eventlog.EventEmitter
}

// newCLIBatchRuntimeFn is the seam RunBatchCommand uses to build the batch
// runtime; tests replace it to exercise the runtime-construction error branch.
var newCLIBatchRuntimeFn = newCLIBatchRuntime

// newCLIBatchRuntime constructs the CLI batch runtime with API-parity audit
// persistence (#244): a real JobStore over the app database (startup recovery
// skipped — a one-shot CLI process must not reconcile jobs a live server may
// own), the eventlog emitter, a per-job workflow whose revert ledger binds
// the pre-generated batch job ID, and the runnable job built through the
// shared factory seam.
func newCLIBatchRuntime(bs *bootstrapResult, cfg *config.Config, opts BatchCommandOptions, batchCfg worker.BatchJobConfig, filePaths []string) (*cliBatchRuntime, error) {
	jobWF, err := bs.NewJobWorkflow(opts.BatchJobID)
	if err != nil {
		return nil, err
	}
	repos := bs.DB.Repositories()
	// #248 codex P2: observe the create-time jobs-row persist failure the
	// store otherwise only LOGS (its best-effort semantics stand for the API
	// path — in-flight jobs must not die on a transient persist failure), so
	// the post-create check below converts it into an ErrJobPersistenceFailed
	// startup error rather than advertising an unqueryable batch id.
	var initialPersistErr error
	storeOpts := []worker.JobStoreOption{
		worker.WithHistoryRepo(repos.HistoryRepo),
		worker.WithSkipStartupRecovery(),
		worker.WithInitialPersistErrorReporter(func(err error) { initialPersistErr = err }),
	}
	if opts.DryRun {
		// A dry run previews work: no jobs row and no envelope persists (the
		// apply ledger already skips dry run) — while history rows still land
		// with their dry_run flag so previews remain visible in the audit.
		storeOpts = append(storeOpts, worker.WithPersistence(worker.NewNoopJobPersistence()))
	}
	jobStore := worker.NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo, cfg.System.TempDir, nil, nil, storeOpts...)
	emitter := eventlog.NewEmitter(repos.EventRepo)
	factory := worker.NewBatchJobFactory(jobStore, jobWF, bs.Matcher, bs.PosterGen, batchCfg, emitter)
	// Persisted job identity at creation (#248 codex P2, F1): mirror the API
	// StartScrapeUseCase wiring so a CLI update batch's jobs row classifies as
	// update (update=true + metadata-artwork) instead of organize. StartApply
	// re-commits the same mapping from ApplyPhaseConfig — persistedJobMode is
	// the single mapping source.
	persistUpdate, persistMode := persistedJobMode(opts.SkipOrganize)
	job := factory.CreatePersistentStandaloneJob(filePaths, worker.BatchJobOptions{
		ID:                    opts.BatchJobID,
		Destination:           opts.Destination,
		OperationModeOverride: persistMode,
		Update:                persistUpdate,
		WF:                    jobWF,
	})
	// #248 codex P2: fail startup on the initial jobs-row persist failure —
	// pre-fix, the store only logged it while OnAuditID(..., true) advertised
	// the never-persisted id and the batch pressed into phases whose audit
	// trail could never land. A dry run's NoopJobPersistence never fails, so
	// previews keep their not-persisted-BY-DESIGN handling.
	if initialPersistErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrJobPersistenceFailed, initialPersistErr)
	}
	return &cliBatchRuntime{job: job, factory: factory, emitter: emitter}, nil
}

// cliBatchPostApply returns the apply-phase per-file hook for CLI batches. It
// mirrors the API resolvers' audit emission
// (internal/api/batch/apply_config_builder.go): organize flows persist an
// "Organized"/"Organize failed" event (source file_move) plus one warning
// event per OrganizeResult.Warnings entry — authorized duplicate skips land
// there and were previously invisible from the CLI because no emitter
// existed (#244). updateMode (javinizer update: files stay in place) takes
// the API update resolver's taxonomy instead (#248 codex P2, F3): the
// nfo_gen source with "Updated"/"Update failed" vocabulary, and NO file_move
// events — an in-place metadata refresh is not a file move, and the previous
// file_move/"Organized <id>" success event carried an empty new_path that
// misrepresented the audit trail.
// Warning text is also printed to the console so CLI output tells the same
// truth as the persisted audit rows. Skip accounting + that console warning
// surface run for DRY RUNS identically to live (#248 codex P2 dry-run leg):
// the organizer's dry-run skip result carries DuplicateSkipped exactly like
// the live leg, so the summary's organized totals reuse the live arithmetic
// (completed minus SkippedDuplicates) and the authorized duplicate warning is
// the ONLY place a preview tells the operator a file would be skipped —
// previews persist nothing, but they must SAY it. Eventlog emission stays
// skipped for dry runs: previews are not operations.
//
// Audit emission is deliberately DETACHED from the per-file task ctx
// (#248 codex P2, F2): when apply exhausts WorkerTimeout, interpretApplyResult
// invokes this hook with the task ctx ALREADY canceled, and the eventlog
// emitter drops events on a canceled ctx (eventlog.emit checks ctx.Err()) —
// the timeout failure previously never landed in the CLI eventlog, while the
// worker's history writer still recorded it because it audits with a fresh
// bounded ctx (worker/history_writer.go historyAuditContext). Mirror that
// pattern here, and log loudly if emission STILL fails instead of discarding
// the error.
func cliBatchPostApply(emitter eventlog.EventEmitter, w io.Writer, jobID string, dryRun, updateMode bool, skipCount *atomic.Int64, printMu *sync.Mutex) func(context.Context, *worker.ApplyFileContext, *worker.ApplyFileResult) {
	return func(_ context.Context, afc *worker.ApplyFileContext, afr *worker.ApplyFileResult) {
		// Guard matches the API resolver: never deref a nil payload; the hook
		// must not mask the original apply outcome with a panic.
		if afc == nil || afc.Movie == nil || afr == nil {
			return
		}
		// Skip accounting + warning print run before the dry-run gate (see
		// the doc comment); a failed apply keeps failures' single surface —
		// no warning double-print on the error path.
		var warnings []string
		if afr.Err == nil && afr.Result != nil && afr.Result.OrganizeResult != nil {
			warnings = afr.Result.OrganizeResult.Warnings
			if afr.Result.OrganizeResult.DuplicateSkipped {
				skipCount.Add(1)
			}
		}
		if len(warnings) > 0 {
			// Serialize with the event-handler prints (worker goroutines) so
			// console lines never interleave mid-line.
			printMu.Lock()
			for _, warning := range warnings {
				fmt.Fprintf(w, "   ⚠️  %s: %s\n", filepath.Base(afc.FilePath), warning)
			}
			printMu.Unlock()
		}
		if dryRun {
			return
		}
		auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		emit := func(source, message string, severity models.EventSeverity, eventCtx map[string]any) {
			if err := emitter.EmitOrganizeEvent(auditCtx, source, message, severity, eventCtx); err != nil {
				logging.Warnf("[cli batch %s] eventlog audit emission failed (source=%s, severity=%s, message=%q): %v", jobID, source, severity, message, err)
			}
		}
		// Event taxonomy by operation mode (see the doc comment): file_move
		// organize vocabulary vs nfo_gen update vocabulary.
		source := "file_move"
		failureVerb := "Organize failed"
		successVerb := "Organized"
		warningVerb := "Organize warning"
		if updateMode {
			source = "nfo_gen"
			failureVerb = "Update failed"
			successVerb = "Updated"
			warningVerb = "Update warning"
		}
		if afr.Err != nil {
			emit(source, fmt.Sprintf("%s for %s", failureVerb, afc.Movie.ID), models.SeverityError, map[string]any{"job_id": jobID, "movie_id": afc.Movie.ID, "error": afr.Err.Error()})
			return
		}
		var newPath string
		if afr.Result != nil && afr.Result.OrganizeResult != nil {
			newPath = afr.Result.OrganizeResult.NewPath
		}
		eventCtx := map[string]any{"job_id": jobID, "movie_id": afc.Movie.ID, "file": afc.FilePath}
		if !updateMode {
			eventCtx["new_path"] = newPath
		}
		emit(source, fmt.Sprintf("%s %s", successVerb, afc.Movie.ID), models.SeverityInfo, eventCtx)
		for _, warning := range warnings {
			warnCtx := map[string]any{"job_id": jobID, "movie_id": afc.Movie.ID, "file": afc.FilePath, "warning": warning}
			if !updateMode {
				warnCtx["new_path"] = newPath
			}
			emit(source, fmt.Sprintf("%s for %s: %s", warningVerb, afc.Movie.ID, warning), models.SeverityWarn, warnCtx)
		}
	}
}

// defaultEventHandler prints ❌ for failures and ✅ for completions.
func defaultEventHandler(w io.Writer, event worker.JobEvent) {
	if event.Step == worker.StepFailed {
		fmt.Fprintf(w, "   ❌ %s\n", event.Message)
	} else if event.Step == worker.StepComplete {
		fmt.Fprintf(w, "   ✅ %s\n", event.Message)
	} else if event.Message != "" {
		logging.Debugf("[%s] %s: %s", event.MovieID, event.Step, event.Message)
	}
}

// defaultSummaryPrinter prints the standard summary for a batch command.
func defaultSummaryPrinter(w io.Writer, opts BatchCommandOptions, result BatchCommandResult) {
	successCount := result.SuccessCount
	// PR #248 codex P2 (F2): a completed authorized duplicate skip lands
	// JobStatusCompleted but moves NO bytes (the loser's row finalizes
	// completed-noop), so organize/NFO success totals must exclude skips —
	// completed minus SkippedDuplicates. Subtraction lives ONLY here at the
	// summary aggregation site: "Metadata found" keeps counting every
	// completed file (the loser WAS scraped and matched), while organize/output
	// claims match the real file movement (1 winner moved must never read
	// "Organized 2 file(s)"). Skips are organize-mode only, so the update-mode
	// lines (driven by len(Movies) above and this subtraction being zero when
	// no skips occurred) are unchanged.
	organizedCount := successCount - result.SkippedDuplicates
	if organizedCount < 0 {
		organizedCount = 0
	}

	if opts.SkipOrganize {
		// Update-style summary
		fmt.Fprintf(w, "   Updated: %d, Failed: %d\n", len(result.Movies), result.FailedCount)
	} else {
		// Sort-style summary
		if opts.DryRun {
			fmt.Fprintf(w, "\n   Would organize %d file(s)\n", organizedCount)
		} else {
			fmt.Fprintf(w, "\n   Organized %d file(s)\n", organizedCount)
		}
	}

	// Summary
	fmt.Fprintln(w, "\n=== Summary ===")
	fmt.Fprintf(w, "Files scanned: %d\n", len(result.ScanResult.Files))
	fmt.Fprintf(w, "IDs matched: %d\n", result.MatchedCount)
	fmt.Fprintf(w, "Metadata found: %d\n", successCount)
	if opts.GenerateNFO {
		fmt.Fprintf(w, "NFOs generated: %s\n", map[bool]string{true: fmt.Sprintf("%d (dry-run)", organizedCount), false: fmt.Sprintf("%d", organizedCount)}[opts.DryRun])
	}
	if !opts.SkipOrganize {
		fmt.Fprintf(w, "Files organized: %s\n", map[bool]string{true: fmt.Sprintf("%d (dry-run)", organizedCount), false: fmt.Sprintf("%d", organizedCount)}[opts.DryRun])
	}
	if result.SkippedDuplicates > 0 {
		// An authorized intra-batch duplicate applies as a successful skip —
		// the console says so explicitly instead of counting the file as
		// organized, matching its persisted noop audit row (#244).
		fmt.Fprintf(w, "Skipped (authorized duplicates): %d\n", result.SkippedDuplicates)
	}
	if opts.ModeLine != "" {
		fmt.Fprintf(w, "Mode: %s\n", opts.ModeLine)
	}

	if opts.DryRun {
		fmt.Fprintln(w, "\n💡 Run without --dry-run to apply changes")
	} else {
		completion := opts.CompletionMessage
		if completion == "" {
			completion = "Complete!"
		}
		fmt.Fprintf(w, "\n✅ %s\n", completion)
	}
}

// UpdateEventHandler prints events with update-specific formatting
// (shows "(scraped)" for scrape-phase completions).
func UpdateEventHandler(w io.Writer, event worker.JobEvent) {
	if event.Step == worker.StepFailed {
		fmt.Fprintf(w, "   ❌ %s\n", event.Message)
	} else if event.Step == worker.StepComplete && event.Phase == worker.JobEventPhaseScrape {
		fmt.Fprintf(w, "   %s... ✅ (scraped)\n", event.MovieID)
	}
}
