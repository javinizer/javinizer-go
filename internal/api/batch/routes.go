package batch

import "github.com/gin-gonic/gin"

func RegisterRoutes(protected *gin.RouterGroup, deps *ServerDependencies) {
	protected.GET("/batch", listBatchJobs(deps))
	protected.POST("/batch/scrape", batchScrape(deps))
	protected.GET("/batch/:id", getBatchJob(deps))
	protected.POST("/batch/:id/cancel", cancelBatchJob(deps))
	protected.PATCH("/batch/:id/movies/:movieId", updateBatchMovie(deps))
	protected.POST("/batch/:id/movies/:movieId/poster-crop", updateBatchMoviePosterCrop(deps))
	protected.POST("/batch/:id/movies/:movieId/exclude", excludeBatchMovie(deps))
	protected.POST("/batch/:id/movies/:movieId/preview", previewOrganize(deps))
	protected.POST("/batch/:id/movies/:movieId/rescrape", rescrapeBatchMovie(deps))
	protected.POST("/batch/:id/organize", organizeJob(deps))
	protected.POST("/batch/:id/update", updateBatchJob(deps))
}
