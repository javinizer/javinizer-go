package temp

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

// RegisterRoutes registers the temporary-file and poster serving routes on the protected router group.
func RegisterRoutes(protected *gin.RouterGroup, rt *core.APIRuntime) {
	protected.GET("/temp/posters/:jobId/:filename", serveTempPoster(rt))
	// HEAD variant: the manual-crop client reads the X-Poster-Revision header
	// (cache generation token for the expected_poster_revision guard) without
	// downloading the image bytes again. Same handler — http.ServeFile serves
	// HEAD bodyless with identical headers when the route exists.
	protected.HEAD("/temp/posters/:jobId/:filename", serveTempPoster(rt))
	protected.GET("/temp/image", serveTempImage(rt))
	protected.GET("/posters/:filename", serveCroppedPoster())
}
