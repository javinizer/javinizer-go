package temp

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

// RegisterRoutes registers the temporary-file and poster serving routes on the protected router group.
func RegisterRoutes(protected *gin.RouterGroup, rt *core.APIRuntime) {
	protected.GET("/temp/posters/:jobId/:filename", serveTempPoster(rt))
	protected.HEAD("/temp/posters/:jobId/:filename", serveTempPoster(rt))
	protected.GET("/temp/image", serveTempImage(rt))
	protected.HEAD("/temp/image", serveTempImage(rt))
	protected.GET("/posters/:filename", serveCroppedPoster())
}
