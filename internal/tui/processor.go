package tui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// simpleRunner implements backgroundRunner using a single background goroutine
// with WaitGroup and error collection. This replaces the former worker.Pool
// for TUI use, which only ever submitted a single batch task.
//
// When multiple Go() tasks fail, all errors are collected and joined via
// errors.Join so no error is silently dropped.
type simpleRunner struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	errs   []error
}

func NewSimpleRunner(ctx context.Context) *simpleRunner {
	ctx, cancel := context.WithCancel(ctx)
	return &simpleRunner{
		ctx:    ctx,
		cancel: cancel,
	}
}

func (r *simpleRunner) Go(fn func() error) error {
	if r.ctx.Err() != nil {
		return r.ctx.Err()
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				r.mu.Lock()
				r.errs = append(r.errs, panicutil.FormatRecover(rec))
				r.mu.Unlock()
			}
		}()
		if err := fn(); err != nil {
			r.mu.Lock()
			r.errs = append(r.errs, err)
			r.mu.Unlock()
		}
	}()
	return nil
}

func (r *simpleRunner) Wait() error {
	r.wg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	return errors.Join(r.errs...)
}

func (r *simpleRunner) Stop() {
	r.cancel()
	r.wg.Wait()
}

// Context returns the runner's cancellable context, so callers can derive child
// contexts whose cancellation propagates when Stop() is called.
func (r *simpleRunner) Context() context.Context {
	return r.ctx
}

// TUIProcessorConfig carries the narrow set of config fields the processingCoordinator
// reads from the application config. Swapped atomically on hot-reload via atomic.Value.
// Embeds worker.BatchJobConfig for the shared 4-field mapping
// (MaxWorkers, WorkerTimeout, ScraperPriority, NFOEnabled), adding only the
// TUI-specific DownloadExtrafanart field.
type TUIProcessorConfig struct {
	worker.BatchJobConfig
	DownloadExtrafanart bool
}

// ProcessorOptions holds all mutable processing options as a single struct,
// swapped atomically so readers always see a consistent snapshot.
// Adding a new toggle only requires adding a field here and updating
// runBatchJob — no new atomic field or Set* method needed.
type ProcessorOptions struct {
	DestPath                    string
	MoveFiles                   bool
	ForceUpdate                 bool
	ForceRefresh                bool
	DryRun                      bool
	ScrapeEnabled               bool
	DownloadEnabled             bool
	OrganizeEnabled             bool
	NFOEnabled                  bool
	DownloadExtrafanartOverride bool
	LinkMode                    string
	UpdateMode                  bool
	ScalarStrategy              string
	ArrayStrategy               string
	CustomScraperPriority       []string
	// Runtime config from TUIProcessorConfig
	MaxWorkers      int
	WorkerTimeout   time.Duration
	ScraperPriority []string
}

// processingCoordinator coordinates task execution for the TUI
type processingCoordinator struct {
	runner    backgroundRunner
	forwardCh chan worker.JobEvent
	factory   worker.BatchJobFactoryInterface
	registry  scrape.ScraperInstanceResolver
	opts      atomic.Value // stores ProcessorOptions
}

// NewProcessingCoordinator creates a new processing coordinator.
// factory is the BatchJobFactoryInterface for constructing batch jobs and phase
// configs without reaching into worker internals. Per NEW-1: replaces the former
// wf/matcher/posterGen fields with a single factory seam.
// registry carries the scraper registry for runtime scraper resolution.
// processorCfg carries the narrow set of fields the coordinator itself reads.
func NewProcessingCoordinator(
	runner backgroundRunner,
	forwardCh chan worker.JobEvent,
	factory worker.BatchJobFactoryInterface,
	registry scrape.ScraperInstanceResolver,
	processorCfg TUIProcessorConfig,
	destPath string,
	moveFiles bool,
) (*processingCoordinator, error) {
	if factory == nil {
		return nil, fmt.Errorf("batch job factory must not be nil")
	}
	if runner == nil {
		return nil, fmt.Errorf("runner must not be nil")
	}
	if registry == nil {
		return nil, fmt.Errorf("scraper registry must not be nil")
	}

	pc := &processingCoordinator{
		runner:    runner,
		forwardCh: forwardCh,
		factory:   factory,
		registry:  registry,
	}

	// Deep-copy slice fields so the stored snapshot owns its backing arrays and
	// cannot race with caller mutations (e.g. in-place config updates).
	scraperPriorityCopy := append([]string(nil), processorCfg.ScraperPriority...)

	pc.opts.Store(ProcessorOptions{
		DestPath:        destPath,
		MoveFiles:       moveFiles,
		ScrapeEnabled:   true,
		DownloadEnabled: true,
		OrganizeEnabled: true,
		NFOEnabled:      true,
		LinkMode:        "",
		ScalarStrategy:  "prefer-nfo",
		ArrayStrategy:   "merge",
		MaxWorkers:      processorCfg.MaxWorkers,
		WorkerTimeout:   processorCfg.WorkerTimeout,
		ScraperPriority: scraperPriorityCopy,
	})

	return pc, nil
}

// loadOptions returns the current ProcessorOptions snapshot.
// The returned struct is a value copy — callers can read fields without races.
func (pc *processingCoordinator) loadOptions() ProcessorOptions {
	return pc.opts.Load().(ProcessorOptions)
}

// applyOptions loads the current ProcessorOptions, applies the mutator, and stores
// the updated struct atomically. Safe for single-writer (TUI update loop) +
// concurrent readers (background goroutines).
func (pc *processingCoordinator) applyOptions(fn func(*ProcessorOptions)) {
	opts := pc.loadOptions()
	fn(&opts)
	pc.opts.Store(opts)
}

// Registry returns the underlying scraper registry used by the workflow.
func (pc *processingCoordinator) Registry() scrape.ScraperInstanceResolver {
	return pc.registry
}

// SetOptions atomically replaces all processing options with the given snapshot.
// For partial updates, use LoadOptions → mutate → SetOptions.
func (pc *processingCoordinator) SetOptions(opts ProcessorOptions) {
	// Deep-copy slice fields so the stored snapshot owns its backing arrays and
	// cannot be mutated by the caller after the swap.
	opts.ScraperPriority = append([]string(nil), opts.ScraperPriority...)
	opts.CustomScraperPriority = append([]string(nil), opts.CustomScraperPriority...)
	pc.opts.Store(opts)
}

// LoadOptions returns the current ProcessorOptions snapshot.
// The returned struct is a value copy — callers can read fields without races.
func (pc *processingCoordinator) LoadOptions() ProcessorOptions {
	return pc.loadOptions()
}

// SetConfig provides runtime config for template-aware NFO merge path resolution.
func (pc *processingCoordinator) SetConfig(cfg TUIProcessorConfig) {
	pc.applyOptions(func(o *ProcessorOptions) {
		o.NFOEnabled = cfg.NFOEnabled
		o.DownloadExtrafanartOverride = cfg.DownloadExtrafanart
		o.MaxWorkers = cfg.MaxWorkers
		o.WorkerTimeout = cfg.WorkerTimeout
		// Defensive copy so later mutations of cfg.ScraperPriority cannot race
		// with readers of the stored snapshot.
		o.ScraperPriority = append([]string(nil), cfg.ScraperPriority...)
	})
}

// SetCustomScrapers sets custom scraper priority for manual search
// Makes a defensive copy to prevent data races with worker goroutines
func (pc *processingCoordinator) SetCustomScrapers(scrapers []string) {
	pc.applyOptions(func(o *ProcessorOptions) {
		if scrapers == nil {
			o.CustomScraperPriority = nil
			return
		}
		o.CustomScraperPriority = append([]string(nil), scrapers...)
	})
}

// GetCustomScrapers returns the current custom scraper priority
// Returns a copy to prevent external mutation
func (pc *processingCoordinator) GetCustomScrapers() []string {
	s := pc.loadOptions().CustomScraperPriority
	if s == nil {
		return nil
	}
	return append([]string(nil), s...)
}

// ProcessFiles processes the selected files with matched JAV IDs
func (pc *processingCoordinator) ProcessFiles(
	ctx context.Context,
	files []fileItem,
	matches map[string]models.FileMatchInfo,
) error {
	// Validate critical dependencies to prevent deep panics in worker package
	if pc.runner == nil {
		return fmt.Errorf("background runner is nil")
	}
	if pc.registry == nil {
		return fmt.Errorf("scraper registry is nil")
	}
	if pc.factory == nil {
		return fmt.Errorf("batch job factory is nil")
	}

	logging.Debugf("ProcessFiles called with %d files", len(files))

	// Collect matched file paths
	filePaths := make([]string, 0, len(files))
	for _, file := range files {
		// Skip directories and unmatched files
		if file.IsDir || !file.Matched {
			continue
		}
		match, found := matches[file.Path]
		if !found {
			continue
		}
		filePaths = append(filePaths, file.Path)
		_ = match // match data is passed through the matches map
	}

	if len(filePaths) == 0 {
		return nil
	}

	// Submit a background task that uses BatchJob.Subscribe() for progress
	if err := pc.runner.Go(func() error {
		return pc.runBatchJob(ctx, filePaths)
	}); err != nil {
		return fmt.Errorf("failed to start batch process: %w", err)
	}

	logging.Debugf("ProcessFiles completed, submitted batch task")
	return nil
}

// Wait waits for all tasks to complete
func (pc *processingCoordinator) Wait() error {
	return pc.runner.Wait()
}

// Stop stops the background runner
func (pc *processingCoordinator) Stop() {
	pc.runner.Stop()
}

// runBatchJob creates a BatchJob for the given files, subscribes to events,
// and forwards per-movie progress to the JobEvent channel.
func (pc *processingCoordinator) runBatchJob(ctx context.Context, filePaths []string) error {
	// Load a single consistent snapshot of all options.
	opts := pc.loadOptions()

	// Derive the job context from the runner's cancellable context so that
	// Stop() (which cancels the runner) propagates into job execution and the
	// event-forwarding goroutine, rather than only the caller's ctx. The
	// caller's ctx is still honored: if it is canceled the derived ctx cancels too.
	runnerCtx := pc.runner.Context()
	jobCtx, cancel := context.WithCancel(runnerCtx)
	defer cancel()
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-jobCtx.Done():
		}
	}()

	factory := pc.factory
	job := factory.CreateStandaloneJob(filePaths, worker.BatchJobOptions{})

	scrapersCopy := []string(nil)
	if opts.CustomScraperPriority != nil {
		scrapersCopy = make([]string, len(opts.CustomScraperPriority))
		copy(scrapersCopy, opts.CustomScraperPriority)
	} else if opts.ScraperPriority != nil {
		// Honor the configured scraper priority as the default when no custom
		// override is set, so normal runs preserve the configured ordering.
		scrapersCopy = make([]string, len(opts.ScraperPriority))
		copy(scrapersCopy, opts.ScraperPriority)
	}

	// Resolve all seam strings through the shared function.
	// Per ADR-0030: resolution happens at the boundary — downstream code
	// receives fully-resolved typed values.
	resolved, resolveErr := workflow.ResolveSeamStrings(workflow.SeamStringsInput{
		LinkMode:       opts.LinkMode,
		ScalarStrategy: opts.ScalarStrategy,
		ArrayStrategy:  opts.ArrayStrategy,
	})
	if resolveErr != nil {
		logging.Errorf("Invalid seam parameters: %v", resolveErr)
		return fmt.Errorf("invalid seam parameters: %w", resolveErr)
	}

	applyOpts := factory.NewApplyConfig(
		workflow.OrganizeOptions{
			MoveFiles:   opts.MoveFiles,
			LinkMode:    resolved.LinkMode,
			ForceUpdate: opts.ForceUpdate,
		},
		workflow.MergeOptions{
			ForceOverwrite: !opts.UpdateMode,
			ScalarStrategy: resolved.ScalarStrategy,
			ArrayStrategy:  resolved.ArrayStrategy,
		},
		opts.DestPath,
	)
	applyOpts.GenerateNFO = opts.NFOEnabled
	applyOpts.Download = opts.DownloadEnabled

	applyOpts.DownloadExtrafanart = &opts.DownloadExtrafanartOverride
	if opts.UpdateMode {
		applyOpts.OrganizeOptions.Skip = true
	}

	job.SetRunOptions(
		factory.NewScrapeConfig(scrapersCopy, false, opts.ForceRefresh),
		applyOpts,
	)

	if pc.forwardCh != nil {
		jobSub := job.Subscribe()
		go func() {
			defer jobSub.Close()
			for evt := range jobSub.Events() {
				select {
				case pc.forwardCh <- evt:
				default:
				}
			}
		}()
	}

	return job.Run(jobCtx)
}
