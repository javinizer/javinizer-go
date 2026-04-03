package system

import "github.com/gin-gonic/gin"

func RegisterRoutes(protected *gin.RouterGroup, deps *ServerDependencies) {
	protected.GET("/config", getConfig(deps))
	protected.PUT("/config", updateConfig(deps))
	protected.GET("/scrapers", getAvailableScrapers(deps))
	protected.POST("/proxy/test", testProxy(deps))
	protected.POST("/translation/models", getTranslationModels(deps))
	protected.POST("/translation/deepl/usage", getDeepLUsage(deps))
}

func RegisterCoreRoutes(router *gin.Engine, deps *ServerDependencies) {
	router.GET("/health", healthCheck(deps))
}
