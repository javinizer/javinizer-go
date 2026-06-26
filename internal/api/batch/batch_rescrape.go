package batch

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"github.com/javinizer/javinizer-go/internal/worker"
)

const bulkRescrapeWorkers = 5
const bulkRescrapeMaxMovies = 100

// bulkRescrapePool runs the bulk rescrape worker pool. It accepts a job,
// movie IDs, request parameters, a batch job factory, and a progress
// broadcast function, and returns per-movie results.
func bulkRescrapePool(
	ctx context.Context,
	job worker.BatchJobInterface,
	movieIDs []string,
	req *contracts.BatchRescrapeRequest,
	factory worker.BatchJobFactoryInterface,
	progressFn func(movieID string, result *contracts.BulkRescrapeMovieResult, progress float64),
) []contracts.BulkRescrapeMovieResult {
	type rescrapeMovieResult struct {
		movieID string
		result  *contracts.BulkRescrapeMovieResult
	}

	var mu sync.Mutex
	var completedCount int
	results := make([]contracts.BulkRescrapeMovieResult, 0, len(movieIDs))

	movieChan := make(chan string, len(movieIDs))
	resultChan := make(chan rescrapeMovieResult, len(movieIDs))

	workerCount := bulkRescrapeWorkers
	if workerCount > len(movieIDs) {
		workerCount = len(movieIDs)
	}

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var currentMovieID string
			defer func() {
				if r := recover(); r != nil {
					panicErr := panicutil.FormatRecover(r)
					logging.Errorf("Batch rescrape worker panicked on movie %s: %v", currentMovieID, panicErr)
					resultChan <- rescrapeMovieResult{movieID: currentMovieID, result: &contracts.BulkRescrapeMovieResult{
						MovieID: currentMovieID,
						Status:  models.RescrapeStatusFailed,
						Error:   panicErr.Error(),
					}}
				}
			}()
			for movieID := range movieChan {
				currentMovieID = movieID
				r := processBulkRescrapeMovie(ctx, movieID, job, req, factory)
				resultChan <- rescrapeMovieResult{movieID: movieID, result: r}
			}
		}()
	}

	for _, movieID := range movieIDs {
		movieChan <- movieID
	}
	close(movieChan)

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for mr := range resultChan {
		mu.Lock()
		results = append(results, *mr.result)
		completedCount++
		if progressFn != nil {
			progressFn(mr.movieID, mr.result, float64(completedCount)/float64(len(movieIDs))*100)
		}
		mu.Unlock()
	}

	return results
}

func batchRescrapeMovies(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")

		var req contracts.BulkRescrapeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		if len(req.MovieIDs) == 0 {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "movie_ids is required and must not be empty"})
			return
		}

		if len(req.MovieIDs) > bulkRescrapeMaxMovies {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: fmt.Sprintf("movie_ids must not exceed %d items", bulkRescrapeMaxMovies)})
			return
		}

		rescrapeReq := &contracts.BatchRescrapeRequest{
			Force:            req.Force,
			SelectedScrapers: req.SelectedScrapers,
			Preset:           req.Preset,
			ScalarStrategy:   req.ScalarStrategy,
			ArrayStrategy:    req.ArrayStrategy,
		}

		if httpStatus, errMsg := validateRescrapeRequest(rescrapeReq); errMsg != "" {
			writeErrorResponse(c, httpStatus, false, errMsg)
			return
		}

		job, ok := deps.GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}

		snap := job.GetStatus()
		if rescrapeNotAllowed(snap) {
			if snap.IsDeleted {
				writeErrorResponse(c, http.StatusGone, true, "Job has been deleted")
			} else {
				writeErrorResponse(c, http.StatusConflict, false, fmt.Sprintf("Cannot rescrape %s job", snap.Status))
			}
			return
		}

		// Delegate to orchestrator for resolve→construct→execute pipeline
		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  deps.GetJobStore(),
			WfFactory: &apiWorkflowFactory{rt: rt},
			Factory:   rt.GetBatchJobFactory(),
			Persist:   deps.GetJobStore(),
			Broadcast: &runtimeStateBroadcaster{rs: rt.GetRuntime()},
			ServerCtx: rt.ServerCtx(),
		})

		result, err := orch.BulkRescrape(c.Request.Context(), jobID, req.MovieIDs, rescrapeReq)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		jobResponse := buildBatchJobResponse(result.JobStatus)

		c.JSON(http.StatusOK, contracts.BulkRescrapeResponse{
			Results:   result.Results,
			Succeeded: result.Succeeded,
			Failed:    result.Failed,
			Job:       jobResponse,
		})
	}
}

func processBulkRescrapeMovie(ctx context.Context, movieID string, job worker.BatchJobInterface, req *contracts.BatchRescrapeRequest, factory worker.BatchJobFactoryInterface) *contracts.BulkRescrapeMovieResult {
	mergeOpts, mergeEnabled, mergeErr := resolveRescrapeMergeOptions(req)
	if mergeErr != nil {
		return &contracts.BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  models.RescrapeStatusFailed,
			Error:   fmt.Sprintf("invalid merge options: %v", mergeErr),
		}
	}
	cmd := factory.NewRescrapeCmd(
		movieID,
		"", // filePath resolved by job
		req.ManualSearchInput,
		req.SelectedScrapers,
		req.Force,
		mergeOpts,
	)
	cmd.MergeEnabled = mergeEnabled
	result, err := job.Rescrape(ctx, cmd)
	if err != nil {
		return &contracts.BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  models.RescrapeStatusFailed,
			Error:   fmt.Sprintf("Rescrape failed: %v", err),
		}
	}

	if result.Status == models.RescrapeStatusGone {
		return &contracts.BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  models.RescrapeStatusFailed,
			Error:   "Job was deleted during rescrape",
		}
	}

	if result.Status == models.RescrapeStatusConflict {
		return &contracts.BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  models.RescrapeStatusFailed,
			Error:   "Concurrent rescrape conflict",
		}
	}

	if result.Status == models.RescrapeStatusFailed {
		return &contracts.BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  models.RescrapeStatusFailed,
			Error:   result.Error,
		}
	}

	return &contracts.BulkRescrapeMovieResult{
		MovieID: movieID,
		Status:  models.RescrapeStatusSuccess,
		Movie:   contracts.MovieViewFromModel(result.Movie),
	}
}
