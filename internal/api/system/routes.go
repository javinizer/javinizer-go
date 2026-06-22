package system

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

func RegisterRoutes(protected *gin.RouterGroup, rt *core.APIRuntime) {
	protected.GET("/config", getConfig(rt.Deps()))
	protected.PUT("/config", updateConfig(rt))
	protected.GET("/scrapers", getAvailableScrapers(rt))
	protected.POST("/proxy/test", testProxy(rt))
	protected.POST("/translation/models", getTranslationModels(rt.Deps()))
	protected.POST("/translation/deepl/usage", getDeepLUsage(rt.Deps()))
}

func RegisterCoreRoutes(router *gin.Engine, rt *core.APIRuntime) {
	deps := rt.Deps()
	router.GET("/health", healthCheck(deps))
}
