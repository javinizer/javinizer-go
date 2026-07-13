package batch

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// ListJobsInput holds the parameters for listing batch jobs.
type ListJobsInput struct {
	Limit  int
	Offset int
}

// ListJobsOutput holds the result of listing batch jobs.
type ListJobsOutput struct {
	Jobs  []contracts.BatchJobResponse
	Total int
}

// ListJobsUseCase retrieves a paginated list of batch jobs with operation counts.
// It handles pagination, batch-fetching of operation/revert counts, and response assembly.
func ListJobsUseCase(ctx context.Context, deps *core.APIDeps, input ListJobsInput) (*ListJobsOutput, error) {
	jobs, err := deps.GetJobRepo().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	total := len(jobs)

	// Apply pagination — normalize Offset/Limit to non-negative values before
	// indexing to avoid panics on bad input, then clamp the upper bound to total.
	if input.Offset < 0 {
		input.Offset = 0
	}
	if input.Limit < 0 {
		input.Limit = 0
	}
	if input.Offset > total {
		input.Offset = total
	}
	end := input.Offset
	if input.Limit > total-input.Offset {
		end = total
	} else {
		end += input.Limit
	}
	pagedJobs := jobs[input.Offset:end]

	// Batch-fetch operation counts for paged jobs — avoids N+1 queries.
	jobIDs := make([]string, 0, len(pagedJobs))
	for _, job := range pagedJobs {
		jobIDs = append(jobIDs, job.ID)
	}

	var opCounts map[string]int64
	var revertedCounts map[string]int64

	if len(jobIDs) > 0 {
		opCounts, err = deps.GetBatchFileOpRepo().CountByBatchJobIDs(ctx, jobIDs)
		if err != nil {
			logging.Errorf("Failed to count operations: %v", err)
			return nil, fmt.Errorf("failed to retrieve operation counts")
		}
		revertedCounts, err = deps.GetBatchFileOpRepo().CountRevertedByBatchJobIDs(ctx, jobIDs)
		if err != nil {
			logging.Errorf("Failed to count reverted operations: %v", err)
			return nil, fmt.Errorf("failed to retrieve revert counts")
		}
	} else {
		opCounts = make(map[string]int64)
		revertedCounts = make(map[string]int64)
	}

	response := contracts.BatchJobListResponse{
		Jobs:  make([]contracts.BatchJobResponse, 0, len(pagedJobs)),
		Total: total,
	}

	for _, job := range pagedJobs {
		// Parse persisted results inline so the /jobs list view can render
		// thumbnail previews from r.movie.poster_url / cropped_poster_url
		// without an extra GET /batch/{id} round trip per job. This restores
		// the behavior of main's listBatchJobs, which unmarshaled job.Results
		// into BatchFileResult (including movie+poster data) on every list call.
		// The previous refactor deferred this to GET /batch/{id}?include_data=true,
		// which broke /jobs page thumbnails (frontend getFirstPoster reads
		// job.results inline). parseAndConvertJobResults also handles the legacy
		// "data" JSON field via worker.ParseJobResultsJSON.
		results := parseAndConvertJobResults(&job, deps.GetFs())

		response.Jobs = append(response.Jobs, contracts.BatchJobResponse{
			ID:                    job.ID,
			Status:                job.Status,
			TotalFiles:            job.TotalFiles,
			Completed:             job.Completed,
			Failed:                job.Failed,
			OperationCount:        opCounts[job.ID],
			RevertedCount:         revertedCounts[job.ID],
			Excluded:              nil, // Deferred — use GET /batch/{id} for excluded details
			Progress:              job.Progress,
			Destination:           job.Destination,
			Files:                 nil, // Deferred — file paths fall back to Object.keys(results) on the client
			Results:               results,
			StartedAt:             contracts.FormatTime(job.StartedAt),
			CompletedAt:           contracts.FormatTimePtr(job.CompletedAt),
			OperationModeOverride: job.OperationModeOverride,
			Update:                job.Update,
		})
	}

	return &ListJobsOutput{
		Jobs:  response.Jobs,
		Total: response.Total,
	}, nil
}

// StartScrapeInput holds the parameters for starting a batch scrape job.
type StartScrapeInput struct {
	Files            []string
	Destination      string
	OperationMode    string
	Preset           string
	ScalarStrategy   string
	ArrayStrategy    string
	Update           *bool
	SelectedScrapers []string
	Strict           bool
	Force            bool
	ManualInputs     map[string]string
}

// StartScrapeOutput holds the result of starting a batch scrape job.
type StartScrapeOutput struct {
	JobID string
}

// StartScrapeUseCase discovers sibling files, resolves seam strings, creates a workflow,
// and starts a batch scrape job. Returns the job ID on success.
func StartScrapeUseCase(
	ctx context.Context,
	rt *core.APIRuntime,
	input StartScrapeInput,
) (*StartScrapeOutput, error) {
	// Take one consistent snapshot so the APIConfig (security/scanner settings),
	// the batch workflow, and the batch job factory all come from the same reload
	// epoch. Reading them via separate accessors could mix old/new state if a
	// config reload lands between the calls (issue #44).
	snap := rt.Snapshot()

	// Auto-discover sibling multi-part files
	allFiles, fileMatchInfoMap := discoverSiblingPartsWithMetadata(ctx, input.Files, snap, snap.APIConfig().SecurityConfig(), snap.APIConfig().ScannerConfig())

	if len(allFiles) > len(input.Files) {
		logging.Infof("Auto-discovered %d sibling files for batch job (original: %d, total: %d)",
			len(allFiles)-len(input.Files), len(input.Files), len(allFiles))
	}

	// Resolve manual inputs: propagate each submitter's input to discovered
	// siblings sharing the matcher MovieID. Files with explicit manual inputs
	// override the matcher-derived grouping key so they split into separate
	// movies in the UI queue.
	resolvedManualInput := resolveManualInputOverride(input.Files, input.ManualInputs, fileMatchInfoMap, allFiles)
	propagatedManualInputs := resolvedManualInput.overrides
	allFiles = resolvedManualInput.allFiles

	resolved, err := workflow.ResolveSeamStrings(workflow.SeamStringsInput{
		OperationMode:  input.OperationMode,
		Preset:         input.Preset,
		ScalarStrategy: input.ScalarStrategy,
		ArrayStrategy:  input.ArrayStrategy,
	})
	if err != nil {
		return nil, fmt.Errorf("invalid seam parameters: %w", err)
	}

	// Pre-generate job ID so we can create the workflow before the job,
	// ensuring RevertLog records are associated with the correct job for revert support.
	jobID := uuid.New().String()

	wf, err := snap.BatchWorkflow(jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow: %w", err)
	}

	matchInfo := fileMatchInfoMap

	// BatchWorkflow above already returns an error when the workflow factory is
	// unavailable, and buildPosterGenerator always returns a non-nil generator on
	// a built factory — so BatchJobFactory cannot be nil here. Guarding it would
	// be dead code (unreachable: BatchWorkflow errors first on a broken factory).
	factory := snap.BatchJobFactory()

	job := factory.CreateJob(allFiles, worker.BatchJobOptions{
		ID:                    jobID,
		Destination:           input.Destination,
		OperationModeOverride: resolved.OperationMode,
		Update:                input.Update,
		WF:                    wf,
		FileMatchInfo:         matchInfo,
	})

	scrapeOpts := factory.NewScrapeConfig(input.SelectedScrapers, input.Strict, input.Force)
	// Propagate the discovered file match metadata into the scrape phase so it
	// is available during scraping (mirrors BatchJobOptions.FileMatchInfo above);
	// otherwise metadata collected earlier in the usecase never reaches the
	// scrape config.
	scrapeOpts.FileMatchInfo = matchInfo
	scrapeOpts.RawInputOverride = propagatedManualInputs
	// Wire per-file scrape progress hooks so the frontend's messagesByFile
	// populates during scrape and ProgressModal shows live per-file status.
	// Restores main's realtime.ProgressAdapter behavior (deleted in this
	// refactor) via the ScrapePhaseConfig hook seam.
	scrapeSink := newOrganizeBroadcastSink(rt)
	scrapeOpts.OnFileScraped = makeScrapeFileScrapedBroadcaster(job, scrapeSink)
	scrapeOpts.OnFileScrapeFailed = makeScrapeFileFailedBroadcaster(job, scrapeSink)
	scrapeOpts.OnScrapeStepProgress = makeScrapeStepProgressBroadcaster(job, scrapeSink)
	go func() {
		if err := job.StartScrape(rt.ServerCtx(), allFiles, scrapeOpts); err != nil {
			logging.Errorf("BatchJob.StartScrape failed: %v", err)
		}
	}()

	return &StartScrapeOutput{JobID: job.GetID()}, nil
}
