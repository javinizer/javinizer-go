package actress

import (
	"github.com/gin-gonic/gin"
)

func RegisterRoutes(protected *gin.RouterGroup, deps ActressDeps) {
	protected.GET("/actresses", listActresses(deps))
	protected.GET("/actresses/:id", getActress(deps))
	protected.POST("/actresses", createActress(deps))
	protected.PUT("/actresses/:id", updateActress(deps))
	protected.DELETE("/actresses/:id", deleteActress(deps))
	protected.GET("/actresses/search", searchActresses(deps))
	protected.POST("/actresses/merge/preview", previewActressMerge(deps))
	protected.POST("/actresses/merge", mergeActresses(deps))
	protected.GET("/actresses/export", exportActresses(deps))
	protected.POST("/actresses/import", importActresses(deps))
}
