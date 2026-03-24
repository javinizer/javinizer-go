package temp

import "github.com/gin-gonic/gin"

func RegisterRoutes(protected *gin.RouterGroup, deps *ServerDependencies) {
	protected.GET("/temp/posters/:jobId/:filename", serveTempPoster())
	protected.GET("/temp/image", serveTempImage(deps))
	protected.GET("/posters/:filename", serveCroppedPoster())
}
