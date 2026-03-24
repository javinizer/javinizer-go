package version

import "github.com/gin-gonic/gin"

func RegisterRoutes(protected *gin.RouterGroup, deps *ServerDependencies) {
	protected.GET("/version", versionStatus(deps))
	protected.POST("/version/check", versionCheck(deps))
}
