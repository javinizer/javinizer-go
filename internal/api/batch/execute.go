package batch

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// prepareAndLaunchApply handles the common workflow for organize and update handlers:
// get batch workflow → set WF on job → launch StartApply in a goroutine.
// Per S-8: extracted to eliminate the 60% code duplication between organizeJob and updateBatchJob.
func prepareAndLaunchApply(
	c *gin.Context,
	rt *core.APIRuntime,
	job worker.BatchJobInterface,
	applyOpts worker.ApplyPhaseConfig,
	successMessage string,
) {
	wf, wfErr := rt.GetBatchWorkflow(job.GetID())
	if wfErr != nil {
		c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Failed to create workflow: %v", wfErr)})
		return
	}

	// Per DEEP-6: set WF on the job's deps before calling StartApply.
	job.SetWorkflow(wf)

	go func() {
		if err := job.StartApply(rt.ServerCtx(), applyOpts); err != nil {
			logging.Errorf("BatchJob.StartApply failed: %v", err)
			rt.Deps().GetJobStore().PersistJobByID(job.GetID())
			return
		}

		if err := job.Wait(); err != nil {
			logging.Warnf("job %s Wait() returned: %v", job.GetID(), err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": successMessage})
}

// organizeJob godoc
// @Summary Organize batch job files
// @Description Organize files from a completed scrape job (move files, download artwork, create NFO)
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param request body contracts.OrganizeRequest true "Organization parameters"
// @Success 200 {object} map[string]string
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/organize [post]
func organizeJob(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		var req contracts.OrganizeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		job, err := prepareBatchRequest(deps, rt, c, withRequireCompleted("Job must be completed before organizing"))
		if err != nil {
			return
		}

		applyOpts, resolveErr := resolveOrganizeApplyConfig(rt, rt.GetBatchJobFactory(), job, req)
		if resolveErr != nil {
			if resolveErr.Error() == "Access denied to requested directory" {
				c.JSON(http.StatusForbidden, contracts.ErrorResponse{Error: resolveErr.Error()})
			} else {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: resolveErr.Error()})
			}
			return
		}

		prepareAndLaunchApply(c, rt, job, applyOpts, "Organization started")
	}
}

// updateBatchJob godoc
// @Summary Update batch job files
// @Description Generate NFOs and download media files in place without moving video files
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param request body contracts.UpdateRequest false "Update options (optional, backward compatible)"
// @Success 200 {object} map[string]string
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/update [post]
func updateBatchJob(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")

		job, ok := deps.GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}

		if job.GetJobStatus() == models.JobStatusRunning {
			c.JSON(http.StatusConflict, gin.H{"error": "job is already running"})
			return
		}

		status := job.GetStatus()
		if status.Status != models.JobStatusCompleted {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Job must be completed before updating"})
			return
		}

		var req contracts.UpdateRequest
		if c.Request.Body != nil && c.Request.ContentLength != 0 {
			bodyBytes, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
			if err != nil {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Failed to read request body"})
				return
			}
			if len(bodyBytes) > 0 {
				c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Invalid request body: " + err.Error()})
					return
				}
				if req.ForceOverwrite && req.PreserveNFO {
					c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "force_overwrite and preserve_nfo are mutually exclusive"})
					return
				}
			}
		}

		applyOpts, err := resolveUpdateApplyConfig(rt, rt.GetBatchJobFactory(), job, req)
		if err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		prepareAndLaunchApply(c, rt, job, applyOpts, "Update started")
	}
}

// previewOrganize godoc
// @Summary Preview organize output
// @Description Generate a preview of the expected output structure for a movie
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param resultId path string true "Result ID"
// @Param request body contracts.OrganizePreviewRequest true "Preview parameters"
// @Success 200 {object} contracts.OrganizePreviewResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/results/{resultId}/preview [post]
func previewOrganize(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		resultID := c.Param("resultId")

		var req contracts.OrganizePreviewRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		apiCfg := rt.GetAPIConfig()
		batchCfg := apiCfg.BatchConfig()
		secCfg := apiCfg.SecurityConfig()

		resolvedPreview, resolveErr := workflow.ResolveSeamStrings(workflow.SeamStringsInput{
			OperationMode: func() string {
				if req.OperationMode != "" {
					return req.OperationMode
				}
				return batchCfg.OperationMode
			}(),
		})
		if resolveErr != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: resolveErr.Error()})
			return
		}

		effectiveMode := resolvedPreview.OperationMode

		if effectiveMode == operationmode.OperationModeOrganize || effectiveMode == operationmode.OperationModePreview {
			deps := rt.Deps()
			if req.Destination == "" {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "destination is required for organize and preview modes"})
				return
			}
			if !isDirAllowed(deps.GetFs(), req.Destination, secCfg) {
				c.JSON(http.StatusForbidden, contracts.ErrorResponse{Error: "Access denied to requested directory"})
				return
			}
		}

		// Resolve preview data: lookup job, find movie, collect file match infos
		movieData, fileMatchInfos, previewResolveErr := ResolvePreviewData(rt.Deps(), jobID, resultID, req)
		if previewResolveErr != nil {
			previewResolveErr.Write(c)
			return
		}

		// Generate preview for all file results (multi-part support)
		wf, wfErr := rt.GetBatchWorkflow(jobID)
		if wfErr != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Failed to create workflow for preview: %v", wfErr)})
			return
		}
		previewResult, previewErr := wf.Preview(c.Request.Context(), workflow.PreviewCmd{
			Movie:         movieData,
			FileResults:   fileMatchInfos,
			Destination:   req.Destination,
			OperationMode: resolvedPreview.OperationMode,
			SkipNFO:       req.SkipNFO,
			SkipDownload:  req.SkipDownload,
		})
		if previewErr != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Preview failed: %v", previewErr)})
			return
		}
		preview := previewResultToResponse(previewResult)
		c.JSON(http.StatusOK, preview)
	}
}

// previewResolveError is a structured error that knows how to write itself
// as an HTTP response, decoupling the resolution logic from gin.Context.
type previewResolveError struct {
	Status int
	Err    string
}

func (e *previewResolveError) Error() string { return e.Err }

// Write sends the error as a JSON response on the given gin.Context.
func (e *previewResolveError) Write(c *gin.Context) {
	c.JSON(e.Status, contracts.ErrorResponse{Error: e.Err})
}

// ResolvePreviewData resolves the movie data and file match infos needed for
// a preview request. It looks up the job, finds the result by resultID,
// resolves the movie (including multi-part), and returns sorted file match
// infos. Returns a previewResolveError for HTTP-layer errors so the caller
// can write the response.
func ResolvePreviewData(deps *core.APIDeps, jobID string, resultID string, req contracts.OrganizePreviewRequest) (*models.Movie, []models.FileMatchInfo, *previewResolveError) {
	job, ok := deps.GetJobStore().GetBatchJob(jobID)
	if !ok {
		return nil, nil, &previewResolveError{Status: http.StatusNotFound, Err: "Job not found"}
	}

	// Resolve the result by resultID to get the movieID for multi-part lookup
	result, _, found := lookupResultByResultID(job, resultID)
	if !found {
		return nil, nil, &previewResolveError{Status: http.StatusNotFound, Err: fmt.Sprintf("Result %s not found in job", resultID)}
	}

	movieID := result.FileMatchInfo.MovieID

	fileResults := job.GetMovieResultsForMovieID(movieID)
	if len(fileResults) == 0 {
		return nil, nil, &previewResolveError{Status: http.StatusNotFound, Err: fmt.Sprintf("Movie %s not found in job", movieID)}
	}

	// Use the movie override from the request if provided (for previewing unsaved edits),
	// otherwise fall back to the movie data stored in the job results
	var movieData *models.Movie
	if req.Movie != nil {
		movieData = contracts.MovieViewToModel(req.Movie)
	} else {
		for _, result := range fileResults {
			if result.Movie != nil {
				movieData = result.Movie
				break
			}
		}
	}

	if movieData == nil {
		return nil, nil, &previewResolveError{Status: http.StatusNotFound, Err: fmt.Sprintf("Movie %s not found in job", movieID)}
	}

	// Sort fileResults by PartNumber to ensure deterministic order
	// (map iteration order is random in Go, so fileResults[0] might not be part 1)
	sort.Slice(fileResults, func(i, j int) bool {
		return fileResults[i].FileMatchInfo.PartNumber < fileResults[j].FileMatchInfo.PartNumber
	})

	// Extract FileMatchInfos from the sorted results to preserve PartNumber order
	fileMatchInfos := make([]models.FileMatchInfo, 0, len(fileResults))
	for _, result := range fileResults {
		fileMatchInfos = append(fileMatchInfos, result.FileMatchInfo)
	}

	return movieData, fileMatchInfos, nil
}
