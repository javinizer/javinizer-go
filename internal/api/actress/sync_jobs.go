package actress

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
)

type actressSyncJobResponse struct {
	Job        models.ActressSyncJob `json:"job"`
	SkippedIDs []uint                `json:"skipped_ids"`
}
type actressSyncJobsResponse struct {
	Jobs []models.ActressSyncJob `json:"jobs"`
}
type actressSyncTasksResponse struct {
	Tasks []models.ActressSyncTask `json:"tasks"`
	Total int                      `json:"total"`
}
type actressSyncNoCandidatesResponse struct {
	Error      string `json:"error"`
	SkippedIDs []uint `json:"skipped_ids"`
}

const maxActressSyncSelectedIDs = 10000

// createActressSyncJob handles POST /api/v1/actresses/sync-jobs.
// @Summary Start an actress metadata sync job
// @Description Queue a background sync job over actresses with missing metadata or explicit IDs.
// @Tags actress
// @Accept json
// @Produce json
// @Param request body worker.ActressSyncCreateRequest true "Sync job request"
// @Success 202 {object} actressSyncJobResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 409 {object} actressSyncNoCandidatesResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/sync-jobs [post]
func createActressSyncJob(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req worker.ActressSyncCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		scope := strings.TrimSpace(req.Scope)
		if scope != "missing" && scope != "selected" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "scope must be missing or selected"})
			return
		}
		if scope == "selected" {
			if len(req.ActressIDs) == 0 {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "actress_ids is required for selected scope"})
				return
			}
			if len(req.ActressIDs) > maxActressSyncSelectedIDs {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "actress_ids must not exceed 10000 entries"})
				return
			}
		}
		manager := rt.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		job, skippedIDs, err := manager.CreateJob(c.Request.Context(), req)
		if err != nil {
			var noCandidates *worker.NoCandidatesError
			if errors.As(err, &noCandidates) || errors.Is(err, database.ErrActressSyncNoCandidates) {
				c.JSON(http.StatusConflict, actressSyncNoCandidatesResponse{Error: "no_candidates", SkippedIDs: normalizeIDs(skippedIDs)})
				return
			}
			if errors.Is(err, worker.ErrActressSyncManagerUnavailable) {
				c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "actress sync manager is unavailable"})
				return
			}
			core.RespondInternalError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, actressSyncJobResponse{Job: *job, SkippedIDs: normalizeIDs(skippedIDs)})
	}
}

// listActiveActressSyncJobs handles GET /api/v1/actresses/sync-jobs/active.
// @Summary List active actress sync jobs
// @Description Return pending and running actress sync jobs, oldest first.
// @Tags actress
// @Produce json
// @Success 200 {object} actressSyncJobsResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/sync-jobs/active [get]
func listActiveActressSyncJobs(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := rt.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		jobs, err := manager.ListActiveJobs()
		if err != nil {
			writeActressSyncError(c, err)
			return
		}
		c.JSON(http.StatusOK, actressSyncJobsResponse{Jobs: jobs})
	}
}

// getActressSyncJob handles GET /api/v1/actresses/sync-jobs/{jobID}.
// @Summary Get an actress sync job
// @Description Return a single actress sync job with its aggregate counters.
// @Tags actress
// @Produce json
// @Param jobID path string true "Sync job ID"
// @Success 200 {object} actressSyncJobResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/sync-jobs/{jobID} [get]
func getActressSyncJob(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := rt.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "actress sync job not found"})
			return
		}
		job, err := manager.GetJob(c.Param("jobID"))
		if err != nil {
			writeActressSyncError(c, err)
			return
		}
		c.JSON(http.StatusOK, actressSyncJobResponse{Job: *job, SkippedIDs: []uint{}})
	}
}

// listActressSyncJobTasks handles GET /api/v1/actresses/sync-jobs/{jobID}/tasks.
// @Summary List actress sync job tasks
// @Description List tasks of a sync job: all by default, only running tasks with view=active, or the bounded terminal diagnostics with view=diagnostics.
// @Tags actress
// @Produce json
// @Param jobID path string true "Sync job ID"
// @Param view query string false "Task view: active or diagnostics (default: bounded all-tasks list)"
// @Param limit query int false "Max tasks for the default view (1-1000; default 500)" default(500)
// @Success 200 {object} actressSyncTasksResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/sync-jobs/{jobID}/tasks [get]
func listActressSyncJobTasks(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := rt.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "actress sync job not found"})
			return
		}
		jobID := c.Param("jobID")
		view := c.Query("view")
		if view != "" && view != "active" && view != "diagnostics" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "view must be active, diagnostics, or omitted"})
			return
		}
		limit, limitErr := parseActressSyncLimit(c)
		if limitErr != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: limitErr.Error()})
			return
		}
		var tasks []models.ActressSyncTask
		var err error
		switch view {
		case "active":
			tasks, err = manager.ListRunningTasks(jobID)
		case "diagnostics":
			tasks, err = manager.ListDiagnosticTasks(jobID, 100)
		default:
			tasks, err = manager.ListTasks(jobID, limit)
		}
		if err != nil {
			writeActressSyncError(c, err)
			return
		}
		total, err := manager.CountTasks(jobID, view)
		if err != nil {
			writeActressSyncError(c, err)
			return
		}
		c.JSON(http.StatusOK, actressSyncTasksResponse{Tasks: tasks, Total: int(total)})
	}
}

// cancelActressSyncJob handles POST /api/v1/actresses/sync-jobs/{jobID}/cancel.
// @Summary Request cancellation of an actress sync job
// @Description Mark the job cancelled: pending tasks are cancelled and running tasks are aborted.
// @Tags actress
// @Produce json
// @Param jobID path string true "Sync job ID"
// @Success 200 {object} actressSyncJobResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/sync-jobs/{jobID}/cancel [post]
func cancelActressSyncJob(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := rt.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		id := c.Param("jobID")
		job, err := manager.GetJob(id)
		if err != nil {
			writeActressSyncError(c, err)
			return
		}
		if job.Status != models.ActressSyncJobPending && job.Status != models.ActressSyncJobRunning {
			c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: "job is already terminal"})
			return
		}
		if err := manager.CancelJob(id); err != nil {
			writeActressSyncError(c, err)
			return
		}
		job, err = manager.GetJob(id)
		if err != nil {
			writeActressSyncError(c, err)
			return
		}
		c.JSON(http.StatusOK, actressSyncJobResponse{Job: *job, SkippedIDs: []uint{}})
	}
}

func parseActressSyncLimit(c *gin.Context) (int, error) {
	raw := strings.TrimSpace(c.Query("limit"))
	if raw == "" {
		return 500, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("limit must be an integer")
	}
	if parsed <= 0 {
		return 0, errors.New("limit must be positive")
	}
	if parsed > 1000 {
		parsed = 1000
	}
	return parsed, nil
}

func normalizeIDs(ids []uint) []uint {
	if ids == nil {
		return []uint{}
	}
	return ids
}

func writeActressSyncError(c *gin.Context, err error) {
	if errors.Is(err, worker.ErrActressSyncManagerUnavailable) {
		c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "actress sync manager is unavailable"})
		return
	}
	if database.IsNotFound(err) {
		c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: err.Error()})
		return
	}
	core.RespondInternalError(c, err)
}
