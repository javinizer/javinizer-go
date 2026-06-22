package version

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

func RegisterRoutes(protected *gin.RouterGroup, deps *core.APIDeps) {
	protected.GET("/version", versionStatus(deps.CoreDeps))
	protected.POST("/version/check", versionCheck(deps.CoreDeps))
}
