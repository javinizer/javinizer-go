package actress

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

// RegisterRoutes registers the actress CRUD, search, merge, and import/export routes on the given protected router group.
func RegisterRoutes(protected, writeProtected *gin.RouterGroup, deps ActressDeps, rt *core.APIRuntime) {
	protected.GET("/actresses", listActresses(deps))
	writeProtected.POST("/actresses", createActress(deps))
	protected.GET("/actresses/sync-candidates", listActressSyncCandidates(rt))
	writeProtected.POST("/actresses/sync-jobs", createActressSyncJob(rt))
	protected.GET("/actresses/sync-jobs/active", listActiveActressSyncJobs(rt))
	protected.GET("/actresses/sync-jobs/:jobID", getActressSyncJob(rt))
	protected.GET("/actresses/sync-jobs/:jobID/tasks", listActressSyncJobTasks(rt))
	writeProtected.POST("/actresses/sync-jobs/:jobID/cancel", cancelActressSyncJob(rt))
	protected.GET("/actresses/search", searchActresses(deps))
	protected.GET("/actresses/alias-group", getAliasGroup(deps))
	writeProtected.POST("/actresses/merge/preview", previewActressMerge(deps))
	writeProtected.POST("/actresses/merge", mergeActresses(deps))
	protected.GET("/actresses/export", exportActresses(deps))
	writeProtected.POST("/actresses/import", importActresses(deps))
	protected.GET("/actresses/:id", getActress(deps))
	writeProtected.PUT("/actresses/:id", updateActress(deps))
	writeProtected.DELETE("/actresses/:id", deleteActress(deps))
}
