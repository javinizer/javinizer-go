//nolint:errcheck
package commandutil

import (
	"context"
	"fmt"
	"io"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// BatchCommandPresenter abstracts CLI presentation for batch commands.
// Per W-5: extracted from RunBatchCommand so that the orchestration logic is
// decoupled from presentation. Default presenter prints to stdout;
// tests can use a silent presenter.
type BatchCommandPresenter interface {
	// OnHeader prints the command header (source, destination, mode, etc.).
	OnHeader(w io.Writer, opts BatchCommandOptions)
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
	if opts.OperationLabel != "" {
		fmt.Fprintf(w, "Operation: %s\n", opts.OperationLabel)
	}
	if opts.GenerateNFO {
		fmt.Fprintf(w, "Generate NFO: %v\n", opts.GenerateNFO)
	}
	fmt.Fprintf(w, "Download Media: %v\n\n", opts.DownloadMedia)
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

func (p *SilentBatchCommandPresenter) OnHeader(_ io.Writer, _ BatchCommandOptions) {}
func (p *SilentBatchCommandPresenter) OnScanStart(_ io.Writer)                     {}
func (p *SilentBatchCommandPresenter) OnNoFiles(_ io.Writer)                       {}
func (p *SilentBatchCommandPresenter) OnProcessingStart(_ io.Writer, _ string)     {}
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
	SkipOrganize   bool
	ForceOverwrite bool
	PreserveNFO    bool

	// Resolved seam strings (caller must resolve before calling)
	Resolved *workflow.ResolvedSeamStrings

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
		return nil
	}

	// Extract file paths with matched IDs
	filePaths := make([]string, 0)
	uniqueIDs := make(map[string]bool)
	for _, fmi := range scanResult.Files {
		if fmi.MovieID != "" {
			filePaths = append(filePaths, fmi.Path)
			uniqueIDs[fmi.MovieID] = true
		}
	}
	if len(filePaths) == 0 {
		return nil
	}

	matchedCount := len(filePaths)

	// Step 3+: Process via BatchJob
	presenter.OnProcessingStart(w, opts.ActionVerb)

	// Create BatchJob using the BatchJobFactory seam.
	// Per NEW-1: the factory owns infrastructure deps (WF, Matcher, PosterGen, BatchCfg)
	// so the CLI only provides per-call varying fields.
	batchCfg := BatchJobConfigFromAppConfig(cfg)
	factory := worker.NewBatchJobFactory(
		nil, // no JobStore — CLI doesn't need persistence
		bs.Workflow,
		bs.Matcher,
		bs.PosterGen,
		batchCfg,
		nil, // no emitter for CLI
	)
	job := factory.CreateStandaloneJob(filePaths, worker.BatchJobOptions{})

	// Validate the resolved seam strings before dereferencing them below; a
	// missing resolution step would otherwise panic when building applyOpts.
	if opts.Resolved == nil {
		return fmt.Errorf("batch command requires resolved seam strings: opts.Resolved is nil")
	}

	// Set run options using the shared CLIApplyOptions helper
	applyOpts := CLIApplyOptions{
		DryRun:       opts.DryRun,
		MoveFiles:    opts.MoveFiles,
		LinkMode:     opts.Resolved.LinkMode,
		ForceUpdate:  opts.ForceUpdate,
		SkipOrganize: opts.SkipOrganize,
		GenerateNFO:  opts.GenerateNFO,
		Download:     opts.DownloadMedia,
		Destination:  opts.Destination,
		MergeOptions: workflow.MergeOptions{
			ForceOverwrite: opts.ForceOverwrite,
			PreserveNFO:    opts.PreserveNFO,
			ScalarStrategy: opts.Resolved.ScalarStrategy,
			ArrayStrategy:  opts.Resolved.ArrayStrategy,
		},
	}
	job.SetRunOptions(
		factory.NewScrapeConfig(opts.ScraperPriority, false, opts.ForceRefresh),
		applyOpts.ToApplyPhaseConfig(),
	)

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
				eventHandler(w, event)
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
		ScanResult:   scanResult,
		FilePaths:    filePaths,
		MatchedCount: matchedCount,
		UniqueIDs:    uniqueIDs,
		Movies:       movies,
		SuccessCount: successCount,
		FailedCount:  failedCount,
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

	if opts.SkipOrganize {
		// Update-style summary
		fmt.Fprintf(w, "   Updated: %d, Failed: %d\n", len(result.Movies), result.FailedCount)
	} else {
		// Sort-style summary
		if opts.DryRun {
			fmt.Fprintf(w, "\n   Would organize %d file(s)\n", successCount)
		} else {
			fmt.Fprintf(w, "\n   Organized %d file(s)\n", successCount)
		}
	}

	// Summary
	fmt.Fprintln(w, "\n=== Summary ===")
	fmt.Fprintf(w, "Files scanned: %d\n", len(result.ScanResult.Files))
	fmt.Fprintf(w, "IDs matched: %d\n", result.MatchedCount)
	fmt.Fprintf(w, "Metadata found: %d\n", successCount)
	if opts.GenerateNFO {
		fmt.Fprintf(w, "NFOs generated: %s\n", map[bool]string{true: fmt.Sprintf("%d (dry-run)", successCount), false: fmt.Sprintf("%d", successCount)}[opts.DryRun])
	}
	if !opts.SkipOrganize {
		fmt.Fprintf(w, "Files organized: %s\n", map[bool]string{true: fmt.Sprintf("%d (dry-run)", successCount), false: fmt.Sprintf("%d", successCount)}[opts.DryRun])
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
