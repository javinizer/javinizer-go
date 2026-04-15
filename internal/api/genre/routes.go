package genre

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

func RegisterRoutes(protected *gin.RouterGroup, deps *core.ServerDependencies) {
	replacements := protected.Group("/genres/replacements")
	replacements.GET("", listGenreReplacements(deps))
	replacements.POST("", createGenreReplacement(deps))
	replacements.DELETE("/:original", deleteGenreReplacement(deps))
}
