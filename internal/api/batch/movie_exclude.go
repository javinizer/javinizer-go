package batch

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
)

const bulkExcludeMaxMovies = 100

// excludeBatchMovie godoc
// @Summary Exclude movie from batch organization
// @Description Mark a movie in a batch job as excluded from file organization
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Param resultId path string true "Result ID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/results/{resultId}/exclude [post]
func excludeBatchMovie(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")
		resultID := c.Param("resultId")

		job, ok := deps.GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}

		result, filePaths, found := lookupResultByResultID(job, resultID)

		if !found {
			logging.Debugf("[ExcludeBatchMovie] No matches found for resultID=%s", resultID)
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
			return
		}

		movieID := result.FileMatchInfo.MovieID

		// Mark ALL parts as excluded (handles multi-part files like CD1, CD2, etc.)
		// ExcludeFile auto-cancels the job when all files are excluded.
		logging.Debugf("[ExcludeBatchMovie] Excluding %d file(s) for movieID=%s", len(filePaths), movieID)
		for _, filePath := range filePaths {
			job.ExcludeFile(filePath)
			logging.Debugf("[ExcludeBatchMovie] Excluded: %s", filePath)
		}

		logging.Infof("Movie %s (%d file(s)) excluded from batch job %s", movieID, len(filePaths), jobID)

		c.JSON(http.StatusOK, gin.H{"message": "Movie excluded from organization"})
	}
}

// batchExcludeMovies godoc
// @Summary Bulk exclude movies from batch organization
// @Description Exclude multiple movies from a batch job in a single request. Best-effort: excludes as many as possible and returns per-result failures.
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param request body contracts.BatchExcludeRequest true "Result IDs to exclude"
// @Success 200 {object} contracts.BatchExcludeResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/movies/batch-exclude [post]
func batchExcludeMovies(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")

		var req contracts.BatchExcludeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		if len(req.ResultIDs) == 0 {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "result_ids is required and must not be empty"})
			return
		}

		if len(req.ResultIDs) > bulkExcludeMaxMovies {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: fmt.Sprintf("result_ids must not exceed %d items", bulkExcludeMaxMovies)})
			return
		}

		job, ok := deps.GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}

		var excluded []string
		var failed []contracts.BatchExcludeFailed

		for _, resultID := range req.ResultIDs {
			result, filePaths, found := lookupResultByResultID(job, resultID)

			if !found {
				failed = append(failed, contracts.BatchExcludeFailed{
					ResultID: resultID,
					Error:    fmt.Sprintf("Result %s not found in job", resultID),
				})
				continue
			}

			for _, filePath := range filePaths {
				job.ExcludeFile(filePath)
			}
			excluded = append(excluded, result.FileMatchInfo.MovieID)
		}

		logging.Infof("Batch exclude: %d movie(s) excluded, %d failed from batch job %s", len(excluded), len(failed), jobID)

		updatedStatus := job.GetStatus()
		jobResponse := buildBatchJobResponse(updatedStatus)

		if excluded == nil {
			excluded = []string{}
		}
		if failed == nil {
			failed = []contracts.BatchExcludeFailed{}
		}

		c.JSON(http.StatusOK, contracts.BatchExcludeResponse{
			Excluded: excluded,
			Failed:   failed,
			Job:      jobResponse,
		})
	}
}
