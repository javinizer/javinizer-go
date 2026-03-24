package actress

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

func RegisterRoutes(protected *gin.RouterGroup, deps *core.ServerDependencies) {
	protected.GET("/actresses", listActresses(deps.ActressRepo))
	protected.GET("/actresses/:id", getActress(deps.ActressRepo))
	protected.POST("/actresses", createActress(deps.ActressRepo))
	protected.PUT("/actresses/:id", updateActress(deps.ActressRepo))
	protected.DELETE("/actresses/:id", deleteActress(deps.ActressRepo))
	protected.GET("/actresses/search", searchActresses(deps.ActressRepo))
	protected.POST("/actresses/merge/preview", previewActressMerge(deps.ActressRepo))
	protected.POST("/actresses/merge", mergeActresses(deps.ActressRepo))
}
