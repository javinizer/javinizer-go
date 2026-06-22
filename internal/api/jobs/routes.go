package jobs

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/middleware"
)

func RegisterRoutes(protected *gin.RouterGroup, deps JobDeps) {
	protected.GET("/jobs", listJobs(deps))

	// All routes with :id param are protected by ValidateJobID middleware
	// to prevent path traversal when jobID is used in filesystem operations.
	jobs := protected.Group("/jobs", middleware.ValidateJobID())
	{
		jobs.GET("/:id", getJob(deps))
		jobs.GET("/:id/operations", listOperations(deps))
		jobs.GET("/:id/revert-check", revertCheck(deps))
		jobs.POST("/:id/revert", revertBatch(deps))
		jobs.POST("/:id/operations/:movieId/revert", revertOperation(deps))
	}
}
