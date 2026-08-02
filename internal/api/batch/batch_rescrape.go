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
// broadcast function, and returns per-movie results plus each movie's
// envelope-persist failure (nil per movie on success). Every movie's mint
// persisted its envelope INSIDE its own poster-source locks — a failed
// persist already rolled that movie's memory/provenance/cache back in the
// phase, so there is no cross-movie rollback to schedule here; the
// persistErrs only feed the caller's failure signal.
func bulkRescrapePool(
	ctx context.Context,
	job worker.BatchJobInterface,
	movieIDs []string,
	req *contracts.BatchRescrapeRequest,
	factory worker.BatchJobFactoryInterface,
	progressFn func(movieID string, result *contracts.BulkRescrapeMovieResult, progress float64),
) ([]contracts.BulkRescrapeMovieResult, []error) {
	type rescrapeMovieResult struct {
		movieID    string
		result     *contracts.BulkRescrapeMovieResult
		persistErr error // non-nil when the movie's in-phase envelope persist failed
	}

	var mu sync.Mutex
	var completedCount int
	results := make([]contracts.BulkRescrapeMovieResult, 0, len(movieIDs))
	persistErrs := make([]error, 0, len(movieIDs))

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
				r, persistErr := processBulkRescrapeMovie(ctx, movieID, job, req, factory)
				resultChan <- rescrapeMovieResult{movieID: movieID, result: r, persistErr: persistErr}
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
		if mr.persistErr != nil {
			persistErrs = append(persistErrs, mr.persistErr)
		}
		results = append(results, *mr.result)
		completedCount++
		if progressFn != nil {
			progressFn(mr.movieID, mr.result, float64(completedCount)/float64(len(movieIDs))*100)
		}
		mu.Unlock()
	}

	return results, persistErrs
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

		statusSnap := job.GetStatus()
		if rescrapeNotAllowed(statusSnap) {
			if statusSnap.IsDeleted {
				writeErrorResponse(c, http.StatusGone, true, "Job has been deleted")
			} else {
				writeErrorResponse(c, http.StatusConflict, false, fmt.Sprintf("Cannot rescrape %s job", statusSnap.Status))
			}
			return
		}

		// Delegate to orchestrator for resolve→construct→execute pipeline.
		// Snapshot so the workflow factory and batch job factory see the same
		// reload epoch (issue #44).
		rtSnap := rt.Snapshot()
		factory := rtSnap.BatchJobFactory()
		if factory == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "batch job factory unavailable — workflow factory not ready; retry the request"})
			return
		}
		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  deps.GetJobStore(),
			WfFactory: &apiWorkflowFactory{snap: rtSnap},
			Factory:   factory,
			Broadcast: &runtimeStateBroadcaster{rs: rt.GetRuntime()},
			ServerCtx: rt.ServerCtx(),
		})

		result, err := orch.BulkRescrape(c.Request.Context(), jobID, req.MovieIDs, rescrapeReq)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		jobResponse := buildBatchJobResponse(result.JobStatus)

		respStatus := http.StatusOK
		resp := contracts.BulkRescrapeResponse{
			Results:   result.Results,
			Succeeded: result.Succeeded,
			Failed:    result.Failed,
			Job:       jobResponse,
		}
		if result.PersistErr != nil {
			// The per-file rescrapes committed (Results stay authoritative), but
			// the envelope did not persist: surface a clear failure signal — an
			// error status WITH the per-file results — instead of acking state a
			// restart would resurrect against the rescraped poster images.
			resp.PersistError = result.PersistErr.Error()
			respStatus = http.StatusInternalServerError
		}
		c.JSON(respStatus, resp)
	}
}

// processBulkRescrapeMovie rescrapes one movie. The second return is the
// movie's in-critical-section envelope-persist failure, if any (the phase
// already rolled this movie's memory/provenance/cache back under its own
// locks): such a movie reports RescrapeStatusFailed with the persist
// detail, because its in-memory state reverted to pre-rescrape — acking
// success would misreport what a restart reproduces.
func processBulkRescrapeMovie(ctx context.Context, movieID string, job worker.BatchJobInterface, req *contracts.BatchRescrapeRequest, factory worker.BatchJobFactoryInterface) (*contracts.BulkRescrapeMovieResult, error) {
	mergeOpts, mergeEnabled, mergeErr := resolveRescrapeMergeOptions(req)
	if mergeErr != nil {
		return &contracts.BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  models.RescrapeStatusFailed,
			Error:   fmt.Sprintf("invalid merge options: %v", mergeErr),
		}, nil
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
		}, nil
	}

	if result.Status == models.RescrapeStatusGone {
		return &contracts.BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  models.RescrapeStatusFailed,
			Error:   "Job was deleted during rescrape",
		}, nil
	}

	if result.Status == models.RescrapeStatusConflict {
		return &contracts.BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  models.RescrapeStatusFailed,
			Error:   "Concurrent rescrape conflict",
		}, nil
	}

	if result.Status == models.RescrapeStatusFailed {
		return &contracts.BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  models.RescrapeStatusFailed,
			Error:   result.Error,
		}, nil
	}

	if result.PersistErr != nil {
		// The phase rolled this movie's state back under its held locks; the
		// movie's CURRENT state is pre-rescrape, so report failure (with the
		// persist detail) rather than a success the envelope contradicts.
		return &contracts.BulkRescrapeMovieResult{
			MovieID: movieID,
			Status:  models.RescrapeStatusFailed,
			Error:   result.PersistErr.Error(),
		}, result.PersistErr
	}

	return &contracts.BulkRescrapeMovieResult{
		MovieID: movieID,
		Status:  models.RescrapeStatusSuccess,
		Movie:   contracts.MovieViewFromModel(result.Movie),
	}, nil
}
