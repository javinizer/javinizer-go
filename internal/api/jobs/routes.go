package jobs

import "github.com/gin-gonic/gin"

func RegisterRoutes(protected *gin.RouterGroup, deps *ServerDependencies) {
	protected.GET("/jobs", listJobs(deps))
	protected.GET("/jobs/:id", getJob(deps))
	protected.GET("/jobs/:id/operations", listOperations(deps))
	protected.GET("/jobs/:id/revert-check", revertCheck(deps))
	protected.POST("/jobs/:id/revert", revertBatch(deps))
	protected.POST("/jobs/:id/operations/:movieId/revert", revertOperation(deps))
}
