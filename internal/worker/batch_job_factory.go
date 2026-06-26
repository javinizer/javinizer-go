package worker

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// BatchJobOptions carries only the per-call varying fields for job construction.
// Infrastructure dependencies (workflow, matcher, posterGen, BatchJobConfig) are
// owned by the BatchJobFactory — callers do not need to know about them.
//
// This is the "narrow waist" of the construction seam: adding a new per-call
// field changes BatchJobOptions and the factory, not 3+ callers independently.
type BatchJobOptions struct {
	// Job identification (pre-generated for API; empty for auto-generated)
	ID string

	// Per-job configuration
	Destination           string
	OperationModeOverride operationmode.OperationMode
	Update                *bool

	// Per-call scrape configuration
	SelectedScrapers []string
	Strict           bool
	Force            bool
	FileMatchInfo    map[string]models.FileMatchInfo

	// Per-call apply configuration
	OrganizeOptions     workflow.OrganizeOptions
	MergeOptions        workflow.MergeOptions
	GenerateNFO         bool
	Download            bool
	DownloadExtrafanart *bool
	DryRun              bool

	// Hooks (apply-phase only)
	PreApplyFunc  func(ctx context.Context, afc *ApplyFileContext) error
	PostApplyFunc func(ctx context.Context, afc *ApplyFileContext, afr *ApplyFileResult)

	// Per-call rescrape configuration
	ManualSearchInput string

	// WF override: if set, used for this job instead of the factory's default.
	// The API handler creates a per-job WF via GetBatchWorkflow(jobID) and
	// passes it here. When empty, the factory's default WF is used (CLI/TUI).
	WF workflow.WorkflowInterface
}

// BatchJobFactoryInterface encapsulates job construction and phase configuration.
// Per NEW-1: API and TUI previously constructed worker.JobConfig, worker.BatchJobDeps,
// worker.ScrapePhaseConfig, worker.ApplyPhaseConfig, and worker.RescrapeCmd directly.
// The BatchJobInterface seam covered read/control but NOT construction.
//
// The factory owns the infrastructure dependencies (WF, Matcher, PosterGen, BatchCfg)
// so callers only provide per-call varying fields via BatchJobOptions.
// Adding a new field to ApplyPhaseConfig or ScrapePhaseConfig changes the factory,
// not 3 callers independently.
type BatchJobFactoryInterface interface {
	// CreateJob creates a new batch job via the JobStore and returns BatchJobInterface.
	// Use this for API handlers that need persistence and JobStore registration.
	CreateJob(files []string, opts BatchJobOptions) BatchJobInterface

	// CreateStandaloneJob creates a new BatchJob without a JobStore.
	// Use this for CLI/TUI usage where persistence is not needed.
	CreateStandaloneJob(files []string, opts BatchJobOptions) StandaloneJob

	// NewScrapeConfig builds a ScrapePhaseConfig with the factory's defaults filled in.
	// Callers only provide the narrow per-call parameters.
	NewScrapeConfig(selectedScrapers []string, strict bool, force bool) ScrapePhaseConfig

	// NewApplyConfig builds an ApplyPhaseConfig with the factory's defaults filled in.
	// Per DEEP-6: WF override removed. API handlers must call SetWorkflow on the job
	// before calling StartApply if the job's deps.WF is nil (reconstructed jobs).
	NewApplyConfig(organizeOpts workflow.OrganizeOptions, mergeOpts workflow.MergeOptions, dest string) ApplyPhaseConfig

	// NewRescrapeCmd builds a RescrapeCmd with the factory's defaults.
	// Per DEEP-6: WF/PosterGen/BatchCfg overrides removed. API handlers must call
	// SetWorkflow on the job before calling Rescrape if deps.WF is nil.
	// mergeOpts carries the resolved NFO merge policy (per ADR-0030); callers
	// that accept preset/scalar_strategy/array_strategy should resolve them via
	// workflow.ResolveSeamStrings and pass the resulting MergeOptions here so
	// CompleteRescrape honors the requested merge behavior.
	NewRescrapeCmd(movieID string, filePath string, manualSearchInput string, selectedScrapers []string, force bool, mergeOpts workflow.MergeOptions) RescrapeCmd
}

// batchJobFactory is the concrete implementation of BatchJobFactoryInterface.
// It holds the infrastructure dependencies so callers don't need to reach into
// the worker package for construction.
type batchJobFactory struct {
	jobStore  JobStoreInterface
	wf        workflow.WorkflowInterface
	matcher   matcher.MatcherInterface
	posterGen poster.PosterGenerator
	batchCfg  BatchJobConfig
	emitter   eventlog.EventEmitter
}

// NewBatchJobFactory creates a BatchJobFactoryInterface with the given infrastructure
// dependencies. The jobStore parameter is used by CreateJob for persistence;
// CreateStandaloneJob does not require it.
//
// Per NEW-1: this is the single source of truth for job construction. All callers
// (API, TUI, CLI) should use the factory instead of constructing worker.JobConfig,
// worker.BatchJobDeps, worker.ScrapePhaseConfig, worker.ApplyPhaseConfig, or
// worker.RescrapeCmd directly.
func NewBatchJobFactory(jobStore JobStoreInterface, wf workflow.WorkflowInterface, m matcher.MatcherInterface, posterGen poster.PosterGenerator, batchCfg BatchJobConfig, emitter eventlog.EventEmitter) BatchJobFactoryInterface {
	return &batchJobFactory{
		jobStore:  jobStore,
		wf:        wf,
		matcher:   m,
		posterGen: posterGen,
		batchCfg:  batchCfg,
		emitter:   emitter,
	}
}

// CreateJob creates a new batch job via the JobStore and returns BatchJobInterface.
// The factory provides the infrastructure dependencies (WF, Matcher, PosterGen, BatchCfg)
// so the caller only needs to specify per-call varying fields in BatchJobOptions.
func (f *batchJobFactory) CreateJob(files []string, opts BatchJobOptions) BatchJobInterface {
	jobCfg := f.buildJobConfig(opts)
	return f.jobStore.CreateJob(files, jobCfg)
}

// CreateStandaloneJob creates a new StandaloneJob using an in-memory JobStore.
// Per NEW-2: routes through JobStore.createJob (the single construction path)
// instead of calling NewBatchJob directly. The in-memory JobStore has no
// database, so persistence is a no-op, but the job still gets JobStore
// registration and PersistFn wiring for free.
// Per DEEP-2: returns StandaloneJob interface instead of *BatchJob, so CLI/TUI
// callers access sub-managers through the composite interface rather than
// passthrough methods on BatchJob.
func (f *batchJobFactory) CreateStandaloneJob(files []string, opts BatchJobOptions) StandaloneJob {
	jobCfg := f.buildJobConfig(opts)
	memStore := NewInMemoryJobStore()
	job := memStore.CreateJobBatch(files, jobCfg)
	return newStandaloneJobFromBatchJob(job)
}

// NewScrapeConfig builds a ScrapePhaseConfig with the factory's defaults filled in.
func (f *batchJobFactory) NewScrapeConfig(selectedScrapers []string, strict bool, force bool) ScrapePhaseConfig {
	return ScrapePhaseConfig{
		SelectedScrapers: selectedScrapers,
		Strict:           strict,
		Force:            force,
	}
}

// NewApplyConfig builds an ApplyPhaseConfig with the factory's defaults filled in.
// Per DEEP-6: WF override removed from config. API handlers set deps.WF via SetWorkflow.
func (f *batchJobFactory) NewApplyConfig(organizeOpts workflow.OrganizeOptions, mergeOpts workflow.MergeOptions, dest string) ApplyPhaseConfig {
	return ApplyPhaseConfig{
		OrganizeOptions: organizeOpts,
		MergeOptions:    mergeOpts,
		Destination:     dest,
	}
}

// NewRescrapeCmd builds a RescrapeCmd with the factory's defaults.
// Per DEEP-6: WF/PosterGen/BatchCfg overrides removed from RescrapeCmd.
// API handlers set deps.WF via SetWorkflow before calling Rescrape.
// mergeOpts carries the resolved NFO merge policy; callers that don't supply
// merge options should pass a zero workflow.MergeOptions (CompleteRescrape only
// merges when MergeEnabled is set, which the caller controls after building).
func (f *batchJobFactory) NewRescrapeCmd(movieID string, filePath string, manualSearchInput string, selectedScrapers []string, force bool, mergeOpts workflow.MergeOptions) RescrapeCmd {
	return RescrapeCmd{
		MovieID:           movieID,
		FilePath:          filePath,
		ManualSearchInput: manualSearchInput,
		SelectedScrapers:  selectedScrapers,
		Force:             force,
		Merge:             mergeOpts,
	}
}

// buildJobConfig constructs a *JobConfig from BatchJobOptions and the factory's
// infrastructure dependencies. This is the single source of truth for mapping
// BatchJobOptions → JobConfig — callers never build JobConfig directly.
func (f *batchJobFactory) buildJobConfig(opts BatchJobOptions) *JobConfig {
	wf := f.wf
	if opts.WF != nil {
		wf = opts.WF
	}
	deps := NewBatchJobDeps(wf, f.matcher, f.posterGen, f.batchCfg)
	if f.emitter != nil {
		deps.Emitter = f.emitter
	}
	return &JobConfig{
		ID:                    opts.ID,
		Destination:           opts.Destination,
		OperationModeOverride: opts.OperationModeOverride,
		Update:                opts.Update,
		BatchJobDeps:          deps,
	}
}

// Compile-time assertion that batchJobFactory satisfies BatchJobFactoryInterface.
var _ BatchJobFactoryInterface = (*batchJobFactory)(nil)
