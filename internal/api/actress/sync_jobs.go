package actress

import (
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
	Job models.ActressSyncJob `json:"job"`
}
type actressSyncJobsResponse struct {
	Jobs []models.ActressSyncJob `json:"jobs"`
}
type actressSyncTasksResponse struct {
	Tasks []models.ActressSyncTask `json:"tasks"`
	Total int                      `json:"total"`
}

// createActressSyncJob handles POST /api/v1/actresses/sync-jobs.
// @Summary Start an actress metadata sync job
// @Description Queue a background sync job over actresses with missing metadata ("missing") or explicit IDs ("selected").
// @Tags actress
// @Accept json
// @Produce json
// @Param request body worker.ActressSyncCreateRequest true "Sync job request"
// @Success 202 {object} actressSyncJobResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/sync-jobs [post]
func createActressSyncJob(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req worker.ActressSyncCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		if req.Scope != "missing" && req.Scope != "selected" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "scope must be missing or selected"})
			return
		}
		if req.Scope == "selected" && len(req.ActressIDs) == 0 {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "actress_ids is required for selected scope"})
			return
		}
		manager := rt.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		job, err := manager.CreateJob(c.Request.Context(), req)
		if err != nil {
			status := http.StatusInternalServerError
			if database.IsNotFound(err) || isActressSyncValidationError(err) {
				status = http.StatusBadRequest
			}
			c.JSON(status, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, actressSyncJobResponse{Job: *job})
	}
}

// listActiveActressSyncJobs handles GET /api/v1/actresses/sync-jobs/active.
// @Summary List active actress sync jobs
// @Description Return pending and running actress sync jobs, oldest first.
// @Tags actress
// @Produce json
// @Success 200 {object} actressSyncJobsResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/sync-jobs/active [get]
func listActiveActressSyncJobs(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := rt.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		jobs, err := manager.ListActiveJobs()
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
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
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/sync-jobs/{jobID} [get]
func getActressSyncJob(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := rt.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		job, err := manager.GetJob(c.Param("jobID"))
		if err != nil {
			writeActressSyncError(c, err)
			return
		}
		c.JSON(http.StatusOK, actressSyncJobResponse{Job: *job})
	}
}

// listActressSyncJobTasks handles GET /api/v1/actresses/sync-jobs/{jobID}/tasks.
// @Summary List actress sync job tasks
// @Description List tasks of a sync job: all by default, only running tasks with view=active, or the bounded terminal diagnostics with view=diagnostics.
// @Tags actress
// @Produce json
// @Param jobID path string true "Sync job ID"
// @Param view query string false "Task view: 'active' or 'diagnostics' (default: bounded all-tasks list)"
// @Param limit query int false "Max tasks for the default view (1-1000; default 500)" default(500)
// @Success 200 {object} actressSyncTasksResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/sync-jobs/{jobID}/tasks [get]
func listActressSyncJobTasks(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := rt.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		jobID := c.Param("jobID")
		view := c.Query("view")
		var tasks []models.ActressSyncTask
		var err error
		switch view {
		case "active":
			tasks, err = manager.ListRunningTasks(jobID)
		case "diagnostics":
			tasks, err = manager.ListDiagnosticTasks(jobID, 100)
		default:
			limit := 0
			if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
				if parsed, convErr := strconv.Atoi(raw); convErr == nil {
					limit = parsed
				}
			}
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

var (
	ensureSyncManager = func(rt *core.APIRuntime) *worker.ActressSyncManager { return rt.EnsureActressSyncManager() }
	cancelSyncJob     = func(manager *worker.ActressSyncManager, id string) error { return manager.CancelJob(id) }
	getSyncJob        = func(manager *worker.ActressSyncManager, id string) (*models.ActressSyncJob, error) {
		return manager.GetJob(id)
	}
)

// cancelActressSyncJob handles POST /api/v1/actresses/sync-jobs/{jobID}/cancel.
// @Summary Request cancellation of an actress sync job
// @Description Mark the job cancelled: pending tasks are cancelled and running tasks are aborted.
// @Tags actress
// @Produce json
// @Param jobID path string true "Sync job ID"
// @Success 200 {object} actressSyncJobResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/sync-jobs/{jobID}/cancel [post]
func cancelActressSyncJob(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := ensureSyncManager(rt)
		if manager == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		id := c.Param("jobID")
		if err := cancelSyncJob(manager, id); err != nil {
			writeActressSyncError(c, err)
			return
		}
		job, err := getSyncJob(manager, id)
		if err != nil {
			writeActressSyncError(c, err)
			return
		}
		c.JSON(http.StatusOK, actressSyncJobResponse{Job: *job})
	}
}

func isActressSyncValidationError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "scope must be") ||
		strings.Contains(message, "actress_ids is required") ||
		strings.Contains(message, "no actresses require metadata sync")
}

func writeActressSyncError(c *gin.Context, err error) {
	if database.IsNotFound(err) {
		c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
}
