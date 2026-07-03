package batch

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/middleware"
)

// RegisterRoutes registers the batch job API routes on the given protected router group.
func RegisterRoutes(protected *gin.RouterGroup, rt *core.APIRuntime) {
	protected.GET("/batch", listBatchJobs(rt))
	protected.POST("/batch/scrape", batchScrape(rt))

	// All routes with :id param are protected by ValidateJobID middleware
	// to prevent path traversal when jobID is used in filesystem operations.
	batch := protected.Group("/batch", middleware.ValidateJobID())
	{
		batch.GET("/:id", getBatchJob(rt))
		batch.DELETE("/:id", deleteBatchJob(rt))
		batch.POST("/:id/cancel", cancelBatchJob(rt))
		batch.PATCH("/:id/results/:resultId", updateBatchMovie(rt))
		batch.POST("/:id/movies/batch-exclude", batchExcludeMovies(rt))
		batch.POST("/:id/movies/batch-rescrape", batchRescrapeMovies(rt))
		batch.GET("/:id/results/:resultId/sources", getBatchMovieSources(rt))
		batch.POST("/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(rt))
		batch.POST("/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(rt))
		batch.POST("/:id/results/:resultId/field-override", overrideBatchMovieField(rt))
		batch.POST("/:id/results/:resultId/exclude", excludeBatchMovie(rt))
		batch.POST("/:id/results/:resultId/preview", previewOrganize(rt))
		batch.POST("/:id/results/:resultId/rescrape", rescrapeBatchMovie(rt))
		batch.POST("/:id/organize", organizeJob(rt))
		batch.POST("/:id/update", updateBatchJob(rt))
	}
}
