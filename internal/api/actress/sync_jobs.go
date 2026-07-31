package actress

import (
	"net/http"
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

func listActressSyncJobTasks(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := rt.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		tasks, err := manager.ListTasks(c.Param("jobID"))
		if err != nil {
			writeActressSyncError(c, err)
			return
		}
		c.JSON(http.StatusOK, actressSyncTasksResponse{Tasks: tasks, Total: len(tasks)})
	}
}

func cancelActressSyncJob(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		manager := rt.EnsureActressSyncManager()
		if manager == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "actress sync manager is unavailable"})
			return
		}
		id := c.Param("jobID")
		if err := manager.CancelJob(id); err != nil {
			writeActressSyncError(c, err)
			return
		}
		job, err := manager.GetJob(id)
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
