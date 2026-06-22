package file

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

func RegisterRoutes(protected *gin.RouterGroup, rt *core.APIRuntime) {
	protected.GET("/cwd", getCurrentWorkingDirectory(rt))
	protected.POST("/scan", scanDirectory(rt))
	protected.POST("/browse", browseDirectory(rt))
	protected.POST("/browse/autocomplete", autocompletePath(rt))
}
