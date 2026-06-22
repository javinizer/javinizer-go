package worker

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// runnableJob is the narrow interface that JobRunner needs to orchestrate
// the scrape→apply pipeline. It's satisfied by both *BatchJob (for internal
// use) and ControlledJob (for external consumers via NewJobRunner).
// Separating this from ControlledJob avoids requiring Cancel/MarkReverted
// on the internal path — BatchJob.Run() handles lifecycle setup itself.
type runnableJob interface {
	// Phase execution (from PhaseController)
	StartScrape(ctx context.Context, files []string, cfg ScrapePhaseConfig) error
	StartApply(ctx context.Context, cfg ApplyPhaseConfig) error
	Wait() error

	// Status access for orchestration decisions
	GetStatus() *BatchJobStatus
}

// Compile-time assertions for JobRunner interfaces.
var (
	_ runnableJob = (ControlledJob)(nil) // ControlledJob satisfies runnableJob
)

// JobRunner orchestrates the sequential scrape→apply workflow.
// Extracted from *BatchJob.Run() so the orchestration logic is testable and
// reusable independently of BatchJob's internal state.
//
// For external consumers (CLI, TUI), use NewJobRunner with a ControlledJob
// obtained from JobStore.GetJobForControl or JobStore.CreateJob.
// BatchJob.Run() delegates internally to NewJobRunner with a ControlledJob
// adapter instead of wrapping *BatchJob directly.
type JobRunner struct {
	job       runnableJob
	scrapeCfg *ScrapePhaseConfig
	applyCfg  *ApplyPhaseConfig
	batchCfg  BatchJobConfig
}

// NewJobRunner creates a JobRunner for the given ControlledJob.
// The job must have WF and BatchCfg configured before Run() is called.
// batchCfg provides default values for scrape/apply configs when
// SetRunOptions has not been called.
func NewJobRunner(job ControlledJob, batchCfg BatchJobConfig) *JobRunner {
	return &JobRunner{job: job, batchCfg: batchCfg}
}

// newInternalRunner is removed. Per W4-A: BatchJob.Run() now uses
// NewJobRunner with a ControlledJob adapter instead of wrapping *BatchJob directly.
// This eliminates the *BatchJob closure dependency from JobRunner.

// SetRunOptions stores scrape and apply phase configs that Run() will use.
// If not called, Run() uses sensible defaults derived from BatchCfg.
func (r *JobRunner) SetRunOptions(scrapeCfg ScrapePhaseConfig, applyCfg ApplyPhaseConfig) {
	r.scrapeCfg = &scrapeCfg
	r.applyCfg = &applyCfg
}

// Run executes the full scrape→apply pipeline sequentially.
// It runs the scrape phase, checks for completed results, and if any exist,
// runs the apply phase. Returns nil on success or an error describing
// which phase failed.
//
// For internal use (BatchJob.Run()): callers are responsible for any pre-run
// setup (e.g., setting keepBroadcasterOpen) and post-run cleanup (e.g.,
// closing the event broadcaster) when orchestrating both phases.
// For external use (NewJobRunner): the caller should manage the broadcaster
// lifecycle around the call to Run().
func (r *JobRunner) Run(ctx context.Context) error {
	// Per DEEP-1: validation moved from BatchJob.Run() to JobRunner.
	// These checks give clear error messages before attempting phase execution.
	status := r.job.GetStatus()
	jobID := status.ID.String()

	// Try StartScrape first — if WF is nil, it returns a descriptive error.
	// We also validate batchCfg here since it's a JobRunner field.
	if r.batchCfg.MaxWorkers <= 0 && r.batchCfg.WorkerTimeout <= 0 && len(r.batchCfg.ScraperPriority) == 0 {
		return fmt.Errorf("job %s: cannot run — batch config not configured (provide JobConfig.BatchCfg at creation)", jobID)
	}

	files := status.Files

	if len(files) == 0 {
		return nil
	}

	scrapeCfg := ScrapePhaseConfig{
		Force:  false,
		Strict: false,
	}
	if r.scrapeCfg != nil {
		scrapeCfg = *r.scrapeCfg
	} else {
		scrapeCfg.SelectedScrapers = r.batchCfg.ScraperPriority
	}

	if err := r.job.StartScrape(ctx, files, scrapeCfg); err != nil {
		return fmt.Errorf("scrape phase failed to start: %w", err)
	}

	if err := r.job.Wait(); err != nil {
		return fmt.Errorf("scrape phase failed: %w", err)
	}

	// Re-read status after scrape completes to check results
	status = r.job.GetStatus()
	hasCompleted := false
	for _, result := range status.Results {
		if result.Status == models.JobStatusCompleted && result.Movie != nil {
			hasCompleted = true
			break
		}
	}
	if !hasCompleted {
		return nil
	}

	applyCfg := ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{
			MoveFiles:   true,
			ForceUpdate: true,
		},
		MergeOptions: workflow.MergeOptions{
			ForceOverwrite: true,
		},
		Destination: status.Destination,
		GenerateNFO: true,
		Download:    true,
	}
	if r.applyCfg != nil {
		applyCfg = *r.applyCfg
	}
	if applyCfg.GenerateNFO {
		applyCfg.GenerateNFO = r.batchCfg.NFOEnabled
	}

	if err := r.job.StartApply(ctx, applyCfg); err != nil {
		return fmt.Errorf("apply phase failed to start: %w", err)
	}

	if err := r.job.Wait(); err != nil {
		return fmt.Errorf("apply phase failed: %w", err)
	}

	return nil
}
