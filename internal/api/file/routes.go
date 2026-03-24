package file

import "github.com/gin-gonic/gin"

func RegisterRoutes(protected *gin.RouterGroup, deps *ServerDependencies) {
	protected.GET("/cwd", getCurrentWorkingDirectory(deps))
	protected.POST("/scan", scanDirectory(deps))
	protected.POST("/browse", browseDirectory(deps))
	protected.POST("/browse/autocomplete", autocompletePath(deps))
}
