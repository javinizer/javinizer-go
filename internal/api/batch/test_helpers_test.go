package batch

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

type updateOptions struct {
	ForceOverwrite bool
	PreserveNFO    bool
	Preset         string
	ScalarStrategy nfo.MergeStrategy
	ArrayStrategy  bool // true=merge, false=replace
	SkipNFO        bool
	SkipDownload   bool
}

func createTestDeps(t *testing.T, cfg *config.Config, configFile string) *core.APIDeps {
	deps := testkit.CreateTestDeps(t, cfg, configFile)
	return deps
}

func lifecycleDepsFromCore(d *core.APIDeps) *core.APIDeps {
	return d
}

func organizeDepsFromCore(d *core.APIDeps) *core.APIDeps {
	return d
}

func movieEditDepsFromCore(d *core.APIDeps) *core.APIRuntime {
	return testkit.GetTestRuntime(d)
}

func rescrapeDepsFromCore(d *core.APIDeps) *core.APIDeps {
	return d
}

// initTestWebSocket is a compatibility stub — CreateTestDeps now initializes the
// WebSocket directly on deps.Runtime. Tests that need a standalone hub should call
// testkit.InitTestWebSocket or testkit.StartStandaloneHub directly.
func initTestWebSocket(t *testing.T) {
	// No-op: WebSocket is initialized in CreateTestDeps
}

type mockWebSocketHub struct {
	mu       sync.Mutex
	messages []*websocket.ProgressMessage
}

func (m *mockWebSocketHub) BroadcastProgress(msg *websocket.ProgressMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *msg
	m.messages = append(m.messages, &cp)
	return nil
}

func (m *mockWebSocketHub) GetMessages() []*websocket.ProgressMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*websocket.ProgressMessage, len(m.messages))
	copy(out, m.messages)
	return out
}

func testStartScrape(ctx context.Context, job *worker.BatchJob, cfg *config.Config, db *database.DB, registry *scraperutil.ScraperRegistry, selectedScrapers []string, strict bool, force bool) error {
	fc, _ := workflow.NewFactoryConfigFromRepos(cfg, registry, db.Repositories())
	factory, err := workflow.NewWorkflowFactory(fc)
	if err != nil {
		return err
	}
	wf, err := factory.NewWorkflow("")
	if err != nil {
		return err
	}

	scrapeOpts := worker.ScrapePhaseConfig{
		SelectedScrapers: selectedScrapers,
		Strict:           strict,
		Force:            force,
	}
	// Per DEEP-6: WF and BatchCfg set on job.deps, not on phase config overrides
	job.Controller().SetWorkflow(wf)
	job.Controller().SetBatchCfg(worker.BatchJobConfig{
		MaxWorkers:      cfg.Performance.MaxWorkers,
		WorkerTimeout:   time.Duration(cfg.Performance.WorkerTimeout) * time.Second,
		ScraperPriority: cfg.Scrapers.Priority,
		NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
	})
	if err := job.Controller().StartScrape(ctx, job.ResultsWriter().GetFiles(), scrapeOpts); err != nil {
		return err
	}
	return job.Controller().Wait()
}

func testStartUpdateApply(ctx context.Context, job *worker.BatchJob, cfg *config.Config, db *database.DB, registry *scraperutil.ScraperRegistry, emitter eventlog.EventEmitter, opts *updateOptions) error {
	if opts == nil {
		opts = &updateOptions{}
	}
	fc, _ := workflow.NewFactoryConfigFromRepos(cfg, registry, db.Repositories())

	factory, err := workflow.NewWorkflowFactory(fc)
	if err != nil {
		return err
	}
	wf, err := factory.NewWorkflow(job.ID.String())
	if err != nil {
		return err
	}

	applyOpts := worker.ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{Skip: true},
		MergeOptions: workflow.MergeOptions{
			ForceOverwrite: opts.ForceOverwrite,
			PreserveNFO:    opts.PreserveNFO,
			ScalarStrategy: opts.ScalarStrategy,
			ArrayStrategy:  opts.ArrayStrategy,
		},
		GenerateNFO: !opts.SkipNFO,
		Download:    !opts.SkipDownload,
		PostApplyFunc: func(ctx context.Context, afc *worker.ApplyFileContext, afr *worker.ApplyFileResult) {
			if afr.Err != nil && emitter != nil {
				_ = emitter.EmitOrganizeEvent(context.Background(), "nfo_gen", fmt.Sprintf("Update failed for %s", afc.Movie.ID), models.SeverityError, map[string]interface{}{"job_id": job.ID, "movie_id": afc.Movie.ID, "error": afr.Err.Error()})
			}
		},
	}
	// Per DEEP-6: WF and BatchCfg set on job.deps, not on phase config overrides
	job.Controller().SetWorkflow(wf)
	job.Controller().SetBatchCfg(worker.BatchJobConfig{
		MaxWorkers:      cfg.Performance.MaxWorkers,
		WorkerTimeout:   time.Duration(cfg.Performance.WorkerTimeout) * time.Second,
		ScraperPriority: cfg.Scrapers.Priority,
		NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
	})
	// API-1+2: StartApply requires Completed lifecycle status (CAS fix for double-start race)
	setJobStatus(job, models.JobStatusCompleted)
	if err := job.Controller().StartApply(ctx, applyOpts); err != nil {
		setJobStatus(job, models.JobStatusFailed)
		return err
	}
	return job.Controller().Wait()
}

func testStartOrganizeApply(ctx context.Context, job *worker.BatchJob, jobStore worker.JobStoreInterface, destination string, copyOnly bool, linkModeRaw string, skipNFO bool, skipDownload bool, db *database.DB, cfg *config.Config, registry *scraperutil.ScraperRegistry, emitter eventlog.EventEmitter) error {
	var opModeOverride *operationmode.OperationMode
	if job.GetOperationModeOverride() != operationmode.OperationModeOrganize {
		m := job.GetOperationModeOverride()
		opModeOverride = &m
	}
	fc, _ := workflow.NewFactoryConfigFromRepos(cfg, registry, db.Repositories())

	fc.OperationMode = opModeOverride
	factory, err := workflow.NewWorkflowFactory(fc)
	if err != nil {
		setJobStatus(job, models.JobStatusFailed)
		if jobStore != nil {
			jobStore.PersistJob(job)
		}
		return err
	}
	wf, err := factory.NewWorkflow(job.ID.String())
	if err != nil {
		setJobStatus(job, models.JobStatusFailed)
		if jobStore != nil {
			jobStore.PersistJob(job)
		}
		return err
	}

	linkMode, err := workflow.ResolveLinkMode(linkModeRaw)
	if err != nil {
		setJobStatus(job, models.JobStatusFailed)
		if jobStore != nil {
			jobStore.PersistJob(job)
		}
		return err
	}

	applyOpts := worker.ApplyPhaseConfig{
		OrganizeOptions: workflow.OrganizeOptions{
			MoveFiles:   !copyOnly,
			LinkMode:    linkMode,
			ForceUpdate: true,
		},
		MergeOptions: workflow.MergeOptions{ForceOverwrite: true},
		Destination:  destination,
		GenerateNFO:  !skipNFO,
		Download:     !skipDownload,
	}
	// Per DEEP-6: WF and BatchCfg set on job.deps, not on phase config overrides
	job.Controller().SetWorkflow(wf)
	job.Controller().SetBatchCfg(worker.BatchJobConfig{
		MaxWorkers:      cfg.Performance.MaxWorkers,
		WorkerTimeout:   time.Duration(cfg.Performance.WorkerTimeout) * time.Second,
		ScraperPriority: cfg.Scrapers.Priority,
		NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
	})
	// API-1+2: StartApply requires Completed lifecycle status (CAS fix for double-start race)
	setJobStatus(job, models.JobStatusCompleted)
	if err := job.Controller().StartApply(ctx, applyOpts); err != nil {
		setJobStatus(job, models.JobStatusFailed)
		if jobStore != nil {
			jobStore.PersistJob(job)
		}
		return err
	}
	return job.Controller().Wait()
}

// setJobResult sets a file result on a BatchJob for test setup.
// This replaces the deleted UpdateFileResult method — test code sets
// Results directly and adjusts counters manually, with mutex protection.
// If the result does not have a ResultID, one is auto-generated from the MovieID.
func setJobResult(job *worker.BatchJob, filePath string, result *worker.MovieResult) {
	if result.ResultID == "" {
		result.ResultID = result.FileMatchInfo.MovieID
	}
	job.ResultsWriter().UpdateFileResult(filePath, result)
}

// setJobStatus sets the job status for test setup.
// This replaces the deleted MarkStarted/MarkCompleted/MarkOrganized/MarkFailed/MarkCancelled methods.
// It also sets the corresponding timestamp fields, matching the behavior of the deleted Mark* methods.
func setJobStatus(job *worker.BatchJob, status models.JobStatus) {
	job.Controller().SetJobStatus(status)
}

// createJobWithWF creates a BatchJob with a Workflow attached at construction.
// This matches the production flow where lifecycle.go creates jobs with jobConfig.WF.
// Tests that hit HTTP handler endpoints (rescrape, organize, etc.) should use this
// instead of bare CreateJob, since handlers assume the job has a WF.
func createJobWithWF(deps *core.APIDeps, cfg *config.Config, files []string) *worker.BatchJob {
	fc, _ := workflow.NewFactoryConfigFromRepos(cfg, deps.CoreDeps.ScraperRegistry, deps.CoreDeps.DB.Repositories())
	factory, err := workflow.NewWorkflowFactory(fc)
	if err != nil {
		panic(fmt.Sprintf("createJobWithWF: failed to create workflow factory: %v", err))
	}
	wf, err := factory.NewWorkflow("")
	if err != nil {
		panic(fmt.Sprintf("createJobWithWF: failed to create workflow: %v", err))
	}

	return deps.JobStore.CreateJobBatch(files, &worker.JobConfig{
		BatchJobDeps: worker.BatchJobDeps{
			WF: wf,
			BatchCfg: worker.BatchJobConfig{
				MaxWorkers:      cfg.Performance.MaxWorkers,
				WorkerTimeout:   time.Duration(cfg.Performance.WorkerTimeout) * time.Second,
				ScraperPriority: cfg.Scrapers.Priority,
				NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
			},
		},
	})
}

// excludeFile excludes a file from the batch job for test setup.
// Per DEEP-1: BatchJob no longer has ExcludeFile — this helper provides
// equivalent behavior by calling ResultTracker and JobLifecycle directly.
func excludeFile(job *worker.BatchJob, filePath string) {
	job.ResultsWriter().MarkExcluded(filePath)

	if job.ResultsWriter().IsAllExcluded() {
		job.Lifecycle().Cancel()
	}
}
