package temp

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

// RegisterRoutes registers the temporary-file and poster serving routes on the protected router group.
func RegisterRoutes(protected *gin.RouterGroup, rt *core.APIRuntime) {
	protected.GET("/temp/posters/:jobId/:filename", serveTempPoster(rt))
	// HEAD variant retained for clients that want the X-Poster-Revision
	// header (cache generation token for the expected_poster_revision guard)
	// without the body. The review manual-crop client reads the header from
	// the GET that produces the displayed bytes (header and image are then
	// the same generation by construction — no GET-vs-HEAD skew window).
	// Same handler — http.ServeFile serves HEAD bodyless with identical
	// headers when the route exists.
	protected.HEAD("/temp/posters/:jobId/:filename", serveTempPoster(rt))
	protected.GET("/temp/image", serveTempImage(rt))
	protected.GET("/posters/:filename", serveCroppedPoster())
}
